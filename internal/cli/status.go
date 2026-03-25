package cli

// This file implements the "status" command for checking platform health.
// It summarizes the core runtime plus the bundled analytics stack.

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

type platformWorkload struct {
	Component string
	Namespace string
	Kind      string
	Name      string
}

var analyticsStatusWorkloads = []platformWorkload{
	{Component: "ClickHouse", Namespace: defaultAnalyticsNamespace, Kind: "statefulset", Name: "clickhouse"},
	{Component: "Zookeeper", Namespace: defaultAnalyticsNamespace, Kind: "deployment", Name: "zookeeper"},
	{Component: "Kafka", Namespace: defaultAnalyticsNamespace, Kind: "statefulset", Name: "kafka"},
	{Component: "Ingest", Namespace: defaultAnalyticsNamespace, Kind: "deployment", Name: "mcp-sentinel-ingest"},
	{Component: "Processor", Namespace: defaultAnalyticsNamespace, Kind: "deployment", Name: "mcp-sentinel-processor"},
	{Component: "API", Namespace: defaultAnalyticsNamespace, Kind: "deployment", Name: "mcp-sentinel-api"},
	{Component: "UI", Namespace: defaultAnalyticsNamespace, Kind: "deployment", Name: "mcp-sentinel-ui"},
	{Component: "Gateway", Namespace: defaultAnalyticsNamespace, Kind: "deployment", Name: "mcp-sentinel-gateway"},
	{Component: "Prometheus", Namespace: defaultAnalyticsNamespace, Kind: "deployment", Name: "prometheus"},
	{Component: "Grafana", Namespace: defaultAnalyticsNamespace, Kind: "deployment", Name: "grafana"},
	{Component: "OTel Collector", Namespace: defaultAnalyticsNamespace, Kind: "deployment", Name: "otel-collector"},
	{Component: "Tempo", Namespace: defaultAnalyticsNamespace, Kind: "statefulset", Name: "tempo"},
	{Component: "Loki", Namespace: defaultAnalyticsNamespace, Kind: "statefulset", Name: "loki"},
	{Component: "Promtail", Namespace: defaultAnalyticsNamespace, Kind: "daemonset", Name: "promtail"},
}

// NewStatusCmd returns the status subcommand for platform health checks.
func NewStatusCmd(logger *zap.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show platform status",
		Long:  "Show the overall status of the MCP platform",
		RunE: func(cmd *cobra.Command, args []string) error {
			return showPlatformStatus(logger)
		},
	}

	return cmd
}

func showPlatformStatus(logger *zap.Logger) error {
	Header("MCP Platform Status")
	DefaultPrinter.Println()

	tableData := [][]string{
		{"Component", "Namespace", "Resource", "Status", "Details"},
	}

	clusterReachable := true
	clusterStatus := Green("OK")
	clusterDetails := "Connected"
	if err := checkClusterStatusQuiet(); err != nil {
		clusterReachable = false
		clusterStatus = Red("ERROR")
		clusterDetails = err.Error()
	}
	tableData = append(tableData, []string{"Cluster", "-", "kube-api", clusterStatus, clusterDetails})

	extRegistry, err := resolveExternalRegistryConfig(nil)
	switch {
	case err != nil:
		// resolveExternalRegistryConfig already returns (nil, nil) when no config exists,
		// so any error here is a real load/parse/validation problem rather than a missing file.
		Warn("Failed to load external registry config: " + err.Error())
		tableData = append(tableData, []string{"Registry", "-", "config", Red("ERROR"), err.Error()})
	case extRegistry != nil && extRegistry.URL != "":
		tableData = append(tableData, []string{"Registry", "-", "external", Cyan("EXTERNAL"), "Configured: " + extRegistry.URL})
	default:
		tableData = append(tableData, workloadStatusRow(
			platformWorkload{Component: "Registry", Namespace: NamespaceRegistry, Kind: "deployment", Name: RegistryDeploymentName},
			clusterReachable,
		))
	}

	tableData = append(tableData, workloadStatusRow(
		platformWorkload{Component: "Operator", Namespace: NamespaceMCPRuntime, Kind: "deployment", Name: OperatorDeploymentName},
		clusterReachable,
	))

	switch installed, analyticsErr := analyticsNamespaceInstalled(clusterReachable); {
	case !clusterReachable:
		tableData = append(tableData, analyticsStackRow(Red("ERROR"), "Cluster unavailable"))
	case analyticsErr != nil:
		tableData = append(tableData, analyticsStackRow(Red("ERROR"), analyticsErr.Error()))
	case !installed:
		tableData = append(tableData, analyticsStackRow(Yellow("SKIPPED"), "Namespace not found"))
	default:
		for _, workload := range analyticsStatusWorkloads {
			tableData = append(tableData, workloadStatusRow(workload, true))
		}
	}

	TableBoxed(tableData)

	// MCP Servers section
	DefaultPrinter.Println()
	Section("MCP Servers")

	if !clusterReachable {
		Warn("Skipping MCP server status: cluster unavailable")
		DefaultPrinter.Println()
		Info("Use 'mcp-runtime server list' for detailed server info")
		return nil
	}

	// #nosec G204 -- fixed kubectl command.
	cmd, err := kubectlClient.CommandArgs([]string{"get", "mcpserver", "--all-namespaces", "-o", "custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name,IMAGE:.spec.image,REPLICAS:.spec.replicas,PATH:.spec.ingressPath"})
	if err != nil {
		Warn("Failed to list MCP servers: " + err.Error())
	} else {
		output, execErr := cmd.CombinedOutput()
		if execErr != nil {
			errDetails := strings.TrimSpace(string(output))
			if errDetails == "" {
				errDetails = execErr.Error()
			}
			Warn("Failed to list MCP servers: " + errDetails)
		} else if len(strings.TrimSpace(string(output))) == 0 {
			Warn("No MCP servers deployed")
		} else {
			lines := strings.Split(strings.TrimSpace(string(output)), "\n")
			if len(lines) <= 1 {
				Warn("No MCP servers deployed")
			} else {
				serverData := [][]string{}
				for _, line := range lines {
					fields := strings.Fields(line)
					serverData = append(serverData, fields)
				}
				Table(serverData)
			}
		}
	}

	// Quick tips
	DefaultPrinter.Println()
	Info("Use 'mcp-runtime server list' for detailed server info")

	return nil
}

