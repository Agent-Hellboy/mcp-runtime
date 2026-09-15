package access

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"

	"mcp-runtime/internal/cli/core"
	"mcp-runtime/pkg/policy"
)

func TestExplainAccessJSONIncludesDecisionAttribution(t *testing.T) {
	doc := explainTestPolicy(t, "allow")
	file := writeExplainPolicy(t, doc)
	mgr := NewAccessManager(core.NewTestKubectlClient(&core.MockExecutor{}), zap.NewNop())

	var out bytes.Buffer
	err := mgr.ExplainAccess(explainOptions{
		Server:     "workspace-assistant",
		Namespace:  "mcp-servers",
		HumanID:    "alice",
		Tool:       "write-file",
		RPCMethod:  "tools/call",
		PolicyFile: file,
		JSON:       true,
	}, &out)
	if err != nil {
		t.Fatalf("ExplainAccess() error = %v", err)
	}

	var got explainOutput
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("JSON output is invalid: %v\n%s", err, out.String())
	}
	if got.Decision != "allow" || got.Status != 200 || got.Reason != "allowed" {
		t.Fatalf("decision = %#v, want allow/200/allowed", got)
	}
	if got.MatchedGrant != "developer" || got.MatchedRule == nil || got.MatchedRule.Name != "write-file" {
		t.Fatalf("attribution = %#v, want developer/write-file", got)
	}
	if got.PolicyRevision != doc.Revision {
		t.Fatalf("policy revision = %q, want %q", got.PolicyRevision, doc.Revision)
	}
}

func TestExplainAccessDenyReturnsExitErrorAfterPrinting(t *testing.T) {
	doc := explainTestPolicy(t, "deny")
	file := writeExplainPolicy(t, doc)
	mgr := NewAccessManager(core.NewTestKubectlClient(&core.MockExecutor{}), zap.NewNop())

	var out bytes.Buffer
	err := mgr.ExplainAccess(explainOptions{
		Server:     "workspace-assistant",
		Namespace:  "mcp-servers",
		HumanID:    "alice",
		Tool:       "write-file",
		RPCMethod:  "tools/call",
		PolicyFile: file,
	}, &out)
	if err == nil || !strings.Contains(err.Error(), "policy denied: tool_denied") {
		t.Fatalf("ExplainAccess() error = %v, want policy denial", err)
	}
	for _, want := range []string{"decision: deny (403)", "reason: tool_denied", "matched grant: mcp-servers/developer", "matched rule: write-file -> deny"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("human output missing %q:\n%s", want, out.String())
		}
	}
}

func TestExplainAccessRequiresIdentityAndTool(t *testing.T) {
	mgr := NewAccessManager(core.NewTestKubectlClient(&core.MockExecutor{}), zap.NewNop())

	err := mgr.ExplainAccess(explainOptions{Server: "workspace-assistant", Namespace: "mcp-servers", RPCMethod: "tools/call"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "one of --human, --agent, or --team is required for tools/call") {
		t.Fatalf("identity validation error = %v", err)
	}

	err = mgr.ExplainAccess(explainOptions{Server: "workspace-assistant", Namespace: "mcp-servers", HumanID: "alice", RPCMethod: "tools/call"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "--tool is required") {
		t.Fatalf("tool validation error = %v", err)
	}
}

func explainTestPolicy(t *testing.T, ruleDecision string) *policy.Document {
	t.Helper()
	doc := &policy.Document{
		Server: policy.Server{Name: "workspace-assistant", Namespace: "mcp-servers"},
		Policy: &policy.Config{Mode: "allow-list", DefaultDecision: "deny", PolicyVersion: "v1"},
		Tools:  []policy.Tool{{Name: "write-file", RequiredTrust: "low", SideEffect: "read"}},
		Grants: []policy.Grant{{
			Name:               "developer",
			Namespace:          "mcp-servers",
			HumanID:            "alice",
			MaxTrust:           "high",
			AllowedSideEffects: []string{"read"},
			ToolRules:          []policy.ToolAccess{{Name: "write-file", Decision: ruleDecision}},
		}},
	}
	if err := policy.Stamp(doc, "2026-09-15T00:00:00Z"); err != nil {
		t.Fatalf("policy.Stamp() error = %v", err)
	}
	return doc
}

func writeExplainPolicy(t *testing.T, doc *policy.Document) string {
	t.Helper()
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	path := filepath.Join(t.TempDir(), "policy.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}
