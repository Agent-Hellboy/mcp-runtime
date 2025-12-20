package cli

import (
	"fmt"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestBuildSetupPlan_DefaultHTTP(t *testing.T) {
	plan := BuildSetupPlan(SetupPlanInput{
		RegistryType:           "docker",
		RegistryStorageSize:    "20Gi",
		IngressMode:            "traefik",
		IngressManifest:        "config/ingress/overlays/http",
		IngressManifestChanged: false,
		ForceIngressInstall:    false,
		TLSEnabled:             false,
		TestMode:               false,
	})

	if plan.Ingress.manifest != "config/ingress/overlays/http" {
		t.Fatalf("expected http ingress manifest, got %q", plan.Ingress.manifest)
	}
	if plan.RegistryManifest != "config/registry" {
		t.Fatalf("expected default registry manifest, got %q", plan.RegistryManifest)
	}
}

func TestBuildSetupPlan_DefaultTLS(t *testing.T) {
	plan := BuildSetupPlan(SetupPlanInput{
		RegistryType:           "docker",
		RegistryStorageSize:    "20Gi",
		IngressMode:            "traefik",
		IngressManifest:        "config/ingress/overlays/http",
		IngressManifestChanged: false,
		ForceIngressInstall:    false,
		TLSEnabled:             true,
		TestMode:               false,
	})

	if plan.Ingress.manifest != "config/ingress/overlays/prod" {
		t.Fatalf("expected tls ingress manifest, got %q", plan.Ingress.manifest)
	}
	if plan.RegistryManifest != "config/registry/overlays/tls" {
		t.Fatalf("expected tls registry manifest, got %q", plan.RegistryManifest)
	}
}

func TestBuildSetupPlan_CustomIngressManifest(t *testing.T) {
	plan := BuildSetupPlan(SetupPlanInput{
		RegistryType:           "docker",
		RegistryStorageSize:    "20Gi",
		IngressMode:            "traefik",
		IngressManifest:        "custom/manifest",
		IngressManifestChanged: true,
		ForceIngressInstall:    true,
		TLSEnabled:             true,
		TestMode:               false,
	})

	if plan.Ingress.manifest != "custom/manifest" {
		t.Fatalf("expected custom ingress manifest, got %q", plan.Ingress.manifest)
	}
	if plan.RegistryManifest != "config/registry/overlays/tls" {
		t.Fatalf("expected tls registry manifest, got %q", plan.RegistryManifest)
	}
}

type callRecorder struct {
	calls []string
	waits []string
}

func (c *callRecorder) add(name string) {
	c.calls = append(c.calls, name)
}

func (c *callRecorder) addWait(name string) {
	c.waits = append(c.waits, name)
}

func (c *callRecorder) has(name string) bool {
	for _, call := range c.calls {
		if call == name {
			return true
		}
	}
	return false
}

func (c *callRecorder) hasWait(name string) bool {
	for _, call := range c.waits {
		if call == name {
			return true
		}
	}
	return false
}

type fakeClusterManager struct {
	rec *callRecorder
}

func (f *fakeClusterManager) InitCluster(_, _ string) error {
	f.rec.add("cluster-init")
	return nil
}

func (f *fakeClusterManager) ConfigureCluster(ingressOptions) error {
	f.rec.add("cluster-config")
	return nil
}

type fakeRegistryManager struct {
	rec *callRecorder
}

func (f *fakeRegistryManager) ShowRegistryInfo() error {
	f.rec.add("registry-info")
	return nil
}

func (f *fakeRegistryManager) PushInCluster(_, _, _ string) error {
	f.rec.add("registry-push")
	return nil
}

func TestSetupPlatformWithDeps_ExternalRegistry(t *testing.T) {
	rec := &callRecorder{}
	deps := SetupDeps{
		ResolveExternalRegistryConfig: func(*ExternalRegistryConfig) (*ExternalRegistryConfig, error) {
			return &ExternalRegistryConfig{
				URL:      "registry.example.com",
				Username: "user",
				Password: "pass",
			}, nil
		},
		ClusterManager:              &fakeClusterManager{rec: rec},
		RegistryManager:             &fakeRegistryManager{rec: rec},
		LoginRegistry:               func(*zap.Logger, string, string, string) error { rec.add("login"); return nil },
		DeployRegistry:              func(*zap.Logger, string, int, string, string, string) error { rec.add("deploy-registry"); return nil },
		WaitForDeploymentAvailable:  func(_ *zap.Logger, name, _, _ string, _ time.Duration) error { rec.addWait(name); return nil },
		PrintDeploymentDiagnostics:  func(string, string, string) { rec.add("diagnostics") },
		SetupTLS:                    func(*zap.Logger) error { rec.add("tls"); return nil },
		BuildOperatorImage:          func(string) error { rec.add("build"); return nil },
		PushOperatorImage:           func(string) error { rec.add("push"); return nil },
		EnsureNamespace:             func(string) error { rec.add("ensure-ns"); return nil },
		GetPlatformRegistryURL:      func(*zap.Logger) string { return "registry.local" },
		PushOperatorImageToInternal: func(*zap.Logger, string, string, string) error { rec.add("push-internal"); return nil },
		DeployOperatorManifests:     func(*zap.Logger, string) error { rec.add("deploy-operator"); return nil },
		ConfigureProvisionedRegistryEnv: func(*ExternalRegistryConfig, string) error {
			rec.add("configure-env")
			return nil
		},
		RestartDeployment:    func(string, string) error { rec.add("restart"); return nil },
		CheckCRDInstalled:    func(string) error { rec.add("check-crd"); return nil },
		GetDeploymentTimeout: func() time.Duration { return time.Second },
		GetRegistryPort:      func() int { return 5000 },
		OperatorImageFor: func(*ExternalRegistryConfig, bool) string {
			rec.add("operator-image")
			return "registry.example.com/mcp-runtime-operator:latest"
		},
	}

	plan := SetupPlan{
		RegistryType:        "docker",
		RegistryStorageSize: "20Gi",
		Ingress: ingressOptions{
			mode:     "traefik",
			manifest: "config/ingress/overlays/http",
			force:    false,
		},
		RegistryManifest: "config/registry",
		TLSEnabled:       false,
		TestMode:         true,
	}

	if err := setupPlatformWithDeps(zap.NewNop(), plan, deps); err != nil {
		t.Fatalf("setupPlatformWithDeps returned error: %v", err)
	}

	if !rec.has("login") {
		t.Fatalf("expected external registry login to be called")
	}
	if rec.has("deploy-registry") {
		t.Fatalf("did not expect internal registry deployment")
	}
	if rec.has("registry-info") {
		t.Fatalf("did not expect internal registry info")
	}
	if rec.has("build") || rec.has("push") || rec.has("push-internal") {
		t.Fatalf("did not expect image build/push in test mode")
	}
}

func TestSetupPlatformWithDeps_InternalRegistryTLS(t *testing.T) {
	rec := &callRecorder{}
	deps := SetupDeps{
		ResolveExternalRegistryConfig: func(*ExternalRegistryConfig) (*ExternalRegistryConfig, error) {
			return nil, nil
		},
		ClusterManager:  &fakeClusterManager{rec: rec},
		RegistryManager: &fakeRegistryManager{rec: rec},
		LoginRegistry: func(*zap.Logger, string, string, string) error {
			rec.add("login")
			return nil
		},
		DeployRegistry:             func(*zap.Logger, string, int, string, string, string) error { rec.add("deploy-registry"); return nil },
		WaitForDeploymentAvailable: func(_ *zap.Logger, name, _, _ string, _ time.Duration) error { rec.addWait(name); return nil },
		PrintDeploymentDiagnostics: func(string, string, string) { rec.add("diagnostics") },
		SetupTLS:                   func(*zap.Logger) error { rec.add("tls"); return nil },
		BuildOperatorImage:         func(string) error { rec.add("build"); return nil },
		PushOperatorImage:          func(string) error { rec.add("push"); return nil },
		EnsureNamespace:            func(string) error { rec.add("ensure-ns"); return nil },
		GetPlatformRegistryURL:     func(*zap.Logger) string { return "registry.local" },
		PushOperatorImageToInternal: func(*zap.Logger, string, string, string) error {
			rec.add("push-internal")
			return nil
		},
		DeployOperatorManifests: func(*zap.Logger, string) error { rec.add("deploy-operator"); return nil },
		ConfigureProvisionedRegistryEnv: func(*ExternalRegistryConfig, string) error {
			rec.add("configure-env")
			return nil
		},
		RestartDeployment:    func(string, string) error { rec.add("restart"); return nil },
		CheckCRDInstalled:    func(string) error { rec.add("check-crd"); return nil },
		GetDeploymentTimeout: func() time.Duration { return time.Second },
		GetRegistryPort:      func() int { return 5000 },
		OperatorImageFor: func(*ExternalRegistryConfig, bool) string {
			rec.add("operator-image")
			return "registry.local/mcp-runtime-operator:latest"
		},
	}

	plan := SetupPlan{
		RegistryType:        "docker",
		RegistryStorageSize: "20Gi",
		Ingress: ingressOptions{
			mode:     "traefik",
			manifest: "config/ingress/overlays/prod",
			force:    false,
		},
		RegistryManifest: "config/registry/overlays/tls",
		TLSEnabled:       true,
		TestMode:         false,
	}

	if err := setupPlatformWithDeps(zap.NewNop(), plan, deps); err != nil {
		t.Fatalf("setupPlatformWithDeps returned error: %v", err)
	}

	if !rec.has("tls") {
		t.Fatalf("expected TLS setup to be called")
	}
	if !rec.has("deploy-registry") {
		t.Fatalf("expected internal registry deployment")
	}
	if !rec.has("registry-info") {
		t.Fatalf("expected registry info")
	}
	if !rec.has("build") || !rec.has("push-internal") || !rec.has("ensure-ns") {
		t.Fatalf("expected internal build/push path, got calls: %v", rec.calls)
	}
	if rec.has("configure-env") || rec.has("login") {
		t.Fatalf("did not expect external registry configuration")
	}
	if !rec.hasWait("registry") || !rec.hasWait("mcp-runtime-operator-controller-manager") {
		t.Fatalf("expected waits for registry and operator deployments")
	}
}

func TestSetupPlatformWithDeps_ExternalRegistryTLS(t *testing.T) {
	rec := &callRecorder{}
	deps := SetupDeps{
		ResolveExternalRegistryConfig: func(*ExternalRegistryConfig) (*ExternalRegistryConfig, error) {
			return &ExternalRegistryConfig{
				URL:      "registry.example.com",
				Username: "user",
				Password: "pass",
			}, nil
		},
		ClusterManager:  &fakeClusterManager{rec: rec},
		RegistryManager: &fakeRegistryManager{rec: rec},
		LoginRegistry: func(*zap.Logger, string, string, string) error {
			rec.add("login")
			return nil
		},
		DeployRegistry:             func(*zap.Logger, string, int, string, string, string) error { rec.add("deploy-registry"); return nil },
		WaitForDeploymentAvailable: func(_ *zap.Logger, name, _, _ string, _ time.Duration) error { rec.addWait(name); return nil },
		PrintDeploymentDiagnostics: func(string, string, string) { rec.add("diagnostics") },
		SetupTLS:                   func(*zap.Logger) error { rec.add("tls"); return nil },
		BuildOperatorImage:         func(string) error { rec.add("build"); return nil },
		PushOperatorImage:          func(string) error { rec.add("push"); return nil },
		EnsureNamespace:            func(string) error { rec.add("ensure-ns"); return nil },
		GetPlatformRegistryURL:     func(*zap.Logger) string { return "registry.local" },
		PushOperatorImageToInternal: func(*zap.Logger, string, string, string) error {
			rec.add("push-internal")
			return nil
		},
		DeployOperatorManifests: func(*zap.Logger, string) error { rec.add("deploy-operator"); return nil },
		ConfigureProvisionedRegistryEnv: func(*ExternalRegistryConfig, string) error {
			rec.add("configure-env")
			return nil
		},
		RestartDeployment:    func(string, string) error { rec.add("restart"); return nil },
		CheckCRDInstalled:    func(string) error { rec.add("check-crd"); return nil },
		GetDeploymentTimeout: func() time.Duration { return time.Second },
		GetRegistryPort:      func() int { return 5000 },
		OperatorImageFor: func(*ExternalRegistryConfig, bool) string {
			rec.add("operator-image")
			return "registry.example.com/mcp-runtime-operator:latest"
		},
	}

	plan := SetupPlan{
		RegistryType:        "docker",
		RegistryStorageSize: "20Gi",
		Ingress: ingressOptions{
			mode:     "traefik",
			manifest: "config/ingress/overlays/prod",
			force:    false,
		},
		RegistryManifest: "config/registry/overlays/tls",
		TLSEnabled:       true,
		TestMode:         false,
	}

	if err := setupPlatformWithDeps(zap.NewNop(), plan, deps); err != nil {
		t.Fatalf("setupPlatformWithDeps returned error: %v", err)
	}

	if !rec.has("tls") {
		t.Fatalf("expected TLS setup to be called")
	}
	if !rec.has("login") {
		t.Fatalf("expected external registry login")
	}
	if rec.has("deploy-registry") || rec.has("registry-info") || rec.has("push-internal") {
		t.Fatalf("did not expect internal registry path")
	}
	if !rec.hasWait("mcp-runtime-operator-controller-manager") {
		t.Fatalf("expected operator wait")
	}
	if rec.hasWait("registry") {
		t.Fatalf("did not expect registry wait with external registry")
	}
}

func TestSetupPlatformWithDeps_DiagnosticsOnRegistryWaitFailure(t *testing.T) {
	rec := &callRecorder{}
	deps := SetupDeps{
		ResolveExternalRegistryConfig: func(*ExternalRegistryConfig) (*ExternalRegistryConfig, error) {
			return nil, nil
		},
		ClusterManager:  &fakeClusterManager{rec: rec},
		RegistryManager: &fakeRegistryManager{rec: rec},
		LoginRegistry: func(*zap.Logger, string, string, string) error {
			rec.add("login")
			return nil
		},
		DeployRegistry: func(*zap.Logger, string, int, string, string, string) error {
			rec.add("deploy-registry")
			return nil
		},
		WaitForDeploymentAvailable: func(_ *zap.Logger, name, _, _ string, _ time.Duration) error {
			rec.addWait(name)
			if name == "registry" {
				return fmt.Errorf("wait failed")
			}
			return nil
		},
		PrintDeploymentDiagnostics: func(string, string, string) { rec.add("diagnostics") },
		SetupTLS:                   func(*zap.Logger) error { return nil },
		BuildOperatorImage:         func(string) error { return nil },
		PushOperatorImage:          func(string) error { return nil },
		EnsureNamespace:            func(string) error { return nil },
		GetPlatformRegistryURL:     func(*zap.Logger) string { return "registry.local" },
		PushOperatorImageToInternal: func(*zap.Logger, string, string, string) error {
			return nil
		},
		DeployOperatorManifests:         func(*zap.Logger, string) error { return nil },
		ConfigureProvisionedRegistryEnv: func(*ExternalRegistryConfig, string) error { return nil },
		RestartDeployment:               func(string, string) error { return nil },
		CheckCRDInstalled:               func(string) error { return nil },
		GetDeploymentTimeout:            func() time.Duration { return time.Second },
		GetRegistryPort:                 func() int { return 5000 },
		OperatorImageFor:                func(*ExternalRegistryConfig, bool) string { return "registry.local/mcp-runtime-operator:latest" },
	}

	plan := SetupPlan{
		RegistryType:        "docker",
		RegistryStorageSize: "20Gi",
		Ingress: ingressOptions{
			mode:     "traefik",
			manifest: "config/ingress/overlays/http",
			force:    false,
		},
		RegistryManifest: "config/registry",
		TLSEnabled:       false,
		TestMode:         false,
	}

	if err := setupPlatformWithDeps(zap.NewNop(), plan, deps); err == nil {
		t.Fatalf("expected error from registry wait failure")
	}
	if !rec.has("diagnostics") {
		t.Fatalf("expected diagnostics to be printed on wait failure")
	}
}

func TestSetupPlatformWithDeps_DiagnosticsOnOperatorWaitFailure(t *testing.T) {
	rec := &callRecorder{}
	deps := SetupDeps{
		ResolveExternalRegistryConfig: func(*ExternalRegistryConfig) (*ExternalRegistryConfig, error) {
			return &ExternalRegistryConfig{URL: "registry.example.com"}, nil
		},
		ClusterManager:  &fakeClusterManager{rec: rec},
		RegistryManager: &fakeRegistryManager{rec: rec},
		LoginRegistry:   func(*zap.Logger, string, string, string) error { return nil },
		DeployRegistry:  func(*zap.Logger, string, int, string, string, string) error { return nil },
		WaitForDeploymentAvailable: func(_ *zap.Logger, name, _, _ string, _ time.Duration) error {
			rec.addWait(name)
			if name == "mcp-runtime-operator-controller-manager" {
				return fmt.Errorf("wait failed")
			}
			return nil
		},
		PrintDeploymentDiagnostics: func(string, string, string) { rec.add("diagnostics") },
		SetupTLS:                   func(*zap.Logger) error { return nil },
		BuildOperatorImage:         func(string) error { return nil },
		PushOperatorImage:          func(string) error { return nil },
		EnsureNamespace:            func(string) error { return nil },
		GetPlatformRegistryURL:     func(*zap.Logger) string { return "registry.local" },
		PushOperatorImageToInternal: func(*zap.Logger, string, string, string) error {
			return nil
		},
		DeployOperatorManifests:         func(*zap.Logger, string) error { return nil },
		ConfigureProvisionedRegistryEnv: func(*ExternalRegistryConfig, string) error { return nil },
		RestartDeployment:               func(string, string) error { return nil },
		CheckCRDInstalled:               func(string) error { return nil },
		GetDeploymentTimeout:            func() time.Duration { return time.Second },
		GetRegistryPort:                 func() int { return 5000 },
		OperatorImageFor:                func(*ExternalRegistryConfig, bool) string { return "registry.example.com/mcp-runtime-operator:latest" },
	}

	plan := SetupPlan{
		RegistryType:        "docker",
		RegistryStorageSize: "20Gi",
		Ingress: ingressOptions{
			mode:     "traefik",
			manifest: "config/ingress/overlays/http",
			force:    false,
		},
		RegistryManifest: "config/registry",
		TLSEnabled:       false,
		TestMode:         false,
	}

	if err := setupPlatformWithDeps(zap.NewNop(), plan, deps); err == nil {
		t.Fatalf("expected error from operator wait failure")
	}
	if !rec.has("diagnostics") {
		t.Fatalf("expected diagnostics to be printed on operator wait failure")
	}
	if rec.hasWait("registry") {
		t.Fatalf("did not expect registry wait with external registry")
	}
}

func TestSetupPlatformWithDeps_CRDCheckFailure(t *testing.T) {
	rec := &callRecorder{}
	deps := SetupDeps{
		ResolveExternalRegistryConfig: func(*ExternalRegistryConfig) (*ExternalRegistryConfig, error) {
			return &ExternalRegistryConfig{URL: "registry.example.com"}, nil
		},
		ClusterManager:  &fakeClusterManager{rec: rec},
		RegistryManager: &fakeRegistryManager{rec: rec},
		LoginRegistry:   func(*zap.Logger, string, string, string) error { return nil },
		DeployRegistry:  func(*zap.Logger, string, int, string, string, string) error { return nil },
		WaitForDeploymentAvailable: func(_ *zap.Logger, name, _, _ string, _ time.Duration) error {
			rec.addWait(name)
			return nil
		},
		PrintDeploymentDiagnostics: func(string, string, string) { rec.add("diagnostics") },
		SetupTLS:                   func(*zap.Logger) error { return nil },
		BuildOperatorImage:         func(string) error { return nil },
		PushOperatorImage:          func(string) error { return nil },
		EnsureNamespace:            func(string) error { return nil },
		GetPlatformRegistryURL:     func(*zap.Logger) string { return "registry.local" },
		PushOperatorImageToInternal: func(*zap.Logger, string, string, string) error {
			return nil
		},
		DeployOperatorManifests:         func(*zap.Logger, string) error { return nil },
		ConfigureProvisionedRegistryEnv: func(*ExternalRegistryConfig, string) error { return nil },
		RestartDeployment:               func(string, string) error { return nil },
		CheckCRDInstalled: func(string) error {
			return fmt.Errorf("crd missing")
		},
		GetDeploymentTimeout: func() time.Duration { return time.Second },
		GetRegistryPort:      func() int { return 5000 },
		OperatorImageFor:     func(*ExternalRegistryConfig, bool) string { return "registry.example.com/mcp-runtime-operator:latest" },
	}

	plan := SetupPlan{
		RegistryType:        "docker",
		RegistryStorageSize: "20Gi",
		Ingress: ingressOptions{
			mode:     "traefik",
			manifest: "config/ingress/overlays/http",
			force:    false,
		},
		RegistryManifest: "config/registry",
		TLSEnabled:       false,
		TestMode:         false,
	}

	if err := setupPlatformWithDeps(zap.NewNop(), plan, deps); err == nil {
		t.Fatalf("expected error from CRD check failure")
	}
	if rec.has("diagnostics") {
		t.Fatalf("did not expect diagnostics on CRD check failure")
	}
	if !rec.hasWait("mcp-runtime-operator-controller-manager") {
		t.Fatalf("expected operator wait before CRD check")
	}
}

func TestSetupPlatformWithDeps_InternalRegistryPushFailure(t *testing.T) {
	rec := &callRecorder{}
	deps := SetupDeps{
		ResolveExternalRegistryConfig: func(*ExternalRegistryConfig) (*ExternalRegistryConfig, error) {
			return nil, nil
		},
		ClusterManager:  &fakeClusterManager{rec: rec},
		RegistryManager: &fakeRegistryManager{rec: rec},
		LoginRegistry:   func(*zap.Logger, string, string, string) error { return nil },
		DeployRegistry:  func(*zap.Logger, string, int, string, string, string) error { return nil },
		WaitForDeploymentAvailable: func(_ *zap.Logger, name, _, _ string, _ time.Duration) error {
			rec.addWait(name)
			return nil
		},
		PrintDeploymentDiagnostics: func(string, string, string) { rec.add("diagnostics") },
		SetupTLS:                   func(*zap.Logger) error { return nil },
		BuildOperatorImage:         func(string) error { rec.add("build"); return nil },
		PushOperatorImage:          func(string) error { rec.add("push"); return nil },
		EnsureNamespace:            func(string) error { rec.add("ensure-ns"); return nil },
		GetPlatformRegistryURL:     func(*zap.Logger) string { return "registry.local" },
		PushOperatorImageToInternal: func(*zap.Logger, string, string, string) error {
			rec.add("push-internal")
			return fmt.Errorf("push failed")
		},
		DeployOperatorManifests:         func(*zap.Logger, string) error { rec.add("deploy-operator"); return nil },
		ConfigureProvisionedRegistryEnv: func(*ExternalRegistryConfig, string) error { return nil },
		RestartDeployment:               func(string, string) error { return nil },
		CheckCRDInstalled:               func(string) error { return nil },
		GetDeploymentTimeout:            func() time.Duration { return time.Second },
		GetRegistryPort:                 func() int { return 5000 },
		OperatorImageFor:                func(*ExternalRegistryConfig, bool) string { return "registry.local/mcp-runtime-operator:latest" },
	}

	plan := SetupPlan{
		RegistryType:        "docker",
		RegistryStorageSize: "20Gi",
		Ingress: ingressOptions{
			mode:     "traefik",
			manifest: "config/ingress/overlays/http",
			force:    false,
		},
		RegistryManifest: "config/registry",
		TLSEnabled:       false,
		TestMode:         false,
	}

	if err := setupPlatformWithDeps(zap.NewNop(), plan, deps); err == nil {
		t.Fatalf("expected error from internal registry push failure")
	}
	if rec.has("deploy-operator") {
		t.Fatalf("did not expect operator deploy after push failure")
	}
	if !rec.has("push-internal") {
		t.Fatalf("expected internal push attempt")
	}
}
