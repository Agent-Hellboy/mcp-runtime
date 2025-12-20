package cli

import (
	"os"
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

func TestAllowlistBins(t *testing.T) {
	validator := AllowlistBins("kubectl", "docker", "git")

	t.Run("allows listed binary", func(t *testing.T) {
		err := validator(ExecSpec{Name: "kubectl", Args: []string{"get", "pods"}})
		if err != nil {
			t.Errorf("expected kubectl to be allowed, got error: %v", err)
		}
	})

	t.Run("blocks unlisted binary", func(t *testing.T) {
		err := validator(ExecSpec{Name: "rm", Args: []string{"-rf", "/"}})
		if err == nil {
			t.Error("expected rm to be blocked")
		}
	})

	t.Run("blocks empty binary name", func(t *testing.T) {
		err := validator(ExecSpec{Name: "", Args: []string{}})
		if err == nil {
			t.Error("expected empty name to be blocked")
		}
	})
}

func TestNoShellMeta(t *testing.T) {
	validator := NoShellMeta()

	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{"safe args", []string{"get", "pods", "-n", "default"}, false},
		{"pipe", []string{"echo", "hello", "|", "cat"}, true},
		{"semicolon", []string{"ls", ";", "rm", "-rf"}, true},
		{"ampersand", []string{"sleep", "10", "&"}, true},
		{"redirect", []string{"cat", ">", "/etc/passwd"}, true},
		{"subshell", []string{"$(whoami)"}, true},
		{"backtick", []string{"`id`"}, true},
		{"backslash", []string{"echo", "test\\"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator(ExecSpec{Name: "test", Args: tt.args})
			if (err != nil) != tt.wantErr {
				t.Errorf("NoShellMeta() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNoControlChars(t *testing.T) {
	validator := NoControlChars()

	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{"safe args", []string{"get", "pods"}, false},
		{"newline", []string{"hello\nworld"}, true},
		{"carriage return", []string{"hello\rworld"}, true},
		{"tab", []string{"hello\tworld"}, true},
		{"space is ok", []string{"hello world"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator(ExecSpec{Name: "test", Args: tt.args})
			if (err != nil) != tt.wantErr {
				t.Errorf("NoControlChars() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPathUnder(t *testing.T) {
	validator := PathUnder("/workspace")

	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{"relative path in root", []string{"file.txt"}, false},
		{"nested path", []string{"subdir/file.txt"}, false},
		{"absolute path in root", []string{"/workspace/file.txt"}, false},
		{"path traversal", []string{"../etc/passwd"}, true},
		{"absolute escape", []string{"/etc/passwd"}, true},
		{"stdin dash allowed", []string{"-"}, false},
		{"dot dot escape", []string{"subdir/../../etc/passwd"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator(ExecSpec{Name: "cat", Args: tt.args})
			if (err != nil) != tt.wantErr {
				t.Errorf("PathUnder() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestExecCommandWithValidators(t *testing.T) {
	t.Run("passes with valid command", func(t *testing.T) {
		cmd, err := execCommandWithValidators("echo", []string{"hello"}, AllowlistBins("echo"))
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if cmd == nil {
			t.Fatal("expected command, got nil")
		}
	})

	t.Run("fails with blocked binary", func(t *testing.T) {
		_, err := execCommandWithValidators("rm", []string{"-rf", "/"}, AllowlistBins("echo"))
		if err == nil {
			t.Error("expected error for blocked binary")
		}
	})

	t.Run("fails with shell metacharacters", func(t *testing.T) {
		_, err := execCommandWithValidators("echo", []string{"hello", "|", "cat"}, NoShellMeta())
		if err == nil {
			t.Error("expected error for shell metacharacters")
		}
	})

	t.Run("chains multiple validators", func(t *testing.T) {
		_, err := execCommandWithValidators(
			"echo",
			[]string{"hello"},
			AllowlistBins("echo"),
			NoShellMeta(),
			NoControlChars(),
		)
		if err != nil {
			t.Errorf("expected no error with valid command, got: %v", err)
		}
	})
}

func TestMockExecutorReset(t *testing.T) {
	mock := &MockExecutor{}

	// Execute some commands
	_, _ = mock.Command("kubectl", []string{"get", "pods"})
	_, _ = mock.Command("docker", []string{"build", "."})

	if len(mock.Commands) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(mock.Commands))
	}

	// Reset and verify cleared
	mock.Reset()

	if len(mock.Commands) != 0 {
		t.Errorf("expected 0 commands after Reset, got %d", len(mock.Commands))
	}
}

func TestMockCommandSetStdin(t *testing.T) {
	mock := &MockCommand{}

	// Create a simple reader
	r, w, _ := os.Pipe()
	defer r.Close()
	defer w.Close()

	mock.SetStdin(r)

	if mock.StdinR != r {
		t.Errorf("SetStdin did not set StdinR correctly")
	}
}

func TestMockExecutorLastCommand(t *testing.T) {
	t.Run("returns_empty_when_no_commands", func(t *testing.T) {
		mock := &MockExecutor{}
		last := mock.LastCommand()
		if last.Name != "" || len(last.Args) != 0 {
			t.Errorf("expected empty ExecSpec, got %+v", last)
		}
	})

	t.Run("returns_last_command", func(t *testing.T) {
		mock := &MockExecutor{}
		_, _ = mock.Command("first", []string{"arg1"})
		_, _ = mock.Command("second", []string{"arg2"})

		last := mock.LastCommand()
		if last.Name != "second" {
			t.Errorf("expected name 'second', got %q", last.Name)
		}
	})
}

func TestMockExecutorHasCommand(t *testing.T) {
	mock := &MockExecutor{}
	_, _ = mock.Command("kubectl", []string{"get", "pods"})

	if !mock.HasCommand("kubectl") {
		t.Error("expected HasCommand('kubectl') to be true")
	}
	if mock.HasCommand("docker") {
		t.Error("expected HasCommand('docker') to be false")
	}
}
