package runtimeapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	mcpv1alpha1 "mcp-runtime/api/v1alpha1"
	sentinelaccess "mcp-runtime/pkg/access"
	"mcp-runtime/pkg/k8sclient"
	"mcp-runtime/pkg/serviceutil"
)

type accessGrantRequest struct {
	Name               string                          `json:"name"`
	Namespace          string                          `json:"namespace"`
	ServerRef          sentinelaccess.ServerReference  `json:"serverRef"`
	Subject            sentinelaccess.SubjectRef       `json:"subject"`
	MaxTrust           sentinelaccess.TrustLevel       `json:"maxTrust"`
	AllowedSideEffects []sentinelaccess.ToolSideEffect `json:"allowedSideEffects"`
	PolicyVersion      string                          `json:"policyVersion"`
	Disabled           *bool                           `json:"disabled,omitempty"`
	ToolRules          []sentinelaccess.ToolRule       `json:"toolRules"`
}

type accessSessionRequest struct {
	Name           string                         `json:"name"`
	Namespace      string                         `json:"namespace"`
	ServerRef      sentinelaccess.ServerReference `json:"serverRef"`
	Subject        sentinelaccess.SubjectRef      `json:"subject"`
	ConsentedTrust sentinelaccess.TrustLevel      `json:"consentedTrust"`
	ExpiresAt      *metav1.Time                   `json:"expiresAt"`
	Revoked        *bool                          `json:"revoked,omitempty"`
	PolicyVersion  string                         `json:"policyVersion"`
}

func (s *RuntimeServer) HandleRuntimeGrants(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleRuntimeGrantList(w, r)
	case http.MethodPost:
		s.handleRuntimeGrantApply(w, r)
	default:
		w.Header().Set("allow", "GET, POST")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

func (s *RuntimeServer) handleRuntimeGrantList(w http.ResponseWriter, r *http.Request) {
	if s.accessMgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "kubernetes not available"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	namespace, err := s.scopedNamespaceForPrincipal(r.Context(), r.URL.Query().Get("namespace"))
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}
	grants, err := s.accessMgr.ListGrants(ctx, namespace)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list grants"})
		return
	}

	p, filterByPrincipal := principalFromContext(ctx)
	filterByPrincipal = filterByPrincipal && p.Role != roleAdmin
	var serverCache accessServerCache
	if filterByPrincipal {
		serverCache, err = s.accessServerCacheForGrantRefs(ctx, namespace, grants.Items)
		if err != nil {
			log.Printf("runtime grant list: list MCPServers for visibility failed: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to inspect server references"})
			return
		}
	}

	summaries := make([]sentinelaccess.GrantSummary, 0, len(grants.Items))
	for _, g := range grants.Items {
		if filterByPrincipal && !accessRefVisibleWithServerCache(p, g.Namespace, g.Spec.ServerRef, serverCache) {
			continue
		}
		summaries = append(summaries, sentinelaccess.ToGrantSummary(g))
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"grants": summaries})
}

