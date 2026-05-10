package access

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"mcp-runtime/internal/cli/core"
	kubeapply "mcp-runtime/internal/cli/kube"
	"mcp-runtime/internal/cli/platformapi"
)

const (
	// GrantResource is the kubectl resource name for MCPAccessGrant.
	GrantResource = "mcpaccessgrant"
	// SessionResource is the kubectl resource name for MCPAgentSession.
	SessionResource = "mcpagentsession"
)

// AccessManager handles access grant and session operations.
type AccessManager struct {
	kubectl *core.KubectlClient
	logger  *zap.Logger
	// useKube forces kubectl; when false, the platform API is used when logged in via mcp-runtime auth.
	useKube bool
}

// NewAccessManager creates an AccessManager with explicit dependencies (tests and advanced wiring).
func NewAccessManager(kubectl *core.KubectlClient, logger *zap.Logger) *AccessManager {
	return &AccessManager{kubectl: kubectl, logger: logger}
}

// DefaultAccessManager returns an AccessManager using the shared runtime clients.
func DefaultAccessManager(runtime *core.Runtime) *AccessManager {
	return NewAccessManager(runtime.KubectlClient(), runtime.Logger())
}

// BindUseKubeFlag wires the shared --use-kube flag onto the command.
func (m *AccessManager) BindUseKubeFlag(cmd *cobra.Command) {
	cmd.PersistentFlags().BoolVar(&m.useKube, "use-kube", false, "Use kubectl and local kubeconfig instead of the platform API for supported commands")
}

func (m *AccessManager) accessListQueryNamespace(namespace string, allNamespaces bool) string {
	switch {
	case namespace != "":
		return namespace
	case allNamespaces:
		return ""
	default:
		return core.NamespaceMCPServers
	}
}

// ListAccessResources lists grants or sessions via the platform API when configured, else kubectl.
func (m *AccessManager) ListAccessResources(resource, namespace string, allNamespaces bool) error {
	plat, kube, err := platformapi.ResolvePlatformOrKube(m.useKube)
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
		return core.WrapWithSentinelAndContext(nil, err, fmt.Sprintf("failed to list %s resources: %v", resource, err), map[string]any{
			"resource":  resource,
			"namespace": namespace,
			"component": "access",
		})
	}
	return nil
}

func (m *AccessManager) listAccessPlatform(ctx context.Context, plat *platformapi.PlatformClient, resource, nsFilter string) error {
	switch resource {
	case GrantResource:
		grants, err := plat.ListGrants(ctx, nsFilter)
		if err != nil {
			return core.WrapWithSentinelAndContext(nil, err, fmt.Sprintf("list grants: %v", err), map[string]any{"component": "access"})
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "NAME\tNAMESPACE\tSERVER\tDISABLED")
		for _, g := range grants {
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%v\n", g.Name, g.Namespace, g.ServerRef.Name, g.Disabled)
		}
		_ = tw.Flush()
		return nil
	case SessionResource:
		sessions, err := plat.ListSessions(ctx, nsFilter)
		if err != nil {
			return core.WrapWithSentinelAndContext(nil, err, fmt.Sprintf("list sessions: %v", err), map[string]any{"component": "access"})
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "NAME\tNAMESPACE\tSERVER\tREVOKED")
		for _, s := range sessions {
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%v\n", s.Name, s.Namespace, s.ServerRef.Name, s.Revoked)
		}
		_ = tw.Flush()
		return nil
	default:
		return core.NewWithSentinel(nil, fmt.Sprintf("unsupported access resource %q", resource))
	}
}

// GetAccessResource prints one grant or session via platform API or kubectl.
func (m *AccessManager) GetAccessResource(resource, name, namespace string) error {
	name, namespace, err := validateAccessResourceInput(name, namespace)
	if err != nil {
		return err
	}

	plat, kube, err := platformapi.ResolvePlatformOrKube(m.useKube)
	if err != nil {
		return err
	}
	if !kube {
		return m.getAccessPlatform(context.Background(), plat, resource, name, namespace)
	}

	args := []string{"get", resource, name, "-n", namespace, "-o", "yaml"}
	if err := m.kubectl.RunWithOutput(args, os.Stdout, os.Stderr); err != nil {
		return core.WrapWithSentinelAndContext(nil, err, fmt.Sprintf("failed to get %s %q in namespace %q: %v", resource, name, namespace, err), map[string]any{
			"resource":  resource,
			"name":      name,
			"namespace": namespace,
			"component": "access",
		})
	}
	return nil
}

func (m *AccessManager) getAccessPlatform(ctx context.Context, plat *platformapi.PlatformClient, resource, name, namespace string) error {
	switch resource {
	case GrantResource:
		grant, err := plat.GetGrant(ctx, namespace, name)
		if err != nil {
			return err
		}
		b, _ := json.MarshalIndent(grant, "", "  ")
		_, _ = os.Stdout.Write(append(b, '\n'))
		return nil
	case SessionResource:
		session, err := plat.GetSession(ctx, namespace, name)
		if err != nil {
			return err
		}
		b, _ := json.MarshalIndent(session, "", "  ")
		_, _ = os.Stdout.Write(append(b, '\n'))
		return nil
	default:
		return core.NewWithSentinel(nil, fmt.Sprintf("unsupported access resource %q", resource))
	}
}

