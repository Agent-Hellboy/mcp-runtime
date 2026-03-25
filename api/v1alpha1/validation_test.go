package v1alpha1

import (
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMCPAccessGrantValidateRequiresToolDecision(t *testing.T) {
	grant := &MCPAccessGrant{
		ObjectMeta: metav1.ObjectMeta{Name: "grant"},
		Spec: MCPAccessGrantSpec{
			ServerRef: ServerReference{Name: "payments"},
			Subject:   SubjectRef{HumanID: "user-1"},
			ToolRules: []ToolRule{
				{Name: "refund_invoice"},
			},
		},
	}

	err := grant.validate()
	if err == nil {
		t.Fatal("expected validation error for missing tool rule decision")
	}
	if !strings.Contains(err.Error(), "toolRules[0].decision") {
		t.Fatalf("expected decision validation error, got %v", err)
	}
}

func TestMCPAgentSessionValidateUsesInjectedTimeSource(t *testing.T) {
	fixedNow := time.Date(2026, time.March, 25, 12, 0, 0, 0, time.UTC)
	originalNowFunc := nowFunc
	nowFunc = func() time.Time { return fixedNow }
	t.Cleanup(func() {
		nowFunc = originalNowFunc
	})

	session := &MCPAgentSession{
		ObjectMeta: metav1.ObjectMeta{Name: "session"},
		Spec: MCPAgentSessionSpec{
			ServerRef:      ServerReference{Name: "payments"},
			Subject:        SubjectRef{AgentID: "ops-agent"},
			ConsentedTrust: TrustLevelMedium,
			ExpiresAt:      &metav1.Time{Time: fixedNow},
		},
	}

	err := session.validate()
	if err == nil {
		t.Fatal("expected validation error for expired session")
	}
	if !strings.Contains(err.Error(), "expiresAt") {
		t.Fatalf("expected expiresAt validation error, got %v", err)
	}
}
