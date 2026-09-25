// Package update owns the `mcp-runtime update` command, which moves an
// installed MCP Runtime platform to a release by patching only the images of
// platform services whose versions changed.
package update

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"mcp-runtime/internal/cli/core"
	"mcp-runtime/internal/platformrelease"
)

// Options holds parsed update flags.
type Options struct {
	To                 string
	ReleaseManifest    string
	DryRun             bool
	Yes                bool
	Only               []string
	IncludeAuth        bool
	IncludeCertManager bool
	AllowDowngrade     bool
	RollbackOnFailure  bool
	Timeout            time.Duration
	Output             string
	Kubeconfig         string
	Context            string
}

// deps are injectable for tests.
type deps struct {
	loadManifest func(ctx context.Context, source string) ([]byte, error)
	kube         func(kubeconfig, context string) (kubernetes.Interface, ClusterInfo, error)
	waiter       func(cs kubernetes.Interface) RolloutWaiter
	confirm      func(in io.Reader, out io.Writer, cluster ClusterInfo) (bool, error)
	stdin        io.Reader
}

func defaultDeps() deps {
	return deps{
		loadManifest: func(ctx context.Context, source string) ([]byte, error) {
			return platformrelease.LoadManifest(ctx, source, nil)
		},
		kube:    kubeClient,
		waiter:  DefaultWaiter,
		confirm: promptConfirm,
		stdin:   os.Stdin,
	}
}

// New returns the update command.
func New(_ *core.Runtime) *cobra.Command {
	return newCommand(defaultDeps())
}

