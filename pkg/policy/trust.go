// Package policy provides trust level utilities for policy enforcement.
package policy

import "strings"

// Trust level constants matching the API definitions.
const (
	TrustLevelLow    = "low"
	TrustLevelMedium = "medium"
	TrustLevelHigh   = "high"
)

// NormalizeTrust normalizes a trust level string to one of the standard values.
// Defaults to "low" if the value is unrecognized.
func NormalizeTrust(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case TrustLevelHigh:
		return TrustLevelHigh
	case TrustLevelMedium:
		return TrustLevelMedium
	default:
		return TrustLevelLow
	}
}

// TrustRank returns a numeric rank for a trust level (1=low, 2=medium, 3=high).
// Higher ranks indicate higher trust.
func TrustRank(value string) int {
	switch NormalizeTrust(value) {
	case TrustLevelHigh:
		return 3
	case TrustLevelMedium:
		return 2
	default:
		return 1
	}
}

// RankToTrust converts a numeric rank back to a trust level string.
func RankToTrust(value int) string {
	switch {
	case value >= 3:
		return TrustLevelHigh
	case value == 2:
		return TrustLevelMedium
	default:
		return TrustLevelLow
	}
}
