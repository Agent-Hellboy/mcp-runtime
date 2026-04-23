package cli

import (
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

type fakeRegistryManagerForSteps struct {
	showInfoCalls int32
}

func (f *fakeRegistryManagerForSteps) ShowRegistryInfo() error {
	atomic.AddInt32(&f.showInfoCalls, 1)
	return nil
}

func (f *fakeRegistryManagerForSteps) PushInCluster(_, _, _ string) error {
	return nil
}

func TestBuildSetupStepsOrderWithTLS(t *testing.T) {
	ctx := &SetupContext{
		Plan: SetupPlan{
			TLSEnabled: true,
		},
	}
	steps := buildSetupSteps(ctx)
	if len(steps) != 6 {
		t.Fatalf("expected 6 steps, got %d", len(steps))
	}

	got := []string{
		steps[0].Name(),
		steps[1].Name(),
		steps[2].Name(),
		steps[3].Name(),
		steps[4].Name(),
		steps[5].Name(),
	}
	want := []string{"cluster", "tls", "registry", "operator-image", "operator-deploy", "verify"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("step %d: expected %q, got %q", i, want[i], got[i])
		}
	}
}

func TestBuildSetupStepsOrderWithoutTLS(t *testing.T) {
	ctx := &SetupContext{
		Plan: SetupPlan{
			TLSEnabled: false,
		},
	}
	steps := buildSetupSteps(ctx)
	if len(steps) != 5 {
		t.Fatalf("expected 5 steps, got %d", len(steps))
	}

	got := []string{
		steps[0].Name(),
		steps[1].Name(),
		steps[2].Name(),
		steps[3].Name(),
		steps[4].Name(),
	}
	want := []string{"cluster", "registry", "operator-image", "operator-deploy", "verify"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("step %d: expected %q, got %q", i, want[i], got[i])
		}
	}
}

func TestBuildSetupStepsOrderWithAnalytics(t *testing.T) {
	ctx := &SetupContext{
		Plan: SetupPlan{
			DeployAnalytics: true,
		},
	}
	steps := buildSetupSteps(ctx)
	if len(steps) != 7 {
		t.Fatalf("expected 7 steps, got %d", len(steps))
	}

	got := []string{
		steps[0].Name(),
		steps[1].Name(),
		steps[2].Name(),
		steps[3].Name(),
		steps[4].Name(),
		steps[5].Name(),
		steps[6].Name(),
	}
	want := []string{"cluster", "registry", "operator-image", "analytics-images", "operator-deploy", "analytics-deploy", "verify"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("step %d: expected %q, got %q", i, want[i], got[i])
		}
	}
}

func TestOperatorImageStepSetsContext(t *testing.T) {
	ctx := &SetupContext{
		Plan: SetupPlan{},
		ExternalRegistry: &ExternalRegistryConfig{
			URL: "registry.example.com",
		},
		UsingExternalRegistry: true,
	}
	deps := SetupDeps{
		OperatorImageFor: func(_ *ExternalRegistryConfig) string {
			return "registry.example.com/mcp-runtime-operator:latest"
		},
		GatewayProxyImageFor: func(_ *ExternalRegistryConfig) string {
			return "registry.example.com/mcp-sentinel-mcp-proxy:latest"
		},
		BuildOperatorImage:     func(string) error { return nil },
		PushOperatorImage:      func(string) error { return nil },
		BuildGatewayProxyImage: func(string) error { return nil },
		PushGatewayProxyImage:  func(string) error { return nil },
	}

	step := operatorImageStep{}
	if err := step.Run(zap.NewNop(), deps, ctx); err != nil {
		t.Fatalf("operator image step failed: %v", err)
	}
	if ctx.OperatorImage != "registry.example.com/mcp-runtime-operator:latest" {
		t.Fatalf("expected operator image to be set, got %q", ctx.OperatorImage)
	}
	if ctx.GatewayProxyImage != "registry.example.com/mcp-sentinel-mcp-proxy:latest" {
		t.Fatalf("expected gateway proxy image to be set, got %q", ctx.GatewayProxyImage)
	}
}