func (s *RuntimeServer) handleRuntimeGrantApply(w http.ResponseWriter, r *http.Request) {
	if s.accessMgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "kubernetes not available"})
		return
	}

	var req accessGrantRequest
	r.Body = http.MaxBytesReader(w, r.Body, accessApplyMaxBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBodyDecodeError(w, err)
		return
	}
	if p, ok := principalFromContext(r.Context()); ok && p.Role != roleAdmin && strings.TrimSpace(req.Namespace) == "" {
		req.Namespace = strings.TrimSpace(p.Namespace)
	}
	if err := validateGrantRequest(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	scopedNamespace, err := s.scopedAccessWriteNamespaceForPrincipal(r.Context(), req.Namespace)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}
	req.Namespace = scopedNamespace
	if err := bindAccessServerRefNamespace(req.Namespace, &req.ServerRef); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// serverRef is checked with a live Get, not a transaction with ApplyGrant. Another actor
	// may delete the MCPServer after this call; the grant can still be written. Clients should retry on policy errors.
	targetServer, err := s.accessMgr.GetMCPServerRef(ctx, req.ServerRef)
	if err != nil {
		if sentinelaccess.IsMCPServerNotFoundForRef(err) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		} else {
			log.Printf("runtime grant: assert MCPServer ref failed: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to verify server reference"})
		}
		return
	}
	if err := s.bindAccessSubjectTeamID(ctx, req.Namespace, targetServer.Spec.TeamID, &req.Subject); err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}
	if !s.principalCanAdministerAccessServer(r.Context(), *targetServer) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden server"})
		return
	}

	disabled, err := s.grantDisabledForApply(ctx, req)
	if err != nil {
		log.Printf("read grant state %s/%s failed: %v", req.Namespace, req.Name, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read grant state"})
		return
	}

	grant := &sentinelaccess.MCPAccessGrant{
		ObjectMeta: metav1.ObjectMeta{
			Name:      req.Name,
			Namespace: defaultAccessNamespace(req.Namespace),
		},
		Spec: sentinelaccess.MCPAccessGrantSpec{
			ServerRef:          req.ServerRef,
			Subject:            req.Subject,
			MaxTrust:           req.MaxTrust,
			AllowedSideEffects: req.AllowedSideEffects,
			PolicyVersion:      defaultPolicyVersion(req.PolicyVersion),
			Disabled:           disabled,
			ToolRules:          req.ToolRules,
		},
	}
	applied, err := s.accessMgr.ApplyGrant(ctx, grant)
	if err != nil {
		writeK8sApplyError(w, "grant", grant.Namespace, grant.Name, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"grant": sentinelaccess.ToGrantSummary(*applied)})
}

// HandleRuntimeSessions returns MCPAgentSession resources.
func (s *RuntimeServer) HandleRuntimeSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleRuntimeSessionList(w, r)
	case http.MethodPost:
		s.handleRuntimeSessionApply(w, r)
	default:
		w.Header().Set("allow", "GET, POST")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

func (s *RuntimeServer) handleRuntimeSessionList(w http.ResponseWriter, r *http.Request) {
	if s.accessMgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "kubernetes not available"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	namespace, err := s.scopedNamespaceForPrincipal(r.Context(), r.URL.Query().Get("namespace"))
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}
	sessions, err := s.accessMgr.ListSessions(ctx, namespace)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list sessions"})
		return
	}

	p, filterByPrincipal := principalFromContext(ctx)
	filterByPrincipal = filterByPrincipal && p.Role != roleAdmin
	var serverCache accessServerCache
	if filterByPrincipal {
		serverCache, err = s.accessServerCacheForSessionRefs(ctx, namespace, sessions.Items)
		if err != nil {
			log.Printf("runtime session list: list MCPServers for visibility failed: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to inspect server references"})
			return
		}
	}

	summaries := make([]sentinelaccess.SessionSummary, 0, len(sessions.Items))
	for _, sess := range sessions.Items {
		if filterByPrincipal && !accessRefVisibleWithServerCache(p, sess.Namespace, sess.Spec.ServerRef, serverCache) {
			continue
		}
		summaries = append(summaries, sentinelaccess.ToSessionSummary(sess))
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"sessions": summaries})
}

func (s *RuntimeServer) handleRuntimeSessionApply(w http.ResponseWriter, r *http.Request) {
	if s.accessMgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "kubernetes not available"})
		return
	}
	if p, ok := principalFromContext(r.Context()); ok && p.Role != roleAdmin {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin role required"})
		return
	}

	var req accessSessionRequest
	r.Body = http.MaxBytesReader(w, r.Body, accessApplyMaxBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBodyDecodeError(w, err)
		return
	}
	if p, ok := principalFromContext(r.Context()); ok && p.Role != roleAdmin && strings.TrimSpace(req.Namespace) == "" {
		req.Namespace = strings.TrimSpace(p.Namespace)
	}
	if err := validateSessionRequest(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	scopedNamespace, err := s.scopedAccessWriteNamespaceForPrincipal(r.Context(), req.Namespace)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}
	req.Namespace = scopedNamespace
	if err := bindAccessServerRefNamespace(req.Namespace, &req.ServerRef); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// See handleRuntimeGrantApply: serverRef check is not transactional with the session write.
	targetServer, err := s.accessMgr.GetMCPServerRef(ctx, req.ServerRef)
	if err != nil {
		if sentinelaccess.IsMCPServerNotFoundForRef(err) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		} else {
			log.Printf("runtime session: assert MCPServer ref failed: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to verify server reference"})
		}
		return
	}
	if err := s.bindAccessSubjectTeamID(ctx, req.Namespace, targetServer.Spec.TeamID, &req.Subject); err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}
	if !s.principalCanAdministerAccessServer(r.Context(), *targetServer) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden server"})
		return
	}

	revoked, err := s.sessionRevokedForApply(ctx, req)
	if err != nil {
		log.Printf("read session state %s/%s failed: %v", req.Namespace, req.Name, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read session state"})
		return
	}

	session := &sentinelaccess.MCPAgentSession{
		ObjectMeta: metav1.ObjectMeta{
			Name:      req.Name,
			Namespace: defaultAccessNamespace(req.Namespace),
		},
		Spec: sentinelaccess.MCPAgentSessionSpec{
			ServerRef:      req.ServerRef,
			Subject:        req.Subject,
			ConsentedTrust: req.ConsentedTrust,
			ExpiresAt:      req.ExpiresAt,
			Revoked:        revoked,
			PolicyVersion:  defaultPolicyVersion(req.PolicyVersion),
		},
	}
	applied, err := s.accessMgr.ApplySession(ctx, session)
	if err != nil {
		writeK8sApplyError(w, "session", session.Namespace, session.Name, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"session": sentinelaccess.ToSessionSummary(*applied)})
}

