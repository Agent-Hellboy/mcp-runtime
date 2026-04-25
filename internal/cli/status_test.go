package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/pterm/pterm"
	"go.uber.org/zap"
)

type commandResponse struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exitCode"`
}

func commandKey(name string, args ...string) string {
	return strings.Join(append([]string{name}, args...), " ")
}

func fakeExecCommand(t *testing.T, base func(string, ...string) *exec.Cmd, responses map[string]commandResponse, calls *[]string) func(string, ...string) *exec.Cmd {
	t.Helper()
	return func(name string, args ...string) *exec.Cmd {
		if calls != nil {
			*calls = append(*calls, commandKey(name, args...))
		}
		cmd := base(os.Args[0], "-test.run=TestHelperProcess", "--", name)
		cmd.Args = append(cmd.Args, args...)
		payload, err := json.Marshal(responses)
		if err != nil {
			t.Fatalf("failed to marshal responses: %v", err)
		}
		cmd.Env = append(os.Environ(),
			"GO_WANT_HELPER_PROCESS=1",
			"MCP_RUNTIME_TEST_COMMANDS="+string(payload),
		)
		return cmd
	}
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	raw := os.Getenv("MCP_RUNTIME_TEST_COMMANDS")
	if raw == "" {
		_, _ = os.Stderr.WriteString("missing MCP_RUNTIME_TEST_COMMANDS\n")
		os.Exit(1)
	}

	var responses map[string]commandResponse
	if err := json.Unmarshal([]byte(raw), &responses); err != nil {
		_, _ = os.Stderr.WriteString("invalid MCP_RUNTIME_TEST_COMMANDS\n")
		os.Exit(1)
	}

	args := os.Args
	sep := -1
	for i, arg := range args {
		if arg == "--" {
			sep = i
			break
		}
	}
	if sep == -1 || sep == len(args)-1 {
		_, _ = os.Stderr.WriteString("missing command args\n")
		os.Exit(1)
	}

	cmdArgs := args[sep+1:]
	key := strings.Join(cmdArgs, " ")
	response, ok := responses[key]
	if !ok {
		_, _ = os.Stderr.WriteString("unexpected command: " + key + "\n")
		os.Exit(1)
	}

	if response.Stdout != "" {
		_, _ = os.Stdout.WriteString(response.Stdout)
	}
	if response.Stderr != "" {
		_, _ = os.Stderr.WriteString(response.Stderr)
	}
	if response.ExitCode != 0 {
		os.Exit(response.ExitCode)
	}
	os.Exit(0)
}

