package cli

// This file implements the "cluster doctor" preflight command.
// It detects the Kubernetes distribution, checks that platform prerequisites
// (registry service, in-cluster DNS for registry.local) are in place, and
// prints distribution-specific remediation when they aren't. See
// docs/cluster-readiness.md for the full list of per-distribution prereqs.

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// Distribution identifies a Kubernetes flavor for remediation messaging.
type Distribution string

const (
	DistroK3s           Distribution = "k3s"
	DistroKind          Distribution = "kind"
	DistroMinikube      Distribution = "minikube"
	DistroDockerDesktop Distribution = "docker-desktop"
	DistroGeneric       Distribution = "generic"
)

// DoctorCheck is a single preflight check result.
type DoctorCheck struct {
	Name   string
	OK     bool
	Detail string
	Remedy string // Short hint; detailed steps come from the distro checklist.
}

// DoctorReport aggregates the full preflight result.
type DoctorReport struct {
	Distribution Distribution
	Checks       []DoctorCheck
}

// AllOK reports whether every check passed.
func (r DoctorReport) AllOK() bool {
	for _, c := range r.Checks {
		if !c.OK {
			return false
		}
	}
	return true
}

func (m *ClusterManager) newClusterDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Preflight: verify the cluster is ready for mcp-runtime setup",
		Long: "Detect the Kubernetes distribution and check that the registry service, cluster DNS, " +
			"and node-side registry mirror are wired up. Prints remediation steps for your distribution " +
			"when something is missing. See docs/cluster-readiness.md for the full per-distribution checklist.",
		RunE: func(cmd *cobra.Command, args []string) error {
			report := RunDoctor(m.kubectl)
			PrintDoctorReport(report)
			if !report.AllOK() {
				return newWithSentinel(ErrSetupStepFailed, "cluster doctor found unmet prerequisites; see docs/cluster-readiness.md")
			}
			return nil
		},
	}
}

// RunDoctor executes all preflight checks and returns a report.
func RunDoctor(kubectl KubectlRunner) DoctorReport {
	distro := DetectDistribution(kubectl)
	return DoctorReport{
		Distribution: distro,
		Checks: []DoctorCheck{
			checkRegistryService(kubectl),
			checkRegistryReachableFromCluster(kubectl),
		},
	}
}

// DetectDistribution inspects node info to guess which distribution is running.
// This is best-effort: callers should treat DistroGeneric as "probably kubeadm/unknown".
func DetectDistribution(kubectl KubectlRunner) Distribution {
	cmd, err := kubectl.CommandArgs([]string{"get", "nodes", "-o", "jsonpath={.items[*].status.nodeInfo.kubeletVersion}"})
	if err == nil {
		if out, err := cmd.Output(); err == nil {
			v := strings.ToLower(string(out))
			if strings.Contains(v, "+k3s") {
				return DistroK3s
			}
		}
	}

	cmd, err = kubectl.CommandArgs([]string{"get", "nodes", "-o", "jsonpath={.items[*].metadata.name}"})
	if err == nil {
		if out, err := cmd.Output(); err == nil {
			names := strings.ToLower(string(out))
			switch {
			case strings.Contains(names, "kind-"):
				return DistroKind
			case strings.Contains(names, "minikube"):
				return DistroMinikube
			case strings.Contains(names, "docker-desktop"):
				return DistroDockerDesktop
			}
		}
	}

	cmd, err = kubectl.CommandArgs([]string{"config", "current-context"})
	if err == nil {
		if out, err := cmd.Output(); err == nil {
			ctx := strings.ToLower(strings.TrimSpace(string(out)))
			switch {
			case strings.HasPrefix(ctx, "kind-"):
				return DistroKind
			case strings.HasPrefix(ctx, "minikube"):
				return DistroMinikube
			case ctx == "docker-desktop":
				return DistroDockerDesktop
			}
		}
	}

	return DistroGeneric
}

func checkRegistryService(kubectl KubectlRunner) DoctorCheck {
	cmd, err := kubectl.CommandArgs([]string{"get", "svc", "-n", "registry", "registry", "-o", "jsonpath={.spec.ports[0].nodePort}"})
	if err != nil {
		return DoctorCheck{Name: "registry Service", OK: false, Detail: fmt.Sprintf("kubectl error: %v", err), Remedy: "run `./bin/mcp-runtime setup` to install the registry, or check cluster connectivity"}
	}
	out, err := cmd.Output()
	port := strings.TrimSpace(string(out))
	if err != nil || port == "" {
		return DoctorCheck{
			Name:   "registry Service",
			OK:     false,
			Detail: "Service registry/registry not found or has no NodePort",
			Remedy: "run `./bin/mcp-runtime setup` to install the registry",
		}
	}
	return DoctorCheck{
		Name:   "registry Service",
		OK:     true,
		Detail: fmt.Sprintf("NodePort %s", port),
	}
}

