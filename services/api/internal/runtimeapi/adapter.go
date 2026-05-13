package runtimeapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	sentinelaccess "mcp-runtime/pkg/access"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Adapter-session bounds: the issued session's lifetime is constrained so a
// compromised cached identity has a bounded blast radius. The defaults are
// generous enough for a long-running agent process to avoid hot-path refreshes
// but short enough to cycle credentials within a working day.
const (
	adapterSessionDefaultTTL = time.Hour
	adapterSessionMaxTTL     = 24 * time.Hour
	// adapterSessionRefreshBuffer is the minimum remaining lifetime an existing
	// session must have to be reused. Anything under this triggers a fresh
	// session so the caller does not race expiry mid-conversation.
	adapterSessionRefreshBuffer   = 30 * time.Second
	adapterSessionRequestMaxBytes = 16 << 10
)

// adapterSessionRequest is the input contract for POST /api/runtime/adapter/sessions.
type adapterSessionRequest struct {
	ServerName     string `json:"serverName"`
	Namespace      string `json:"namespace"`
	AgentID        string `json:"agentID"`
	RequestedTrust string `json:"requestedTrust,omitempty"`
	RequestedTTL   string `json:"requestedTTL,omitempty"`
}

// adapterSessionResponse is the body returned on success.
type adapterSessionResponse struct {
	Name           string    `json:"name"`
	Namespace      string    `json:"namespace"`
	HumanID        string    `json:"humanID"`
	AgentID        string    `json:"agentID"`
	TeamID         string    `json:"teamID,omitempty"`
	ServerName     string    `json:"serverName"`
	ConsentedTrust string    `json:"consentedTrust"`
	PolicyVersion  string    `json:"policyVersion"`
	ExpiresAt      time.Time `json:"expiresAt"`
	Reused         bool      `json:"reused"`
}

