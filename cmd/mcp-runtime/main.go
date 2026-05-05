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
	// Keep usage enabled during Cobra validation; command wrappers disable it
	// after validation passes so runtime errors do not dump command help.
	SilenceUsage: false,
	// main() prints the error itself, so silence Cobra's own error print to
	// avoid duplicates.
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
	silenceUsageAfterValidation(rootCmd)
}

func silenceUsageAfterValidation(cmd *cobra.Command) {
	if cmd.RunE != nil {
		runE := cmd.RunE
		cmd.RunE = func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			return runE(cmd, args)
		}
	}
	if cmd.Run != nil {
		run := cmd.Run
		cmd.Run = func(cmd *cobra.Command, args []string) {
			cmd.SilenceUsage = true
			run(cmd, args)
		}
	}
	for _, child := range cmd.Commands() {
		silenceUsageAfterValidation(child)
	}
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
