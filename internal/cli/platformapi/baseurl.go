package platformapi

import "strings"

// NormalizeBaseURL trims whitespace, trailing slashes, and an optional trailing
// /api suffix from a platform base URL.
func NormalizeBaseURL(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimRight(s, "/")
	if strings.HasSuffix(strings.ToLower(s), "/api") {
		s = strings.TrimSpace(s[:len(s)-len("/api")])
		s = strings.TrimRight(s, "/")
	}
	return s
}
