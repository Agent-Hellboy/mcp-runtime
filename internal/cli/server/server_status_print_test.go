package server

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/pterm/pterm"

	"mcp-runtime/internal/cli/core"
)

func TestServerStatus_printingAndKubectl(t *testing.T) {
	namespace := "mcp-servers"

	t.Run("returns-error-and-logs-combined-output-on-mcpserver-list-failure", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				if spec.Name == "kubectl" && contains(spec.Args, "mcpserver") {
					return &core.MockCommand{
						Args:       spec.Args,
						OutputData: []byte("boom-out\nboom-err\n"),
						OutputErr:  errors.New("kubectl failed"),
					}
				}
				return &core.MockCommand{}
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := newKubeTestServerManager(kubectl)

		var buf bytes.Buffer
		pterm.SetDefaultOutput(&buf)
		pterm.DisableStyling()
		setDefaultPrinterWriter(t, &buf)
		t.Cleanup(func() {
			pterm.SetDefaultOutput(os.Stdout)
			pterm.EnableStyling()
		})

		err := mgr.ServerStatus(namespace)
		if err == nil {
			t.Fatal("expected error from ServerStatus, got nil")
		}
		out := buf.String()
		if !strings.Contains(out, "boom-out") || !strings.Contains(out, "boom-err") {
			t.Fatalf("expected combined output to be logged, got output: %s", out)
		}
	})

	t.Run("prints warning when_no_servers_found", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				if spec.Name == "kubectl" && contains(spec.Args, "mcpserver") {
					return &core.MockCommand{Args: spec.Args, OutputData: []byte(""), OutputErr: nil}
				}
				return &core.MockCommand{}
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := newKubeTestServerManager(kubectl)

		var buf bytes.Buffer
		pterm.SetDefaultOutput(&buf)
		pterm.DisableStyling()
		setDefaultPrinterWriter(t, &buf)
		t.Cleanup(func() {
			pterm.SetDefaultOutput(os.Stdout)
			pterm.EnableStyling()
		})

		if err := mgr.ServerStatus(namespace); err != nil {
			t.Fatalf("ServerStatus unexpected error = %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "No MCP servers found in namespace "+namespace) {
			t.Fatalf("expected no servers warning, got output: %s", out)
		}
	})

	t.Run("uses-managed-by-label-when-listing-pods", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				cmd := &core.MockCommand{Args: spec.Args}
				if spec.Name == "kubectl" && contains(spec.Args, "mcpserver") {
					cmd.OutputData = []byte("server1|image:tag|1|/server|false\n")
				} else if spec.Name == "kubectl" && contains(spec.Args, "pods") {
					cmd.OutputData = []byte("NAME READY STATUS RESTARTS\npod-1 true Running 0\n")
				}
				return cmd
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := newKubeTestServerManager(kubectl)

		if err := mgr.ServerStatus(namespace); err != nil {
			t.Fatalf("ServerStatus unexpected error = %v", err)
		}

		found := false
		for _, c := range mock.Commands {
			if c.Name == "kubectl" && contains(c.Args, "pods") && contains(c.Args, core.SelectorManagedBy) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected managed-by label selector, got commands: %v", mock.Commands)
		}
	})

	t.Run("prints_no_pods_found_when_only_header_returned", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				cmd := &core.MockCommand{Args: spec.Args}
				if spec.Name == "kubectl" && contains(spec.Args, "mcpserver") {
					cmd.OutputData = []byte("server1|image:tag|1|/server|false\n")
				} else if spec.Name == "kubectl" && contains(spec.Args, "pods") {
					cmd.OutputData = []byte("NAME READY STATUS RESTARTS\n")
				}
				return cmd
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		mgr := newKubeTestServerManager(kubectl)

		var buf bytes.Buffer
		pterm.SetDefaultOutput(&buf)
		pterm.DisableStyling()
		setDefaultPrinterWriter(t, &buf)
		t.Cleanup(func() {
			pterm.SetDefaultOutput(os.Stdout)
			pterm.EnableStyling()
		})

		if err := mgr.ServerStatus(namespace); err != nil {
			t.Fatalf("ServerStatus unexpected error = %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "No pods found") {
			t.Fatalf("expected no pods message, got output: %s", out)
		}
	})
}
