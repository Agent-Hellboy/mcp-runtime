package cli

// This file implements the "cluster doctor" preflight command.
// It detects the Kubernetes distribution, checks that platform prerequisites
// (registry service, in-cluster DNS for registry.local) are in place, and
// prints distribution-specific remediation when they aren't. See
// docs/cluster-readiness.md for the full list of per-distribution prereqs.

import (
	"encoding/base64"
	"fmt"
	"strconv"
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

const (
	doctorMCPServersNamespace = "mcp-servers"
	doctorTraefikNamespace    = "traefik"
	doctorTraefikServiceName  = "traefik"
	doctorTraefikWebPort      = 8000
	doctorSentinelNamespace   = "mcp-sentinel"
	doctorSentinelAPIService  = "mcp-sentinel-api"
)

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
			"operator/CRD prerequisites, and ingress (Traefik) wiring are in place. Prints remediation steps for your distribution " +
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
			checkNamespaceExists(kubectl, doctorMCPServersNamespace),
			checkNamespaceDefaultServiceAccount(kubectl, doctorMCPServersNamespace),
			checkNamespacePolicyGuardrails(kubectl, doctorMCPServersNamespace),
			checkNamespacePodAdmission(kubectl, doctorMCPServersNamespace),
			checkMCPServerCRD(kubectl),
			checkOperatorReady(kubectl),
			checkOperatorRecentReconcileErrors(kubectl),
			checkTraefikIngressClass(kubectl),
			checkTraefikDeploymentReady(kubectl),
			checkTraefikWebEntrypoint(kubectl),
			checkTraefikServiceExposure(kubectl),
			checkMCPServersDNSAndNetwork(kubectl),
			checkIngressRouteProbe(kubectl, doctorMCPServersNamespace),
			checkRegistryService(kubectl),
			checkRegistryReachableFromCluster(kubectl),
			checkMCPServersImagePullSecrets(kubectl, doctorMCPServersNamespace),
			checkMCPServersImagePullSmoke(kubectl, doctorMCPServersNamespace),
			checkSentinelSecrets(kubectl),
			checkSentinelAPIAuthProbe(kubectl),
			checkNodeCapacity(kubectl),
			checkPendingPodsByNamespace(kubectl),
			checkMCPServerReconcileSmoke(kubectl, doctorMCPServersNamespace),
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

func checkNamespaceExists(kubectl KubectlRunner, namespace string) DoctorCheck {
	cmd, err := kubectl.CommandArgs([]string{"get", "namespace", namespace, "-o", "jsonpath={.metadata.name}"})
	if err != nil {
		return DoctorCheck{
			Name:   fmt.Sprintf("namespace %s", namespace),
			OK:     false,
			Detail: fmt.Sprintf("kubectl error: %v", err),
			Remedy: "check cluster connectivity and kubeconfig",
		}
	}
	out, err := cmd.Output()
	got := strings.TrimSpace(string(out))
	if err != nil || got != namespace {
		return DoctorCheck{
			Name:   fmt.Sprintf("namespace %s", namespace),
			OK:     false,
			Detail: fmt.Sprintf("namespace %s not found", namespace),
			Remedy: "run `./bin/mcp-runtime setup` to create the runtime namespaces",
		}
	}
	return DoctorCheck{
		Name:   fmt.Sprintf("namespace %s", namespace),
		OK:     true,
		Detail: "present",
	}
}

func checkNamespaceDefaultServiceAccount(kubectl KubectlRunner, namespace string) DoctorCheck {
	cmd, err := kubectl.CommandArgs([]string{"get", "serviceaccount", "default", "-n", namespace, "-o", "jsonpath={.metadata.name}"})
	if err != nil {
		return DoctorCheck{
			Name:   fmt.Sprintf("namespace %s default serviceaccount", namespace),
			OK:     false,
			Detail: fmt.Sprintf("kubectl error: %v", err),
			Remedy: "check namespace permissions and kubeconfig",
		}
	}
	out, err := cmd.Output()
	name := strings.TrimSpace(string(out))
	if err != nil || name != "default" {
		return DoctorCheck{
			Name:   fmt.Sprintf("namespace %s default serviceaccount", namespace),
			OK:     false,
			Detail: "serviceaccount default missing",
			Remedy: fmt.Sprintf("recreate the namespace or run `kubectl create serviceaccount default -n %s`", namespace),
		}
	}
	return DoctorCheck{
		Name:   fmt.Sprintf("namespace %s default serviceaccount", namespace),
		OK:     true,
		Detail: "present",
	}
}

func checkNamespacePolicyGuardrails(kubectl KubectlRunner, namespace string) DoctorCheck {
	cmd, err := kubectl.CommandArgs([]string{"get", "resourcequota,limitrange", "-n", namespace, "--no-headers", "-o", "name"})
	if err != nil {
		return DoctorCheck{
			Name:   fmt.Sprintf("namespace %s quota/limitrange", namespace),
			OK:     false,
			Detail: fmt.Sprintf("kubectl error: %v", err),
			Remedy: "verify RBAC allows listing quota and limitrange resources",
		}
	}
	out, execErr := cmd.CombinedOutput()
	if execErr != nil {
		return DoctorCheck{
			Name:   fmt.Sprintf("namespace %s quota/limitrange", namespace),
			OK:     false,
			Detail: strings.TrimSpace(string(out)),
			Remedy: "inspect namespace policies: `kubectl get resourcequota,limitrange -n mcp-servers`",
		}
	}
	listing := strings.TrimSpace(string(out))
	if listing == "" {
		return DoctorCheck{
			Name:   fmt.Sprintf("namespace %s quota/limitrange", namespace),
			OK:     true,
			Detail: "no ResourceQuota/LimitRange defined",
		}
	}
	count := len(strings.Split(listing, "\n"))
	return DoctorCheck{
		Name:   fmt.Sprintf("namespace %s quota/limitrange", namespace),
		OK:     true,
		Detail: fmt.Sprintf("%d policy objects detected", count),
	}
}

func checkNamespacePodAdmission(kubectl KubectlRunner, namespace string) DoctorCheck {
	podName := fmt.Sprintf("doctor-admission-%d", time.Now().UnixNano())
	manifest := fmt.Sprintf(`apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: %s
spec:
  restartPolicy: Never
  containers:
  - name: pause
    image: registry.k8s.io/pause:3.9
`, podName, namespace)
	cmd, err := kubectl.CommandArgs([]string{"apply", "--dry-run=server", "-f", "-"})
	if err != nil {
		return DoctorCheck{
			Name:   fmt.Sprintf("namespace %s pod admission", namespace),
			OK:     false,
			Detail: fmt.Sprintf("kubectl error: %v", err),
			Remedy: "check API server admission webhooks and RBAC",
		}
	}
	cmd.SetStdin(strings.NewReader(manifest))
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		return DoctorCheck{
			Name:   fmt.Sprintf("namespace %s pod admission", namespace),
			OK:     false,
			Detail: strings.TrimSpace(string(out)),
			Remedy: "inspect ResourceQuota/LimitRange/admission policies blocking pod creation",
		}
	}
	return DoctorCheck{
		Name:   fmt.Sprintf("namespace %s pod admission", namespace),
		OK:     true,
		Detail: "server-side dry-run pod creation succeeded",
	}
}

