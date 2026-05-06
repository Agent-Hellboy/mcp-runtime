package bootstrap

import (
	"errors"
	"strings"
	"testing"

	"mcp-runtime/internal/cli/core"
)

func TestDetectProviderReportsKubectlOutputOnFailure(t *testing.T) {
	mock := &core.MockExecutor{
		DefaultOutput: []byte(`The connection to the server 127.0.0.1:59146 was refused - did you specify the right host or port?`),
		DefaultErr:    errors.New("exit status 1"),
	}
	kubectl := core.NewTestKubectlClient(mock)

	_, err := detectProvider(kubectl)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	for _, want := range []string{
		"kubectl get nodes failed",
		"127.0.0.1:59146",
		"refused the connection",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q does not contain %q", msg, want)
		}
	}
}

func TestFormatKubectlFailureFallsBackToErrorString(t *testing.T) {
	msg := formatKubectlFailure("kubectl cannot reach cluster", nil, errors.New("exit status 1"))
	if !strings.Contains(msg, "kubectl cannot reach cluster") {
		t.Fatalf("message = %q", msg)
	}
	if !strings.Contains(msg, "exit status 1") {
		t.Fatalf("message = %q", msg)
	}
}
