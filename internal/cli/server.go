package cli

// This file implements the "server" command for managing MCP server resources.
// It handles creating, listing, viewing, and deleting MCPServer custom resources.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// ServerManager handles MCP server operations with injected dependencies.
type ServerManager struct {
	kubectl *KubectlClient
	logger  *zap.Logger
}

// NewServerManager creates a ServerManager with the given dependencies.
func NewServerManager(kubectl *KubectlClient, logger *zap.Logger) *ServerManager {
	return &ServerManager{
		kubectl: kubectl,
		logger:  logger,
	}
}

// DefaultServerManager returns a ServerManager using the default kubectl client.
func DefaultServerManager(logger *zap.Logger) *ServerManager {
	return NewServerManager(kubectlClient, logger)
}

// validServerName matches Kubernetes resource name requirements (RFC 1123 subdomain).
var validServerName = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// validateServerInput validates name and namespace for kubectl commands.
// Returns sanitized values or an error if validation fails.
func validateServerInput(name, namespace string) (string, string, error) {
	if !validServerName.MatchString(name) {
		return "", "", newWithSentinel(ErrInvalidServerName, fmt.Sprintf("invalid server name %q: must be lowercase alphanumeric with optional hyphens", name))
	}

	var err error
	if name, err = validateManifestValue("name", name); err != nil {
		return "", "", err
	}
	if namespace, err = validateManifestValue("namespace", namespace); err != nil {
		return "", "", err
	}

	return name, namespace, nil
}

// NewServerCmd returns the server subcommand (build/deploy helpers).
func NewServerCmd(logger *zap.Logger) *cobra.Command {
	mgr := DefaultServerManager(logger)
	return NewServerCmdWithManager(mgr)
}

// NewServerCmdWithManager returns the server subcommand using the provided manager.
// This is useful for testing with mock dependencies.
func NewServerCmdWithManager(mgr *ServerManager) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Manage MCP servers",
		Long: `Commands for managing MCP server deployments.

For building images from source, use 'server build'.
For pushing images, use 'registry push'.`,
	}

	cmd.AddCommand(mgr.newServerListCmd())
	cmd.AddCommand(mgr.newServerGetCmd())
	cmd.AddCommand(mgr.newServerCreateCmd())
	cmd.AddCommand(mgr.newServerDeleteCmd())
	cmd.AddCommand(mgr.newServerLogsCmd())
	cmd.AddCommand(mgr.newServerStatusCmd())
	cmd.AddCommand(newServerBuildCmd(mgr.logger))

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

func (m *ServerManager) newServerListCmd() *cobra.Command {
	var namespace string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List MCP servers",
		Long:  "List all MCP server deployments",
		RunE: func(cmd *cobra.Command, args []string) error {
			return m.ListServers(namespace)
		},
	}

	cmd.Flags().StringVar(&namespace, "namespace", NamespaceMCPServers, "Namespace to list servers from")

	return cmd
}

func (m *ServerManager) newServerGetCmd() *cobra.Command {
	var namespace string

	cmd := &cobra.Command{
		Use:   "get [name]",
		Short: "Get MCP server details",
		Long:  "Get detailed information about an MCP server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return m.GetServer(args[0], namespace)
		},
	}

	cmd.Flags().StringVar(&namespace, "namespace", NamespaceMCPServers, "Namespace")

	return cmd
}

func (m *ServerManager) newServerCreateCmd() *cobra.Command {
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
				return m.CreateServerFromFile(file)
			}
			return m.CreateServer(args[0], namespace, image, imageTag)
		},
	}

	cmd.Flags().StringVar(&namespace, "namespace", NamespaceMCPServers, "Namespace")
	cmd.Flags().StringVar(&image, "image", "", "Container image")
	cmd.Flags().StringVar(&imageTag, "tag", "latest", "Image tag")
	cmd.Flags().StringVar(&file, "file", "", "YAML file with server spec")

	return cmd
}

func (m *ServerManager) newServerDeleteCmd() *cobra.Command {
	var namespace string

	cmd := &cobra.Command{
		Use:   "delete [name]",
		Short: "Delete an MCP server",
		Long:  "Delete an MCP server deployment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return m.DeleteServer(args[0], namespace)
		},
	}

	cmd.Flags().StringVar(&namespace, "namespace", NamespaceMCPServers, "Namespace")

	return cmd
}