func checkMCPServerCRD(kubectl KubectlRunner) DoctorCheck {
	crd := "mcpservers.mcpruntime.org"
	cmd, err := kubectl.CommandArgs([]string{"get", "crd", crd, "-o", "jsonpath={.metadata.name}"})
	if err != nil {
		return DoctorCheck{
			Name:   "MCPServer CRD",
			OK:     false,
			Detail: fmt.Sprintf("kubectl error: %v", err),
			Remedy: "run `./bin/mcp-runtime setup` to install CRDs",
		}
	}
	out, err := cmd.Output()
	got := strings.TrimSpace(string(out))
	if err != nil || got != crd {
		return DoctorCheck{
			Name:   "MCPServer CRD",
			OK:     false,
			Detail: fmt.Sprintf("CRD %s not found", crd),
			Remedy: "apply CRDs (for example `make manifests` then `kubectl apply -f config/crd/bases`)",
		}
	}
	return DoctorCheck{
		Name:   "MCPServer CRD",
		OK:     true,
		Detail: crd,
	}
}

func checkOperatorReady(kubectl KubectlRunner) DoctorCheck {
	deployName := "mcp-runtime-operator-controller-manager"
	ns := "mcp-runtime"
	cmd, err := kubectl.CommandArgs([]string{"get", "deploy", "-n", ns, deployName, "-o", "jsonpath={.status.readyReplicas}/{.spec.replicas}"})
	if err != nil {
		return DoctorCheck{
			Name:   "operator readiness",
			OK:     false,
			Detail: fmt.Sprintf("kubectl error: %v", err),
			Remedy: "run `./bin/mcp-runtime setup` to install the operator",
		}
	}
	out, err := cmd.Output()
	pair := strings.TrimSpace(string(out))
	if err != nil || pair == "" {
		return DoctorCheck{
			Name:   "operator readiness",
			OK:     false,
			Detail: fmt.Sprintf("deployment %s/%s not found", ns, deployName),
			Remedy: "run `./bin/mcp-runtime setup` to install the operator",
		}
	}
	parts := strings.SplitN(pair, "/", 2)
	if len(parts) != 2 {
		return DoctorCheck{
			Name:   "operator readiness",
			OK:     false,
			Detail: fmt.Sprintf("unexpected replica status %q", pair),
			Remedy: "inspect `kubectl -n mcp-runtime get deploy mcp-runtime-operator-controller-manager -o wide`",
		}
	}
	ready, readyErr := strconv.Atoi(strings.TrimSpace(parts[0]))
	desired, desiredErr := strconv.Atoi(strings.TrimSpace(parts[1]))
	if readyErr != nil || desiredErr != nil {
		return DoctorCheck{
			Name:   "operator readiness",
			OK:     false,
			Detail: fmt.Sprintf("unexpected replica status %q", pair),
			Remedy: "inspect `kubectl -n mcp-runtime get deploy mcp-runtime-operator-controller-manager -o wide`",
		}
	}
	if desired == 0 || ready < desired {
		return DoctorCheck{
			Name:   "operator readiness",
			OK:     false,
			Detail: fmt.Sprintf("%d/%d replicas ready", ready, desired),
			Remedy: "check operator pods: `kubectl -n mcp-runtime get pods -l control-plane=controller-manager`",
		}
	}
	return DoctorCheck{
		Name:   "operator readiness",
		OK:     true,
		Detail: fmt.Sprintf("%d/%d replicas ready", ready, desired),
	}
}