func (s *RuntimeServer) grantDisabledForApply(ctx context.Context, req accessGrantRequest) (bool, error) {
	if req.Disabled != nil {
		return *req.Disabled, nil
	}
	existing, err := s.accessMgr.GetGrant(ctx, req.Name, defaultAccessNamespace(req.Namespace))
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return existing.Spec.Disabled, nil
}

func (s *RuntimeServer) sessionRevokedForApply(ctx context.Context, req accessSessionRequest) (bool, error) {
	if req.Revoked != nil {
		return *req.Revoked, nil
	}
	existing, err := s.accessMgr.GetSession(ctx, req.Name, defaultAccessNamespace(req.Namespace))
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return existing.Spec.Revoked, nil
}

func (s *RuntimeServer) grantVisibleToPrincipal(ctx context.Context, grant sentinelaccess.MCPAccessGrant) bool {
	if p, ok := principalFromContext(ctx); !ok || p.Role == roleAdmin {
		return true
	}
	allowed, err := s.canAdministerAccessServerRef(ctx, grant.Namespace, grant.Spec.ServerRef)
	return err == nil && allowed
}

func (s *RuntimeServer) sessionVisibleToPrincipal(ctx context.Context, session sentinelaccess.MCPAgentSession) bool {
	if p, ok := principalFromContext(ctx); !ok || p.Role == roleAdmin {
		return true
	}
	allowed, err := s.canAdministerAccessServerRef(ctx, session.Namespace, session.Spec.ServerRef)
	return err == nil && allowed
}

type accessServerCache map[string]mcpv1alpha1.MCPServer

func (s *RuntimeServer) accessServerCacheForGrantRefs(ctx context.Context, namespace string, grants []sentinelaccess.MCPAccessGrant) (accessServerCache, error) {
	refs := make([]sentinelaccess.ServerReference, 0, len(grants))
	for _, grant := range grants {
		refs = append(refs, grant.Spec.ServerRef)
	}
	return s.accessServerCacheForRefs(ctx, namespace, refs)
}

func (s *RuntimeServer) accessServerCacheForSessionRefs(ctx context.Context, namespace string, sessions []sentinelaccess.MCPAgentSession) (accessServerCache, error) {
	refs := make([]sentinelaccess.ServerReference, 0, len(sessions))
	for _, session := range sessions {
		refs = append(refs, session.Spec.ServerRef)
	}
	return s.accessServerCacheForRefs(ctx, namespace, refs)
}

func (s *RuntimeServer) accessServerCacheForRefs(ctx context.Context, namespace string, refs []sentinelaccess.ServerReference) (accessServerCache, error) {
	namespaces := map[string]struct{}{}
	for _, ref := range refs {
		if strings.TrimSpace(ref.Name) == "" {
			continue
		}
		namespaces[accessServerRefNamespace(namespace, ref)] = struct{}{}
	}

	cache := accessServerCache{}
	for ns := range namespaces {
		servers, err := s.accessMgr.ListMCPServers(ctx, ns)
		if err != nil {
			return nil, err
		}
		for _, server := range servers.Items {
			cache[accessServerCacheKey(server.Namespace, server.Name)] = server
		}
	}
	return cache, nil
}

