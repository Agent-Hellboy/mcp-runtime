package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootCommandHelp(t *testing.T) {
	logger, err := newConsoleLogger()
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
