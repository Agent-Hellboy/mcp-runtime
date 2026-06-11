package registry

import "testing"

func TestRegistryHostFromAPIBaseURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		apiURL string
		want   string
	}{
		{
			name:   "maps_platform_host_to_registry_host",
			apiURL: "https://platform.mcpruntime.org",
			want:   "registry.mcpruntime.org",
		},
		{
			name:   "preserves_port_when_present",
			apiURL: "https://platform.example.com:8443",
			want:   "registry.example.com:8443",
		},
		{
			name:   "ignores_localhost_api_url",
			apiURL: "http://127.0.0.1:18080",
			want:   "",
		},
		{
			name:   "returns_non_platform_host_as_is",
			apiURL: "https://api.example.com",
			want:   "api.example.com",
		},
		{
			name:   "rejects_invalid_url",
			apiURL: "://bad url",
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := registryHostFromAPIBaseURL(tt.apiURL); got != tt.want {
				t.Fatalf("registryHostFromAPIBaseURL(%q) = %q, want %q", tt.apiURL, got, tt.want)
			}
		})
	}
}