func accessRefVisibleWithServerCache(p principal, resourceNamespace string, ref sentinelaccess.ServerReference, cache accessServerCache) bool {
	serverNamespace := accessServerRefNamespace(resourceNamespace, ref)
	if server, ok := cache[accessServerCacheKey(serverNamespace, ref.Name)]; ok {
		return principalCanAdministerMCPServer(p, server)
	}
	return principalCanAdministerServerLabels(p, defaultAccessNamespace(resourceNamespace), nil)
}

func accessServerRefNamespace(resourceNamespace string, ref sentinelaccess.ServerReference) string {
	if ns := strings.TrimSpace(ref.Namespace); ns != "" {
		return ns
	}
	return defaultAccessNamespace(resourceNamespace)
}

func accessServerCacheKey(namespace, name string) string {
	return strings.TrimSpace(namespace) + "\x00" + strings.TrimSpace(name)
}

func (s *RuntimeServer) HandleGrantItemPath(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ns, name, err := extractNamespacedPath(r.URL.Path, "/api/runtime/grants/", 2)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		s.handleGrantGet(w, r, ns, name)
		return
	case http.MethodDelete:
		ns, name, err := serviceutil.ExtractNamespacedResourceDelete(r, "/api/runtime/grants/")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		s.handleGrantDelete(w, r, ns, name)
		return
	case http.MethodPost:
		s.handleGrantPostTogglePath(w, r)
		return
	default:
		w.Header().Set("allow", "GET, POST, DELETE")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

func (s *RuntimeServer) handleGrantGet(w http.ResponseWriter, r *http.Request, namespace, name string) {
	if s.accessMgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "kubernetes not available"})
		return
	}
	namespace, err := s.scopedNamespaceForPrincipal(r.Context(), namespace)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	grant, err := s.accessMgr.GetGrant(ctx, name, namespace)
	if err != nil {
		code, msg := k8sclient.HTTPStatusFromK8sError(err)
		writeJSON(w, code, map[string]string{"error": msg})
		return
	}
	if !s.grantVisibleToPrincipal(ctx, *grant) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden server"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"grant": sentinelaccess.ToGrantSummary(*grant)})
}

func (s *RuntimeServer) handleGrantDelete(w http.ResponseWriter, r *http.Request, namespace, name string) {
	if s.accessMgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "kubernetes not available"})
		return
	}
	namespace, err := s.scopedAccessWriteNamespaceForPrincipal(r.Context(), namespace)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	grant, err := s.accessMgr.GetGrant(ctx, name, namespace)
	if err != nil {
		code, msg := k8sclient.HTTPStatusFromK8sError(err)
		log.Printf("delete grant %s/%s failed before authz (status=%d): %v", namespace, name, code, err)
		writeJSON(w, code, map[string]string{"error": fmt.Sprintf("failed to delete grant: %s", msg)})
		return
	}
	allowed, err := s.canAdministerAccessServerRef(ctx, namespace, grant.Spec.ServerRef)
	if err != nil {
		log.Printf("delete grant %s/%s authz lookup failed: %v", namespace, name, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to verify server reference"})
		return
	}
	if !allowed {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden server"})
		return
	}
	if err := s.accessMgr.DeleteGrant(ctx, name, namespace); err != nil {
		code, msg := k8sclient.HTTPStatusFromK8sError(err)
		log.Printf("delete grant %s/%s failed (status=%d): %v", namespace, name, code, err)
		writeJSON(w, code, map[string]string{"error": fmt.Sprintf("failed to delete grant: %s", msg)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"name":      name,
		"namespace": namespace,
	})
}

// handleGrantPostTogglePath handles POST /api/runtime/grants/{namespace}/{name}/disable|enable
func (s *RuntimeServer) handleGrantPostTogglePath(w http.ResponseWriter, r *http.Request) {
	params, err := serviceutil.ExtractGrantActionParams(r, "/api/runtime/grants/")
	if err != nil {
		if errors.Is(err, serviceutil.ErrMethodNotAllowed) {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": err.Error()})
		} else {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		return
	}

	disable := !serviceutil.IsActionEnabled(params.Action)
	s.handleGrantToggle(w, r, params.Namespace, params.Name, disable)
}

func (s *RuntimeServer) handleGrantToggle(w http.ResponseWriter, r *http.Request, namespace, name string, disable bool) {
	if s.accessMgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "kubernetes not available"})
		return
	}

	namespace, nsErr := s.scopedAccessWriteNamespaceForPrincipal(r.Context(), namespace)
	if nsErr != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": nsErr.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	grant, err := s.accessMgr.GetGrant(ctx, name, namespace)
	if err != nil {
		code, msg := k8sclient.HTTPStatusFromK8sError(err)
		writeJSON(w, code, map[string]string{"error": msg})
		return
	}
	allowed, err := s.canAdministerAccessServerRef(ctx, namespace, grant.Spec.ServerRef)
	if err != nil {
		log.Printf("toggle grant %s/%s authz lookup failed: %v", namespace, name, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to verify server reference"})
		return
	}
	if !allowed {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden server"})
		return
	}

	var updateErr error
	if disable {
		updateErr = s.accessMgr.DisableGrant(ctx, name, namespace)
	} else {
		updateErr = s.accessMgr.EnableGrant(ctx, name, namespace)
	}

	if updateErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update grant"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"name":      name,
		"namespace": namespace,
		"disabled":  disable,
	})
}

