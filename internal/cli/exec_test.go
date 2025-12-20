package cli

import (
	"testing"
)

func TestExecCommand(t *testing.T) {
	cmd := execCommand("echo", "hello")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("failed to execute command: %v", err)
	}
	// echo adds a newline
	if string(out) != "hello\n" {
		t.Fatalf("expected output 'hello\\n', got '%s'", string(out))
	}
}