func checkClusterStatusQuiet() error {
	output, err := runKubectlCombinedOutput([]string{"cluster-info"})
	if err == nil {
		return nil
	}
	detail := commandErrorDetail(output, err)
	return wrapWithSentinel(ErrClusterNotAccessible, err, fmt.Sprintf("cluster not accessible: %s", detail))
}

func analyticsNamespaceInstalled(clusterReachable bool) (bool, error) {
	if !clusterReachable {
		return false, nil
	}

	output, err := runKubectlCombinedOutput([]string{"get", "namespace", defaultAnalyticsNamespace, "-o", "jsonpath={.metadata.name}"})
	if err == nil {
		return strings.TrimSpace(output) == defaultAnalyticsNamespace, nil
	}
	if strings.TrimSpace(output) == "" {
		return false, fmt.Errorf("empty output from namespace probe")
	}

	lower := strings.ToLower(output)
	if strings.Contains(lower, "not found") || strings.Contains(lower, "notfound") {
		return false, nil
	}

	return false, fmt.Errorf("%s", commandErrorDetail(output, err))
}

func analyticsStackRow(status, details string) []string {
	return []string{"Analytics Stack", defaultAnalyticsNamespace, "namespace/" + defaultAnalyticsNamespace, status, details}
}

func workloadStatusRow(workload platformWorkload, clusterReachable bool) []string {
	resource := fmt.Sprintf("%s/%s", workload.Kind, workload.Name)
	if !clusterReachable {
		return []string{workload.Component, workload.Namespace, resource, Red("ERROR"), "Cluster unavailable"}
	}

	status, details := workloadReadinessStatus(workload)
	return []string{workload.Component, workload.Namespace, resource, status, details}
}

func workloadReadinessStatus(workload platformWorkload) (string, string) {
	jsonPath, err := workloadReadyJSONPath(workload.Kind)
	if err != nil {
		return Red("ERROR"), err.Error()
	}

	output, cmdErr := runKubectlCombinedOutput([]string{
		"get", workload.Kind, workload.Name,
		"-n", workload.Namespace,
		"-o", "jsonpath=" + jsonPath,
	})
	if cmdErr != nil {
		return Red("ERROR"), commandErrorDetail(output, cmdErr)
	}

	if workloadReady(output) {
		return Green("OK"), "Ready: " + output
	}
	return Yellow("PENDING"), "Ready: " + output
}

func workloadReadyJSONPath(kind string) (string, error) {
	switch strings.ToLower(kind) {
	case "deployment", "statefulset":
		return "{.status.readyReplicas}/{.spec.replicas}", nil
	case "daemonset":
		return "{.status.numberReady}/{.status.desiredNumberScheduled}", nil
	default:
		return "", fmt.Errorf("unsupported workload kind %q", kind)
	}
}

func workloadReady(value string) bool {
	parts := strings.Split(strings.TrimSpace(value), "/")
	if len(parts) != 2 {
		return false
	}

	ready, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return false
	}
	desired, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return false
	}
	return desired > 0 && ready >= desired
}

func runKubectlCombinedOutput(args []string) (string, error) {
	cmd, err := kubectlClient.CommandArgs(args)
	if err != nil {
		return "", err
	}
	output, execErr := cmd.CombinedOutput()
	return strings.TrimSpace(string(output)), execErr
}

func commandErrorDetail(output string, fallback error) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			return line
		}
	}
	if fallback != nil {
		return fallback.Error()
	}
	return "Unknown error"
}
