package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

const (
	accessGrantResource   = "mcpaccessgrant"
	accessSessionResource = "mcpagentsession"
)

type AccessManager struct {
	kubectl *KubectlClient
	logger  *zap.Logger
	// useKube forces kubectl; when false, the platform API is used when logged in via mcp-runtime auth.
	useKube bool
}

func NewAccessManager(kubectl *KubectlClient, logger *zap.Logger) *AccessManager {
	return &AccessManager{kubectl: kubectl, logger: logger}
}

func DefaultAccessManager(logger *zap.Logger) *AccessManager {
	return NewAccessManager(kubectlClient, logger)
}

func NewAccessCmd(logger *zap.Logger) *cobra.Command {
	mgr := DefaultAccessManager(logger)
	return NewAccessCmdWithManager(mgr)
}

func NewAccessCmdWithManager(mgr *AccessManager) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "access",
		Short: "Manage grants and agent sessions",
		Long: `Commands for managing MCPAccessGrant and MCPAgentSession resources that feed the gateway policy layer.

With mcp-runtime auth login, commands use the platform API by default. Use --use-kube
to target the cluster with kubectl and a kubeconfig (cluster admin path).`,
	}

	cmd.PersistentFlags().BoolVar(&mgr.useKube, "use-kube", false, "Use kubectl and local kubeconfig instead of the platform API for supported commands")

	cmd.AddCommand(mgr.newAccessGrantCmd())
	cmd.AddCommand(mgr.newAccessSessionCmd())

	return cmd
}

func (m *AccessManager) newAccessGrantCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "grant",
		Short: "Manage MCPAccessGrant resources",
	}

	cmd.AddCommand(m.newAccessListCmd(accessGrantResource, "grants"))
	cmd.AddCommand(m.newAccessGetCmd(accessGrantResource, "grant"))
	cmd.AddCommand(m.newAccessApplyCmd(accessGrantResource, "grant"))
	cmd.AddCommand(m.newAccessDeleteCmd(accessGrantResource, "grant"))
	cmd.AddCommand(m.newAccessToggleCmd(accessGrantResource, "disable", "Disable a grant", true))
	cmd.AddCommand(m.newAccessToggleCmd(accessGrantResource, "enable", "Enable a grant", false))

	return cmd
}

func (m *AccessManager) newAccessSessionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Manage MCPAgentSession resources",
	}

	cmd.AddCommand(m.newAccessListCmd(accessSessionResource, "sessions"))
	cmd.AddCommand(m.newAccessGetCmd(accessSessionResource, "session"))
	cmd.AddCommand(m.newAccessApplyCmd(accessSessionResource, "session"))
	cmd.AddCommand(m.newAccessDeleteCmd(accessSessionResource, "session"))
	cmd.AddCommand(m.newAccessToggleCmd(accessSessionResource, "revoke", "Revoke an agent session", true))
	cmd.AddCommand(m.newAccessToggleCmd(accessSessionResource, "unrevoke", "Clear the revoked flag on an agent session", false))

	return cmd
}

func (m *AccessManager) newAccessListCmd(resource, label string) *cobra.Command {
	var namespace string
	var allNamespaces bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: fmt.Sprintf("List access %s", label),
		RunE: func(cmd *cobra.Command, args []string) error {
			return m.ListAccessResources(resource, namespace, allNamespaces)
		},
	}

	cmd.Flags().StringVar(&namespace, "namespace", "", "Namespace to inspect")
	cmd.Flags().BoolVar(&allNamespaces, "all-namespaces", true, "List resources across all namespaces when no namespace is specified")

	return cmd
}

func (m *AccessManager) newAccessGetCmd(resource, label string) *cobra.Command {
	var namespace string

	cmd := &cobra.Command{
		Use:   "get [name]",
		Short: fmt.Sprintf("Get an access %s", label),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return m.GetAccessResource(resource, args[0], namespace)
		},
	}

	cmd.Flags().StringVar(&namespace, "namespace", NamespaceMCPServers, "Namespace")

	return cmd
}

func (m *AccessManager) newAccessApplyCmd(resource, label string) *cobra.Command {
	var file string

	cmd := &cobra.Command{
		Use:   "apply",
		Short: fmt.Sprintf("Apply a %s manifest", label),
		RunE: func(cmd *cobra.Command, args []string) error {
			return m.ApplyAccessResource(file)
		},
	}

	cmd.Flags().StringVar(&file, "file", "", "Manifest file to apply")
	_ = cmd.MarkFlagRequired("file")

	return cmd
}

func (m *AccessManager) newAccessDeleteCmd(resource, label string) *cobra.Command {
	var namespace string

	cmd := &cobra.Command{
		Use:   "delete [name]",
		Short: fmt.Sprintf("Delete an access %s", label),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return m.DeleteAccessResource(resource, args[0], namespace)
		},
	}

	cmd.Flags().StringVar(&namespace, "namespace", NamespaceMCPServers, "Namespace")

	return cmd
}

