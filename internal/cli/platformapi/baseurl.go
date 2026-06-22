package platformapi

import "strings"

// NormalizeBaseURL trims whitespace, trailing slashes, and an optional trailing
// /api suffix from a platform base URL.
func NormalizeBaseURL(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimRight(s, "/")
	lower := strings.ToLower(s)
	for _, suffix := range []string{"/api/v1", "/api"} {
		if strings.HasSuffix(lower, suffix) {
			s = strings.TrimSpace(s[:len(s)-len(suffix)])
			s = strings.TrimRight(s, "/")
			break
		}
	}
	return s
}