func checkOperatorRecentReconcileErrors(kubectl KubectlRunner) DoctorCheck {
	cmd, err := kubectl.CommandArgs([]string{"logs", "-n", "mcp-runtime", "deploy/mcp-runtime-operator-controller-manager", "--since=10m"})
	if err != nil {
		return DoctorCheck{
			Name:   "operator reconcile errors (last 10m)",
			OK:     false,
			Detail: fmt.Sprintf("kubectl error: %v", err),
			Remedy: "verify operator deployment exists and logs are accessible",
		}
	}
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		return DoctorCheck{
			Name:   "operator reconcile errors (last 10m)",
			OK:     false,
			Detail: strings.TrimSpace(string(out)),
			Remedy: "inspect operator logs directly and fix reconcile failures",
		}
	}
	logs := strings.ToLower(string(out))
	patterns := []string{"reconciler error", "failed to reconcile", "error syncing"}
	for _, p := range patterns {
		if strings.Contains(logs, p) {
			return DoctorCheck{
				Name:   "operator reconcile errors (last 10m)",
				OK:     false,
				Detail: fmt.Sprintf("detected %q in recent operator logs", p),
				Remedy: "inspect `kubectl logs -n mcp-runtime deploy/mcp-runtime-operator-controller-manager --since=10m`",
			}
		}
	}
	return DoctorCheck{
		Name:   "operator reconcile errors (last 10m)",
		OK:     true,
		Detail: "no reconcile error patterns detected",
	}
}

func checkTraefikIngressClass(kubectl KubectlRunner) DoctorCheck {
	cmd, err := kubectl.CommandArgs([]string{"get", "ingressclass", "traefik", "-o", "jsonpath={.metadata.name}"})
	if err != nil {
		return DoctorCheck{
			Name:   "traefik ingressClass",
			OK:     false,
			Detail: fmt.Sprintf("kubectl error: %v", err),
			Remedy: "install or expose Traefik ingress controller",
		}
	}
	out, err := cmd.Output()
	got := strings.TrimSpace(string(out))
	if err != nil || got != "traefik" {
		return DoctorCheck{
			Name:   "traefik ingressClass",
			OK:     false,
			Detail: "ingressClass traefik not found",
			Remedy: "ensure Traefik is installed and ingressClassName is `traefik`",
		}
	}
	return DoctorCheck{
		Name:   "traefik ingressClass",
		OK:     true,
		Detail: "present",
	}
}

func checkTraefikDeploymentReady(kubectl KubectlRunner) DoctorCheck {
	cmd, err := kubectl.CommandArgs([]string{"get", "deploy", "-n", doctorTraefikNamespace, doctorTraefikServiceName, "-o", "jsonpath={.status.readyReplicas}/{.spec.replicas}"})
	if err != nil {
		return DoctorCheck{
			Name:   "traefik deployment readiness",
			OK:     false,
			Detail: fmt.Sprintf("kubectl error: %v", err),
			Remedy: "install Traefik deployment in namespace `traefik`",
		}
	}
	out, execErr := cmd.Output()
	pair := strings.TrimSpace(string(out))
	if execErr != nil || pair == "" {
		return DoctorCheck{
			Name:   "traefik deployment readiness",
			OK:     false,
			Detail: fmt.Sprintf("deployment %s/%s not found", doctorTraefikNamespace, doctorTraefikServiceName),
			Remedy: "install Traefik deployment in namespace `traefik`",
		}
	}
	parts := strings.SplitN(pair, "/", 2)
	if len(parts) != 2 {
		return DoctorCheck{
			Name:   "traefik deployment readiness",
			OK:     false,
			Detail: fmt.Sprintf("unexpected replica status %q", pair),
			Remedy: "inspect `kubectl -n traefik get deploy traefik -o wide`",
		}
	}
	ready, readyErr := strconv.Atoi(strings.TrimSpace(parts[0]))
	desired, desiredErr := strconv.Atoi(strings.TrimSpace(parts[1]))
	if readyErr != nil || desiredErr != nil || desired == 0 || ready < desired {
		return DoctorCheck{
			Name:   "traefik deployment readiness",
			OK:     false,
			Detail: fmt.Sprintf("%s replicas ready", pair),
			Remedy: "check Traefik pods and events: `kubectl -n traefik get pods`",
		}
	}
	return DoctorCheck{
		Name:   "traefik deployment readiness",
		OK:     true,
		Detail: fmt.Sprintf("%s replicas ready", pair),
	}
}

func checkTraefikWebEntrypoint(kubectl KubectlRunner) DoctorCheck {
	cmd, err := kubectl.CommandArgs([]string{"get", "svc", "-n", doctorTraefikNamespace, doctorTraefikServiceName, "-o", "jsonpath={range .spec.ports[*]}{.name}:{.port}:{.nodePort}{\"\\n\"}{end}"})
	if err != nil {
		return DoctorCheck{
			Name:   "traefik web entrypoint",
			OK:     false,
			Detail: fmt.Sprintf("kubectl error: %v", err),
			Remedy: "install Traefik service in namespace `traefik`",
		}
	}
	out, err := cmd.Output()
	if err != nil {
		return DoctorCheck{
			Name:   "traefik web entrypoint",
			OK:     false,
			Detail: "service traefik/traefik not found",
			Remedy: "install Traefik service in namespace `traefik`",
		}
	}
	ports := strings.TrimSpace(string(out))
	for _, line := range strings.Split(ports, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, fmt.Sprintf(":%d:", doctorTraefikWebPort)) {
			return DoctorCheck{
				Name:   "traefik web entrypoint",
				OK:     true,
				Detail: fmt.Sprintf("service %s/%s exposes port %d (web)", doctorTraefikNamespace, doctorTraefikServiceName, doctorTraefikWebPort),
			}
		}
	}
	return DoctorCheck{
		Name:   "traefik web entrypoint",
		OK:     false,
		Detail: fmt.Sprintf("service traefik/traefik ports: %q", ports),
		Remedy: "ensure Traefik `web` entrypoint is exposed on service port 8000",
	}
}

