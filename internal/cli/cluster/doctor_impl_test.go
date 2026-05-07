package cluster

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"mcp-runtime/internal/cli/core"
)

func TestDetectDistribution(t *testing.T) {
	cases := []struct {
		name    string
		kubelet string // stdout for `get nodes -o jsonpath=.status.nodeInfo.kubeletVersion`
		names   string // stdout for `get nodes -o jsonpath=.metadata.name`
		context string // stdout for `config current-context`
		want    Distribution
	}{
		{
			name:    "k3s from kubelet version",
			kubelet: "v1.34.6+k3s1",
			want:    DistroK3s,
		},
		{
			name:  "kind from node name",
			names: "kind-control-plane",
			want:  DistroKind,
		},
		{
			name:  "does not treat generic control-plane names as kind",
			names: "prod-control-plane",
			want:  DistroGeneric,
		},
		{
			name:  "minikube from node name",
			names: "minikube",
			want:  DistroMinikube,
		},
		{
			name:  "docker-desktop from node name",
			names: "docker-desktop",
			want:  DistroDockerDesktop,
		},
		{
			name:    "minikube from context fallback",
			context: "minikube",
			want:    DistroMinikube,
		},
		{
			name: "unknown returns generic",
			want: DistroGeneric,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mock := &core.MockExecutor{
				CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
					switch {
					case contains(spec.Args, "jsonpath={.items[*].status.nodeInfo.kubeletVersion}"):
						return &core.MockCommand{OutputData: []byte(tc.kubelet)}
					case contains(spec.Args, "jsonpath={.items[*].metadata.name}"):
						return &core.MockCommand{OutputData: []byte(tc.names)}
					case contains(spec.Args, "current-context"):
						return &core.MockCommand{OutputData: []byte(tc.context)}
					}
					return &core.MockCommand{}
				},
			}
			kubectl := core.NewTestKubectlClient(mock)
			got := DetectDistribution(kubectl)
			if got != tc.want {
				t.Fatalf("DetectDistribution() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCheckRegistryService(t *testing.T) {
	t.Run("ok with nodeport", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				return &core.MockCommand{OutputData: []byte("32000")}
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		check := checkRegistryService(kubectl)
		if !check.OK {
			t.Fatalf("expected OK, got detail=%q", check.Detail)
		}
		if !strings.Contains(check.Detail, "32000") {
			t.Fatalf("detail should mention the NodePort, got %q", check.Detail)
		}
	})

	t.Run("fails when service missing", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				return &core.MockCommand{OutputErr: errors.New("not found")}
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		check := checkRegistryService(kubectl)
		if check.OK {
			t.Fatal("expected failure when service missing")
		}
		if check.Remedy == "" {
			t.Fatal("expected a remedy hint")
		}
	})
}

func TestCheckNamespaceExists(t *testing.T) {
	t.Run("ok when namespace exists", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				return &core.MockCommand{OutputData: []byte("mcp-servers")}
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		check := checkNamespaceExists(kubectl, "mcp-servers")
		if !check.OK {
			t.Fatalf("expected OK, got detail=%q", check.Detail)
		}
	})

	t.Run("fails when namespace missing", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				return &core.MockCommand{OutputErr: errors.New("not found")}
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		check := checkNamespaceExists(kubectl, "mcp-servers")
		if check.OK {
			t.Fatal("expected failure when namespace is missing")
		}
	})
}

func TestCheckMCPServerCRD(t *testing.T) {
	t.Run("ok when CRD exists", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				return &core.MockCommand{OutputData: []byte("mcpservers.mcpruntime.org")}
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		check := checkMCPServerCRD(kubectl)
		if !check.OK {
			t.Fatalf("expected OK, got detail=%q", check.Detail)
		}
	})

	t.Run("fails when CRD missing", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				return &core.MockCommand{OutputErr: errors.New("not found")}
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		check := checkMCPServerCRD(kubectl)
		if check.OK {
			t.Fatal("expected failure when CRD is missing")
		}
	})
}

func TestCheckOperatorReady(t *testing.T) {
	t.Run("ok when desired replicas are ready", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				return &core.MockCommand{OutputData: []byte("1/1")}
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		check := checkOperatorReady(kubectl)
		if !check.OK {
			t.Fatalf("expected OK, got detail=%q", check.Detail)
		}
	})

	t.Run("fails when not enough replicas are ready", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				return &core.MockCommand{OutputData: []byte("0/1")}
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		check := checkOperatorReady(kubectl)
		if check.OK {
			t.Fatal("expected failure for 0/1 ready replicas")
		}
	})
}

func TestCheckTraefikIngressClass(t *testing.T) {
	t.Run("ok when ingressClass exists", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				return &core.MockCommand{OutputData: []byte("traefik")}
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		check := checkTraefikIngressClass(kubectl)
		if !check.OK {
			t.Fatalf("expected OK, got detail=%q", check.Detail)
		}
	})

	t.Run("fails when ingressClass is missing", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				return &core.MockCommand{OutputErr: errors.New("not found")}
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		check := checkTraefikIngressClass(kubectl)
		if check.OK {
			t.Fatal("expected failure when ingressClass missing")
		}
	})
}

func TestCheckTraefikWebEntrypoint(t *testing.T) {
	t.Run("ok when service exposes named web entrypoint", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				return &core.MockCommand{OutputData: []byte("web:8000:32080\nwebsecure:8443:32443\n")}
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		check := checkTraefikWebEntrypoint(kubectl, DistroGeneric)
		if !check.OK {
			t.Fatalf("expected OK, got detail=%q", check.Detail)
		}
	})

	t.Run("ok with k3s bundled traefik service", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				switch {
				case contains(spec.Args, "kube-system"):
					return &core.MockCommand{OutputData: []byte("web:80:0\nwebsecure:443:0\n")}
				case contains(spec.Args, "traefik"):
					return &core.MockCommand{OutputErr: errors.New("not found")}
				}
				return &core.MockCommand{}
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		check := checkTraefikWebEntrypoint(kubectl, DistroK3s)
		if !check.OK {
			t.Fatalf("expected OK for k3s bundled Traefik, got detail=%q", check.Detail)
		}
		if !strings.Contains(check.Detail, "k3s bundled Traefik") {
			t.Fatalf("detail should mention k3s bundled Traefik, got %q", check.Detail)
		}
	})

	t.Run("fails when service does not expose web entrypoint", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				return &core.MockCommand{OutputData: []byte("admin:9000:32090\n")}
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		check := checkTraefikWebEntrypoint(kubectl, DistroGeneric)
		if check.OK {
			t.Fatal("expected failure when web entrypoint is not exposed")
		}
	})
}