func TestOperatorImageStepTestModeBuildsAndPushesToRegistry(t *testing.T) {
	var buildCalls int32
	var gatewayBuildCalls int32
	var pushCalls int32
	var gatewayPushCalls int32
	ctx := &SetupContext{
		Plan: SetupPlan{
			TestMode: true,
		},
		ExternalRegistry:      &ExternalRegistryConfig{URL: "registry.example.com"},
		UsingExternalRegistry: true,
	}
	deps := SetupDeps{
		OperatorImageFor:     func(_ *ExternalRegistryConfig) string { return "registry.example.com/mcp-runtime-operator:latest" },
		GatewayProxyImageFor: func(_ *ExternalRegistryConfig) string { return "registry.example.com/mcp-sentinel-mcp-proxy:latest" },
		BuildOperatorImage:   func(string) error { atomic.AddInt32(&buildCalls, 1); return nil },
		PushOperatorImage:    func(string) error { atomic.AddInt32(&pushCalls, 1); return nil },
		BuildGatewayProxyImage: func(string) error {
			atomic.AddInt32(&gatewayBuildCalls, 1)
			return nil
		},
		PushGatewayProxyImage: func(string) error { atomic.AddInt32(&gatewayPushCalls, 1); return nil },
	}

	step := operatorImageStep{}
	if err := step.Run(zap.NewNop(), deps, ctx); err != nil {
		t.Fatalf("operator image step failed: %v", err)
	}
	if ctx.OperatorImage != "registry.example.com/mcp-runtime-operator:latest" {
		t.Fatalf("expected test mode operator image to use registry, got %q", ctx.OperatorImage)
	}
	if ctx.GatewayProxyImage != "registry.example.com/mcp-sentinel-mcp-proxy:latest" {
		t.Fatalf("expected test mode gateway image to use registry, got %q", ctx.GatewayProxyImage)
	}
	if atomic.LoadInt32(&buildCalls) != 1 {
		t.Fatalf("expected operator build in test mode, got %d calls", buildCalls)
	}
	if atomic.LoadInt32(&gatewayBuildCalls) != 1 {
		t.Fatalf("expected gateway build in test mode, got %d calls", gatewayBuildCalls)
	}
	if atomic.LoadInt32(&pushCalls) != 1 {
		t.Fatalf("expected operator push in test mode, got %d calls", pushCalls)
	}
	if atomic.LoadInt32(&gatewayPushCalls) != 1 {
		t.Fatalf("expected gateway push in test mode, got %d calls", gatewayPushCalls)
	}
}

func TestDeployOperatorStepCmdPassesOperatorArgs(t *testing.T) {
	ctx := &SetupContext{
		Plan: SetupPlan{
			OperatorArgs: []string{"--metrics-bind-address=:9090", "--leader-elect=false"},
		},
		OperatorImage:         "registry.example.com/mcp-runtime-operator:latest",
		GatewayProxyImage:     "registry.example.com/mcp-sentinel-mcp-proxy:latest",
		UsingExternalRegistry: false,
	}
	var gotArgs []string
	var gotGatewayImage string
	deps := SetupDeps{
		DeployOperatorManifests: func(_ *zap.Logger, image, gatewayImage string, args []string) error {
			if image != ctx.OperatorImage {
				t.Fatalf("expected operator image %q, got %q", ctx.OperatorImage, image)
			}
			gotGatewayImage = gatewayImage
			gotArgs = append([]string(nil), args...)
			return nil
		},
		RestartDeployment: func(string, string) error { return nil },
	}

	step := deployOperatorStepCmd{}
	if err := step.Run(zap.NewNop(), deps, ctx); err != nil {
		t.Fatalf("deploy operator step failed: %v", err)
	}
	if len(gotArgs) != len(ctx.Plan.OperatorArgs) {
		t.Fatalf("expected %d operator args, got %d (%v)", len(ctx.Plan.OperatorArgs), len(gotArgs), gotArgs)
	}
	for i := range ctx.Plan.OperatorArgs {
		if gotArgs[i] != ctx.Plan.OperatorArgs[i] {
			t.Fatalf("expected operator arg %d to be %q, got %q", i, ctx.Plan.OperatorArgs[i], gotArgs[i])
		}
	}
	if gotGatewayImage != ctx.GatewayProxyImage {
		t.Fatalf("expected gateway image %q, got %q", ctx.GatewayProxyImage, gotGatewayImage)
	}
}

