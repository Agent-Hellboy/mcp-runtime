package cli

import (
	"fmt"
	"os"
	"strings"
	"time"

	"go.uber.org/zap"
)

const (
	// certManagerRelease is pinned for reproducible installs (kubectl apply).
	certManagerRelease           = "v1.16.2"
	letsencryptProdURL           = "https://acme-v02.api.letsencrypt.org/directory"
	letsencryptStagingURL        = "https://acme-staging-v02.api.letsencrypt.org/directory"
	letsencryptProdIssuerName    = "letsencrypt-prod"
	letsencryptStagingIssuerName = "letsencrypt-staging"
)

func certManagerInstallManifestURL() string {
	return fmt.Sprintf("https://github.com/cert-manager/cert-manager/releases/download/%s/cert-manager.yaml", certManagerRelease)
}

// ClusterIssuerNameForACME returns the ClusterIssuer resource name for Let's Encrypt.
func ClusterIssuerNameForACME(staging bool) string {
	if staging {
		return letsencryptStagingIssuerName
	}
	return letsencryptProdIssuerName
}

func acmeServerURL(staging bool) string {
	if staging {
		return letsencryptStagingURL
	}
	return letsencryptProdURL
}

func validateACMEHostnameForPublicCA() error {
	host := strings.TrimSpace(GetRegistryIngressHost())
	if host == "" || strings.EqualFold(host, "registry.local") || strings.HasSuffix(strings.ToLower(host), ".local") {
		return fmt.Errorf("ACME public CA requires a public DNS name; set MCP_REGISTRY_HOST (or MCP_REGISTRY_INGRESS_HOST) to a hostname that resolves to this cluster (not %q)", host)
	}
	return nil
}

// ensureCertManagerInstalled applies upstream cert-manager if CRDs are missing and waits for deployments.
func ensureCertManagerInstalled(kubectl KubectlRunner, logger *zap.Logger) error {
	if err := checkCertManagerInstalledWithKubectl(kubectl); err == nil {
		Info("cert-manager already installed")
		return nil
	}
	Info(fmt.Sprintf("Installing cert-manager %s", certManagerRelease))
	warnMsg := "If this fails (no network), install cert-manager manually, then re-run setup with --skip-cert-manager-install"
	Warn(warnMsg)
	url := certManagerInstallManifestURL()
	// #nosec G204 -- fixed release URL.
	if err := kubectl.RunWithOutput([]string{"apply", "-f", url}, os.Stdout, os.Stderr); err != nil {
		wrapped := wrapWithSentinel(ErrCertManagerInstallFailed, err, fmt.Sprintf("cert-manager install failed: %v. %s", err, warnMsg))
		Error("cert-manager install failed")
		if logger != nil {
			logStructuredError(logger, wrapped, "cert-manager install failed")
		}
		return wrapped
	}
	deadline := 5 * time.Minute
	Info(fmt.Sprintf("Waiting for cert-manager deployments (timeout %s)", deadline))
	for _, dep := range []string{"cert-manager", "cert-manager-cainjector", "cert-manager-webhook"} {
		// #nosec G204 -- fixed deployment name.
		if err := kubectl.RunWithOutput([]string{
			"wait", "--for=condition=Available",
			"deployment/" + dep, "-n", certManagerNamespace,
			"--timeout=" + deadline.String(),
		}, os.Stdout, os.Stderr); err != nil {
			wrapped := wrapWithSentinel(ErrCertManagerInstallFailed, err, fmt.Sprintf("cert-manager component %s not ready: %v", dep, err))
			Error("cert-manager did not become ready")
			if logger != nil {
				logStructuredError(logger, wrapped, "cert-manager did not become ready")
			}
			return wrapped
		}
	}
	Info("cert-manager is ready")
	return nil
}

func applyLetsEncryptClusterIssuer(kubectl KubectlRunner, email string, staging bool, logger *zap.Logger) error {
	email = strings.TrimSpace(email)
	if email == "" {
		return fmt.Errorf("ACME email is required")
	}
	name := ClusterIssuerNameForACME(staging)
	manifest := renderLetsEncryptClusterIssuerManifest(name, email, acmeServerURL(staging))
	if err := applyManifestContent(kubectl, manifest); err != nil {
		wrapped := wrapWithSentinel(ErrClusterIssuerApplyFailed, err, fmt.Sprintf("failed to apply Let's Encrypt ClusterIssuer: %v", err))
		Error("Failed to apply ClusterIssuer")
		if logger != nil {
			logStructuredError(logger, wrapped, "Failed to apply ClusterIssuer")
		}
		return wrapped
	}
	return nil
}

func renderLetsEncryptClusterIssuerManifest(name, email, serverURL string) string {
	var b strings.Builder
	b.WriteString("apiVersion: cert-manager.io/v1\n")
	b.WriteString("kind: ClusterIssuer\n")
	b.WriteString("metadata:\n")
	b.WriteString("  name: ")
	b.WriteString(name)
	b.WriteString("\n")
	b.WriteString("spec:\n")
	b.WriteString("  acme:\n")
	b.WriteString("    email: ")
	b.WriteString(email)
	b.WriteString("\n")
	b.WriteString("    server: ")
	b.WriteString(serverURL)
	b.WriteString("\n")
	b.WriteString("    privateKeySecretRef:\n")
	b.WriteString("      name: ")
	b.WriteString(name)
	b.WriteString("-account-key\n")
	b.WriteString("    solvers:\n")
	b.WriteString("      - http01:\n")
	b.WriteString("          ingress:\n")
	b.WriteString("            ingressClassName: traefik\n")
	return b.String()
}

func applyRegistryCertificateForACME(kubectl KubectlRunner, dnsName, issuerName string) error {
	dnsName = strings.TrimSpace(dnsName)
	if dnsName == "" {
		return fmt.Errorf("registry DNS name is empty")
	}
	manifest := renderRegistryCertificateForACME(registryCertificateName, dnsName, issuerName)
	return applyManifestContent(kubectl, manifest)
}

func renderRegistryCertificateForACME(certName, dnsName, issuerName string) string {
	var b strings.Builder
	b.WriteString("apiVersion: cert-manager.io/v1\n")
	b.WriteString("kind: Certificate\n")
	b.WriteString("metadata:\n")
	b.WriteString("  name: ")
	b.WriteString(certName)
	b.WriteString("\n")
	b.WriteString("  namespace: ")
	b.WriteString(NamespaceRegistry)
	b.WriteString("\n")
	b.WriteString("spec:\n")
	b.WriteString("  secretName: registry-tls\n")
	b.WriteString("  issuerRef:\n")
	b.WriteString("    name: ")
	b.WriteString(issuerName)
	b.WriteString("\n")
	b.WriteString("    kind: ClusterIssuer\n")
	b.WriteString("  dnsNames:\n")
	b.WriteString("    - ")
	b.WriteString(dnsName)
	b.WriteString("\n")
	return b.String()
}