func newCommand(d deps) *cobra.Command {
	opts := Options{}
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update installed platform services to a release",
		Long: `Update an installed MCP Runtime platform to a release.

The target comes from a release component manifest (service -> image
repository, tag, optional digest), selected with --to (fetches the manifest
attached to that GitHub release) or --release-manifest (local path or https
URL). update compares it with the images running in the cluster and patches
only the Deployments whose images changed, one at a time, waiting for each
rollout.

update only patches container images (and the operator's
MCP_GATEWAY_PROXY_IMAGE env var) plus version labels/annotations. It never
modifies Secrets, PVCs, ConfigMaps, cert-manager Issuers/Certificates, CRDs,
Services, or Ingresses, and never deletes or recreates workloads. mcp-auth and
cert-manager are skipped unless selected with --include-auth,
--include-cert-manager, or --only. Releases that change CRDs are refused; run
setup from that release instead.

The plan always shows the kube context and cluster ID. Without --dry-run,
update asks for confirmation (or requires --yes when not interactive).
Images must already be published to the registry the manifest resolves to;
relative repositories resolve against the registry of the running image.`,
		Example: `  mcp-runtime update --to v0.5.0 --dry-run
  mcp-runtime update --release-manifest ./platform-manifest.json
  mcp-runtime update --to v0.5.0 --only ui,platform-api --yes
  mcp-runtime update --release-manifest ./platform-manifest.json --include-auth --output json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return run(cmd.Context(), cmd.OutOrStdout(), opts, d)
		},
	}
	f := cmd.Flags()
	f.StringVar(&opts.To, "to", "", "Target release version (for example v0.5.0); fetches that release's platform-manifest.json unless --release-manifest is set")
	f.StringVar(&opts.ReleaseManifest, "release-manifest", "", "Release component manifest path or https URL; its version must match --to when both are set")
	f.BoolVar(&opts.DryRun, "dry-run", false, "Print the update plan and exit without changing the cluster")
	f.BoolVar(&opts.Yes, "yes", false, "Apply without the interactive confirmation prompt")
	f.StringSliceVar(&opts.Only, "only", nil, "Comma-separated components to consider (default: all non-opt-in components)")
	f.BoolVar(&opts.IncludeAuth, "include-auth", false, "Include the mcp-auth authorization server")
	f.BoolVar(&opts.IncludeCertManager, "include-cert-manager", false, "Include cert-manager images (patch releases only; CRDs are not upgraded)")
	f.BoolVar(&opts.AllowDowngrade, "allow-downgrade", false, "Allow a target version lower than the installed version")
	f.BoolVar(&opts.RollbackOnFailure, "rollback-on-failure", true, "Restore previous images of workloads changed in this run if a rollout fails")
	f.DurationVar(&opts.Timeout, "timeout", 5*time.Minute, "Rollout wait timeout per workload")
	f.StringVar(&opts.Output, "output", "text", "Output format: text or json")
	f.StringVar(&opts.Kubeconfig, "kubeconfig", "", "Path to kubeconfig file (default: KUBECONFIG or ~/.kube/config)")
	f.StringVar(&opts.Context, "context", "", "Kubernetes context to use (default: current context)")
	return cmd
}

func run(ctx context.Context, out io.Writer, opts Options, d deps) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateOptions(opts); err != nil {
		return err
	}
	manifest, source, err := resolveManifest(ctx, opts, d)
	if err != nil {
		return err
	}
	cs, cluster, err := d.kube(opts.Kubeconfig, opts.Context)
	if err != nil {
		return core.WrapWithSentinel(core.ErrUpdateKubeClientFailed, err, fmt.Sprintf("connect to Kubernetes: %v (pass --kubeconfig/--context)", err))
	}
	plan, err := BuildPlan(ctx, cs, manifest, Selection{
		Only:               opts.Only,
		IncludeAuth:        opts.IncludeAuth,
		IncludeCertManager: opts.IncludeCertManager,
		AllowDowngrade:     opts.AllowDowngrade,
	})
	if err != nil {
		return err
	}
	plan.Cluster = cluster
	plan.ManifestSource = source

	jsonOut := opts.Output == "json"
	report := func(res *Result) error {
		if jsonOut {
			return writeJSON(out, plan, res, opts.DryRun)
		}
		if res != nil {
			writeResultText(out, res)
		}
		return nil
	}
	if !jsonOut {
		writePlanText(out, plan)
	}

	if blocked := plan.Blocked(); len(blocked) > 0 {
		_ = report(nil)
		names := make([]string, 0, len(blocked))
		for _, r := range blocked {
			names = append(names, r.Component)
		}
		return core.NewWithSentinel(core.ErrUpdateBlocked, fmt.Sprintf("update blocked for %s; see plan reasons", strings.Join(names, ", ")))
	}
	changed := plan.Changed()
	if opts.DryRun || len(changed) == 0 {
		if !jsonOut {
			if len(changed) == 0 {
				fmt.Fprintln(out, "\nPlatform is up to date; nothing to roll out.")
			} else {
				fmt.Fprintf(out, "\nDry run: %d component(s) would be updated. Re-run without --dry-run to apply.\n", len(changed))
			}
		}
		return report(nil)
	}

	if !opts.Yes {
		ok, err := d.confirm(d.stdin, out, cluster)
		if err != nil {
			return err
		}
		if !ok {
			return core.NewWithSentinel(core.ErrUpdateAborted, "update aborted; no changes were made")
		}
	}

	progress := func(msg string) {
		if !jsonOut {
			fmt.Fprintln(out, msg)
		}
	}
	res := Apply(ctx, cs, plan, ApplyOptions{
		Timeout:           opts.Timeout,
		RollbackOnFailure: opts.RollbackOnFailure,
		Waiter:            d.waiter(cs),
		Progress:          progress,
	})
	if err := report(res); err != nil {
		return err
	}
	if res.Failed {
		return core.NewWithSentinel(core.ErrUpdateRolloutFailed, "platform update failed; see per-workload status and recovery commands above")
	}
	return nil
}

func validateOptions(opts Options) error {
	if strings.TrimSpace(opts.To) == "" && strings.TrimSpace(opts.ReleaseManifest) == "" {
		return core.NewWithSentinel(core.ErrUpdateTargetRequired, "no update target: pass --to <version> or --release-manifest <path|https-url>; update never picks a release implicitly")
	}
	if opts.To != "" {
		if _, err := platformrelease.ParseVersion(opts.To); err != nil {
			return core.NewWithSentinel(core.ErrUpdateInvalidFlag, fmt.Sprintf("--to: %v", err))
		}
	}
	if opts.Output != "text" && opts.Output != "json" {
		return core.NewWithSentinel(core.ErrUpdateInvalidFlag, fmt.Sprintf("--output must be text or json, got %q", opts.Output))
	}
	if opts.Timeout <= 0 {
		return core.NewWithSentinel(core.ErrUpdateInvalidFlag, "--timeout must be positive")
	}
	return nil
}

func resolveManifest(ctx context.Context, opts Options, d deps) (*platformrelease.Manifest, string, error) {
	source := strings.TrimSpace(opts.ReleaseManifest)
	if source == "" {
		source = platformrelease.ReleaseManifestURL(opts.To)
	}
	data, err := d.loadManifest(ctx, source)
	if err != nil {
		return nil, source, core.WrapWithSentinel(core.ErrUpdateManifestInvalid, err, fmt.Sprintf("load release manifest: %v", err))
	}
	m, err := platformrelease.ParseManifest(data)
	if err != nil {
		return nil, source, core.WrapWithSentinel(core.ErrUpdateManifestInvalid, err, fmt.Sprintf("release manifest %s: %v", source, err))
	}
	if opts.To != "" && m.Version != opts.To {
		return nil, source, core.NewWithSentinel(core.ErrUpdateTargetMismatch, fmt.Sprintf("release manifest %s is for %s but --to is %s; refusing to change the target silently", source, m.Version, opts.To))
	}
	return m, source, nil
}

// kubeClient builds a clientset from kubeconfig and reports the context,
// API server, and cluster ID (kube-system namespace UID).
func kubeClient(kubeconfig, kubeContext string) (kubernetes.Interface, ClusterInfo, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		rules.ExplicitPath = kubeconfig
	}
	cfg := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{CurrentContext: kubeContext})
	raw, err := cfg.RawConfig()
	if err != nil {
		return nil, ClusterInfo{}, err
	}
	info := ClusterInfo{Context: kubeContext}
	if info.Context == "" {
		info.Context = raw.CurrentContext
	}
	if kc, ok := raw.Contexts[info.Context]; ok {
		if cl, ok := raw.Clusters[kc.Cluster]; ok {
			info.Server = cl.Server
		}
	}
	restCfg, err := cfg.ClientConfig()
	if err != nil {
		return nil, info, err
	}
	if info.Server == "" {
		info.Server = restCfg.Host
	}
	cs, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, info, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ns, err := cs.CoreV1().Namespaces().Get(ctx, "kube-system", metav1.GetOptions{})
	if err != nil {
		return nil, info, fmt.Errorf("read cluster identity (kube-system namespace) on %s: %w", info.Server, err)
	}
	info.ClusterID = string(ns.UID)
	return cs, info, nil
}

func promptConfirm(in io.Reader, out io.Writer, cluster ClusterInfo) (bool, error) {
	f, ok := in.(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) { // #nosec G115 -- file descriptors fit in int.
		return false, core.NewWithSentinel(core.ErrUpdateConfirmationMissing, "refusing to update without confirmation: stdin is not a terminal; review the plan (or --dry-run) and pass --yes")
	}
	fmt.Fprintf(out, "\nApply this update to context %q (cluster %s)? Type 'yes' to continue: ", cluster.Context, cluster.ClusterID)
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	return strings.TrimSpace(line) == "yes", nil
}

func writeJSON(out io.Writer, plan *Plan, res *Result, dryRun bool) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(struct {
		DryRun bool    `json:"dryRun"`
		Plan   *Plan   `json:"plan"`
		Result *Result `json:"result,omitempty"`
	}{DryRun: dryRun, Plan: plan, Result: res})
}
