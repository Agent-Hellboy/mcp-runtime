package core

import "go.uber.org/zap"

// Runtime is the shared CLI facade for wiring common dependencies once and
// handing typed managers to the foldered command packages.
type Runtime struct {
	logger   *zap.Logger
	config   *CLIConfig
	kubectl  *KubectlClient
	executor Executor
	printer  *Printer
}

// NewRuntime builds the shared CLI runtime facade.
func NewRuntime(logger *zap.Logger) *Runtime {
	return &Runtime{
		logger:   logger,
		config:   DefaultCLIConfig,
		kubectl:  kubectlClient,
		executor: execExecutor,
		printer:  DefaultPrinter,
	}
}

// Logger returns the shared logger.
func (r *Runtime) Logger() *zap.Logger {
	return r.logger
}

// Config returns the loaded CLI configuration.
func (r *Runtime) Config() *CLIConfig {
	return r.config
}

// KubectlRunner returns the shared kubectl runner.
func (r *Runtime) KubectlRunner() KubectlRunner {
	return r.kubectl
}

// KubectlClient returns the shared kubectl client.
func (r *Runtime) KubectlClient() *KubectlClient {
	return r.kubectl
}

// Executor returns the shared process executor.
func (r *Runtime) Executor() Executor {
	return r.executor
}

// Printer returns the shared terminal printer.
func (r *Runtime) Printer() *Printer {
	return r.printer
}
