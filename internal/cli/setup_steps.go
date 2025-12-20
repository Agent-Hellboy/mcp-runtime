package cli

import (
	"fmt"

	"go.uber.org/zap"
)

// SetupContext carries state shared across setup steps.
type SetupContext struct {
	Plan                  SetupPlan
	ExternalRegistry      *ExternalRegistryConfig
	UsingExternalRegistry bool
	RegistrySecretName    string
	OperatorImage         string
}

// SetupStep models a single setup phase.
type SetupStep interface {
	Name() string
	Run(logger *zap.Logger, deps SetupDeps, ctx *SetupContext) error
}

// SetupPipeline provides a fluent API for building step sequences.
type SetupPipeline struct {
	steps []SetupStep
}

func NewSetupPipeline() *SetupPipeline {
	return &SetupPipeline{}
}

func (p *SetupPipeline) With(step SetupStep) *SetupPipeline {
	p.steps = append(p.steps, step)
	return p
}

func (p *SetupPipeline) WithIf(condition bool, step SetupStep) *SetupPipeline {
	if condition {
		p.steps = append(p.steps, step)
	}
	return p
}

func (p *SetupPipeline) Build() []SetupStep {
	return p.steps
}

type clusterStep struct{}

func (s clusterStep) Name() string { return "cluster" }
func (s clusterStep) Run(logger *zap.Logger, deps SetupDeps, ctx *SetupContext) error {
	return setupClusterSteps(logger, ctx.Plan.Ingress, deps)
}

type tlsStep struct{}

func (s tlsStep) Name() string { return "tls" }
func (s tlsStep) Run(logger *zap.Logger, deps SetupDeps, ctx *SetupContext) error {
	return setupTLSStep(logger, ctx.Plan.TLSEnabled, deps)
}

type registryStep struct{}

func (s registryStep) Name() string { return "registry" }
func (s registryStep) Run(logger *zap.Logger, deps SetupDeps, ctx *SetupContext) error {
	return setupRegistryStep(
		logger,
		ctx.ExternalRegistry,
		ctx.UsingExternalRegistry,
		ctx.Plan.RegistryType,
		ctx.Plan.RegistryStorageSize,
		ctx.Plan.RegistryManifest,
		ctx.Plan.TLSEnabled,
		deps,
	)
}

type operatorImageStep struct{}

func (s operatorImageStep) Name() string { return "operator-image" }
func (s operatorImageStep) Run(logger *zap.Logger, deps SetupDeps, ctx *SetupContext) error {
	operatorImage, err := prepareOperatorImage(
		logger,
		ctx.ExternalRegistry,
		ctx.UsingExternalRegistry,
		ctx.Plan.TestMode,
		deps,
	)
	if err != nil {
		return err
	}
	ctx.OperatorImage = operatorImage
	return nil
}

type deployOperatorStepCmd struct{}

func (s deployOperatorStepCmd) Name() string { return "operator-deploy" }
func (s deployOperatorStepCmd) Run(logger *zap.Logger, deps SetupDeps, ctx *SetupContext) error {
	return deployOperatorStep(
		logger,
		ctx.OperatorImage,
		ctx.ExternalRegistry,
		ctx.RegistrySecretName,
		ctx.UsingExternalRegistry,
		deps,
	)
}

type verifyStep struct{}

func (s verifyStep) Name() string { return "verify" }
func (s verifyStep) Run(logger *zap.Logger, deps SetupDeps, ctx *SetupContext) error {
	if err := verifySetup(ctx.UsingExternalRegistry, deps); err != nil {
		Error(fmt.Sprintf("Post-setup verification failed: %v", err))
		return err
	}
	return nil
}

func buildSetupSteps(ctx *SetupContext) []SetupStep {
	return NewSetupPipeline().
		With(clusterStep{}).
		WithIf(ctx.Plan.TLSEnabled, tlsStep{}).
		With(registryStep{}).
		With(operatorImageStep{}).
		With(deployOperatorStepCmd{}).
		With(verifyStep{}).
		Build()
}

func runSetupSteps(logger *zap.Logger, deps SetupDeps, ctx *SetupContext, steps []SetupStep) error {
	for _, step := range steps {
		if err := step.Run(logger, deps, ctx); err != nil {
			return fmt.Errorf("setup step %q failed: %w", step.Name(), err)
		}
	}
	return nil
}
