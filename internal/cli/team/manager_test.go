package team

import (
	"strings"
	"testing"

	"go.uber.org/zap"
)

func TestInitTeamRejected(t *testing.T) {
	mgr := NewManager(zap.NewNop())

	err := mgr.InitTeam(InitOptions{Slug: "acme"})
	if err == nil {
		t.Fatal("expected team init rejection")
	}
	if !strings.Contains(err.Error(), "team create") {
		t.Fatalf("expected team create guidance, got %v", err)
	}
}