func TestCheckRegistryReachableFromCluster(t *testing.T) {
	t.Run("ok on HTTP 200", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				return &core.MockCommand{OutputData: []byte("HTTP/1.1 200 OK\nDocker-Distribution-Api-Version: registry/2.0\n")}
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		check := checkRegistryReachableFromCluster(kubectl)
		if !check.OK {
			t.Fatalf("expected OK, got detail=%q", check.Detail)
		}
	})

	t.Run("fails on non-200", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				return &core.MockCommand{OutputData: []byte("HTTP/1.1 503 Service Unavailable\n")}
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		check := checkRegistryReachableFromCluster(kubectl)
		if check.OK {
			t.Fatal("expected failure for non-200")
		}
	})

	t.Run("does not false-pass when body includes non-status 200", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				return &core.MockCommand{OutputData: []byte("diagnostic: 200 retries\nHTTP/1.1 503 Service Unavailable\n")}
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		check := checkRegistryReachableFromCluster(kubectl)
		if check.OK {
			t.Fatal("expected failure when HTTP status line is not 200")
		}
	})

	t.Run("fails when helper pod errors", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				return &core.MockCommand{OutputErr: errors.New("pod failed"), RunErr: errors.New("pod failed")}
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		check := checkRegistryReachableFromCluster(kubectl)
		if check.OK {
			t.Fatal("expected failure when helper pod errors")
		}
	})
}

func TestParseImagePullCandidates(t *testing.T) {
	sep := imagePullListSep
	out := strings.Join([]string{
		"mcp-sentinel|mcp-sentinel-api-abc|10.96.64.95:5000/mcp-sentinel-api:latest" + sep + "|ImagePullBackOff" + sep + "|",
		"mcp-runtime|operator-abc|registry.registry.svc.cluster.local:5000/mcp-runtime-operator:latest" + sep + "|Running" + sep + "|",
		"registry|registry-abc|registry.local/distribution:latest" + sep + "|ErrImagePull" + sep + "|",
		"mcp-servers|demo-init-abc|registry.local/bootstrap:latest" + sep + "registry.local/demo:latest" + sep + "|ImagePullBackOff" + sep + "|",
	}, "\n")

	candidates := parseImagePullCandidates(out)
	if len(candidates) != 3 {
		t.Fatalf("expected 3 image pull candidates, got %d", len(candidates))
	}
	if candidates[0].Namespace != "mcp-sentinel" || candidates[0].Name != "mcp-sentinel-api-abc" {
		t.Fatalf("unexpected first candidate: %#v", candidates[0])
	}
	if candidates[0].Images[0] != "10.96.64.95:5000/mcp-sentinel-api:latest" {
		t.Fatalf("expected ClusterIP registry image, got %q", candidates[0].Images[0])
	}
	if candidates[1].Images[0] != "registry.local/distribution:latest" {
		t.Fatalf("expected host registry image, got %q", candidates[1].Images[0])
	}
	if candidates[2].Images[0] != "registry.local/bootstrap:latest" || candidates[2].Reasons[0] != "ImagePullBackOff" {
		t.Fatalf("expected init-container pull candidate, got %#v", candidates[2])
	}
}

func TestParseImagePullCandidatesPreservesCommasInMessages(t *testing.T) {
	// Real kubelet messages contain commas; the unit-separator delimiter
	// must keep them intact rather than fragmenting the message.
	sep := imagePullListSep
	msg := `failed to pull image "registry.local/demo:latest": rpc error: code = Unknown, desc = failed to do request: ` + registryHTTPPullMismatch
	line := "mcp-servers|demo-abc|registry.local/demo:latest" + sep + "|ImagePullBackOff" + sep + "|" + msg + sep
	candidates := parseImagePullCandidates(line)
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	if len(candidates[0].Messages) != 1 || candidates[0].Messages[0] != msg {
		t.Fatalf("expected message preserved verbatim, got %#v", candidates[0].Messages)
	}
	if !hasRegistryHTTPPullMismatchMessage(candidates[0].Messages) {
		t.Fatal("expected mismatch substring detected in unsplit message")
	}
}

