package cli

import (
	"bytes"
	"errors"
	"fmt"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestCheckCertManagerInstalledWithKubectl(t *testing.T) {
	mock := &MockExecutor{}
	kubectl := &KubectlClient{exec: mock, validators: nil}

	if err := checkCertManagerInstalledWithKubectl(kubectl); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.Commands) != 1 {
		t.Fatalf("expected 1 kubectl command, got %d", len(mock.Commands))
	}
	if !commandHasArgs(mock.Commands[0], "get", "crd", CertManagerCRDName) {
		t.Fatalf("unexpected args: %v", mock.Commands[0].Args)
	}
}

func TestCheckCertManagerInstalledWithKubectlError(t *testing.T) {
	mock := &MockExecutor{DefaultRunErr: errors.New("missing")}
	kubectl := &KubectlClient{exec: mock, validators: nil}

	if err := checkCertManagerInstalledWithKubectl(kubectl); !errors.Is(err, ErrCertManagerNotInstalled) {
		t.Fatalf("expected ErrCertManagerNotInstalled, got %v", err)
	}
}

func TestCheckCASecretWithKubectl(t *testing.T) {
	mock := &MockExecutor{}
	kubectl := &KubectlClient{exec: mock, validators: nil}

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
	mock := &MockExecutor{DefaultRunErr: errors.New("missing")}
	kubectl := &KubectlClient{exec: mock, validators: nil}

	if err := checkCASecretWithKubectl(kubectl); !errors.Is(err, ErrCASecretNotFound) {
		t.Fatalf("expected ErrCASecretNotFound, got %v", err)
	}
}

func TestApplyClusterIssuerWithKubectl(t *testing.T) {
	mock := &MockExecutor{}
	kubectl := &KubectlClient{exec: mock, validators: nil}

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
	mock := &MockExecutor{}
	kubectl := &KubectlClient{exec: mock, validators: nil}

	if err := applyRegistryCertificateWithKubectl(kubectl); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.Commands) != 1 {
		t.Fatalf("expected 1 kubectl command, got %d", len(mock.Commands))
	}
	if !commandHasArgs(mock.Commands[0], "apply", "-f", registryCertificateManifestPath) {
		t.Fatalf("unexpected args: %v", mock.Commands[0].Args)
	}
}

func TestWaitForCertificateReadyWithKubectl(t *testing.T) {
	mock := &MockExecutor{}
	kubectl := &KubectlClient{exec: mock, validators: nil}

	timeout := 15 * time.Second
	if err := waitForCertificateReadyWithKubectl(kubectl, registryCertificateName, NamespaceRegistry, timeout); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.Commands) != 1 {
		t.Fatalf("expected 1 kubectl command, got %d", len(mock.Commands))
	}
	if !commandHasArgs(mock.Commands[0], "wait", "--for=condition=Ready", "certificate/"+registryCertificateName, "-n", NamespaceRegistry, "--timeout=15s") {
		t.Fatalf("unexpected args: %v", mock.Commands[0].Args)
	}
}

func TestCertManagerStatus(t *testing.T) {
	mock := &MockExecutor{}
	kubectl := &KubectlClient{exec: mock, validators: nil}
	manager := NewCertManager(kubectl, zap.NewNop())

	if err := manager.Status(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.Commands) != 4 {
		t.Fatalf("expected 4 kubectl commands, got %d", len(mock.Commands))
	}
}

func TestCertManagerStatusMissingCertificate(t *testing.T) {
	mock := &MockExecutor{
		CommandFunc: func(spec ExecSpec) *MockCommand {
			cmd := &MockCommand{Args: spec.Args}
			if commandHasArgs(spec, "get", "certificate", registryCertificateName, "-n", NamespaceRegistry) {
				cmd.RunErr = errors.New("missing cert")
			}
			return cmd
		},
	}
	kubectl := &KubectlClient{exec: mock, validators: nil}
	manager := NewCertManager(kubectl, zap.NewNop())

	if err := manager.Status(); err == nil {
		t.Fatal("expected error")
	}
}

func TestCertManagerApplyMissingCASecret(t *testing.T) {
	mock := &MockExecutor{
		CommandFunc: func(spec ExecSpec) *MockCommand {
			cmd := &MockCommand{Args: spec.Args}
			if commandHasArgs(spec, "get", "secret", certCASecretName, "-n", certManagerNamespace) {
				cmd.RunErr = errors.New("missing secret")
			}
			return cmd
		},
	}
	kubectl := &KubectlClient{exec: mock, validators: nil}
	manager := NewCertManager(kubectl, zap.NewNop())

	if err := manager.Apply(); err == nil {
		t.Fatal("expected error")
	}
}

func TestCertManagerApplyClusterIssuerError(t *testing.T) {
	mock := &MockExecutor{
		CommandFunc: func(spec ExecSpec) *MockCommand {
			cmd := &MockCommand{Args: spec.Args}
			if commandHasArgs(spec, "apply", "-f", clusterIssuerManifestPath) {
				cmd.RunErr = errors.New("apply issuer failed")
			}
			return cmd
		},
	}
	kubectl := &KubectlClient{exec: mock, validators: nil}
	manager := NewCertManager(kubectl, zap.NewNop())

	if err := manager.Apply(); err == nil {
		t.Fatal("expected error")
	}
}