func checkTraefikServiceExposure(kubectl KubectlRunner) DoctorCheck {
	cmd, err := kubectl.CommandArgs([]string{"get", "svc", "-n", doctorTraefikNamespace, doctorTraefikServiceName, "-o", "jsonpath={.spec.type}|{.status.loadBalancer.ingress[0].ip}|{.status.loadBalancer.ingress[0].hostname}|{range .spec.ports[*]}{.port}:{.nodePort}{\",\"}{end}"})
	if err != nil {
		return DoctorCheck{
			Name:   "traefik service exposure",
			OK:     false,
			Detail: fmt.Sprintf("kubectl error: %v", err),
			Remedy: "ensure traefik service exists",
		}
	}
	out, execErr := cmd.Output()
	if execErr != nil {
		return DoctorCheck{
			Name:   "traefik service exposure",
			OK:     false,
			Detail: "failed reading traefik service exposure fields",
			Remedy: "inspect `kubectl -n traefik get svc traefik -o wide`",
		}
	}
	parts := strings.SplitN(strings.TrimSpace(string(out)), "|", 4)
	if len(parts) < 4 {
		return DoctorCheck{
			Name:   "traefik service exposure",
			OK:     false,
			Detail: fmt.Sprintf("unexpected service exposure payload %q", strings.TrimSpace(string(out))),
			Remedy: "inspect `kubectl -n traefik get svc traefik -o yaml`",
		}
	}
	svcType := strings.TrimSpace(parts[0])
	lbIP := strings.TrimSpace(parts[1])
	lbHost := strings.TrimSpace(parts[2])
	ports := strings.TrimSpace(parts[3])
	if svcType == "LoadBalancer" && (lbIP != "" || lbHost != "") {
		addr := lbIP
		if addr == "" {
			addr = lbHost
		}
		return DoctorCheck{
			Name:   "traefik service exposure",
			OK:     true,
			Detail: fmt.Sprintf("LoadBalancer ready at %s", addr),
		}
	}
	if strings.Contains(ports, fmt.Sprintf("%d:", doctorTraefikWebPort)) {
		return DoctorCheck{
			Name:   "traefik service exposure",
			OK:     true,
			Detail: fmt.Sprintf("%s service exposes nodePort for %d", svcType, doctorTraefikWebPort),
		}
	}
	return DoctorCheck{
		Name:   "traefik service exposure",
		OK:     false,
		Detail: fmt.Sprintf("service type=%s exposure not ready (lbIP=%q lbHost=%q ports=%q)", svcType, lbIP, lbHost, ports),
		Remedy: "ensure Traefik service has an external LoadBalancer address or NodePort for web entrypoint",
	}
}

func checkMCPServersDNSAndNetwork(kubectl KubectlRunner) DoctorCheck {
	podName := fmt.Sprintf("mcp-runtime-doctor-dns-%d", time.Now().UnixNano())
	args := []string{
		"run", "-n", doctorMCPServersNamespace,
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
			Name:   "mcp-servers DNS/network",
			OK:     false,
			Detail: fmt.Sprintf("kubectl error: %v", err),
			Remedy: "check kubeconfig and namespace access",
		}
	}
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		return DoctorCheck{
			Name:   "mcp-servers DNS/network",
			OK:     false,
			Detail: strings.TrimSpace(string(out)),
			Remedy: "check CoreDNS and network policies for namespace mcp-servers",
		}
	}
	if !hasHTTP200Status(string(out)) {
		return DoctorCheck{
			Name:   "mcp-servers DNS/network",
			OK:     false,
			Detail: fmt.Sprintf("unexpected response: %q", strings.TrimSpace(string(out))),
			Remedy: "check CoreDNS and service routing from namespace mcp-servers",
		}
	}
	return DoctorCheck{
		Name:   "mcp-servers DNS/network",
		OK:     true,
		Detail: "can resolve and reach registry service from mcp-servers namespace",
	}
}