// HandleAdapterSession issues (or reuses) an MCPAgentSession for an adapter
// call. The platform — not the adapter — picks the matching grant, caps the
// trust at the grant's ceiling, and writes the session resource. The adapter
// then attaches the returned identity fields as governance headers on every
// request to the runtime gateway.
//
// Errors:
//   - 400 when body decoding or input validation fails
//   - 401 when no Principal is on the request (auth middleware should reject first)
//   - 403 when no matching enabled grant is found, or the principal lacks the team
//   - 503 when Kubernetes is unavailable
func (s *RuntimeServer) HandleAdapterSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if s.accessMgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "kubernetes not available"})
		return
	}

	var req adapterSessionRequest
	r.Body = http.MaxBytesReader(w, r.Body, adapterSessionRequestMaxBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBodyDecodeError(w, err)
		return
	}
	req.ServerName = strings.TrimSpace(req.ServerName)
	req.Namespace = strings.TrimSpace(req.Namespace)
	req.AgentID = strings.TrimSpace(req.AgentID)

	principal, ok := principalFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "no principal on request"})
		return
	}

	if req.ServerName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "serverName is required"})
		return
	}
	if req.AgentID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "agentID is required"})
		return
	}
	if req.Namespace == "" {
		// Default to the principal's primary namespace so single-team callers
		// don't have to pass it on every request.
		req.Namespace = principal.Namespace
	}

	humanID := strings.TrimSpace(principal.Subject)
	if humanID == "" {
		humanID = strings.TrimSpace(principal.Email)
	}
	if humanID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "principal has no subject or email"})
		return
	}

	teamID, err := principalTeamForNamespace(principal, req.Namespace)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}

	requestedTrust, err := parseAdapterTrust(req.RequestedTrust)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	requestedTTL, err := parseAdapterTTL(req.RequestedTTL)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	grant, err := s.selectAdapterGrant(ctx, req.Namespace, req.ServerName, humanID, req.AgentID, teamID)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}

	consentedTrust := capTrust(requestedTrust, grant.Spec.MaxTrust)
	policyVersion := defaultPolicyVersion(grant.Spec.PolicyVersion)
	sessionName := adapterSessionName(humanID, req.AgentID, teamID, req.ServerName)
	expiresAt := time.Now().UTC().Add(requestedTTL)

	// Reuse an existing session when its identity, policy version, and trust
	// still match and it has enough remaining lifetime to be useful.
	existing, _ := s.accessMgr.GetSession(ctx, sessionName, req.Namespace)
	if existing != nil && adapterSessionReusable(existing, policyVersion, consentedTrust) {
		writeJSON(w, http.StatusOK, adapterSessionResponse{
			Name:           existing.Name,
			Namespace:      existing.Namespace,
			HumanID:        existing.Spec.Subject.HumanID,
			AgentID:        existing.Spec.Subject.AgentID,
			TeamID:         existing.Spec.Subject.TeamID,
			ServerName:     existing.Spec.ServerRef.Name,
			ConsentedTrust: string(existing.Spec.ConsentedTrust),
			PolicyVersion:  existing.Spec.PolicyVersion,
			ExpiresAt:      existing.Spec.ExpiresAt.Time,
			Reused:         true,
		})
		return
	}

	session := &sentinelaccess.MCPAgentSession{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sessionName,
			Namespace: defaultAccessNamespace(req.Namespace),
		},
		Spec: sentinelaccess.MCPAgentSessionSpec{
			ServerRef: sentinelaccess.ServerReference{
				Name:      req.ServerName,
				Namespace: defaultAccessNamespace(req.Namespace),
			},
			Subject: sentinelaccess.SubjectRef{
				HumanID: humanID,
				AgentID: req.AgentID,
				TeamID:  teamID,
			},
			ConsentedTrust: consentedTrust,
			ExpiresAt:      &metav1.Time{Time: expiresAt},
			PolicyVersion:  policyVersion,
		},
	}
	applied, err := s.accessMgr.ApplySession(ctx, session)
	if err != nil {
		log.Printf("adapter session apply %s/%s failed: %v", session.Namespace, session.Name, err)
		writeK8sApplyError(w, "adapter session", session.Namespace, session.Name, err)
		return
	}
	writeJSON(w, http.StatusOK, adapterSessionResponse{
		Name:           applied.Name,
		Namespace:      applied.Namespace,
		HumanID:        applied.Spec.Subject.HumanID,
		AgentID:        applied.Spec.Subject.AgentID,
		TeamID:         applied.Spec.Subject.TeamID,
		ServerName:     applied.Spec.ServerRef.Name,
		ConsentedTrust: string(applied.Spec.ConsentedTrust),
		PolicyVersion:  applied.Spec.PolicyVersion,
		ExpiresAt:      applied.Spec.ExpiresAt.Time,
		Reused:         false,
	})
}

// principalTeamForNamespace resolves the team identity the principal must hold
// for the supplied namespace. Admin principals (no team binding) are allowed
// to operate without a team; everyone else must be a member of the team that
// owns the namespace.
func principalTeamForNamespace(p principal, namespace string) (string, error) {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return "", errors.New("namespace is required")
	}
	if team, ok := p.TeamForNamespace(namespace); ok {
		return strings.TrimSpace(team.ID), nil
	}
	if p.Role == roleAdmin {
		return "", nil
	}
	return "", fmt.Errorf("principal is not a member of namespace %q", namespace)
}

// selectAdapterGrant lists enabled MCPAccessGrants in namespace whose serverRef
// matches and whose subject either matches the caller exactly or leaves the
// field empty (wildcard). When multiple grants match, the one with the highest
// MaxTrust wins; ties are broken by oldest creationTimestamp so the result is
// deterministic across replicas.
func (s *RuntimeServer) selectAdapterGrant(ctx context.Context, namespace, serverName, humanID, agentID, teamID string) (*sentinelaccess.MCPAccessGrant, error) {
	grants, err := s.accessMgr.ListGrants(ctx, namespace)
	if err != nil {
		return nil, fmt.Errorf("list grants in %s: %w", namespace, err)
	}
	var matches []sentinelaccess.MCPAccessGrant
	for _, g := range grants.Items {
		if g.Spec.Disabled {
			continue
		}
		if g.Spec.ServerRef.Name != serverName {
			continue
		}
		if !subjectMatches(g.Spec.Subject, humanID, agentID, teamID) {
			continue
		}
		matches = append(matches, g)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no enabled MCPAccessGrant in %s matches server=%q humanID=%q agentID=%q",
			namespace, serverName, humanID, agentID)
	}
	sort.SliceStable(matches, func(i, j int) bool {
		ti := trustRank(matches[i].Spec.MaxTrust)
		tj := trustRank(matches[j].Spec.MaxTrust)
		if ti != tj {
			return ti > tj
		}
		return matches[i].CreationTimestamp.Time.Before(matches[j].CreationTimestamp.Time)
	})
	g := matches[0]
	return &g, nil
}