// checkRegistryReachableFromCluster verifies that an in-cluster pod can talk to
// the registry over the cluster-internal service DNS. This exercises the same
// path the in-cluster push helper uses, so a failure here means `registry push
// --mode=in-cluster` will also fail. Kubelet's pull path (node-side containerd
// with registries.yaml mirrors) is distribution-specific and surfaced via the
// remediation hint, not as a pass/fail check — we can't reach into kubelet
// non-destructively.
func checkRegistryReachableFromCluster(kubectl KubectlRunner) DoctorCheck {
	podName := fmt.Sprintf("mcp-runtime-doctor-curl-%d", time.Now().UnixNano())
	args := []string{
		"run", "-n", "registry",
		"--rm", "--restart=Never", "--attach",
		"--pod-running-timeout=30s",
		"--quiet",
		"--image=curlimages/curl:8.7.1",
		podName,
		"--command", "--", "curl", "-sSI", "--connect-timeout", "5", "--max-time", "15",
		"http://registry.registry.svc.cluster.local:5000/v2/",
	}
	cmd, err := kubectl.CommandArgs(args)
	if err != nil {
		return DoctorCheck{
			Name:   "registry reachability (in-cluster)",
			OK:     false,
			Detail: fmt.Sprintf("kubectl error: %v", err),
			Remedy: "check cluster connectivity and kubeconfig",
		}
	}
	out, runErr := cmd.CombinedOutput()
	body := string(out)
	if runErr != nil {
		return DoctorCheck{
			Name:   "registry reachability (in-cluster)",
			OK:     false,
			Detail: fmt.Sprintf("helper pod failed: %v", runErr),
			Remedy: "run `./bin/mcp-runtime setup` if the registry is missing; check `kubectl -n registry get pods`",
		}
	}
	if !hasHTTP200Status(body) {
		return DoctorCheck{
			Name:   "registry reachability (in-cluster)",
			OK:     false,
			Detail: fmt.Sprintf("unexpected response: %q", strings.TrimSpace(body)),
			Remedy: "inspect the registry deployment: `kubectl -n registry get pods -o wide`",
		}
	}
	return DoctorCheck{
		Name:   "registry reachability (in-cluster)",
		OK:     true,
		Detail: "HTTP 200 from registry.registry.svc.cluster.local:5000/v2/",
	}
}

func hasHTTP200Status(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "HTTP/") {
			continue
		}
		fields := strings.Fields(line)
		return len(fields) >= 2 && fields[1] == "200"
	}
	return false
}

// PrintDoctorReport emits a human-readable report using the standard printer.
func PrintDoctorReport(r DoctorReport) {
	Section("Cluster Doctor")
	Info(fmt.Sprintf("Distribution: %s", r.Distribution))
	for _, c := range r.Checks {
		if c.OK {
			Success(fmt.Sprintf("%s — %s", c.Name, c.Detail))
			continue
		}
		Error(fmt.Sprintf("%s — %s", c.Name, c.Detail))
		if c.Remedy != "" {
			Info("  Remedy: " + c.Remedy)
		}
	}
	if !r.AllOK() {
		Info("")
		Info("Full remediation steps per distribution are in docs/cluster-readiness.md.")
		Info(remediationHint(r.Distribution))
	}
}

func remediationHint(d Distribution) string {
	switch d {
	case DistroK3s:
		return "k3s: write /etc/rancher/k3s/registries.yaml mapping registry.local -> http://127.0.0.1:<NodePort>, add 127.0.0.1 registry.local to /etc/hosts, then `systemctl restart k3s`."
	case DistroKind:
		return "kind: recreate the cluster with containerdConfigPatches for the mirror and extraPortMappings for the NodePort, or use `kind load docker-image`."
	case DistroMinikube:
		return "minikube: start with `--insecure-registry=registry.local`, enable the ingress addon, and map registry.local in /etc/hosts to $(minikube ip)."
	case DistroDockerDesktop:
		return "Docker Desktop: add \"insecure-registries\": [\"registry.local\"] in Docker Engine settings and add 127.0.0.1 registry.local to /etc/hosts."
	default:
		return "generic k8s: edit /etc/containerd/config.toml on each node to add a mirror for registry.local -> http://<reachable>:<NodePort>, map /etc/hosts, and `systemctl restart containerd`."
	}
}
