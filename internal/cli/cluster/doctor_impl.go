// Cluster doctor diagnostics: distribution detection, registry and Traefik
// checks, Sentinel probes, and remediation hints. See docs/cluster-readiness.md.
package cluster

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"mcp-runtime/internal/cli/core"
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

// DoctorCheckProgress is called before each doctor check starts. It returns an
// optional completion callback that receives the finished check result.
type DoctorCheckProgress func(DoctorCheckProgressEvent) func(DoctorCheck)

// DoctorCheckProgressEvent describes the check that is about to run.
type DoctorCheckProgressEvent struct {
	Name   string
	Detail string
	Index  int
	Total  int
}

type doctorCheckSpec struct {
	Name   string
	Detail string
	Run    func() DoctorCheck
}

const (
	doctorMCPServersNamespace = "mcp-servers"
	doctorTraefikNamespace    = "traefik"
	doctorK3sTraefikNamespace = "kube-system"
	doctorTraefikServiceName  = "traefik"
	doctorTraefikWebPort      = 8000
	doctorK3sTraefikWebPort   = 80
	doctorSentinelNamespace   = "mcp-sentinel"
	doctorSentinelAPIService  = "mcp-sentinel-api"
	doctorRestrictedRunAsUser = int64(65532)
	doctorProbePodRunTimeout  = "90s"

	registryHTTPPullMismatch = "http: server gave HTTP response to HTTPS client"

	// imagePullListSep separates list items emitted by the image-pull jsonpath.
	// ASCII Unit Separator (0x1f) avoids collisions with commas that appear
	// inside kubelet error messages.
	imagePullListSep = "\x1f"

	// imagePullDescribeLimit caps how many `kubectl describe pod` fallbacks
	// the HTTP-mismatch check issues per run. Most clusters that hit this
	// failure surface it via the waiting message first pass; the describe
	// pass is just a fallback for stale events.
	imagePullDescribeLimit = 8
)

type doctorTraefikEndpoint struct {
	Namespace string
	Name      string
	WebPort   int
	Source    string
}

type doctorServicePort struct {
	Name     string
	Port     int
	NodePort string
}

type doctorSmokeTarget struct {
	Image        string
	Port         int32
	Source       string
	WaitForReady bool
}

