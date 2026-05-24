package team

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"go.uber.org/zap"

	"mcp-runtime/internal/cli/core"
	kubeapply "mcp-runtime/internal/cli/kube"
	"mcp-runtime/internal/cli/platformapi"
	sentinelaccess "mcp-runtime/pkg/access"
)

const teamNamespacePrefix = "mcp-team-"

type Manager struct {
	logger  *zap.Logger
	kubectl *core.KubectlClient
}

func NewManager(logger *zap.Logger) *Manager {
	return NewManagerWithKubectl(logger, core.DefaultKubectlClient())
}

// NewManagerWithKubectl creates a team manager with an explicit kubectl client.
func NewManagerWithKubectl(logger *zap.Logger, kubectl *core.KubectlClient) *Manager {
	return &Manager{logger: logger, kubectl: kubectl}
}

// InitOptions configures local Kubernetes team namespace initialization.
type InitOptions struct {
	Slug                  string
	Namespace             string
	Group                 string
	Users                 []string
	ServiceAccounts       []string
	RoleName              string
	BindingName           string
	SkipTraefikWatch      bool
	TraefikNamespace      string
	TraefikDeployment     string
	TraefikServiceAccount string
	DryRun                bool
}

func (m *Manager) ListTeams() error {
	client, err := platformapi.NewPlatformClient()
	if err != nil {
		return err
	}
	teams, err := client.ListTeams(context.Background())
	if err != nil {
		return err
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "SLUG\tNAME\tNAMESPACE")
	for _, team := range teams {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", team.Slug, team.Name, team.Namespace)
	}
	_ = tw.Flush()
	return nil
}

func (m *Manager) CreateTeam(slug, name string) error {
	client, err := platformapi.NewPlatformClient()
	if err != nil {
		return err
	}
	slug = strings.TrimSpace(slug)
	name = strings.TrimSpace(name)
	if name == "" {
		name = slug
	}
	team, err := client.CreateTeam(context.Background(), slug, name)
	if err != nil {
		return err
	}
	core.Success(fmt.Sprintf("Created team %s (namespace: %s)", team.Slug, team.Namespace))
	return nil
}

func (m *Manager) ListTeamUsers(slug string) error {
	client, err := platformapi.NewPlatformClient()
	if err != nil {
		return err
	}
	members, err := client.ListTeamMembers(context.Background(), strings.TrimSpace(slug))
	if err != nil {
		return err
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "EMAIL\tUSER ID\tROLE\tTEAM\tNAMESPACE")
	for _, member := range members {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", member.Email, member.UserID, member.Role, member.TeamSlug, member.TeamNamespace)
	}
	_ = tw.Flush()
	return nil
}

func (m *Manager) CreateTeamUser(slug, email, password, role string) error {
	client, err := platformapi.NewPlatformClient()
	if err != nil {
		return err
	}
	slug = strings.TrimSpace(slug)
	email = strings.TrimSpace(email)
	role = strings.TrimSpace(role)
	if email == "" {
		return errors.New("email is required (use --email or --username)")
	}
	if strings.TrimSpace(password) == "" {
		return errors.New("password is required")
	}
	if role == "" {
		role = "member"
	}
	member, err := client.CreateTeamUser(context.Background(), slug, email, password, role)
	if err != nil {
		return err
	}
	core.Success(fmt.Sprintf("Ensured user %s in team %s as %s", member.Email, member.TeamSlug, member.Role))
	return nil
}

func (m *Manager) InitTeam(opts InitOptions) error {
	normalized, err := normalizeInitOptions(opts)
	if err != nil {
		return err
	}
	manifest, err := renderTeamInitManifest(normalized)
	if err != nil {
		return err
	}
	if normalized.DryRun {
		_, _ = fmt.Fprint(os.Stdout, manifest)
		if !normalized.SkipTraefikWatch {
			core.Warn(fmt.Sprintf("Dry run: skipped patching deployment/%s in namespace %s to watch %s", normalized.TraefikDeployment, normalized.TraefikNamespace, normalized.Namespace))
		}
		return nil
	}
	if m.kubectl == nil {
		return errors.New("kubectl client is required")
	}
	if err := kubeapply.ApplyManifestContent(m.kubectl.CommandArgs, manifest); err != nil {
		return core.WrapWithSentinelAndContext(nil, err, fmt.Sprintf("failed to initialize team namespace %q: %v", normalized.Namespace, err), map[string]any{
			"namespace": normalized.Namespace,
			"component": "team",
		})
	}
	if !normalized.SkipTraefikWatch {
		if err := m.ensureTraefikWatchesNamespace(normalized); err != nil {
			return err
		}
	}
	core.Success(fmt.Sprintf("Initialized team %s (namespace: %s)", normalized.Slug, normalized.Namespace))
	return nil
}

