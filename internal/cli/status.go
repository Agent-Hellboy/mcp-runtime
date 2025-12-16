package cli

import (
	"fmt"
	"os"
	"os/exec"

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
	fmt.Println("=== MCP Platform Status ===")
	fmt.Println()

	// Cluster status
	fmt.Println("Cluster:")
	if err := checkClusterStatus(logger); err != nil {
		fmt.Printf("  Status: ERROR - %v\n", err)
	} else {
		fmt.Println("  Status: OK")
	}

	fmt.Println("\nNamespaces:")
	nsCmd := exec.Command("kubectl", "get", "ns")
	nsCmd.Stdout = os.Stdout
	nsCmd.Stderr = os.Stderr
	_ = nsCmd.Run()

	fmt.Println("\nRegistry:")
	if err := checkRegistryStatus(logger, "registry"); err != nil {
		fmt.Printf("  Status: ERROR - %v\n", err)
	} else {
		fmt.Println("  Status: OK")
	}

	fmt.Println("\nOperator:")
	imageCmd := exec.Command("kubectl", "get", "deployment", "mcp-runtime-operator-controller-manager", "-n", "mcp-runtime", "-o", "jsonpath={.spec.template.spec.containers[0].image}")
	imageOut, _ := imageCmd.Output()
	replicasCmd := exec.Command("kubectl", "get", "deployment", "mcp-runtime-operator-controller-manager", "-n", "mcp-runtime", "-o", "jsonpath={.status.readyReplicas}/{.spec.replicas}")
	replicasOut, _ := replicasCmd.Output()
	fmt.Printf("  Image: %s\n", string(imageOut))
	fmt.Printf("  Replicas: %s\n", string(replicasOut))
	fmt.Println("  Pods:")
	podsCmd := exec.Command("kubectl", "get", "pods", "-n", "mcp-runtime", "-o", "wide")
	podsCmd.Stdout = os.Stdout
	podsCmd.Stderr = os.Stderr
	_ = podsCmd.Run()

	fmt.Println("\nMCP Servers:")
	cmd := exec.Command("kubectl", "get", "mcpserver", "--all-namespaces", "--no-headers")
	output, _ := cmd.Output()
	if len(output) == 0 {
		fmt.Println("  No servers deployed")
	} else {
		fmt.Println(string(output))
	}

	return nil
}
