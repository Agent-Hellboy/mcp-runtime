package cli

import (
	"errors"
	"strings"
	"testing"
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
			mock := &MockExecutor{
				CommandFunc: func(spec ExecSpec) *MockCommand {
					switch {
					case contains(spec.Args, "jsonpath={.items[*].status.nodeInfo.kubeletVersion}"):
						return &MockCommand{OutputData: []byte(tc.kubelet)}
					case contains(spec.Args, "jsonpath={.items[*].metadata.name}"):
						return &MockCommand{OutputData: []byte(tc.names)}
					case contains(spec.Args, "current-context"):
						return &MockCommand{OutputData: []byte(tc.context)}
					}
					return &MockCommand{}
				},
			}
			kubectl := &KubectlClient{exec: mock, validators: nil}
			got := DetectDistribution(kubectl)
			if got != tc.want {
				t.Fatalf("DetectDistribution() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCheckRegistryService(t *testing.T) {
	t.Run("ok with nodeport", func(t *testing.T) {
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				return &MockCommand{OutputData: []byte("32000")}
			},
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		check := checkRegistryService(kubectl)
		if !check.OK {
			t.Fatalf("expected OK, got detail=%q", check.Detail)
		}
		if !strings.Contains(check.Detail, "32000") {
			t.Fatalf("detail should mention the NodePort, got %q", check.Detail)
		}
	})

	t.Run("fails when service missing", func(t *testing.T) {
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				return &MockCommand{OutputErr: errors.New("not found")}
			},
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
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
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				return &MockCommand{OutputData: []byte("mcp-servers")}
			},
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		check := checkNamespaceExists(kubectl, "mcp-servers")
		if !check.OK {
			t.Fatalf("expected OK, got detail=%q", check.Detail)
		}
	})

	t.Run("fails when namespace missing", func(t *testing.T) {
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				return &MockCommand{OutputErr: errors.New("not found")}
			},
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		check := checkNamespaceExists(kubectl, "mcp-servers")
		if check.OK {
			t.Fatal("expected failure when namespace is missing")
		}
	})
}

func TestCheckMCPServerCRD(t *testing.T) {
	t.Run("ok when CRD exists", func(t *testing.T) {
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				return &MockCommand{OutputData: []byte("mcpservers.mcpruntime.org")}
			},
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		check := checkMCPServerCRD(kubectl)
		if !check.OK {
			t.Fatalf("expected OK, got detail=%q", check.Detail)
		}
	})

	t.Run("fails when CRD missing", func(t *testing.T) {
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				return &MockCommand{OutputErr: errors.New("not found")}
			},
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		check := checkMCPServerCRD(kubectl)
		if check.OK {
			t.Fatal("expected failure when CRD is missing")
		}
	})
}

func TestCheckOperatorReady(t *testing.T) {
	t.Run("ok when desired replicas are ready", func(t *testing.T) {
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				return &MockCommand{OutputData: []byte("1/1")}
			},
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		check := checkOperatorReady(kubectl)
		if !check.OK {
			t.Fatalf("expected OK, got detail=%q", check.Detail)
		}
	})

	t.Run("fails when not enough replicas are ready", func(t *testing.T) {
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				return &MockCommand{OutputData: []byte("0/1")}
			},
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		check := checkOperatorReady(kubectl)
		if check.OK {
			t.Fatal("expected failure for 0/1 ready replicas")
		}
	})
}

func TestCheckTraefikIngressClass(t *testing.T) {
	t.Run("ok when ingressClass exists", func(t *testing.T) {
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				return &MockCommand{OutputData: []byte("traefik")}
			},
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		check := checkTraefikIngressClass(kubectl)
		if !check.OK {
			t.Fatalf("expected OK, got detail=%q", check.Detail)
		}
	})

	t.Run("fails when ingressClass is missing", func(t *testing.T) {
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				return &MockCommand{OutputErr: errors.New("not found")}
			},
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		check := checkTraefikIngressClass(kubectl)
		if check.OK {
			t.Fatal("expected failure when ingressClass missing")
		}
	})
}

