package cli

// This file implements the "server" command for managing MCP server resources.
// It handles creating, listing, viewing, and deleting MCPServer custom resources.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// ServerManager handles MCP server operations with injected dependencies.
type ServerManager struct {
	kubectl *KubectlClient
	logger  *zap.Logger
	// useKube forces kubectl; when false, platform API is used for supported read-only commands when logged in.
	useKube bool
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

With mcp-runtime auth login, list, status, and policy use the platform API when
--use-kube is not set. Create, apply, delete, patch, and logs require kubectl
and a cluster kubeconfig (or --use-kube for those operations).

For building images from source, use 'server build'.
For pushing images, use 'registry push'.`,
	}

	cmd.PersistentFlags().BoolVar(&mgr.useKube, "use-kube", false, "Use kubectl and local kubeconfig instead of the platform API for supported commands")

	cmd.AddCommand(mgr.newServerListCmd())
	cmd.AddCommand(mgr.newServerGetCmd())
	cmd.AddCommand(mgr.newServerCreateCmd())
	cmd.AddCommand(mgr.newServerApplyCmd())
	cmd.AddCommand(mgr.newServerExportCmd())
	cmd.AddCommand(mgr.newServerPatchCmd())
	cmd.AddCommand(mgr.newServerDeleteCmd())
	cmd.AddCommand(mgr.newServerLogsCmd())
	cmd.AddCommand(mgr.newServerStatusCmd())
	cmd.AddCommand(mgr.newServerPolicyCmd())
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

func (m *ServerManager) newServerApplyCmd() *cobra.Command {
	var file string

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply an MCP server manifest",
		RunE: func(cmd *cobra.Command, args []string) error {
			return m.ApplyServerFromFile(file)
		},
	}

	cmd.Flags().StringVar(&file, "file", "", "YAML file with MCPServer manifest")
	_ = cmd.MarkFlagRequired("file")

	return cmd
}

func (m *ServerManager) newServerExportCmd() *cobra.Command {
	var namespace string
	var file string

	cmd := &cobra.Command{
		Use:   "export [name]",
		Short: "Export an MCP server manifest",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return m.ExportServer(args[0], namespace, file)
		},
	}

	cmd.Flags().StringVar(&namespace, "namespace", NamespaceMCPServers, "Namespace")
	cmd.Flags().StringVar(&file, "file", "", "Write the manifest to a file instead of stdout")

	return cmd
}

func (m *ServerManager) newServerPatchCmd() *cobra.Command {
	var namespace string
	var patchType string
	var patch string
	var patchFile string

	cmd := &cobra.Command{
		Use:   "patch [name]",
		Short: "Patch an MCP server manifest",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return m.PatchServer(args[0], namespace, patchType, patch, patchFile)
		},
	}

	cmd.Flags().StringVar(&namespace, "namespace", NamespaceMCPServers, "Namespace")
	cmd.Flags().StringVar(&patchType, "type", "merge", "Patch type (merge|json|strategic)")
	cmd.Flags().StringVar(&patch, "patch", "", "Inline JSON/YAML patch document")
	cmd.Flags().StringVar(&patchFile, "patch-file", "", "Path to a JSON/YAML patch document")

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

func (m *ServerManager) newServerPolicyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "policy",
		Short: "Inspect rendered gateway policy for an MCP server",
	}

	cmd.AddCommand(m.newServerPolicyInspectCmd())

	return cmd
}

func (m *ServerManager) newServerPolicyInspectCmd() *cobra.Command {
	var namespace string

	cmd := &cobra.Command{
		Use:   "inspect [name]",
		Short: "Show the rendered gateway policy document for a server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return m.InspectServerPolicy(args[0], namespace)
		},
	}

	cmd.Flags().StringVar(&namespace, "namespace", NamespaceMCPServers, "Namespace")

	return cmd
}

// ListServers lists all MCP servers in the given namespace.
func (m *ServerManager) ListServers(namespace string) error {
	namespace, err := validateManifestValue("namespace", namespace)
	if err != nil {
		return err
	}

	plat, useK, err := m.platformOrKube()
	if err != nil {
		return err
	}
	if !useK {
		items, err := plat.listRuntimeServers(context.Background(), namespace)
		if err != nil {
			return err
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "NAME\tNAMESPACE\tREADY\tSTATUS\tAGE")
		for _, s := range items {
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", s.Name, s.Namespace, s.Ready, s.Status, s.Age)
		}
		_ = tw.Flush()
		return nil
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

	plat, useK, err := m.platformOrKube()
	if err != nil {
		return err
	}
	if !useK {
		items, err := plat.listRuntimeServers(context.Background(), namespace)
		if err != nil {
			return err
		}
		for _, s := range items {
			if s.Name == name && s.Namespace == namespace {
				b, _ := json.MarshalIndent(s, "", "  ")
				_, _ = os.Stdout.Write(append(b, '\n'))
				_, _ = os.Stderr.WriteString("# For the full MCPServer YAML, use mcp-runtime server get --use-kube ... with kubectl.\n")
				return nil
			}
		}
		return newWithSentinel(ErrGetMCPServerFailed, fmt.Sprintf("server %q not found in namespace %q (platform API)", name, namespace))
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
	if err := m.requireKubectlForMutation(); err != nil {
		return err
	}
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
			IngressPath: "/" + name + "/mcp",
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

	if err := applyManifestContent(m.kubectl, string(manifestBytes)); err != nil {
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

// ApplyServerFromFile applies an MCPServer manifest from disk.
func (m *ServerManager) ApplyServerFromFile(file string) error {
	if err := m.requireKubectlForMutation(); err != nil {
		return err
	}
	if err := applyManifestFromFile(m.kubectl, file, os.Stdout, os.Stderr); err != nil {
		wrappedErr := wrapWithSentinelAndContext(
			nil,
			err,
			fmt.Sprintf("failed to apply server manifest from file %q: %v", file, err),
			map[string]any{"file": file, "component": "server"},
		)
		Error("Failed to apply server manifest")
		logStructuredError(m.logger, wrappedErr, "Failed to apply server manifest")
		return wrappedErr
	}
	return nil
}

// CreateServerFromFile creates an MCP server from a YAML file.
func (m *ServerManager) CreateServerFromFile(file string) error {
	if err := m.requireKubectlForMutation(); err != nil {
		return err
	}
	absPath, err := resolveRegularFilePath(file)
	if err != nil {
		Error("Cannot access file")
		logStructuredError(m.logger, err, "Cannot access file")
		return err
	}

	manifestBytes, err := readFileAtPath(absPath)
	if err != nil {
		wrappedErr := wrapWithSentinel(ErrFileNotAccessible, err, fmt.Sprintf("cannot read file %q: %v", file, err))
		Error("Cannot access file")
		logStructuredError(m.logger, wrappedErr, "Cannot access file")
		return wrappedErr
	}

	if err := applyManifestContent(m.kubectl, string(manifestBytes)); err != nil {
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

// ExportServer exports an MCPServer manifest to stdout or a file.
func (m *ServerManager) ExportServer(name, namespace, file string) error {
	if err := m.requireKubectlForMutation(); err != nil {
		return err
	}
	name, namespace, err := validateServerInput(name, namespace)
	if err != nil {
		return err
	}

	cmd, err := m.kubectl.CommandArgs([]string{"get", "mcpserver", name, "-n", namespace, "-o", "yaml"})
	if err != nil {
		return err
	}
	output, execErr := cmd.CombinedOutput()
	if execErr != nil {
		wrappedErr := wrapWithSentinelAndContext(
			nil,
			execErr,
			fmt.Sprintf("failed to export server %q in namespace %q: %s", name, namespace, commandErrorDetail(string(output), execErr)),
			map[string]any{"server": name, "namespace": namespace, "component": "server"},
		)
		Error("Failed to export server")
		logStructuredError(m.logger, wrappedErr, "Failed to export server")
		return wrappedErr
	}

	if file != "" {
		if err := writeOutputFile(file, output); err != nil {
			wrappedErr := wrapWithSentinelAndContext(
				nil,
				err,
				fmt.Sprintf("failed to write server manifest to %q: %v", file, err),
				map[string]any{"server": name, "namespace": namespace, "file": file, "component": "server"},
			)
			Error("Failed to write server manifest")
			logStructuredError(m.logger, wrappedErr, "Failed to write server manifest")
			return wrappedErr
		}
		return nil
	}

	if _, err := os.Stdout.Write(output); err != nil {
		wrappedErr := wrapWithSentinelAndContext(
			nil,
			err,
			fmt.Sprintf("failed to write server manifest to stdout: %v", err),
			map[string]any{"server": name, "namespace": namespace, "component": "server"},
		)
		Error("Failed to write server manifest")
		logStructuredError(m.logger, wrappedErr, "Failed to write server manifest")
		return wrappedErr
	}
	return nil
}

// PatchServer patches an existing MCPServer resource using merge/json/strategic patch types.
func (m *ServerManager) PatchServer(name, namespace, patchType, patch, patchFile string) error {
	if err := m.requireKubectlForMutation(); err != nil {
		return err
	}
	name, namespace, err := validateServerInput(name, namespace)
	if err != nil {
		return err
	}

	patchType = strings.TrimSpace(strings.ToLower(patchType))
	switch patchType {
	case "merge", "json", "strategic":
	default:
		return newWithSentinel(nil, fmt.Sprintf("unsupported patch type %q (use merge|json|strategic)", patchType))
	}

	inlinePatch := strings.TrimSpace(patch)
	patchFile = strings.TrimSpace(patchFile)
	switch {
	case inlinePatch == "" && patchFile == "":
		return newWithSentinel(nil, "either --patch or --patch-file is required")
	case inlinePatch != "" && patchFile != "":
		return newWithSentinel(nil, "use either --patch or --patch-file, not both")
	}

	normalizedPatch := inlinePatch
	if patchFile != "" {
		normalizedPatch, err = normalizePatchFile(patchFile)
	} else {
		normalizedPatch, err = normalizePatchDocument(inlinePatch)
	}
	if err != nil {
		wrappedErr := wrapWithSentinelAndContext(
			nil,
			err,
			fmt.Sprintf("failed to prepare patch for server %q: %v", name, err),
			map[string]any{"server": name, "namespace": namespace, "patch_type": patchType, "component": "server"},
		)
		Error("Failed to prepare server patch")
		logStructuredError(m.logger, wrappedErr, "Failed to prepare server patch")
		return wrappedErr
	}

	args := []string{"patch", "mcpserver", name, "-n", namespace, "--type", patchType, "--patch", normalizedPatch}
	if err := m.kubectl.RunWithOutput(args, os.Stdout, os.Stderr); err != nil {
		wrappedErr := wrapWithSentinelAndContext(
			nil,
			err,
			fmt.Sprintf("failed to patch server %q in namespace %q: %v", name, namespace, err),
			map[string]any{"server": name, "namespace": namespace, "patch_type": patchType, "component": "server"},
		)
		Error("Failed to patch server")
		logStructuredError(m.logger, wrappedErr, "Failed to patch server")
		return wrappedErr
	}

	return nil
}

// InspectServerPolicy prints the rendered gateway policy ConfigMap content for a server.
func (m *ServerManager) InspectServerPolicy(name, namespace string) error {
	name, namespace, err := validateServerInput(name, namespace)
	if err != nil {
		return err
	}

	plat, useK, err := m.platformOrKube()
	if err != nil {
		return err
	}
	if !useK {
		b, err := plat.getRuntimePolicy(context.Background(), namespace, name)
		if err != nil {
			wrappedErr := wrapWithSentinelAndContext(
				nil,
				err,
				fmt.Sprintf("platform API policy for server %q: %v", name, err),
				map[string]any{"server": name, "namespace": namespace, "component": "server"},
			)
			Error("Failed to read server policy")
			logStructuredError(m.logger, wrappedErr, "Failed to read server policy")
			return wrappedErr
		}
		var pretty map[string]interface{}
		if err := json.Unmarshal(b, &pretty); err != nil {
			_, _ = os.Stdout.Write(b)
			_, _ = os.Stdout.WriteString("\n")
		} else {
			enc, _ := json.MarshalIndent(pretty, "", "  ")
			_, _ = os.Stdout.Write(append(enc, '\n'))
		}
		return nil
	}

	configMapName := name + "-gateway-policy"
	args := []string{"get", "configmap", configMapName, "-n", namespace, "-o", `go-template={{index .data "policy.json"}}`}
	cmd, err := m.kubectl.CommandArgs(args)
	if err != nil {
		return err
	}
	output, execErr := cmd.CombinedOutput()
	if execErr != nil {
		wrappedErr := wrapWithSentinelAndContext(
			nil,
			execErr,
			fmt.Sprintf("failed to inspect rendered policy for server %q in namespace %q: %s", name, namespace, commandErrorDetail(string(output), execErr)),
			map[string]any{"server": name, "namespace": namespace, "component": "server"},
		)
		Error("Failed to inspect server policy")
		logStructuredError(m.logger, wrappedErr, "Failed to inspect server policy")
		return wrappedErr
	}

	if len(output) > 0 {
		if _, err := os.Stdout.Write(output); err != nil {
			wrappedErr := wrapWithSentinelAndContext(
				nil,
				err,
				fmt.Sprintf("failed to write rendered policy to stdout: %v", err),
				map[string]any{"server": name, "namespace": namespace, "component": "server"},
			)
			Error("Failed to inspect server policy")
			logStructuredError(m.logger, wrappedErr, "Failed to inspect server policy")
			return wrappedErr
		}
	}
	if len(output) == 0 || output[len(output)-1] != '\n' {
		fmt.Fprintln(os.Stdout)
	}

	return nil
}

// DeleteServer deletes an MCP server.
func (m *ServerManager) DeleteServer(name, namespace string) error {
	if err := m.requireKubectlForMutation(); err != nil {
		return err
	}
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
	if err := m.requireKubectlForMutation(); err != nil {
		return err
	}
	name, namespace, err := validateServerInput(name, namespace)
	if err != nil {
		return err
	}

	args := []string{"logs", "-l", LabelApp + "=" + name, "-n", namespace, "--all-containers=true"}
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

	plat, useK, err := m.platformOrKube()
	if err != nil {
		return err
	}
	if !useK {
		items, err := plat.listRuntimeServers(context.Background(), namespace)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			Warn("No MCP servers found in namespace " + namespace)
		} else {
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(tw, "NAME\tNAMESPACE\tREADY\tSTATUS\tAGE")
			for _, s := range items {
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", s.Name, s.Namespace, s.Ready, s.Status, s.Age)
			}
			_ = tw.Flush()
		}
		Info("Pod details need kubectl. Run with --use-kube for full status including pods.")
		return nil
	}

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
