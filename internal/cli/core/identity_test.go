package core

import (
	"strings"
	"testing"
)

func TestResolveEmailAliasAcceptsUsernameAlias(t *testing.T) {
	got, err := ResolveEmailAlias("", "user@example.com")
	if err != nil {
		t.Fatalf("ResolveEmailAlias() error = %v", err)
	}
	if got != "user@example.com" {
		t.Fatalf("email = %q, want username alias", got)
	}
}

func TestResolveEmailAliasRejectsMismatchedAlias(t *testing.T) {
	_, err := ResolveEmailAlias("user@example.com", "other@example.com")
	if err == nil {
		t.Fatal("expected mismatch error")
	}
	if !strings.Contains(err.Error(), "must match") {
		t.Fatalf("expected mismatch error, got %v", err)
	}
}
