package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

// NewBootstrapCmd provides an on-prem focused bootstrap workflow.
//
// Production note: this intentionally does not attempt to provision clusters across all distributions.
// It performs preflights and (optionally) applies a small set of safe, local-distro-specific fixes.
func NewBootstrapCmd(logger *zap.Logger) *cobra.Command {
	var apply bool
	var provider string

	cmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Bootstrap cluster prerequisites (on-prem focused)",
		Long: `Bootstrap validates and (optionally) installs cluster prerequisites needed by mcp-runtime setup.

By design, this does not provision Kubernetes clusters end-to-end across all distributions.
Use this to prepare an existing cluster for running 'mcp-runtime setup'.

Note: bootstrap --apply is automated for k3s only and must be executed on the k3s server node (it expects local manifests under /var/lib/rancher/k3s/server/manifests).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			Section("MCP Runtime Bootstrap")

			chosenProvider := provider
			if chosenProvider == "" || chosenProvider == "auto" {
				detectedProvider, err := detectProvider(kubectlClient)
				if err != nil {
					return err
				}
				chosenProvider = detectedProvider
			}
			Info(fmt.Sprintf("Provider: %s", chosenProvider))

			if err := runBootstrapPreflight(kubectlClient); err != nil {
				return err
			}

			if !apply {
				Success("Bootstrap preflight complete (no changes applied)")
				Info("Next: run `./bin/mcp-runtime setup` (or `./bin/mcp-runtime setup --storage-mode hostpath` for single-node dev)")
				return nil
			}

			switch chosenProvider {
			case "k3s":
				if err := bootstrapApplyK3s(kubectlClient); err != nil {
					return err
				}
			case "rke2", "kubeadm", "generic":
				Warn("Apply mode is currently only automated for k3s. For other distributions, use the preflight output and install DNS/storage/ingress/load-balancer via your standard platform tooling.")
			default:
				Warn(fmt.Sprintf("Unknown provider %q; skipping apply", chosenProvider))
			}

			Success("Bootstrap complete")
			Info("Next: run `./bin/mcp-runtime setup`")
			return nil
		},
	}

	cmd.Flags().BoolVar(&apply, "apply", false, "Apply safe bootstrap fixes when possible (k3s only today; run on the k3s server node)")
	cmd.Flags().StringVar(&provider, "provider", "auto", "Cluster provider hint (auto|k3s|rke2|kubeadm|generic)")
	return cmd
}

func detectProvider(kubectl KubectlRunner) (string, error) {
	out, err := kubectlOutput(kubectl, []string{"get", "nodes", "-o", "jsonpath={range .items[*]}{.status.nodeInfo.kubeletVersion}{\"\\n\"}{end}"})
	if err != nil {
		return "", wrapWithSentinel(ErrClusterNotAccessible, err, fmt.Sprintf("kubectl get nodes failed: %v", err))
	}
	lower := strings.ToLower(string(out))
	switch {
	case strings.Contains(lower, "k3s"):
		return "k3s", nil
	case strings.Contains(lower, "rke2"):
		return "rke2", nil
	default:
		return "generic", nil
	}
}

func runBootstrapPreflight(kubectl KubectlRunner) error {
	Info("Preflight: kubectl connectivity")
	if err := kubectl.Run([]string{"version", "--client=true"}); err != nil {
		return wrapWithSentinel(ErrClusterNotAccessible, err, fmt.Sprintf("kubectl not available: %v", err))
	}
	if err := kubectl.Run([]string{"get", "nodes"}); err != nil {
		return wrapWithSentinel(ErrClusterNotAccessible, err, fmt.Sprintf("kubectl cannot reach cluster: %v", err))
	}

	Info("Preflight: CoreDNS")
	if err := checkDeploymentExists(kubectl, "kube-system", "coredns"); err != nil {
		Warn("CoreDNS not detected (kube-system/deployment coredns). Cluster DNS must be installed for in-cluster service discovery.")
	}

	Info("Preflight: Default StorageClass")
	if err := checkHasDefaultStorageClass(kubectl); err != nil {
		Warn(fmt.Sprintf("No default StorageClass detected: %v", err))
	}

	Info("Preflight: IngressClass traefik")
	if err := kubectl.Run([]string{"get", "ingressclass", "traefik"}); err != nil {
		Warn("IngressClass traefik not found. If you plan to use Traefik, install it before running setup (or let setup install it when configured).")
	}

	Info("Preflight: MetalLB")
	if err := kubectl.Run([]string{"get", "ns", "metallb-system"}); err != nil {
		Warn("MetalLB not detected (namespace metallb-system). If you need LoadBalancer services on bare metal, install MetalLB.")
	}

	return nil
}

func checkDeploymentExists(kubectl KubectlRunner, namespace, name string) error {
	return kubectl.Run([]string{"get", "deployment", name, "-n", namespace})
}

func checkHasDefaultStorageClass(kubectl KubectlRunner) error {
	out, err := kubectlOutput(kubectl, []string{"get", "storageclass", "-o", "jsonpath={range .items[*]}{.metadata.name}{\" \"}{.metadata.annotations.storageclass\\.kubernetes\\.io/is-default-class}{\"\\n\"}{end}"})
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == "true" {
			return nil
		}
	}
	return fmt.Errorf("no StorageClass annotated with storageclass.kubernetes.io/is-default-class=true")
}

func bootstrapApplyK3s(kubectl KubectlRunner) error {
	Info("Applying k3s addons: CoreDNS + local-path provisioner (if missing)")

	// Apply only when the manifests exist on disk (k3s server).
	paths := []string{
		"/var/lib/rancher/k3s/server/manifests/coredns.yaml",
		"/var/lib/rancher/k3s/server/manifests/local-storage.yaml",
	}
	var missing []string
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		msg := fmt.Sprintf("k3s manifests missing on disk (%s); bootstrap --apply expects to run on the k3s server node", strings.Join(missing, ", "))
		return wrapWithSentinel(ErrClusterConfigFailed, fmt.Errorf("missing manifests"), msg)
	}

	for _, p := range paths {
		if err := kubectl.Run([]string{"apply", "-f", p}); err != nil {
			return wrapWithSentinel(ErrClusterConfigFailed, err, fmt.Sprintf("failed to apply %s: %v", p, err))
		}
	}

	Info("Waiting for kube-system addons to be ready")
	if err := kubectl.Run([]string{"rollout", "status", "deployment/coredns", "-n", "kube-system", "--timeout=180s"}); err != nil {
		return wrapWithSentinel(ErrDeploymentTimeout, err, fmt.Sprintf("coredns rollout failed: %v", err))
	}
	if err := kubectl.Run([]string{"rollout", "status", "deployment/local-path-provisioner", "-n", "kube-system", "--timeout=180s"}); err != nil {
		return wrapWithSentinel(ErrDeploymentTimeout, err, fmt.Sprintf("local-path-provisioner rollout failed: %v", err))
	}

	// Best-effort: show disk-pressure so users don't get surprised by evictions.
	Info("Node disk-pressure check")
	cond, err := kubectlOutput(kubectl, []string{"get", "nodes", "-o", "jsonpath={range .items[*]}{.metadata.name}{\" \"}{range .status.conditions[?(@.type==\"DiskPressure\")]}{.status}{end}{\"\\n\"}{end}"})
	if err == nil {
		Info(strings.TrimSpace(string(cond)))
	}

	return nil
}

func kubectlOutput(kubectl KubectlRunner, args []string) ([]byte, error) {
	cmd, err := kubectl.CommandArgs(args)
	if err != nil {
		return nil, err
	}
	return cmd.Output()
}
