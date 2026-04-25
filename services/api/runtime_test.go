package main

import (
	"strings"
	"testing"

	sentinelaccess "mcp-runtime/pkg/access"
)

func TestValidateGrantRequestDefaultsAndNormalizes(t *testing.T) {
	req := &accessGrantRequest{
		Name: " grant-a ",
		ServerRef: sentinelaccess.ServerReference{
			Name: " demo ",
		},
		Subject: sentinelaccess.SubjectRef{
			HumanID: " user-1 ",
		},
		MaxTrust: sentinelaccess.TrustLevel(" high "),
		ToolRules: []sentinelaccess.ToolRule{
			{Name: " aaa-ping ", Decision: sentinelaccess.PolicyDecision(" allow ")},
		},
	}

	if err := validateGrantRequest(req); err != nil {
		t.Fatalf("validateGrantRequest returned error: %v", err)
	}
	if req.Name != "grant-a" {
		t.Fatalf("Name = %q, want grant-a", req.Name)
	}
	if req.Namespace != "mcp-servers" {
		t.Fatalf("Namespace = %q, want mcp-servers", req.Namespace)
	}
	if req.PolicyVersion != "v1" {
		t.Fatalf("PolicyVersion = %q, want v1", req.PolicyVersion)
	}
	if req.ToolRules[0].Name != "aaa-ping" || req.ToolRules[0].Decision != "allow" {
		t.Fatalf("tool rule was not normalized: %#v", req.ToolRules[0])
	}
}

func TestValidateGrantRequestRejectsInvalidToolRule(t *testing.T) {
	req := &accessGrantRequest{
		Name:      "grant-a",
		ServerRef: sentinelaccess.ServerReference{Name: "demo"},
		Subject:   sentinelaccess.SubjectRef{HumanID: "user-1"},
		ToolRules: []sentinelaccess.ToolRule{
			{Name: "aaa-ping", Decision: sentinelaccess.PolicyDecision("audit")},
		},
	}

	err := validateGrantRequest(req)
	if err == nil || !strings.Contains(err.Error(), "decision must be allow or deny") {
		t.Fatalf("validateGrantRequest error = %v, want invalid decision", err)
	}
}

func TestValidateSessionRequestRequiresSubject(t *testing.T) {
	req := &accessSessionRequest{
		Name:           "session-a",
		ServerRef:      sentinelaccess.ServerReference{Name: "demo"},
		ConsentedTrust: sentinelaccess.TrustLevel("low"),
	}

	err := validateSessionRequest(req)
	if err == nil || !strings.Contains(err.Error(), "either subject.humanID or subject.agentID is required") {
		t.Fatalf("validateSessionRequest error = %v, want subject requirement", err)
	}
}
