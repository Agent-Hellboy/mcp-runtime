package server

// This file implements the "server" command for managing MCP server resources.
// It handles creating, listing, viewing, and deleting MCPServer custom resources.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	mcpv1alpha1 "mcp-runtime/api/v1alpha1"
	"mcp-runtime/internal/cli/core"
	"mcp-runtime/internal/cli/kube"
	"mcp-runtime/internal/cli/kubeerr"
	"mcp-runtime/internal/cli/platformapi"
)

// ServerManager handles MCP server operations with injected dependencies.
type ServerManager struct {
	kubectl *core.KubectlClient
	logger  *zap.Logger
	// useKube forces kubectl; when false, platform API is used for supported read-only commands when logged in.
	useKube bool
}

// NewServerManager creates a ServerManager with the given dependencies.
func NewServerManager(kubectl *core.KubectlClient, logger *zap.Logger) *ServerManager {
	return &ServerManager{
		kubectl: kubectl,
		logger:  logger,
	}
}

// DefaultServerManager returns a ServerManager using the default kubectl client.
func DefaultServerManager(logger *zap.Logger) *ServerManager {
	return NewServerManager(core.DefaultKubectlClient(), logger)
}

// BindUseKubeFlag wires the shared --use-kube flag onto the command.
func (m *ServerManager) BindUseKubeFlag(cmd *cobra.Command) {
	cmd.PersistentFlags().BoolVar(&m.useKube, "use-kube", false, "Use kubectl and local kubeconfig instead of the platform API for supported commands")
}

func (m *ServerManager) requireKubectlForMutation() error {
	_, useK, err := platformapi.ResolvePlatformOrKube(m.useKube)
	if err != nil {
		return err
	}
	if !useK {
		return core.NewWithSentinel(nil, "this command requires kubectl and a cluster kubeconfig, or set --use-kube when you use kubectl alongside platform auth. Use mcp-runtime auth for API-backed list, status, and policy when kubeconfig is not used.")
	}
	return nil
}

// Logger exposes the manager logger to foldered command packages.
func (m *ServerManager) Logger() *zap.Logger {
	return m.logger
}

// ListServers lists all MCP servers in the given namespace.
func (m *ServerManager) ListServers(namespace, team string) error {
	namespace = strings.TrimSpace(namespace)
	team = strings.TrimSpace(team)
	if namespace != "" && team != "" {
		return core.NewWithSentinel(nil, "use either --namespace or --team, not both")
	}
	plat, useK, err := platformapi.ResolvePlatformOrKube(m.useKube)
	if err != nil {
		return err
	}
	if useK && team != "" {
		return core.NewWithSentinel(nil, "cannot use --team with --use-kube")
	}
	if !useK {
		if team != "" {
			t, err := plat.GetTeam(context.Background(), team)
			if err != nil {
				return err
			}
			namespace = t.Namespace
		}
		if namespace != "" {
			namespace, err = validateManifestValue("namespace", namespace)
			if err != nil {
				return err
			}
		}
		items, err := plat.ListRuntimeServers(context.Background(), namespace)
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
	if namespace == "" {
		namespace = core.NamespaceMCPServers
	}
	namespace, err = validateManifestValue("namespace", namespace)
	if err != nil {
		return err
	}

	// #nosec G204 -- namespace validated above; kubectl validates resource names.
	if err := m.kubectl.RunWithOutput([]string{"get", "mcpserver", "-n", namespace}, os.Stdout, os.Stderr); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrListServersFailed,
			err,
			fmt.Sprintf("failed to list servers in namespace %q: %v", namespace, err),
			map[string]any{"namespace": namespace, "component": "server"},
		)
		core.Error("Failed to list servers")
		core.LogStructuredError(m.logger, wrappedErr, "Failed to list servers")
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

	plat, useK, err := platformapi.ResolvePlatformOrKube(m.useKube)
	if err != nil {
		return err
	}
	if !useK {
		items, err := plat.ListRuntimeServers(context.Background(), namespace)
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
		return core.NewWithSentinel(core.ErrGetMCPServerFailed, fmt.Sprintf("server %q not found in namespace %q (platform API)", name, namespace))
	}

	// #nosec G204 -- name/namespace validated via validateServerInput.
	if err := m.kubectl.RunWithOutput([]string{"get", "mcpserver", name, "-n", namespace, "-o", "yaml"}, os.Stdout, os.Stderr); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrGetMCPServerFailed,
			err,
			fmt.Sprintf("failed to get server %q in namespace %q: %v", name, namespace, err),
			map[string]any{"server": name, "namespace": namespace, "component": "server"},
		)
		core.Error("Failed to get server")
		core.LogStructuredError(m.logger, wrappedErr, "Failed to get server")
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
		return core.ErrImageRequired
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
			Port:        core.GetDefaultServerPort(),
			ServicePort: 80,
			IngressPath: "/" + name + "/mcp",
		},
	}

	manifestBytes, err := yaml.Marshal(manifest)
	if err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrMarshalManifestFailed,
			err,
			fmt.Sprintf("failed to marshal manifest: %v", err),
			map[string]any{"server": name, "namespace": namespace, "component": "server"},
		)
		core.Error("Failed to marshal manifest")
		core.LogStructuredError(m.logger, wrappedErr, "Failed to marshal manifest")
		return wrappedErr
	}

	if err := kube.ApplyManifestContent(m.kubectl.CommandArgs, string(manifestBytes)); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrCreateServerFailed,
			err,
			fmt.Sprintf("failed to create server %q: %v", name, err),
			map[string]any{"server": name, "namespace": namespace, "image": image, "component": "server"},
		)
		core.Error("Failed to create server")
		core.LogStructuredError(m.logger, wrappedErr, "Failed to create server")
		return wrappedErr
	}
	return nil
}

