package cli

import (
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
)

// execCommand is a test seam for stubbing command creation in tests.
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

// execCmd wraps exec.Cmd to implement Command interface.
type execCmd struct {
	cmd *exec.Cmd
}

func (c *execCmd) Output() ([]byte, error)         { return c.cmd.Output() }
func (c *execCmd) CombinedOutput() ([]byte, error) { return c.cmd.CombinedOutput() }
func (c *execCmd) Run() error                      { return c.cmd.Run() }
func (c *execCmd) SetStdout(w io.Writer)           { c.cmd.Stdout = w }
func (c *execCmd) SetStderr(w io.Writer)           { c.cmd.Stderr = w }
func (c *execCmd) SetStdin(r io.Reader)            { c.cmd.Stdin = r }

// osExecutor is the production implementation using os/exec.
type osExecutor struct{}

func (osExecutor) Command(name string, args []string, validators ...ExecValidator) (Command, error) {
	spec := ExecSpec{Name: name, Args: args}
	for _, validate := range validators {
		if err := validate(spec); err != nil {
			return nil, err
		}
	}
	return &execCmd{cmd: execCommand(name, args...)}, nil
}

var execExecutor Executor = osExecutor{}

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