func (m *AccessManager) newAccessToggleCmd(resource, use, short string, value bool) *cobra.Command {
	var namespace string

	cmd := &cobra.Command{
		Use:   use + " [name]",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return m.ToggleAccessResource(resource, args[0], namespace, value)
		},
	}

	cmd.Flags().StringVar(&namespace, "namespace", NamespaceMCPServers, "Namespace")

	return cmd
}

func (m *AccessManager) accessListQueryNamespace(namespace string, allNamespaces bool) string {
	switch {
	case namespace != "":
		return namespace
	case allNamespaces:
		return ""
	default:
		return NamespaceMCPServers
	}
}

// ListAccessResources lists grants or sessions via the platform API when configured, else kubectl.
func (m *AccessManager) ListAccessResources(resource, namespace string, allNamespaces bool) error {
	plat, kube, err := m.platformOrKube()
	if err != nil {
		return err
	}
	if !kube {
		return m.listAccessPlatform(context.Background(), plat, resource, m.accessListQueryNamespace(namespace, allNamespaces))
	}

	args := []string{"get", resource}
	switch {
	case namespace != "":
		args = append(args, "-n", namespace)
	case allNamespaces:
		args = append(args, "-A")
	default:
		args = append(args, "-n", m.accessListQueryNamespace(namespace, allNamespaces))
	}

	if err := m.kubectl.RunWithOutput(args, os.Stdout, os.Stderr); err != nil {
		return wrapWithSentinelAndContext(nil, err, fmt.Sprintf("failed to list %s resources: %v", resource, err), map[string]any{
			"resource":  resource,
			"namespace": namespace,
			"component": "access",
		})
	}
	return nil
}

func (m *AccessManager) listAccessPlatform(ctx context.Context, plat *platformClient, resource, nsFilter string) error {
	switch resource {
	case accessGrantResource:
		grants, err := plat.listGrants(ctx, nsFilter)
		if err != nil {
			return wrapWithSentinelAndContext(nil, err, fmt.Sprintf("list grants: %v", err), map[string]any{"component": "access"})
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "NAME\tNAMESPACE\tSERVER\tDISABLED")
		for _, g := range grants {
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%v\n", g.Name, g.Namespace, g.ServerRef.Name, g.Disabled)
		}
		_ = tw.Flush()
		return nil
	case accessSessionResource:
		sessions, err := plat.listSessions(ctx, nsFilter)
		if err != nil {
			return wrapWithSentinelAndContext(nil, err, fmt.Sprintf("list sessions: %v", err), map[string]any{"component": "access"})
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "NAME\tNAMESPACE\tSERVER\tREVOKED")
		for _, s := range sessions {
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%v\n", s.Name, s.Namespace, s.ServerRef.Name, s.Revoked)
		}
		_ = tw.Flush()
		return nil
	default:
		return newWithSentinel(nil, fmt.Sprintf("unsupported access resource %q", resource))
	}
}

func (m *AccessManager) GetAccessResource(resource, name, namespace string) error {
	name, namespace, err := validateAccessInput(name, namespace)
	if err != nil {
		return err
	}

	plat, kube, err := m.platformOrKube()
	if err != nil {
		return err
	}
	if !kube {
		return m.getAccessPlatform(context.Background(), plat, resource, name, namespace)
	}

	args := []string{"get", resource, name, "-n", namespace, "-o", "yaml"}
	if err := m.kubectl.RunWithOutput(args, os.Stdout, os.Stderr); err != nil {
		return wrapWithSentinelAndContext(nil, err, fmt.Sprintf("failed to get %s %q in namespace %q: %v", resource, name, namespace, err), map[string]any{
			"resource":  resource,
			"name":      name,
			"namespace": namespace,
			"component": "access",
		})
	}
	return nil
}

func (m *AccessManager) getAccessPlatform(ctx context.Context, plat *platformClient, resource, name, namespace string) error {
	switch resource {
	case accessGrantResource:
		grant, err := plat.getGrant(ctx, namespace, name)
		if err != nil {
			return err
		}
		b, _ := json.MarshalIndent(grant, "", "  ")
		_, _ = os.Stdout.Write(append(b, '\n'))
		return nil
	case accessSessionResource:
		session, err := plat.getSession(ctx, namespace, name)
		if err != nil {
			return err
		}
		b, _ := json.MarshalIndent(session, "", "  ")
		_, _ = os.Stdout.Write(append(b, '\n'))
		return nil
	default:
		return newWithSentinel(nil, fmt.Sprintf("unsupported access resource %q", resource))
	}
}

