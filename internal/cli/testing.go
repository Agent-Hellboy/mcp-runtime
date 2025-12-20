package cli

import "io"

// MockCommand is a test double for Command interface.
type MockCommand struct {
	Args       []string
	OutputData []byte
	OutputErr  error
	RunErr     error
	StdoutW    io.Writer
	StderrW    io.Writer
	StdinR     io.Reader
	RunFunc    func() error
}

func (m *MockCommand) Output() ([]byte, error)         { return m.OutputData, m.OutputErr }
func (m *MockCommand) CombinedOutput() ([]byte, error) { return m.OutputData, m.OutputErr }
func (m *MockCommand) Run() error {
	if m.RunFunc != nil {
		if err := m.RunFunc(); err != nil {
			return err
		}
	}
	return m.RunErr
}
func (m *MockCommand) SetStdout(w io.Writer) { m.StdoutW = w }
func (m *MockCommand) SetStderr(w io.Writer) { m.StderrW = w }
func (m *MockCommand) SetStdin(r io.Reader)  { m.StdinR = r }

// MockExecutor is a test double for Executor interface.
type MockExecutor struct {
	// Commands records all commands that were created.
	Commands []ExecSpec
	// DefaultOutput is returned by commands when CommandFunc is nil.
	DefaultOutput []byte
	// DefaultErr is the error returned by Output/CombinedOutput.
	DefaultErr error
	// DefaultRunErr is the error returned by Run.
	DefaultRunErr error
	// CommandFunc allows custom behavior per command.
	CommandFunc func(spec ExecSpec) *MockCommand
}

func (m *MockExecutor) Command(name string, args []string, validators ...ExecValidator) (Command, error) {
	spec := ExecSpec{Name: name, Args: args}
	for _, v := range validators {
		if err := v(spec); err != nil {
			return nil, err
		}
	}
	m.Commands = append(m.Commands, spec)

	if m.CommandFunc != nil {
		return m.CommandFunc(spec), nil
	}
	return &MockCommand{
		Args:       args,
		OutputData: m.DefaultOutput,
		OutputErr:  m.DefaultErr,
		RunErr:     m.DefaultRunErr,
	}, nil
}

// LastCommand returns the most recent command spec.
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

// HasCommand checks if a command with the given name was executed.
func (m *MockExecutor) HasCommand(name string) bool {
	for _, c := range m.Commands {
		if c.Name == name {
			return true
		}
	}
	return false
}
