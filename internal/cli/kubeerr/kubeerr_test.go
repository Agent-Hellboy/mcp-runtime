package kubeerr

import (
	"errors"
	"strings"
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

func TestDirectModeHint(t *testing.T) {
	for _, detail := range []string{
		`Error from server (Forbidden): mcpservers.mcpruntime.org is forbidden: User "alice" cannot list resource "mcpservers"`,
		"no configuration has been provided",
		"exit status 1",
	} {
		got := WithDirectModeHint(detail)
		if !strings.Contains(got, "Direct Kubernetes mode requires admin/operator cluster access") {
			t.Fatalf("hint for %q missing admin/operator guidance: %q", detail, got)
		}
		if !strings.Contains(got, "normal CLI operations") {
			t.Fatalf("hint for %q missing normal platform path guidance: %q", detail, got)
		}
		if !strings.Contains(got, "mcp-runtime auth login --api-url <platform-url>") {
			t.Fatalf("hint for %q missing platform login guidance: %q", detail, got)
		}
	}
}

func TestWithDirectModeHintTrimsTrailingPeriod(t *testing.T) {
	got := WithDirectModeHint("exit status 1.")
	if strings.Contains(got, "1..") {
		t.Fatalf("hint should not duplicate trailing periods: %q", got)
	}
	if !strings.HasPrefix(got, "exit status 1. Direct Kubernetes mode") {
		t.Fatalf("hint prefix mismatch: %q", got)
	}
}

func TestDirectModeFailureMessage(t *testing.T) {
	got := DirectModeFailureMessage("failed to list servers", "exit status 1.")
	if strings.Contains(got, "1..") {
		t.Fatalf("message should not duplicate trailing periods: %q", got)
	}
	if !strings.HasPrefix(got, "failed to list servers: exit status 1. Direct Kubernetes mode") {
		t.Fatalf("message prefix mismatch: %q", got)
	}
}