func (m *ServerManager) DeployServer(name, namespace, team, image, imageTag string, replicas, port, servicePort int32) error {
	if m.useKube {
		return core.NewWithSentinel(nil, "server deploy uses the platform API; remove --use-kube")
	}
	name, err := validateManifestValue("name", name)
	if err != nil {
		return err
	}
	image, err = validateManifestValue("image", image)
	if err != nil {
		return err
	}
	imageTag, err = validateManifestValue("tag", imageTag)
	if err != nil {
		return err
	}
	namespace = strings.TrimSpace(namespace)
	team = strings.TrimSpace(team)
	if namespace != "" && team != "" {
		return core.NewWithSentinel(nil, "use either --namespace or --team, not both")
	}
	plat, err := platformapi.NewPlatformClient()
	if err != nil {
		return err
	}
	if team != "" {
		t, err := plat.GetTeam(context.Background(), team)
		if err != nil {
			return err
		}
		namespace = t.Namespace
	}
	if namespace != "" {
		namespace, err = validateManifestValue("namespace", namespace)
		if err != nil {
			return err
		}
	}
	spec := mcpv1alpha1.MCPServerSpec{
		Image:            image,
		ImageTag:         imageTag,
		Replicas:         &replicas,
		Port:             port,
		ServicePort:      servicePort,
		PublicPathPrefix: name,
		IngressPath:      "/" + name + "/mcp",
	}
	applied, err := plat.ApplyRuntimeServer(context.Background(), name, namespace, spec)
	if err != nil {
		return err
	}
	core.Success(fmt.Sprintf("Deployed server %s in namespace %s", applied.Name, applied.Namespace))
	return nil
}

// ApplyServerFromFile applies an MCPServer manifest from disk.
func (m *ServerManager) ApplyServerFromFile(file string) error {
	if err := m.requireKubectlForMutation(); err != nil {
		return err
	}
	if err := kube.ApplyManifestFromFile(m.kubectl.CommandArgs, file, os.Stdout, os.Stderr); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			nil,
			err,
			fmt.Sprintf("failed to apply server manifest from file %q: %v", file, err),
			map[string]any{"file": file, "component": "server"},
		)
		core.Error("Failed to apply server manifest")
		core.LogStructuredError(m.logger, wrappedErr, "Failed to apply server manifest")
		return wrappedErr
	}
	return nil
}