func TestCheckRegistryHTTPPullMismatch(t *testing.T) {
	sep := imagePullListSep

	t.Run("reports HTTP registry mismatch from describe pod events", func(t *testing.T) {
		pods := "mcp-sentinel|mcp-sentinel-api-abc|10.96.64.95:5000/mcp-sentinel-api:latest" + sep + "|ImagePullBackOff" + sep + "|\n"
		describe := `Name:             mcp-sentinel-api-abc
Namespace:        mcp-sentinel
Events:
  Type     Reason     Age   From               Message
  ----     ------     ----  ----               -------
  Warning  Failed     31s   kubelet            Failed to pull image "10.96.64.95:5000/mcp-sentinel-api:latest": failed to resolve reference "10.96.64.95:5000/mcp-sentinel-api:latest": failed to do request: Head "https://10.96.64.95:5000/v2/mcp-sentinel-api/manifests/latest": http: server gave HTTP response to HTTPS client
`
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				switch {
				case contains(spec.Args, "pods") && contains(spec.Args, "-A"):
					return &core.MockCommand{OutputData: []byte(pods)}
				case contains(spec.Args, "describe"):
					return &core.MockCommand{OutputData: []byte(describe)}
				default:
					return &core.MockCommand{}
				}
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		check := checkRegistryHTTPPullMismatch(kubectl)
		if check.OK {
			t.Fatal("expected registry HTTP mismatch to fail")
		}
		for _, want := range []string{"mcp-sentinel/mcp-sentinel-api-abc", "10.96.64.95:5000/mcp-sentinel-api:latest", "(ImagePullBackOff)", registryHTTPPullMismatch} {
			if !strings.Contains(check.Detail, want) {
				t.Fatalf("detail should contain %q, got %q", want, check.Detail)
			}
		}
		for _, want := range []string{"insecure registry", "container runtime", "exact image host"} {
			if !strings.Contains(check.Remedy, want) {
				t.Fatalf("remedy should contain %q, got %q", want, check.Remedy)
			}
		}
	})

	t.Run("reports HTTP registry mismatch from init container pull events", func(t *testing.T) {
		pods := "mcp-servers|demo-init-abc|registry.local/bootstrap:latest" + sep + "registry.local/demo:latest" + sep + "|Init:ImagePullBackOff" + sep + "|\n"
		describe := `Name:             demo-init-abc
Namespace:        mcp-servers
Init Containers:
  bootstrap:
    Image:          registry.local/bootstrap:latest
Events:
  Type     Reason     Age   From               Message
  ----     ------     ----  ----               -------
  Warning  Failed     31s   kubelet            Failed to pull image "registry.local/bootstrap:latest": failed to do request: Head "https://registry.local/v2/bootstrap/manifests/latest": http: server gave HTTP response to HTTPS client
`
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				switch {
				case contains(spec.Args, "pods") && contains(spec.Args, "-A"):
					return &core.MockCommand{OutputData: []byte(pods)}
				case contains(spec.Args, "describe"):
					return &core.MockCommand{OutputData: []byte(describe)}
				default:
					return &core.MockCommand{}
				}
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		check := checkRegistryHTTPPullMismatch(kubectl)
		if check.OK {
			t.Fatal("expected registry HTTP mismatch for init container to fail")
		}
		for _, want := range []string{"mcp-servers/demo-init-abc", "registry.local/bootstrap:latest", "(Init:ImagePullBackOff)", registryHTTPPullMismatch} {
			if !strings.Contains(check.Detail, want) {
				t.Fatalf("detail should contain %q, got %q", want, check.Detail)
			}
		}
	})

	t.Run("reports HTTP registry mismatch from waiting status message", func(t *testing.T) {
		// Reasons field is empty here; only the waiting message identifies
		// the candidate. Detail should still surface the mismatch but won't
		// include a reason in parentheses.
		msg := `failed to pull image "registry.local/bootstrap:latest": failed to do request: Head "https://registry.local/v2/bootstrap/manifests/latest": http: server gave HTTP response to HTTPS client`
		pods := "mcp-servers|demo-status-abc|registry.local/bootstrap:latest" + sep + "registry.local/demo:latest" + sep + "||" + msg + sep
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				if contains(spec.Args, "describe") {
					t.Fatalf("did not expect describe call when waiting status message has mismatch")
				}
				return &core.MockCommand{OutputData: []byte(pods)}
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		check := checkRegistryHTTPPullMismatch(kubectl)
		if check.OK {
			t.Fatal("expected registry HTTP mismatch from status message to fail")
		}
		for _, want := range []string{"mcp-servers/demo-status-abc", "registry.local/bootstrap:latest", registryHTTPPullMismatch} {
			if !strings.Contains(check.Detail, want) {
				t.Fatalf("detail should contain %q, got %q", want, check.Detail)
			}
		}
		if strings.Contains(check.Detail, "()") {
			t.Fatalf("detail should omit empty reason parentheses, got %q", check.Detail)
		}
	})

	t.Run("passes when pull failures do not include HTTP mismatch event", func(t *testing.T) {
		pods := "mcp-servers|demo-abc|registry.local/demo:latest" + sep + "|ErrImagePull" + sep + "|\n"
		describe := `Events:
  Warning  Failed  kubelet  Failed to pull image "registry.local/demo:latest": not found
`
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				if contains(spec.Args, "describe") {
					return &core.MockCommand{OutputData: []byte(describe)}
				}
				return &core.MockCommand{OutputData: []byte(pods)}
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		check := checkRegistryHTTPPullMismatch(kubectl)
		if !check.OK {
			t.Fatalf("expected OK when no HTTP mismatch event exists, got detail=%q", check.Detail)
		}
	})

	t.Run("fails when pod inspection fails for pull candidates", func(t *testing.T) {
		pods := strings.Join([]string{
			"mcp-servers|demo-a|registry.local/demo-a:latest" + sep + "|ErrImagePull" + sep + "|",
			"mcp-servers|demo-b|registry.local/demo-b:latest" + sep + "|ImagePullBackOff" + sep + "|",
		}, "\n")
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				if contains(spec.Args, "describe") {
					return &core.MockCommand{OutputErr: errors.New("pods is forbidden")}
				}
				return &core.MockCommand{OutputData: []byte(pods)}
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		check := checkRegistryHTTPPullMismatch(kubectl)
		if check.OK {
			t.Fatal("expected registry HTTP mismatch check to fail when pod inspection fails")
		}
		for _, want := range []string{"pod inspection failed", "2/2", "mcp-servers/demo-a", "pods is forbidden"} {
			if !strings.Contains(check.Detail, want) {
				t.Fatalf("detail should contain %q, got %q", want, check.Detail)
			}
		}
		if !strings.Contains(check.Remedy, "kubectl describe pod") {
			t.Fatalf("remedy should mention manual describe, got %q", check.Remedy)
		}
	})

	t.Run("caps describe fallback when many ImagePullBackOff pods exist", func(t *testing.T) {
		// Build imagePullDescribeLimit + 5 candidates whose waiting messages
		// don't match. Only the first imagePullDescribeLimit should trigger
		// describe calls.
		var lines []string
		for i := range imagePullDescribeLimit + 5 {
			lines = append(lines, fmt.Sprintf("ns%d|pod%d|registry.local/x:latest"+sep+"|ImagePullBackOff"+sep+"|", i, i))
		}
		pods := strings.Join(lines, "\n")
		describeCalls := 0
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				if contains(spec.Args, "describe") {
					describeCalls++
					return &core.MockCommand{OutputData: []byte("Events:\n  Warning Failed kubelet Failed to pull image: not found\n")}
				}
				return &core.MockCommand{OutputData: []byte(pods)}
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		check := checkRegistryHTTPPullMismatch(kubectl)
		if !check.OK {
			t.Fatalf("expected OK when no candidates have HTTP mismatch, got detail=%q", check.Detail)
		}
		if describeCalls != imagePullDescribeLimit {
			t.Fatalf("expected describe calls capped at %d, got %d", imagePullDescribeLimit, describeCalls)
		}
		if !strings.Contains(check.Detail, fmt.Sprintf("inspected first %d", imagePullDescribeLimit)) {
			t.Fatalf("detail should mention capped inspection, got %q", check.Detail)
		}
	})
}

