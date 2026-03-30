package access

import (
	"testing"

	mcpv1alpha1 "mcp-runtime/api/v1alpha1"
)

func TestManagerUsesRuntimeCRDGroup(t *testing.T) {
	if APIGroup != mcpv1alpha1.GroupVersion.Group {
		t.Fatalf("APIGroup = %q, want %q", APIGroup, mcpv1alpha1.GroupVersion.Group)
	}
	if APIVersion != mcpv1alpha1.GroupVersion.Version {
		t.Fatalf("APIVersion = %q, want %q", APIVersion, mcpv1alpha1.GroupVersion.Version)
	}
	if grantGVR.Group != APIGroup || sessionGVR.Group != APIGroup {
		t.Fatalf("expected grant/session GVRs to use APIGroup %q, got %q and %q", APIGroup, grantGVR.Group, sessionGVR.Group)
	}
}
