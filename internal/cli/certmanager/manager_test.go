package certmanager

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"mcp-runtime/internal/cli/core"
)

func TestCheckCertManagerInstalledWithKubectl(t *testing.T) {
	mock := &core.MockExecutor{}
	kubectl := core.NewTestKubectlClient(mock)

	if err := checkCertManagerInstalledWithKubectl(kubectl); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.Commands) != 1 {
		t.Fatalf("expected 1 kubectl command, got %d", len(mock.Commands))
	}
	if !commandHasArgs(mock.Commands[0], "get", "crd", core.CertManagerCRDName) {
		t.Fatalf("unexpected args: %v", mock.Commands[0].Args)
	}
}

func TestCheckCertManagerInstalledWithKubectlError(t *testing.T) {
	mock := &core.MockExecutor{DefaultRunErr: errors.New("missing")}
	kubectl := core.NewTestKubectlClient(mock)

	if err := checkCertManagerInstalledWithKubectl(kubectl); !errors.Is(err, core.ErrCertManagerNotInstalled) {
		t.Fatalf("expected core.ErrCertManagerNotInstalled, got %v", err)
	}
}

func TestCheckCASecretWithKubectl(t *testing.T) {
	mock := &core.MockExecutor{}
	kubectl := core.NewTestKubectlClient(mock)

	if err := checkCASecretWithKubectl(kubectl); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.Commands) != 1 {
		t.Fatalf("expected 1 kubectl command, got %d", len(mock.Commands))
	}
	if !commandHasArgs(mock.Commands[0], "get", "secret", certCASecretName, "-n", certManagerNamespace) {
		t.Fatalf("unexpected args: %v", mock.Commands[0].Args)
	}
}

func TestCheckCASecretWithKubectlError(t *testing.T) {
	mock := &core.MockExecutor{DefaultRunErr: errors.New("missing")}
	kubectl := core.NewTestKubectlClient(mock)

	if err := checkCASecretWithKubectl(kubectl); !errors.Is(err, core.ErrCASecretNotFound) {
		t.Fatalf("expected core.ErrCASecretNotFound, got %v", err)
	}
}

func TestApplyClusterIssuerWithKubectl(t *testing.T) {
	mock := &core.MockExecutor{}
	kubectl := core.NewTestKubectlClient(mock)

	if err := applyClusterIssuerWithKubectl(kubectl); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.Commands) != 1 {
		t.Fatalf("expected 1 kubectl command, got %d", len(mock.Commands))
	}
	if !commandHasArgs(mock.Commands[0], "apply", "-f", clusterIssuerManifestPath) {
		t.Fatalf("unexpected args: %v", mock.Commands[0].Args)
	}
}

func TestApplyRegistryCertificateWithKubectl(t *testing.T) {
	orig := core.DefaultCLIConfig
	t.Cleanup(func() { core.DefaultCLIConfig = orig })
	core.DefaultCLIConfig = &core.CLIConfig{RegistryEndpoint: "10.43.39.164:5000", RegistryIngressHost: "registry.prod.example.com"}

	root := repoRootForTest(t)
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working dir: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir to repo root: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origDir)
	})

	var applyCmd *core.MockCommand
	mock := &core.MockExecutor{
		CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
			cmd := &core.MockCommand{Args: spec.Args}
			if commandHasArgs(spec, "apply", "-f", "-") {
				applyCmd = cmd
			}
			return cmd
		},
	}
	kubectl := core.NewTestKubectlClient(mock)

	if err := applyRegistryCertificateWithKubectl(kubectl); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.Commands) != 1 {
		t.Fatalf("expected 1 kubectl command, got %d", len(mock.Commands))
	}
	if applyCmd == nil {
		t.Fatal("expected apply command")
	}
	captured, err := io.ReadAll(applyCmd.StdinR)
	if err != nil {
		t.Fatalf("read apply stdin: %v", err)
	}
	if !strings.Contains(string(captured), "registry.prod.example.com") {
		t.Fatalf("expected rewritten registry host, got: %s", string(captured))
	}
}

