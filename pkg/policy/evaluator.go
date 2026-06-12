package policy

import (
	"net/http"
	"strings"
	"time"
)

// Identity is the subject context used to evaluate a rendered policy document.
type Identity struct {
	HumanID   HumanID
	AgentID   AgentID
	TeamID    TeamID
	SessionID SessionID
}

// Request describes the MCP RPC request being evaluated.
type Request struct {
	Identity  Identity
	RPCMethod string
	ToolName  ToolName
}

// Decision is the result of evaluating a rendered policy document.
type Decision struct {
	Allowed            bool
	Status             int
	Reason             string
	PolicyVersion      string
	RequiredTrust      string
	RequiredSideEffect string
	RiskLevel          string
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
	_, _, decision.RiskLevel = resolveToolMetadata(policyTools(policy), request.ToolName)
	if policyModeObserve(policy) {
		return decision
	}

	identity := request.Identity
	if identity.HumanID == "" && identity.AgentID == "" && identity.TeamID == "" {
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

	requiredTrust, requiredSideEffect, riskLevel := resolveToolMetadata(tools, request.ToolName)
	matchingGrants := matchingGrants(grants, identity)
	if len(matchingGrants) == 0 {
		return decideByDefault(policy, "no_matching_grant")
	}

	grant := bestGrantFor(matchingGrants, request.ToolName, requiredTrust, requiredSideEffect, policyVersionOrDefault(policy, ""))
	if grant.deny != nil {
		return *grant.deny
	}
	if !grant.toolAllowed {
		return decideByDefault(policy, "tool_not_granted")
	}
	if requiredSideEffect == "" {
		return Decision{
			Status:        http.StatusForbidden,
			Reason:        "tool_side_effect_unknown",
			PolicyVersion: grant.policyVersion,
			RequiredTrust: grant.requiredTrust,
			RiskLevel:     riskLevel,
		}
	}
	if !grant.sideEffectAllowed {
		return Decision{
			Status:             http.StatusForbidden,
			Reason:             "side_effect_not_allowed",
			PolicyVersion:      grant.policyVersion,
			RequiredTrust:      grant.requiredTrust,
			RequiredSideEffect: requiredSideEffect,
			RiskLevel:          riskLevel,
		}
	}
	if grant.adminTrustRank == 0 {
		return decideByDefault(policy, "grant_without_trust")
	}

	consentedRank := grant.adminTrustRank
	consentedTrust := RankToTrust(grant.adminTrustRank)
	if sessionFound && session.ConsentedTrust != "" {
		consentedRank = TrustRank(session.ConsentedTrust)
		consentedTrust = RankToTrust(consentedRank)
	}
	effectiveRank := minInt(grant.adminTrustRank, consentedRank)
	if effectiveRank < grant.requiredTrustRank {
		return Decision{
			Status:             http.StatusForbidden,
			Reason:             "trust_too_low",
			PolicyVersion:      grant.policyVersion,
			RequiredTrust:      grant.requiredTrust,
			RequiredSideEffect: requiredSideEffect,
			RiskLevel:          riskLevel,
			AdminTrust:         RankToTrust(grant.adminTrustRank),
			ConsentedTrust:     consentedTrust,
			EffectiveTrust:     RankToTrust(effectiveRank),
		}
	}

	return Decision{
		Allowed:            true,
		Status:             http.StatusOK,
		Reason:             "allowed",
		PolicyVersion:      grant.policyVersion,
		RequiredTrust:      grant.requiredTrust,
		RequiredSideEffect: requiredSideEffect,
		RiskLevel:          riskLevel,
		AdminTrust:         RankToTrust(grant.adminTrustRank),
		ConsentedTrust:     consentedTrust,
		EffectiveTrust:     RankToTrust(effectiveRank),
	}
}

func policyTools(policy *Document) []Tool {
	if policy == nil {
		return nil
	}
	return policy.Tools
}

type grantSelection struct {
	toolAllowed       bool
	sideEffectAllowed bool
	adminTrustRank    int
	requiredTrustRank int
	requiredTrust     string
	policyVersion     string
	deny              *Decision
}

func bestGrantFor(grants []Grant, toolName ToolName, requiredTrust, requiredSideEffect, policyVersion string) grantSelection {
	selection := grantSelection{
		requiredTrustRank: TrustRank(requiredTrust),
		requiredTrust:     requiredTrust,
		policyVersion:     policyVersion,
	}
	for _, grant := range grants {
		if grant.Disabled {
			continue
		}
		if grant.PolicyVersion != "" {
			selection.policyVersion = grant.PolicyVersion
		}
		adminRank := TrustRank(grant.MaxTrust)
		if len(grant.ToolRules) == 0 {
			selection.toolAllowed = true
			if sideEffectAllowed(grant.AllowedSideEffects, requiredSideEffect) {
				selection.sideEffectAllowed = true
				selection.adminTrustRank = maxInt(selection.adminTrustRank, adminRank)
			}
			continue
		}
		for _, rule := range grant.ToolRules {
			if rule.Name != toolName {
				continue
			}
			if strings.EqualFold(rule.Decision, "deny") {
				deny := Deny(http.StatusForbidden, "tool_denied", ChoosePolicyVersion(grant.PolicyVersion, policyVersion))
				selection.deny = &deny
				return selection
			}
			selection.toolAllowed = true
			if sideEffectAllowed(grant.AllowedSideEffects, requiredSideEffect) {
				selection.sideEffectAllowed = true
				ruleRank := TrustRank(rule.RequiredTrust)
				if ruleRank > selection.requiredTrustRank {
					selection.requiredTrustRank = ruleRank
					selection.requiredTrust = NormalizeTrust(rule.RequiredTrust)
				}
				selection.adminTrustRank = maxInt(selection.adminTrustRank, adminRank)
			}
		}
	}
	return selection
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
		if subjectMatchesTeam(grant.HumanID, grant.AgentID, grant.TeamID, identity) {
			matched = append(matched, grant)
		}
	}
	return matched
}