func TestShowPlatformStatus(t *testing.T) {
	t.Run("marks-operator-pending-when-replicas-start-with-zero", func(t *testing.T) {
		resetStatusTestConfig(t)

		responses := map[string]commandResponse{
			commandKey("kubectl", "cluster-info"): {Stdout: "cluster ok\n"},
			commandKey("kubectl", "get", "deployment", "registry", "-n", "registry", "-o", "jsonpath={.status.readyReplicas}/{.spec.replicas}"): {
				Stdout: "1/1",
			},
			commandKey("kubectl", "get", "deployment", "mcp-runtime-operator-controller-manager", "-n", "mcp-runtime", "-o", "jsonpath={.status.readyReplicas}/{.spec.replicas}"): {
				Stdout: "0/1",
			},
			commandKey("kubectl", "get", "namespace", "mcp-sentinel", "-o", "jsonpath={.metadata.name}"): {
				Stdout:   "Error from server (NotFound): namespaces \"mcp-sentinel\" not found\n",
				ExitCode: 1,
			},
			commandKey("kubectl", "get", "mcpserver", "--all-namespaces", "-o", "custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name,IMAGE:.spec.image,REPLICAS:.spec.replicas,PATH:.spec.ingressPath"): {},
		}

		output := runShowPlatformStatus(t, responses)
		if !strings.Contains(output, "PENDING") {
			t.Fatalf("expected operator status to be PENDING, got output: %s", output)
		}
		if !strings.Contains(output, "Ready: 0/1") {
			t.Fatalf("expected operator replica details, got output: %s", output)
		}
		if !strings.Contains(output, "Analytics Stack") || !strings.Contains(output, "SKIPPED") {
			t.Fatalf("expected analytics stack to be reported as skipped, got output: %s", output)
		}
	})

	t.Run("shows-setup-hint-when-cluster-missing", func(t *testing.T) {
		resetStatusTestConfig(t)

		responses := map[string]commandResponse{
			commandKey("kubectl", "cluster-info"): {
				Stderr:   "exec: \"kubectl\": executable file not found in $PATH\n",
				ExitCode: 127,
			},
		}

		output := runShowPlatformStatus(t, responses)
		lower := strings.ToLower(output)
		if !strings.Contains(lower, "kubectl is missing") {
			t.Fatalf("expected kubectl missing hint when cluster missing, got output: %s", output)
		}
		if !strings.Contains(lower, "install kubectl") {
			t.Fatalf("expected install guidance in output, got: %s", output)
		}
	})

	t.Run("surfaces external registry config errors instead of falling back to in-cluster registry", func(t *testing.T) {
		resetStatusTestConfig(t)
		DefaultCLIConfig = &CLIConfig{ProvisionedRegistryUsername: "user-only"}

		var calls []string
		responses := map[string]commandResponse{
			commandKey("kubectl", "cluster-info"): {Stdout: "cluster ok\n"},
			commandKey("kubectl", "get", "deployment", "mcp-runtime-operator-controller-manager", "-n", "mcp-runtime", "-o", "jsonpath={.status.readyReplicas}/{.spec.replicas}"): {
				Stdout: "1/1",
			},
			commandKey("kubectl", "get", "namespace", "mcp-sentinel", "-o", "jsonpath={.metadata.name}"): {
				Stdout:   "Error from server (NotFound): namespaces \"mcp-sentinel\" not found\n",
				ExitCode: 1,
			},
			commandKey("kubectl", "get", "mcpserver", "--all-namespaces", "-o", "custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name,IMAGE:.spec.image,REPLICAS:.spec.replicas,PATH:.spec.ingressPath"): {},
		}

		output := runShowPlatformStatusWithCalls(t, responses, &calls)
		if !strings.Contains(output, "registry url is required") {
			t.Fatalf("expected registry config error in output, got: %s", output)
		}
		for _, call := range calls {
			if strings.Contains(call, "get deployment registry") {
				t.Fatalf("did not expect registry deployment probe when config is invalid, got calls: %v", calls)
			}
		}
	})

	t.Run("lists-analytics-services-when-installed", func(t *testing.T) {
		resetStatusTestConfig(t)
		var calls []string

		responses := map[string]commandResponse{
			commandKey("kubectl", "cluster-info"): {Stdout: "cluster ok\n"},
			commandKey("kubectl", "get", "deployment", "registry", "-n", "registry", "-o", "jsonpath={.status.readyReplicas}/{.spec.replicas}"): {
				Stdout: "1/1",
			},
			commandKey("kubectl", "get", "deployment", "mcp-runtime-operator-controller-manager", "-n", "mcp-runtime", "-o", "jsonpath={.status.readyReplicas}/{.spec.replicas}"): {
				Stdout: "1/1",
			},
			commandKey("kubectl", "get", "namespace", "mcp-sentinel", "-o", "jsonpath={.metadata.name}"): {
				Stdout: "mcp-sentinel",
			},
			commandKey("kubectl", "get", "statefulset", "clickhouse", "-n", "mcp-sentinel", "-o", "jsonpath={.status.readyReplicas}/{.spec.replicas}"): {
				Stdout: "1/1",
			},
			commandKey("kubectl", "get", "deployment", "zookeeper", "-n", "mcp-sentinel", "-o", "jsonpath={.status.readyReplicas}/{.spec.replicas}"): {
				Stdout: "1/1",
			},
			commandKey("kubectl", "get", "statefulset", "kafka", "-n", "mcp-sentinel", "-o", "jsonpath={.status.readyReplicas}/{.spec.replicas}"): {
				Stdout: "1/1",
			},
			commandKey("kubectl", "get", "deployment", "mcp-sentinel-ingest", "-n", "mcp-sentinel", "-o", "jsonpath={.status.readyReplicas}/{.spec.replicas}"): {
				Stdout: "2/2",
			},
			commandKey("kubectl", "get", "deployment", "mcp-sentinel-processor", "-n", "mcp-sentinel", "-o", "jsonpath={.status.readyReplicas}/{.spec.replicas}"): {
				Stdout: "1/1",
			},
			commandKey("kubectl", "get", "deployment", "mcp-sentinel-api", "-n", "mcp-sentinel", "-o", "jsonpath={.status.readyReplicas}/{.spec.replicas}"): {
				Stdout: "1/1",
			},
			commandKey("kubectl", "get", "deployment", "mcp-sentinel-ui", "-n", "mcp-sentinel", "-o", "jsonpath={.status.readyReplicas}/{.spec.replicas}"): {
				Stdout: "1/1",
			},
			commandKey("kubectl", "get", "deployment", "mcp-sentinel-gateway", "-n", "mcp-sentinel", "-o", "jsonpath={.status.readyReplicas}/{.spec.replicas}"): {
				Stdout: "1/1",
			},
			commandKey("kubectl", "get", "deployment", "prometheus", "-n", "mcp-sentinel", "-o", "jsonpath={.status.readyReplicas}/{.spec.replicas}"): {
				Stdout: "1/1",
			},
			commandKey("kubectl", "get", "deployment", "grafana", "-n", "mcp-sentinel", "-o", "jsonpath={.status.readyReplicas}/{.spec.replicas}"): {
				Stdout: "1/1",
			},
			commandKey("kubectl", "get", "deployment", "otel-collector", "-n", "mcp-sentinel", "-o", "jsonpath={.status.readyReplicas}/{.spec.replicas}"): {
				Stdout: "1/1",
			},
			commandKey("kubectl", "get", "statefulset", "tempo", "-n", "mcp-sentinel", "-o", "jsonpath={.status.readyReplicas}/{.spec.replicas}"): {
				Stdout: "1/1",
			},
			commandKey("kubectl", "get", "statefulset", "loki", "-n", "mcp-sentinel", "-o", "jsonpath={.status.readyReplicas}/{.spec.replicas}"): {
				Stdout: "1/1",
			},
			commandKey("kubectl", "get", "daemonset", "promtail", "-n", "mcp-sentinel", "-o", "jsonpath={.status.numberReady}/{.status.desiredNumberScheduled}"): {
				Stdout: "3/3",
			},
			commandKey("kubectl", "get", "mcpserver", "--all-namespaces", "-o", "custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name,IMAGE:.spec.image,REPLICAS:.spec.replicas,PATH:.spec.ingressPath"): {},
		}

		output := runShowPlatformStatusWithCalls(t, responses, &calls)
		for _, component := range []string{"ClickHouse", "Ingest", "Gateway", "Promtail"} {
			if !strings.Contains(output, component) {
				t.Fatalf("expected %s in output, got: %s", component, output)
			}
		}

		foundPromtail := false
		for _, call := range calls {
			if strings.Contains(call, "get daemonset promtail") {
				foundPromtail = true
				break
			}
		}
		if !foundPromtail {
			t.Fatalf("expected daemonset readiness check for promtail, got calls: %v", calls)
		}
	})
}

