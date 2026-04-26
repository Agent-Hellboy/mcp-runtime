package cli

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
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
	// acmeHTTP01DevIngressOverlay is the kustomize overlay that binds Traefik to 8000/8443 for local port-forwards, not public :80.
	acmeHTTP01DevIngressOverlay = "config/ingress/overlays/http"
	traefikManagedNamespace     = "traefik"
	traefikManagedDeployment    = "traefik"
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

func acmeTLSDNSNames() []string {
	seen := make(map[string]struct{})
	var out []string
	for _, h := range []string{GetRegistryIngressHost(), GetMcpIngressHost()} {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		out = append(out, h)
	}
	return out
}

func validateACMEHostnameForPublicCA() error {
	names := acmeTLSDNSNames()
	if len(names) == 0 {
		return fmt.Errorf("ACME public CA requires a public DNS name; set MCP_PLATFORM_DOMAIN, MCP_REGISTRY_HOST, or MCP_REGISTRY_INGRESS_HOST")
	}
	for _, host := range names {
		if isDevRegistryURL(host) {
			return fmt.Errorf("ACME public CA requires a public DNS name; set MCP_PLATFORM_DOMAIN (e.g. mcpruntime.com for registry. and mcp. names) or MCP_REGISTRY_INGRESS_HOST, not %q", host)
		}
	}
	return nil
}

// validateIngressManifestForACME rejects the dev "http" overlay, which does not listen on 80/443, so Let’s Encrypt HTTP-01 cannot work.
func validateIngressManifestForACME(ingressManifest string) error {
	m := strings.TrimSpace(ingressManifest)
	if m == "" {
		return nil
	}
	if filepath.Base(filepath.Clean(m)) == "http" {
		return fmt.Errorf(
			"http-01 (Let's Encrypt) must reach your hostnames on port 80, but the %q overlay uses 8000/8443. Omit --ingress-manifest so setup uses the prod overlay, or set --ingress-manifest %q, then re-run (use --force-ingress-install if an old ingress is already present)",
			acmeHTTP01DevIngressOverlay, "config/ingress/overlays/prod",
		)
	}
	return nil
}

// waitForTraefikDeploymentForACME waits for the Traefik this repo installs in namespace "traefik". If it is missing (e.g. skipped install, or another cluster ingress), a warning is printed and we continue.
func waitForTraefikDeploymentForACME(kubectl KubectlRunner) error {
	if err := kubectl.RunWithOutput(
		[]string{"get", "deployment", traefikManagedDeployment, "-n", traefikManagedNamespace},
		io.Discard, io.Discard,
	); err != nil {
		Warn("No " + traefikManagedNamespace + "/" + traefikManagedDeployment + " deployment found; skipping Traefik wait. cert-manager still needs the Traefik ingress class to serve HTTP-01, with port 80 on your public hostnames")
		return nil
	}
	Info("Waiting for " + traefikManagedNamespace + "/" + traefikManagedDeployment + " (ingress must be up before the ACME request)")
	// #nosec G204 -- fixed resource names; timeout is fixed.
	if err := kubectl.RunWithOutput([]string{
		"wait", "--for=condition=Available",
		"deployment/" + traefikManagedDeployment, "-n", traefikManagedNamespace, "--timeout=3m",
	}, os.Stdout, os.Stderr); err != nil {
		return fmt.Errorf("traefik not ready: %w", err)
	}
	Info(traefikManagedNamespace + "/" + traefikManagedDeployment + " is available")
	return nil
}

// preflightACMEHostnamesPort80 tries TCP dials to host:80 from the machine running setup. Failing does not block setup (Operator may be off-node); success helps confirm DNS and a listener before a long cert wait.
func preflightACMEHostnamesPort80(dnsNames []string) {
	for _, h := range dedupeHostnames(dnsNames) {
		if h == "" {
			continue
		}
		addr := net.JoinHostPort(h, "80")
		c, err := net.DialTimeout("tcp", addr, 5*time.Second)
		if err != nil {
			Warn("From this host, could not open TCP to " + addr + " (" + err.Error() + "). Let's Encrypt will try from the public internet, so check DNS, firewall, and that Traefik listens on port 80. If the cluster is on another network, you can ignore this if port 80 is open publicly")
			continue
		}
		_ = c.Close()
		Info("TCP to " + addr + " succeeded from this host (a good sign for HTTP-01)")
	}
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
	overall := 5 * time.Minute
	start := time.Now()
	Info(fmt.Sprintf("Waiting for cert-manager deployments (combined timeout %s across three deployments)", overall))
	for _, dep := range []string{"cert-manager", "cert-manager-cainjector", "cert-manager-webhook"} {
		remaining := time.Until(start.Add(overall))
		if remaining <= 0 {
			err := fmt.Errorf("timed out waiting for cert-manager before deployment/%s", dep)
			wrapped := wrapWithSentinel(ErrCertManagerInstallFailed, err, err.Error())
			Error("cert-manager did not become ready")
			if logger != nil {
				logStructuredError(logger, wrapped, "cert-manager did not become ready")
			}
			return wrapped
		}
		// #nosec G204 -- fixed deployment name; timeout is remaining wall-clock budget.
		if err := kubectl.RunWithOutput([]string{
			"wait", "--for=condition=Available",
			"deployment/" + dep, "-n", certManagerNamespace,
			"--timeout=" + remaining.Round(time.Second).String(),
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
	b.WriteString(strconv.Quote(email))
	b.WriteString("\n")
	b.WriteString("    server: ")
	b.WriteString(strconv.Quote(serverURL))
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

func applyRegistryCertificateForACME(kubectl KubectlRunner, dnsNames []string, issuerName string) error {
	uniq := dedupeHostnames(dnsNames)
	if len(uniq) == 0 {
		return fmt.Errorf("registry TLS has no DNS names to request")
	}
	manifest := renderRegistryCertificateForACME(registryCertificateName, uniq, issuerName)
	return applyManifestContent(kubectl, manifest)
}

func dedupeHostnames(hs []string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, h := range hs {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		out = append(out, h)
	}
	return out
}

func renderRegistryCertificateForACME(certName string, dnsNames []string, issuerName string) string {
	uniq := dedupeHostnames(dnsNames)
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
	for _, name := range uniq {
		b.WriteString("    - ")
		b.WriteString(strconv.Quote(name))
		b.WriteString("\n")
	}
	return b.String()
}
