package main

import (
	"testing"

	"mcp-platform-api/identity"
)

func TestRegistryCredentialUsernameUsesNamespace(t *testing.T) {
	got := identity.RegistryCredentialUsername(principal{
		Namespace: "mcp-team-acme",
		Subject:   "user-1",
		Email:     "user@example.com",
	})
	if got != "mcp-team-acme" {
		t.Fatalf("username = %q, want namespace", got)
	}
}

func TestRegistryCredentialUsernameFallsBackToSubject(t *testing.T) {
	got := identity.RegistryCredentialUsername(principal{
		Subject: "user-1",
		Email:   "user@example.com",
	})
	if got != "user-1" {
		t.Fatalf("username = %q, want subject", got)
	}
}

func TestRegistryCredentialUsernameFallsBackToEmail(t *testing.T) {
	got := identity.RegistryCredentialUsername(principal{
		Email: "user@example.com",
	})
	if got != "user@example.com" {
		t.Fatalf("username = %q, want email", got)
	}
}