// ApplyAccessResource applies a grant or session manifest via platform API or kubectl.
func (m *AccessManager) ApplyAccessResource(file string) error {
	m.warnAccessManifest(file)
	plat, kube, err := platformapi.ResolvePlatformOrKube(m.useKube)
	if err != nil {
		return err
	}
	if !kube {
		if err := plat.ApplyAccessFromYAMLFile(context.Background(), file); err != nil {
			return core.WrapWithSentinelAndContext(nil, err, fmt.Sprintf("apply access resource from file %q: %v", file, err), map[string]any{
				"file":      file,
				"component": "access",
			})
		}
		return nil
	}
	if err := kubeapply.ApplyManifestFromFile(m.kubectl.CommandArgs, file, os.Stdout, os.Stderr); err != nil {
		return core.WrapWithSentinelAndContext(nil, err, fmt.Sprintf("failed to apply access resource from file %q: %v", file, err), map[string]any{
			"file":      file,
			"component": "access",
		})
	}
	return nil
}

func (m *AccessManager) warnAccessManifest(file string) {
	warnings, err := accessManifestWarningsFromFile(file)
	if err != nil {
		core.Warn(fmt.Sprintf("Could not inspect %s for access identity warnings: %v", file, err))
		return
	}
	for _, warning := range warnings {
		core.Warn(warning)
	}
}

// DeleteAccessResource deletes a grant or session via platform API or kubectl.
func (m *AccessManager) DeleteAccessResource(resource, name, namespace string) error {
	name, namespace, err := validateAccessResourceInput(name, namespace)
	if err != nil {
		return err
	}

	plat, kube, err := platformapi.ResolvePlatformOrKube(m.useKube)
	if err != nil {
		return err
	}
	if !kube {
		ctx := context.Background()
		switch resource {
		case GrantResource:
			err = plat.DeleteGrant(ctx, namespace, name)
		case SessionResource:
			err = plat.DeleteSession(ctx, namespace, name)
		default:
			return core.NewWithSentinel(nil, fmt.Sprintf("unsupported access resource %q", resource))
		}
		if err != nil {
			return core.WrapWithSentinelAndContext(nil, err, fmt.Sprintf("delete %s %q: %v", resource, name, err), map[string]any{
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
		return core.WrapWithSentinelAndContext(nil, err, fmt.Sprintf("failed to delete %s %q in namespace %q: %v", resource, name, namespace, err), map[string]any{
			"resource":  resource,
			"name":      name,
			"namespace": namespace,
			"component": "access",
		})
	}
	return nil
}

// ToggleAccessResource enables/disables grants or revokes/unrevokes sessions.
func (m *AccessManager) ToggleAccessResource(resource, name, namespace string, value bool) error {
	name, namespace, err := validateAccessResourceInput(name, namespace)
	if err != nil {
		return err
	}

	plat, kube, err := platformapi.ResolvePlatformOrKube(m.useKube)
	if err != nil {
		return err
	}
	if !kube {
		ctx := context.Background()
		switch resource {
		case GrantResource:
			if value {
				err = plat.PostGrantToggle(ctx, namespace, name, "disable")
			} else {
				err = plat.PostGrantToggle(ctx, namespace, name, "enable")
			}
		case SessionResource:
			if value {
				err = plat.PostSessionToggle(ctx, namespace, name, "revoke")
			} else {
				err = plat.PostSessionToggle(ctx, namespace, name, "unrevoke")
			}
		default:
			return core.NewWithSentinel(nil, fmt.Sprintf("unsupported access resource %q", resource))
		}
		if err != nil {
			return core.WrapWithSentinelAndContext(nil, err, fmt.Sprintf("toggle %s %q: %v", resource, name, err), map[string]any{
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
	case GrantResource:
		patchValue["spec"].(map[string]any)["disabled"] = value
	case SessionResource:
		patchValue["spec"].(map[string]any)["revoked"] = value
	default:
		return core.NewWithSentinel(nil, fmt.Sprintf("unsupported access resource %q", resource))
	}

	data, err := json.Marshal(patchValue)
	if err != nil {
		return core.WrapWithSentinelAndContext(nil, err, fmt.Sprintf("failed to marshal access patch for %s %q: %v", resource, name, err), map[string]any{
			"resource":  resource,
			"name":      name,
			"namespace": namespace,
			"component": "access",
		})
	}

	args := []string{"patch", resource, name, "-n", namespace, "--type", "merge", "--patch", string(data)}
	if err := m.kubectl.RunWithOutput(args, os.Stdout, os.Stderr); err != nil {
		return core.WrapWithSentinelAndContext(nil, err, fmt.Sprintf("failed to patch %s %q in namespace %q: %v", resource, name, namespace, err), map[string]any{
			"resource":  resource,
			"name":      name,
			"namespace": namespace,
			"component": "access",
		})
	}
	return nil
}