func TestRunDoctorAggregates(t *testing.T) {
	mock := &core.MockExecutor{
		CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
			switch {
			case contains(spec.Args, "jsonpath={.items[*].status.nodeInfo.kubeletVersion}"):
				return &core.MockCommand{OutputData: []byte("v1.34.6+k3s1")}
			case contains(spec.Args, "namespace mcp-servers"):
				return &core.MockCommand{OutputData: []byte("mcp-servers")}
			case contains(spec.Args, "crd mcpservers.mcpruntime.org"):
				return &core.MockCommand{OutputData: []byte("mcpservers.mcpruntime.org")}
			case contains(spec.Args, "mcp-runtime-operator-controller-manager"):
				return &core.MockCommand{OutputData: []byte("1/1")}
			case contains(spec.Args, "ingressclass traefik"):
				return &core.MockCommand{OutputData: []byte("traefik")}
			case contains(spec.Args, "svc -n traefik traefik"):
				return &core.MockCommand{OutputData: []byte("web:8000:32080\n")}
			case contains(spec.Args, "jsonpath={.spec.ports[0].nodePort}"):
				return &core.MockCommand{OutputData: []byte("32000")}
			case contains(spec.Args, "get") && contains(spec.Args, "pod") && argContains(spec.Args, "imageID"):
				return &core.MockCommand{OutputData: []byte("docker-pullable://registry.k8s.io/pause@sha256:test")}
			case contains(spec.Args, "get") && contains(spec.Args, "pods") && argContains(spec.Args, "spec.nodeName"):
				return &core.MockCommand{OutputData: []byte("node-a")}
			case contains(spec.Args, "curl"):
				return &core.MockCommand{OutputData: []byte("HTTP/1.1 503 Service Unavailable\n")}
			}
			return &core.MockCommand{}
		},
	}
	kubectl := core.NewTestKubectlClient(mock)
	report := RunDoctor(kubectl)
	if report.Distribution != DistroK3s {
		t.Fatalf("expected DistroK3s, got %q", report.Distribution)
	}
	if report.AllOK() {
		t.Fatal("expected at least one failing check (registry reachability 503)")
	}
	if len(report.Checks) < 7 {
		t.Fatalf("expected multiple checks, got %d", len(report.Checks))
	}
}

func TestRunDoctorWithProgressReportsEachCheck(t *testing.T) {
	mock := &core.MockExecutor{
		CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
			switch {
			case contains(spec.Args, "jsonpath={.items[*].status.nodeInfo.kubeletVersion}"):
				return &core.MockCommand{OutputData: []byte("v1.34.6+k3s1")}
			case contains(spec.Args, "namespace mcp-servers"):
				return &core.MockCommand{OutputData: []byte("mcp-servers")}
			case contains(spec.Args, "crd mcpservers.mcpruntime.org"):
				return &core.MockCommand{OutputData: []byte("mcpservers.mcpruntime.org")}
			case contains(spec.Args, "mcp-runtime-operator-controller-manager"):
				return &core.MockCommand{OutputData: []byte("1/1")}
			case contains(spec.Args, "ingressclass traefik"):
				return &core.MockCommand{OutputData: []byte("traefik")}
			case contains(spec.Args, "svc -n traefik traefik"):
				return &core.MockCommand{OutputData: []byte("web:8000:32080\n")}
			case contains(spec.Args, "jsonpath={.spec.ports[0].nodePort}"):
				return &core.MockCommand{OutputData: []byte("32000")}
			case contains(spec.Args, "get") && contains(spec.Args, "pod") && argContains(spec.Args, "imageID"):
				return &core.MockCommand{OutputData: []byte("docker-pullable://registry.k8s.io/pause@sha256:test")}
			case contains(spec.Args, "get") && contains(spec.Args, "pods") && argContains(spec.Args, "spec.nodeName"):
				return &core.MockCommand{OutputData: []byte("node-a")}
			case contains(spec.Args, "curl"):
				return &core.MockCommand{OutputData: []byte("HTTP/1.1 503 Service Unavailable\n")}
			}
			return &core.MockCommand{}
		},
	}
	kubectl := core.NewTestKubectlClient(mock)
	var events []string
	report := RunDoctorWithProgress(kubectl, func(event DoctorCheckProgressEvent) func(DoctorCheck) {
		if event.Index <= 0 || event.Total <= 0 {
			t.Fatalf("progress event has invalid position: %+v", event)
		}
		if event.Detail == "" {
			t.Fatalf("progress event for %q should describe what the check is doing", event.Name)
		}
		events = append(events, "start:"+event.Name)
		return func(check DoctorCheck) {
			events = append(events, "finish:"+check.Name)
		}
	})

	if len(report.Checks) == 0 {
		t.Fatal("expected checks")
	}
	if len(events) != len(report.Checks)*2 {
		t.Fatalf("got %d progress events for %d checks", len(events), len(report.Checks))
	}
	for i, check := range report.Checks {
		start := events[i*2]
		finish := events[i*2+1]
		if !strings.HasPrefix(start, "start:") {
			t.Fatalf("event %d = %q, want start event", i*2, start)
		}
		if finish != "finish:"+check.Name {
			t.Fatalf("finish event for check %d = %q, want %q", i, finish, "finish:"+check.Name)
		}
	}
}

func TestDoctorCurlProbesPassPathValidator(t *testing.T) {
	validators := []core.ExecValidator{core.NoControlChars(), core.PathUnder("/workspace")}

	t.Run("ingress route probe", func(t *testing.T) {
		var probeArgs []string
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				switch {
				case strings.Contains(strings.Join(spec.Args, "\x00"), "{.metadata.name}|{.spec.rules[0].host}|{.spec.rules[0].http.paths[0].path}"):
					return &core.MockCommand{OutputData: []byte("doctor-smoke-old||/doctor-smoke-old/mcp\ndemo||/demo/mcp\n")}
				case contains(spec.Args, "get") && contains(spec.Args, "svc"):
					return &core.MockCommand{OutputData: []byte("web:8000:32080\n")}
				case len(spec.Args) > 0 && spec.Args[0] == "run":
					probeArgs = append([]string(nil), spec.Args...)
					if strings.Contains(strings.Join(spec.Args, "\x00"), "/dev/null") {
						t.Fatal("doctor curl helper should not pass /dev/null through kubectl validators")
					}
					return &core.MockCommand{OutputData: []byte("200")}
				default:
					return &core.MockCommand{}
				}
			},
		}
		kubectl := core.NewTestKubectlClientWithValidators(mock, validators)
		check := checkIngressRouteProbe(kubectl, "mcp-servers", DistroGeneric)
		if !check.OK {
			t.Fatalf("expected OK, got detail=%q", check.Detail)
		}
		overrides := argValueWithPrefix(probeArgs, "--overrides=")
		if overrides == "" {
			t.Fatalf("ingress route probe should use restricted-compliant overrides, got args=%v", probeArgs)
		}
		for _, want := range []string{
			`"allowPrivilegeEscalation":false`,
			`"runAsNonRoot":true`,
			`"runAsUser":65532`,
			`"seccompProfile":{"type":"RuntimeDefault"}`,
			`"command":["curl"]`,
		} {
			if !strings.Contains(overrides, want) {
				t.Fatalf("ingress route probe overrides missing %s: %s", want, overrides)
			}
		}
	})

	t.Run("sentinel API auth probe", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				switch {
				case contains(spec.Args, "namespace"):
					return &core.MockCommand{OutputData: []byte(doctorSentinelNamespace)}
				case contains(spec.Args, "jsonpath={.data.UI_API_KEY}"):
					return &core.MockCommand{OutputData: []byte("dGVzdA==")}
				case contains(spec.Args, "curl"):
					if contains(spec.Args, "/dev/null") {
						t.Fatal("doctor curl helper should not pass /dev/null through kubectl validators")
					}
					return &core.MockCommand{OutputData: []byte("200")}
				default:
					return &core.MockCommand{}
				}
			},
		}
		kubectl := core.NewTestKubectlClientWithValidators(mock, validators)
		check := checkSentinelAPIAuthProbe(kubectl)
		if !check.OK {
			t.Fatalf("expected OK, got detail=%q", check.Detail)
		}
	})
}

