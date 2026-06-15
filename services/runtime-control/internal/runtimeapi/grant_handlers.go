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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtimeaccess "mcp-runtime-control/internal/runtimeapi/access"
	mcpv1alpha1 "mcp-runtime/api/v1alpha1"
	sentinelaccess "mcp-runtime/pkg/access"
)

func validateGrantRequest(req *accessGrantRequest) error {
	req.Name = strings.TrimSpace(req.Name)
	req.Namespace = runtimeaccess.DefaultAccessNamespace(req.Namespace)
	req.ServerRef.Name = sentinelaccess.ServerName(strings.TrimSpace(string(req.ServerRef.Name)))
	req.ServerRef.Namespace = sentinelaccess.Namespace(strings.TrimSpace(string(req.ServerRef.Namespace)))
	req.Subject.HumanID = sentinelaccess.HumanID(strings.TrimSpace(string(req.Subject.HumanID)))
	req.Subject.AgentID = sentinelaccess.AgentID(strings.TrimSpace(string(req.Subject.AgentID)))
	req.Subject.TeamID = sentinelaccess.TeamID(strings.TrimSpace(string(req.Subject.TeamID)))
	req.PolicyVersion = runtimeaccess.DefaultPolicyVersion(req.PolicyVersion)
	req.MaxTrust = runtimeaccess.NormalizeTrust(req.MaxTrust)
	if err := sentinelaccess.ValidateResourceName("name", req.Name); err != nil {
		return err
	}
	if err := sentinelaccess.ValidateResourceName("namespace", req.Namespace); err != nil {
		return err
	}
	if err := sentinelaccess.ValidateResourceName("serverRef.name", string(req.ServerRef.Name)); err != nil {
		return err
	}
	if err := sentinelaccess.ValidateOptionalResourceName("serverRef.namespace", string(req.ServerRef.Namespace)); err != nil {
		return err
	}
	if req.Subject.HumanID == "" && req.Subject.AgentID == "" && req.Subject.TeamID == "" {
		return errors.New("one of subject.humanID, subject.agentID, or subject.teamID is required")
	}
	if err := runtimeaccess.ValidateTeamIDValue("subject.teamID", string(req.Subject.TeamID)); err != nil {
		return err
	}
	if req.MaxTrust != "" && !runtimeaccess.ValidTrust(req.MaxTrust) {
		return errors.New("maxTrust must be low, medium, or high")
	}
	if len(req.AllowedSideEffects) == 0 {
		return errors.New("at least one allowed side effect is required")
	}
	seenSideEffects := map[sentinelaccess.ToolSideEffect]struct{}{}
	for i := range req.AllowedSideEffects {
		req.AllowedSideEffects[i] = runtimeaccess.NormalizeSideEffect(req.AllowedSideEffects[i])
		if req.AllowedSideEffects[i] == "" {
			return fmt.Errorf("allowedSideEffects[%d] is required", i)
		}
		if !runtimeaccess.ValidSideEffect(req.AllowedSideEffects[i]) {
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
		req.ToolRules[i].RequiredTrust = runtimeaccess.NormalizeTrust(req.ToolRules[i].RequiredTrust)
		if req.ToolRules[i].Name == "" {
			return fmt.Errorf("toolRules[%d].name is required", i)
		}
		if !runtimeaccess.ValidDecision(req.ToolRules[i].Decision) {
			return fmt.Errorf("toolRules[%d].decision must be allow or deny", i)
		}
		if req.ToolRules[i].RequiredTrust != "" && !runtimeaccess.ValidTrust(req.ToolRules[i].RequiredTrust) {
			return fmt.Errorf("toolRules[%d].requiredTrust must be low, medium, or high", i)
		}
	}
	return nil
}

func (s *RuntimeServer) handleRuntimeGrantList(w http.ResponseWriter, r *http.Request) {
	if s.accessMgr == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "kubernetes not available")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	namespace, err := s.scopedNamespaceForPrincipal(r.Context(), r.URL.Query().Get("namespace"))
	if err != nil {
		writeAPIError(w, http.StatusForbidden, err.Error())
		return
	}
	grants, err := s.accessMgr.ListGrants(ctx, namespace)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to list grants")
		return
	}

	p, filterByPrincipal := principalFromContext(ctx)
	filterByPrincipal = filterByPrincipal && p.Role != roleAdmin
	var serverCache accessServerCache
	if filterByPrincipal {
		serverCache, err = s.accessServerCacheForGrantRefs(ctx, namespace, grants.Items)
		if err != nil {
			log.Printf("runtime grant list: list MCPServers for visibility failed: %v", err)
			writeAPIError(w, http.StatusInternalServerError, "failed to inspect server references")
			return
		}
	}

	summaries := make([]sentinelaccess.GrantSummary, 0, len(grants.Items))
	for _, g := range grants.Items {
		if filterByPrincipal && !runtimeaccess.AccessRefVisibleWithServerCache(g.Namespace, g.Spec.ServerRef, serverCache,
			func(server mcpv1alpha1.MCPServer) bool { return principalCanAdministerMCPServer(p, server) },
			func(namespace string, serverLabels map[string]string) bool {
				return principalCanAdministerServerLabels(p, namespace, serverLabels)
			},
		) {
			continue
		}
		summaries = append(summaries, sentinelaccess.ToGrantSummary(g))
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"grants": summaries})
}

func (s *RuntimeServer) handleRuntimeGrantApply(w http.ResponseWriter, r *http.Request) {
	if s.accessMgr == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "kubernetes not available")
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
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	scopedNamespace, err := s.scopedAccessWriteNamespaceForPrincipal(r.Context(), req.Namespace)
	if err != nil {
		writeAPIError(w, http.StatusForbidden, err.Error())
		return
	}
	req.Namespace = scopedNamespace
	if err := runtimeaccess.BindAccessServerRefNamespace(req.Namespace, &req.ServerRef); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	targetServer, err := s.accessMgr.GetMCPServerRef(ctx, req.ServerRef)
	if err != nil {
		if sentinelaccess.IsMCPServerNotFoundForRef(err) {
			writeAPIError(w, http.StatusBadRequest, err.Error())
		} else {
			log.Printf("runtime grant: assert MCPServer ref failed: %v", err)
			writeAPIError(w, http.StatusInternalServerError, "failed to verify server reference")
		}
		return
	}
	if err := s.bindAccessSubjectTeamID(ctx, req.Namespace, targetServer.Spec.TeamID, &req.Subject); err != nil {
		writeAPIError(w, http.StatusForbidden, err.Error())
		return
	}
	if !s.principalCanAdministerAccessServer(r.Context(), *targetServer) {
		writeAPIError(w, http.StatusForbidden, "forbidden server")
		return
	}

	disabled, err := s.grantDisabledForApply(ctx, req)
	if err != nil {
		log.Printf("read grant state %s/%s failed: %v", req.Namespace, req.Name, err)
		writeAPIError(w, http.StatusInternalServerError, "failed to read grant state")
		return
	}

	grant := &sentinelaccess.MCPAccessGrant{
		ObjectMeta: metav1.ObjectMeta{
			Name:      req.Name,
			Namespace: runtimeaccess.DefaultAccessNamespace(req.Namespace),
		},
		Spec: sentinelaccess.MCPAccessGrantSpec{
			ServerRef:          req.ServerRef,
			Subject:            req.Subject,
			MaxTrust:           req.MaxTrust,
			AllowedSideEffects: req.AllowedSideEffects,
			PolicyVersion:      runtimeaccess.DefaultPolicyVersion(req.PolicyVersion),
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
