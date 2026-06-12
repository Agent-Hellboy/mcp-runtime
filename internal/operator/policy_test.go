package operator

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	mcpv1alpha1 "mcp-runtime/api/v1alpha1"
	"mcp-runtime/pkg/policy"
)

func TestRenderGatewayPolicyStampsAndValidates(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = mcpv1alpha1.AddToScheme(scheme)

	mcpServer := &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "payments", Namespace: "servers"},
		Spec: mcpv1alpha1.MCPServerSpec{
			Tools: []mcpv1alpha1.ToolConfig{
				{Name: "refund_invoice", SideEffect: mcpv1alpha1.ToolSideEffectWrite},
			},
		},
	}
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mcpServer).Build()
	r := MCPServerReconciler{Client: client, Scheme: scheme}

	doc, err := r.renderGatewayPolicy(context.Background(), mcpServer)
	if err != nil {
		t.Fatalf("renderGatewayPolicy() error = %v", err)
	}
	if doc.SchemaVersion != policy.SchemaVersion {
		t.Fatalf("SchemaVersion = %q, want %q", doc.SchemaVersion, policy.SchemaVersion)
	}
	if doc.Revision == "" || !strings.HasPrefix(doc.Revision, "sha256:") {
		t.Fatalf("Revision = %q, want sha256: digest", doc.Revision)
	}
	if doc.GeneratedAt != "" {
		t.Fatalf("GeneratedAt = %q, want empty (set at write time)", doc.GeneratedAt)
	}
	want, err := policy.ComputeRevision(doc)
	if err != nil {
		t.Fatalf("ComputeRevision() error = %v", err)
	}
	if doc.Revision != want {
		t.Fatalf("Revision = %q, want recomputed %q", doc.Revision, want)
	}
	if err := policy.Validate(doc); err != nil {
		t.Fatalf("rendered policy failed validation: %v", err)
	}
}

func TestRenderPolicyConfigMapDataPreservesUnchangedRevision(t *testing.T) {
	doc := &policy.Document{Server: policy.Server{Name: "demo"}}
	if err := policy.Stamp(doc, ""); err != nil {
		t.Fatalf("Stamp() error = %v", err)
	}

	// First write: no existing data, generated_at is stamped.
	first, err := renderPolicyConfigMapData("", doc)
	if err != nil {
		t.Fatalf("renderPolicyConfigMapData() error = %v", err)
	}
	var firstDoc policy.Document
	if err := json.Unmarshal([]byte(first), &firstDoc); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if firstDoc.GeneratedAt == "" {
		t.Fatal("expected generated_at to be set on fresh write")
	}

	// Re-render with the same content: payload must be preserved verbatim so an
	// unchanged policy does not churn the ConfigMap.
	second := &policy.Document{Server: policy.Server{Name: "demo"}}
	if err := policy.Stamp(second, ""); err != nil {
		t.Fatalf("Stamp() error = %v", err)
	}
	out, err := renderPolicyConfigMapData(first, second)
	if err != nil {
		t.Fatalf("renderPolicyConfigMapData() error = %v", err)
	}
	if out != first {
		t.Fatalf("unchanged revision rewrote payload:\n old=%s\n new=%s", first, out)
	}
}

func TestRenderPolicyConfigMapDataRewritesOnChange(t *testing.T) {
	doc := &policy.Document{Server: policy.Server{Name: "demo"}}
	_ = policy.Stamp(doc, "")
	existing, err := renderPolicyConfigMapData("", doc)
	if err != nil {
		t.Fatalf("renderPolicyConfigMapData() error = %v", err)
	}

	changed := &policy.Document{
		Server: policy.Server{Name: "demo"},
		Tools:  []policy.Tool{{Name: "echo", RequiredTrust: "low", SideEffect: "read"}},
	}
	_ = policy.Stamp(changed, "")
	out, err := renderPolicyConfigMapData(existing, changed)
	if err != nil {
		t.Fatalf("renderPolicyConfigMapData() error = %v", err)
	}
	if out == existing {
		t.Fatal("changed revision did not rewrite payload")
	}
	var outDoc policy.Document
	if err := json.Unmarshal([]byte(out), &outDoc); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if outDoc.Revision != changed.Revision {
		t.Fatalf("written revision = %q, want %q", outDoc.Revision, changed.Revision)
	}
}
