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
	if len(report.Checks) < 2 {
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
