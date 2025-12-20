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

func TestOperatorImageStepSetsContext(t *testing.T) {
	ctx := &SetupContext{
		Plan: SetupPlan{
			TestMode: true,
		},
	}
	deps := SetupDeps{
		OperatorImageFor: func(_ *ExternalRegistryConfig, _ bool) string {
			return "docker.io/example/operator:latest"
		},
	}

	step := operatorImageStep{}
	if err := step.Run(zap.NewNop(), deps, ctx); err != nil {
		t.Fatalf("operator image step failed: %v", err)
	}
	if ctx.OperatorImage != "docker.io/example/operator:latest" {
		t.Fatalf("expected operator image to be set, got %q", ctx.OperatorImage)
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
