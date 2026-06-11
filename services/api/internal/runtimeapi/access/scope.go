package access

import (
	"strings"

	sentinelaccess "mcp-runtime/pkg/access"
)

const DefaultPolicyVersionValue = "v1"

func DefaultAccessNamespace(namespace string) string {
	if namespace = strings.TrimSpace(namespace); namespace != "" {
		return namespace
	}
	return sentinelaccess.DefaultMCPResourceNamespace
}

func DefaultPolicyVersion(policyVersion string) string {
	if policyVersion = strings.TrimSpace(policyVersion); policyVersion != "" {
		return policyVersion
	}
	return DefaultPolicyVersionValue
}

func NormalizeTrust(trust sentinelaccess.TrustLevel) sentinelaccess.TrustLevel {
	return sentinelaccess.TrustLevel(strings.TrimSpace(string(trust)))
}

func NormalizeSideEffect(sideEffect sentinelaccess.ToolSideEffect) sentinelaccess.ToolSideEffect {
	return sentinelaccess.ToolSideEffect(strings.TrimSpace(string(sideEffect)))
}

func ValidTrust(trust sentinelaccess.TrustLevel) bool {
	switch trust {
	case "low", "medium", "high":
		return true
	default:
		return false
	}
}

func ValidSideEffect(sideEffect sentinelaccess.ToolSideEffect) bool {
	switch sideEffect {
	case "read", "write", "destructive":
		return true
	default:
		return false
	}
}

func ValidDecision(decision sentinelaccess.PolicyDecision) bool {
	switch decision {
	case "allow", "deny":
		return true
	default:
		return false
	}
}