func validateGrantRequest(req *accessGrantRequest) error {
	req.Name = strings.TrimSpace(req.Name)
	req.Namespace = defaultAccessNamespace(req.Namespace)
	req.ServerRef.Name = strings.TrimSpace(req.ServerRef.Name)
	req.ServerRef.Namespace = strings.TrimSpace(req.ServerRef.Namespace)
	req.Subject.HumanID = strings.TrimSpace(req.Subject.HumanID)
	req.Subject.AgentID = strings.TrimSpace(req.Subject.AgentID)
	req.Subject.TeamID = strings.TrimSpace(req.Subject.TeamID)
	req.PolicyVersion = defaultPolicyVersion(req.PolicyVersion)
	req.MaxTrust = normalizeTrust(req.MaxTrust)
	if err := sentinelaccess.ValidateResourceName("name", req.Name); err != nil {
		return err
	}
	if err := sentinelaccess.ValidateResourceName("namespace", req.Namespace); err != nil {
		return err
	}
	if err := sentinelaccess.ValidateResourceName("serverRef.name", req.ServerRef.Name); err != nil {
		return err
	}
	if err := sentinelaccess.ValidateOptionalResourceName("serverRef.namespace", req.ServerRef.Namespace); err != nil {
		return err
	}
	if req.Subject.HumanID == "" && req.Subject.AgentID == "" && req.Subject.TeamID == "" {
		return errors.New("one of subject.humanID, subject.agentID, or subject.teamID is required")
	}
	if err := validateTeamIDValue("subject.teamID", req.Subject.TeamID); err != nil {
		return err
	}
	if req.MaxTrust != "" && !validTrust(req.MaxTrust) {
		return errors.New("maxTrust must be low, medium, or high")
	}
	if len(req.AllowedSideEffects) == 0 {
		return errors.New("at least one allowed side effect is required")
	}
	seenSideEffects := map[sentinelaccess.ToolSideEffect]struct{}{}
	for i := range req.AllowedSideEffects {
		req.AllowedSideEffects[i] = normalizeSideEffect(req.AllowedSideEffects[i])
		if req.AllowedSideEffects[i] == "" {
			return fmt.Errorf("allowedSideEffects[%d] is required", i)
		}
		if !validSideEffect(req.AllowedSideEffects[i]) {
			return fmt.Errorf("allowedSideEffects[%d] must be read, write, or destructive", i)
		}
		if _, ok := seenSideEffects[req.AllowedSideEffects[i]]; ok {
			return fmt.Errorf("allowedSideEffects[%d] is a duplicate", i)
		}
		seenSideEffects[req.AllowedSideEffects[i]] = struct{}{}
	}
	for i := range req.ToolRules {
		req.ToolRules[i].Name = strings.TrimSpace(req.ToolRules[i].Name)
		req.ToolRules[i].Decision = sentinelaccess.PolicyDecision(strings.TrimSpace(string(req.ToolRules[i].Decision)))
		req.ToolRules[i].RequiredTrust = normalizeTrust(req.ToolRules[i].RequiredTrust)
		if req.ToolRules[i].Name == "" {
			return fmt.Errorf("toolRules[%d].name is required", i)
		}
		if !validDecision(req.ToolRules[i].Decision) {
			return fmt.Errorf("toolRules[%d].decision must be allow or deny", i)
		}
		if req.ToolRules[i].RequiredTrust != "" && !validTrust(req.ToolRules[i].RequiredTrust) {
			return fmt.Errorf("toolRules[%d].requiredTrust must be low, medium, or high", i)
		}
	}
	return nil
}

