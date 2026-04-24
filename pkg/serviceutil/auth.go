// Package serviceutil provides authentication utilities for MCP services.
package serviceutil

import (
	"strings"
)

// ExtractBearer extracts the JWT token from an Authorization header.
// It expects the format "Bearer <token>" and returns the token part.
// Returns empty string if the format is invalid.
func ExtractBearer(auth string) string {
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	return ""
}

// ExtractToken extracts a token from a header value, handling Bearer prefix.
// If the headerName is "authorization", it only extracts Bearer tokens.
// Otherwise, it returns the raw value or the Bearer-extracted token if present.
func ExtractToken(headerName, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(headerName), "authorization") {
		return ExtractBearer(value)
	}
	if token := ExtractBearer(value); token != "" {
		return token
	}
	return value
}

// FormatTokenHeaderValue formats a token for a specific header.
// If the header is "authorization", it returns "Bearer <token>".
// Otherwise, it returns the token as-is.
func FormatTokenHeaderValue(headerName, token string) string {
	if strings.EqualFold(strings.TrimSpace(headerName), "authorization") {
		return "Bearer " + token
	}
	return token
}

// AudienceMatches validates if the JWT audience claim matches the expected value.
// It handles both string and string slice audience claims as per JWT specifications.
func AudienceMatches(audClaim any, expected string) bool {
	switch aud := audClaim.(type) {
	case string:
		return aud == expected
	case []any:
		for _, item := range aud {
			if s, ok := item.(string); ok && s == expected {
				return true
			}
		}
	case []string:
		for _, value := range aud {
			if value == expected {
				return true
			}
		}
	}
	return false
}