func TestWaitForCertificateReadyWithKubectl(t *testing.T) {
	mock := &core.MockExecutor{}
	kubectl := core.NewTestKubectlClient(mock)

	timeout := 15 * time.Second
	if err := waitForCertificateReadyWithKubectl(kubectl, registryCertificateName, core.NamespaceRegistry, timeout); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.Commands) != 1 {
		t.Fatalf("expected 1 kubectl command, got %d", len(mock.Commands))
	}
	if !commandHasArgs(mock.Commands[0], "wait", "--for=condition=Ready", "certificate/"+registryCertificateName, "-n", core.NamespaceRegistry, "--timeout=15s") {
		t.Fatalf("unexpected args: %v", mock.Commands[0].Args)
	}
}

func TestCertManagerStatus(t *testing.T) {
	mock := &core.MockExecutor{}
	kubectl := core.NewTestKubectlClient(mock)
	manager := NewCertManager(kubectl, zap.NewNop())

	if err := manager.Status(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.Commands) != 4 {
		t.Fatalf("expected 4 kubectl commands, got %d", len(mock.Commands))
	}
}

func TestCertManagerStatusMissingCertificate(t *testing.T) {
	mock := &core.MockExecutor{
		CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
			cmd := &core.MockCommand{Args: spec.Args}
			if commandHasArgs(spec, "get", "certificate", registryCertificateName, "-n", core.NamespaceRegistry) {
				cmd.RunErr = errors.New("missing cert")
			}
			return cmd
		},
	}
	kubectl := core.NewTestKubectlClient(mock)
	manager := NewCertManager(kubectl, zap.NewNop())

	if err := manager.Status(); err == nil {
		t.Fatal("expected error")
	}
}

func TestCertManagerApplyMissingCASecret(t *testing.T) {
	mock := &core.MockExecutor{
		CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
			cmd := &core.MockCommand{Args: spec.Args}
			if commandHasArgs(spec, "get", "secret", certCASecretName, "-n", certManagerNamespace) {
				cmd.RunErr = errors.New("missing secret")
			}
			return cmd
		},
	}
	kubectl := core.NewTestKubectlClient(mock)
	manager := NewCertManager(kubectl, zap.NewNop())

	if err := manager.Apply(false); err == nil {
		t.Fatal("expected error")
	}
}

func TestCertManagerApplyClusterIssuerError(t *testing.T) {
	mock := &core.MockExecutor{
		CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
			cmd := &core.MockCommand{Args: spec.Args}
			if commandHasArgs(spec, "apply", "-f", clusterIssuerManifestPath) {
				cmd.RunErr = errors.New("apply issuer failed")
			}
			return cmd
		},
	}
	kubectl := core.NewTestKubectlClient(mock)
	manager := NewCertManager(kubectl, zap.NewNop())

	if err := manager.Apply(false); err == nil {
		t.Fatal("expected error")
	}
}

func TestCertManagerApplyEnsureNamespaceError(t *testing.T) {
	mock := &core.MockExecutor{
		CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
			cmd := &core.MockCommand{Args: spec.Args}
			if commandHasArgs(spec, "apply", "-f", "-") {
				cmd.RunErr = errors.New("apply namespace failed")
			}
			return cmd
		},
	}
	kubectl := core.NewTestKubectlClient(mock)
	manager := NewCertManager(kubectl, zap.NewNop())

	if err := manager.Apply(false); err == nil {
		t.Fatal("expected error")
	}
}

func TestCertManagerApplyRegistryCertificateError(t *testing.T) {
	// The registry certificate is applied via `kubectl apply -f - -n registry` with the
	// manifest content piped over stdin, so match on those args rather than on the
	// on-disk manifest path.
	mock := &core.MockExecutor{
		CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
			cmd := &core.MockCommand{Args: spec.Args}
			if commandHasArgs(spec, "apply", "-f", "-", "-n", core.NamespaceRegistry) {
				cmd.RunErr = errors.New("apply cert failed")
			}
			return cmd
		},
	}
	kubectl := core.NewTestKubectlClient(mock)
	manager := NewCertManager(kubectl, zap.NewNop())

	if err := manager.Apply(false); err == nil {
		t.Fatal("expected error")
	}
}