func TestAnalyticsNamespaceInstalledRequiresExactMatch(t *testing.T) {
	resetStatusTestConfig(t)

	responses := map[string]commandResponse{
		commandKey("kubectl", "get", "namespace", "mcp-sentinel", "-o", "jsonpath={.metadata.name}"): {
			Stdout: "unexpected-namespace",
		},
	}

	origExec := execCommand
	execCommand = fakeExecCommand(t, origExec, responses, nil)
	t.Cleanup(func() { execCommand = origExec })

	installed, err := analyticsNamespaceInstalled(true)
	if err != nil {
		t.Fatalf("analyticsNamespaceInstalled() unexpected error = %v", err)
	}
	if installed {
		t.Fatal("expected namespace check to fail on mismatched namespace name")
	}
}

func TestAnalyticsNamespaceInstalledReturnsErrorOnEmptyFailure(t *testing.T) {
	resetStatusTestConfig(t)

	responses := map[string]commandResponse{
		commandKey("kubectl", "get", "namespace", "mcp-sentinel", "-o", "jsonpath={.metadata.name}"): {
			ExitCode: 1,
		},
	}

	origExec := execCommand
	execCommand = fakeExecCommand(t, origExec, responses, nil)
	t.Cleanup(func() { execCommand = origExec })

	installed, err := analyticsNamespaceInstalled(true)
	if err == nil {
		t.Fatal("expected empty namespace probe failure to surface an error")
	}
	if installed {
		t.Fatal("expected namespace check to report not installed")
	}
	if !strings.Contains(err.Error(), "empty output from namespace probe") {
		t.Fatalf("expected empty-output error, got %v", err)
	}
}

