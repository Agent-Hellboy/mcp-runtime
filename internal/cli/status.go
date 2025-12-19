package cli

import (
	"fmt"
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
	if err := checkClusterStatus(logger); err != nil {
		clusterStatus = Red("ERROR")
		clusterDetails = err.Error()
	}
	tableData = append(tableData, []string{"Cluster", clusterStatus, clusterDetails})

	// Registry
	registryStatus := Green("OK")
	registryDetails := "Running"
	if err := checkRegistryStatusQuiet(logger, "registry"); err != nil {
		registryStatus = Red("ERROR")
		registryDetails = err.Error()
	}
	tableData = append(tableData, []string{"Registry", registryStatus, registryDetails})

	// Operator
	operatorStatus := Green("OK")
	operatorDetails := ""
	replicasCmd := execCommand("kubectl", "get", "deployment", "mcp-runtime-operator-controller-manager", "-n", "mcp-runtime", "-o", "jsonpath={.status.readyReplicas}/{.spec.replicas}")
	replicasOut, err := replicasCmd.Output()
	if err != nil {
		operatorStatus = Red("ERROR")
		operatorDetails = "Not found"
	} else {
		replicas := strings.TrimSpace(string(replicasOut))
		if replicas == "" || strings.HasPrefix(replicas, "/") || strings.HasPrefix(replicas, "0/") {
			operatorStatus = Yellow("PENDING")
		}
		operatorDetails = "Replicas: " + replicas
	}
	tableData = append(tableData, []string{"Operator", operatorStatus, operatorDetails})

	TableBoxed(tableData)

	// MCP Servers section
	DefaultPrinter.Println()
	Section("MCP Servers")

	cmd := execCommand("kubectl", "get", "mcpserver", "--all-namespaces", "-o", "custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name,IMAGE:.spec.image,REPLICAS:.spec.replicas,PATH:.spec.ingressPath")
	output, err := cmd.CombinedOutput()
	if err != nil {
		errDetails := strings.TrimSpace(string(output))
		if errDetails == "" {
			errDetails = err.Error()
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

	// Quick tips
	DefaultPrinter.Println()
	Info("Use 'mcp-runtime server list' for detailed server info")

	return nil
}

// checkRegistryStatusQuiet checks registry without printing output
func checkRegistryStatusQuiet(logger *zap.Logger, namespace string) error {
	cmd := execCommand("kubectl", "get", "deployment", "registry", "-n", namespace, "-o", "jsonpath={.status.readyReplicas}")
	out, err := cmd.Output()
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(out)) == "" || strings.TrimSpace(string(out)) == "0" {
		return fmt.Errorf("registry not ready")
	}
	return nil
}
