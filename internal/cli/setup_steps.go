package cli

// This file defines the setup step execution framework.
// It provides a pipeline-based approach for running setup steps with dependency injection and testability.

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
	GatewayProxyImage     string
	AnalyticsImages       AnalyticsImageSet
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
	return setupClusterSteps(logger, ctx.Plan.Kubeconfig, ctx.Plan.Context, ctx.Plan.Ingress, deps)
}

type tlsStep struct{}

func (s tlsStep) Name() string { return "tls" }
func (s tlsStep) Run(logger *zap.Logger, deps SetupDeps, ctx *SetupContext) error {
	return setupTLSStep(logger, ctx.Plan, deps)
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
	operatorImage, gatewayProxyImage, err := prepareDeploymentImages(
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
	ctx.GatewayProxyImage = gatewayProxyImage
	return nil
}

type deployOperatorStepCmd struct{}

func (s deployOperatorStepCmd) Name() string { return "operator-deploy" }
func (s deployOperatorStepCmd) Run(logger *zap.Logger, deps SetupDeps, ctx *SetupContext) error {
	return deployOperatorStep(
		logger,
		ctx.OperatorImage,
		ctx.GatewayProxyImage,
		ctx.ExternalRegistry,
		ctx.RegistrySecretName,
		ctx.UsingExternalRegistry,
		ctx.Plan.OperatorArgs,
		deps,
	)
}

type analyticsImageStep struct{}

func (s analyticsImageStep) Name() string { return "analytics-images" }
func (s analyticsImageStep) Run(logger *zap.Logger, deps SetupDeps, ctx *SetupContext) error {
	images, err := prepareAnalyticsImages(
		logger,
		ctx.ExternalRegistry,
		ctx.UsingExternalRegistry,
		ctx.Plan.TestMode,
		deps,
	)
	if err != nil {
		return err
	}
	ctx.AnalyticsImages = images
	return nil
}

type deployAnalyticsStep struct{}

func (s deployAnalyticsStep) Name() string { return "analytics-deploy" }
func (s deployAnalyticsStep) Run(logger *zap.Logger, deps SetupDeps, ctx *SetupContext) error {
	return deployAnalyticsStepCmd(logger, ctx.AnalyticsImages, ctx.Plan.StorageMode, deps)
}

type verifyStep struct{}

func (s verifyStep) Name() string { return "verify" }
func (s verifyStep) Run(logger *zap.Logger, deps SetupDeps, ctx *SetupContext) error {
	if err := verifySetup(logger, ctx.UsingExternalRegistry, deps); err != nil {
		Error("Post-setup verification failed")
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
		WithIf(ctx.Plan.DeployAnalytics, analyticsImageStep{}).
		With(deployOperatorStepCmd{}).
		WithIf(ctx.Plan.DeployAnalytics, deployAnalyticsStep{}).
		With(verifyStep{}).
		Build()
}

func runSetupSteps(logger *zap.Logger, deps SetupDeps, ctx *SetupContext, steps []SetupStep) error {
	for _, step := range steps {
		if err := step.Run(logger, deps, ctx); err != nil {
			wrappedErr := wrapWithSentinelAndContext(
				ErrSetupStepFailed,
				err,
				fmt.Sprintf("setup step %q failed: %v", step.Name(), err),
				map[string]any{"step": step.Name(), "component": "setup"},
			)
			Error("Setup step failed")
			logStructuredError(logger, wrappedErr, "Setup step failed")
			return wrappedErr
		}
	}
	return nil
}
