package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// NewServerCmd returns the server subcommand (build/deploy helpers).
func NewServerCmd(logger *zap.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Manage MCP servers",
		Long: `Commands for managing MCP server deployments.

For building images from source, use 'server build'.
For pushing images, use 'registry push'.`,
	}

	cmd.AddCommand(newServerListCmd(logger))
	cmd.AddCommand(newServerGetCmd(logger))
	cmd.AddCommand(newServerCreateCmd(logger))
	cmd.AddCommand(newServerDeleteCmd(logger))
	cmd.AddCommand(newServerLogsCmd(logger))
	cmd.AddCommand(newServerStatusCmd(logger))
	cmd.AddCommand(newServerBuildCmd(logger))

	return cmd
}

func newServerBuildCmd(logger *zap.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build MCP server images (push via `registry push`)",
	}

	// Only expose image build here; pushing is handled by `registry push`.
	cmd.AddCommand(newBuildImageCmd(logger))

	return cmd
}

func newServerListCmd(logger *zap.Logger) *cobra.Command {
	var namespace string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List MCP servers",
		Long:  "List all MCP server deployments",
		RunE: func(cmd *cobra.Command, args []string) error {
			return listServers(logger, namespace)
		},
	}

	cmd.Flags().StringVar(&namespace, "namespace", "mcp-servers", "Namespace to list servers from")

	return cmd
}

func newServerGetCmd(logger *zap.Logger) *cobra.Command {
	var namespace string

	cmd := &cobra.Command{
		Use:   "get [name]",
		Short: "Get MCP server details",
		Long:  "Get detailed information about an MCP server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getServer(logger, args[0], namespace)
		},
	}

	cmd.Flags().StringVar(&namespace, "namespace", "mcp-servers", "Namespace")

	return cmd
}

func newServerCreateCmd(logger *zap.Logger) *cobra.Command {
	var namespace string
	var image string
	var imageTag string
	var file string

	cmd := &cobra.Command{
		Use:   "create [name]",
		Short: "Create an MCP server",
		Long:  "Create a new MCP server deployment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if file != "" {
				return createServerFromFile(logger, file)
			}
			return createServer(logger, args[0], namespace, image, imageTag)
		},
	}

	cmd.Flags().StringVar(&namespace, "namespace", "mcp-servers", "Namespace")
	cmd.Flags().StringVar(&image, "image", "", "Container image")
	cmd.Flags().StringVar(&imageTag, "tag", "latest", "Image tag")
	cmd.Flags().StringVar(&file, "file", "", "YAML file with server spec")

	return cmd
}

func newServerDeleteCmd(logger *zap.Logger) *cobra.Command {
	var namespace string

	cmd := &cobra.Command{
		Use:   "delete [name]",
		Short: "Delete an MCP server",
		Long:  "Delete an MCP server deployment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return deleteServer(logger, args[0], namespace)
		},
	}

	cmd.Flags().StringVar(&namespace, "namespace", "mcp-servers", "Namespace")

	return cmd
}

func newServerLogsCmd(logger *zap.Logger) *cobra.Command {
	var namespace string
	var follow bool

	cmd := &cobra.Command{
		Use:   "logs [name]",
		Short: "View server logs",
		Long:  "View logs from an MCP server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return viewServerLogs(logger, args[0], namespace, follow)
		},
	}

	cmd.Flags().StringVar(&namespace, "namespace", "mcp-servers", "Namespace")
	cmd.Flags().BoolVar(&follow, "follow", false, "Follow log output")

	return cmd
}

func newServerStatusCmd(logger *zap.Logger) *cobra.Command {
	var namespace string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show MCP server runtime status (pods, images, pull secrets)",
		Long:  "List MCPServer resources with their Deployment/pod status, image, and pull secrets.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return serverStatus(logger, namespace)
		},
	}

	cmd.Flags().StringVar(&namespace, "namespace", "mcp-servers", "Namespace to inspect")

	return cmd
}

