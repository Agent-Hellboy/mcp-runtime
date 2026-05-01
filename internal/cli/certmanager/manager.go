package certmanager

// This file implements certificate and TLS management functionality.
// It handles cert-manager integration, CA secret management, and certificate provisioning.

import (
	"fmt"
	"os"
	"strings"
	"time"

	"go.uber.org/zap"

	"mcp-runtime/internal/cli/core"
	"mcp-runtime/internal/cli/kube"
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

const (
	CertClusterIssuerName   = certClusterIssuerName
	RegistryCertificateName = registryCertificateName
)

// CertManager manages cert-manager resources for the platform.
type CertManager struct {
	kubectl core.KubectlRunner
	logger  *zap.Logger
}

// NewCertManager creates a CertManager with the given dependencies.
func NewCertManager(kubectl core.KubectlRunner, logger *zap.Logger) *CertManager {
	return &CertManager{kubectl: kubectl, logger: logger}
}

// Status verifies cert-manager installation and required resources.
func (m *CertManager) Status() error {
	core.Info("Checking cert-manager installation")
	if err := checkCertManagerInstalledWithKubectl(m.kubectl); err != nil {
		err := core.WrapWithSentinel(core.ErrCertManagerNotInstalled, err, "cert-manager not installed. Install it first:\n  helm install cert-manager jetstack/cert-manager --namespace cert-manager --create-namespace --set crds.enabled=true")
		core.Error("Cert-manager not installed")
		core.LogStructuredError(m.logger, err, "Cert-manager not installed")
		return err
	}
	core.Info("Checking CA secret")
	if err := checkCASecretWithKubectl(m.kubectl); err != nil {
		err := core.NewWithSentinel(core.ErrCASecretNotFound, fmt.Sprintf("CA secret %q not found in cert-manager namespace. Create it first:\n  kubectl create secret tls %s --cert=ca.crt --key=ca.key -n %s", certCASecretName, certCASecretName, certManagerNamespace))
		core.Error("CA secret not found")
		core.LogStructuredError(m.logger, err, "CA secret not found")
		return err
	}
	core.Info("Checking ClusterIssuer")
	if err := checkClusterIssuerWithKubectl(m.kubectl); err != nil {
		err := core.NewWithSentinel(core.ErrClusterIssuerNotFound, fmt.Sprintf("ClusterIssuer %q not found. Apply it first:\n  kubectl apply -f %s", certClusterIssuerName, clusterIssuerManifestPath))
		core.Error("ClusterIssuer not found")
		core.LogStructuredError(m.logger, err, "ClusterIssuer not found")
		return err
	}
	core.Info("Checking registry Certificate")
	if err := checkCertificateWithKubectl(m.kubectl, registryCertificateName, core.NamespaceRegistry); err != nil {
		err := core.NewWithSentinel(core.ErrRegistryCertificateNotFound, fmt.Sprintf("registry Certificate not found. Apply it first:\n  kubectl apply -f %s", registryCertificateManifestPath))
		core.Error("Registry Certificate not found")
		core.LogStructuredError(m.logger, err, "Registry Certificate not found")
		return err
	}
	core.Success("Cert-manager resources are present")
	return nil
}

// Apply installs cert-manager resources required for registry TLS. When dryRun
// is true, the read-only preflight checks still run (to catch obvious problems
// like missing cert-manager) but no kubectl apply is performed.
func (m *CertManager) Apply(dryRun bool) error {
	core.Info("Checking cert-manager installation")
	if err := checkCertManagerInstalledWithKubectl(m.kubectl); err != nil {
		err := core.WrapWithSentinel(core.ErrCertManagerNotInstalled, err, "cert-manager not installed. Install it first:\n  helm install cert-manager jetstack/cert-manager --namespace cert-manager --create-namespace --set crds.enabled=true")
		core.Error("Cert-manager not installed")
		core.LogStructuredError(m.logger, err, "Cert-manager not installed")
		return err
	}
	core.Info("Checking CA secret")
	if err := checkCASecretWithKubectl(m.kubectl); err != nil {
		err := core.NewWithSentinel(core.ErrCASecretNotFound, fmt.Sprintf("CA secret %q not found in cert-manager namespace. Create it first:\n  kubectl create secret tls %s --cert=ca.crt --key=ca.key -n %s", certCASecretName, certCASecretName, certManagerNamespace))
		core.Error("CA secret not found")
		core.LogStructuredError(m.logger, err, "CA secret not found")
		return err
	}

	if dryRun {
		core.Info("[dry-run] would apply ClusterIssuer")
		core.Info(fmt.Sprintf("[dry-run] would ensure namespace %q exists", core.NamespaceRegistry))
		core.Info(fmt.Sprintf("[dry-run] would apply Certificate %q in namespace %q", registryCertificateName, core.NamespaceRegistry))
		core.Success("Dry-run complete; no resources applied")
		return nil
	}

	core.Info("Applying ClusterIssuer")
	if err := applyClusterIssuerWithKubectl(m.kubectl); err != nil {
		wrappedErr := core.WrapWithSentinel(core.ErrClusterIssuerApplyFailed, err, fmt.Sprintf("failed to apply ClusterIssuer: %v", err))
		core.Error("Failed to apply ClusterIssuer")
		core.LogStructuredError(m.logger, wrappedErr, "Failed to apply ClusterIssuer")
		return wrappedErr
	}
	if err := kube.EnsureNamespace(m.kubectl.CommandArgs, core.NamespaceRegistry); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrCreateRegistryNamespaceFailed,
			err,
			fmt.Sprintf("failed to create registry namespace: %v", err),
			map[string]any{"namespace": core.NamespaceRegistry, "component": "cert"},
		)
		core.Error("Failed to create registry namespace")
		core.LogStructuredError(m.logger, wrappedErr, "Failed to create registry namespace")
		return wrappedErr
	}
	core.Info("Applying Certificate for registry")
	if err := applyRegistryCertificateWithKubectl(m.kubectl); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrApplyCertificateFailed,
			err,
			fmt.Sprintf("failed to apply Certificate: %v", err),
			map[string]any{"certificate": registryCertificateName, "namespace": core.NamespaceRegistry, "component": "cert"},
		)
		core.Error("Failed to apply Certificate")
		core.LogStructuredError(m.logger, wrappedErr, "Failed to apply Certificate")
		return wrappedErr
	}

	core.Success("Cert-manager resources applied")
	return nil
}