func TestCertManagerWaitFailure(t *testing.T) {
	mock := &core.MockExecutor{
		CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
			cmd := &core.MockCommand{Args: spec.Args}
			if commandHasArgs(spec, "wait", "--for=condition=Ready", "certificate/"+registryCertificateName, "-n", core.NamespaceRegistry) {
				cmd.RunErr = errors.New("wait failed")
			}
			return cmd
		},
	}
	kubectl := core.NewTestKubectlClient(mock)
	manager := NewCertManager(kubectl, zap.NewNop())

	if err := manager.Wait(time.Second); err == nil {
		t.Fatal("expected error")
	}
}

func TestCertManagerStatusMissingCertManager(t *testing.T) {
	mock := &core.MockExecutor{
		CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
			cmd := &core.MockCommand{Args: spec.Args}
			if commandHasArgs(spec, "get", "crd", core.CertManagerCRDName) {
				cmd.RunErr = errors.New("not found")
			}
			return cmd
		},
	}
	kubectl := core.NewTestKubectlClient(mock)
	manager := NewCertManager(kubectl, zap.NewNop())

	var buf bytes.Buffer
	setDefaultPrinterWriter(t, &buf)

	if err := manager.Status(); err == nil {
		t.Fatal("expected error when cert-manager not installed")
	}
}

func TestCertManagerStatusMissingCASecret(t *testing.T) {
	mock := &core.MockExecutor{
		CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
			cmd := &core.MockCommand{Args: spec.Args}
			if commandHasArgs(spec, "get", "secret", certCASecretName, "-n", certManagerNamespace) {
				cmd.RunErr = errors.New("not found")
			}
			return cmd
		},
	}
	kubectl := core.NewTestKubectlClient(mock)
	manager := NewCertManager(kubectl, zap.NewNop())

	var buf bytes.Buffer
	setDefaultPrinterWriter(t, &buf)

	if err := manager.Status(); err == nil {
		t.Fatal("expected error when CA secret not found")
	}
}

func TestCertManagerStatusMissingClusterIssuer(t *testing.T) {
	mock := &core.MockExecutor{
		CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
			cmd := &core.MockCommand{Args: spec.Args}
			if commandHasArgs(spec, "get", "clusterissuer", certClusterIssuerName) {
				cmd.RunErr = errors.New("not found")
			}
			return cmd
		},
	}
	kubectl := core.NewTestKubectlClient(mock)
	manager := NewCertManager(kubectl, zap.NewNop())

	var buf bytes.Buffer
	setDefaultPrinterWriter(t, &buf)

	if err := manager.Status(); err == nil {
		t.Fatal("expected error when ClusterIssuer not found")
	}
}

func TestCertManagerApplyMissingCertManager(t *testing.T) {
	mock := &core.MockExecutor{
		CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
			cmd := &core.MockCommand{Args: spec.Args}
			if commandHasArgs(spec, "get", "crd", core.CertManagerCRDName) {
				cmd.RunErr = errors.New("not found")
			}
			return cmd
		},
	}
	kubectl := core.NewTestKubectlClient(mock)
	manager := NewCertManager(kubectl, zap.NewNop())

	var buf bytes.Buffer
	setDefaultPrinterWriter(t, &buf)

	if err := manager.Apply(false); err == nil {
		t.Fatal("expected error when cert-manager not installed")
	}
}

func TestCheckClusterIssuerWithKubectlSuccess(t *testing.T) {
	mock := &core.MockExecutor{}
	kubectl := core.NewTestKubectlClient(mock)

	if err := checkClusterIssuerWithKubectl(kubectl); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.Commands) != 1 {
		t.Fatalf("expected 1 kubectl command, got %d", len(mock.Commands))
	}
	if !commandHasArgs(mock.Commands[0], "get", "clusterissuer", certClusterIssuerName) {
		t.Fatalf("unexpected args: %v", mock.Commands[0].Args)
	}
}

