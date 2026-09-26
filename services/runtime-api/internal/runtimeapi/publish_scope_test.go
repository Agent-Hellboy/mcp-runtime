package runtimeapi

import (
	"strings"
	"testing"

	"mcp-runtime/pkg/publishscope"
)

// Registry push and server deploy must agree on which scopes a platform
// accepts, so an image is never published to a scope it cannot deploy from.
func TestPublishScopeConsistentAcrossPushAndDeploy(t *testing.T) {
	admin := principal{Role: roleAdmin}
	cases := []struct {
		mode    string
		scope   publishscope.Scope
		enabled bool
	}{
		{mode: "tenant", scope: publishscope.Org, enabled: false},
		{mode: "tenant", scope: publishscope.Public, enabled: false},
		{mode: "org", scope: publishscope.Org, enabled: true},
		{mode: "org", scope: publishscope.Public, enabled: false},
		{mode: "public", scope: publishscope.Public, enabled: true},
		{mode: "public", scope: publishscope.Org, enabled: false},
	}
	for _, tc := range cases {
		t.Run(tc.mode+"/"+string(tc.scope), func(t *testing.T) {
			t.Setenv("PLATFORM_MODE", tc.mode)
			_, _, pushErr := registryPushAuthContext("registry.example.com/"+string(tc.scope)+"/demo:v1", tc.scope, admin)
			scopeErr := publishScopeEnabledError(tc.scope)
			if tc.enabled {
				if pushErr != nil || scopeErr != nil {
					t.Fatalf("push err=%v scope err=%v, want enabled", pushErr, scopeErr)
				}
				return
			}
			if pushErr == nil {
				t.Fatalf("registry push accepted %s scope in %s mode, but deploy rejects it", tc.scope, tc.mode)
			}
			if scopeErr == nil || pushErr.Error() != scopeErr.Error() {
				t.Fatalf("push err=%v, deploy err=%v; want the same error", pushErr, scopeErr)
			}
		})
	}
}

func TestPublishScopeEnabledErrorIsActionable(t *testing.T) {
	t.Setenv("PLATFORM_MODE", "tenant")
	err := publishScopeEnabledError(publishscope.Org)
	if err == nil {
		t.Fatal("expected org scope to be disabled in tenant mode")
	}
	msg := err.Error()
	for _, want := range []string{"org scope is not enabled on this platform", `platform mode "tenant"`, "enabled scopes: tenant", "--scope tenant", "omit --scope"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q does not mention %q", msg, want)
		}
	}
	if err := publishScopeEnabledError(publishscope.Tenant); err != nil {
		t.Fatalf("tenant scope must always be enabled: %v", err)
	}
	if err := publishScopeEnabledError(""); err != nil {
		t.Fatalf("empty scope must be accepted: %v", err)
	}
}
