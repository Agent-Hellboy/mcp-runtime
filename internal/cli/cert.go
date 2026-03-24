package cli

// This file implements certificate and TLS management functionality.
// It handles cert-manager integration, CA secret management, and certificate provisioning.

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

const (
	certManagerNamespace = "cert-manager"
	// #nosec G101 -- This is the name of a Kubernetes secret resource, not actual credentials
	certCASecretName                = "mcp-runtime-ca"
	certClusterIssuerName           = "mcp-runtime-ca"
	registryCertificateName         = "registry-cert"
	clusterIssuerManifestPath       = "config/cert-manager/cluster-issuer.yaml"
	registryCertificateManifestPath = "config/cert-manager/example-registry-certificate.yaml"
)

// CertManager manages cert-manager resources for the platform.
type CertManager struct {
	kubectl KubectlRunner
	logger  *zap.Logger
}

// NewCertManager creates a CertManager with the given dependencies.
func NewCertManager(kubectl KubectlRunner, logger *zap.Logger) *CertManager {
	return &CertManager{kubectl: kubectl, logger: logger}
}

func (m *ClusterManager) newClusterCertCmd() *cobra.Command {
	certMgr := NewCertManager(m.kubectl, m.logger)
	cmd := &cobra.Command{
		Use:   "cert",
		Short: "Manage cert-manager resources",
		Long:  "Manage cert-manager resources required for TLS in the MCP platform",
	}

	cmd.AddCommand(certMgr.newCertStatusCmd())
	cmd.AddCommand(certMgr.newCertApplyCmd())
	cmd.AddCommand(certMgr.newCertWaitCmd())

	return cmd
}

func (m *CertManager) newCertStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check cert-manager resources",
		Long:  "Check cert-manager installation, CA secret, issuer, and registry certificate",
		RunE: func(cmd *cobra.Command, args []string) error {
			return m.Status()
		},
	}

	return cmd
}

func (m *CertManager) newCertApplyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply cert-manager resources",
		Long:  "Apply ClusterIssuer and registry Certificate manifests",
		RunE: func(cmd *cobra.Command, args []string) error {
			return m.Apply()
		},
	}

	return cmd
}

func (m *CertManager) newCertWaitCmd() *cobra.Command {
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "wait",
		Short: "Wait for registry certificate readiness",
		Long:  "Wait for the registry certificate to reach Ready state",
		RunE: func(cmd *cobra.Command, args []string) error {
			if timeout == 0 {
				timeout = GetCertTimeout()
			}
			return m.Wait(timeout)
		},
	}

	cmd.Flags().DurationVar(&timeout, "timeout", 0, "Timeout for certificate readiness (default from MCP_CERT_TIMEOUT)")
	return cmd
}

// Status verifies cert-manager installation and required resources.
func (m *CertManager) Status() error {
	Info("Checking cert-manager installation")
	if err := checkCertManagerInstalledWithKubectl(m.kubectl); err != nil {
		err := wrapWithSentinel(ErrCertManagerNotInstalled, err, "cert-manager not installed. Install it first:\n  helm install cert-manager jetstack/cert-manager --namespace cert-manager --create-namespace --set crds.enabled=true")
		Error("Cert-manager not installed")
		logStructuredError(m.logger, err, "Cert-manager not installed")
		return err
	}
	Info("Checking CA secret")
	if err := checkCASecretWithKubectl(m.kubectl); err != nil {
		err := newWithSentinel(ErrCASecretNotFound, fmt.Sprintf("CA secret %q not found in cert-manager namespace. Create it first:\n  kubectl create secret tls %s --cert=ca.crt --key=ca.key -n %s", certCASecretName, certCASecretName, certManagerNamespace))
		Error("CA secret not found")
		logStructuredError(m.logger, err, "CA secret not found")
		return err
	}
	Info("Checking ClusterIssuer")
	if err := checkClusterIssuerWithKubectl(m.kubectl); err != nil {
		err := newWithSentinel(ErrClusterIssuerNotFound, fmt.Sprintf("ClusterIssuer %q not found. Apply it first:\n  kubectl apply -f %s", certClusterIssuerName, clusterIssuerManifestPath))
		Error("ClusterIssuer not found")
		logStructuredError(m.logger, err, "ClusterIssuer not found")
		return err
	}
	Info("Checking registry Certificate")
	if err := checkCertificateWithKubectl(m.kubectl, registryCertificateName, NamespaceRegistry); err != nil {
		err := newWithSentinel(ErrRegistryCertificateNotFound, fmt.Sprintf("registry Certificate not found. Apply it first:\n  kubectl apply -f %s", registryCertificateManifestPath))
		Error("Registry Certificate not found")
		logStructuredError(m.logger, err, "Registry Certificate not found")
		return err
	}
	Success("Cert-manager resources are present")
	return nil
}