func TestRemediationHintPerDistro(t *testing.T) {
	for _, d := range []Distribution{DistroK3s, DistroKind, DistroMinikube, DistroDockerDesktop, DistroGeneric} {
		hint := remediationHint(d)
		if hint == "" {
			t.Errorf("no remediation hint for %q", d)
		}
	}
}

func TestReportHasRegistryOrPullFailure(t *testing.T) {
	if reportHasRegistryOrPullFailure(DoctorReport{Checks: []DoctorCheck{{Name: "sentinel secrets", OK: false}}}) {
		t.Fatal("sentinel-only failures should not print registry remediation")
	}
	if !reportHasRegistryOrPullFailure(DoctorReport{Checks: []DoctorCheck{{Name: "registry HTTP pull mismatch", OK: false}}}) {
		t.Fatal("registry pull failures should print registry remediation")
	}
}

func TestCheckNamespacePodAdmission(t *testing.T) {
	t.Run("ok on dry-run success", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				return &core.MockCommand{OutputData: []byte("pod/doctor-admission created (dry run)")}
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		check := checkNamespacePodAdmission(kubectl, "mcp-servers")
		if !check.OK {
			t.Fatalf("expected OK, got detail=%q", check.Detail)
		}
	})

	t.Run("fails when admission rejects", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				return &core.MockCommand{
					OutputData: []byte("pods \"doctor-admission\" is forbidden: exceeds quota"),
					OutputErr:  errors.New("exit status 1"),
				}
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		check := checkNamespacePodAdmission(kubectl, "mcp-servers")
		if check.OK {
			t.Fatalf("expected failure when dry-run rejected; detail=%q", check.Detail)
		}
		if !strings.Contains(check.Detail, "forbidden") {
			t.Fatalf("expected server error passthrough in detail, got %q", check.Detail)
		}
		if check.Remedy == "" {
			t.Fatal("expected remedy hint")
		}
	})
}

func TestCheckTraefikDeploymentReady(t *testing.T) {
	cases := []struct {
		name   string
		output string
		outErr error
		wantOK bool
	}{
		{name: "ready 2/2", output: "2/2", wantOK: true},
		{name: "partially ready 1/3", output: "1/3", wantOK: false},
		{name: "zero desired", output: "0/0", wantOK: false},
		{name: "empty output", output: "", wantOK: false},
		{name: "malformed pair", output: "ready", wantOK: false},
		{name: "non-numeric ready", output: "x/2", wantOK: false},
		{name: "kubectl error", output: "", outErr: errors.New("not found"), wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mock := &core.MockExecutor{
				CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
					return &core.MockCommand{OutputData: []byte(tc.output), OutputErr: tc.outErr}
				},
			}
			kubectl := core.NewTestKubectlClient(mock)
			check := checkTraefikDeploymentReady(kubectl, DistroGeneric)
			if check.OK != tc.wantOK {
				t.Fatalf("OK=%v want %v; detail=%q", check.OK, tc.wantOK, check.Detail)
			}
		})
	}

	t.Run("ok with k3s bundled traefik deployment", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				switch {
				case contains(spec.Args, "kube-system"):
					return &core.MockCommand{OutputData: []byte("1/1")}
				case contains(spec.Args, "traefik"):
					return &core.MockCommand{OutputErr: errors.New("not found")}
				}
				return &core.MockCommand{}
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		check := checkTraefikDeploymentReady(kubectl, DistroK3s)
		if !check.OK {
			t.Fatalf("expected OK for k3s bundled Traefik, got detail=%q", check.Detail)
		}
		if !strings.Contains(check.Detail, "k3s bundled Traefik") {
			t.Fatalf("detail should mention k3s bundled Traefik, got %q", check.Detail)
		}
	})
}

func TestCheckTraefikServiceExposure(t *testing.T) {
	cases := []struct {
		name       string
		output     string
		wantOK     bool
		wantDetail string
	}{
		{
			name:       "LoadBalancer with IP",
			output:     "LoadBalancer|10.1.2.3||8000:0,",
			wantOK:     true,
			wantDetail: "10.1.2.3",
		},
		{
			name:       "LoadBalancer with hostname only",
			output:     "LoadBalancer||lb.example.com|8000:0,",
			wantOK:     true,
			wantDetail: "lb.example.com",
		},
		{
			name:   "NodePort exposes web port",
			output: "NodePort|||8000:32080,8443:32443,",
			wantOK: true,
		},
		{
			name:   "LoadBalancer pending, no NodePort for web",
			output: "LoadBalancer|||9999:32099,",
			wantOK: false,
		},
		{
			name:   "ClusterIP only",
			output: "ClusterIP|||9999:0,",
			wantOK: false,
		},
		{
			name:   "malformed payload",
			output: "LoadBalancer",
			wantOK: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mock := &core.MockExecutor{
				CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
					return &core.MockCommand{OutputData: []byte(tc.output)}
				},
			}
			kubectl := core.NewTestKubectlClient(mock)
			check := checkTraefikServiceExposure(kubectl, DistroGeneric)
			if check.OK != tc.wantOK {
				t.Fatalf("OK=%v want %v; detail=%q", check.OK, tc.wantOK, check.Detail)
			}
			if tc.wantDetail != "" && !strings.Contains(check.Detail, tc.wantDetail) {
				t.Fatalf("detail=%q does not contain %q", check.Detail, tc.wantDetail)
			}
		})
	}

	t.Run("ok with k3s bundled traefik service", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				switch {
				case contains(spec.Args, "kube-system"):
					return &core.MockCommand{OutputData: []byte("LoadBalancer|10.1.2.3||web:80:0,websecure:443:0,")}
				case contains(spec.Args, "traefik"):
					return &core.MockCommand{OutputErr: errors.New("not found")}
				}
				return &core.MockCommand{}
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		check := checkTraefikServiceExposure(kubectl, DistroK3s)
		if !check.OK {
			t.Fatalf("expected OK for k3s bundled Traefik, got detail=%q", check.Detail)
		}
		if !strings.Contains(check.Detail, "k3s bundled Traefik") {
			t.Fatalf("detail should mention k3s bundled Traefik, got %q", check.Detail)
		}
	})
}