func TestNewSetupCmdIncludesFeatureFlags(t *testing.T) {
	cmd := NewSetupCmd(zap.NewNop())

	for _, name := range []string{
		"test-mode",
		"strict-prod",
		"operator-metrics-addr",
		"operator-probe-addr",
		"operator-leader-elect",
	} {
		if flag := cmd.Flags().Lookup(name); flag == nil {
			t.Fatalf("expected %q flag to exist", name)
		}
	}
}

func TestRegistryStepDeploysInternalRegistry(t *testing.T) {
	var deployCalls int32
	var waitCalls int32
	fakeRegistry := &fakeRegistryManagerForSteps{}
	ctx := &SetupContext{
		Plan: SetupPlan{
			RegistryType:        "docker",
			RegistryStorageSize: "1Gi",
			RegistryManifest:    "config/registry",
		},
		UsingExternalRegistry: false,
	}
	deps := SetupDeps{
		DeployRegistry: func(_ *zap.Logger, namespace string, port int, registryType, registryStorageSize, manifestPath string) error {
			if namespace != "registry" || port != 5000 || registryType != "docker" || registryStorageSize != "1Gi" || manifestPath != "config/registry" {
				t.Fatalf("unexpected deploy args: %s %d %s %s %s", namespace, port, registryType, registryStorageSize, manifestPath)
			}
			atomic.AddInt32(&deployCalls, 1)
			return nil
		},
		WaitForDeploymentAvailable: func(_ *zap.Logger, name, namespace, selector string, _ time.Duration) error {
			if name != "registry" || namespace != "registry" || selector != "app=registry" {
				t.Fatalf("unexpected wait args: %s %s %s", name, namespace, selector)
			}
			atomic.AddInt32(&waitCalls, 1)
			return nil
		},
		PrintDeploymentDiagnostics: func(_, _, _ string) {},
		GetDeploymentTimeout:       func() time.Duration { return time.Second },
		GetRegistryPort:            func() int { return 5000 },
		RegistryManager:            fakeRegistry,
	}

	step := registryStep{}
	if err := step.Run(zap.NewNop(), deps, ctx); err != nil {
		t.Fatalf("registry step failed: %v", err)
	}
	if atomic.LoadInt32(&deployCalls) != 1 {
		t.Fatalf("expected deploy to be called once, got %d", deployCalls)
	}
	if atomic.LoadInt32(&waitCalls) != 1 {
		t.Fatalf("expected wait to be called once, got %d", waitCalls)
	}
	if atomic.LoadInt32(&fakeRegistry.showInfoCalls) != 1 {
		t.Fatalf("expected registry info to be shown once, got %d", fakeRegistry.showInfoCalls)
	}
}

func TestVerifyStepCallsChecks(t *testing.T) {
	var waitCalls int32
	var crdCalls int32
	ctx := &SetupContext{
		UsingExternalRegistry: false,
	}
	deps := SetupDeps{
		WaitForDeploymentAvailable: func(_ *zap.Logger, name, namespace, selector string, _ time.Duration) error {
			atomic.AddInt32(&waitCalls, 1)
			return nil
		},
		PrintDeploymentDiagnostics: func(_, _, _ string) {},
		CheckCRDInstalled: func(_ string) error {
			atomic.AddInt32(&crdCalls, 1)
			return nil
		},
		GetDeploymentTimeout: func() time.Duration { return time.Second },
	}

	step := verifyStep{}
	if err := step.Run(zap.NewNop(), deps, ctx); err != nil {
		t.Fatalf("verify step failed: %v", err)
	}
	if atomic.LoadInt32(&waitCalls) != 2 {
		t.Fatalf("expected 2 wait calls, got %d", waitCalls)
	}
	if atomic.LoadInt32(&crdCalls) != 1 {
		t.Fatalf("expected 1 CRD check, got %d", crdCalls)
	}
}