func validateSessionRequest(req *accessSessionRequest) error {
	req.Name = strings.TrimSpace(req.Name)
	req.Namespace = defaultAccessNamespace(req.Namespace)
	req.ServerRef.Name = strings.TrimSpace(req.ServerRef.Name)
	req.ServerRef.Namespace = strings.TrimSpace(req.ServerRef.Namespace)
	req.Subject.HumanID = strings.TrimSpace(req.Subject.HumanID)
	req.Subject.AgentID = strings.TrimSpace(req.Subject.AgentID)
	req.Subject.TeamID = strings.TrimSpace(req.Subject.TeamID)
	req.PolicyVersion = defaultPolicyVersion(req.PolicyVersion)
	req.ConsentedTrust = normalizeTrust(req.ConsentedTrust)
	if err := sentinelaccess.ValidateResourceName("name", req.Name); err != nil {
		return err
	}
	if err := sentinelaccess.ValidateResourceName("namespace", req.Namespace); err != nil {
		return err
	}
	if err := sentinelaccess.ValidateResourceName("serverRef.name", req.ServerRef.Name); err != nil {
		return err
	}
	if err := sentinelaccess.ValidateOptionalResourceName("serverRef.namespace", req.ServerRef.Namespace); err != nil {
		return err
	}
	if req.Subject.HumanID == "" && req.Subject.AgentID == "" && req.Subject.TeamID == "" {
		return errors.New("one of subject.humanID, subject.agentID, or subject.teamID is required")
	}
	if err := validateTeamIDValue("subject.teamID", req.Subject.TeamID); err != nil {
		return err
	}
	if req.ConsentedTrust != "" && !validTrust(req.ConsentedTrust) {
		return errors.New("consentedTrust must be low, medium, or high")
	}
	return nil
}

func defaultAccessNamespace(namespace string) string {
	if namespace = strings.TrimSpace(namespace); namespace != "" {
		return namespace
	}
	return sentinelaccess.DefaultMCPResourceNamespace
}

func defaultPolicyVersion(policyVersion string) string {
	if policyVersion = strings.TrimSpace(policyVersion); policyVersion != "" {
		return policyVersion
	}
	return "v1"
}

func writeK8sApplyError(w http.ResponseWriter, kind, namespace, name string, err error) {
	code, msg := k8sclient.HTTPStatusFromK8sError(err)
	log.Printf("apply %s %s/%s failed (status=%d): %v", kind, namespace, name, code, err)
	writeJSON(w, code, map[string]string{"error": fmt.Sprintf("failed to apply %s: %s", kind, msg)})
}

func normalizeTrust(trust sentinelaccess.TrustLevel) sentinelaccess.TrustLevel {
	return sentinelaccess.TrustLevel(strings.TrimSpace(string(trust)))
}

func normalizeSideEffect(sideEffect sentinelaccess.ToolSideEffect) sentinelaccess.ToolSideEffect {
	return sentinelaccess.ToolSideEffect(strings.TrimSpace(string(sideEffect)))
}

func validTrust(trust sentinelaccess.TrustLevel) bool {
	switch trust {
	case "low", "medium", "high":
		return true
	default:
		return false
	}
}

func validSideEffect(sideEffect sentinelaccess.ToolSideEffect) bool {
	switch sideEffect {
	case "read", "write", "destructive":
		return true
	default:
		return false
	}
}

func validDecision(decision sentinelaccess.PolicyDecision) bool {
	switch decision {
	case "allow", "deny":
		return true
	default:
		return false
	}
}

