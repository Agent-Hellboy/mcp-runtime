package cli

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

type SentinelManager struct {
	kubectl *KubectlClient
	logger  *zap.Logger
}

type sentinelComponent struct {
	Key        string
	Display    string
	Namespace  string
	Kind       string
	Resource   string
	Label      string
	Aliases    []string
	PortTarget *sentinelPortTarget
}

type sentinelPortTarget struct {
	ResourceKind string
	ResourceName string
	LocalPort    int
	RemotePort   int
}

var sentinelComponents = []sentinelComponent{
	{Key: "clickhouse", Display: "ClickHouse", Namespace: defaultAnalyticsNamespace, Kind: "statefulset", Resource: "clickhouse", Label: "clickhouse"},
	{Key: "zookeeper", Display: "Zookeeper", Namespace: defaultAnalyticsNamespace, Kind: "deployment", Resource: "zookeeper", Label: "zookeeper"},
	{Key: "kafka", Display: "Kafka", Namespace: defaultAnalyticsNamespace, Kind: "statefulset", Resource: "kafka", Label: "kafka"},
	{Key: "ingest", Display: "Ingest", Namespace: defaultAnalyticsNamespace, Kind: "deployment", Resource: "mcp-sentinel-ingest", Label: "mcp-sentinel-ingest"},
	{
		Key:       "api",
		Display:   "API",
		Namespace: defaultAnalyticsNamespace,
		Kind:      "deployment",
		Resource:  "mcp-sentinel-api",
		Label:     "mcp-sentinel-api",
		PortTarget: &sentinelPortTarget{
			ResourceKind: "service",
			ResourceName: "mcp-sentinel-api",
			LocalPort:    8080,
			RemotePort:   8080,
		},
	},
	{Key: "processor", Display: "Processor", Namespace: defaultAnalyticsNamespace, Kind: "deployment", Resource: "mcp-sentinel-processor", Label: "mcp-sentinel-processor"},
	{
		Key:       "ui",
		Display:   "UI",
		Namespace: defaultAnalyticsNamespace,
		Kind:      "deployment",
		Resource:  "mcp-sentinel-ui",
		Label:     "mcp-sentinel-ui",
		PortTarget: &sentinelPortTarget{
			ResourceKind: "service",
			ResourceName: "mcp-sentinel-ui",
			LocalPort:    8082,
			RemotePort:   8082,
		},
	},
	{Key: "gateway", Display: "Gateway", Namespace: defaultAnalyticsNamespace, Kind: "deployment", Resource: "mcp-sentinel-gateway", Label: "mcp-sentinel-gateway"},
	{
		Key:       "prometheus",
		Display:   "Prometheus",
		Namespace: defaultAnalyticsNamespace,
		Kind:      "deployment",
		Resource:  "prometheus",
		Label:     "prometheus",
		Aliases:   []string{"prom"},
		PortTarget: &sentinelPortTarget{
			ResourceKind: "service",
			ResourceName: "prometheus",
			LocalPort:    9090,
			RemotePort:   9090,
		},
	},
	{
		Key:       "grafana",
		Display:   "Grafana",
		Namespace: defaultAnalyticsNamespace,
		Kind:      "deployment",
		Resource:  "grafana",
		Label:     "grafana",
		PortTarget: &sentinelPortTarget{
			ResourceKind: "service",
			ResourceName: "grafana",
			LocalPort:    3000,
			RemotePort:   3000,
		},
	},
	{Key: "otel-collector", Display: "OTel Collector", Namespace: defaultAnalyticsNamespace, Kind: "deployment", Resource: "otel-collector", Label: "otel-collector", Aliases: []string{"otel"}},
	{Key: "tempo", Display: "Tempo", Namespace: defaultAnalyticsNamespace, Kind: "statefulset", Resource: "tempo", Label: "tempo"},
	{Key: "loki", Display: "Loki", Namespace: defaultAnalyticsNamespace, Kind: "statefulset", Resource: "loki", Label: "loki"},
	{Key: "promtail", Display: "Promtail", Namespace: defaultAnalyticsNamespace, Kind: "daemonset", Resource: "promtail", Label: "promtail"},
}

func NewSentinelManager(kubectl *KubectlClient, logger *zap.Logger) *SentinelManager {
	return &SentinelManager{kubectl: kubectl, logger: logger}
}

func DefaultSentinelManager(logger *zap.Logger) *SentinelManager {
	return NewSentinelManager(kubectlClient, logger)
}

func NewSentinelCmd(logger *zap.Logger) *cobra.Command {
	mgr := DefaultSentinelManager(logger)
	return NewSentinelCmdWithManager(mgr)
}

