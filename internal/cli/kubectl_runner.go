package cli

// This file defines the KubectlRunner interface for kubectl operations.
// This interface is used by setup helpers to abstract kubectl command execution.

import "io"

// KubectlRunner captures the kubectl methods used by setup helpers.
type KubectlRunner interface {
	CommandArgs(args []string) (Command, error)
	Run(args []string) error
	RunWithOutput(args []string, stdout, stderr io.Writer) error
}
