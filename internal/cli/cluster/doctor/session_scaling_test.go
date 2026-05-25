package doctor

import (
	"strings"
	"testing"

	"mcp-runtime/internal/cli/core"
)

func TestCheckSessionLocalDeploymentScaling(t *testing.T) {
	t.Run("passes when ui and gateway are single replica", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				switch {
				case contains(spec.Args, "namespace"):
					return &core.MockCommand{OutputData: []byte(doctorSentinelNamespace)}
				case contains(spec.Args, "mcp-sentinel-ui"):
					return &core.MockCommand{OutputData: []byte("1")}
				case contains(spec.Args, "mcp-sentinel-gateway"):
					return &core.MockCommand{OutputData: []byte("1")}
				default:
					return &core.MockCommand{}
				}
			},
		}
		check := checkSessionLocalDeploymentScaling(core.NewTestKubectlClient(mock))
		if !check.OK {
			t.Fatalf("expected ok check, got detail=%q remedy=%q", check.Detail, check.Remedy)
		}
	})

	t.Run("fails when ui is scaled above one replica", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				switch {
				case contains(spec.Args, "namespace"):
					return &core.MockCommand{OutputData: []byte(doctorSentinelNamespace)}
				case contains(spec.Args, "mcp-sentinel-ui"):
					return &core.MockCommand{OutputData: []byte("3")}
				case contains(spec.Args, "mcp-sentinel-gateway"):
					return &core.MockCommand{OutputData: []byte("1")}
				default:
					return &core.MockCommand{}
				}
			},
		}
		check := checkSessionLocalDeploymentScaling(core.NewTestKubectlClient(mock))
		if check.OK {
			t.Fatal("expected failure when ui replicas > 1")
		}
		if !strings.Contains(check.Detail, "mcp-sentinel-ui") {
			t.Fatalf("detail = %q, want ui deployment called out", check.Detail)
		}
	})
}

func TestParseDoctorReplicaCount(t *testing.T) {
	got, err := parseDoctorReplicaCount(" 3 ")
	if err != nil || got != 3 {
		t.Fatalf("parseDoctorReplicaCount() = (%d, %v), want (3, nil)", got, err)
	}
}