// Wait blocks until the registry certificate is Ready or times out.
func (m *CertManager) Wait(timeout time.Duration) error {
	core.Info(fmt.Sprintf("Waiting for certificate to be issued (timeout: %s)", timeout))
	if err := waitForCertificateReadyWithKubectl(m.kubectl, registryCertificateName, core.NamespaceRegistry, timeout); err != nil {
		err := core.NewWithSentinel(core.ErrCertificateNotReady, fmt.Sprintf("certificate not ready after %s. Check cert-manager logs: kubectl logs -n cert-manager deployment/cert-manager", timeout))
		core.Error("Certificate not ready")
		core.LogStructuredError(m.logger, err, "Certificate not ready")
		return err
	}
	core.Success("Certificate issued successfully")
	return nil
}

func checkCertManagerInstalledWithKubectl(kubectl core.KubectlRunner) error {
	// #nosec G204 -- fixed kubectl command to check CRD.
	if err := kubectl.Run([]string{"get", "crd", core.CertManagerCRDName}); err != nil {
		return core.ErrCertManagerNotInstalled
	}
	return nil
}

func CheckCertManagerInstalledWithKubectl(kubectl core.KubectlRunner) error {
	return checkCertManagerInstalledWithKubectl(kubectl)
}

func checkCASecretWithKubectl(kubectl core.KubectlRunner) error {
	// #nosec G204 -- fixed kubectl command to check secret.
	if err := kubectl.Run([]string{"get", "secret", certCASecretName, "-n", certManagerNamespace}); err != nil {
		return core.ErrCASecretNotFound
	}
	return nil
}

func CheckCASecretWithKubectl(kubectl core.KubectlRunner) error {
	return checkCASecretWithKubectl(kubectl)
}