func TestCheckTraefikWebEntrypoint(t *testing.T) {
	t.Run("ok when service exposes 8000", func(t *testing.T) {
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				return &MockCommand{OutputData: []byte("web:8000:32080\nwebsecure:8443:32443\n")}
			},
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		check := checkTraefikWebEntrypoint(kubectl)
		if !check.OK {
			t.Fatalf("expected OK, got detail=%q", check.Detail)
		}
	})

	t.Run("fails when service does not expose 8000", func(t *testing.T) {
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				return &MockCommand{OutputData: []byte("web:80:32080\n")}
			},
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		check := checkTraefikWebEntrypoint(kubectl)
		if check.OK {
			t.Fatal("expected failure when port 8000 is not exposed")
		}
	})
}

func TestCheckRegistryReachableFromCluster(t *testing.T) {
	t.Run("ok on HTTP 200", func(t *testing.T) {
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				return &MockCommand{OutputData: []byte("HTTP/1.1 200 OK\nDocker-Distribution-Api-Version: registry/2.0\n")}
			},
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		check := checkRegistryReachableFromCluster(kubectl)
		if !check.OK {
			t.Fatalf("expected OK, got detail=%q", check.Detail)
		}
	})

	t.Run("fails on non-200", func(t *testing.T) {
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				return &MockCommand{OutputData: []byte("HTTP/1.1 503 Service Unavailable\n")}
			},
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		check := checkRegistryReachableFromCluster(kubectl)
		if check.OK {
			t.Fatal("expected failure for non-200")
		}
	})

	t.Run("does not false-pass when body includes non-status 200", func(t *testing.T) {
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				return &MockCommand{OutputData: []byte("diagnostic: 200 retries\nHTTP/1.1 503 Service Unavailable\n")}
			},
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		check := checkRegistryReachableFromCluster(kubectl)
		if check.OK {
			t.Fatal("expected failure when HTTP status line is not 200")
		}
	})

	t.Run("fails when helper pod errors", func(t *testing.T) {
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				return &MockCommand{OutputErr: errors.New("pod failed"), RunErr: errors.New("pod failed")}
			},
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		check := checkRegistryReachableFromCluster(kubectl)
		if check.OK {
			t.Fatal("expected failure when helper pod errors")
		}
	})
}

