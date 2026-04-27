package main

import (
	"context"
	"testing"
)

func TestListUserAPIKeysReturnsUnavailableWhenKubernetesUnavailable(t *testing.T) {
	server := &RuntimeServer{}

	_, err := server.ListUserAPIKeys(context.Background(), "user-123")
	if err == nil {
		t.Fatal("ListUserAPIKeys() error = nil, want kubernetes not available")
	}
	if err.Error() != "kubernetes not available" {
		t.Fatalf("ListUserAPIKeys() error = %v, want %q", err, "kubernetes not available")
	}
}