func TestServerStatus(t *testing.T) {
	t.Run("returns-error-and-logs-combined-output-on-mcpserver-list-failure", func(t *testing.T) {
		logger := zap.NewNop()
		namespace := "mcp-servers"
		responses := map[string]commandResponse{
			commandKey("kubectl", "get", "mcpserver", "-n", namespace, "-o", "jsonpath={range .items[*]}{.metadata.name}|{.spec.image}:{.spec.imageTag}|{.spec.replicas}|{.spec.ingressPath}|{.spec.useProvisionedRegistry}{\"\\n\"}{end}"): {
				Stdout:   "boom-out\n",
				Stderr:   "boom-err\n",
				ExitCode: 1,
			},
		}

		origExec := execCommand
		execCommand = fakeExecCommand(t, origExec, responses, nil)
		t.Cleanup(func() { execCommand = origExec })

		var buf bytes.Buffer
		pterm.SetDefaultOutput(&buf)
		pterm.DisableStyling()
		setDefaultPrinterWriter(t, &buf)
		t.Cleanup(func() {
			pterm.SetDefaultOutput(os.Stdout)
			pterm.EnableStyling()
		})

		mgr := DefaultServerManager(logger)
		err := mgr.ServerStatus(namespace)
		if err == nil {
			t.Fatal("expected error from serverStatus, got nil")
		}

		output := buf.String()
		if !strings.Contains(output, "boom-out") || !strings.Contains(output, "boom-err") {
			t.Fatalf("expected combined output to be logged, got output: %s", output)
		}
	})

	t.Run("prints warning when no servers found", func(t *testing.T) {
		logger := zap.NewNop()
		namespace := "mcp-servers"
		responses := map[string]commandResponse{
			commandKey("kubectl", "get", "mcpserver", "-n", namespace, "-o", "jsonpath={range .items[*]}{.metadata.name}|{.spec.image}:{.spec.imageTag}|{.spec.replicas}|{.spec.ingressPath}|{.spec.useProvisionedRegistry}{\"\\n\"}{end}"): {},
		}

		origExec := execCommand
		execCommand = fakeExecCommand(t, origExec, responses, nil)
		t.Cleanup(func() { execCommand = origExec })

		var buf bytes.Buffer
		pterm.SetDefaultOutput(&buf)
		pterm.DisableStyling()
		setDefaultPrinterWriter(t, &buf)
		t.Cleanup(func() {
			pterm.SetDefaultOutput(os.Stdout)
			pterm.EnableStyling()
		})

		mgr := DefaultServerManager(logger)
		if err := mgr.ServerStatus(namespace); err != nil {
			t.Fatalf("serverStatus() unexpected error = %v", err)
		}

		output := buf.String()
		if !strings.Contains(output, "No MCP servers found in namespace "+namespace) {
			t.Fatalf("expected no servers warning, got output: %s", output)
		}
	})

	t.Run("uses-managed-by-label-when-listing-pods", func(t *testing.T) {
		logger := zap.NewNop()
		namespace := "mcp-servers"
		var calls []string

		responses := map[string]commandResponse{
			commandKey("kubectl", "get", "mcpserver", "-n", namespace, "-o", "jsonpath={range .items[*]}{.metadata.name}|{.spec.image}:{.spec.imageTag}|{.spec.replicas}|{.spec.ingressPath}|{.spec.useProvisionedRegistry}{\"\\n\"}{end}"): {
				Stdout: "server1|image:tag|1|/server|false\n",
			},
			commandKey("kubectl", "get", "pods", "-n", namespace, "-l", "app.kubernetes.io/managed-by=mcp-runtime", "-o", "custom-columns=NAME:.metadata.name,READY:.status.containerStatuses[0].ready,STATUS:.status.phase,RESTARTS:.status.containerStatuses[0].restartCount"): {
				Stdout: "NAME READY STATUS RESTARTS\npod-1 true Running 0\n",
			},
		}

		origExec := execCommand
		execCommand = fakeExecCommand(t, origExec, responses, &calls)
		t.Cleanup(func() { execCommand = origExec })

		mgr2 := DefaultServerManager(logger)
		if err := mgr2.ServerStatus(namespace); err != nil {
			t.Fatalf("serverStatus() unexpected error = %v", err)
		}

		found := false
		for _, call := range calls {
			if strings.Contains(call, "get pods") && strings.Contains(call, "app.kubernetes.io/managed-by=mcp-runtime") {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected managed-by label selector, got calls: %v", calls)
		}
	})

	t.Run("prints no pods found when only header returned", func(t *testing.T) {
		logger := zap.NewNop()
		namespace := "mcp-servers"
		responses := map[string]commandResponse{
			commandKey("kubectl", "get", "mcpserver", "-n", namespace, "-o", "jsonpath={range .items[*]}{.metadata.name}|{.spec.image}:{.spec.imageTag}|{.spec.replicas}|{.spec.ingressPath}|{.spec.useProvisionedRegistry}{\"\\n\"}{end}"): {
				Stdout: "server1|image:tag|1|/server|false\n",
			},
			commandKey("kubectl", "get", "pods", "-n", namespace, "-l", "app.kubernetes.io/managed-by=mcp-runtime", "-o", "custom-columns=NAME:.metadata.name,READY:.status.containerStatuses[0].ready,STATUS:.status.phase,RESTARTS:.status.containerStatuses[0].restartCount"): {
				Stdout: "NAME READY STATUS RESTARTS\n",
			},
		}

		origExec := execCommand
		execCommand = fakeExecCommand(t, origExec, responses, nil)
		t.Cleanup(func() { execCommand = origExec })

		var buf bytes.Buffer
		pterm.SetDefaultOutput(&buf)
		pterm.DisableStyling()
		setDefaultPrinterWriter(t, &buf)
		t.Cleanup(func() {
			pterm.SetDefaultOutput(os.Stdout)
			pterm.EnableStyling()
		})

		mgr := DefaultServerManager(logger)
		if err := mgr.ServerStatus(namespace); err != nil {
			t.Fatalf("serverStatus() unexpected error = %v", err)
		}

		output := buf.String()
		if !strings.Contains(output, "No pods found") {
			t.Fatalf("expected no pods message, got output: %s", output)
		}
	})
}

func TestNewStatusCmd(t *testing.T) {
	logger := zap.NewNop()
	cmd := NewStatusCmd(logger)

	t.Run("command_created", func(t *testing.T) {
		if cmd == nil {
			t.Fatal("NewStatusCmd should not return nil")
		}
		if cmd.Use != "status" {
			t.Errorf("expected Use='status', got %q", cmd.Use)
		}
		if cmd.Short == "" {
			t.Error("expected Short description to be set")
		}
	})

	t.Run("has_runE", func(t *testing.T) {
		if cmd.RunE == nil {
			t.Error("expected RunE to be set")
		}
	})
}

func setDefaultPrinterWriter(t *testing.T, w *bytes.Buffer) {
	t.Helper()
	orig := DefaultPrinter.Writer
	DefaultPrinter.Writer = w
	t.Cleanup(func() {
		DefaultPrinter.Writer = orig
	})
}

func resetStatusTestConfig(t *testing.T) {
	t.Helper()
	orig := DefaultCLIConfig
	DefaultCLIConfig = &CLIConfig{}
	t.Cleanup(func() {
		DefaultCLIConfig = orig
	})
	t.Setenv("HOME", t.TempDir())
}

func runShowPlatformStatus(t *testing.T, responses map[string]commandResponse) string {
	t.Helper()
	return runShowPlatformStatusWithCalls(t, responses, nil)
}

func runShowPlatformStatusWithCalls(t *testing.T, responses map[string]commandResponse, calls *[]string) string {
	t.Helper()

	logger := zap.NewNop()
	origExec := execCommand
	execCommand = fakeExecCommand(t, origExec, responses, calls)
	t.Cleanup(func() { execCommand = origExec })

	var buf bytes.Buffer
	pterm.SetDefaultOutput(&buf)
	pterm.DisableStyling()
	setDefaultPrinterWriter(t, &buf)
	t.Cleanup(func() {
		pterm.SetDefaultOutput(os.Stdout)
		pterm.EnableStyling()
	})

	if err := showPlatformStatus(logger); err != nil {
		t.Fatalf("showPlatformStatus() unexpected error = %v", err)
	}

	return buf.String()
}