func NewSentinelCmdWithManager(mgr *SentinelManager) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sentinel",
		Short: "Operate the bundled mcp-sentinel stack",
		Long:  "Commands for inspecting and operating the bundled mcp-sentinel analytics, gateway, and observability stack.",
	}

	cmd.AddCommand(mgr.newSentinelStatusCmd())
	cmd.AddCommand(mgr.newSentinelLogsCmd())
	cmd.AddCommand(mgr.newSentinelEventsCmd())
	cmd.AddCommand(mgr.newSentinelPortForwardCmd())
	cmd.AddCommand(mgr.newSentinelRestartCmd())

	return cmd
}

func sentinelComponentKeys() []string {
	keys := make([]string, 0, len(sentinelComponents))
	for _, component := range sentinelComponents {
		keys = append(keys, component.Key)
	}
	sort.Strings(keys)
	return keys
}

func findSentinelComponent(name string) (*sentinelComponent, error) {
	candidate := strings.ToLower(strings.TrimSpace(name))
	for i := range sentinelComponents {
		component := &sentinelComponents[i]
		if component.Key == candidate {
			return component, nil
		}
		for _, alias := range component.Aliases {
			if alias == candidate {
				return component, nil
			}
		}
	}

	return nil, newWithSentinel(nil, fmt.Sprintf("unknown sentinel component %q (use one of: %s)", name, strings.Join(sentinelComponentKeys(), ", ")))
}

func findSentinelPortTarget(name string) (*sentinelPortTarget, error) {
	component, err := findSentinelComponent(name)
	if err != nil {
		return nil, err
	}
	if component.PortTarget == nil {
		return nil, newWithSentinel(nil, fmt.Sprintf("component %q does not expose a predefined port-forward target", name))
	}
	return component.PortTarget, nil
}

func (m *SentinelManager) newSentinelStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show mcp-sentinel stack status",
		RunE: func(cmd *cobra.Command, args []string) error {
			return m.ShowSentinelStatus()
		},
	}
}

func (m *SentinelManager) newSentinelLogsCmd() *cobra.Command {
	var follow bool
	var previous bool
	var tail int
	var since string

	cmd := &cobra.Command{
		Use:       "logs [component]",
		Short:     "View logs for a mcp-sentinel component",
		Args:      cobra.ExactArgs(1),
		ValidArgs: sentinelComponentKeys(),
		RunE: func(cmd *cobra.Command, args []string) error {
			return m.ViewSentinelLogs(args[0], follow, previous, tail, since)
		},
	}

	cmd.Flags().BoolVar(&follow, "follow", false, "Follow log output")
	cmd.Flags().BoolVar(&previous, "previous", false, "Show logs from the previous container instance")
	cmd.Flags().IntVar(&tail, "tail", 200, "Number of recent log lines to show (-1 for all)")
	cmd.Flags().StringVar(&since, "since", "", "Only return logs newer than a relative duration like 5m or 1h")

	return cmd
}

func (m *SentinelManager) newSentinelEventsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "events",
		Short: "Show recent Kubernetes events for mcp-sentinel",
		RunE: func(cmd *cobra.Command, args []string) error {
			return m.ShowSentinelEvents()
		},
	}
}

func (m *SentinelManager) newSentinelPortForwardCmd() *cobra.Command {
	var localPort int
	var address string

	cmd := &cobra.Command{
		Use:   "port-forward [target]",
		Short: "Port-forward a common mcp-sentinel service",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return m.PortForwardSentinelTarget(args[0], localPort, address)
		},
	}

	cmd.Flags().IntVar(&localPort, "port", 0, "Local port to bind (defaults to the target service port)")
	cmd.Flags().StringVar(&address, "address", "127.0.0.1", "Addresses to listen on")

	return cmd
}

func (m *SentinelManager) newSentinelRestartCmd() *cobra.Command {
	var restartAll bool

	cmd := &cobra.Command{
		Use:   "restart [component]",
		Short: "Restart one or all mcp-sentinel workloads",
		Args: func(cmd *cobra.Command, args []string) error {
			if restartAll && len(args) == 0 {
				return nil
			}
			return cobra.ExactArgs(1)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			component := ""
			if len(args) > 0 {
				component = args[0]
			}
			return m.RestartSentinel(component, restartAll)
		},
	}

	cmd.Flags().BoolVar(&restartAll, "all", false, "Restart every mcp-sentinel workload")

	return cmd
}