func TestCheckClusterIssuerWithKubectlError(t *testing.T) {
	mock := &core.MockExecutor{DefaultRunErr: errors.New("not found")}
	kubectl := core.NewTestKubectlClient(mock)

	if err := checkClusterIssuerWithKubectl(kubectl); err == nil {
		t.Fatal("expected error when cluster issuer not found")
	}
}

func TestCheckNamedClusterIssuerWithKubectl(t *testing.T) {
	mock := &core.MockExecutor{}
	kubectl := core.NewTestKubectlClient(mock)
	if err := checkNamedClusterIssuerWithKubectl(kubectl, " company-ca "); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.Commands) != 1 || !commandHasArgs(mock.Commands[0], "get", "clusterissuer", "company-ca") {
		t.Fatalf("unexpected command: %v", mock.Commands)
	}
}

func TestCheckNamedClusterIssuerWithKubectlError(t *testing.T) {
	mock := &core.MockExecutor{DefaultRunErr: errors.New("not found")}
	kubectl := core.NewTestKubectlClient(mock)
	if err := checkNamedClusterIssuerWithKubectl(kubectl, "missing"); err == nil {
		t.Fatal("expected error")
	}
}

func TestCheckNamedClusterIssuerWithKubectlEmptyName(t *testing.T) {
	kubectl := core.NewTestKubectlClient(&core.MockExecutor{})
	err := checkNamedClusterIssuerWithKubectl(kubectl, "  ")
	if err == nil {
		t.Fatal("expected error for empty name")
	}
	if !errors.Is(err, core.ErrClusterIssuerNotFound) {
		t.Fatalf("expected core.ErrClusterIssuerNotFound, got %v", err)
	}
}

func TestCheckCertificateWithKubectlError(t *testing.T) {
	mock := &core.MockExecutor{DefaultRunErr: errors.New("not found")}
	kubectl := core.NewTestKubectlClient(mock)

	if err := checkCertificateWithKubectl(kubectl, "test-cert", "test-ns"); err == nil {
		t.Fatal("expected error when certificate not found")
	}
}

func TestRemoveRegistryIngressShimAnnotationWithKubectl(t *testing.T) {
	t.Run("skips when registry ingress is absent", func(t *testing.T) {
		mock := &core.MockExecutor{
			CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
				cmd := &core.MockCommand{Args: spec.Args}
				if commandHasArgs(spec, "get", "ingress", core.RegistryServiceName, "-n", core.NamespaceRegistry) {
					cmd.RunErr = errors.New("not found")
				}
				return cmd
			},
		}
		kubectl := core.NewTestKubectlClient(mock)

		if err := removeRegistryIngressShimAnnotationWithKubectl(kubectl); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(mock.Commands) != 1 {
			t.Fatalf("expected only get ingress command, got: %v", mock.Commands)
		}
	})

	t.Run("removes annotation when registry ingress exists", func(t *testing.T) {
		mock := &core.MockExecutor{}
		kubectl := core.NewTestKubectlClient(mock)

		if err := removeRegistryIngressShimAnnotationWithKubectl(kubectl); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(mock.Commands) != 2 {
			t.Fatalf("expected get and patch commands, got: %v", mock.Commands)
		}
		if !commandHasArgs(mock.Commands[1], "patch", "ingress", core.RegistryServiceName, "-n", core.NamespaceRegistry, "--type=merge", "-p", `{"metadata":{"annotations":{"cert-manager.io/cluster-issuer":null}}}`) {
			t.Fatalf("unexpected patch command: %v", mock.Commands[1].Args)
		}
	})
}