func findSession(sessions []Binding, identity Identity) (Binding, bool) {
	if identity.SessionID != "" {
		for _, session := range sessions {
			if session.Name == identity.SessionID && subjectMatchesTeam(session.HumanID, session.AgentID, session.TeamID, identity) {
				return session, true
			}
		}
		return Binding{}, false
	}
	for _, session := range sessions {
		if subjectMatchesTeam(session.HumanID, session.AgentID, session.TeamID, identity) {
			return session, true
		}
	}
	return Binding{}, false
}

func subjectMatchesTeam(humanID HumanID, agentID AgentID, teamID TeamID, identity Identity) bool {
	if humanID != "" && humanID != identity.HumanID {
		return false
	}
	if agentID != "" && agentID != identity.AgentID {
		return false
	}
	if teamID != "" && teamID != identity.TeamID {
		return false
	}
	return humanID != "" || agentID != "" || teamID != ""
}

func resolveToolMetadata(tools []Tool, toolName ToolName) (string, string, string) {
	requiredTrust := TrustLevelLow
	for _, tool := range tools {
		if tool.Name == toolName {
			if tool.RequiredTrust != "" {
				requiredTrust = NormalizeTrust(tool.RequiredTrust)
			}
			return requiredTrust, NormalizeSideEffect(tool.SideEffect), NormalizeRiskLevel(tool.RiskLevel, requiredTrust, tool.SideEffect)
		}
	}
	return requiredTrust, "", ""
}

func NormalizeRiskLevel(risk, trust, sideEffect string) string {
	switch strings.ToLower(strings.TrimSpace(risk)) {
	case "low", "medium", "high":
		return strings.ToLower(strings.TrimSpace(risk))
	}
	trust = NormalizeTrust(trust)
	sideEffect = NormalizeSideEffect(sideEffect)
	switch {
	case sideEffect == "destructive" || trust == TrustLevelHigh:
		return "high"
	case sideEffect == "write" || trust == TrustLevelMedium:
		return "medium"
	case sideEffect == "read" && trust == TrustLevelLow:
		return "low"
	default:
		return ""
	}
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

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