func (m *SentinelManager) ShowSentinelStatus() error {
	Header("MCP Sentinel Status")
	DefaultPrinter.Println()

	tableData := [][]string{{"Component", "Namespace", "Resource", "Status", "Details"}}

	clusterReachable := true
	if err := checkClusterStatusQuiet(); err != nil {
		clusterReachable = false
		tableData = append(tableData, analyticsStackRow(Red("ERROR"), err.Error()))
		TableBoxed(tableData)
		return nil
	}

	installed, err := analyticsNamespaceInstalled(clusterReachable)
	switch {
	case err != nil:
		tableData = append(tableData, analyticsStackRow(Red("ERROR"), err.Error()))
	case !installed:
		tableData = append(tableData, analyticsStackRow(Yellow("SKIPPED"), "Namespace not found"))
	default:
		for _, workload := range analyticsStatusWorkloads {
			tableData = append(tableData, workloadStatusRow(workload, true))
		}
	}

	TableBoxed(tableData)
	return nil
}

func (m *SentinelManager) ViewSentinelLogs(component string, follow, previous bool, tail int, since string) error {
	target, err := findSentinelComponent(component)
	if err != nil {
		return err
	}

	args := []string{
		"logs",
		"-n", target.Namespace,
		"-l", "app=" + target.Label,
		"--all-containers=true",
		"--prefix=true",
		"--tail", strconv.Itoa(tail),
	}
	if follow {
		args = append(args, "-f")
	}
	if previous {
		args = append(args, "--previous")
	}
	if strings.TrimSpace(since) != "" {
		args = append(args, "--since", strings.TrimSpace(since))
	}

	if err := m.kubectl.RunWithOutput(args, os.Stdout, os.Stderr); err != nil {
		return wrapWithSentinelAndContext(nil, err, fmt.Sprintf("failed to stream logs for sentinel component %q: %v", component, err), map[string]any{
			"component": component,
			"namespace": target.Namespace,
		})
	}
	return nil
}

func (m *SentinelManager) ShowSentinelEvents() error {
	args := []string{"get", "events", "-n", defaultAnalyticsNamespace, "--sort-by=.lastTimestamp"}
	if err := m.kubectl.RunWithOutput(args, os.Stdout, os.Stderr); err != nil {
		return wrapWithSentinelAndContext(nil, err, fmt.Sprintf("failed to list sentinel events: %v", err), map[string]any{
			"namespace": defaultAnalyticsNamespace,
			"component": "sentinel",
		})
	}
	return nil
}

func (m *SentinelManager) PortForwardSentinelTarget(target string, localPort int, address string) error {
	portTarget, err := findSentinelPortTarget(target)
	if err != nil {
		return err
	}
	if localPort <= 0 {
		localPort = portTarget.LocalPort
	}

	args := []string{
		"port-forward",
		"-n", defaultAnalyticsNamespace,
		fmt.Sprintf("%s/%s", portTarget.ResourceKind, portTarget.ResourceName),
		fmt.Sprintf("%d:%d", localPort, portTarget.RemotePort),
		"--address", address,
	}

	if err := m.kubectl.RunWithOutput(args, os.Stdout, os.Stderr); err != nil {
		return wrapWithSentinelAndContext(nil, err, fmt.Sprintf("failed to port-forward sentinel target %q: %v", target, err), map[string]any{
			"target":    target,
			"namespace": defaultAnalyticsNamespace,
			"component": "sentinel",
		})
	}
	return nil
}

func (m *SentinelManager) RestartSentinel(component string, restartAll bool) error {
	if restartAll {
		for _, target := range sentinelComponents {
			args := []string{"rollout", "restart", fmt.Sprintf("%s/%s", target.Kind, target.Resource), "-n", target.Namespace}
			if err := m.kubectl.RunWithOutput(args, os.Stdout, os.Stderr); err != nil {
				return wrapWithSentinelAndContext(nil, err, fmt.Sprintf("failed to restart sentinel component %q: %v", target.Key, err), map[string]any{
					"component": target.Key,
					"namespace": target.Namespace,
				})
			}
		}
		return nil
	}

	target, err := findSentinelComponent(component)
	if err != nil {
		return err
	}
	args := []string{"rollout", "restart", fmt.Sprintf("%s/%s", target.Kind, target.Resource), "-n", target.Namespace}
	if err := m.kubectl.RunWithOutput(args, os.Stdout, os.Stderr); err != nil {
		return wrapWithSentinelAndContext(nil, err, fmt.Sprintf("failed to restart sentinel component %q: %v", component, err), map[string]any{
			"component": component,
			"namespace": target.Namespace,
		})
	}
	return nil
}
