package platform

import (
	"testing"

	"mcp-runtime/internal/cli/certmanager"
	"mcp-runtime/internal/cli/core"
	setupplan "mcp-runtime/internal/cli/setup/plan"
)

func TestApplySetupPlanToCLIConfig_TLSClusterIssuer(t *testing.T) {
	orig := core.DefaultCLIConfig
	t.Cleanup(func() { core.DefaultCLIConfig = orig })
	core.DefaultCLIConfig = &core.CLIConfig{RegistryClusterIssuerName: "unset"}

	applySetupPlanToCLIConfig(setupplan.Plan{TLSEnabled: true, TLSClusterIssuer: "internal-ca", ACMEmail: ""})
	if core.GetRegistryClusterIssuerName() != "internal-ca" {
		t.Fatalf("expected custom issuer, got %q", core.GetRegistryClusterIssuerName())
	}

	applySetupPlanToCLIConfig(setupplan.Plan{TLSEnabled: true, TLSClusterIssuer: "ignored", ACMEmail: "ops@mcpruntime.com"})
	if want := certmanager.ClusterIssuerNameForACME(false); core.GetRegistryClusterIssuerName() != want {
		t.Fatalf("expected ACME issuer to take precedence, got %q", core.GetRegistryClusterIssuerName())
	}

	applySetupPlanToCLIConfig(setupplan.Plan{TLSEnabled: false})
	if core.GetRegistryClusterIssuerName() != "" {
		t.Fatalf("expected cleared when TLS off, got %q", core.GetRegistryClusterIssuerName())
	}
}