// HandleSessionItemPath handles POST /api/runtime/sessions/{namespace}/{name}/revoke|unrevoke
// and DELETE /api/runtime/sessions/{namespace}/{name}.
func (s *RuntimeServer) HandleSessionItemPath(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ns, name, err := extractNamespacedPath(r.URL.Path, "/api/runtime/sessions/", 2)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		s.handleSessionGet(w, r, ns, name)
		return
	case http.MethodDelete:
		ns, name, err := serviceutil.ExtractNamespacedResourceDelete(r, "/api/runtime/sessions/")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		s.handleSessionDelete(w, r, ns, name)
		return
	case http.MethodPost:
		s.handleSessionPostTogglePath(w, r)
		return
	default:
		w.Header().Set("allow", "GET, POST, DELETE")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

func (s *RuntimeServer) handleSessionGet(w http.ResponseWriter, r *http.Request, namespace, name string) {
	if s.accessMgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "kubernetes not available"})
		return
	}
	namespace, err := s.scopedNamespaceForPrincipal(r.Context(), namespace)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	session, err := s.accessMgr.GetSession(ctx, name, namespace)
	if err != nil {
		code, msg := k8sclient.HTTPStatusFromK8sError(err)
		writeJSON(w, code, map[string]string{"error": msg})
		return
	}
	if !s.sessionVisibleToPrincipal(ctx, *session) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden server"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session": sentinelaccess.ToSessionSummary(*session)})
}

func extractNamespacedPath(path, prefix string, expectedParts int) (string, string, error) {
	path = strings.TrimPrefix(path, prefix)
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != expectedParts {
		return "", "", fmt.Errorf("invalid path")
	}
	namespace := strings.TrimSpace(parts[0])
	name := strings.TrimSpace(parts[1])
	if namespace == "" || name == "" {
		return "", "", fmt.Errorf("invalid path")
	}
	if err := sentinelaccess.ValidateResourceName("namespace", namespace); err != nil {
		return "", "", err
	}
	if err := sentinelaccess.ValidateResourceName("name", name); err != nil {
		return "", "", err
	}
	return namespace, name, nil
}

func (s *RuntimeServer) handleSessionDelete(w http.ResponseWriter, r *http.Request, namespace, name string) {
	if s.accessMgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "kubernetes not available"})
		return
	}
	namespace, err := s.scopedAccessWriteNamespaceForPrincipal(r.Context(), namespace)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	session, err := s.accessMgr.GetSession(ctx, name, namespace)
	if err != nil {
		code, msg := k8sclient.HTTPStatusFromK8sError(err)
		log.Printf("delete session %s/%s failed before authz (status=%d): %v", namespace, name, code, err)
		writeJSON(w, code, map[string]string{"error": fmt.Sprintf("failed to delete session: %s", msg)})
		return
	}
	allowed, err := s.canAdministerAccessServerRef(ctx, namespace, session.Spec.ServerRef)
	if err != nil {
		log.Printf("delete session %s/%s authz lookup failed: %v", namespace, name, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to verify server reference"})
		return
	}
	if !allowed {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden server"})
		return
	}
	if err := s.accessMgr.DeleteSession(ctx, name, namespace); err != nil {
		code, msg := k8sclient.HTTPStatusFromK8sError(err)
		log.Printf("delete session %s/%s failed (status=%d): %v", namespace, name, code, err)
		writeJSON(w, code, map[string]string{"error": fmt.Sprintf("failed to delete session: %s", msg)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"name":      name,
		"namespace": namespace,
	})
}

// handleSessionPostTogglePath handles POST /api/runtime/sessions/{namespace}/{name}/revoke|unrevoke
func (s *RuntimeServer) handleSessionPostTogglePath(w http.ResponseWriter, r *http.Request) {
	params, err := serviceutil.ExtractSessionActionParams(r, "/api/runtime/sessions/")
	if err != nil {
		if errors.Is(err, serviceutil.ErrMethodNotAllowed) {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": err.Error()})
		} else {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		return
	}

	revoke := !serviceutil.IsActionEnabled(params.Action)
	s.handleSessionToggle(w, r, params.Namespace, params.Name, revoke)
}

func (s *RuntimeServer) handleSessionToggle(w http.ResponseWriter, r *http.Request, namespace, name string, revoke bool) {
	if s.accessMgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "kubernetes not available"})
		return
	}

	namespace, nsErr := s.scopedAccessWriteNamespaceForPrincipal(r.Context(), namespace)
	if nsErr != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": nsErr.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	session, err := s.accessMgr.GetSession(ctx, name, namespace)
	if err != nil {
		code, msg := k8sclient.HTTPStatusFromK8sError(err)
		writeJSON(w, code, map[string]string{"error": msg})
		return
	}
	allowed, err := s.canAdministerAccessServerRef(ctx, namespace, session.Spec.ServerRef)
	if err != nil {
		log.Printf("toggle session %s/%s authz lookup failed: %v", namespace, name, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to verify server reference"})
		return
	}
	if !allowed {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden server"})
		return
	}

	var updateErr error
	if revoke {
		updateErr = s.accessMgr.RevokeSession(ctx, name, namespace)
	} else {
		updateErr = s.accessMgr.UnrevokeSession(ctx, name, namespace)
	}

	if updateErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update session"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"name":      name,
		"namespace": namespace,
		"revoked":   revoke,
	})
}

