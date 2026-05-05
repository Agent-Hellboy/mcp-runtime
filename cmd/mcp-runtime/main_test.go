package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRootCommandHelp(t *testing.T) {
	logger, err := newConsoleLogger(false)
	if err != nil {
		t.Fatalf("newConsoleLogger() error: %v", err)
	}
	defer logger.Sync()

	initCommands(logger)

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs([]string{"--help"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("rootCmd.Execute() error: %v", err)
	}

	if !strings.Contains(out.String(), "MCP Runtime CLI provides commands to manage the MCP platform") {
		t.Fatalf("help output missing expected text")
	}
}

func TestSilenceUsageAfterValidation(t *testing.T) {
	t.Run("keeps usage for validation errors", func(t *testing.T) {
		root := &cobra.Command{Use: "root", SilenceErrors: true}
		cmd := &cobra.Command{
			Use:  "needs-arg [name]",
			Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return nil
			},
		}
		root.AddCommand(cmd)
		silenceUsageAfterValidation(root)

		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs([]string{"needs-arg"})

		if err := root.Execute(); err == nil {
			t.Fatal("expected validation error")
		}
		if !strings.Contains(out.String(), "Usage:") {
			t.Fatalf("expected usage for validation error, got: %q", out.String())
		}
	})

	t.Run("hides usage for runtime errors", func(t *testing.T) {
		root := &cobra.Command{Use: "root", SilenceErrors: true}
		cmd := &cobra.Command{
			Use: "runtime",
			RunE: func(cmd *cobra.Command, args []string) error {
				return errors.New("runtime failed")
			},
		}
		root.AddCommand(cmd)
		silenceUsageAfterValidation(root)

		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs([]string{"runtime"})

		if err := root.Execute(); err == nil {
			t.Fatal("expected runtime error")
		}
		if strings.Contains(out.String(), "Usage:") {
			t.Fatalf("did not expect usage for runtime error, got: %q", out.String())
		}
	})
}
