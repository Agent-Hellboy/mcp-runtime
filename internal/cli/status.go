package cli

import (
	"strings"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

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

	// Component status table
	tableData := [][]string{
		{"Component", "Status", "Details"},
	}

	// Cluster
	clusterStatus := Green("OK")
	clusterDetails := "Connected"
	clusterMgr := DefaultClusterManager(logger)
	if err := clusterMgr.CheckClusterStatus(); err != nil {
		clusterStatus = Red("ERROR")
		clusterDetails = err.Error()
	}
	tableData = append(tableData, []string{"Cluster", clusterStatus, clusterDetails})

	// Registry
	registryStatus := Green("OK")
	registryDetails := "Running"
	if err := checkRegistryStatusQuiet(logger, NamespaceRegistry); err != nil {
		registryStatus = Red("ERROR")
		registryDetails = err.Error()
	}
	tableData = append(tableData, []string{"Registry", registryStatus, registryDetails})

	// Operator
	operatorStatus := Green("OK")
	operatorDetails := ""
	// #nosec G204 -- fixed kubectl command with hardcoded deployment name.
	replicasCmd, err := kubectlClient.CommandArgs([]string{"get", "deployment", OperatorDeploymentName, "-n", NamespaceMCPRuntime, "-o", "jsonpath={.status.readyReplicas}/{.spec.replicas}"})
	if err != nil {
		operatorStatus = Red("ERROR")
		operatorDetails = err.Error()
	} else {
		replicasOut, execErr := replicasCmd.Output()
		if execErr != nil {
			operatorStatus = Red("ERROR")
			operatorDetails = "Not found"
		} else {
			replicas := strings.TrimSpace(string(replicasOut))
			if replicas == "" || strings.HasPrefix(replicas, "/") || strings.HasPrefix(replicas, "0/") {
				operatorStatus = Yellow("PENDING")
			}
			operatorDetails = "Replicas: " + replicas
		}
	}
	tableData = append(tableData, []string{"Operator", operatorStatus, operatorDetails})

	TableBoxed(tableData)

	// MCP Servers section
	DefaultPrinter.Println()
	Section("MCP Servers")

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

// checkRegistryStatusQuiet checks registry without printing output
func checkRegistryStatusQuiet(logger *zap.Logger, namespace string) error {
	// #nosec G204 -- fixed kubectl command; namespace from internal config.
	cmd, err := kubectlClient.CommandArgs([]string{"get", "deployment", RegistryDeploymentName, "-n", namespace, "-o", "jsonpath={.status.readyReplicas}"})
	if err != nil {
		return err
	}
	out, execErr := cmd.Output()
	if execErr != nil {
		return ErrRegistryNotFound
	}
	if strings.TrimSpace(string(out)) == "" || strings.TrimSpace(string(out)) == "0" {
		return ErrRegistryNotReady
	}
	return nil
}