type doctorIngressRoute struct {
	Name string
	Host string
	Path string
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

// RunDoctor executes cluster diagnostics and returns a report.
func RunDoctor(kubectl core.KubectlRunner) DoctorReport {
	distro := DetectDistribution(kubectl)
	return runDoctorChecks(kubectl, distro, nil)
}

// RunDoctorWithProgress executes cluster diagnostics and calls progress hooks
// before and after each check. It is useful for UIs that need live feedback.
func RunDoctorWithProgress(kubectl core.KubectlRunner, progress DoctorCheckProgress) DoctorReport {
	distro := DetectDistribution(kubectl)
	return runDoctorChecks(kubectl, distro, progress)
}

// RunDoctorAndPrint streams doctor progress and results as checks execute.
func RunDoctorAndPrint(kubectl core.KubectlRunner) DoctorReport {
	core.Section("Cluster Doctor")
	core.Info("Detecting Kubernetes distribution — reading node kubelet versions, node names, and current context")
	distro := DetectDistribution(kubectl)
	core.Info(fmt.Sprintf("Distribution: %s", distro))

	report := runDoctorChecks(kubectl, distro, printDoctorCheckProgress)
	printDoctorReportFooter(report)
	return report
}

func runDoctorChecks(kubectl core.KubectlRunner, distro Distribution, progress DoctorCheckProgress) DoctorReport {
	specs := doctorCheckSpecs(kubectl, distro)
	checks := make([]DoctorCheck, 0, len(specs))
	for i, spec := range specs {
		finish := func(DoctorCheck) {}
		if progress != nil {
			event := DoctorCheckProgressEvent{
				Name:   spec.Name,
				Detail: spec.Detail,
				Index:  i + 1,
				Total:  len(specs),
			}
			if progressFinish := progress(event); progressFinish != nil {
				finish = progressFinish
			}
		}
		check := spec.Run()
		if check.Name == "" {
			check.Name = spec.Name
		}
		finish(check)
		checks = append(checks, check)
	}
	return DoctorReport{
		Distribution: distro,
		Checks:       checks,
	}
}

func doctorCheckSpecs(kubectl core.KubectlRunner, distro Distribution) []doctorCheckSpec {
	return []doctorCheckSpec{
		{
			Name:   fmt.Sprintf("namespace %s", doctorMCPServersNamespace),
			Detail: "reading namespace metadata from the Kubernetes API",
			Run:    func() DoctorCheck { return checkNamespaceExists(kubectl, doctorMCPServersNamespace) },
		},
		{
			Name:   fmt.Sprintf("namespace %s default serviceaccount", doctorMCPServersNamespace),
			Detail: "confirming pods in the runtime namespace have a default service account",
			Run:    func() DoctorCheck { return checkNamespaceDefaultServiceAccount(kubectl, doctorMCPServersNamespace) },
		},
		{
			Name:   fmt.Sprintf("namespace %s quota/limitrange", doctorMCPServersNamespace),
			Detail: "listing ResourceQuota and LimitRange objects that can block smoke pods",
			Run:    func() DoctorCheck { return checkNamespacePolicyGuardrails(kubectl, doctorMCPServersNamespace) },
		},
		{
			Name:   fmt.Sprintf("namespace %s pod admission", doctorMCPServersNamespace),
			Detail: "submitting a server-side dry-run pod to exercise admission webhooks and quota",
			Run:    func() DoctorCheck { return checkNamespacePodAdmission(kubectl, doctorMCPServersNamespace) },
		},
		{Name: "MCPServer CRD", Detail: "checking that the MCPServer API type is installed", Run: func() DoctorCheck { return checkMCPServerCRD(kubectl) }},
		{Name: "operator readiness", Detail: "reading ready and desired replicas for the operator deployment", Run: func() DoctorCheck { return checkOperatorReady(kubectl) }},
		{Name: "operator reconcile errors (last 10m)", Detail: "scanning recent operator logs for reconcile failure patterns", Run: func() DoctorCheck { return checkOperatorRecentReconcileErrors(kubectl) }},
		{Name: "operator ClusterRole rules", Detail: "verifying mcp-runtime-operator-role grants get/list/watch on the resources the informer cache needs", Run: func() DoctorCheck { return checkOperatorClusterRoleRules(kubectl) }},
		{Name: "traefik ingressClass", Detail: "checking that the traefik IngressClass exists", Run: func() DoctorCheck { return checkTraefikIngressClass(kubectl) }},
		{Name: "traefik deployment readiness", Detail: "reading ready and desired replicas for Traefik", Run: func() DoctorCheck { return checkTraefikDeploymentReady(kubectl, distro) }},
		{Name: "traefik web entrypoint", Detail: "checking the Traefik Service ports for the web entrypoint", Run: func() DoctorCheck { return checkTraefikWebEntrypoint(kubectl, distro) }},
		{Name: "traefik service exposure", Detail: "checking LoadBalancer or NodePort exposure for the web entrypoint", Run: func() DoctorCheck { return checkTraefikServiceExposure(kubectl, distro) }},
		{Name: "mcp-servers DNS/network", Detail: "launching a temporary curl pod in mcp-servers to reach the registry service", Run: func() DoctorCheck { return checkMCPServersDNSAndNetwork(kubectl) }},
		{
			Name:   "ingress route probe",
			Detail: "reading the first MCP ingress and launching a temporary curl pod against Traefik",
			Run:    func() DoctorCheck { return checkIngressRouteProbe(kubectl, doctorMCPServersNamespace, distro) },
		},
		{Name: "registry Service", Detail: "checking the bundled registry Service and NodePort", Run: func() DoctorCheck { return checkRegistryService(kubectl) }},
		{Name: "registry reachability (in-cluster)", Detail: "launching a temporary curl pod in registry to call /v2/ over cluster DNS", Run: func() DoctorCheck { return checkRegistryReachableFromCluster(kubectl) }},
		{
			Name:   "mcp-servers imagePullSecrets",
			Detail: "reading default service account pull secrets and verifying referenced Secret objects",
			Run:    func() DoctorCheck { return checkMCPServersImagePullSecrets(kubectl, doctorMCPServersNamespace) },
		},
		{
			Name:   "mcp-servers image pull smoke",
			Detail: "creating a temporary pod and waiting up to 90s for kubelet image pull readiness",
			Run:    func() DoctorCheck { return checkMCPServersImagePullSmoke(kubectl, doctorMCPServersNamespace) },
		},
		{Name: "registry HTTP pull mismatch", Detail: "listing pods and inspecting image-pull failures for HTTP-vs-HTTPS registry errors", Run: func() DoctorCheck { return checkRegistryHTTPPullMismatch(kubectl) }},
		{Name: "sentinel secrets", Detail: "reading Sentinel API, admin, UI, and ingest keys from mcp-sentinel-secrets", Run: func() DoctorCheck { return checkSentinelSecrets(kubectl) }},
		{Name: "sentinel API auth probe", Detail: "launching a temporary curl pod with UI_API_KEY against the Sentinel API", Run: func() DoctorCheck { return checkSentinelAPIAuthProbe(kubectl) }},
		{Name: "node capacity", Detail: "checking node metrics, then falling back to allocatable resources if metrics-server is absent", Run: func() DoctorCheck { return checkNodeCapacity(kubectl) }},
		{Name: "pending pods", Detail: "listing Pending pods across all namespaces", Run: func() DoctorCheck { return checkPendingPodsByNamespace(kubectl) }},
		{
			Name:   "MCPServer reconcile smoke",
			Detail: "applying a temporary MCPServer and waiting up to 150s for deployment/service/ingress resources",
			Run:    func() DoctorCheck { return checkMCPServerReconcileSmoke(kubectl, doctorMCPServersNamespace) },
		},
	}
}

// DetectDistribution inspects node info to guess which distribution is running.
// This is best-effort: callers should treat DistroGeneric as "probably kubeadm/unknown".
func DetectDistribution(kubectl core.KubectlRunner) Distribution {
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

func checkRegistryService(kubectl core.KubectlRunner) DoctorCheck {
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

func checkNamespaceExists(kubectl core.KubectlRunner, namespace string) DoctorCheck {
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

func checkNamespaceDefaultServiceAccount(kubectl core.KubectlRunner, namespace string) DoctorCheck {
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

func checkNamespacePolicyGuardrails(kubectl core.KubectlRunner, namespace string) DoctorCheck {
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

func checkNamespacePodAdmission(kubectl core.KubectlRunner, namespace string) DoctorCheck {
	podName := fmt.Sprintf("doctor-admission-%d", time.Now().UnixNano())
	manifest := fmt.Sprintf(`apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: %s
spec:
  automountServiceAccountToken: false
  restartPolicy: Never
  securityContext:
    runAsNonRoot: true
    seccompProfile:
      type: RuntimeDefault
  containers:
  - name: pause
    image: registry.k8s.io/pause:3.9
    securityContext:
      allowPrivilegeEscalation: false
      runAsNonRoot: true
      capabilities:
        drop:
        - ALL
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

func checkMCPServerCRD(kubectl core.KubectlRunner) DoctorCheck {
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

func checkOperatorReady(kubectl core.KubectlRunner) DoctorCheck {
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

func checkOperatorRecentReconcileErrors(kubectl core.KubectlRunner) DoctorCheck {
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
	patterns := []string{"reconciler error", "failed to reconcile", "error syncing"}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.ToLower(scanner.Text())
		if strings.Contains(line, "doctor-smoke-") {
			continue
		}
		for _, p := range patterns {
			if !strings.Contains(line, p) {
				continue
			}
			return DoctorCheck{
				Name:   "operator reconcile errors (last 10m)",
				OK:     false,
				Detail: fmt.Sprintf("detected %q in recent operator logs", p),
				Remedy: "inspect `kubectl logs -n mcp-runtime deploy/mcp-runtime-operator-controller-manager --since=10m`",
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return DoctorCheck{
			Name:   "operator reconcile errors (last 10m)",
			OK:     false,
			Detail: fmt.Sprintf("failed scanning operator logs: %v", err),
			Remedy: "inspect `kubectl logs -n mcp-runtime deploy/mcp-runtime-operator-controller-manager --since=10m`",
		}
	}
	return DoctorCheck{
		Name:   "operator reconcile errors (last 10m)",
		OK:     true,
		Detail: "no reconcile error patterns detected",
	}
}

type operatorClusterRoleResource struct {
	Resource string
	APIGroup string
}

// operatorClusterRoleResources is the minimum set of resource/API group pairs
// the deployed operator ClusterRole must allow (verbs at least get;list;watch)
// for the controller-runtime informer cache to sync. A drift here typically
// means the cluster was set up against an older config/rbac/role.yaml and never
// re-ran setup; the symptom is silent: MCPServer creates land in etcd but the
// operator never reconciles them.
var operatorClusterRoleResources = []operatorClusterRoleResource{
	{Resource: "serviceaccounts", APIGroup: ""},
	{Resource: "configmaps", APIGroup: ""},
	{Resource: "services", APIGroup: ""},
	{Resource: "deployments", APIGroup: "apps"},
	{Resource: "ingresses", APIGroup: "networking.k8s.io"},
}

func checkOperatorClusterRoleRules(kubectl core.KubectlRunner) DoctorCheck {
	const name = "operator ClusterRole rules"
	const remedy = "re-run `./bin/mcp-runtime setup` (or `kubectl apply -k config/rbac/`) to reapply config/rbac/role.yaml; the controller-runtime informer cache will not sync without these"

	cmd, err := kubectl.CommandArgs([]string{"get", "clusterrole", "mcp-runtime-operator-role", "-o", "json"})
	if err != nil {
		return DoctorCheck{Name: name, OK: false, Detail: fmt.Sprintf("kubectl error: %v", err), Remedy: remedy}
	}
	out, err := cmd.Output()
	if err != nil {
		return DoctorCheck{Name: name, OK: false, Detail: fmt.Sprintf("ClusterRole mcp-runtime-operator-role not readable: %v", err), Remedy: remedy}
	}

	var role struct {
		Rules []struct {
			APIGroups []string `json:"apiGroups"`
			Resources []string `json:"resources"`
			Verbs     []string `json:"verbs"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(out, &role); err != nil {
		return DoctorCheck{Name: name, OK: false, Detail: fmt.Sprintf("could not parse ClusterRole JSON: %v", err), Remedy: remedy}
	}

	required := []string{"get", "list", "watch"}

	var missing []string
	for _, want := range operatorClusterRoleResources {
		verbs := map[string]bool{}
		for _, rule := range role.Rules {
			if !operatorClusterRoleRuleIncludes(rule.APIGroups, want.APIGroup) ||
				!operatorClusterRoleRuleIncludes(rule.Resources, want.Resource) {
				continue
			}
			for _, verb := range rule.Verbs {
				verbs[verb] = true
			}
		}
		if !operatorClusterRoleHasRequiredVerbs(verbs, required) {
			missing = append(missing, operatorClusterRoleResourceLabel(want))
		}
	}

	if len(missing) > 0 {
		return DoctorCheck{
			Name:   name,
			OK:     false,
			Detail: fmt.Sprintf("ClusterRole mcp-runtime-operator-role missing get/list/watch on: %s", strings.Join(missing, ", ")),
			Remedy: remedy,
		}
	}
	return DoctorCheck{
		Name:   name,
		OK:     true,
		Detail: fmt.Sprintf("get/list/watch present for %d expected resources", len(operatorClusterRoleResources)),
	}
}

func operatorClusterRoleRuleIncludes(values []string, want string) bool {
	for _, got := range values {
		if got == want || got == "*" {
			return true
		}
	}
	return false
}

func operatorClusterRoleHasRequiredVerbs(verbs map[string]bool, required []string) bool {
	if verbs["*"] {
		return true
	}
	for _, verb := range required {
		if !verbs[verb] {
			return false
		}
	}
	return true
}

func operatorClusterRoleResourceLabel(resource operatorClusterRoleResource) string {
	if resource.APIGroup == "" {
		return resource.Resource
	}
	return resource.Resource + "." + resource.APIGroup
}

func checkTraefikIngressClass(kubectl core.KubectlRunner) DoctorCheck {
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

func doctorTraefikEndpoints(distro Distribution) []doctorTraefikEndpoint {
	if distro == DistroK3s {
		return []doctorTraefikEndpoint{
			{
				Namespace: doctorK3sTraefikNamespace,
				Name:      doctorTraefikServiceName,
				WebPort:   doctorK3sTraefikWebPort,
				Source:    "k3s bundled Traefik",
			},
			{
				Namespace: doctorTraefikNamespace,
				Name:      doctorTraefikServiceName,
				WebPort:   doctorTraefikWebPort,
				Source:    "repo-managed Traefik",
			},
		}
	}
	return []doctorTraefikEndpoint{
		{
			Namespace: doctorTraefikNamespace,
			Name:      doctorTraefikServiceName,
			WebPort:   doctorTraefikWebPort,
			Source:    "repo-managed Traefik",
		},
	}
}

func (e doctorTraefikEndpoint) label() string {
	return fmt.Sprintf("%s %s/%s", e.Source, e.Namespace, e.Name)
}

func traefikRemedy(distro Distribution) string {
	if distro == DistroK3s {
		return "k3s usually installs Traefik as `kube-system/traefik`; verify it is enabled with `kubectl -n kube-system get deploy,svc traefik`, or install the repo ingress overlay."
	}
	return "install Traefik deployment/service in namespace `traefik`, or run setup with the repo ingress overlay"
}

func checkTraefikDeploymentReady(kubectl core.KubectlRunner, distro Distribution) DoctorCheck {
	failures := make([]string, 0, len(doctorTraefikEndpoints(distro)))
	for _, endpoint := range doctorTraefikEndpoints(distro) {
		check := checkTraefikDeploymentReadyAt(kubectl, endpoint)
		if check.OK {
			return check
		}
		failures = append(failures, fmt.Sprintf("%s: %s", endpoint.label(), check.Detail))
	}
	return DoctorCheck{
		Name:   "traefik deployment readiness",
		OK:     false,
		Detail: strings.Join(failures, "; "),
		Remedy: traefikRemedy(distro),
	}
}

func checkTraefikDeploymentReadyAt(kubectl core.KubectlRunner, endpoint doctorTraefikEndpoint) DoctorCheck {
	cmd, err := kubectl.CommandArgs([]string{"get", "deploy", "-n", endpoint.Namespace, endpoint.Name, "-o", "jsonpath={.status.readyReplicas}/{.spec.replicas}"})
	if err != nil {
		return DoctorCheck{
			Name:   "traefik deployment readiness",
			OK:     false,
			Detail: fmt.Sprintf("kubectl error: %v", err),
		}
	}
	out, execErr := cmd.Output()
	pair := strings.TrimSpace(string(out))
	if execErr != nil || pair == "" {
		return DoctorCheck{
			Name:   "traefik deployment readiness",
			OK:     false,
			Detail: fmt.Sprintf("deployment %s/%s not found", endpoint.Namespace, endpoint.Name),
		}
	}
	parts := strings.SplitN(pair, "/", 2)
	if len(parts) != 2 {
		return DoctorCheck{
			Name:   "traefik deployment readiness",
			OK:     false,
			Detail: fmt.Sprintf("unexpected replica status %q", pair),
		}
	}
	ready, readyErr := strconv.Atoi(strings.TrimSpace(parts[0]))
	desired, desiredErr := strconv.Atoi(strings.TrimSpace(parts[1]))
	if readyErr != nil || desiredErr != nil || desired == 0 || ready < desired {
		return DoctorCheck{
			Name:   "traefik deployment readiness",
			OK:     false,
			Detail: fmt.Sprintf("%s replicas ready at %s/%s", pair, endpoint.Namespace, endpoint.Name),
		}
	}
	return DoctorCheck{
		Name:   "traefik deployment readiness",
		OK:     true,
		Detail: fmt.Sprintf("%s replicas ready at %s/%s (%s)", pair, endpoint.Namespace, endpoint.Name, endpoint.Source),
	}
}

func checkTraefikWebEntrypoint(kubectl core.KubectlRunner, distro Distribution) DoctorCheck {
	endpoint, ports, ok := resolveDoctorTraefikWebEndpoint(kubectl, distro)
	if ok {
		return DoctorCheck{
			Name:   "traefik web entrypoint",
			OK:     true,
			Detail: fmt.Sprintf("service %s/%s exposes web entrypoint on port %d (%s)", endpoint.Namespace, endpoint.Name, endpoint.WebPort, endpoint.Source),
		}
	}
	return DoctorCheck{
		Name:   "traefik web entrypoint",
		OK:     false,
		Detail: ports,
		Remedy: traefikRemedy(distro),
	}
}

func resolveDoctorTraefikWebEndpoint(kubectl core.KubectlRunner, distro Distribution) (doctorTraefikEndpoint, string, bool) {
	failures := make([]string, 0, len(doctorTraefikEndpoints(distro)))
	for _, endpoint := range doctorTraefikEndpoints(distro) {
		ports, err := readTraefikServicePorts(kubectl, endpoint)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", endpoint.label(), err))
			continue
		}
		webPort, ok := findTraefikWebPort(ports)
		if !ok {
			failures = append(failures, fmt.Sprintf("%s ports: %q", endpoint.label(), strings.TrimSpace(ports)))
			continue
		}
		endpoint.WebPort = webPort.Port
		return endpoint, ports, true
	}
	return doctorTraefikEndpoint{}, strings.Join(failures, "; "), false
}

func readTraefikServicePorts(kubectl core.KubectlRunner, endpoint doctorTraefikEndpoint) (string, error) {
	cmd, err := kubectl.CommandArgs([]string{"get", "svc", "-n", endpoint.Namespace, endpoint.Name, "-o", "jsonpath={range .spec.ports[*]}{.name}:{.port}:{.nodePort}{\"\\n\"}{end}"})
	if err != nil {
		return "", fmt.Errorf("kubectl error: %v", err)
	}
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("service %s/%s not found", endpoint.Namespace, endpoint.Name)
	}
	return strings.TrimSpace(string(out)), nil
}

func checkTraefikServiceExposure(kubectl core.KubectlRunner, distro Distribution) DoctorCheck {
	failures := make([]string, 0, len(doctorTraefikEndpoints(distro)))
	for _, endpoint := range doctorTraefikEndpoints(distro) {
		check := checkTraefikServiceExposureAt(kubectl, endpoint)
		if check.OK {
			return check
		}
		failures = append(failures, fmt.Sprintf("%s: %s", endpoint.label(), check.Detail))
	}
	return DoctorCheck{
		Name:   "traefik service exposure",
		OK:     false,
		Detail: strings.Join(failures, "; "),
		Remedy: "ensure the active Traefik service has an external LoadBalancer address or NodePort for the web entrypoint",
	}
}

func checkTraefikServiceExposureAt(kubectl core.KubectlRunner, endpoint doctorTraefikEndpoint) DoctorCheck {
	cmd, err := kubectl.CommandArgs([]string{"get", "svc", "-n", endpoint.Namespace, endpoint.Name, "-o", "jsonpath={.spec.type}|{.status.loadBalancer.ingress[0].ip}|{.status.loadBalancer.ingress[0].hostname}|{range .spec.ports[*]}{.name}:{.port}:{.nodePort}{\",\"}{end}"})
	if err != nil {
		return DoctorCheck{
			Name:   "traefik service exposure",
			OK:     false,
			Detail: fmt.Sprintf("kubectl error: %v", err),
		}
	}
	out, execErr := cmd.Output()
	if execErr != nil {
		return DoctorCheck{
			Name:   "traefik service exposure",
			OK:     false,
			Detail: fmt.Sprintf("failed reading service exposure fields for %s/%s", endpoint.Namespace, endpoint.Name),
		}
	}
	parts := strings.SplitN(strings.TrimSpace(string(out)), "|", 4)
	if len(parts) < 4 {
		return DoctorCheck{
			Name:   "traefik service exposure",
			OK:     false,
			Detail: fmt.Sprintf("unexpected service exposure payload %q", strings.TrimSpace(string(out))),
		}
	}
	svcType := strings.TrimSpace(parts[0])
	lbIP := strings.TrimSpace(parts[1])
	lbHost := strings.TrimSpace(parts[2])
	ports := strings.TrimSpace(parts[3])
	webPort, hasWebPort := findTraefikWebPort(ports)
	if !hasWebPort {
		return DoctorCheck{
			Name:   "traefik service exposure",
			OK:     false,
			Detail: fmt.Sprintf("service type=%s has no web entrypoint port (ports=%q)", svcType, ports),
		}
	}
	if svcType == "LoadBalancer" && (lbIP != "" || lbHost != "") {
		addr := lbIP
		if addr == "" {
			addr = lbHost
		}
		return DoctorCheck{
			Name:   "traefik service exposure",
			OK:     true,
			Detail: fmt.Sprintf("%s/%s LoadBalancer ready at %s (%s)", endpoint.Namespace, endpoint.Name, addr, endpoint.Source),
		}
	}
	if webPort.NodePort != "" && webPort.NodePort != "0" {
		return DoctorCheck{
			Name:   "traefik service exposure",
			OK:     true,
			Detail: fmt.Sprintf("%s/%s %s service exposes nodePort %s for web port %d (%s)", endpoint.Namespace, endpoint.Name, svcType, webPort.NodePort, webPort.Port, endpoint.Source),
		}
	}
	return DoctorCheck{
		Name:   "traefik service exposure",
		OK:     false,
		Detail: fmt.Sprintf("service type=%s exposure not ready (lbIP=%q lbHost=%q ports=%q)", svcType, lbIP, lbHost, ports),
	}
}

func checkMCPServersDNSAndNetwork(kubectl core.KubectlRunner) DoctorCheck {
	podName := fmt.Sprintf("mcp-runtime-doctor-dns-%d", time.Now().UnixNano())
	image := "curlimages/curl:8.7.1"
	curlArgs := []string{
		"-sSI", "--connect-timeout", "5", "--max-time", "15",
		"http://registry.registry.svc.cluster.local:5000/v2/",
	}
	defer func() {
		_ = kubectl.Run([]string{"delete", "pod", podName, "-n", doctorMCPServersNamespace, "--ignore-not-found"})
	}()
	args := []string{
		"run", podName,
		"-n", doctorMCPServersNamespace,
		"--restart=Never",
		"--image=" + image,
		"--overrides=" + restrictedRunOverrides(podName, image, "curl", curlArgs...),
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
	createOut, runErr := cmd.CombinedOutput()
	if runErr != nil {
		return DoctorCheck{
			Name:   "mcp-servers DNS/network",
			OK:     false,
			Detail: strings.TrimSpace(string(createOut)),
			Remedy: "check CoreDNS and network policies for namespace mcp-servers",
		}
	}
	if err := waitForDoctorPodSucceeded(kubectl, podName, doctorMCPServersNamespace, 90*time.Second); err != nil {
		logs, _ := readKubectlOutput(kubectl, []string{"logs", podName, "-n", doctorMCPServersNamespace, "--tail=50"})
		detail := fmt.Sprintf("helper pod did not succeed: %v", err)
		if strings.TrimSpace(logs) != "" {
			detail += ": " + strings.TrimSpace(logs)
		}
		return DoctorCheck{
			Name:   "mcp-servers DNS/network",
			OK:     false,
			Detail: detail,
			Remedy: "check CoreDNS and network policies for namespace mcp-servers",
		}
	}
	out, logsErr := readKubectlOutput(kubectl, []string{"logs", podName, "-n", doctorMCPServersNamespace})
	if logsErr != nil {
		return DoctorCheck{
			Name:   "mcp-servers DNS/network",
			OK:     false,
			Detail: fmt.Sprintf("failed reading helper pod logs: %v", logsErr),
			Remedy: "inspect pod events: `kubectl -n mcp-servers describe pod " + podName + "`",
		}
	}
	if !hasHTTP200Status(out) {
		return DoctorCheck{
			Name:   "mcp-servers DNS/network",
			OK:     false,
			Detail: fmt.Sprintf("unexpected response: %q", strings.TrimSpace(out)),
			Remedy: "check CoreDNS and service routing from namespace mcp-servers",
		}
	}
	return DoctorCheck{
		Name:   "mcp-servers DNS/network",
		OK:     true,
		Detail: "can resolve and reach registry service from mcp-servers namespace",
	}
}

func checkIngressRouteProbe(kubectl core.KubectlRunner, namespace string, distro Distribution) DoctorCheck {
	route, err := resolveIngressRouteProbeTarget(kubectl, namespace)
	if err != nil {
		return DoctorCheck{
			Name:   "ingress route probe",
			OK:     true,
			Detail: "no ingress resources found in mcp-servers; skipping live route probe",
		}
	}
	if route.Name == "" {
		return DoctorCheck{
			Name:   "ingress route probe",
			OK:     true,
			Detail: "no ingress resources found in mcp-servers; skipping live route probe",
		}
	}
	host := strings.TrimSpace(route.Host)
	path := doctorNormalizePath(strings.TrimSpace(route.Path))
	if path == "" {
		path = "/"
	}
	traefik, traefikDetail, ok := resolveDoctorTraefikWebEndpoint(kubectl, distro)
	if !ok {
		return DoctorCheck{
			Name:   "ingress route probe",
			OK:     false,
			Detail: fmt.Sprintf("failed resolving active Traefik service for route probe: %s", traefikDetail),
			Remedy: traefikRemedy(distro),
		}
	}
	podName := fmt.Sprintf("mcp-runtime-doctor-ingress-%d", time.Now().UnixNano())
	image := "curlimages/curl:8.7.1"
	probeArgs := []string{
		"-sS", "-o", "/tmp/doctor-response",
		"-w", "%{http_code}",
		"--connect-timeout", "5",
		"--max-time", "20",
		"-H", "content-type: application/json",
		"-H", "accept: application/json, text/event-stream",
		"-H", "Mcp-Protocol-Version: 2025-06-18",
	}
	if host != "" {
		probeArgs = append(probeArgs, "-H", "Host: "+host)
	}
	probeArgs = append(probeArgs,
		"-d", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		fmt.Sprintf("http://%s.%s.svc.cluster.local:%d%s", traefik.Name, traefik.Namespace, traefik.WebPort, path),
	)
	curlArgs := []string{
		"run", "-n", namespace,
		"--rm", "--restart=Never", "--attach",
		"--pod-running-timeout=" + doctorProbePodRunTimeout,
		"--quiet",
		"--image=" + image,
		"--overrides=" + restrictedRunOverrides(podName, image, "curl", probeArgs...),
		podName,
	}
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
			Detail: fmt.Sprintf("ingress %s returned HTTP 404 for path %s", route.Name, path),
			Remedy: "confirm MCPServer ingress path/host matches the public route",
		}
	}
	return DoctorCheck{
		Name:   "ingress route probe",
		OK:     true,
		Detail: fmt.Sprintf("ingress %s returned HTTP %s for %s via %s/%s", route.Name, status, path, traefik.Namespace, traefik.Name),
	}
}

func resolveIngressRouteProbeTarget(kubectl core.KubectlRunner, namespace string) (doctorIngressRoute, error) {
	out, err := readKubectlOutput(kubectl, []string{"get", "ingress", "-n", namespace, "-o", "jsonpath={range .items[*]}{.metadata.name}|{.spec.rules[0].host}|{.spec.rules[0].http.paths[0].path}{\"\\n\"}{end}"})
	if err != nil {
		return doctorIngressRoute{}, err
	}
	for _, line := range filterNonEmptyLines(out) {
		parts := strings.SplitN(line, "|", 3)
		if len(parts) == 0 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		if name == "" || strings.HasPrefix(name, "doctor-smoke-") {
			continue
		}
		route := doctorIngressRoute{Name: name}
		if len(parts) > 1 {
			route.Host = strings.TrimSpace(parts[1])
		}
		if len(parts) > 2 {
			route.Path = strings.TrimSpace(parts[2])
		}
		return route, nil
	}
	return doctorIngressRoute{}, nil
}

// checkRegistryReachableFromCluster verifies that an in-cluster pod can talk to
// the registry over the cluster-internal service DNS. This exercises the same
// path the in-cluster push helper uses, so a failure here means `registry push
// --mode=in-cluster` will also fail. Kubelet's pull path (node-side containerd
// with registries.yaml mirrors) is distribution-specific and surfaced via the
// remediation hint, not as a pass/fail check — we can't reach into kubelet
// non-destructively.
func checkRegistryReachableFromCluster(kubectl core.KubectlRunner) DoctorCheck {
	podName := fmt.Sprintf("mcp-runtime-doctor-curl-%d", time.Now().UnixNano())
	args := []string{
		"run", "-n", "registry",
		"--rm", "--restart=Never", "--attach",
		"--pod-running-timeout=" + doctorProbePodRunTimeout,
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

func checkMCPServersImagePullSecrets(kubectl core.KubectlRunner, namespace string) DoctorCheck {
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

func checkMCPServersImagePullSmoke(kubectl core.KubectlRunner, namespace string) DoctorCheck {
	image, imageSource := resolveDoctorSmokeImage(kubectl, namespace)
	podName := fmt.Sprintf("doctor-pull-%d", time.Now().UnixNano())
	defer func() {
		_ = kubectl.Run([]string{"delete", "pod", podName, "-n", namespace, "--ignore-not-found"})
	}()
	createCmd, cmdErr := kubectl.CommandArgs([]string{
		"run", podName,
		"-n", namespace,
		"--restart=Never",
		"--image=" + image,
		"--overrides=" + restrictedRunOverrides(podName, image, ""),
	})
	if cmdErr != nil {
		return DoctorCheck{
			Name:   "mcp-servers image pull smoke",
			OK:     false,
			Detail: fmt.Sprintf("kubectl error: %v", cmdErr),
			Remedy: "check kubectl setup",
		}
	}
	createOut, err := createCmd.CombinedOutput()
	if err != nil {
		return DoctorCheck{
			Name:   "mcp-servers image pull smoke",
			OK:     false,
			Detail: fmt.Sprintf("failed creating smoke pod: %v: %s", err, strings.TrimSpace(string(createOut))),
			Remedy: "check pull credentials, registry reachability, and image existence",
		}
	}
	if err := waitForDoctorPodImagePulled(kubectl, podName, namespace, 90*time.Second); err != nil {
		return DoctorCheck{
			Name:   "mcp-servers image pull smoke",
			OK:     false,
			Detail: fmt.Sprintf("pod image was not pulled: %v", err),
			Remedy: "inspect pod events: `kubectl -n mcp-servers describe pod " + podName + "`",
		}
	}
	return DoctorCheck{
		Name:   "mcp-servers image pull smoke",
		OK:     true,
		Detail: fmt.Sprintf("pull/ready succeeded using image %s (%s)", image, imageSource),
	}
}

func waitForDoctorPodImagePulled(kubectl core.KubectlRunner, name, namespace string, timeout time.Duration) error {
	timeoutTimer := time.NewTimer(timeout)
	defer timeoutTimer.Stop()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	var lastStatus string
	for {
		imageID, imageErr := readKubectlOutput(kubectl, []string{"get", "pod", name, "-n", namespace, "-o", "jsonpath={.status.containerStatuses[0].imageID}"})
		if imageErr == nil && strings.TrimSpace(imageID) != "" {
			return nil
		}

		reason, _ := readKubectlOutput(kubectl, []string{"get", "pod", name, "-n", namespace, "-o", "jsonpath={.status.containerStatuses[0].state.waiting.reason}"})
		reason = strings.TrimSpace(reason)
		if isImagePullWaitingReason(reason) {
			return fmt.Errorf("%s", reason)
		}

		phase, _ := readKubectlOutput(kubectl, []string{"get", "pod", name, "-n", namespace, "-o", "jsonpath={.status.phase}"})
		lastStatus = strings.TrimSpace(phase)
		if reason != "" {
			lastStatus = reason
		}
		if lastStatus == "" && imageErr != nil {
			lastStatus = imageErr.Error()
		}

		select {
		case <-timeoutTimer.C:
			if lastStatus == "" {
				lastStatus = "timed out"
			}
			return fmt.Errorf("%s", lastStatus)
		case <-ticker.C:
		}
	}
}

func waitForDoctorPodSucceeded(kubectl core.KubectlRunner, name, namespace string, timeout time.Duration) error {
	timeoutTimer := time.NewTimer(timeout)
	defer timeoutTimer.Stop()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	var lastStatus string
	for {
		phase, phaseErr := readKubectlOutput(kubectl, []string{"get", "pod", name, "-n", namespace, "-o", "jsonpath={.status.phase}"})
		phase = strings.TrimSpace(phase)
		switch phase {
		case "Succeeded":
			return nil
		case "Failed":
			return fmt.Errorf("pod phase Failed")
		}

		reason, _ := readKubectlOutput(kubectl, []string{"get", "pod", name, "-n", namespace, "-o", "jsonpath={.status.containerStatuses[0].state.waiting.reason}"})
		reason = strings.TrimSpace(reason)
		if isImagePullWaitingReason(reason) {
			return fmt.Errorf("%s", reason)
		}
		lastStatus = phase
		if reason != "" {
			lastStatus = reason
		}
		if lastStatus == "" && phaseErr != nil {
			lastStatus = phaseErr.Error()
		}

		select {
		case <-timeoutTimer.C:
			if lastStatus == "" {
				lastStatus = "timed out"
			}
			return fmt.Errorf("%s", lastStatus)
		case <-ticker.C:
		}
	}
}

func isImagePullWaitingReason(reason string) bool {
	switch reason {
	case "ErrImagePull", "ImagePullBackOff", "InvalidImageName":
		return true
	default:
		return false
	}
}

func restrictedRunOverrides(containerName, image, command string, args ...string) string {
	container := map[string]any{
		"name":       strings.TrimSpace(containerName),
		"image":      strings.TrimSpace(image),
		"workingDir": "/tmp",
		"securityContext": map[string]any{
			"allowPrivilegeEscalation": false,
			"runAsNonRoot":             true,
			"runAsUser":                doctorRestrictedRunAsUser,
			"capabilities": map[string]any{
				"drop": []string{"ALL"},
			},
		},
	}
	if strings.TrimSpace(command) != "" {
		container["command"] = []string{strings.TrimSpace(command)}
	}
	if len(args) > 0 {
		container["args"] = args
	}
	overrides := map[string]any{
		"spec": map[string]any{
			"automountServiceAccountToken": false,
			"securityContext": map[string]any{
				"runAsNonRoot": true,
				"runAsUser":    doctorRestrictedRunAsUser,
				"seccompProfile": map[string]any{
					"type": "RuntimeDefault",
				},
			},
			"containers": []map[string]any{container},
		},
	}
	data, err := json.Marshal(overrides)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func checkSentinelSecrets(kubectl core.KubectlRunner) DoctorCheck {
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
			Remedy: "create/update mcp-sentinel-secrets with API_KEYS, ADMIN_API_KEYS, INGEST_API_KEYS, and UI_API_KEY",
		}
	}
	adminAPIKeysB64, err := readKubectlOutput(kubectl, []string{"get", "secret", "mcp-sentinel-secrets", "-n", doctorSentinelNamespace, "-o", "jsonpath={.data.ADMIN_API_KEYS}"})
	if err != nil {
		return DoctorCheck{
			Name:   "sentinel secrets",
			OK:     false,
			Detail: "secret mcp-sentinel-secrets missing ADMIN_API_KEYS key",
			Remedy: "create/update mcp-sentinel-secrets with ADMIN_API_KEYS; include UI_API_KEY for browser admin access",
		}
	}
	ingestAPIKeysB64, err := readKubectlOutput(kubectl, []string{"get", "secret", "mcp-sentinel-secrets", "-n", doctorSentinelNamespace, "-o", "jsonpath={.data.INGEST_API_KEYS}"})
	if err != nil {
		return DoctorCheck{
			Name:   "sentinel secrets",
			OK:     false,
			Detail: "secret mcp-sentinel-secrets missing INGEST_API_KEYS key",
			Remedy: "create/update mcp-sentinel-secrets with ingest-only INGEST_API_KEYS for MCP proxy analytics",
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
	adminAPIKeys := strings.TrimSpace(decodeBase64(strings.TrimSpace(adminAPIKeysB64)))
	ingestAPIKeys := strings.TrimSpace(decodeBase64(strings.TrimSpace(ingestAPIKeysB64)))
	uiKey := strings.TrimSpace(decodeBase64(strings.TrimSpace(uiKeyB64)))
	if apiKeys == "" || adminAPIKeys == "" || ingestAPIKeys == "" || uiKey == "" {
		return DoctorCheck{
			Name:   "sentinel secrets",
			OK:     false,
			Detail: "API_KEYS, ADMIN_API_KEYS, INGEST_API_KEYS, or UI_API_KEY is empty",
			Remedy: "populate non-empty API_KEYS, ADMIN_API_KEYS, INGEST_API_KEYS, and UI_API_KEY in mcp-sentinel-secrets",
		}
	}
	keys := splitCommaTrim(apiKeys)
	adminKeys := splitCommaTrim(adminAPIKeys)
	uiInAPIKeys := false
	for _, k := range keys {
		if k == uiKey {
			uiInAPIKeys = true
			break
		}
	}
	if !uiInAPIKeys {
		return DoctorCheck{
			Name:   "sentinel secrets",
			OK:     false,
			Detail: "UI_API_KEY not present in API_KEYS",
			Remedy: "align API_KEYS and UI_API_KEY values in mcp-sentinel-secrets",
		}
	}
	for _, k := range adminKeys {
		if k == uiKey {
			return DoctorCheck{
				Name:   "sentinel secrets",
				OK:     true,
				Detail: "UI_API_KEY is present in API_KEYS and ADMIN_API_KEYS; INGEST_API_KEYS is populated separately",
			}
		}
	}
	return DoctorCheck{
		Name:   "sentinel secrets",
		OK:     false,
		Detail: "UI_API_KEY not present in ADMIN_API_KEYS",
		Remedy: "include UI_API_KEY in ADMIN_API_KEYS for browser admin access",
	}
}

func checkSentinelAPIAuthProbe(kubectl core.KubectlRunner) DoctorCheck {
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
	image := "curlimages/curl:8.7.1"
	curlArgs := []string{
		"-sS", "-o", "doctor-response",
		"-w", "%{http_code}",
		"--connect-timeout", "5",
		"--max-time", "20",
		"-H", "x-api-key: " + apiKey,
		fmt.Sprintf("http://%s.%s.svc.cluster.local:8080/api/runtime/components", doctorSentinelAPIService, doctorSentinelNamespace),
	}
	defer func() {
		_ = kubectl.Run([]string{"delete", "pod", podName, "-n", doctorSentinelNamespace, "--ignore-not-found"})
	}()
	cmd, cmdErr := kubectl.CommandArgs([]string{
		"run", podName,
		"-n", doctorSentinelNamespace,
		"--restart=Never",
		"--image=" + image,
		"--overrides=" + restrictedRunOverrides(podName, image, "curl", curlArgs...),
	})
	if cmdErr != nil {
		return DoctorCheck{
			Name:   "sentinel API auth probe",
			OK:     false,
			Detail: fmt.Sprintf("kubectl error: %v", cmdErr),
			Remedy: "check kubectl connectivity and helper image access",
		}
	}
	createOut, runErr := cmd.CombinedOutput()
	if runErr != nil {
		return DoctorCheck{
			Name:   "sentinel API auth probe",
			OK:     false,
			Detail: fmt.Sprintf("failed creating auth probe pod: %v: %s", runErr, strings.TrimSpace(string(createOut))),
			Remedy: "verify sentinel API deployment/service and API key config",
		}
	}
	if err := waitForDoctorPodSucceeded(kubectl, podName, doctorSentinelNamespace, 90*time.Second); err != nil {
		logs, _ := readKubectlOutput(kubectl, []string{"logs", podName, "-n", doctorSentinelNamespace, "--tail=50"})
		detail := fmt.Sprintf("auth probe pod did not complete: %v", err)
		if strings.TrimSpace(logs) != "" {
			detail += ": " + strings.TrimSpace(logs)
		}
		return DoctorCheck{
			Name:   "sentinel API auth probe",
			OK:     false,
			Detail: detail,
			Remedy: "verify sentinel API deployment/service and API key config",
		}
	}
	out, logsErr := readKubectlOutput(kubectl, []string{"logs", podName, "-n", doctorSentinelNamespace})
	if logsErr != nil {
		return DoctorCheck{
			Name:   "sentinel API auth probe",
			OK:     false,
			Detail: fmt.Sprintf("failed reading auth probe logs: %v", logsErr),
			Remedy: "inspect auth probe pod logs",
		}
	}
	status := strings.TrimSpace(out)
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

func checkNodeCapacity(kubectl core.KubectlRunner) DoctorCheck {
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

func checkPendingPodsByNamespace(kubectl core.KubectlRunner) DoctorCheck {
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

type imagePullPodCandidate struct {
	Namespace string
	Name      string
	Images    []string
	Reasons   []string
	Messages  []string
}

func checkRegistryHTTPPullMismatch(kubectl core.KubectlRunner) DoctorCheck {
	out, err := readKubectlOutput(kubectl, []string{"get", "pods", "-A", "-o", buildImagePullJSONPath()})
	if err != nil {
		return DoctorCheck{
			Name:   "registry HTTP pull mismatch",
			OK:     false,
			Detail: fmt.Sprintf("kubectl error: %v", err),
			Remedy: "check API connectivity and RBAC for listing pods",
		}
	}

	candidates := parseImagePullCandidates(out)
	if len(candidates) == 0 {
		return DoctorCheck{
			Name:   "registry HTTP pull mismatch",
			OK:     true,
			Detail: "no ErrImagePull/ImagePullBackOff pods detected",
		}
	}

	// First pass: check waiting messages already returned by `kubectl get`.
	// Cheap and avoids any describe calls when the kubelet event is fresh.
	for _, candidate := range candidates {
		if hasRegistryHTTPPullMismatchMessage(candidate.Messages) {
			return mismatchResult(candidate)
		}
	}

	// Second pass: fall back to `kubectl describe`, capped so a cluster
	// with many ImagePullBackOff pods doesn't shell out indefinitely.
	describeFailures := 0
	var firstDescribeFailure string
	inspected := 0
	for _, candidate := range candidates {
		if inspected >= imagePullDescribeLimit {
			break
		}
		inspected++
		describe, err := readKubectlOutput(kubectl, []string{"describe", "pod", candidate.Name, "-n", candidate.Namespace})
		if err != nil {
			describeFailures++
			if firstDescribeFailure == "" {
				firstDescribeFailure = fmt.Sprintf("%s/%s: %v", candidate.Namespace, candidate.Name, err)
			}
			continue
		}
		if strings.Contains(describe, registryHTTPPullMismatch) {
			return mismatchResult(candidate)
		}
	}
	if describeFailures > 0 {
		detail := fmt.Sprintf("pod inspection failed for %d/%d ErrImagePull/ImagePullBackOff candidate(s); first error: %s", describeFailures, inspected, firstDescribeFailure)
		if inspected < len(candidates) {
			detail += fmt.Sprintf(" (inspected first %d of %d)", inspected, len(candidates))
		}
		return DoctorCheck{
			Name:   "registry HTTP pull mismatch",
			OK:     false,
			Detail: detail,
			Remedy: "inspect pull-failing pods manually with `kubectl describe pod <name> -n <namespace>` and check kubectl RBAC/connectivity",
		}
	}

	detail := fmt.Sprintf("%d ErrImagePull/ImagePullBackOff pod(s) found, none with HTTP-vs-HTTPS registry mismatch events", len(candidates))
	if inspected < len(candidates) {
		detail = fmt.Sprintf("%d ErrImagePull/ImagePullBackOff pod(s) found; inspected first %d, none with HTTP-vs-HTTPS registry mismatch events", len(candidates), inspected)
	}
	return DoctorCheck{
		Name:   "registry HTTP pull mismatch",
		OK:     true,
		Detail: detail,
	}
}

// buildImagePullJSONPath returns the kubectl jsonpath used by the HTTP
// mismatch check. Inner list items are joined with imagePullListSep (0x1f)
// so commas in kubelet messages don't fragment the parse.
func buildImagePullJSONPath() string {
	return fmt.Sprintf(
		`jsonpath={range .items[*]}{.metadata.namespace}{"|"}{.metadata.name}{"|"}{range .spec.initContainers[*]}{.image}{"%[1]s"}{end}{range .spec.containers[*]}{.image}{"%[1]s"}{end}{"|"}{range .status.initContainerStatuses[*]}{.state.waiting.reason}{"%[1]s"}{end}{range .status.containerStatuses[*]}{.state.waiting.reason}{"%[1]s"}{end}{"|"}{range .status.initContainerStatuses[*]}{.state.waiting.message}{"%[1]s"}{end}{range .status.containerStatuses[*]}{.state.waiting.message}{"%[1]s"}{end}{"\n"}{end}`,
		imagePullListSep,
	)
}

func mismatchResult(c imagePullPodCandidate) DoctorCheck {
	image := firstNonEmpty(c.Images, "unknown")
	reason := pickImagePullReason(c.Reasons)
	var detail string
	if reason != "" {
		detail = fmt.Sprintf("pod %s/%s image %s (%s) failed pull: %s", c.Namespace, c.Name, image, reason, registryHTTPPullMismatch)
	} else {
		detail = fmt.Sprintf("pod %s/%s image %s failed pull: %s", c.Namespace, c.Name, image, registryHTTPPullMismatch)
	}
	return DoctorCheck{
		Name:   "registry HTTP pull mismatch",
		OK:     false,
		Detail: detail,
		Remedy: "Registry HTTP pull mismatch: kubelet tried HTTPS against the HTTP registry. Configure the node container runtime's insecure registry mirror for the exact image host, or use TLS.",
	}
}

func pickImagePullReason(reasons []string) string {
	for _, r := range reasons {
		switch trimmed := strings.TrimSpace(r); trimmed {
		case "Init:ErrImagePull", "Init:ImagePullBackOff", "ErrImagePull", "ImagePullBackOff":
			return trimmed
		}
	}
	return ""
}

func parseImagePullCandidates(value string) []imagePullPodCandidate {
	var candidates []imagePullPodCandidate
	for _, line := range filterNonEmptyLines(value) {
		parts := strings.SplitN(line, "|", 5)
		if len(parts) < 4 {
			continue
		}
		reasons := splitSepTrim(parts[3], imagePullListSep)
		messages := []string{}
		if len(parts) == 5 {
			messages = splitSepTrim(parts[4], imagePullListSep)
		}
		if !hasImagePullReason(reasons) && !hasRegistryHTTPPullMismatchMessage(messages) {
			continue
		}
		candidates = append(candidates, imagePullPodCandidate{
			Namespace: strings.TrimSpace(parts[0]),
			Name:      strings.TrimSpace(parts[1]),
			Images:    splitSepTrim(parts[2], imagePullListSep),
			Reasons:   reasons,
			Messages:  messages,
		})
	}
	return candidates
}

func hasImagePullReason(reasons []string) bool {
	for _, reason := range reasons {
		switch strings.TrimSpace(reason) {
		case "ErrImagePull", "ImagePullBackOff", "Init:ErrImagePull", "Init:ImagePullBackOff":
			return true
		}
	}
	return false
}

func hasRegistryHTTPPullMismatchMessage(messages []string) bool {
	for _, message := range messages {
		if strings.Contains(message, registryHTTPPullMismatch) {
			return true
		}
	}
	return false
}

func checkMCPServerReconcileSmoke(kubectl core.KubectlRunner, namespace string) DoctorCheck {
	target := resolveDoctorSmokeTarget(kubectl, namespace)
	name := fmt.Sprintf("doctor-smoke-%d", time.Now().UnixNano()%1_000_000)
	manifest := fmt.Sprintf(`apiVersion: mcpruntime.org/v1alpha1
kind: MCPServer
metadata:
  name: %s
  namespace: %s
spec:
  image: %s
  port: %d
  servicePort: 80
  publicPathPrefix: %s
  ingressClass: traefik
  ingressAnnotations:
    traefik.ingress.kubernetes.io/router.entrypoints: web
`, name, namespace, strings.TrimSpace(target.Image), target.Port, name)
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

	if err := waitForDoctorResource(kubectl, "deployment", name, namespace, 150*time.Second); err != nil {
		return DoctorCheck{
			Name:   "MCPServer reconcile smoke",
			OK:     false,
			Detail: fmt.Sprintf("deployment was not created: %v", err),
			Remedy: "inspect operator reconcile errors and MCPServer status",
		}
	}
	if target.WaitForReady {
		if err := waitForDoctorDeploymentReady(kubectl, name, namespace, 150*time.Second); err != nil {
			return DoctorCheck{
				Name:   "MCPServer reconcile smoke",
				OK:     false,
				Detail: fmt.Sprintf("deployment did not become ready: %v", err),
				Remedy: "inspect operator reconcile and smoke deployment events",
			}
		}
	}
	if !target.WaitForReady {
		if err := waitForDoctorPodsScheduled(kubectl, name, namespace, 30*time.Second); err != nil {
			return DoctorCheck{
				Name:   "MCPServer reconcile smoke",
				OK:     false,
				Detail: fmt.Sprintf("deployment pod was not scheduled: %v", err),
				Remedy: "inspect operator reconcile and smoke deployment events",
			}
		}
	}
	if err := waitForDoctorResource(kubectl, "svc", name, namespace, 150*time.Second); err != nil {
		return DoctorCheck{
			Name:   "MCPServer reconcile smoke",
			OK:     false,
			Detail: fmt.Sprintf("service not created for smoke MCPServer: %v", err),
			Remedy: "inspect operator service reconciliation",
		}
	}
	if err := waitForDoctorResource(kubectl, "ingress", name, namespace, 150*time.Second); err != nil {
		return DoctorCheck{
			Name:   "MCPServer reconcile smoke",
			OK:     false,
			Detail: fmt.Sprintf("ingress not created for smoke MCPServer: %v", err),
			Remedy: "inspect operator ingress reconciliation",
		}
	}
	if target.WaitForReady {
		return DoctorCheck{
			Name:   "MCPServer reconcile smoke",
			OK:     true,
			Detail: fmt.Sprintf("temporary MCPServer %s reconciled ready deployment/service/ingress using %s", name, target.Source),
		}
	}
	return DoctorCheck{
		Name:   "MCPServer reconcile smoke",
		OK:     true,
		Detail: fmt.Sprintf("temporary MCPServer %s reconciled deployment/service/ingress using %s; skipped readiness because the fallback image does not expose the MCP port", name, target.Source),
	}
}

func waitForDoctorResource(kubectl core.KubectlRunner, resource, name, namespace string, timeout time.Duration) error {
	timeoutTimer := time.NewTimer(timeout)
	defer timeoutTimer.Stop()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	var lastErr error
	for {
		if _, err := readKubectlOutput(kubectl, []string{"get", resource, name, "-n", namespace, "-o", "jsonpath={.metadata.name}"}); err == nil {
			return nil
		} else {
			lastErr = err
		}
		select {
		case <-timeoutTimer.C:
			if lastErr != nil {
				return lastErr
			}
			return fmt.Errorf("%s/%s not found before timeout", resource, name)
		case <-ticker.C:
		}
	}
}

func waitForDoctorDeploymentReady(kubectl core.KubectlRunner, name, namespace string, timeout time.Duration) error {
	cmd, err := kubectl.CommandArgs([]string{"rollout", "status", "deployment/" + name, "-n", namespace, "--timeout=" + timeout.String()})
	if err != nil {
		return err
	}
	out, runErr := cmd.CombinedOutput()
	if runErr == nil {
		return nil
	}
	detail := strings.TrimSpace(string(out))
	if detail == "" {
		return runErr
	}
	return fmt.Errorf("%w: %s", runErr, detail)
}

func waitForDoctorPodsScheduled(kubectl core.KubectlRunner, name, namespace string, timeout time.Duration) error {
	timeoutTimer := time.NewTimer(timeout)
	defer timeoutTimer.Stop()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		out, err := readKubectlOutput(kubectl, []string{"get", "pods", "-n", namespace, "-l", "app=" + name, "-o", "jsonpath={.items[0].spec.nodeName}"})
		if err == nil && strings.TrimSpace(out) != "" {
			return nil
		}
		select {
		case <-timeoutTimer.C:
			return fmt.Errorf("no scheduled pod found for deployment %s before timeout", name)
		case <-ticker.C:
		}
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

func readKubectlOutput(kubectl core.KubectlRunner, args []string) (string, error) {
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
	return splitSepTrim(value, ",")
}

func splitSepTrim(value, sep string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, sep)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func firstNonEmpty(values []string, fallback string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return fallback
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

func parseDoctorServicePorts(value string) []doctorServicePort {
	entries := strings.FieldsFunc(value, func(r rune) bool {
		return r == '\n' || r == ','
	})
	ports := make([]doctorServicePort, 0, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.Split(entry, ":")
		switch len(parts) {
		case 2:
			port, err := strconv.Atoi(strings.TrimSpace(parts[0]))
			if err != nil {
				continue
			}
			ports = append(ports, doctorServicePort{
				Port:     port,
				NodePort: strings.TrimSpace(parts[1]),
			})
		case 3:
			port, err := strconv.Atoi(strings.TrimSpace(parts[1]))
			if err != nil {
				continue
			}
			ports = append(ports, doctorServicePort{
				Name:     strings.TrimSpace(parts[0]),
				Port:     port,
				NodePort: strings.TrimSpace(parts[2]),
			})
		}
	}
	return ports
}

func findTraefikWebPort(value string) (doctorServicePort, bool) {
	ports := parseDoctorServicePorts(value)
	for _, port := range ports {
		if port.Name == "web" && port.Port > 0 {
			return port, true
		}
	}
	for _, port := range ports {
		if port.Port == doctorTraefikWebPort || port.Port == doctorK3sTraefikWebPort {
			return port, true
		}
	}
	return doctorServicePort{}, false
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

func resolveDoctorSmokeImage(kubectl core.KubectlRunner, preferredNamespace string) (string, string) {
	target := resolveDoctorSmokeTarget(kubectl, preferredNamespace)
	return target.Image, target.Source
}

func resolveDoctorSmokeTarget(kubectl core.KubectlRunner, preferredNamespace string) doctorSmokeTarget {
	mcpServerNames, haveMCPServerNames := readDoctorMCPServerNames(kubectl, preferredNamespace)
	out, err := readKubectlOutput(kubectl, []string{"get", "deploy", "-n", preferredNamespace, "-o", "jsonpath={range .items[*]}{.metadata.name}|{.status.readyReplicas}|{.spec.template.spec.containers[0].image}|{.spec.template.spec.containers[0].ports[0].containerPort}{\"\\n\"}{end}"})
	if err == nil {
		for _, line := range filterNonEmptyLines(out) {
			parts := strings.SplitN(line, "|", 4)
			if len(parts) < 3 {
				continue
			}
			name := strings.TrimSpace(parts[0])
			ready := strings.TrimSpace(parts[1])
			image := strings.TrimSpace(parts[2])
			if name == "" || strings.HasPrefix(name, "doctor-smoke-") || ready == "" || ready == "0" || image == "" {
				continue
			}
			if haveMCPServerNames && !mcpServerNames[name] {
				continue
			}
			port := int32(8088)
			if len(parts) == 4 {
				if parsed, parseErr := strconv.ParseInt(strings.TrimSpace(parts[3]), 10, 32); parseErr == nil && parsed > 0 && parsed <= 65535 {
					port = int32(parsed)
				}
			}
			return doctorSmokeTarget{
				Image:        image,
				Port:         port,
				Source:       fmt.Sprintf("ready deployment %s/%s", preferredNamespace, name),
				WaitForReady: true,
			}
		}
	}
	return doctorSmokeTarget{
		Image:        "registry.k8s.io/pause:3.9",
		Port:         8088,
		Source:       "fallback image registry.k8s.io/pause:3.9",
		WaitForReady: false,
	}
}

func readDoctorMCPServerNames(kubectl core.KubectlRunner, namespace string) (map[string]bool, bool) {
	out, err := readKubectlOutput(kubectl, []string{"get", "mcpservers", "-n", namespace, "-o", "jsonpath={range .items[*]}{.metadata.name}{\"\\n\"}{end}"})
	if err != nil {
		return nil, false
	}
	names := map[string]bool{}
	for _, line := range filterNonEmptyLines(out) {
		name := strings.TrimSpace(line)
		if name != "" {
			names[name] = true
		}
	}
	return names, true
}

// PrintDoctorReport emits a human-readable report using the standard printer.
func PrintDoctorReport(r DoctorReport) {
	core.Section("Cluster Doctor")
	core.Info(fmt.Sprintf("Distribution: %s", r.Distribution))
	for _, c := range r.Checks {
		printDoctorCheckResult(c)
	}
	printDoctorReportFooter(r)
}

func printDoctorCheckProgress(event DoctorCheckProgressEvent) func(DoctorCheck) {
	core.Info(doctorCheckProgressMessage(event))
	return func(c DoctorCheck) {
		printDoctorCheckResult(c)
	}
}

func doctorCheckProgressMessage(event DoctorCheckProgressEvent) string {
	prefix := "Checking"
	if event.Total > 0 {
		prefix = fmt.Sprintf("Checking %d/%d", event.Index, event.Total)
	}
	if event.Detail == "" {
		return fmt.Sprintf("%s %s", prefix, event.Name)
	}
	return fmt.Sprintf("%s %s — %s", prefix, event.Name, event.Detail)
}

func printDoctorCheckResult(c DoctorCheck) {
	if c.OK {
		core.Success(doctorCheckMessage(c))
		return
	}
	core.Error(doctorCheckMessage(c))
	if c.Remedy != "" {
		core.Info("  Remedy: " + c.Remedy)
	}
}

func doctorCheckMessage(c DoctorCheck) string {
	return fmt.Sprintf("%s — %s", c.Name, c.Detail)
}

func printDoctorReportFooter(r DoctorReport) {
	if !r.AllOK() {
		core.Info("")
		core.Info("Full remediation steps per distribution are in docs/cluster-readiness.md.")
		if reportHasRegistryOrPullFailure(r) {
			core.Info(remediationHint(r.Distribution))
		}
	}
}

func reportHasRegistryOrPullFailure(r DoctorReport) bool {
	for _, check := range r.Checks {
		if check.OK {
			continue
		}
		switch check.Name {
		case "registry Service",
			"registry reachability (in-cluster)",
			"mcp-servers imagePullSecrets",
			"mcp-servers image pull smoke",
			"registry HTTP pull mismatch":
			return true
		}
	}
	return false
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
