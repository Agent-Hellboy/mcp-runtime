// Package status owns the status top-level command and platform status output.
package status

import (
	"strings"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"mcp-runtime/internal/cli/core"
	"mcp-runtime/internal/cli/platformstatus"
	"mcp-runtime/internal/cli/registry/config"
)

type manager struct {
	logger *zap.Logger
}

func newManager(runtime *core.Runtime) *manager {
	return &manager{logger: runtime.Logger()}
}

// New returns the status command.
func New(runtime *core.Runtime) *cobra.Command {
	mgr := newManager(runtime)
	return &cobra.Command{
		Use:   "status",
		Short: "Show platform status",
		Long:  "Show the overall status of the MCP platform",
		RunE: func(cmd *cobra.Command, args []string) error {
			return ShowPlatformStatus(mgr.logger)
		},
	}
}

// ShowPlatformStatus prints the MCP platform status table and MCP server list.
func ShowPlatformStatus(logger *zap.Logger) error {
	core.Header("MCP Platform Status")
	core.DefaultPrinter.Println()

	tableData := [][]string{
		{"Component", "Namespace", "Resource", "Status", "Details"},
	}

	clusterReachable := true
	clusterStatus := core.Green("OK")
	clusterDetails := "Connected"
	if err := platformstatus.CheckClusterStatusQuiet(); err != nil {
		clusterReachable = false
		clusterStatus = core.Red("ERROR")
		clusterDetails = err.Error()
	}
	tableData = append(tableData, []string{"Cluster", "-", "kube-api", clusterStatus, clusterDetails})

	extRegistry, err := config.Resolve(nil, config.Env{
		URL:      core.DefaultCLIConfig.ProvisionedRegistryURL,
		Username: core.DefaultCLIConfig.ProvisionedRegistryUsername,
		Password: core.DefaultCLIConfig.ProvisionedRegistryPassword,
	})
	switch {
	case err != nil:
		core.Warn("Failed to load external registry config: " + err.Error())
		tableData = append(tableData, []string{"Registry", "-", "config", core.Red("ERROR"), err.Error()})
	case extRegistry != nil && extRegistry.URL != "":
		tableData = append(tableData, []string{"Registry", "-", "external", core.Cyan("EXTERNAL"), "Configured: " + extRegistry.URL})
	default:
		tableData = append(tableData, platformstatus.WorkloadStatusRow(
			platformstatus.PlatformWorkload{Component: "Registry", Namespace: core.NamespaceRegistry, Kind: "deployment", Name: core.RegistryDeploymentName},
			clusterReachable,
		))
	}

	tableData = append(tableData, platformstatus.WorkloadStatusRow(
		platformstatus.PlatformWorkload{Component: "Operator", Namespace: core.NamespaceMCPRuntime, Kind: "deployment", Name: core.OperatorDeploymentName},
		clusterReachable,
	))

	switch installed, analyticsErr := platformstatus.AnalyticsNamespaceInstalled(clusterReachable); {
	case !clusterReachable:
		tableData = append(tableData, platformstatus.AnalyticsStackRow(core.Red("ERROR"), "Cluster unavailable"))
	case analyticsErr != nil:
		tableData = append(tableData, platformstatus.AnalyticsStackRow(core.Red("ERROR"), analyticsErr.Error()))
	case !installed:
		tableData = append(tableData, platformstatus.AnalyticsStackRow(core.Yellow("SKIPPED"), "Namespace not found"))
	default:
		for _, workload := range platformstatus.DefaultPlatformStatusWorkloads {
			tableData = append(tableData, platformstatus.WorkloadStatusRow(workload, true))
		}
	}

	core.TableBoxed(tableData)

	core.DefaultPrinter.Println()
	core.Section("MCP Servers")

	if !clusterReachable {
		core.Warn("Skipping MCP server status: cluster unavailable")
		core.DefaultPrinter.Println()
		core.Info("Use 'mcp-runtime server list' for detailed server info")
		return nil
	}

	cmd, err := core.DefaultKubectlClient().CommandArgs([]string{"get", "mcpserver", "--all-namespaces", "-o", "custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name,IMAGE:.spec.image,REPLICAS:.spec.replicas,PATH:.spec.ingressPath"})
	if err != nil {
		core.Warn("Failed to list MCP servers: " + err.Error())
	} else {
		output, execErr := cmd.CombinedOutput()
		if execErr != nil {
			errDetails := strings.TrimSpace(string(output))
			if errDetails == "" {
				errDetails = execErr.Error()
			}
			core.Warn("Failed to list MCP servers: " + errDetails)
		} else if len(strings.TrimSpace(string(output))) == 0 {
			core.Warn("No MCP servers deployed")
		} else {
			lines := strings.Split(strings.TrimSpace(string(output)), "\n")
			if len(lines) <= 1 {
				core.Warn("No MCP servers deployed")
			} else {
				serverData := [][]string{}
				for _, line := range lines {
					fields := strings.Fields(line)
					serverData = append(serverData, fields)
				}
				core.Table(serverData)
			}
		}
	}

	core.DefaultPrinter.Println()
	core.Info("Use 'mcp-runtime server list' for detailed server info")

	return nil
}
