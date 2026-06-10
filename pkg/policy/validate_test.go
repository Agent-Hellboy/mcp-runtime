package policy

import (
	"strings"
	"testing"
)

func TestValidateAcceptsWellFormedDocument(t *testing.T) {
	doc := &Document{
		SchemaVersion: SchemaVersion,
		Revision:      "sha256:abc",
		Server:        Server{Name: "demo", Namespace: "mcp-servers"},
		Auth:          &Auth{Mode: "oauth", IssuerURL: "https://issuer.example.com"},
		Policy:        &Config{Mode: "allow-list", DefaultDecision: "deny"},
		Tools: []Tool{
			{Name: "read-file", RequiredTrust: "low", SideEffect: "read"},
			{Name: "write-file", RequiredTrust: "high", SideEffect: "write"},
		},
		Grants: []Grant{{
			Name:               "dev",
			HumanID:            "user@example.com",
			MaxTrust:           "high",
			AllowedSideEffects: []string{"read", "write"},
			ToolRules:          []ToolAccess{{Name: "read-file", Decision: "allow", RequiredTrust: "low"}},
		}},
		Sessions: []Binding{{Name: "s1", HumanID: "user@example.com", ConsentedTrust: "high"}},
	}
	if err := Validate(doc); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestValidateRejects(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Document)
		wantSub string
	}{
		{"nil schema version", func(d *Document) { d.SchemaVersion = "" }, "schema version"},
		{"unsupported schema version", func(d *Document) { d.SchemaVersion = "v999" }, "schema version"},
		{"missing server name", func(d *Document) { d.Server.Name = "" }, "server.name"},
		{"invalid auth mode", func(d *Document) { d.Auth = &Auth{Mode: "saml"} }, "auth mode"},
		{"oauth without issuer", func(d *Document) { d.Auth = &Auth{Mode: "oauth"} }, "issuer_url"},
		{"invalid policy mode", func(d *Document) { d.Policy = &Config{Mode: "deny-everything"} }, "policy mode"},
		{"invalid default decision", func(d *Document) { d.Policy = &Config{DefaultDecision: "maybe"} }, "default decision"},
		{"invalid tool trust", func(d *Document) { d.Tools = []Tool{{Name: "t", RequiredTrust: "ultra"}} }, "required_trust"},
		{"invalid tool side effect", func(d *Document) { d.Tools = []Tool{{Name: "t", SideEffect: "explode"}} }, "side_effect"},
		{"duplicate tool", func(d *Document) { d.Tools = []Tool{{Name: "t"}, {Name: "t"}} }, "duplicate tool"},
		{"empty tool name", func(d *Document) { d.Tools = []Tool{{Name: ""}} }, "name is required"},
		{"duplicate grant", func(d *Document) { d.Grants = []Grant{{Name: "g"}, {Name: "g"}} }, "duplicate grant"},
		{"invalid grant trust", func(d *Document) { d.Grants = []Grant{{Name: "g", MaxTrust: "extreme"}} }, "max_trust"},
		{"invalid allowed side effect", func(d *Document) {
			d.Grants = []Grant{{Name: "g", AllowedSideEffects: []string{"boom"}}}
		}, "allowed side effect"},
		{"invalid tool rule decision", func(d *Document) {
			d.Grants = []Grant{{Name: "g", ToolRules: []ToolAccess{{Name: "t", Decision: "perhaps"}}}}
		}, "decision"},
		{"duplicate session", func(d *Document) { d.Sessions = []Binding{{Name: "s"}, {Name: "s"}} }, "duplicate session"},
		{"invalid consented trust", func(d *Document) {
			d.Sessions = []Binding{{Name: "s", ConsentedTrust: "godmode"}}
		}, "consented_trust"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := &Document{
				SchemaVersion: SchemaVersion,
				Server:        Server{Name: "demo"},
			}
			tc.mutate(doc)
			err := Validate(doc)
			if err == nil {
				t.Fatalf("Validate() error = nil, want error containing %q", tc.wantSub)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("Validate() error = %q, want substring %q", err.Error(), tc.wantSub)
			}
		})
	}
}

func TestValidateNilDocument(t *testing.T) {
	if err := Validate(nil); err == nil {
		t.Fatal("Validate(nil) error = nil, want error")
	}
}