func TestCertManagerApplyEnsureNamespaceError(t *testing.T) {
	origKubectl := kubectlClient
	t.Cleanup(func() { kubectlClient = origKubectl })

	mock := &MockExecutor{
		CommandFunc: func(spec ExecSpec) *MockCommand {
			cmd := &MockCommand{Args: spec.Args}
			if commandHasArgs(spec, "apply", "-f", "-") {
				cmd.RunErr = errors.New("apply namespace failed")
			}
			return cmd
		},
	}
	kubectl := &KubectlClient{exec: mock, validators: nil}
	kubectlClient = kubectl
	manager := NewCertManager(kubectl, zap.NewNop())

	if err := manager.Apply(); err == nil {
		t.Fatal("expected error")
	}
}

func TestCertManagerApplyRegistryCertificateError(t *testing.T) {
	origKubectl := kubectlClient
	t.Cleanup(func() { kubectlClient = origKubectl })

	mock := &MockExecutor{
		CommandFunc: func(spec ExecSpec) *MockCommand {
			cmd := &MockCommand{Args: spec.Args}
			if commandHasArgs(spec, "apply", "-f", registryCertificateManifestPath) {
				cmd.RunErr = errors.New("apply cert failed")
			}
			return cmd
		},
	}
	kubectl := &KubectlClient{exec: mock, validators: nil}
	kubectlClient = kubectl
	manager := NewCertManager(kubectl, zap.NewNop())

	if err := manager.Apply(); err == nil {
		t.Fatal("expected error")
	}
}

func TestCertManagerWaitFailure(t *testing.T) {
	mock := &MockExecutor{
		CommandFunc: func(spec ExecSpec) *MockCommand {
			cmd := &MockCommand{Args: spec.Args}
			if commandHasArgs(spec, "wait", "--for=condition=Ready", "certificate/"+registryCertificateName, "-n", NamespaceRegistry) {
				cmd.RunErr = errors.New("wait failed")
			}
			return cmd
		},
	}
	kubectl := &KubectlClient{exec: mock, validators: nil}
	manager := NewCertManager(kubectl, zap.NewNop())

	if err := manager.Wait(time.Second); err == nil {
		t.Fatal("expected error")
	}
}

func TestCertWaitCmdUsesDefaultTimeout(t *testing.T) {
	var waitArgs []string
	mock := &MockExecutor{
		CommandFunc: func(spec ExecSpec) *MockCommand {
			if commandHasArgs(spec, "wait", "--for=condition=Ready", "certificate/"+registryCertificateName, "-n", NamespaceRegistry) {
				waitArgs = spec.Args
			}
			return &MockCommand{Args: spec.Args}
		},
	}
	kubectl := &KubectlClient{exec: mock, validators: nil}
	manager := NewCertManager(kubectl, zap.NewNop())

	var buf bytes.Buffer
	setDefaultPrinterWriter(t, &buf)

	cmd := manager.newCertWaitCmd()
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if waitArgs == nil {
		t.Fatal("expected wait command to be invoked")
	}
	wantTimeout := fmt.Sprintf("--timeout=%s", GetCertTimeout())
	if !contains(waitArgs, wantTimeout) {
		t.Fatalf("expected timeout %q, got args: %v", wantTimeout, waitArgs)
	}
}

func TestCertWaitCmdUsesFlagTimeout(t *testing.T) {
	var waitArgs []string
	mock := &MockExecutor{
		CommandFunc: func(spec ExecSpec) *MockCommand {
			if commandHasArgs(spec, "wait", "--for=condition=Ready", "certificate/"+registryCertificateName, "-n", NamespaceRegistry) {
				waitArgs = spec.Args
			}
			return &MockCommand{Args: spec.Args}
		},
	}
	kubectl := &KubectlClient{exec: mock, validators: nil}
	manager := NewCertManager(kubectl, zap.NewNop())

	var buf bytes.Buffer
	setDefaultPrinterWriter(t, &buf)

	cmd := manager.newCertWaitCmd()
	if err := cmd.Flags().Set("timeout", "5s"); err != nil {
		t.Fatalf("set timeout flag: %v", err)
	}
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if waitArgs == nil {
		t.Fatal("expected wait command to be invoked")
	}
	if !contains(waitArgs, "--timeout=5s") {
		t.Fatalf("expected timeout flag to be used, got args: %v", waitArgs)
	}
}

func TestCertApplyCmdInvokesApply(t *testing.T) {
	origKubectl := kubectlClient
	t.Cleanup(func() { kubectlClient = origKubectl })

	mock := &MockExecutor{}
	kubectl := &KubectlClient{exec: mock, validators: nil}
	kubectlClient = kubectl
	manager := NewCertManager(kubectl, zap.NewNop())

	var buf bytes.Buffer
	setDefaultPrinterWriter(t, &buf)

	cmd := manager.newCertApplyCmd()
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.Commands) == 0 {
		t.Fatal("expected kubectl commands to be invoked")
	}
}

func TestCertStatusCmdInvokesStatus(t *testing.T) {
	mock := &MockExecutor{}
	kubectl := &KubectlClient{exec: mock, validators: nil}
	manager := NewCertManager(kubectl, zap.NewNop())

	var buf bytes.Buffer
	setDefaultPrinterWriter(t, &buf)

	cmd := manager.newCertStatusCmd()
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.Commands) == 0 {
		t.Fatal("expected kubectl commands to be invoked")
	}
}