func TestRunDoctorAggregates(t *testing.T) {
	mock := &MockExecutor{
		CommandFunc: func(spec ExecSpec) *MockCommand {
			switch {
			case contains(spec.Args, "jsonpath={.items[*].status.nodeInfo.kubeletVersion}"):
				return &MockCommand{OutputData: []byte("v1.34.6+k3s1")}
			case contains(spec.Args, "namespace mcp-servers"):
				return &MockCommand{OutputData: []byte("mcp-servers")}
			case contains(spec.Args, "crd mcpservers.mcpruntime.org"):
				return &MockCommand{OutputData: []byte("mcpservers.mcpruntime.org")}
			case contains(spec.Args, "mcp-runtime-operator-controller-manager"):
				return &MockCommand{OutputData: []byte("1/1")}
			case contains(spec.Args, "ingressclass traefik"):
				return &MockCommand{OutputData: []byte("traefik")}
			case contains(spec.Args, "svc -n traefik traefik"):
				return &MockCommand{OutputData: []byte("web:8000:32080\n")}
			case contains(spec.Args, "jsonpath={.spec.ports[0].nodePort}"):
				return &MockCommand{OutputData: []byte("32000")}
			case contains(spec.Args, "curl"):
				return &MockCommand{OutputData: []byte("HTTP/1.1 503 Service Unavailable\n")}
			}
			return &MockCommand{}
		},
	}
	kubectl := &KubectlClient{exec: mock, validators: nil}
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

func TestRemediationHintPerDistro(t *testing.T) {
	for _, d := range []Distribution{DistroK3s, DistroKind, DistroMinikube, DistroDockerDesktop, DistroGeneric} {
		hint := remediationHint(d)
		if hint == "" {
			t.Errorf("no remediation hint for %q", d)
		}
	}
}

func TestCheckNamespacePodAdmission(t *testing.T) {
	t.Run("ok on dry-run success", func(t *testing.T) {
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				return &MockCommand{OutputData: []byte("pod/doctor-admission created (dry run)")}
			},
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		check := checkNamespacePodAdmission(kubectl, "mcp-servers")
		if !check.OK {
			t.Fatalf("expected OK, got detail=%q", check.Detail)
		}
	})

	t.Run("fails when admission rejects", func(t *testing.T) {
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				return &MockCommand{
					OutputData: []byte("pods \"doctor-admission\" is forbidden: exceeds quota"),
					OutputErr:  errors.New("exit status 1"),
				}
			},
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
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
			mock := &MockExecutor{
				CommandFunc: func(spec ExecSpec) *MockCommand {
					return &MockCommand{OutputData: []byte(tc.output), OutputErr: tc.outErr}
				},
			}
			kubectl := &KubectlClient{exec: mock, validators: nil}
			check := checkTraefikDeploymentReady(kubectl)
			if check.OK != tc.wantOK {
				t.Fatalf("OK=%v want %v; detail=%q", check.OK, tc.wantOK, check.Detail)
			}
		})
	}
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
			mock := &MockExecutor{
				CommandFunc: func(spec ExecSpec) *MockCommand {
					return &MockCommand{OutputData: []byte(tc.output)}
				},
			}
			kubectl := &KubectlClient{exec: mock, validators: nil}
			check := checkTraefikServiceExposure(kubectl)
			if check.OK != tc.wantOK {
				t.Fatalf("OK=%v want %v; detail=%q", check.OK, tc.wantOK, check.Detail)
			}
			if tc.wantDetail != "" && !strings.Contains(check.Detail, tc.wantDetail) {
				t.Fatalf("detail=%q does not contain %q", check.Detail, tc.wantDetail)
			}
		})
	}
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
		{name: "no logs, OK", logs: "", wantOK: true},
		{name: "kubectl error surfaces", outErr: errors.New("no such deploy"), wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mock := &MockExecutor{
				CommandFunc: func(spec ExecSpec) *MockCommand {
					return &MockCommand{OutputData: []byte(tc.logs), OutputErr: tc.outErr}
				},
			}
			kubectl := &KubectlClient{exec: mock, validators: nil}
			check := checkOperatorRecentReconcileErrors(kubectl)
			if check.OK != tc.wantOK {
				t.Fatalf("OK=%v want %v; detail=%q", check.OK, tc.wantOK, check.Detail)
			}
		})
	}
}