func checkIngressRouteProbe(kubectl KubectlRunner, namespace string) DoctorCheck {
	ingressName, err := readKubectlOutput(kubectl, []string{"get", "ingress", "-n", namespace, "-o", "jsonpath={.items[0].metadata.name}"})
	if err != nil {
		return DoctorCheck{
			Name:   "ingress route probe",
			OK:     true,
			Detail: "no ingress resources found in mcp-servers; skipping live route probe",
		}
	}
	ingressName = strings.TrimSpace(ingressName)
	if ingressName == "" {
		return DoctorCheck{
			Name:   "ingress route probe",
			OK:     true,
			Detail: "no ingress resources found in mcp-servers; skipping live route probe",
		}
	}
	host, hostErr := readKubectlOutput(kubectl, []string{"get", "ingress", ingressName, "-n", namespace, "-o", "jsonpath={.spec.rules[0].host}"})
	if hostErr != nil {
		return DoctorCheck{
			Name:   "ingress route probe",
			OK:     false,
			Detail: fmt.Sprintf("failed reading ingress host: %v", hostErr),
			Remedy: "inspect ingress rule structure",
		}
	}
	path, pathErr := readKubectlOutput(kubectl, []string{"get", "ingress", ingressName, "-n", namespace, "-o", "jsonpath={.spec.rules[0].http.paths[0].path}"})
	if pathErr != nil {
		return DoctorCheck{
			Name:   "ingress route probe",
			OK:     false,
			Detail: fmt.Sprintf("failed reading ingress path: %v", pathErr),
			Remedy: "inspect ingress rule structure",
		}
	}
	host = strings.TrimSpace(host)
	path = doctorNormalizePath(strings.TrimSpace(path))
	if path == "" {
		path = "/"
	}
	podName := fmt.Sprintf("mcp-runtime-doctor-ingress-%d", time.Now().UnixNano())
	curlArgs := []string{
		"run", "-n", namespace,
		"--rm", "--restart=Never", "--attach",
		"--pod-running-timeout=30s",
		"--quiet",
		"--image=curlimages/curl:8.7.1",
		podName,
		"--command", "--", "curl",
		"-sS", "-o", "/dev/null",
		"-w", "%{http_code}",
		"--connect-timeout", "5",
		"--max-time", "20",
		"-H", "content-type: application/json",
		"-H", "accept: application/json, text/event-stream",
		"-H", "Mcp-Protocol-Version: 2025-06-18",
	}
	if host != "" {
		curlArgs = append(curlArgs, "-H", "Host: "+host)
	}
	curlArgs = append(curlArgs,
		"-d", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		fmt.Sprintf("http://traefik.%s.svc.cluster.local:%d%s", doctorTraefikNamespace, doctorTraefikWebPort, path),
	)
	cmd, err := kubectl.CommandArgs(curlArgs)
	if err != nil {
		return DoctorCheck{
			Name:   "ingress route probe",
			OK:     false,
			Detail: fmt.Sprintf("kubectl error: %v", err),
			Remedy: "check kubectl connectivity and helper pod image access",
		}
	}
	out, runErr := cmd.CombinedOutput()
	status := strings.TrimSpace(string(out))
	if runErr != nil {
		return DoctorCheck{
			Name:   "ingress route probe",
			OK:     false,
			Detail: fmt.Sprintf("probe failed: %s", status),
			Remedy: "inspect Traefik logs and ingress rules",
		}
	}
	if status == "" {
		return DoctorCheck{
			Name:   "ingress route probe",
			OK:     false,
			Detail: "probe returned empty HTTP status",
			Remedy: "inspect Traefik service and ingress path rules",
		}
	}
	if status == "404" {
		return DoctorCheck{
			Name:   "ingress route probe",
			OK:     false,
			Detail: fmt.Sprintf("ingress %s returned HTTP 404 for path %s", ingressName, path),
			Remedy: "confirm MCPServer ingress path/host matches the public route",
		}
	}
	return DoctorCheck{
		Name:   "ingress route probe",
		OK:     true,
		Detail: fmt.Sprintf("ingress %s returned HTTP %s for %s", ingressName, status, path),
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

func checkMCPServersImagePullSecrets(kubectl KubectlRunner, namespace string) DoctorCheck {
	cmd, err := kubectl.CommandArgs([]string{"get", "serviceaccount", "default", "-n", namespace, "-o", "jsonpath={range .imagePullSecrets[*]}{.name}{\"\\n\"}{end}"})
	if err != nil {
		return DoctorCheck{
			Name:   "mcp-servers imagePullSecrets",
			OK:     false,
			Detail: fmt.Sprintf("kubectl error: %v", err),
			Remedy: "check default serviceaccount in mcp-servers",
		}
	}
	out, execErr := cmd.Output()
	if execErr != nil {
		return DoctorCheck{
			Name:   "mcp-servers imagePullSecrets",
			OK:     false,
			Detail: "failed reading default serviceaccount imagePullSecrets",
			Remedy: "inspect `kubectl -n mcp-servers get sa default -o yaml`",
		}
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return DoctorCheck{
			Name:   "mcp-servers imagePullSecrets",
			OK:     true,
			Detail: "no imagePullSecrets configured on default serviceaccount",
		}
	}
	names := strings.Split(raw, "\n")
	for _, name := range names {
		n := strings.TrimSpace(name)
		if n == "" {
			continue
		}
		if _, getErr := readKubectlOutput(kubectl, []string{"get", "secret", n, "-n", namespace, "-o", "jsonpath={.metadata.name}"}); getErr != nil {
			return DoctorCheck{
				Name:   "mcp-servers imagePullSecrets",
				OK:     false,
				Detail: fmt.Sprintf("referenced imagePullSecret %s is missing", n),
				Remedy: fmt.Sprintf("create secret %s in namespace %s or update serviceaccount", n, namespace),
			}
		}
	}
	return DoctorCheck{
		Name:   "mcp-servers imagePullSecrets",
		OK:     true,
		Detail: fmt.Sprintf("%d imagePullSecrets present", len(names)),
	}
}

func checkMCPServersImagePullSmoke(kubectl KubectlRunner, namespace string) DoctorCheck {
	image, imageSource := resolveDoctorSmokeImage(kubectl, namespace)
	podName := fmt.Sprintf("doctor-pull-%d", time.Now().UnixNano())
	defer func() {
		_ = kubectl.Run([]string{"delete", "pod", podName, "-n", namespace, "--ignore-not-found"})
	}()
	if err := kubectl.Run([]string{"run", podName, "-n", namespace, "--restart=Never", "--image=" + image}); err != nil {
		return DoctorCheck{
			Name:   "mcp-servers image pull smoke",
			OK:     false,
			Detail: fmt.Sprintf("failed creating smoke pod: %v", err),
			Remedy: "check pull credentials, registry reachability, and image existence",
		}
	}
	waitCmd, cmdErr := kubectl.CommandArgs([]string{"wait", "--for=condition=Ready", "pod/" + podName, "-n", namespace, "--timeout=90s"})
	if cmdErr != nil {
		return DoctorCheck{
			Name:   "mcp-servers image pull smoke",
			OK:     false,
			Detail: fmt.Sprintf("kubectl error: %v", cmdErr),
			Remedy: "check kubectl setup",
		}
	}
	waitOut, waitErr := waitCmd.CombinedOutput()
	if waitErr != nil {
		reason, _ := readKubectlOutput(kubectl, []string{"get", "pod", podName, "-n", namespace, "-o", "jsonpath={.status.containerStatuses[0].state.waiting.reason}"})
		return DoctorCheck{
			Name:   "mcp-servers image pull smoke",
			OK:     false,
			Detail: fmt.Sprintf("pod failed to become ready (%s): %s", strings.TrimSpace(reason), strings.TrimSpace(string(waitOut))),
			Remedy: "inspect pod events: `kubectl -n mcp-servers describe pod " + podName + "`",
		}
	}
	return DoctorCheck{
		Name:   "mcp-servers image pull smoke",
		OK:     true,
		Detail: fmt.Sprintf("pull/ready succeeded using image %s (%s)", image, imageSource),
	}
}

func checkSentinelSecrets(kubectl KubectlRunner) DoctorCheck {
	if _, err := readKubectlOutput(kubectl, []string{"get", "namespace", doctorSentinelNamespace, "-o", "jsonpath={.metadata.name}"}); err != nil {
		return DoctorCheck{
			Name:   "sentinel secrets",
			OK:     true,
			Detail: "namespace mcp-sentinel not found; skipping sentinel secret checks",
		}
	}
	apiKeysB64, err := readKubectlOutput(kubectl, []string{"get", "secret", "mcp-sentinel-secrets", "-n", doctorSentinelNamespace, "-o", "jsonpath={.data.API_KEYS}"})
	if err != nil {
		return DoctorCheck{
			Name:   "sentinel secrets",
			OK:     false,
			Detail: "secret mcp-sentinel-secrets missing or API_KEYS key absent",
			Remedy: "create/update mcp-sentinel-secrets with API_KEYS and UI_API_KEY",
		}
	}
	uiKeyB64, err := readKubectlOutput(kubectl, []string{"get", "secret", "mcp-sentinel-secrets", "-n", doctorSentinelNamespace, "-o", "jsonpath={.data.UI_API_KEY}"})
	if err != nil {
		return DoctorCheck{
			Name:   "sentinel secrets",
			OK:     false,
			Detail: "secret mcp-sentinel-secrets missing UI_API_KEY key",
			Remedy: "create/update mcp-sentinel-secrets with UI_API_KEY",
		}
	}
	apiKeys := strings.TrimSpace(decodeBase64(strings.TrimSpace(apiKeysB64)))
	uiKey := strings.TrimSpace(decodeBase64(strings.TrimSpace(uiKeyB64)))
	if apiKeys == "" || uiKey == "" {
		return DoctorCheck{
			Name:   "sentinel secrets",
			OK:     false,
			Detail: "API_KEYS or UI_API_KEY is empty",
			Remedy: "populate non-empty API_KEYS and UI_API_KEY in mcp-sentinel-secrets",
		}
	}
	keys := splitCommaTrim(apiKeys)
	for _, k := range keys {
		if k == uiKey {
			return DoctorCheck{
				Name:   "sentinel secrets",
				OK:     true,
				Detail: "UI_API_KEY matches one API_KEYS entry",
			}
		}
	}
	return DoctorCheck{
		Name:   "sentinel secrets",
		OK:     false,
		Detail: "UI_API_KEY not present in API_KEYS",
		Remedy: "align API_KEYS and UI_API_KEY values in mcp-sentinel-secrets",
	}
}

func checkSentinelAPIAuthProbe(kubectl KubectlRunner) DoctorCheck {
	if _, err := readKubectlOutput(kubectl, []string{"get", "namespace", doctorSentinelNamespace, "-o", "jsonpath={.metadata.name}"}); err != nil {
		return DoctorCheck{
			Name:   "sentinel API auth probe",
			OK:     true,
			Detail: "namespace mcp-sentinel not found; skipping auth probe",
		}
	}
	apiKeyB64, err := readKubectlOutput(kubectl, []string{"get", "secret", "mcp-sentinel-secrets", "-n", doctorSentinelNamespace, "-o", "jsonpath={.data.UI_API_KEY}"})
	if err != nil {
		return DoctorCheck{
			Name:   "sentinel API auth probe",
			OK:     false,
			Detail: "UI_API_KEY not available in mcp-sentinel-secrets",
			Remedy: "configure UI_API_KEY before probing API auth",
		}
	}
	apiKey := strings.TrimSpace(decodeBase64(strings.TrimSpace(apiKeyB64)))
	if apiKey == "" {
		return DoctorCheck{
			Name:   "sentinel API auth probe",
			OK:     false,
			Detail: "UI_API_KEY decoded to empty value",
			Remedy: "set non-empty UI_API_KEY in mcp-sentinel-secrets",
		}
	}
	podName := fmt.Sprintf("doctor-sentinel-probe-%d", time.Now().UnixNano())
	args := []string{
		"run", "-n", doctorSentinelNamespace,
		"--rm", "--restart=Never", "--attach",
		"--pod-running-timeout=30s",
		"--quiet",
		"--image=curlimages/curl:8.7.1",
		podName,
		"--command", "--", "curl",
		"-sS", "-o", "/dev/null",
		"-w", "%{http_code}",
		"--connect-timeout", "5",
		"--max-time", "20",
		"-H", "x-api-key: " + apiKey,
		fmt.Sprintf("http://%s.%s.svc.cluster.local:8080/api/runtime/components", doctorSentinelAPIService, doctorSentinelNamespace),
	}
	cmd, cmdErr := kubectl.CommandArgs(args)
	if cmdErr != nil {
		return DoctorCheck{
			Name:   "sentinel API auth probe",
			OK:     false,
			Detail: fmt.Sprintf("kubectl error: %v", cmdErr),
			Remedy: "check kubectl connectivity and helper image access",
		}
	}
	out, runErr := cmd.CombinedOutput()
	status := strings.TrimSpace(string(out))
	if runErr != nil {
		return DoctorCheck{
			Name:   "sentinel API auth probe",
			OK:     false,
			Detail: fmt.Sprintf("probe failed: %s", status),
			Remedy: "verify sentinel API deployment/service and API key config",
		}
	}
	if status == "200" {
		return DoctorCheck{
			Name:   "sentinel API auth probe",
			OK:     true,
			Detail: "authenticated probe returned HTTP 200",
		}
	}
	return DoctorCheck{
		Name:   "sentinel API auth probe",
		OK:     false,
		Detail: fmt.Sprintf("authenticated probe returned HTTP %s", status),
		Remedy: "verify API key and sentinel API route availability",
	}
}

func checkNodeCapacity(kubectl KubectlRunner) DoctorCheck {
	cmd, err := kubectl.CommandArgs([]string{"top", "nodes", "--no-headers"})
	if err == nil {
		out, topErr := cmd.CombinedOutput()
		if topErr == nil {
			lines := filterNonEmptyLines(string(out))
			if len(lines) == 0 {
				return DoctorCheck{Name: "node capacity", OK: false, Detail: "no node metrics returned", Remedy: "check metrics-server installation"}
			}
			hot := make([]string, 0, len(lines))
			for _, line := range lines {
				fields := strings.Fields(line)
				if len(fields) < 5 {
					continue
				}
				cpuPct := strings.TrimSuffix(fields[2], "%")
				memPct := strings.TrimSuffix(fields[4], "%")
				cpu, _ := strconv.Atoi(cpuPct)
				mem, _ := strconv.Atoi(memPct)
				if cpu >= 95 || mem >= 95 {
					hot = append(hot, fmt.Sprintf("%s(cpu=%d%% mem=%d%%)", fields[0], cpu, mem))
				}
			}
			if len(hot) > 0 {
				return DoctorCheck{
					Name:   "node capacity",
					OK:     false,
					Detail: "high node utilization: " + strings.Join(hot, ", "),
					Remedy: "scale cluster capacity or reduce workload requests",
				}
			}
			return DoctorCheck{
				Name:   "node capacity",
				OK:     true,
				Detail: fmt.Sprintf("metrics available for %d node(s); utilization below 95%%", len(lines)),
			}
		}
	}

	alloc, allocErr := readKubectlOutput(kubectl, []string{"get", "nodes", "-o", "custom-columns=NAME:.metadata.name,ALLOC_CPU:.status.allocatable.cpu,ALLOC_MEM:.status.allocatable.memory", "--no-headers"})
	if allocErr != nil {
		return DoctorCheck{
			Name:   "node capacity",
			OK:     false,
			Detail: fmt.Sprintf("failed to read node allocatable resources: %v", allocErr),
			Remedy: "check cluster node readiness and kubectl permissions",
		}
	}
	lines := filterNonEmptyLines(alloc)
	if len(lines) == 0 {
		return DoctorCheck{
			Name:   "node capacity",
			OK:     false,
			Detail: "no nodes returned by API",
			Remedy: "check cluster connection",
		}
	}
	return DoctorCheck{
		Name:   "node capacity",
		OK:     true,
		Detail: fmt.Sprintf("allocatable resources visible on %d node(s) (metrics-server unavailable)", len(lines)),
	}
}

func checkPendingPodsByNamespace(kubectl KubectlRunner) DoctorCheck {
	out, err := readKubectlOutput(kubectl, []string{"get", "pods", "-A", "--field-selector=status.phase=Pending", "-o", "custom-columns=NS:.metadata.namespace,NAME:.metadata.name", "--no-headers"})
	if err != nil {
		return DoctorCheck{
			Name:   "pending pods",
			OK:     false,
			Detail: fmt.Sprintf("kubectl error: %v", err),
			Remedy: "check API connectivity and RBAC for listing pods",
		}
	}
	lines := filterNonEmptyLines(out)
	if len(lines) == 0 {
		return DoctorCheck{
			Name:   "pending pods",
			OK:     true,
			Detail: "no Pending pods across namespaces",
		}
	}
	counts := map[string]int{}
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 1 {
			continue
		}
		counts[fields[0]]++
	}
	summary := make([]string, 0, len(counts))
	for ns, count := range counts {
		summary = append(summary, fmt.Sprintf("%s=%d", ns, count))
	}
	return DoctorCheck{
		Name:   "pending pods",
		OK:     false,
		Detail: fmt.Sprintf("%d pending pods detected (%s)", len(lines), strings.Join(summary, ", ")),
		Remedy: "inspect pending pods/events: `kubectl get pods -A --field-selector=status.phase=Pending`",
	}
}

func checkMCPServerReconcileSmoke(kubectl KubectlRunner, namespace string) DoctorCheck {
	image := "registry.k8s.io/pause:3.9"
	imageSource := "fixed smoke image registry.k8s.io/pause:3.9"
	name := fmt.Sprintf("doctor-smoke-%d", time.Now().UnixNano()%1_000_000)
	manifest := fmt.Sprintf(`apiVersion: mcpruntime.org/v1alpha1
kind: MCPServer
metadata:
  name: %s
  namespace: %s
spec:
  image: %s
  port: 8088
  servicePort: 80
  publicPathPrefix: %s
  ingressClass: traefik
  ingressAnnotations:
    traefik.ingress.kubernetes.io/router.entrypoints: web
`, name, namespace, strings.TrimSpace(image), name)
	cleanup := func() {
		_ = kubectl.Run([]string{"delete", "mcpserver", name, "-n", namespace, "--ignore-not-found"})
		_ = kubectl.Run([]string{"delete", "deploy", name, "-n", namespace, "--ignore-not-found"})
		_ = kubectl.Run([]string{"delete", "svc", name, "-n", namespace, "--ignore-not-found"})
		_ = kubectl.Run([]string{"delete", "ingress", name, "-n", namespace, "--ignore-not-found"})
	}
	defer cleanup()

	applyCmd, cmdErr := kubectl.CommandArgs([]string{"apply", "-f", "-"})
	if cmdErr != nil {
		return DoctorCheck{
			Name:   "MCPServer reconcile smoke",
			OK:     false,
			Detail: fmt.Sprintf("kubectl error: %v", cmdErr),
			Remedy: "check kubeconfig access",
		}
	}
	applyCmd.SetStdin(strings.NewReader(manifest))
	if out, runErr := applyCmd.CombinedOutput(); runErr != nil {
		return DoctorCheck{
			Name:   "MCPServer reconcile smoke",
			OK:     false,
			Detail: fmt.Sprintf("failed to apply smoke MCPServer: %s", strings.TrimSpace(string(out))),
			Remedy: "check MCPServer webhook/CRD/operator availability",
		}
	}

	if err := kubectl.Run([]string{"rollout", "status", "deployment/" + name, "-n", namespace, "--timeout=150s"}); err != nil {
		return DoctorCheck{
			Name:   "MCPServer reconcile smoke",
			OK:     false,
			Detail: fmt.Sprintf("deployment did not become ready: %v", err),
			Remedy: "inspect operator reconcile and deployment events",
		}
	}
	if _, err := readKubectlOutput(kubectl, []string{"get", "svc", name, "-n", namespace, "-o", "jsonpath={.metadata.name}"}); err != nil {
		return DoctorCheck{
			Name:   "MCPServer reconcile smoke",
			OK:     false,
			Detail: "service not created for smoke MCPServer",
			Remedy: "inspect operator service reconciliation",
		}
	}
	if _, err := readKubectlOutput(kubectl, []string{"get", "ingress", name, "-n", namespace, "-o", "jsonpath={.metadata.name}"}); err != nil {
		return DoctorCheck{
			Name:   "MCPServer reconcile smoke",
			OK:     false,
			Detail: "ingress not created for smoke MCPServer",
			Remedy: "inspect operator ingress reconciliation",
		}
	}
	return DoctorCheck{
		Name:   "MCPServer reconcile smoke",
		OK:     true,
		Detail: fmt.Sprintf("temporary MCPServer %s reconciled (deployment/service/ingress) using %s", name, imageSource),
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

func readKubectlOutput(kubectl KubectlRunner, args []string) (string, error) {
	cmd, err := kubectl.CommandArgs(args)
	if err != nil {
		return "", err
	}
	out, execErr := cmd.Output()
	if execErr != nil {
		return "", execErr
	}
	return string(out), nil
}

func decodeBase64(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	decoded, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		return ""
	}
	return string(decoded)
}

func splitCommaTrim(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func filterNonEmptyLines(value string) []string {
	raw := strings.Split(value, "\n")
	out := make([]string, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func doctorNormalizePath(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "/"
	}
	if !strings.HasPrefix(trimmed, "/") {
		return "/" + trimmed
	}
	return trimmed
}

func resolveDoctorSmokeImage(kubectl KubectlRunner, preferredNamespace string) (string, string) {
	image, err := readKubectlOutput(kubectl, []string{"get", "deploy", "-n", preferredNamespace, "-o", "jsonpath={.items[0].spec.template.spec.containers[0].image}"})
	if err == nil && strings.TrimSpace(image) != "" {
		return strings.TrimSpace(image), fmt.Sprintf("deployment in %s", preferredNamespace)
	}
	all, allErr := readKubectlOutput(kubectl, []string{"get", "deploy", "-A", "-o", "jsonpath={range .items[*]}{.metadata.namespace}|{.metadata.name}|{.spec.template.spec.containers[0].image}{\"\\n\"}{end}"})
	if allErr != nil {
		return "registry.k8s.io/pause:3.9", "default fallback image registry.k8s.io/pause:3.9"
	}
	for _, line := range filterNonEmptyLines(all) {
		parts := strings.SplitN(line, "|", 3)
		if len(parts) != 3 {
			continue
		}
		img := strings.TrimSpace(parts[2])
		if img == "" {
			continue
		}
		return img, fmt.Sprintf("deployment %s/%s", strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
	}
	return "registry.k8s.io/pause:3.9", "default fallback image registry.k8s.io/pause:3.9"
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