func (s *RuntimeServer) scopedNamespaceForPrincipal(ctx context.Context, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	p, ok := principalFromContext(ctx)
	if !ok || p.Role == roleAdmin {
		return requested, nil
	}
	if requested == "" {
		if sharedCatalogWritableForUsers() {
			return defaultCatalogNamespaceForMode(), nil
		}
		if preferred := strings.TrimSpace(p.Namespace); preferred != "" {
			return preferred, nil
		}
		return "", errPrincipalIdentityRequired
	}
	if !principalCanReadNamespace(p, requested) {
		return "", errors.New("forbidden namespace")
	}
	return requested, nil
}

func (s *RuntimeServer) scopedAccessWriteNamespaceForPrincipal(ctx context.Context, requested string) (string, error) {
	namespace, err := s.scopedNamespaceForPrincipal(ctx, requested)
	if err != nil {
		return "", err
	}
	p, ok := principalFromContext(ctx)
	if ok && p.Role != roleAdmin && namespace == sharedCatalogNamespace {
		return "", errors.New("shared catalog namespace is read-only for access resources")
	}
	return namespace, nil
}

func bindAccessServerRefNamespace(resourceNamespace string, serverRef *sentinelaccess.ServerReference) error {
	resourceNamespace = defaultAccessNamespace(resourceNamespace)
	serverRef.Namespace = strings.TrimSpace(serverRef.Namespace)
	if serverRef.Namespace == "" {
		serverRef.Namespace = resourceNamespace
		return nil
	}
	if serverRef.Namespace != resourceNamespace {
		return fmt.Errorf("serverRef.namespace %q must match access resource namespace %q", serverRef.Namespace, resourceNamespace)
	}
	return nil
}

func (s *RuntimeServer) bindAccessSubjectTeamID(ctx context.Context, namespace, serverTeamID string, subject *sentinelaccess.SubjectRef) error {
	subject.TeamID = strings.TrimSpace(subject.TeamID)
	serverTeamID = strings.TrimSpace(serverTeamID)
	namespaceTeamID := strings.TrimSpace(s.teamIDForPrincipalNamespace(ctx, namespace))
	if subject.TeamID == "" {
		subject.TeamID = firstNonEmpty(serverTeamID, namespaceTeamID)
	}
	if err := validateTeamIDValue("subject.teamID", subject.TeamID); err != nil {
		return err
	}
	p, ok := principalFromContext(ctx)
	if ok && p.Role != roleAdmin && namespaceTeamID == "" && subject.TeamID != "" {
		return errors.New("subject.teamID is only allowed in a team namespace")
	}
	return nil
}

func (s *RuntimeServer) teamIDForPrincipalNamespace(ctx context.Context, namespace string) string {
	namespace = strings.TrimSpace(namespace)
	if p, ok := principalFromContext(ctx); ok {
		if team, found := p.TeamForNamespace(namespace); found {
			return strings.TrimSpace(team.ID)
		}
	}
	if s != nil && s.platform != nil && namespace != "" {
		if record, found, err := s.platform.GetNamespace(ctx, namespace); err == nil && found {
			return strings.TrimSpace(fmt.Sprint(record["team_id"]))
		}
	}
	return ""
}

func validateTeamIDValue(name, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if strings.ContainsAny(value, " \t\r\n") {
		return fmt.Errorf("%s must not contain whitespace", name)
	}
	if len(value) > 128 {
		return fmt.Errorf("%s must be at most 128 characters", name)
	}
	return nil
}
