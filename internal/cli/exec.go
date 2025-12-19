package cli

import (
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
)

// execCommand is a seam for tests to stub out command creation.
var execCommand = exec.Command

// Command represents a command that can be executed.
type Command interface {
	Output() ([]byte, error)
	CombinedOutput() ([]byte, error)
	Run() error
	SetStdout(w io.Writer)
	SetStderr(w io.Writer)
	SetStdin(r io.Reader)
}

// Executor creates commands for execution.
type Executor interface {
	Command(name string, args []string, validators ...ExecValidator) (Command, error)
}

// realCommand wraps exec.Cmd to implement Command interface.
type realCommand struct {
	cmd *exec.Cmd
}

func (c *realCommand) Output() ([]byte, error)         { return c.cmd.Output() }
func (c *realCommand) CombinedOutput() ([]byte, error) { return c.cmd.CombinedOutput() }
func (c *realCommand) Run() error                      { return c.cmd.Run() }
func (c *realCommand) SetStdout(w io.Writer)           { c.cmd.Stdout = w }
func (c *realCommand) SetStderr(w io.Writer)           { c.cmd.Stderr = w }
func (c *realCommand) SetStdin(r io.Reader)            { c.cmd.Stdin = r }

type defaultExecutor struct{}

func (defaultExecutor) Command(name string, args []string, validators ...ExecValidator) (Command, error) {
	spec := ExecSpec{Name: name, Args: args}
	for _, validate := range validators {
		if err := validate(spec); err != nil {
			return nil, err
		}
	}
	return &realCommand{cmd: execCommand(name, args...)}, nil
}

var execExecutor Executor = defaultExecutor{}

type ExecSpec struct {
	Name string
	Args []string
}

type ExecValidator func(ExecSpec) error

func execCommandWithValidators(name string, args []string, validators ...ExecValidator) (Command, error) {
	return execExecutor.Command(name, args, validators...)
}

func AllowlistBins(allowed ...string) ExecValidator {
	set := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		set[name] = struct{}{}
	}
	return func(spec ExecSpec) error {
		if _, ok := set[spec.Name]; !ok {
			return errors.New("exec: binary not allowed")
		}
		return nil
	}
}

func NoShellMeta() ExecValidator {
	return func(spec ExecSpec) error {
		for _, arg := range spec.Args {
			if strings.ContainsAny(arg, "&|;<>()$`\\") {
				return errors.New("exec: shell metacharacters not allowed")
			}
		}
		return nil
	}
}

func NoControlChars() ExecValidator {
	return func(spec ExecSpec) error {
		for _, arg := range spec.Args {
			if strings.ContainsAny(arg, "\r\n\t") {
				return errors.New("exec: control characters not allowed")
			}
		}
		return nil
	}
}

func PathUnder(root string) ExecValidator {
	absRoot := root
	if abs, err := filepath.Abs(root); err == nil {
		absRoot = abs
	}
	return func(spec ExecSpec) error {
		for _, arg := range spec.Args {
			if arg == "-" {
				continue
			}
			candidate := arg
			if !filepath.IsAbs(candidate) {
				candidate = filepath.Join(absRoot, candidate)
			}
			candidate = filepath.Clean(candidate)
			rel, err := filepath.Rel(absRoot, candidate)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return errors.New("exec: path escapes root")
			}
		}
		return nil
	}
}

// MockCommand is a test double for Command.
type MockCommand struct {
	Spec       ExecSpec
	OutputData []byte
	OutputErr  error
	RunErr     error
	StdoutW    io.Writer
	StderrW    io.Writer
	StdinR     io.Reader
}

func (m *MockCommand) Output() ([]byte, error)         { return m.OutputData, m.OutputErr }
func (m *MockCommand) CombinedOutput() ([]byte, error) { return m.OutputData, m.OutputErr }
func (m *MockCommand) Run() error                      { return m.RunErr }
func (m *MockCommand) SetStdout(w io.Writer)           { m.StdoutW = w }
func (m *MockCommand) SetStderr(w io.Writer)           { m.StderrW = w }
func (m *MockCommand) SetStdin(r io.Reader)            { m.StdinR = r }

// MockExecutor is a test double for Executor.
type MockExecutor struct {
	Commands   []ExecSpec
	OutputData []byte
	OutputErr  error
	RunErr     error
	// CommandFunc allows custom behavior per command.
	CommandFunc func(spec ExecSpec) *MockCommand
}

func (m *MockExecutor) Command(name string, args []string, validators ...ExecValidator) (Command, error) {
	spec := ExecSpec{Name: name, Args: args}
	for _, validate := range validators {
		if err := validate(spec); err != nil {
			return nil, err
		}
	}
	m.Commands = append(m.Commands, spec)

	if m.CommandFunc != nil {
		return m.CommandFunc(spec), nil
	}
	return &MockCommand{
		Spec:       spec,
		OutputData: m.OutputData,
		OutputErr:  m.OutputErr,
		RunErr:     m.RunErr,
	}, nil
}

// LastCommand returns the last recorded command.
func (m *MockExecutor) LastCommand() ExecSpec {
	if len(m.Commands) == 0 {
		return ExecSpec{}
	}
	return m.Commands[len(m.Commands)-1]
}

// Reset clears recorded commands.
func (m *MockExecutor) Reset() {
	m.Commands = nil
}