func TestCheckOperatorRecentReconcileErrors(t *testing.T) {
	cases := []struct {
		name   string
		logs   string
		outErr error
		wantOK bool
	}{
		{name: "clean logs", logs: "started reconciler\nresource synced\n", wantOK: true},
		{name: "reconciler error pattern", logs: "ERROR Reconciler error: something broke\n", wantOK: false},
		{name: "failed to reconcile pattern", logs: "msg=\"failed to reconcile\" server=foo\n", wantOK: false},
		{name: "error syncing pattern", logs: "level=error error syncing mcpserver/foo\n", wantOK: false},
		{name: "case-insensitive match", logs: "FAILED TO RECONCILE\n", wantOK: false},
		{name: "ignores doctor smoke transient errors", logs: "ERROR Reconciler error mcpserver=doctor-smoke-123\n", wantOK: true},
		{name: "no logs, OK", logs: "", wantOK: true},
		{name: "kubectl error surfaces", outErr: errors.New("no such deploy"), wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mock := &core.MockExecutor{
				CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
					return &core.MockCommand{OutputData: []byte(tc.logs), OutputErr: tc.outErr}
				},
			}
			kubectl := core.NewTestKubectlClient(mock)
			check := checkOperatorRecentReconcileErrors(kubectl)
			if check.OK != tc.wantOK {
				t.Fatalf("OK=%v want %v; detail=%q", check.OK, tc.wantOK, check.Detail)
			}
		})
	}
}

func TestCheckOperatorClusterRoleRules(t *testing.T) {
	t.Run("accepts split verbs with matching API groups", func(t *testing.T) {
		check := checkOperatorClusterRoleRulesFromJSON(t, `{
			"rules": [
				{"apiGroups":[""],"resources":["serviceaccounts","configmaps","services"],"verbs":["get"]},
				{"apiGroups":[""],"resources":["serviceaccounts","configmaps","services"],"verbs":["list"]},
				{"apiGroups":[""],"resources":["serviceaccounts","configmaps","services"],"verbs":["watch"]},
				{"apiGroups":["apps"],"resources":["deployments"],"verbs":["get","list"]},
				{"apiGroups":["apps"],"resources":["deployments"],"verbs":["watch"]},
				{"apiGroups":["networking.k8s.io"],"resources":["ingresses"],"verbs":["get"]},
				{"apiGroups":["networking.k8s.io"],"resources":["ingresses"],"verbs":["list","watch"]}
			]
		}`)
		if !check.OK {
			t.Fatalf("expected OK for additive RBAC rules, got detail=%q", check.Detail)
		}
	})

	t.Run("requires the expected API group", func(t *testing.T) {
		check := checkOperatorClusterRoleRulesFromJSON(t, `{
			"rules": [
				{"apiGroups":[""],"resources":["serviceaccounts","configmaps","services"],"verbs":["get","list","watch"]},
				{"apiGroups":["extensions"],"resources":["deployments","ingresses"],"verbs":["get","list","watch"]}
			]
		}`)
		if check.OK {
			t.Fatal("expected failure for permissions granted on the wrong API groups")
		}
		for _, want := range []string{"deployments.apps", "ingresses.networking.k8s.io"} {
			if !strings.Contains(check.Detail, want) {
				t.Fatalf("detail should mention %q, got %q", want, check.Detail)
			}
		}
	})

	t.Run("accepts wildcard grants", func(t *testing.T) {
		check := checkOperatorClusterRoleRulesFromJSON(t, `{
			"rules": [
				{"apiGroups":["*"],"resources":["*"],"verbs":["*"]}
			]
		}`)
		if !check.OK {
			t.Fatalf("expected OK for wildcard RBAC rule, got detail=%q", check.Detail)
		}
	})
}

func checkOperatorClusterRoleRulesFromJSON(t *testing.T, body string) DoctorCheck {
	t.Helper()
	mock := &core.MockExecutor{
		CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
			if !contains(spec.Args, "mcp-runtime-operator-role") {
				t.Fatalf("unexpected command args: %v", spec.Args)
			}
			return &core.MockCommand{OutputData: []byte(body)}
		},
	}
	return checkOperatorClusterRoleRules(core.NewTestKubectlClient(mock))
}

