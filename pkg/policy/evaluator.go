package policy

import (
	"net/http"
	"strings"
	"time"
)

// Identity is the subject context used to evaluate a rendered policy document.
type Identity struct {
	HumanID   string
	AgentID   string
	SessionID string
}

// Request describes the MCP RPC request being evaluated.
type Request struct {
	Identity  Identity
	RPCMethod string
	ToolName  string
}

// Decision is the result of evaluating a rendered policy document.
type Decision struct {
	Allowed            bool
	Status             int
	Reason             string
	PolicyVersion      string
	RequiredTrust      string
	RequiredSideEffect string
	AdminTrust         string
	ConsentedTrust     string
	EffectiveTrust     string
}

// Deny builds a denied authorization decision.
func Deny(status int, reason, policyVersion string) Decision {
	return Decision{
		Status:        status,
		Reason:        reason,
		PolicyVersion: policyVersion,
	}
}

// Authorize evaluates a rendered gateway policy document for a single MCP RPC request.
func Authorize(policy *Document, request Request, now time.Time) Decision {
	decision := Decision{
		Allowed:       true,
		Status:        http.StatusOK,
		Reason:        "allowed",
		PolicyVersion: policyVersionOrDefault(policy, ""),
	}
	if !IsToolCallMethod(request.RPCMethod) {
		return decision
	}
	if policyModeObserve(policy) {
		return decision
	}

	identity := request.Identity
	if identity.HumanID == "" && identity.AgentID == "" {
		return Deny(http.StatusUnauthorized, "missing_identity", policyVersionOrDefault(policy, ""))
	}
	if sessionRequired(policy) && identity.SessionID == "" {
		return Deny(http.StatusUnauthorized, "missing_session", policyVersionOrDefault(policy, ""))
	}
	if now.IsZero() {
		now = time.Now()
	}

	sessions, tools, grants := policySlices(policy)
	session, sessionFound := findSession(sessions, identity)
	if sessionRequired(policy) {
		if !sessionFound {
			return Deny(http.StatusUnauthorized, "session_not_found", policyVersionOrDefault(policy, ""))
		}
		if session.Revoked {
			return Deny(http.StatusUnauthorized, "session_revoked", ChoosePolicyVersion(session.PolicyVersion, policyVersionOrDefault(policy, "")))
		}
		if isExpiredAt(session.ExpiresAt, now) {
			return Deny(http.StatusUnauthorized, "session_expired", ChoosePolicyVersion(session.PolicyVersion, policyVersionOrDefault(policy, "")))
		}
	} else if identity.SessionID == "" || !sessionFound || session.Revoked || isExpiredAt(session.ExpiresAt, now) {
		session = Binding{}
		sessionFound = false
	}

	requiredTrust := resolveToolTrust(tools, request.ToolName)
	requiredSideEffect := resolveToolSideEffect(tools, request.ToolName)
	requiredRank := TrustRank(requiredTrust)
	matchingGrants := matchingGrants(grants, identity)
	if len(matchingGrants) == 0 {
		return decideByDefault(policy, "no_matching_grant")
	}

	bestAdminRank := 0
	toolAllowed := false
	sideEffectAllowedByGrant := false
	policyVersion := policyVersionOrDefault(policy, "")
	for _, grant := range matchingGrants {
		if grant.Disabled {
			continue
		}
		if grant.PolicyVersion != "" {
			policyVersion = grant.PolicyVersion
		}
		adminRank := TrustRank(grant.MaxTrust)
		if len(grant.ToolRules) == 0 {
			toolAllowed = true
			if sideEffectAllowed(grant.AllowedSideEffects, requiredSideEffect) {
				sideEffectAllowedByGrant = true
				if adminRank > bestAdminRank {
					bestAdminRank = adminRank
				}
			}
			continue
		}
		for _, rule := range grant.ToolRules {
			if rule.Name != request.ToolName {
				continue
			}
			if strings.EqualFold(rule.Decision, "deny") {
				return Deny(http.StatusForbidden, "tool_denied", ChoosePolicyVersion(grant.PolicyVersion, policyVersionOrDefault(policy, "")))
			}
			toolAllowed = true
			if sideEffectAllowed(grant.AllowedSideEffects, requiredSideEffect) {
				sideEffectAllowedByGrant = true
				ruleRank := TrustRank(rule.RequiredTrust)
				if ruleRank > requiredRank {
					requiredRank = ruleRank
					requiredTrust = NormalizeTrust(rule.RequiredTrust)
				}
				if adminRank > bestAdminRank {
					bestAdminRank = adminRank
				}
			}
		}
	}

	if !toolAllowed {
		return decideByDefault(policy, "tool_not_granted")
	}
	if requiredSideEffect == "" {
		return Decision{
			Status:        http.StatusForbidden,
			Reason:        "tool_side_effect_unknown",
			PolicyVersion: policyVersion,
			RequiredTrust: requiredTrust,
		}
	}
	if !sideEffectAllowedByGrant {
		return Decision{
			Status:             http.StatusForbidden,
			Reason:             "side_effect_not_allowed",
			PolicyVersion:      policyVersion,
			RequiredTrust:      requiredTrust,
			RequiredSideEffect: requiredSideEffect,
		}
	}
	if bestAdminRank == 0 {
		return decideByDefault(policy, "grant_without_trust")
	}

	consentedRank := bestAdminRank
	consentedTrust := RankToTrust(bestAdminRank)
	if sessionFound && session.ConsentedTrust != "" {
		consentedRank = TrustRank(session.ConsentedTrust)
		consentedTrust = RankToTrust(consentedRank)
	}
	effectiveRank := minInt(bestAdminRank, consentedRank)
	if effectiveRank < requiredRank {
		return Decision{
			Status:             http.StatusForbidden,
			Reason:             "trust_too_low",
			PolicyVersion:      policyVersion,
			RequiredTrust:      requiredTrust,
			RequiredSideEffect: requiredSideEffect,
			AdminTrust:         RankToTrust(bestAdminRank),
			ConsentedTrust:     consentedTrust,
			EffectiveTrust:     RankToTrust(effectiveRank),
		}
	}

	return Decision{
		Allowed:            true,
		Status:             http.StatusOK,
		Reason:             "allowed",
		PolicyVersion:      policyVersion,
		RequiredTrust:      requiredTrust,
		RequiredSideEffect: requiredSideEffect,
		AdminTrust:         RankToTrust(bestAdminRank),
		ConsentedTrust:     consentedTrust,
		EffectiveTrust:     RankToTrust(effectiveRank),
	}
}

