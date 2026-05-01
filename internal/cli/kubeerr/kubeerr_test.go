package kubeerr

import (
	"errors"
	"testing"
)

func TestCommandDetail(t *testing.T) {
	if got := CommandDetail("first\n\nlast\n", errors.New("fallback")); got != "last" {
		t.Fatalf("expected last output line, got %q", got)
	}
	if got := CommandDetail("", errors.New("fallback")); got != "fallback" {
		t.Fatalf("expected fallback error, got %q", got)
	}
	if got := CommandDetail("", nil); got != "Unknown error" {
		t.Fatalf("expected unknown error, got %q", got)
	}
}

func TestSetupHint(t *testing.T) {
	if _, ok := SetupHint("kubectl: not found"); !ok {
		t.Fatal("expected kubectl missing hint")
	}
	if _, ok := SetupHint("no configuration has been provided"); !ok {
		t.Fatal("expected kubeconfig hint")
	}
	if _, ok := SetupHint("connection refused"); !ok {
		t.Fatal("expected connectivity hint")
	}
	if _, ok := SetupHint("some other error"); ok {
		t.Fatal("unexpected hint")
	}
}