func (m *AccessManager) ApplyAccessResource(file string) error {
	plat, kube, err := m.platformOrKube()
	if err != nil {
		return err
	}
	if !kube {
		if err := plat.applyAccessFromYAMLFile(context.Background(), file); err != nil {
			return wrapWithSentinelAndContext(nil, err, fmt.Sprintf("apply access resource from file %q: %v", file, err), map[string]any{
				"file":      file,
				"component": "access",
			})
		}
		return nil
	}
	if err := applyManifestFromFile(m.kubectl, file, os.Stdout, os.Stderr); err != nil {
		return wrapWithSentinelAndContext(nil, err, fmt.Sprintf("failed to apply access resource from file %q: %v", file, err), map[string]any{
			"file":      file,
			"component": "access",
		})
	}
	return nil
}

func (m *AccessManager) DeleteAccessResource(resource, name, namespace string) error {
	name, namespace, err := validateAccessInput(name, namespace)
	if err != nil {
		return err
	}

	plat, kube, err := m.platformOrKube()
	if err != nil {
		return err
	}
	if !kube {
		ctx := context.Background()
		switch resource {
		case accessGrantResource:
			err = plat.deleteGrant(ctx, namespace, name)
		case accessSessionResource:
			err = plat.deleteSession(ctx, namespace, name)
		default:
			return newWithSentinel(nil, fmt.Sprintf("unsupported access resource %q", resource))
		}
		if err != nil {
			return wrapWithSentinelAndContext(nil, err, fmt.Sprintf("delete %s %q: %v", resource, name, err), map[string]any{
				"resource":  resource,
				"name":      name,
				"namespace": namespace,
				"component": "access",
			})
		}
		_, _ = fmt.Fprintf(os.Stdout, "%s %q deleted\n", resource, name)
		return nil
	}

	args := []string{"delete", resource, name, "-n", namespace}
	if err := m.kubectl.RunWithOutput(args, os.Stdout, os.Stderr); err != nil {
		return wrapWithSentinelAndContext(nil, err, fmt.Sprintf("failed to delete %s %q in namespace %q: %v", resource, name, namespace, err), map[string]any{
			"resource":  resource,
			"name":      name,
			"namespace": namespace,
			"component": "access",
		})
	}
	return nil
}

func (m *AccessManager) ToggleAccessResource(resource, name, namespace string, value bool) error {
	name, namespace, err := validateAccessInput(name, namespace)
	if err != nil {
		return err
	}

	plat, kube, err := m.platformOrKube()
	if err != nil {
		return err
	}
	if !kube {
		ctx := context.Background()
		switch resource {
		case accessGrantResource:
			if value {
				err = plat.postGrantToggle(ctx, namespace, name, "disable")
			} else {
				err = plat.postGrantToggle(ctx, namespace, name, "enable")
			}
		case accessSessionResource:
			if value {
				err = plat.postSessionToggle(ctx, namespace, name, "revoke")
			} else {
				err = plat.postSessionToggle(ctx, namespace, name, "unrevoke")
			}
		default:
			return newWithSentinel(nil, fmt.Sprintf("unsupported access resource %q", resource))
		}
		if err != nil {
			return wrapWithSentinelAndContext(nil, err, fmt.Sprintf("toggle %s %q: %v", resource, name, err), map[string]any{
				"resource":  resource,
				"name":      name,
				"namespace": namespace,
				"component": "access",
			})
		}
		_, _ = fmt.Fprintf(os.Stdout, "updated %s %q\n", resource, name)
		return nil
	}

	patchValue := map[string]any{"spec": map[string]any{}}
	switch resource {
	case accessGrantResource:
		patchValue["spec"].(map[string]any)["disabled"] = value
	case accessSessionResource:
		patchValue["spec"].(map[string]any)["revoked"] = value
	default:
		return newWithSentinel(nil, fmt.Sprintf("unsupported access resource %q", resource))
	}

	data, err := json.Marshal(patchValue)
	if err != nil {
		return wrapWithSentinelAndContext(nil, err, fmt.Sprintf("failed to marshal access patch for %s %q: %v", resource, name, err), map[string]any{
			"resource":  resource,
			"name":      name,
			"namespace": namespace,
			"component": "access",
		})
	}

	args := []string{"patch", resource, name, "-n", namespace, "--type", "merge", "--patch", string(data)}
	if err := m.kubectl.RunWithOutput(args, os.Stdout, os.Stderr); err != nil {
		return wrapWithSentinelAndContext(nil, err, fmt.Sprintf("failed to patch %s %q in namespace %q: %v", resource, name, namespace, err), map[string]any{
			"resource":  resource,
			"name":      name,
			"namespace": namespace,
			"component": "access",
		})
	}
	return nil
}

func validateAccessInput(name, namespace string) (string, string, error) {
	if !validServerName.MatchString(name) {
		return "", "", newWithSentinel(nil, fmt.Sprintf("invalid resource name %q: must be lowercase alphanumeric with optional hyphens", name))
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
