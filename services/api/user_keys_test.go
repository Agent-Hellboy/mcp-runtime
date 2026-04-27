package main

import (
	"context"
	"strings"
	"testing"
)

func TestListUserAPIKeysReturnsUnavailableWhenKubernetesUnavailable(t *testing.T) {
	server := &RuntimeServer{}

	_, err := server.ListUserAPIKeys(context.Background(), "user-123")
	if err == nil {
		t.Fatal("ListUserAPIKeys() error = nil, want kubernetes not available")
	}
	if !strings.Contains(err.Error(), "kubernetes not available") {
		t.Fatalf("ListUserAPIKeys() error = %v, want substring %q", err, "kubernetes not available")
	}
}
