package team

import (
	"io"
	"strings"
	"testing"

	"go.uber.org/zap"

	"mcp-runtime/internal/cli/core"
)

func TestInitTeamAppliesNamespaceRBACAndPatchesTraefik(t *testing.T) {
	traefikJSON := `{
	  "spec": {
	    "template": {
	      "spec": {
	        "containers": [{
	          "name": "traefik",
	          "args": [
	            "--providers.kubernetesingress=true",
	            "--providers.kubernetesingress.namespaces=registry,mcp-sentinel,mcp-servers"
	          ]
	        }]
	      }
	    }
	  }
	}`
	var applyCmd *core.MockCommand
	var patchArgs []string
	mock := &core.MockExecutor{
		CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
			cmd := &core.MockCommand{Args: spec.Args}
			if len(spec.Args) >= 2 && spec.Args[0] == "apply" {
				applyCmd = cmd
			}
			if len(spec.Args) >= 3 && spec.Args[0] == "get" && spec.Args[1] == "deployment" {
				cmd.OutputData = []byte(traefikJSON)
			}
			if len(spec.Args) >= 3 && spec.Args[0] == "patch" && spec.Args[1] == "deployment" {
				patchArgs = append([]string(nil), spec.Args...)
			}
			return cmd
		},
	}
	mgr := NewManagerWithKubectl(zap.NewNop(), core.NewTestKubectlClient(mock))

	if err := mgr.InitTeam(InitOptions{Slug: "acme"}); err != nil {
		t.Fatalf("InitTeam() error = %v", err)
	}
	if applyCmd == nil {
		t.Fatal("expected kubectl apply command")
	}
	manifestBytes, err := io.ReadAll(applyCmd.StdinR)
	if err != nil {
		t.Fatalf("read apply stdin: %v", err)
	}
	manifest := string(manifestBytes)
	for _, want := range []string{
		"name: mcp-team-acme",
		"mcpruntime.org/team-slug: acme",
		"name: mcp-workload",
		"automountServiceAccountToken: false",
		"name: platform-default-quota",
		"name: platform-default-limits",
		"name: platform-default-deny",
		"kubernetes.io/metadata.name: traefik",
		"name: mcp-runtime-team-admin",
		"name: acme-mcp-runtime-admins",
		`name: "acme-mcp-admins"`,
		"name: traefik-watch",
		"namespace: traefik",
	} {
		if !strings.Contains(manifest, want) {
			t.Fatalf("expected manifest to contain %q, got:\n%s", want, manifest)
		}
	}
	if len(patchArgs) == 0 {
		t.Fatal("expected Traefik deployment patch")
	}
	patch := strings.Join(patchArgs, " ")
	for _, want := range []string{"patch deployment traefik", "-n traefik", "--type=json", "mcp-team-acme"} {
		if !strings.Contains(patch, want) {
			t.Fatalf("expected patch args to contain %q, got %v", want, patchArgs)
		}
	}
}

func TestInitTeamDryRunDoesNotRunKubectl(t *testing.T) {
	mock := &core.MockExecutor{}
	mgr := NewManagerWithKubectl(zap.NewNop(), core.NewTestKubectlClient(mock))

	if err := mgr.InitTeam(InitOptions{Slug: "acme", DryRun: true}); err != nil {
		t.Fatalf("InitTeam() error = %v", err)
	}
	if len(mock.Commands) != 0 {
		t.Fatalf("expected no kubectl commands during dry run, got %#v", mock.Commands)
	}
}

func TestInitTeamRejectsReservedNamespace(t *testing.T) {
	mgr := NewManagerWithKubectl(zap.NewNop(), core.NewTestKubectlClient(&core.MockExecutor{}))

	err := mgr.InitTeam(InitOptions{Slug: "acme", Namespace: "mcp-servers"})
	if err == nil {
		t.Fatal("expected reserved namespace error")
	}
	if !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("expected reserved namespace error, got %v", err)
	}
}
