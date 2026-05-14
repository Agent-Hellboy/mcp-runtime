// Package server owns routing for the server top-level command.
package server

import (
	"math"

	"github.com/spf13/cobra"

	"mcp-runtime/internal/cli/core"
)

// New returns the server command.
func New(runtime *core.Runtime) *cobra.Command {
	return NewWithManager(NewServerManager(runtime.KubectlClient(), runtime.Logger()))
}

// NewWithManager returns the server command using the provided manager.
func NewWithManager(mgr *ServerManager) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Manage MCP servers",
		Long: `Commands for managing MCP server deployments.

With mcp-runtime auth login, list, status, policy, deploy, and delete use the
platform API when --use-kube is not set. Create, apply, patch, and logs require
kubectl and a cluster kubeconfig (or --use-kube for those operations).

For building images from source, use 'server build'.
For pushing images, use 'registry push'.`,
	}

	mgr.BindUseKubeFlag(cmd)

	var namespace string
	var team string
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List MCP servers",
		Long:  "List all MCP server deployments",
		RunE: func(cmd *cobra.Command, args []string) error {
			return mgr.ListServers(namespace, team)
		},
	}
	listCmd.Flags().StringVar(&namespace, "namespace", "", "Namespace to list servers from")
	listCmd.Flags().StringVar(&team, "team", "", "Team slug to resolve namespace from via the platform API")

	var getNamespace string
	getCmd := &cobra.Command{
		Use:   "get [name]",
		Short: "Get MCP server details",
		Long:  "Get detailed information about an MCP server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return mgr.GetServer(args[0], getNamespace)
		},
	}
	getCmd.Flags().StringVar(&getNamespace, "namespace", core.NamespaceMCPServers, "Namespace")

	var createNamespace string
	var image string
	var imageTag string
	var file string
	createCmd := &cobra.Command{
		Use:   "create [name]",
		Short: "Create an MCP server",
		Long:  "Create a new MCP server deployment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if file != "" {
				return mgr.CreateServerFromFile(file)
			}
			return mgr.CreateServer(args[0], createNamespace, image, imageTag)
		},
	}
	createCmd.Flags().StringVar(&createNamespace, "namespace", core.NamespaceMCPServers, "Namespace")
	createCmd.Flags().StringVar(&image, "image", "", "Container image")
	createCmd.Flags().StringVar(&imageTag, "tag", "latest", "Image tag")
	createCmd.Flags().StringVar(&file, "file", "", "YAML file with server spec")

	var applyFile string
	applyCmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply an MCP server manifest",
		RunE: func(cmd *cobra.Command, args []string) error {
			return mgr.ApplyServerFromFile(applyFile)
		},
	}
	applyCmd.Flags().StringVar(&applyFile, "file", "", "YAML file with MCPServer manifest")
	_ = applyCmd.MarkFlagRequired("file")

	var deployNamespace string
	var deployTeam string
	var deployImage string
	var deployTag string
	var deployReplicas int32
	var deployPort int32
	var deployServicePort int32
	deployCmd := &cobra.Command{
		Use:   "deploy [name]",
		Short: "Deploy an MCP server through the platform API",
		Long:  "Apply an MCPServer resource using the authenticated platform API identity.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return mgr.DeployServer(args[0], deployNamespace, deployTeam, deployImage, deployTag, deployReplicas, deployPort, deployServicePort)
		},
	}
	deployCmd.Flags().StringVar(&deployNamespace, "namespace", "", "Target namespace (optional when --team is provided)")
	deployCmd.Flags().StringVar(&deployTeam, "team", "", "Team slug to resolve target namespace")
	deployCmd.Flags().StringVar(&deployImage, "image", "", "Container image repository")
	deployCmd.Flags().StringVar(&deployTag, "tag", "latest", "Container image tag")
	deployCmd.Flags().Int32Var(&deployReplicas, "replicas", 1, "Replica count")
	deployCmd.Flags().Int32Var(&deployPort, "port", defaultDeployPort(), "Container port")
	deployCmd.Flags().Int32Var(&deployServicePort, "service-port", 80, "Service port")
	_ = deployCmd.MarkFlagRequired("image")

	var exportNamespace string
	var exportFile string
	exportCmd := &cobra.Command{
		Use:   "export [name]",
		Short: "Export an MCP server manifest",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return mgr.ExportServer(args[0], exportNamespace, exportFile)
		},
	}
	exportCmd.Flags().StringVar(&exportNamespace, "namespace", core.NamespaceMCPServers, "Namespace")
	exportCmd.Flags().StringVar(&exportFile, "file", "", "Write the manifest to a file instead of stdout")

	var patchNamespace string
	var patchType string
	var patch string
	var patchFile string
	patchCmd := &cobra.Command{
		Use:   "patch [name]",
		Short: "Patch an MCP server manifest",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return mgr.PatchServer(args[0], patchNamespace, patchType, patch, patchFile)
		},
	}
	patchCmd.Flags().StringVar(&patchNamespace, "namespace", core.NamespaceMCPServers, "Namespace")
	patchCmd.Flags().StringVar(&patchType, "type", "merge", "Patch type (merge|json|strategic)")
	patchCmd.Flags().StringVar(&patch, "patch", "", "Inline JSON/YAML patch document")
	patchCmd.Flags().StringVar(&patchFile, "patch-file", "", "Path to a JSON/YAML patch document")

	var deleteNamespace string
	deleteCmd := &cobra.Command{
		Use:   "delete [name]",
		Short: "Delete an MCP server",
		Long:  "Delete an MCP server deployment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return mgr.DeleteServer(args[0], deleteNamespace)
		},
	}
	deleteCmd.Flags().StringVar(&deleteNamespace, "namespace", core.NamespaceMCPServers, "Namespace")

	var logsNamespace string
	var follow bool
	var previous bool
	var tail int
	var since string
	logsCmd := &cobra.Command{
		Use:   "logs [name]",
		Short: "View server logs",
		Long:  "View logs from an MCP server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return mgr.ViewServerLogs(args[0], logsNamespace, follow, previous, tail, since)
		},
	}
	logsCmd.Flags().StringVar(&logsNamespace, "namespace", core.NamespaceMCPServers, "Namespace")
	logsCmd.Flags().BoolVar(&follow, "follow", false, "Follow log output")
	logsCmd.Flags().BoolVar(&previous, "previous", false, "Show logs from the previous container instance")
	logsCmd.Flags().IntVar(&tail, "tail", 200, "Number of recent log lines to show (-1 for all)")
	logsCmd.Flags().StringVar(&since, "since", "", "Only return logs newer than a relative duration like 5m or 1h")

	var statusNamespace string
	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show MCP server runtime status (pods, images, pull secrets)",
		Long:  "List MCPServer resources with their Deployment/pod status, image, and pull secrets.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return mgr.ServerStatus(statusNamespace)
		},
	}
	statusCmd.Flags().StringVar(&statusNamespace, "namespace", core.NamespaceMCPServers, "Namespace to inspect")

	var policyNamespace string
	policyCmd := &cobra.Command{
		Use:   "policy",
		Short: "Inspect rendered gateway policy for an MCP server",
	}
	inspectCmd := &cobra.Command{
		Use:   "inspect [name]",
		Short: "Show the rendered gateway policy document for a server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return mgr.InspectServerPolicy(args[0], policyNamespace)
		},
	}
	inspectCmd.Flags().StringVar(&policyNamespace, "namespace", core.NamespaceMCPServers, "Namespace")
	policyCmd.AddCommand(inspectCmd)

	buildCmd := &cobra.Command{
		Use:   "build",
		Short: "Build MCP server images (push via `registry push`)",
	}
	buildCmd.AddCommand(newBuildImageCmd(mgr.Logger()))

	cmd.AddCommand(listCmd, getCmd, createCmd, applyCmd, deployCmd, exportCmd, patchCmd, deleteCmd, logsCmd, statusCmd, policyCmd, buildCmd)
	return cmd
}

func defaultDeployPort() int32 {
	port := core.GetDefaultServerPort()
	if port <= 0 || port > math.MaxInt32 {
		return 8088
	}
	return int32(port)
}
