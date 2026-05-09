package policy

import "strings"

const (
	SideEffectRead        = "read"
	SideEffectWrite       = "write"
	SideEffectDestructive = "destructive"
)

// NormalizeSideEffect normalizes known side-effect values and returns empty for unknown values.
func NormalizeSideEffect(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case SideEffectRead:
		return SideEffectRead
	case SideEffectWrite:
		return SideEffectWrite
	case SideEffectDestructive:
		return SideEffectDestructive
	default:
		return ""
	}
}

func sideEffectAllowed(allowed []string, required string) bool {
	required = NormalizeSideEffect(required)
	if required == "" {
		return false
	}
	for _, candidate := range allowed {
		if NormalizeSideEffect(candidate) == required {
			return true
		}
	}
	return false
}