func TestCheckMCPServerReconcileSmoke(t *testing.T) {
	t.Run("waits for deployment rollout readiness", func(t *testing.T) {
		sawRollout := false
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				switch {
				case contains(spec.Args, "get") && contains(spec.Args, "mcpservers"):
					return &core.MockCommand{OutputData: []byte("go-example\n")}
				case argContains(spec.Args, "readyReplicas") && argContains(spec.Args, "containerPort"):
					return &core.MockCommand{OutputData: []byte("go-example|1|registry.local/go-example:dev|8088\n")}
				case contains(spec.Args, "apply"):
					return &core.MockCommand{}
				case contains(spec.Args, "rollout"):
					sawRollout = true
					if !contains(spec.Args, "--timeout=2m30s") {
						t.Fatalf("rollout status args %v missing timeout", spec.Args)
					}
					return &core.MockCommand{}
				case contains(spec.Args, "get"):
					return &core.MockCommand{OutputData: []byte("doctor-smoke")}
				case contains(spec.Args, "delete"):
					return &core.MockCommand{}
				}
				return &core.MockCommand{OutputErr: fmt.Errorf("unexpected command: %v", spec.Args)}
			},
		}
		kubectl := core.NewTestKubectlClient(mock)

		check := checkMCPServerReconcileSmoke(kubectl, "mcp-servers")
		if !check.OK {
			t.Fatalf("expected OK, got detail=%q remedy=%q", check.Detail, check.Remedy)
		}
		if !sawRollout {
			t.Fatal("expected smoke check to wait for deployment rollout readiness")
		}
		if !strings.Contains(check.Detail, "ready deployment/service/ingress") {
			t.Fatalf("detail should mention ready deployment resources, got %q", check.Detail)
		}
	})

	t.Run("fails when deployment rollout does not become ready", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				switch {
				case contains(spec.Args, "get") && contains(spec.Args, "mcpservers"):
					return &core.MockCommand{OutputData: []byte("go-example\n")}
				case argContains(spec.Args, "readyReplicas") && argContains(spec.Args, "containerPort"):
					return &core.MockCommand{OutputData: []byte("go-example|1|registry.local/go-example:dev|8088\n")}
				case contains(spec.Args, "apply"):
					return &core.MockCommand{}
				case contains(spec.Args, "rollout"):
					return &core.MockCommand{
						OutputData: []byte("deployment \"doctor-smoke\" exceeded its progress deadline"),
						OutputErr:  errors.New("rollout timed out"),
					}
				case contains(spec.Args, "get"):
					return &core.MockCommand{OutputData: []byte("doctor-smoke")}
				case contains(spec.Args, "delete"):
					return &core.MockCommand{}
				}
				return &core.MockCommand{OutputErr: fmt.Errorf("unexpected command: %v", spec.Args)}
			},
		}
		kubectl := core.NewTestKubectlClient(mock)

		check := checkMCPServerReconcileSmoke(kubectl, "mcp-servers")
		if check.OK {
			t.Fatalf("expected failure when rollout fails; detail=%q", check.Detail)
		}
		if !strings.Contains(check.Detail, "deployment did not become ready") {
			t.Fatalf("detail should describe rollout readiness failure, got %q", check.Detail)
		}
		if !strings.Contains(check.Detail, "exceeded its progress deadline") {
			t.Fatalf("detail should include rollout output, got %q", check.Detail)
		}
	})

	t.Run("skips impossible rollout wait for fallback pause image", func(t *testing.T) {
		sawRollout := false
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				switch {
				case contains(spec.Args, "get") && contains(spec.Args, "mcpservers"):
					return &core.MockCommand{}
				case argContains(spec.Args, "readyReplicas") && argContains(spec.Args, "containerPort"):
					return &core.MockCommand{OutputData: []byte("oauth-issuer|1|docker.io/library/python:3.12-alpine|8080\n")}
				case contains(spec.Args, "apply"):
					return &core.MockCommand{}
				case contains(spec.Args, "rollout"):
					sawRollout = true
					return &core.MockCommand{OutputErr: errors.New("rollout should not be called")}
				case contains(spec.Args, "get") && contains(spec.Args, "pods"):
					return &core.MockCommand{OutputData: []byte("node-a")}
				case contains(spec.Args, "get"):
					return &core.MockCommand{OutputData: []byte("doctor-smoke")}
				case contains(spec.Args, "delete"):
					return &core.MockCommand{}
				}
				return &core.MockCommand{OutputErr: fmt.Errorf("unexpected command: %v", spec.Args)}
			},
		}
		kubectl := core.NewTestKubectlClient(mock)

		check := checkMCPServerReconcileSmoke(kubectl, "mcp-servers")
		if !check.OK {
			t.Fatalf("expected OK, got detail=%q remedy=%q", check.Detail, check.Remedy)
		}
		if sawRollout {
			t.Fatal("fallback pause smoke should not wait for rollout readiness")
		}
		if !strings.Contains(check.Detail, "skipped readiness") {
			t.Fatalf("detail should mention skipped readiness, got %q", check.Detail)
		}
	})
}

func argContains(args []string, value string) bool {
	for _, arg := range args {
		if strings.Contains(arg, value) {
			return true
		}
	}
	return false
}

func TestCheckNodeCapacity(t *testing.T) {
	t.Run("metrics-server healthy", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				if contains(spec.Args, "top") {
					return &core.MockCommand{OutputData: []byte("node-a  200m  10%  1Gi  20%\nnode-b  400m  20%  2Gi  40%\n")}
				}
				return &core.MockCommand{}
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		check := checkNodeCapacity(kubectl)
		if !check.OK {
			t.Fatalf("expected OK, got detail=%q", check.Detail)
		}
		if !strings.Contains(check.Detail, "2 node") {
			t.Fatalf("detail should mention node count, got %q", check.Detail)
		}
	})

	t.Run("flags hot node at CPU>=95%%", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				if contains(spec.Args, "top") {
					return &core.MockCommand{OutputData: []byte("node-a  3800m  96%  7Gi  80%\n")}
				}
				return &core.MockCommand{}
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		check := checkNodeCapacity(kubectl)
		if check.OK {
			t.Fatal("expected failure for 96% CPU")
		}
		if !strings.Contains(check.Detail, "node-a") {
			t.Fatalf("detail should name the hot node, got %q", check.Detail)
		}
	})

	t.Run("flags hot node at memory>=95%%", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				if contains(spec.Args, "top") {
					return &core.MockCommand{OutputData: []byte("node-a  100m  10%  8Gi  97%\n")}
				}
				return &core.MockCommand{}
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		check := checkNodeCapacity(kubectl)
		if check.OK {
			t.Fatal("expected failure for 97% memory")
		}
	})

	t.Run("falls back to allocatable when metrics-server missing", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				switch {
				case contains(spec.Args, "top"):
					return &core.MockCommand{OutputData: []byte("error: Metrics API not available"), OutputErr: errors.New("exit status 1")}
				case contains(spec.Args, "nodes"):
					return &core.MockCommand{OutputData: []byte("node-a  4  16Gi\nnode-b  4  16Gi\n")}
				}
				return &core.MockCommand{}
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		check := checkNodeCapacity(kubectl)
		if !check.OK {
			t.Fatalf("expected OK fallback, got detail=%q", check.Detail)
		}
		if !strings.Contains(check.Detail, "metrics-server unavailable") {
			t.Fatalf("detail should note metrics-server fallback, got %q", check.Detail)
		}
	})

	t.Run("fails when both metrics and allocatable are unavailable", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				return &core.MockCommand{OutputErr: errors.New("cluster unreachable")}
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		check := checkNodeCapacity(kubectl)
		if check.OK {
			t.Fatal("expected failure when both paths fail")
		}
	})
}