func TestCheckRegistryCertificateOwnershipWithKubectl(t *testing.T) {
	t.Run("allows registry-cert owner", func(t *testing.T) {
		mock := certificateListMock(`{"items":[{"metadata":{"name":"registry-cert"},"spec":{"secretName":"registry-tls"}}]}`)
		kubectl := core.NewTestKubectlClient(mock)

		if err := checkRegistryCertificateOwnershipWithKubectl(kubectl); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects ingress-shim owner", func(t *testing.T) {
		mock := certificateListMock(`{"items":[{"metadata":{"name":"registry-tls"},"spec":{"secretName":"registry-tls"}}]}`)
		kubectl := core.NewTestKubectlClient(mock)

		err := checkRegistryCertificateOwnershipWithKubectl(kubectl)
		if err == nil {
			t.Fatal("expected conflict")
		}
		if !strings.Contains(err.Error(), "registry-tls") || !strings.Contains(err.Error(), "registry-cert") {
			t.Fatalf("expected actionable ownership error, got: %v", err)
		}
	})

	t.Run("rejects duplicate owners", func(t *testing.T) {
		mock := certificateListMock(`{"items":[{"metadata":{"name":"registry-cert"},"spec":{"secretName":"registry-tls"}},{"metadata":{"name":"registry-tls"},"spec":{"secretName":"registry-tls"}}]}`)
		kubectl := core.NewTestKubectlClient(mock)

		err := checkRegistryCertificateOwnershipWithKubectl(kubectl)
		if err == nil {
			t.Fatal("expected conflict")
		}
		if !strings.Contains(err.Error(), "registry-cert, registry-tls") {
			t.Fatalf("expected sorted certificate names, got: %v", err)
		}
	})
}

func TestApplyClusterIssuerWithKubectlError(t *testing.T) {
	mock := &core.MockExecutor{DefaultRunErr: errors.New("apply failed")}
	kubectl := core.NewTestKubectlClient(mock)

	if err := applyClusterIssuerWithKubectl(kubectl); err == nil {
		t.Fatal("expected error when apply fails")
	}
}

func TestApplyRegistryCertificateWithKubectlError(t *testing.T) {
	mock := &core.MockExecutor{DefaultRunErr: errors.New("apply failed")}
	kubectl := core.NewTestKubectlClient(mock)

	if err := applyRegistryCertificateWithKubectl(kubectl); err == nil {
		t.Fatal("expected error when apply fails")
	}
}

func TestWaitForCertificateReadyWithKubectlError(t *testing.T) {
	mock := &core.MockExecutor{DefaultRunErr: errors.New("timeout")}
	kubectl := core.NewTestKubectlClient(mock)

	if err := waitForCertificateReadyWithKubectl(kubectl, "test-cert", "test-ns", time.Second); err == nil {
		t.Fatal("expected error when wait times out")
	}
}

func commandHasArgs(cmd core.ExecSpec, args ...string) bool {
	if len(args) == 0 {
		return true
	}
	for i := 0; i <= len(cmd.Args)-len(args); i++ {
		matches := true
		for j, arg := range args {
			if cmd.Args[i+j] != arg {
				matches = false
				break
			}
		}
		if matches {
			return true
		}
	}
	return false
}

func certificateListMock(output string) *core.MockExecutor {
	return &core.MockExecutor{
		CommandFunc: func(spec core.ExecSpec) *core.MockCommand {
			cmd := &core.MockCommand{Args: spec.Args}
			if commandHasArgs(spec, "get", "certificates", "-n", core.NamespaceRegistry, "-o", "json") {
				cmd.RunFunc = func() error {
					if cmd.StdoutW != nil {
						_, _ = cmd.StdoutW.Write([]byte(output))
					}
					return nil
				}
			}
			return cmd
		},
	}
}

func repoRootForTest(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working dir: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repo root not found")
		}
		dir = parent
	}
}

func setDefaultPrinterWriter(t *testing.T, w *bytes.Buffer) {
	t.Helper()
	prevWriter := core.DefaultPrinter.Writer
	prevQuiet := core.DefaultPrinter.Quiet
	core.DefaultPrinter.Writer = w
	core.DefaultPrinter.Quiet = false
	t.Cleanup(func() {
		core.DefaultPrinter.Writer = prevWriter
		core.DefaultPrinter.Quiet = prevQuiet
	})
}
