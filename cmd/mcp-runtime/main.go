package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"mcp-runtime/internal/cli/core"
	cliroot "mcp-runtime/internal/cli/root"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
	debug   = false
)

func main() {
	logger, err := newConsoleLogger(debug)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to init logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	initCommands(logger)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "mcp-runtime",
	Short: "MCP Runtime Management CLI",
	Long: `MCP Runtime CLI provides commands to manage the MCP platform including:
- Container registry
- Kubernetes cluster
- MCP server deployments
- Platform configuration`,
	Version: fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date),
	// Runtime errors should not trigger Cobra's usage/help dump; flag/arg
	// validation errors still do (those happen before RunE). main() prints
	// the error itself, so silence Cobra's own error print to avoid duplicates.
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Set debug mode globally so logStructuredError can check it
		core.SetDebugMode(debug)
	},
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "Enable debug mode with structured error logging")
}

func initCommands(logger *zap.Logger) {
	cliroot.AddCommands(rootCmd, logger)
}

// newConsoleLogger returns a human-friendly console logger with timestamps and caller info.
// If debug is true, sets log level to Debug to enable all debug logs.
// Otherwise, sets to ErrorLevel so structured error logs (when debug flag is enabled) will show.
func newConsoleLogger(debug bool) (*zap.Logger, error) {
	cfg := zap.NewProductionConfig()
	cfg.Encoding = "console"
	level := zap.ErrorLevel // Error level allows Error logs to show
	if debug {
		level = zap.DebugLevel // Debug level shows all logs
	}
	cfg.Level = zap.NewAtomicLevelAt(level)
	cfg.EncoderConfig = zapcore.EncoderConfig{
		TimeKey:        "ts",
		LevelKey:       "level",
		NameKey:        "",
		CallerKey:      "",
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalColorLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
	}
	cfg.OutputPaths = []string{"stdout"}
	cfg.ErrorOutputPaths = []string{"stderr"}
	cfg.DisableCaller = true
	cfg.DisableStacktrace = true
	return cfg.Build()
}