func (m *ServerManager) newServerLogsCmd() *cobra.Command {
	var namespace string
	var follow bool

	cmd := &cobra.Command{
		Use:   "logs [name]",
		Short: "View server logs",
		Long:  "View logs from an MCP server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return m.ViewServerLogs(args[0], namespace, follow)
		},
	}

	cmd.Flags().StringVar(&namespace, "namespace", NamespaceMCPServers, "Namespace")
	cmd.Flags().BoolVar(&follow, "follow", false, "Follow log output")

	return cmd
}

func (m *ServerManager) newServerStatusCmd() *cobra.Command {
	var namespace string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show MCP server runtime status (pods, images, pull secrets)",
		Long:  "List MCPServer resources with their Deployment/pod status, image, and pull secrets.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return m.ServerStatus(namespace)
		},
	}

	cmd.Flags().StringVar(&namespace, "namespace", NamespaceMCPServers, "Namespace to inspect")

	return cmd
}

// ListServers lists all MCP servers in the given namespace.
func (m *ServerManager) ListServers(namespace string) error {
	namespace, err := validateManifestValue("namespace", namespace)
	if err != nil {
		return err
	}

	// #nosec G204 -- namespace validated above; kubectl validates resource names.
	if err := m.kubectl.RunWithOutput([]string{"get", "mcpserver", "-n", namespace}, os.Stdout, os.Stderr); err != nil {
		wrappedErr := wrapWithSentinelAndContext(
			ErrListServersFailed,
			err,
			fmt.Sprintf("failed to list servers in namespace %q: %v", namespace, err),
			map[string]any{"namespace": namespace, "component": "server"},
		)
		Error("Failed to list servers")
		logStructuredError(m.logger, wrappedErr, "Failed to list servers")
		return wrappedErr
	}
	return nil
}

// GetServer retrieves details for a specific MCP server.
func (m *ServerManager) GetServer(name, namespace string) error {
	name, namespace, err := validateServerInput(name, namespace)
	if err != nil {
		return err
	}

	// #nosec G204 -- name/namespace validated via validateServerInput.
	if err := m.kubectl.RunWithOutput([]string{"get", "mcpserver", name, "-n", namespace, "-o", "yaml"}, os.Stdout, os.Stderr); err != nil {
		wrappedErr := wrapWithSentinelAndContext(
			ErrGetMCPServerFailed,
			err,
			fmt.Sprintf("failed to get server %q in namespace %q: %v", name, namespace, err),
			map[string]any{"server": name, "namespace": namespace, "component": "server"},
		)
		Error("Failed to get server")
		logStructuredError(m.logger, wrappedErr, "Failed to get server")
		return wrappedErr
	}
	return nil
}

