package platformstore

import (
	"context"
	"strings"
	"testing"
)

func TestEnsureTeamPasswordUserTrimsBeforeLengthCheck(t *testing.T) {
	store := NewForTest(nil)
	_, err := store.EnsureTeamPasswordUser(context.Background(), "member@example.com", " 12345678901 ")
	if err == nil {
		t.Fatal("expected short password error")
	}
	if !strings.Contains(err.Error(), "at least 12 characters") {
		t.Fatalf("expected password length error, got %v", err)
	}
}
