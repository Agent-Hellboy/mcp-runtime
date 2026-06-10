package policy

import (
	"fmt"
	"strings"
)

// Validate checks that a rendered gateway policy document is structurally sound
// and safe to activate. It is the shared contract used by both the producer
// (operator, before writing the policy ConfigMap) and the consumer (gateway,
// after decoding and before activating a snapshot).
//
// Validation is strict and fails closed: unknown trust, side-effect, decision,
// auth-mode, or policy-mode values are rejected rather than silently normalized,
// and incomplete combinations (such as OAuth without an issuer) are errors. A
// document that does not validate must never replace a known-good policy.
func Validate(doc *Document) error {
	if doc == nil {
		return fmt.Errorf("policy: document is nil")
	}
	if _, ok := supportedSchemaVersions[doc.SchemaVersion]; !ok {
		return fmt.Errorf("policy: unsupported schema version %q", doc.SchemaVersion)
	}
	if strings.TrimSpace(string(doc.Server.Name)) == "" {
		return fmt.Errorf("policy: server.name is required")
	}

	if err := validateAuth(doc.Auth); err != nil {
		return err
	}
	if err := validateConfig(doc.Policy); err != nil {
		return err
	}
	if err := validateTools(doc.Tools); err != nil {
		return err
	}
	if err := validateGrants(doc.Grants); err != nil {
		return err
	}
	return validateBindings(doc.Sessions)
}

func validateAuth(auth *Auth) error {
	if auth == nil {
		return nil
	}
	mode := strings.ToLower(strings.TrimSpace(auth.Mode))
	switch mode {
	case "", "none", "header", "oauth":
	default:
		return fmt.Errorf("policy: invalid auth mode %q", auth.Mode)
	}
	if mode == "oauth" && strings.TrimSpace(auth.IssuerURL) == "" {
		return fmt.Errorf("policy: auth mode %q requires issuer_url", auth.Mode)
	}
	return nil
}

func validateConfig(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Mode)) {
	case "", "allow-list", "observe":
	default:
		return fmt.Errorf("policy: invalid policy mode %q", cfg.Mode)
	}
	switch strings.ToLower(strings.TrimSpace(cfg.DefaultDecision)) {
	case "", "allow", "deny":
	default:
		return fmt.Errorf("policy: invalid default decision %q", cfg.DefaultDecision)
	}
	return nil
}

func validateTools(tools []Tool) error {
	seen := make(map[ToolName]struct{}, len(tools))
	for i, tool := range tools {
		if strings.TrimSpace(string(tool.Name)) == "" {
			return fmt.Errorf("policy: tools[%d] name is required", i)
		}
		if _, dup := seen[tool.Name]; dup {
			return fmt.Errorf("policy: duplicate tool name %q", tool.Name)
		}
		seen[tool.Name] = struct{}{}
		if !validTrust(tool.RequiredTrust) {
			return fmt.Errorf("policy: tool %q has invalid required_trust %q", tool.Name, tool.RequiredTrust)
		}
		if !validSideEffect(tool.SideEffect, true) {
			return fmt.Errorf("policy: tool %q has invalid side_effect %q", tool.Name, tool.SideEffect)
		}
	}
	return nil
}

func validateGrants(grants []Grant) error {
	seen := make(map[string]struct{}, len(grants))
	for i, grant := range grants {
		if strings.TrimSpace(grant.Name) == "" {
			return fmt.Errorf("policy: grants[%d] name is required", i)
		}
		if _, dup := seen[grant.Name]; dup {
			return fmt.Errorf("policy: duplicate grant name %q", grant.Name)
		}
		seen[grant.Name] = struct{}{}
		if !validTrust(grant.MaxTrust) {
			return fmt.Errorf("policy: grant %q has invalid max_trust %q", grant.Name, grant.MaxTrust)
		}
		for _, sideEffect := range grant.AllowedSideEffects {
			if !validSideEffect(sideEffect, false) {
				return fmt.Errorf("policy: grant %q has invalid allowed side effect %q", grant.Name, sideEffect)
			}
		}
		for j, rule := range grant.ToolRules {
			if strings.TrimSpace(string(rule.Name)) == "" {
				return fmt.Errorf("policy: grant %q tool_rules[%d] name is required", grant.Name, j)
			}
			if !validDecision(rule.Decision) {
				return fmt.Errorf("policy: grant %q tool rule %q has invalid decision %q", grant.Name, rule.Name, rule.Decision)
			}
			if !validTrust(rule.RequiredTrust) {
				return fmt.Errorf("policy: grant %q tool rule %q has invalid required_trust %q", grant.Name, rule.Name, rule.RequiredTrust)
			}
		}
	}
	return nil
}

func validateBindings(bindings []Binding) error {
	seen := make(map[SessionID]struct{}, len(bindings))
	for i, binding := range bindings {
		if strings.TrimSpace(string(binding.Name)) == "" {
			return fmt.Errorf("policy: sessions[%d] name is required", i)
		}
		if _, dup := seen[binding.Name]; dup {
			return fmt.Errorf("policy: duplicate session name %q", binding.Name)
		}
		seen[binding.Name] = struct{}{}
		if !validTrust(binding.ConsentedTrust) {
			return fmt.Errorf("policy: session %q has invalid consented_trust %q", binding.Name, binding.ConsentedTrust)
		}
	}
	return nil
}

// validTrust reports whether a trust value is empty (defaulted downstream) or
// one of the recognized trust levels. Unknown values are rejected.
func validTrust(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", TrustLevelLow, TrustLevelMedium, TrustLevelHigh:
		return true
	default:
		return false
	}
}

// validSideEffect reports whether a side-effect value is acceptable. When
// allowEmpty is true an empty value is permitted (a tool may declare no side
// effect, which fails closed at evaluation time); when false (a grant's allowed
// side effect) an empty value is rejected as meaningless.
func validSideEffect(value string, allowEmpty bool) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return allowEmpty
	case SideEffectRead, SideEffectWrite, SideEffectDestructive:
		return true
	default:
		return false
	}
}

// validDecision reports whether a tool-rule decision is empty (treated as allow
// downstream) or one of the recognized decisions.
func validDecision(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "allow", "deny":
		return true
	default:
		return false
	}
}