func TestCheckNodeCapacity(t *testing.T) {
	t.Run("metrics-server healthy", func(t *testing.T) {
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				if contains(spec.Args, "top") {
					return &MockCommand{OutputData: []byte("node-a  200m  10%  1Gi  20%\nnode-b  400m  20%  2Gi  40%\n")}
				}
				return &MockCommand{}
			},
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		check := checkNodeCapacity(kubectl)
		if !check.OK {
			t.Fatalf("expected OK, got detail=%q", check.Detail)
		}
		if !strings.Contains(check.Detail, "2 node") {
			t.Fatalf("detail should mention node count, got %q", check.Detail)
		}
	})

	t.Run("flags hot node at CPU>=95%%", func(t *testing.T) {
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				if contains(spec.Args, "top") {
					return &MockCommand{OutputData: []byte("node-a  3800m  96%  7Gi  80%\n")}
				}
				return &MockCommand{}
			},
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		check := checkNodeCapacity(kubectl)
		if check.OK {
			t.Fatal("expected failure for 96% CPU")
		}
		if !strings.Contains(check.Detail, "node-a") {
			t.Fatalf("detail should name the hot node, got %q", check.Detail)
		}
	})

	t.Run("flags hot node at memory>=95%%", func(t *testing.T) {
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				if contains(spec.Args, "top") {
					return &MockCommand{OutputData: []byte("node-a  100m  10%  8Gi  97%\n")}
				}
				return &MockCommand{}
			},
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		check := checkNodeCapacity(kubectl)
		if check.OK {
			t.Fatal("expected failure for 97% memory")
		}
	})

	t.Run("falls back to allocatable when metrics-server missing", func(t *testing.T) {
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				switch {
				case contains(spec.Args, "top"):
					return &MockCommand{OutputData: []byte("error: Metrics API not available"), OutputErr: errors.New("exit status 1")}
				case contains(spec.Args, "nodes"):
					return &MockCommand{OutputData: []byte("node-a  4  16Gi\nnode-b  4  16Gi\n")}
				}
				return &MockCommand{}
			},
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		check := checkNodeCapacity(kubectl)
		if !check.OK {
			t.Fatalf("expected OK fallback, got detail=%q", check.Detail)
		}
		if !strings.Contains(check.Detail, "metrics-server unavailable") {
			t.Fatalf("detail should note metrics-server fallback, got %q", check.Detail)
		}
	})

	t.Run("fails when both metrics and allocatable are unavailable", func(t *testing.T) {
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				return &MockCommand{OutputErr: errors.New("cluster unreachable")}
			},
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		check := checkNodeCapacity(kubectl)
		if check.OK {
			t.Fatal("expected failure when both paths fail")
		}
	})
}

func TestCheckMCPServersImagePullSecrets(t *testing.T) {
	t.Run("ok when no imagePullSecrets configured", func(t *testing.T) {
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				return &MockCommand{OutputData: []byte("")}
			},
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		check := checkMCPServersImagePullSecrets(kubectl, "mcp-servers")
		if !check.OK {
			t.Fatalf("expected OK when no secrets configured, got detail=%q", check.Detail)
		}
	})

	t.Run("ok when all referenced secrets exist", func(t *testing.T) {
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				switch {
				case contains(spec.Args, "serviceaccount"):
					return &MockCommand{OutputData: []byte("reg-creds\ngcr-creds\n")}
				case contains(spec.Args, "secret"):
					// both secret lookups succeed
					return &MockCommand{OutputData: []byte("reg-creds")}
				}
				return &MockCommand{}
			},
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		check := checkMCPServersImagePullSecrets(kubectl, "mcp-servers")
		if !check.OK {
			t.Fatalf("expected OK, got detail=%q", check.Detail)
		}
	})

	t.Run("fails when a referenced secret is missing", func(t *testing.T) {
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				switch {
				case contains(spec.Args, "serviceaccount"):
					return &MockCommand{OutputData: []byte("reg-creds\nmissing-creds\n")}
				case contains(spec.Args, "missing-creds"):
					return &MockCommand{OutputErr: errors.New("secrets \"missing-creds\" not found")}
				case contains(spec.Args, "secret"):
					return &MockCommand{OutputData: []byte("reg-creds")}
				}
				return &MockCommand{}
			},
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		check := checkMCPServersImagePullSecrets(kubectl, "mcp-servers")
		if check.OK {
			t.Fatalf("expected failure when a pull secret is missing; detail=%q", check.Detail)
		}
		if !strings.Contains(check.Detail, "missing-creds") {
			t.Fatalf("detail should name the missing secret, got %q", check.Detail)
		}
	})

	t.Run("fails when serviceaccount lookup errors", func(t *testing.T) {
		mock := &MockExecutor{
			CommandFunc: func(spec ExecSpec) *MockCommand {
				return &MockCommand{OutputErr: errors.New("serviceaccount default not found")}
			},
		}
		kubectl := &KubectlClient{exec: mock, validators: nil}
		check := checkMCPServersImagePullSecrets(kubectl, "mcp-servers")
		if check.OK {
			t.Fatal("expected failure when serviceaccount lookup fails")
		}
	})
}
