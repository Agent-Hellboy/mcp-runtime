package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

const (
	certManagerNamespace            = "cert-manager"
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
		return fmt.Errorf("cert-manager not installed. Install it first:\n  helm install cert-manager jetstack/cert-manager --namespace cert-manager --create-namespace --set crds.enabled=true")
	}
	Info("Checking CA secret")
	if err := checkCASecretWithKubectl(m.kubectl); err != nil {
		return fmt.Errorf("CA secret %q not found in cert-manager namespace. Create it first:\n  kubectl create secret tls %s --cert=ca.crt --key=ca.key -n %s", certCASecretName, certCASecretName, certManagerNamespace)
	}
	Info("Checking ClusterIssuer")
	if err := checkClusterIssuerWithKubectl(m.kubectl); err != nil {
		return fmt.Errorf("ClusterIssuer %q not found. Apply it first:\n  kubectl apply -f %s", certClusterIssuerName, clusterIssuerManifestPath)
	}
	Info("Checking registry Certificate")
	if err := checkCertificateWithKubectl(m.kubectl, registryCertificateName, NamespaceRegistry); err != nil {
		return fmt.Errorf("registry Certificate not found. Apply it first:\n  kubectl apply -f %s", registryCertificateManifestPath)
	}
	Success("Cert-manager resources are present")
	return nil
}

// Apply installs cert-manager resources required for registry TLS.
func (m *CertManager) Apply() error {
	Info("Checking cert-manager installation")
	if err := checkCertManagerInstalledWithKubectl(m.kubectl); err != nil {
		return fmt.Errorf("cert-manager not installed. Install it first:\n  helm install cert-manager jetstack/cert-manager --namespace cert-manager --create-namespace --set crds.enabled=true")
	}
	Info("Checking CA secret")
	if err := checkCASecretWithKubectl(m.kubectl); err != nil {
		return fmt.Errorf("CA secret %q not found in cert-manager namespace. Create it first:\n  kubectl create secret tls %s --cert=ca.crt --key=ca.key -n %s", certCASecretName, certCASecretName, certManagerNamespace)
	}

	Info("Applying ClusterIssuer")
	if err := applyClusterIssuerWithKubectl(m.kubectl); err != nil {
		return fmt.Errorf("failed to apply ClusterIssuer: %w", err)
	}
	if err := ensureNamespace(NamespaceRegistry); err != nil {
		return fmt.Errorf("failed to create registry namespace: %w", err)
	}
	Info("Applying Certificate for registry")
	if err := applyRegistryCertificateWithKubectl(m.kubectl); err != nil {
		return fmt.Errorf("failed to apply Certificate: %w", err)
	}

	Success("Cert-manager resources applied")
	return nil
}

// Wait blocks until the registry certificate is Ready or times out.
func (m *CertManager) Wait(timeout time.Duration) error {
	Info(fmt.Sprintf("Waiting for certificate to be issued (timeout: %s)", timeout))
	if err := waitForCertificateReadyWithKubectl(m.kubectl, registryCertificateName, NamespaceRegistry, timeout); err != nil {
		return fmt.Errorf("certificate not ready after %s. Check cert-manager logs: kubectl logs -n cert-manager deployment/cert-manager", timeout)
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
		return err
	}
	return nil
}

func checkCertificateWithKubectl(kubectl KubectlRunner, name, namespace string) error {
	// #nosec G204 -- fixed kubectl command to check certificate.
	if err := kubectl.Run([]string{"get", "certificate", name, "-n", namespace}); err != nil {
		return err
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