func checkClusterIssuerWithKubectl(kubectl core.KubectlRunner) error {
	// #nosec G204 -- fixed kubectl command to check ClusterIssuer.
	if err := kubectl.Run([]string{"get", "clusterissuer", certClusterIssuerName}); err != nil {
		return core.WrapWithSentinel(core.ErrClusterIssuerNotFound, err, fmt.Sprintf("ClusterIssuer %q not found: %v", certClusterIssuerName, err))
	}
	return nil
}

func CheckClusterIssuerWithKubectl(kubectl core.KubectlRunner) error {
	return checkClusterIssuerWithKubectl(kubectl)
}

// checkNamedClusterIssuerWithKubectl verifies a cert-manager ClusterIssuer exists
// (e.g. a company-managed CA; setup does not apply it).
func checkNamedClusterIssuerWithKubectl(kubectl core.KubectlRunner, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return core.NewWithSentinel(core.ErrClusterIssuerNotFound, "ClusterIssuer name is empty (set --tls-cluster-issuer or MCP_TLS_CLUSTER_ISSUER)")
	}
	// #nosec G204 -- issuer name is validated, fixed kubectl subresource.
	if err := kubectl.Run([]string{"get", "clusterissuer", name}); err != nil {
		return core.WrapWithSentinel(core.ErrClusterIssuerNotFound, err, fmt.Sprintf("ClusterIssuer %q not found. Install your org issuer first (cert-manager) or fix --tls-cluster-issuer / MCP_TLS_CLUSTER_ISSUER: %v", name, err))
	}
	return nil
}

func CheckNamedClusterIssuerWithKubectl(kubectl core.KubectlRunner, name string) error {
	return checkNamedClusterIssuerWithKubectl(kubectl, name)
}

func checkCertificateWithKubectl(kubectl core.KubectlRunner, name, namespace string) error {
	// #nosec G204 -- fixed kubectl command to check certificate.
	if err := kubectl.Run([]string{"get", "certificate", name, "-n", namespace}); err != nil {
		return core.WrapWithSentinel(core.ErrRegistryCertificateNotFound, err, fmt.Sprintf("Certificate %q not found in namespace %q: %v", name, namespace, err))
	}
	return nil
}

func CheckCertificateWithKubectl(kubectl core.KubectlRunner, name, namespace string) error {
	return checkCertificateWithKubectl(kubectl, name, namespace)
}

func applyClusterIssuerWithKubectl(kubectl core.KubectlRunner) error {
	// #nosec G204 -- fixed file path from repository.
	return kubectl.RunWithOutput([]string{"apply", "-f", clusterIssuerManifestPath}, os.Stdout, os.Stderr)
}

func ApplyClusterIssuerWithKubectl(kubectl core.KubectlRunner) error {
	return applyClusterIssuerWithKubectl(kubectl)
}

func applyRegistryCertificateWithKubectl(kubectl core.KubectlRunner) error {
	content, err := os.ReadFile(registryCertificateManifestPath)
	if err != nil {
		return err
	}
	manifest := rewriteRegistryHost(string(content), core.GetRegistryIngressHost())
	return kube.ApplyManifestContentWithNamespace(kubectl.CommandArgs, manifest, core.NamespaceRegistry)
}

func ApplyRegistryCertificateWithKubectl(kubectl core.KubectlRunner) error {
	return applyRegistryCertificateWithKubectl(kubectl)
}

func rewriteRegistryHost(manifest, host string) string {
	host = strings.TrimSpace(host)
	if host == "" || host == "registry.local" {
		return manifest
	}
	return strings.ReplaceAll(manifest, "registry.local", host)
}

func waitForCertificateReadyWithKubectl(kubectl core.KubectlRunner, name, namespace string, timeout time.Duration) error {
	// #nosec G204 -- command arguments are built from trusted inputs and fixed verbs.
	return kubectl.RunWithOutput([]string{
		"wait", "--for=condition=Ready",
		"certificate/" + name, "-n", namespace,
		fmt.Sprintf("--timeout=%s", timeout),
	}, os.Stdout, os.Stderr)
}

func WaitForCertificateReadyWithKubectl(kubectl core.KubectlRunner, name, namespace string, timeout time.Duration) error {
	return waitForCertificateReadyWithKubectl(kubectl, name, namespace, timeout)
}
