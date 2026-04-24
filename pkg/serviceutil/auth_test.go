// Package serviceutil provides tests for authentication utilities.
package serviceutil

import (
	"testing"
)

func TestExtractBearer(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Bearer token123", "token123"},
		{"bearer token123", "token123"},
		{"BEARER token123", "token123"},
		{"Bearer   token123  ", "token123"},
		{"token123", ""},
		{"", ""},
		{"Basic token123", ""},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := ExtractBearer(tc.input)
			if result != tc.expected {
				t.Errorf("ExtractBearer(%q) = %q, expected %q", tc.input, result, tc.expected)
			}
		})
	}
}

func TestExtractToken(t *testing.T) {
	tests := []struct {
		headerName string
		value      string
		expected   string
	}{
		// Authorization header - only Bearer
		{"authorization", "Bearer token123", "token123"},
		{"Authorization", "bearer token123", "token123"},
		{"authorization", "token123", ""}, // No Bearer prefix
		// Non-authorization header - accepts raw or Bearer
		{"x-api-key", "token123", "token123"},
		{"x-api-key", "Bearer token123", "token123"},
		// Empty values
		{"authorization", "", ""},
		{"x-api-key", "", ""},
		{"x-api-key", "   ", ""},
	}

	for _, tc := range tests {
		t.Run(tc.headerName+"_"+tc.value, func(t *testing.T) {
			result := ExtractToken(tc.headerName, tc.value)
			if result != tc.expected {
				t.Errorf("ExtractToken(%q, %q) = %q, expected %q", tc.headerName, tc.value, result, tc.expected)
			}
		})
	}
}

func TestFormatTokenHeaderValue(t *testing.T) {
	tests := []struct {
		headerName string
		token      string
		expected   string
	}{
		{"authorization", "token123", "Bearer token123"},
		{"Authorization", "token123", "Bearer token123"},
		{"x-api-key", "token123", "token123"},
		{"X-Custom-Auth", "token123", "token123"},
	}

	for _, tc := range tests {
		t.Run(tc.headerName, func(t *testing.T) {
			result := FormatTokenHeaderValue(tc.headerName, tc.token)
			if result != tc.expected {
				t.Errorf("FormatTokenHeaderValue(%q, %q) = %q, expected %q", tc.headerName, tc.token, result, tc.expected)
			}
		})
	}
}

func TestAudienceMatches(t *testing.T) {
	tests := []struct {
		name     string
		claim    any
		expected string
		matches  bool
	}{
		{"string match", "audience1", "audience1", true},
		{"string no match", "audience1", "audience2", false},
		{"[]any match", []any{"aud1", "audience1", "aud2"}, "audience1", true},
		{"[]any no match", []any{"aud1", "aud2"}, "audience1", false},
		{"[]string match", []string{"aud1", "audience1", "aud2"}, "audience1", true},
		{"[]string no match", []string{"aud1", "aud2"}, "audience1", false},
		{"nil claim", nil, "audience1", false},
		{"int claim", 123, "audience1", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := AudienceMatches(tc.claim, tc.expected)
			if result != tc.matches {
				t.Errorf("AudienceMatches(%v, %q) = %v, expected %v", tc.claim, tc.expected, result, tc.matches)
			}
		})
	}
}
