package main

import "testing"

func TestValidateTeamSlug(t *testing.T) {
	cases := []struct {
		name    string
		slug    string
		wantErr bool
	}{
		{name: "valid", slug: "platform-core", wantErr: false},
		{name: "empty", slug: "", wantErr: true},
		{name: "invalid characters", slug: "core/team", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateTeamSlug(tc.slug)
			if tc.wantErr && err == nil {
				t.Fatalf("validateTeamSlug(%q) error = nil, want error", tc.slug)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("validateTeamSlug(%q) error = %v, want nil", tc.slug, err)
			}
		})
	}
}

func TestValidateTeamNamespaceReserved(t *testing.T) {
	cases := []struct {
		name      string
		namespace string
		wantErr   bool
	}{
		{name: "team namespace", namespace: "mcp-team-core", wantErr: false},
		{name: "shared reserved", namespace: sharedCatalogNamespace, wantErr: true},
		{name: "kube reserved", namespace: "kube-system", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateTeamNamespace(tc.namespace)
			if tc.wantErr && err == nil {
				t.Fatalf("validateTeamNamespace(%q) error = nil, want error", tc.namespace)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("validateTeamNamespace(%q) error = %v, want nil", tc.namespace, err)
			}
		})
	}
}

func TestValidateDeployImageScope(t *testing.T) {
	if err := validateDeployImage("registry.example.com/core/demo:latest", "mcp-team-core", "core", roleUser); err != nil {
		t.Fatalf("validateDeployImage() error = %v, want nil", err)
	}
	if err := validateDeployImage("registry.example.com/other/demo:latest", "mcp-team-core", "core", roleUser); err == nil {
		t.Fatal("expected image scope validation error for non-admin")
	}
}