func normalizeInitOptions(opts InitOptions) (InitOptions, error) {
	opts.Slug = normalizeTeamSlug(opts.Slug)
	if err := sentinelaccess.ValidateResourceName("team", opts.Slug); err != nil {
		return InitOptions{}, err
	}
	opts.Namespace = strings.TrimSpace(opts.Namespace)
	if opts.Namespace == "" {
		opts.Namespace = teamNamespacePrefix + opts.Slug
	}
	if err := validateTeamNamespace(opts.Namespace); err != nil {
		return InitOptions{}, err
	}
	opts.Group = strings.TrimSpace(opts.Group)
	if opts.Group == "" && len(opts.Users) == 0 && len(opts.ServiceAccounts) == 0 {
		opts.Group = opts.Slug + "-mcp-admins"
	}
	opts.RoleName = strings.TrimSpace(opts.RoleName)
	if opts.RoleName == "" {
		opts.RoleName = "mcp-runtime-team-admin"
	}
	if err := sentinelaccess.ValidateResourceName("role-name", opts.RoleName); err != nil {
		return InitOptions{}, err
	}
	opts.BindingName = strings.TrimSpace(opts.BindingName)
	if opts.BindingName == "" {
		opts.BindingName = opts.Slug + "-mcp-runtime-admins"
	}
	if err := sentinelaccess.ValidateResourceName("binding-name", opts.BindingName); err != nil {
		return InitOptions{}, err
	}
	opts.TraefikNamespace = strings.TrimSpace(opts.TraefikNamespace)
	if opts.TraefikNamespace == "" {
		opts.TraefikNamespace = "traefik"
	}
	if err := sentinelaccess.ValidateResourceName("traefik-namespace", opts.TraefikNamespace); err != nil {
		return InitOptions{}, err
	}
	opts.TraefikDeployment = strings.TrimSpace(opts.TraefikDeployment)
	if opts.TraefikDeployment == "" {
		opts.TraefikDeployment = "traefik"
	}
	if err := sentinelaccess.ValidateResourceName("traefik-deployment", opts.TraefikDeployment); err != nil {
		return InitOptions{}, err
	}
	opts.TraefikServiceAccount = strings.TrimSpace(opts.TraefikServiceAccount)
	if opts.TraefikServiceAccount == "" {
		opts.TraefikServiceAccount = "traefik"
	}
	if err := sentinelaccess.ValidateResourceName("traefik-service-account", opts.TraefikServiceAccount); err != nil {
		return InitOptions{}, err
	}
	for i := range opts.Users {
		opts.Users[i] = strings.TrimSpace(opts.Users[i])
	}
	for i := range opts.ServiceAccounts {
		opts.ServiceAccounts[i] = strings.TrimSpace(opts.ServiceAccounts[i])
	}
	if opts.Group == "" && len(nonEmpty(opts.Users)) == 0 && len(nonEmpty(opts.ServiceAccounts)) == 0 {
		return InitOptions{}, errors.New("at least one of --group, --user, or --service-account is required")
	}
	return opts, nil
}

