# cmd/mcp-runtime/main.go

- L1: declares the `main` package so the file builds to a binary.
- L3-L11: imports standard IO/OS plus `cobra` for CLI wiring and `zap` for logging, pulling CLI commands from `internal/cli`.
- L13-L17: version metadata variables (`version`, `commit`, `date`) defaulted for local builds and typically set at link time.
- L19-L33: `main()` builds a console logger, aborts on failure, defers sync, registers all subcommands via `initCommands`, and executes the Cobra root command; errors are printed to stderr and exit non-zero.
- L35-L48: `rootCmd` defines the Cobra root with usage text, a multi-line description of the platform management focus, and a version string that includes build metadata.
- L50-L57: `initCommands` attaches the cluster, registry, server, access, bootstrap, setup, status, sentinel, and pipeline subcommands (nine total) provided by `internal/cli` packages using the shared logger.
- L60-L82: `newConsoleLogger` creates a `zap` production config tuned for human console output (console encoding, warn level, ISO timestamps, colored levels, no caller/stack info) and directs output to stdout/stderr.
