package cli

import (
	"fmt"
	"io"
	"os"
)

// KubectlClient wraps kubectl command execution with validation.
type KubectlClient struct {
	exec       Executor
	validators []ExecValidator
}

// NewKubectlClient creates a KubectlClient with default validators.
func NewKubectlClient(exec Executor) (*KubectlClient, error) {
	root, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get working directory: %w", err)
	}
	return &KubectlClient{
		exec: exec,
		validators: []ExecValidator{
			NoControlChars(), // Prevent YAML/command injection via control chars
			PathUnder(root),
		},
	}, nil
}

// CommandArgs builds a kubectl command with the given arguments.
// Validates arguments against configured validators before building.
func (c *KubectlClient) CommandArgs(args []string) (Command, error) {
	return c.exec.Command("kubectl", args, c.validators...)
}

// Output runs kubectl with the given arguments and returns stdout.
func (c *KubectlClient) Output(args []string) ([]byte, error) {
	cmd, err := c.CommandArgs(args)
	if err != nil {
		return nil, err
	}
	return cmd.Output()
}

// CombinedOutput runs kubectl with the given arguments and returns combined stdout/stderr.
func (c *KubectlClient) CombinedOutput(args []string) ([]byte, error) {
	cmd, err := c.CommandArgs(args)
	if err != nil {
		return nil, err
	}
	return cmd.CombinedOutput()
}

// Run runs kubectl with the given arguments.
func (c *KubectlClient) Run(args []string) error {
	cmd, err := c.CommandArgs(args)
	if err != nil {
		return err
	}
	return cmd.Run()
}

// RunWithOutput runs kubectl with the given arguments, piping to the provided writers.
func (c *KubectlClient) RunWithOutput(args []string, stdout, stderr io.Writer) error {
	cmd, err := c.CommandArgs(args)
	if err != nil {
		return err
	}
	cmd.SetStdout(stdout)
	cmd.SetStderr(stderr)
	return cmd.Run()
}

var kubectlClient = mustNewKubectlClient()

func mustNewKubectlClient() *KubectlClient {
	client, err := NewKubectlClient(execExecutor)
	if err != nil {
		panic(err)
	}
	return client
}