func normalizeTeamSlug(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func validateTeamNamespace(namespace string) error {
	if namespace == core.NamespaceMCPServers {
		return errors.New("shared catalog namespace is reserved")
	}
	reserved := map[string]struct{}{
		"default":                {},
		"kube-system":            {},
		"kube-public":            {},
		"kube-node-lease":        {},
		core.NamespaceMCPRuntime: {},
		"mcp-sentinel":           {},
		"registry":               {},
		"traefik":                {},
	}
	if _, ok := reserved[namespace]; ok {
		return fmt.Errorf("namespace %q is reserved", namespace)
	}
	return sentinelaccess.ValidateResourceName("namespace", namespace)
}

func renderTeamInitManifest(opts InitOptions) (string, error) {
	subjects, err := roleBindingSubjects(opts)
	if err != nil {
		return "", err
	}
	ingressRules := renderTeamNetworkPolicyIngress(opts)
	var b strings.Builder
	fmt.Fprintf(&b, `apiVersion: v1
kind: Namespace
metadata:
  name: %s
  labels:
    mcpruntime.org/scope: team
    mcpruntime.org/team-slug: %s
    pod-security.kubernetes.io/enforce: restricted
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: mcp-workload
  namespace: %s
automountServiceAccountToken: false
---
apiVersion: v1
kind: ResourceQuota
metadata:
  name: platform-default-quota
  namespace: %s
spec:
  hard:
    pods: "20"
    requests.cpu: "4"
    requests.memory: 8Gi
    limits.cpu: "8"
    limits.memory: 16Gi
    persistentvolumeclaims: "4"
---
apiVersion: v1
kind: LimitRange
metadata:
  name: platform-default-limits
  namespace: %s
spec:
  limits:
    - type: Container
      defaultRequest:
        cpu: 100m
        memory: 128Mi
      default:
        cpu: 500m
        memory: 512Mi
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: platform-default-deny
  namespace: %s
spec:
  podSelector: {}
  policyTypes:
    - Ingress
    - Egress
%s  egress:
    - to:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: kube-system
          podSelector:
            matchLabels:
              k8s-app: kube-dns
      ports:
        - protocol: UDP
          port: 53
        - protocol: TCP
          port: 53
    - to:
        - podSelector: {}
    - ports:
        - protocol: TCP
          port: 80
        - protocol: TCP
          port: 443
    - to:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: mcp-sentinel
      ports:
        - protocol: TCP
          port: 8081
        - protocol: TCP
          port: 4318
    - to:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: registry
      ports:
        - protocol: TCP
          port: 5000
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: %s
  namespace: %s
rules:
  - apiGroups: ["mcpruntime.org"]
    resources: ["mcpservers", "mcpaccessgrants", "mcpagentsessions"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: [""]
    resources: ["secrets", "configmaps"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: [""]
    resources: ["pods", "pods/log", "events", "services"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["networking.k8s.io"]
    resources: ["ingresses"]
    verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: %s
  namespace: %s
roleRef:
  kind: Role
  name: %s
  apiGroup: rbac.authorization.k8s.io
subjects:
`, opts.Namespace, opts.Slug, opts.Namespace, opts.Namespace, opts.Namespace, opts.Namespace, ingressRules, opts.RoleName, opts.Namespace, opts.BindingName, opts.Namespace, opts.RoleName)
	for _, subject := range subjects {
		b.WriteString(subject)
	}
	if !opts.SkipTraefikWatch {
		fmt.Fprintf(&b, `---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: traefik-watch
  namespace: %s
rules:
  - apiGroups: [""]
    resources: ["services", "endpoints", "secrets"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["networking.k8s.io"]
    resources: ["ingresses"]
    verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: traefik-watch
  namespace: %s
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: traefik-watch
subjects:
  - kind: ServiceAccount
    name: %s
    namespace: %s
`, opts.Namespace, opts.Namespace, opts.TraefikServiceAccount, opts.TraefikNamespace)
	}
	return b.String(), nil
}

func renderTeamNetworkPolicyIngress(opts InitOptions) string {
	var b strings.Builder
	b.WriteString(`  ingress:
    - from:
        - podSelector: {}
`)
	if !opts.SkipTraefikWatch {
		fmt.Fprintf(&b, `    - from:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: %s
`, opts.TraefikNamespace)
	}
	b.WriteString(`    - from:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: mcp-sentinel
`)
	return b.String()
}

func roleBindingSubjects(opts InitOptions) ([]string, error) {
	var subjects []string
	if opts.Group != "" {
		subjects = append(subjects, fmt.Sprintf("  - kind: Group\n    name: %s\n    apiGroup: rbac.authorization.k8s.io\n", yamlString(opts.Group)))
	}
	for _, user := range nonEmpty(opts.Users) {
		subjects = append(subjects, fmt.Sprintf("  - kind: User\n    name: %s\n    apiGroup: rbac.authorization.k8s.io\n", yamlString(user)))
	}
	for _, serviceAccount := range nonEmpty(opts.ServiceAccounts) {
		ns, name, err := parseServiceAccountSubject(serviceAccount, opts.Namespace)
		if err != nil {
			return nil, err
		}
		subjects = append(subjects, fmt.Sprintf("  - kind: ServiceAccount\n    name: %s\n    namespace: %s\n", yamlString(name), yamlString(ns)))
	}
	return subjects, nil
}

func yamlString(value string) string {
	return strconv.Quote(value)
}

func parseServiceAccountSubject(raw, defaultNamespace string) (string, string, error) {
	parts := strings.Split(raw, "/")
	switch len(parts) {
	case 1:
		name := strings.TrimSpace(parts[0])
		if err := sentinelaccess.ValidateResourceName("service-account", name); err != nil {
			return "", "", err
		}
		return defaultNamespace, name, nil
	case 2:
		namespace := strings.TrimSpace(parts[0])
		name := strings.TrimSpace(parts[1])
		if err := sentinelaccess.ValidateResourceName("service-account namespace", namespace); err != nil {
			return "", "", err
		}
		if err := sentinelaccess.ValidateResourceName("service-account", name); err != nil {
			return "", "", err
		}
		return namespace, name, nil
	default:
		return "", "", fmt.Errorf("service-account %q must be name or namespace/name", raw)
	}
}

func nonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

type traefikDeployment struct {
	Spec struct {
		Template struct {
			Spec struct {
				Containers []struct {
					Name string   `json:"name"`
					Args []string `json:"args"`
				} `json:"containers"`
			} `json:"spec"`
		} `json:"template"`
	} `json:"spec"`
}

func (m *Manager) ensureTraefikWatchesNamespace(opts InitOptions) error {
	out, err := m.kubectl.Output([]string{"get", "deployment", opts.TraefikDeployment, "-n", opts.TraefikNamespace, "-o", "json"})
	if err != nil {
		return core.WrapWithSentinelAndContext(nil, err, fmt.Sprintf("failed to inspect Traefik deployment %s/%s: %v", opts.TraefikNamespace, opts.TraefikDeployment, err), map[string]any{
			"namespace":  opts.TraefikNamespace,
			"deployment": opts.TraefikDeployment,
			"component":  "team",
		})
	}
	containerIndex, argIndex, arg, err := findTraefikNamespaceArg(out)
	if err != nil {
		return err
	}
	watched := splitWatchedNamespaces(arg)
	for _, namespace := range watched {
		if namespace == opts.Namespace {
			core.Info(fmt.Sprintf("Traefik deployment %s/%s already watches namespace %s", opts.TraefikNamespace, opts.TraefikDeployment, opts.Namespace))
			return nil
		}
	}
	watched = append(watched, opts.Namespace)
	value := "--providers.kubernetesingress.namespaces=" + strings.Join(watched, ",")
	patch, err := json.Marshal([]map[string]any{{
		"op":    "replace",
		"path":  fmt.Sprintf("/spec/template/spec/containers/%d/args/%d", containerIndex, argIndex),
		"value": value,
	}})
	if err != nil {
		return err
	}
	if err := m.kubectl.RunWithOutput([]string{"patch", "deployment", opts.TraefikDeployment, "-n", opts.TraefikNamespace, "--type=json", "--patch", string(patch)}, os.Stdout, os.Stderr); err != nil {
		return core.WrapWithSentinelAndContext(nil, err, fmt.Sprintf("failed to patch Traefik deployment %s/%s watch list: %v", opts.TraefikNamespace, opts.TraefikDeployment, err), map[string]any{
			"namespace":  opts.TraefikNamespace,
			"deployment": opts.TraefikDeployment,
			"component":  "team",
		})
	}
	return nil
}

func findTraefikNamespaceArg(raw []byte) (int, int, string, error) {
	var deployment traefikDeployment
	if err := json.Unmarshal(raw, &deployment); err != nil {
		return 0, 0, "", fmt.Errorf("parse Traefik deployment JSON: %w", err)
	}
	const prefix = "--providers.kubernetesingress.namespaces="
	for containerIndex, container := range deployment.Spec.Template.Spec.Containers {
		if container.Name != "" && container.Name != "traefik" {
			continue
		}
		for argIndex, arg := range container.Args {
			if strings.HasPrefix(arg, prefix) {
				return containerIndex, argIndex, arg, nil
			}
		}
	}
	for containerIndex, container := range deployment.Spec.Template.Spec.Containers {
		for argIndex, arg := range container.Args {
			if strings.HasPrefix(arg, prefix) {
				return containerIndex, argIndex, arg, nil
			}
		}
	}
	return 0, 0, "", errors.New("traefik deployment does not expose --providers.kubernetesingress.namespaces; rerun with --skip-traefik-watch or patch your ingress controller manually")
}

func splitWatchedNamespaces(arg string) []string {
	_, raw, _ := strings.Cut(arg, "=")
	seen := make(map[string]struct{})
	var out []string
	for _, namespace := range strings.Split(raw, ",") {
		namespace = strings.TrimSpace(namespace)
		if namespace == "" {
			continue
		}
		if _, ok := seen[namespace]; ok {
			continue
		}
		seen[namespace] = struct{}{}
		out = append(out, namespace)
	}
	return out
}