// Apply installs cert-manager resources required for registry TLS.
func (m *CertManager) Apply() error {
	Info("Checking cert-manager installation")
	if err := checkCertManagerInstalledWithKubectl(m.kubectl); err != nil {
		err := wrapWithSentinel(ErrCertManagerNotInstalled, err, "cert-manager not installed. Install it first:\n  helm install cert-manager jetstack/cert-manager --namespace cert-manager --create-namespace --set crds.enabled=true")
		Error("Cert-manager not installed")
		logStructuredError(m.logger, err, "Cert-manager not installed")
		return err
	}
	Info("Checking CA secret")
	if err := checkCASecretWithKubectl(m.kubectl); err != nil {
		err := newWithSentinel(ErrCASecretNotFound, fmt.Sprintf("CA secret %q not found in cert-manager namespace. Create it first:\n  kubectl create secret tls %s --cert=ca.crt --key=ca.key -n %s", certCASecretName, certCASecretName, certManagerNamespace))
		Error("CA secret not found")
		logStructuredError(m.logger, err, "CA secret not found")
		return err
	}

	Info("Applying ClusterIssuer")
	if err := applyClusterIssuerWithKubectl(m.kubectl); err != nil {
		wrappedErr := wrapWithSentinel(ErrClusterIssuerApplyFailed, err, fmt.Sprintf("failed to apply ClusterIssuer: %v", err))
		Error("Failed to apply ClusterIssuer")
		logStructuredError(m.logger, wrappedErr, "Failed to apply ClusterIssuer")
		return wrappedErr
	}
	if err := ensureNamespace(NamespaceRegistry); err != nil {
		wrappedErr := wrapWithSentinelAndContext(
			ErrCreateRegistryNamespaceFailed,
			err,
			fmt.Sprintf("failed to create registry namespace: %v", err),
			map[string]any{"namespace": NamespaceRegistry, "component": "cert"},
		)
		Error("Failed to create registry namespace")
		logStructuredError(m.logger, wrappedErr, "Failed to create registry namespace")
		return wrappedErr
	}
	Info("Applying Certificate for registry")
	if err := applyRegistryCertificateWithKubectl(m.kubectl); err != nil {
		wrappedErr := wrapWithSentinelAndContext(
			ErrApplyCertificateFailed,
			err,
			fmt.Sprintf("failed to apply Certificate: %v", err),
			map[string]any{"certificate": registryCertificateName, "namespace": NamespaceRegistry, "component": "cert"},
		)
		Error("Failed to apply Certificate")
		logStructuredError(m.logger, wrappedErr, "Failed to apply Certificate")
		return wrappedErr
	}

	Success("Cert-manager resources applied")
	return nil
}

// Wait blocks until the registry certificate is Ready or times out.
func (m *CertManager) Wait(timeout time.Duration) error {
	Info(fmt.Sprintf("Waiting for certificate to be issued (timeout: %s)", timeout))
	if err := waitForCertificateReadyWithKubectl(m.kubectl, registryCertificateName, NamespaceRegistry, timeout); err != nil {
		err := newWithSentinel(ErrCertificateNotReady, fmt.Sprintf("certificate not ready after %s. Check cert-manager logs: kubectl logs -n cert-manager deployment/cert-manager", timeout))
		Error("Certificate not ready")
		logStructuredError(m.logger, err, "Certificate not ready")
		return err
	}
	Success("Certificate issued successfully")
	return nil
}

func checkCertManagerInstalledWithKubectl(kubectl KubectlRunner) error {
	// #nosec G204 -- fixed kubectl command to check CRD.
	if err := kubectl.Run([]string{"get", "crd", CertManagerCRDName}); err != nil {
		return ErrCertManagerNotInstalled
	}
	return nil
}

func checkCASecretWithKubectl(kubectl KubectlRunner) error {
	// #nosec G204 -- fixed kubectl command to check secret.
	if err := kubectl.Run([]string{"get", "secret", certCASecretName, "-n", certManagerNamespace}); err != nil {
		return ErrCASecretNotFound
	}
	return nil
}

func checkClusterIssuerWithKubectl(kubectl KubectlRunner) error {
	// #nosec G204 -- fixed kubectl command to check ClusterIssuer.
	if err := kubectl.Run([]string{"get", "clusterissuer", certClusterIssuerName}); err != nil {
		return wrapWithSentinel(ErrClusterIssuerNotFound, err, fmt.Sprintf("ClusterIssuer %q not found: %v", certClusterIssuerName, err))
	}
	return nil
}

func checkCertificateWithKubectl(kubectl KubectlRunner, name, namespace string) error {
	// #nosec G204 -- fixed kubectl command to check certificate.
	if err := kubectl.Run([]string{"get", "certificate", name, "-n", namespace}); err != nil {
		return wrapWithSentinel(ErrRegistryCertificateNotFound, err, fmt.Sprintf("Certificate %q not found in namespace %q: %v", name, namespace, err))
	}
	return nil
}

func applyClusterIssuerWithKubectl(kubectl KubectlRunner) error {
	// #nosec G204 -- fixed file path from repository.
	return kubectl.RunWithOutput([]string{"apply", "-f", clusterIssuerManifestPath}, os.Stdout, os.Stderr)
}

func applyRegistryCertificateWithKubectl(kubectl KubectlRunner) error {
	// #nosec G204 -- fixed file path from repository.
	return kubectl.RunWithOutput([]string{"apply", "-f", registryCertificateManifestPath}, os.Stdout, os.Stderr)
}

func waitForCertificateReadyWithKubectl(kubectl KubectlRunner, name, namespace string, timeout time.Duration) error {
	// #nosec G204 -- command arguments are built from trusted inputs and fixed verbs.
	return kubectl.RunWithOutput([]string{
		"wait", "--for=condition=Ready",
		"certificate/" + name, "-n", namespace,
		fmt.Sprintf("--timeout=%s", timeout),
	}, os.Stdout, os.Stderr)
}
