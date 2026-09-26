package platformapi

import (
	"net/http"
	"strings"
	"testing"
)

func TestRegistryPushHTTPErrorExplainsProxyFailures(t *testing.T) {
	err := registryPushHTTPError(http.StatusBadGateway, []byte("Bad Gateway"))
	msg := err.Error()
	for _, want := range []string{"API 502: Bad Gateway", "mcp-runtime-api", "MCP_REGISTRY_PUSH_UPLOAD_TIMEOUT", "/api/v1/runtime/registry/push"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q does not mention %q", msg, want)
		}
	}
}

func TestRegistryPushHTTPErrorKeepsRuntimeAPIMessage(t *testing.T) {
	err := registryPushHTTPError(http.StatusRequestTimeout, []byte(`{"error":"image upload did not finish within 20m0s"}`))
	if got := err.Error(); got != "API 408: image upload did not finish within 20m0s" {
		t.Fatalf("error = %q", got)
	}
	err = registryPushHTTPError(http.StatusBadGateway, []byte(`{"error":"registry push failed"}`))
	if strings.Contains(err.Error(), "proxy") {
		t.Fatalf("a JSON runtime API error must be surfaced as-is, got %q", err.Error())
	}
}