func listServers(logger *zap.Logger, namespace string) error {
	cmd := exec.Command("kubectl", "get", "mcpserver", "-n", namespace)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func getServer(logger *zap.Logger, name, namespace string) error {
	cmd := exec.Command("kubectl", "get", "mcpserver", name, "-n", namespace, "-o", "yaml")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func createServer(logger *zap.Logger, name, namespace, image, imageTag string) error {
	if image == "" {
		return fmt.Errorf("image is required")
	}

	var err error
	if name, err = validateManifestValue("name", name); err != nil {
		return err
	}
	if namespace, err = validateManifestValue("namespace", namespace); err != nil {
		return err
	}
	if image, err = validateManifestValue("image", image); err != nil {
		return err
	}
	if imageTag, err = validateManifestValue("tag", imageTag); err != nil {
		return err
	}

	logger.Info("Creating MCP server", zap.String("name", name), zap.String("image", image))

	manifest := mcpServerManifest{
		APIVersion: "mcp.agent-hellboy.io/v1alpha1",
		Kind:       "MCPServer",
		Metadata: manifestMetadata{
			Name:      name,
			Namespace: namespace,
		},
		Spec: manifestSpec{
			Image:       image,
			ImageTag:    imageTag,
			Replicas:    1,
			Port:        GetDefaultServerPort(),
			ServicePort: 80,
			IngressPath: "/" + name,
		},
	}

	manifestBytes, err := yaml.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}

	tmpFile := fmt.Sprintf("/tmp/mcpserver-%s.yaml", name)
	if err := os.WriteFile(tmpFile, manifestBytes, 0644); err != nil {
		return fmt.Errorf("failed to create manifest: %w", err)
	}
	defer os.Remove(tmpFile)

	cmd := exec.Command("kubectl", "apply", "-f", tmpFile)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func createServerFromFile(logger *zap.Logger, file string) error {
	cmd := exec.Command("kubectl", "apply", "-f", file)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func deleteServer(logger *zap.Logger, name, namespace string) error {
	logger.Info("Deleting MCP server", zap.String("name", name))

	cmd := exec.Command("kubectl", "delete", "mcpserver", name, "-n", namespace)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func viewServerLogs(logger *zap.Logger, name, namespace string, follow bool) error {
	args := []string{"logs", "-l", "app=" + name, "-n", namespace}
	if follow {
		args = append(args, "-f")
	}

	cmd := exec.Command("kubectl", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func serverStatus(logger *zap.Logger, namespace string) error {
	Header(fmt.Sprintf("MCP Servers in %s", namespace))
	DefaultPrinter.Println()

	// Get MCPServer details
	getServersCmd := execCommand("kubectl", "get", "mcpserver", "-n", namespace, "-o", "jsonpath={range .items[*]}{.metadata.name}|{.spec.image}:{.spec.imageTag}|{.spec.replicas}|{.spec.ingressPath}|{.spec.useProvisionedRegistry}{\"\\n\"}{end}")
	out, err := getServersCmd.CombinedOutput()
	if err != nil {
		errDetails := strings.TrimSpace(string(out))
		if errDetails == "" {
			errDetails = err.Error()
		}
		DefaultPrinter.Println("ERROR: Failed to list MCP servers: " + errDetails)
		return fmt.Errorf("kubectl get mcpserver failed: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") {
		Warn("No MCP servers found in namespace " + namespace)
		return nil
	}

	// Build table
	tableData := [][]string{
		{"Name", "Image", "Replicas", "Path", "Registry"},
	}

	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) >= 5 {
			name := parts[0]
			image := parts[1]
			replicas := parts[2]
			path := parts[3]
			useProv := parts[4]

			registry := "custom"
			if useProv == "true" {
				registry = "provisioned"
			}

			tableData = append(tableData, []string{name, image, replicas, path, registry})
		}
	}

	if len(tableData) > 1 {
		TableBoxed(tableData)
	}

	// Pod status section
	DefaultPrinter.Println()
	Section("Pod Status")

	podCmd := execCommand("kubectl", "get", "pods", "-n", namespace, "-l", "app.kubernetes.io/managed-by=mcp-runtime", "-o", "custom-columns=NAME:.metadata.name,READY:.status.containerStatuses[0].ready,STATUS:.status.phase,RESTARTS:.status.containerStatuses[0].restartCount")
	podOut, err := podCmd.Output()
	if err != nil {
		Warn("Failed to list pods: " + err.Error())
		return nil
	}
	if len(strings.TrimSpace(string(podOut))) > 0 {
		podLines := strings.Split(strings.TrimSpace(string(podOut)), "\n")
		if len(podLines) > 1 {
			podData := [][]string{}
			for _, pl := range podLines {
				podData = append(podData, strings.Fields(pl))
			}
			Table(podData)
		} else {
			Info("No pods found")
		}
	}

	return nil
}

type mcpServerManifest struct {
	APIVersion string           `yaml:"apiVersion"`
	Kind       string           `yaml:"kind"`
	Metadata   manifestMetadata `yaml:"metadata"`
	Spec       manifestSpec     `yaml:"spec"`
}

type manifestMetadata struct {
	Name      string `yaml:"name"`
	Namespace string `yaml:"namespace"`
}

type manifestSpec struct {
	Image       string `yaml:"image"`
	ImageTag    string `yaml:"imageTag"`
	Replicas    int    `yaml:"replicas"`
	Port        int    `yaml:"port"`
	ServicePort int    `yaml:"servicePort"`
	IngressPath string `yaml:"ingressPath"`
}

// validateManifestValue ensures basic values do not contain control characters that would break YAML.
func validateManifestValue(field, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if strings.ContainsAny(value, "\r\n\t") {
		return "", fmt.Errorf("%s must not contain control characters", field)
	}
	return value, nil
}