// CreateServerFromFile creates an MCP server from a YAML file.
func (m *ServerManager) CreateServerFromFile(file string) error {
	if err := m.requireKubectlForMutation(); err != nil {
		return err
	}
	absPath, err := kube.ResolveRegularFilePath(file)
	if err != nil {
		core.Error("Cannot access file")
		core.LogStructuredError(m.logger, err, "Cannot access file")
		return err
	}

	manifestBytes, err := kube.ReadFileAtPath(absPath)
	if err != nil {
		wrappedErr := core.WrapWithSentinel(core.ErrFileNotAccessible, err, fmt.Sprintf("cannot read file %q: %v", file, err))
		core.Error("Cannot access file")
		core.LogStructuredError(m.logger, wrappedErr, "Cannot access file")
		return wrappedErr
	}

	if err := kube.ApplyManifestContent(m.kubectl.CommandArgs, string(manifestBytes)); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrCreateServerFailed,
			err,
			fmt.Sprintf("failed to create server from file %q: %v", file, err),
			map[string]any{"file": file, "component": "server"},
		)
		core.Error("Failed to create server from file")
		core.LogStructuredError(m.logger, wrappedErr, "Failed to create server from file")
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
		wrappedErr := core.WrapWithSentinelAndContext(
			nil,
			execErr,
			fmt.Sprintf("failed to export server %q in namespace %q: %s", name, namespace, kubeerr.CommandDetail(string(output), execErr)),
			map[string]any{"server": name, "namespace": namespace, "component": "server"},
		)
		core.Error("Failed to export server")
		core.LogStructuredError(m.logger, wrappedErr, "Failed to export server")
		return wrappedErr
	}

	if file != "" {
		if err := kube.WriteOutputFile(file, output); err != nil {
			wrappedErr := core.WrapWithSentinelAndContext(
				nil,
				err,
				fmt.Sprintf("failed to write server manifest to %q: %v", file, err),
				map[string]any{"server": name, "namespace": namespace, "file": file, "component": "server"},
			)
			core.Error("Failed to write server manifest")
			core.LogStructuredError(m.logger, wrappedErr, "Failed to write server manifest")
			return wrappedErr
		}
		return nil
	}

	if _, err := os.Stdout.Write(output); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			nil,
			err,
			fmt.Sprintf("failed to write server manifest to stdout: %v", err),
			map[string]any{"server": name, "namespace": namespace, "component": "server"},
		)
		core.Error("Failed to write server manifest")
		core.LogStructuredError(m.logger, wrappedErr, "Failed to write server manifest")
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
		return core.NewWithSentinel(nil, fmt.Sprintf("unsupported patch type %q (use merge|json|strategic)", patchType))
	}

	inlinePatch := strings.TrimSpace(patch)
	patchFile = strings.TrimSpace(patchFile)
	switch {
	case inlinePatch == "" && patchFile == "":
		return core.NewWithSentinel(nil, "either --patch or --patch-file is required")
	case inlinePatch != "" && patchFile != "":
		return core.NewWithSentinel(nil, "use either --patch or --patch-file, not both")
	}

	normalizedPatch := inlinePatch
	if patchFile != "" {
		normalizedPatch, err = kube.NormalizePatchFile(patchFile)
	} else {
		normalizedPatch, err = kube.NormalizePatchDocument(inlinePatch)
	}
	if err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			nil,
			err,
			fmt.Sprintf("failed to prepare patch for server %q: %v", name, err),
			map[string]any{"server": name, "namespace": namespace, "patch_type": patchType, "component": "server"},
		)
		core.Error("Failed to prepare server patch")
		core.LogStructuredError(m.logger, wrappedErr, "Failed to prepare server patch")
		return wrappedErr
	}

	args := []string{"patch", "mcpserver", name, "-n", namespace, "--type", patchType, "--patch", normalizedPatch}
	if err := m.kubectl.RunWithOutput(args, os.Stdout, os.Stderr); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			nil,
			err,
			fmt.Sprintf("failed to patch server %q in namespace %q: %v", name, namespace, err),
			map[string]any{"server": name, "namespace": namespace, "patch_type": patchType, "component": "server"},
		)
		core.Error("Failed to patch server")
		core.LogStructuredError(m.logger, wrappedErr, "Failed to patch server")
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

	plat, useK, err := platformapi.ResolvePlatformOrKube(m.useKube)
	if err != nil {
		return err
	}
	if !useK {
		b, err := plat.GetRuntimePolicy(context.Background(), namespace, name)
		if err != nil {
			wrappedErr := core.WrapWithSentinelAndContext(
				nil,
				err,
				fmt.Sprintf("platform API policy for server %q: %v", name, err),
				map[string]any{"server": name, "namespace": namespace, "component": "server"},
			)
			core.Error("Failed to read server policy")
			core.LogStructuredError(m.logger, wrappedErr, "Failed to read server policy")
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
		wrappedErr := core.WrapWithSentinelAndContext(
			nil,
			execErr,
			fmt.Sprintf("failed to inspect rendered policy for server %q in namespace %q: %s", name, namespace, kubeerr.CommandDetail(string(output), execErr)),
			map[string]any{"server": name, "namespace": namespace, "component": "server"},
		)
		core.Error("Failed to inspect server policy")
		core.LogStructuredError(m.logger, wrappedErr, "Failed to inspect server policy")
		return wrappedErr
	}

	if len(output) > 0 {
		if _, err := os.Stdout.Write(output); err != nil {
			wrappedErr := core.WrapWithSentinelAndContext(
				nil,
				err,
				fmt.Sprintf("failed to write rendered policy to stdout: %v", err),
				map[string]any{"server": name, "namespace": namespace, "component": "server"},
			)
			core.Error("Failed to inspect server policy")
			core.LogStructuredError(m.logger, wrappedErr, "Failed to inspect server policy")
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
	name, namespace, err := validateServerInput(name, namespace)
	if err != nil {
		return err
	}

	plat, useK, err := platformapi.ResolvePlatformOrKube(m.useKube)
	if err != nil {
		return err
	}
	if !useK {
		if err := plat.DeleteRuntimeServer(context.Background(), namespace, name); err != nil {
			return err
		}
		core.Success(fmt.Sprintf("Retired server %s in namespace %s", name, namespace))
		return nil
	}

	m.logger.Info("Deleting MCP server", zap.String("name", name))

	// #nosec G204 -- name/namespace validated via validateServerInput.
	if err := m.kubectl.RunWithOutput([]string{"delete", "mcpserver", name, "-n", namespace}, os.Stdout, os.Stderr); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrDeleteServerFailed,
			err,
			fmt.Sprintf("failed to delete server %q in namespace %q: %v", name, namespace, err),
			map[string]any{"server": name, "namespace": namespace, "component": "server"},
		)
		core.Error("Failed to delete server")
		core.LogStructuredError(m.logger, wrappedErr, "Failed to delete server")
		return wrappedErr
	}
	return nil
}