func policySlices(policy *Document) ([]Binding, []Tool, []Grant) {
	if policy == nil {
		return nil, nil, nil
	}
	return policy.Sessions, policy.Tools, policy.Grants
}

func decideByDefault(policy *Document, reason string) Decision {
	policyVersion := policyVersionOrDefault(policy, "")
	if defaultDecisionAllow(policy) {
		return Decision{
			Allowed:       true,
			Status:        http.StatusOK,
			Reason:        reason,
			PolicyVersion: policyVersion,
		}
	}
	return Deny(http.StatusForbidden, reason, policyVersion)
}

func matchingGrants(grants []Grant, identity Identity) []Grant {
	var matched []Grant
	for _, grant := range grants {
		if subjectMatches(grant.HumanID, grant.AgentID, identity) {
			matched = append(matched, grant)
		}
	}
	return matched
}

func findSession(sessions []Binding, identity Identity) (Binding, bool) {
	if identity.SessionID != "" {
		for _, session := range sessions {
			if session.Name == identity.SessionID && subjectMatches(session.HumanID, session.AgentID, identity) {
				return session, true
			}
		}
		return Binding{}, false
	}
	for _, session := range sessions {
		if subjectMatches(session.HumanID, session.AgentID, identity) {
			return session, true
		}
	}
	return Binding{}, false
}

func subjectMatches(humanID, agentID string, identity Identity) bool {
	if humanID != "" && humanID != identity.HumanID {
		return false
	}
	if agentID != "" && agentID != identity.AgentID {
		return false
	}
	return humanID != "" || agentID != ""
}

func resolveToolTrust(tools []Tool, toolName string) string {
	for _, tool := range tools {
		if tool.Name == toolName && tool.RequiredTrust != "" {
			return NormalizeTrust(tool.RequiredTrust)
		}
	}
	return TrustLevelLow
}

func resolveToolSideEffect(tools []Tool, toolName string) string {
	for _, tool := range tools {
		if tool.Name == toolName {
			return NormalizeSideEffect(tool.SideEffect)
		}
	}
	return ""
}

func policyVersionOrDefault(policy *Document, def string) string {
	if policy != nil && policy.Policy != nil && policy.Policy.PolicyVersion != "" {
		return policy.Policy.PolicyVersion
	}
	return def
}

func sessionRequired(policy *Document) bool {
	return policy != nil && policy.Session != nil && policy.Session.Required
}

func policyModeObserve(policy *Document) bool {
	return policy != nil && policy.Policy != nil && strings.EqualFold(policy.Policy.Mode, "observe")
}

func defaultDecisionAllow(policy *Document) bool {
	return policy != nil && policy.Policy != nil && strings.EqualFold(policy.Policy.DefaultDecision, "allow")
}

func isExpiredAt(value string, now time.Time) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	expiresAt, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return true
	}
	return now.After(expiresAt)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