// CreateServer creates a new MCP server with the given parameters.
func (m *ServerManager) CreateServer(name, namespace, image, imageTag string) error {
	if image == "" {
		return ErrImageRequired
	}

	name, namespace, err := validateServerInput(name, namespace)
	if err != nil {
		return err
	}
	if image, err = validateManifestValue("image", image); err != nil {
		return err
	}
	if imageTag, err = validateManifestValue("tag", imageTag); err != nil {
		return err
	}

	m.logger.Info("Creating MCP server", zap.String("name", name), zap.String("image", image))

	manifest := mcpServerManifest{
		APIVersion: "mcpruntime.org/v1alpha1",
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
		wrappedErr := wrapWithSentinelAndContext(
			ErrMarshalManifestFailed,
			err,
			fmt.Sprintf("failed to marshal manifest: %v", err),
			map[string]any{"server": name, "namespace": namespace, "component": "server"},
		)
		Error("Failed to marshal manifest")
		logStructuredError(m.logger, wrappedErr, "Failed to marshal manifest")
		return wrappedErr
	}

	// Use os.CreateTemp for secure temp file creation (random suffix, no race conditions)
	tmpFile, err := os.CreateTemp("", "mcpserver-*.yaml")
	if err != nil {
		wrappedErr := wrapWithSentinelAndContext(
			ErrCreateTempFileFailed,
			err,
			fmt.Sprintf("failed to create temp file: %v", err),
			map[string]any{"server": name, "namespace": namespace, "component": "server"},
		)
		Error("Failed to create temp file")
		logStructuredError(m.logger, wrappedErr, "Failed to create temp file")
		return wrappedErr
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.Write(manifestBytes); err != nil {
		closeErr := tmpFile.Close()
		if closeErr != nil {
			wrappedErr := wrapWithSentinelAndContext(
				ErrWriteManifestFailed,
				errors.Join(err, closeErr),
				fmt.Sprintf("failed to write manifest: %v; failed to close temp file: %v", err, closeErr),
				map[string]any{"server": name, "namespace": namespace, "component": "server"},
			)
			Error("Failed to write manifest")
			logStructuredError(m.logger, wrappedErr, "Failed to write manifest")
			return wrappedErr
		}
		wrappedErr := wrapWithSentinelAndContext(
			ErrWriteManifestFailed,
			err,
			fmt.Sprintf("failed to write manifest: %v", err),
			map[string]any{"server": name, "namespace": namespace, "component": "server"},
		)
		Error("Failed to write manifest")
		logStructuredError(m.logger, wrappedErr, "Failed to write manifest")
		return wrappedErr
	}
	if err := tmpFile.Close(); err != nil {
		wrappedErr := wrapWithSentinelAndContext(
			ErrCloseTempFileFailed,
			err,
			fmt.Sprintf("failed to close temp file: %v", err),
			map[string]any{"server": name, "namespace": namespace, "component": "server"},
		)
		Error("Failed to close temp file")
		logStructuredError(m.logger, wrappedErr, "Failed to close temp file")
		return wrappedErr
	}

	// #nosec G204 -- tmpPath is from os.CreateTemp, kubectl is a fixed command.
	if err := m.kubectl.RunWithOutput([]string{"apply", "-f", tmpPath}, os.Stdout, os.Stderr); err != nil {
		wrappedErr := wrapWithSentinelAndContext(
			ErrCreateServerFailed,
			err,
			fmt.Sprintf("failed to create server %q: %v", name, err),
			map[string]any{"server": name, "namespace": namespace, "image": image, "component": "server"},
		)
		Error("Failed to create server")
		logStructuredError(m.logger, wrappedErr, "Failed to create server")
		return wrappedErr
	}
	return nil
}

// CreateServerFromFile creates an MCP server from a YAML file.
func (m *ServerManager) CreateServerFromFile(file string) error {
	// Validate file path exists and is a regular file
	absPath, err := filepath.Abs(file)
	if err != nil {
		wrappedErr := wrapWithSentinel(ErrInvalidFilePath, err, fmt.Sprintf("invalid file path: %v", err))
		Error("Invalid file path")
		logStructuredError(m.logger, wrappedErr, "Invalid file path")
		return wrappedErr
	}

	info, err := os.Stat(absPath)
	if err != nil {
		wrappedErr := wrapWithSentinel(ErrFileNotAccessible, err, fmt.Sprintf("cannot access file %q: %v", file, err))
		Error("Cannot access file")
		logStructuredError(m.logger, wrappedErr, "Cannot access file")
		return wrappedErr
	}
	if info.IsDir() {
		err := newWithSentinel(ErrFileIsDirectory, fmt.Sprintf("path %q is a directory, not a file", file))
		Error("Path is a directory")
		logStructuredError(m.logger, err, "Path is a directory")
		return err
	}

	// #nosec G204 -- execCommand passes arguments directly without shell interpretation;
	// file path validated above (exists, is regular file); kubectl validates manifest contents.
	if err := m.kubectl.RunWithOutput([]string{"apply", "-f", absPath}, os.Stdout, os.Stderr); err != nil {
		wrappedErr := wrapWithSentinelAndContext(
			ErrCreateServerFailed,
			err,
			fmt.Sprintf("failed to create server from file %q: %v", file, err),
			map[string]any{"file": file, "component": "server"},
		)
		Error("Failed to create server from file")
		logStructuredError(m.logger, wrappedErr, "Failed to create server from file")
		return wrappedErr
	}
	return nil
}

// DeleteServer deletes an MCP server.
func (m *ServerManager) DeleteServer(name, namespace string) error {
	name, namespace, err := validateServerInput(name, namespace)
	if err != nil {
		return err
	}

	m.logger.Info("Deleting MCP server", zap.String("name", name))

	// #nosec G204 -- name/namespace validated via validateServerInput.
	if err := m.kubectl.RunWithOutput([]string{"delete", "mcpserver", name, "-n", namespace}, os.Stdout, os.Stderr); err != nil {
		wrappedErr := wrapWithSentinelAndContext(
			ErrDeleteServerFailed,
			err,
			fmt.Sprintf("failed to delete server %q in namespace %q: %v", name, namespace, err),
			map[string]any{"server": name, "namespace": namespace, "component": "server"},
		)
		Error("Failed to delete server")
		logStructuredError(m.logger, wrappedErr, "Failed to delete server")
		return wrappedErr
	}
	return nil
}

// ViewServerLogs views logs from an MCP server.
func (m *ServerManager) ViewServerLogs(name, namespace string, follow bool) error {
	name, namespace, err := validateServerInput(name, namespace)
	if err != nil {
		return err
	}

	args := []string{"logs", "-l", LabelApp + "=" + name, "-n", namespace}
	if follow {
		args = append(args, "-f")
	}

	// #nosec G204 -- name/namespace validated via validateServerInput.
	if err := m.kubectl.RunWithOutput(args, os.Stdout, os.Stderr); err != nil {
		wrappedErr := wrapWithSentinelAndContext(
			ErrViewServerLogsFailed,
			err,
			fmt.Sprintf("failed to view logs for server %q in namespace %q: %v", name, namespace, err),
			map[string]any{"server": name, "namespace": namespace, "component": "server"},
		)
		Error("Failed to view server logs")
		logStructuredError(m.logger, wrappedErr, "Failed to view server logs")
		return wrappedErr
	}
	return nil
}

// ServerStatus shows the status of MCP servers in a namespace.
func (m *ServerManager) ServerStatus(namespace string) error {
	Header(fmt.Sprintf("MCP Servers in %s", namespace))
	DefaultPrinter.Println()

	// Get MCPServer details
	// #nosec G204 -- namespace from CLI flag; kubectl validates namespace names.
	getServersCmd, err := m.kubectl.CommandArgs([]string{"get", "mcpserver", "-n", namespace, "-o", "jsonpath={range .items[*]}{.metadata.name}|{.spec.image}:{.spec.imageTag}|{.spec.replicas}|{.spec.ingressPath}|{.spec.useProvisionedRegistry}{\"\\n\"}{end}"})
	if err != nil {
		return err
	}
	out, err := getServersCmd.CombinedOutput()
	if err != nil {
		errDetails := strings.TrimSpace(string(out))
		if errDetails == "" {
			errDetails = err.Error()
		}
		DefaultPrinter.Println("ERROR: Failed to list MCP servers: " + errDetails)
		wrappedErr := wrapWithSentinelAndContext(
			ErrGetMCPServerFailed,
			err,
			fmt.Sprintf("kubectl get mcpserver failed: %v", err),
			map[string]any{"namespace": namespace, "component": "server"},
		)
		logStructuredError(m.logger, wrappedErr, "Failed to get MCP servers")
		return wrappedErr
	}

	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		Warn("No MCP servers found in namespace " + namespace)
		return nil
	}
	rawLines := strings.Split(trimmed, "\n")
	lines := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
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

	// #nosec G204 -- namespace from CLI flag; fixed label selector.
	podCmd, err := m.kubectl.CommandArgs([]string{"get", "pods", "-n", namespace, "-l", SelectorManagedBy, "-o", "custom-columns=NAME:.metadata.name,READY:.status.containerStatuses[0].ready,STATUS:.status.phase,RESTARTS:.status.containerStatuses[0].restartCount"})
	if err != nil {
		return err
	}
	podOut, err := podCmd.Output()
	if err != nil {
		Warn("Failed to list pods: " + err.Error())
		return nil
	}
	trimmedPods := strings.TrimSpace(string(podOut))
	if trimmedPods == "" {
		return nil
	}
	rawPodLines := strings.Split(trimmedPods, "\n")
	podLines := make([]string, 0, len(rawPodLines))
	for _, line := range rawPodLines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		podLines = append(podLines, line)
	}
	if len(podLines) > 1 {
		podData := [][]string{}
		for _, pl := range podLines {
			podData = append(podData, strings.Fields(pl))
		}
		Table(podData)
	} else {
		Info("No pods found")
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
	if strings.ContainsAny(value, "\r\n\t") {
		return "", newWithSentinel(ErrControlCharsNotAllowed, fmt.Sprintf("%s must not contain control characters", field))
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", newWithSentinel(ErrFieldRequired, fmt.Sprintf("%s is required", field))
	}
	return value, nil
}