// ViewServerLogs views logs from an MCP server.
func (m *ServerManager) ViewServerLogs(name, namespace string, follow, previous bool, tail int, since string) error {
	if err := m.requireKubectlForMutation(); err != nil {
		return err
	}
	name, namespace, err := validateServerInput(name, namespace)
	if err != nil {
		return err
	}

	args := []string{
		"logs",
		"-l", core.LabelApp + "=" + name,
		"-n", namespace,
		"--all-containers=true",
		"--tail", strconv.Itoa(tail),
	}
	if follow {
		args = append(args, "-f")
	}
	if previous {
		args = append(args, "--previous")
	}
	if s := strings.TrimSpace(since); s != "" {
		args = append(args, "--since", s)
	}

	// #nosec G204 -- name/namespace validated via validateServerInput.
	if err := m.kubectl.RunWithOutput(args, os.Stdout, os.Stderr); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrViewServerLogsFailed,
			err,
			fmt.Sprintf("failed to view logs for server %q in namespace %q: %v", name, namespace, err),
			map[string]any{"server": name, "namespace": namespace, "component": "server"},
		)
		core.Error("Failed to view server logs")
		core.LogStructuredError(m.logger, wrappedErr, "Failed to view server logs")
		return wrappedErr
	}
	return nil
}

// ServerStatus shows the status of MCP servers in a namespace.
func (m *ServerManager) ServerStatus(namespace string) error {
	core.Header(fmt.Sprintf("MCP Servers in %s", namespace))
	core.DefaultPrinter.Println()

	plat, useK, err := platformapi.ResolvePlatformOrKube(m.useKube)
	if err != nil {
		return err
	}
	if !useK {
		items, err := plat.ListRuntimeServers(context.Background(), namespace)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			core.Warn("No MCP servers found in namespace " + namespace)
		} else {
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(tw, "NAME\tNAMESPACE\tREADY\tSTATUS\tAGE")
			for _, s := range items {
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", s.Name, s.Namespace, s.Ready, s.Status, s.Age)
			}
			_ = tw.Flush()
		}
		core.Info("Pod details need kubectl. Run with --use-kube for full status including pods.")
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
		core.DefaultPrinter.Println("ERROR: Failed to list MCP servers: " + errDetails)
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrGetMCPServerFailed,
			err,
			fmt.Sprintf("kubectl get mcpserver failed: %v", err),
			map[string]any{"namespace": namespace, "component": "server"},
		)
		core.LogStructuredError(m.logger, wrappedErr, "Failed to get MCP servers")
		return wrappedErr
	}

	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		core.Warn("No MCP servers found in namespace " + namespace)
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
		core.Warn("No MCP servers found in namespace " + namespace)
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
		core.TableBoxed(tableData)
	}

	// Pod status section
	core.DefaultPrinter.Println()
	core.Section("Pod Status")

	// #nosec G204 -- namespace from CLI flag; fixed label selector.
	podCmd, err := m.kubectl.CommandArgs([]string{"get", "pods", "-n", namespace, "-l", core.SelectorManagedBy, "-o", "custom-columns=NAME:.metadata.name,READY:.status.containerStatuses[0].ready,STATUS:.status.phase,RESTARTS:.status.containerStatuses[0].restartCount"})
	if err != nil {
		return err
	}
	podOut, err := podCmd.Output()
	if err != nil {
		core.Warn("Failed to list pods: " + err.Error())
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
		core.Table(podData)
	} else {
		core.Info("No pods found")
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