func TestCheckMCPServersImagePullSecrets(t *testing.T) {
	t.Run("ok when no imagePullSecrets configured", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				return &core.MockCommand{OutputData: []byte("")}
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		check := checkMCPServersImagePullSecrets(kubectl, "mcp-servers")
		if !check.OK {
			t.Fatalf("expected OK when no secrets configured, got detail=%q", check.Detail)
		}
	})

	t.Run("ok when all referenced secrets exist", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				switch {
				case contains(spec.Args, "serviceaccount"):
					return &core.MockCommand{OutputData: []byte("reg-creds\ngcr-creds\n")}
				case contains(spec.Args, "secret"):
					// both secret lookups succeed
					return &core.MockCommand{OutputData: []byte("reg-creds")}
				}
				return &core.MockCommand{}
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		check := checkMCPServersImagePullSecrets(kubectl, "mcp-servers")
		if !check.OK {
			t.Fatalf("expected OK, got detail=%q", check.Detail)
		}
	})

	t.Run("fails when a referenced secret is missing", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				switch {
				case contains(spec.Args, "serviceaccount"):
					return &core.MockCommand{OutputData: []byte("reg-creds\nmissing-creds\n")}
				case contains(spec.Args, "missing-creds"):
					return &core.MockCommand{OutputErr: errors.New("secrets \"missing-creds\" not found")}
				case contains(spec.Args, "secret"):
					return &core.MockCommand{OutputData: []byte("reg-creds")}
				}
				return &core.MockCommand{}
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		check := checkMCPServersImagePullSecrets(kubectl, "mcp-servers")
		if check.OK {
			t.Fatalf("expected failure when a pull secret is missing; detail=%q", check.Detail)
		}
		if !strings.Contains(check.Detail, "missing-creds") {
			t.Fatalf("detail should name the missing secret, got %q", check.Detail)
		}
	})

	t.Run("fails when serviceaccount lookup errors", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				return &core.MockCommand{OutputErr: errors.New("serviceaccount default not found")}
			},
		}
		kubectl := core.NewTestKubectlClient(mock)
		check := checkMCPServersImagePullSecrets(kubectl, "mcp-servers")
		if check.OK {
			t.Fatal("expected failure when serviceaccount lookup fails")
		}
	})
}

func TestCheckMCPServersImagePullSmokeUsesRestrictedCompliantPodSpec(t *testing.T) {
	var smokeRunArgs []string
	mock := &core.MockExecutor{
		CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
			switch {
			case contains(spec.Args, "get") && contains(spec.Args, "mcpservers"):
				return &core.MockCommand{}
			case contains(spec.Args, "get") && contains(spec.Args, "deploy"):
				return &core.MockCommand{OutputData: []byte("")}
			case len(spec.Args) > 0 && spec.Args[0] == "run":
				smokeRunArgs = append([]string(nil), spec.Args...)
				return &core.MockCommand{}
			case contains(spec.Args, "get") && argContains(spec.Args, "imageID"):
				return &core.MockCommand{OutputData: []byte("docker-pullable://registry.k8s.io/pause@sha256:test")}
			case contains(spec.Args, "delete"):
				return &core.MockCommand{}
			default:
				return &core.MockCommand{}
			}
		},
	}
	kubectl := core.NewTestKubectlClient(mock)
	check := checkMCPServersImagePullSmoke(kubectl, "mcp-servers")
	if !check.OK {
		t.Fatalf("expected image pull smoke to pass, got detail=%q", check.Detail)
	}
	if len(smokeRunArgs) == 0 {
		t.Fatal("expected image pull smoke to create a pod")
	}
	overrides := argValueWithPrefix(smokeRunArgs, "--overrides=")
	if overrides == "" {
		t.Fatalf("image pull smoke should use restricted-compliant overrides, got args=%v", smokeRunArgs)
	}
	for _, want := range []string{
		`"automountServiceAccountToken":false`,
		`"allowPrivilegeEscalation":false`,
		`"runAsNonRoot":true`,
		`"runAsUser":65532`,
		`"seccompProfile":{"type":"RuntimeDefault"}`,
		`"capabilities":{"drop":["ALL"]}`,
	} {
		if !strings.Contains(overrides, want) {
			t.Fatalf("image pull smoke overrides missing %s: %s", want, overrides)
		}
	}
	for _, notWant := range []string{`"command":`, `"args":`} {
		if strings.Contains(overrides, notWant) {
			t.Fatalf("image pull smoke overrides should not set %s: %s", notWant, overrides)
		}
	}
}

func TestCheckMCPServersDNSAndNetworkAllowsColdCurlImagePull(t *testing.T) {
	var runArgs []string
	mock := &core.MockExecutor{
		CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
			if len(spec.Args) > 0 && spec.Args[0] == "run" {
				runArgs = append([]string(nil), spec.Args...)
				return &core.MockCommand{OutputData: []byte("HTTP/1.1 200 OK\r\n")}
			}
			return &core.MockCommand{OutputErr: fmt.Errorf("unexpected command: %v", spec.Args)}
		},
	}
	kubectl := core.NewTestKubectlClient(mock)
	check := checkMCPServersDNSAndNetwork(kubectl)
	if !check.OK {
		t.Fatalf("expected DNS/network probe to pass, got detail=%q", check.Detail)
	}
	if len(runArgs) == 0 {
		t.Fatal("expected DNS/network probe to create a curl pod")
	}
	wantTimeout := "--pod-running-timeout=" + doctorProbePodRunTimeout
	if !contains(runArgs, wantTimeout) {
		t.Fatalf("DNS/network probe should allow cold curl image pulls with %s, got args=%v", wantTimeout, runArgs)
	}
	if contains(runArgs, "--pod-running-timeout=30s") {
		t.Fatalf("DNS/network probe should not use the old 30s running timeout, got args=%v", runArgs)
	}
}

func TestRestrictedRunOverridesUsesNumericNonRootUser(t *testing.T) {
	overrides := restrictedRunOverrides("probe", "curlimages/curl:8.7.1", "curl", "-sSI", "http://example.test")
	for _, want := range []string{
		`"runAsNonRoot":true`,
		`"runAsUser":65532`,
		`"command":["curl"]`,
		`"args":["-sSI","http://example.test"]`,
	} {
		if !strings.Contains(overrides, want) {
			t.Fatalf("restricted overrides missing %s: %s", want, overrides)
		}
	}
}

func argValueWithPrefix(args []string, prefix string) string {
	for _, arg := range args {
		if strings.HasPrefix(arg, prefix) {
			return strings.TrimPrefix(arg, prefix)
		}
	}
	return ""
}
