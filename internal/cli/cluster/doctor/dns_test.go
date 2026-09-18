package doctor

import "testing"

func TestDoctorServiceDNSUsesConfiguredClusterDomain(t *testing.T) {
	t.Setenv("MCP_CLUSTER_DOMAIN", "corp.example.")
	if got, want := doctorServiceDNS("mcp-runtime-api", "mcp-sentinel"), "mcp-runtime-api.mcp-sentinel.svc.corp.example"; got != want {
		t.Fatalf("service DNS = %q, want %q", got, want)
	}
}

func TestDoctorServiceDNSDefaultsToClusterLocal(t *testing.T) {
	t.Setenv("MCP_CLUSTER_DOMAIN", "")
	if got, want := doctorServiceDNS("registry", "registry"), "registry.registry.svc.cluster.local"; got != want {
		t.Fatalf("service DNS = %q, want %q", got, want)
	}
}
