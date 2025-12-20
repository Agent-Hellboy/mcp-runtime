package cli

import "io"

// KubectlRunner captures the kubectl methods used by setup helpers.
type KubectlRunner interface {
	CommandArgs(args []string) (Command, error)
	Run(args []string) error
	RunWithOutput(args []string, stdout, stderr io.Writer) error
}