// subjectMatches treats empty fields on the grant's subject as wildcards,
// preserving the SubjectRef semantics already used by the gateway when
// evaluating policy at runtime.
func subjectMatches(subj sentinelaccess.SubjectRef, humanID, agentID, teamID string) bool {
	if subj.HumanID != "" && subj.HumanID != humanID {
		return false
	}
	if subj.AgentID != "" && subj.AgentID != agentID {
		return false
	}
	if subj.TeamID != "" && subj.TeamID != teamID {
		return false
	}
	return true
}

// adapterSessionName derives a deterministic resource name from the caller's
// identity so repeated calls with the same identity converge on a single
// MCPAgentSession (and re-use it as long as it remains valid).
func adapterSessionName(humanID, agentID, teamID, serverName string) string {
	h := sha256.New()
	h.Write([]byte(humanID))
	h.Write([]byte{0})
	h.Write([]byte(agentID))
	h.Write([]byte{0})
	h.Write([]byte(teamID))
	h.Write([]byte{0})
	h.Write([]byte(serverName))
	digest := hex.EncodeToString(h.Sum(nil))[:16]
	return "adapter-" + digest
}

// adapterSessionReusable reports whether an existing session can be returned
// to the caller as-is without writing to Kubernetes. Reuse fails closed: if
// any condition is unmet we issue a fresh session.
func adapterSessionReusable(s *sentinelaccess.MCPAgentSession, policyVersion string, consentedTrust sentinelaccess.TrustLevel) bool {
	if s == nil || s.Spec.Revoked {
		return false
	}
	if s.Spec.PolicyVersion != policyVersion {
		return false
	}
	if string(s.Spec.ConsentedTrust) != string(consentedTrust) {
		return false
	}
	if s.Spec.ExpiresAt == nil {
		return false
	}
	if time.Until(s.Spec.ExpiresAt.Time) <= adapterSessionRefreshBuffer {
		return false
	}
	return true
}

// parseAdapterTrust normalises the caller's requested trust level. Empty
// requests default to "low" (least privilege) so callers explicitly opt in to
// higher trust. Unknown values are rejected.
func parseAdapterTrust(raw string) (sentinelaccess.TrustLevel, error) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	switch raw {
	case "":
		return sentinelaccess.TrustLow, nil
	case string(sentinelaccess.TrustNone), string(sentinelaccess.TrustLow),
		string(sentinelaccess.TrustMid), string(sentinelaccess.TrustHigh),
		string(sentinelaccess.TrustFull):
		return sentinelaccess.TrustLevel(raw), nil
	default:
		return "", fmt.Errorf("requestedTrust %q is not a known trust level", raw)
	}
}

func parseAdapterTTL(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return adapterSessionDefaultTTL, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("requestedTTL %q is not a valid duration: %w", raw, err)
	}
	if d <= 0 {
		return 0, errors.New("requestedTTL must be greater than zero")
	}
	if d > adapterSessionMaxTTL {
		return adapterSessionMaxTTL, nil
	}
	return d, nil
}

// capTrust returns the requested trust capped at the grant's max trust.
// Empty grant maxTrust is treated as "no ceiling" — the grant author has not
// asserted a cap and we accept whatever the caller asked for (or the default).
func capTrust(requested, max sentinelaccess.TrustLevel) sentinelaccess.TrustLevel {
	if max == "" {
		return requested
	}
	if trustRank(requested) > trustRank(max) {
		return max
	}
	return requested
}

func trustRank(t sentinelaccess.TrustLevel) int {
	switch t {
	case sentinelaccess.TrustNone:
		return 0
	case sentinelaccess.TrustLow:
		return 1
	case sentinelaccess.TrustMid:
		return 2
	case sentinelaccess.TrustHigh:
		return 3
	case sentinelaccess.TrustFull:
		return 4
	default:
		return -1
	}
}
