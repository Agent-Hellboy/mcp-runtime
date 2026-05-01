package platformapi

import "testing"

func TestNormalizeBaseURL(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"https://example.com/", "https://example.com"},
		{"https://example.com/api", "https://example.com"},
		{"https://example.com/api/", "https://example.com"},
		{"  https://x  ", "https://x"},
	}
	for _, tc := range cases {
		if got := NormalizeBaseURL(tc.in); got != tc.want {
			t.Fatalf("NormalizeBaseURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
