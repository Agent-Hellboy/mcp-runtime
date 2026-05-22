package platform

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"go.uber.org/zap"

	"mcp-runtime/internal/cli/certmanager"
	"mcp-runtime/internal/cli/core"
	"mcp-runtime/internal/cli/kube"
	setupplan "mcp-runtime/internal/cli/setup/plan"
)

// validateTLSSetupCLIFlags enforces ACME / internal-issuer mutual exclusion and
// requires --with-tls when any TLS or cert-manager-related options are set.
func ValidateTLSSetupCLIFlags(
	tlsEnabled bool,
	acmeEmailResolved, tlsCIResolved string,
	acmeStagingResolved, skipCertManagerInstall bool,
) error {
	if acmeEmailResolved != "" && tlsCIResolved != "" {
		return core.NewWithSentinel(core.ErrFieldRequired, "use either --acme-email (or MCP_ACME_EMAIL) for public Let's Encrypt, or --tls-cluster-issuer (or MCP_TLS_CLUSTER_ISSUER) for an existing internal ClusterIssuer, not both")
	}
	if !tlsEnabled && (tlsCIResolved != "" || acmeEmailResolved != "" || acmeStagingResolved || skipCertManagerInstall) {
		return core.NewWithSentinel(core.ErrFieldRequired, "--with-tls is required when using --acme-email, --tls-cluster-issuer, --acme-staging, --skip-cert-manager-install, or related environment variables (MCP_ACME_EMAIL, MCP_ACME_STAGING, MCP_TLS_CLUSTER_ISSUER)")
	}
	return nil
}

func setupTLSStep(logger *zap.Logger, plan setupplan.Plan, deps SetupDeps) error {
	// Step 3: Configure TLS (if enabled)
	core.Step("Step 3: Configure TLS")
	if !plan.TLSEnabled {
		core.Info("Skipped (TLS disabled, use --with-tls to enable)")
		return nil
	}
	if err := deps.SetupTLS(logger, plan); err != nil {
		wrappedErr := core.WrapWithSentinel(core.ErrTLSSetupFailed, err, fmt.Sprintf("TLS setup failed: %v", err))
		core.Error("TLS setup failed")
		core.LogStructuredError(logger, wrappedErr, "TLS setup failed")
		return wrappedErr
	}
	core.Success("TLS configured successfully")
	return nil
}

// setupTLSWithKubectlAndPlan provisions TLS: Let's Encrypt when plan.ACMEmail is set, an existing
// ClusterIssuer when plan.TLSClusterIssuer is set, otherwise the bundled private CA (mcp-runtime-ca).
func setupTLSWithKubectlAndPlan(kubectl core.KubectlRunner, logger *zap.Logger, plan setupplan.Plan) error {
	if strings.TrimSpace(plan.ACMEmail) != "" {
		return setupTLSLetsEncrypt(kubectl, logger, plan)
	}
	if strings.TrimSpace(plan.TLSClusterIssuer) != "" {
		return setupTLSWithExistingClusterIssuer(kubectl, logger, plan)
	}
	return setupTLSPrivateCA(kubectl, logger, plan)
}

func registryCertificateSANs(plan setupplan.Plan) ([]string, []string) {
	dnsNames := append([]string{}, certmanager.ACMETLSDNSNames()...)
	return dedupeStrings(dnsNames), nil
}

func registryInternalCertificateSANs(plan setupplan.Plan) ([]string, []string) {
	var dnsNames []string
	var ipAddresses []string
	if plan.RegistryMode == setupplan.RegistryModeBundledHTTPS {
		dnsNames = append(dnsNames,
			core.DefaultRegistryIngressHost,
			"registry.registry.svc",
			"registry.registry.svc.cluster.local",
		)
		addRegistrySAN(strings.TrimSpace(core.GetRegistryEndpoint()), &dnsNames, &ipAddresses)
	}
	return dedupeStrings(dnsNames), dedupeStrings(ipAddresses)
}

func bundledRegistryInternalIssuerName(kubectl core.KubectlRunner, plan setupplan.Plan) (string, error) {
	issuerName := strings.TrimSpace(plan.TLSClusterIssuer)
	if issuerName == "" {
		return certmanager.CertClusterIssuerName, nil
	}
	usesACME, err := clusterIssuerUsesACME(kubectl, issuerName)
	if err != nil {
		return "", err
	}
	if usesACME {
		core.Warn(fmt.Sprintf("ClusterIssuer %q uses ACME and cannot issue internal registry DNS names; using %s for registry-internal-tls", issuerName, certmanager.CertClusterIssuerName))
		return certmanager.CertClusterIssuerName, nil
	}
	return issuerName, nil
}

func clusterIssuerUsesACME(kubectl core.KubectlRunner, name string) (bool, error) {
	if kubectl == nil {
		return false, fmt.Errorf("kubectl runner is nil")
	}
	cmd, err := kubectl.CommandArgs([]string{"get", "clusterissuer", name, "-o", "jsonpath={.spec.acme.server}"})
	if err != nil {
		return false, err
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		if detail := strings.TrimSpace(string(out)); detail != "" {
			return false, fmt.Errorf("inspect ClusterIssuer %q: %w: %s", name, err, detail)
		}
		return false, err
	}
	return strings.TrimSpace(string(out)) != "", nil
}

func addRegistrySAN(raw string, dnsNames, ipAddresses *[]string) {
	host := registryEndpointHost(raw)
	if host == "" {
		return
	}
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil {
		*ipAddresses = append(*ipAddresses, ip.String())
		return
	}
	*dnsNames = append(*dnsNames, host)
}

func registryEndpointHost(raw string) string {
	trimmed := strings.TrimSpace(strings.TrimSuffix(raw, "/"))
	if trimmed == "" {
		return ""
	}
	if strings.Contains(trimmed, "://") {
		if parsed, err := url.Parse(trimmed); err == nil && parsed.Host != "" {
			trimmed = parsed.Host
		}
	}
	if slash := strings.Index(trimmed, "/"); slash >= 0 {
		trimmed = trimmed[:slash]
	}
	if host, _, err := net.SplitHostPort(trimmed); err == nil {
		return strings.Trim(host, "[]")
	}
	if idx := strings.LastIndex(trimmed, ":"); idx >= 0 && strings.Count(trimmed, ":") == 1 {
		return strings.Trim(trimmed[:idx], "[]")
	}
	return strings.Trim(trimmed, "[]")
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func setupTLSLetsEncrypt(kubectl core.KubectlRunner, logger *zap.Logger, plan setupplan.Plan) error {
	core.Info("Configuring TLS with Let's Encrypt (cert-manager HTTP-01)")
	if err := certmanager.ValidateACMEHostnameForPublicCA(); err != nil {
		wrappedErr := core.WrapWithSentinel(core.ErrTLSSetupFailed, err, err.Error())
		core.Error("Invalid configuration for Let's Encrypt")
		if logger != nil {
			core.LogStructuredError(logger, wrappedErr, "Invalid configuration for Let's Encrypt")
		}
		return wrappedErr
	}
	if err := certmanager.ValidateIngressManifestForACME(plan.Ingress.Manifest); err != nil {
		wrappedErr := core.WrapWithSentinel(core.ErrTLSSetupFailed, err, err.Error())
		core.Error("Ingress configuration blocks Let's Encrypt")
		if logger != nil {
			core.LogStructuredError(logger, wrappedErr, "Ingress configuration blocks Let's Encrypt")
		}
		return wrappedErr
	}
	if plan.InstallCertManager {
		if err := certmanager.EnsureCertManagerInstalled(kubectl, logger); err != nil {
			return err
		}
	} else {
		core.Info("Checking cert-manager installation (--skip-cert-manager-install)")
		if err := certmanager.CheckCertManagerInstalledWithKubectl(kubectl); err != nil {
			err := core.WrapWithSentinel(core.ErrCertManagerNotInstalled, err, "cert-manager not installed. Install it, or omit --skip-cert-manager-install to let setup apply it from upstream")
			core.Error("Cert-manager not installed")
			if logger != nil {
				core.LogStructuredError(logger, err, "Cert-manager not installed")
			}
			return err
		}
		core.Info("cert-manager CRDs found")
	}
	if err := certmanager.WaitForTraefikDeploymentForACME(kubectl); err != nil {
		wrappedErr := core.WrapWithSentinel(core.ErrTLSSetupFailed, err, err.Error())
		core.Error("Traefik is not ready for HTTP-01")
		if logger != nil {
			core.LogStructuredError(logger, wrappedErr, "Traefik is not ready for HTTP-01")
		}
		return wrappedErr
	}
	core.Info("Checking TCP connectivity to your ACME hostnames on port 80 (best effort from this machine)")
	certmanager.PreflightACMEHostnamesPort80(certmanager.ACMETLSDNSNames())

	core.Info("Applying Let's Encrypt ClusterIssuer")
	if err := certmanager.ApplyLetsEncryptClusterIssuer(kubectl, plan.ACMEmail, plan.ACMEStaging, logger); err != nil {
		return err
	}

	if err := kube.EnsureNamespace(kubectl.CommandArgs, core.NamespaceRegistry); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrCreateRegistryNamespaceFailed,
			err,
			fmt.Sprintf("failed to create registry namespace: %v", err),
			map[string]any{"namespace": core.NamespaceRegistry, "component": "setup"},
		)
		core.Error("Failed to create registry namespace")
		if logger != nil {
			core.LogStructuredError(logger, wrappedErr, "Failed to create registry namespace")
		}
		return wrappedErr
	}
	if err := ensureRegistryCertificateOwnership(kubectl, logger); err != nil {
		return err
	}

	issuerName := certmanager.ClusterIssuerNameForACME(plan.ACMEStaging)
	dnsNames := certmanager.ACMETLSDNSNames()
	core.Info("Applying Certificate for registry (Let's Encrypt SANs)")
	if err := certmanager.ApplyRegistryCertificateForACME(kubectl, dnsNames, issuerName); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrApplyCertificateFailed,
			err,
			fmt.Sprintf("failed to apply Certificate: %v", err),
			map[string]any{"certificate": certmanager.RegistryCertificateName, "namespace": core.NamespaceRegistry, "component": "setup"},
		)
		core.Error("Failed to apply Certificate")
		if logger != nil {
			core.LogStructuredError(logger, wrappedErr, "Failed to apply Certificate")
		}
		return wrappedErr
	}

	certTimeout := core.GetCertTimeout()
	if certTimeout < 5*time.Minute {
		certTimeout = 5 * time.Minute
	}
	core.Info(fmt.Sprintf("Waiting for certificate to be issued (timeout: %s)", certTimeout))
	if err := certmanager.WaitForCertificateReadyWithKubectl(kubectl, certmanager.RegistryCertificateName, core.NamespaceRegistry, certTimeout); err != nil {
		err := core.NewWithSentinel(core.ErrCertificateNotReady, fmt.Sprintf("certificate not ready after %s. Check cert-manager logs: kubectl logs -n cert-manager deployment/cert-manager", certTimeout))
		core.Error("Certificate not ready")
		if logger != nil {
			core.LogStructuredError(logger, err, "Certificate not ready")
		}
		return err
	}
	core.Success("Certificate issued successfully")
	if err := setupBundledRegistryInternalTLSStep(kubectl, logger, plan); err != nil {
		return err
	}
	return nil
}

// setupTLSWithExistingClusterIssuer issues the registry (and optional mcp SAN) Certificate using a
// ClusterIssuer that already exists in the cluster (internal / enterprise CA).
func setupTLSWithExistingClusterIssuer(kubectl core.KubectlRunner, logger *zap.Logger, plan setupplan.Plan) error {
	issuerName := strings.TrimSpace(plan.TLSClusterIssuer)
	core.Info("Configuring TLS with existing ClusterIssuer: " + issuerName)
	if plan.InstallCertManager {
		if err := certmanager.EnsureCertManagerInstalled(kubectl, logger); err != nil {
			return err
		}
	} else {
		core.Info("Checking cert-manager installation (--skip-cert-manager-install)")
		if err := certmanager.CheckCertManagerInstalledWithKubectl(kubectl); err != nil {
			err := core.WrapWithSentinel(core.ErrCertManagerNotInstalled, err, "cert-manager not installed. Install it, or omit --skip-cert-manager-install to let setup apply it from upstream")
			core.Error("Cert-manager not installed")
			if logger != nil {
				core.LogStructuredError(logger, err, "Cert-manager not installed")
			}
			return err
		}
		core.Info("cert-manager CRDs found")
	}

	if err := certmanager.CheckNamedClusterIssuerWithKubectl(kubectl, issuerName); err != nil {
		core.Error("Cluster issuer not found")
		if logger != nil {
			core.LogStructuredError(logger, err, "Cluster issuer not found")
		}
		return err
	}

	if err := kube.EnsureNamespace(kubectl.CommandArgs, core.NamespaceRegistry); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrCreateRegistryNamespaceFailed,
			err,
			fmt.Sprintf("failed to create registry namespace: %v", err),
			map[string]any{"namespace": core.NamespaceRegistry, "component": "setup"},
		)
		core.Error("Failed to create registry namespace")
		if logger != nil {
			core.LogStructuredError(logger, wrappedErr, "Failed to create registry namespace")
		}
		return wrappedErr
	}
	if err := ensureRegistryCertificateOwnership(kubectl, logger); err != nil {
		return err
	}

	dnsNames, ipAddresses := registryCertificateSANs(plan)
	if len(dnsNames) == 0 && len(ipAddresses) == 0 {
		err := fmt.Errorf("no DNS names or IP addresses resolved for the Certificate; set MCP_PLATFORM_DOMAIN, MCP_REGISTRY_HOST, or MCP_REGISTRY_INGRESS_HOST (and optional MCP_MCP_INGRESS_HOST)")
		wrappedErr := core.WrapWithSentinel(core.ErrTLSSetupFailed, err, err.Error())
		core.Error("Invalid TLS host configuration")
		if logger != nil {
			core.LogStructuredError(logger, wrappedErr, "Invalid TLS host configuration")
		}
		return wrappedErr
	}

	core.Info("Applying Certificate for registry (custom ClusterIssuer)")
	if err := certmanager.ApplyRegistryCertificate(kubectl, dnsNames, ipAddresses, issuerName); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrApplyCertificateFailed,
			err,
			fmt.Sprintf("failed to apply Certificate: %v", err),
			map[string]any{"certificate": certmanager.RegistryCertificateName, "namespace": core.NamespaceRegistry, "component": "setup"},
		)
		core.Error("Failed to apply Certificate")
		if logger != nil {
			core.LogStructuredError(logger, wrappedErr, "Failed to apply Certificate")
		}
		return wrappedErr
	}

	certTimeout := core.GetCertTimeout()
	if certTimeout < 5*time.Minute {
		certTimeout = 5 * time.Minute
	}
	core.Info(fmt.Sprintf("Waiting for certificate to be issued (timeout: %s)", certTimeout))
	if err := certmanager.WaitForCertificateReadyWithKubectl(kubectl, certmanager.RegistryCertificateName, core.NamespaceRegistry, certTimeout); err != nil {
		err := core.NewWithSentinel(core.ErrCertificateNotReady, fmt.Sprintf("certificate not ready after %s. Check cert-manager and your ClusterIssuer configuration: kubectl logs -n cert-manager deployment/cert-manager", certTimeout))
		core.Error("Certificate not ready")
		if logger != nil {
			core.LogStructuredError(logger, err, "Certificate not ready")
		}
		return err
	}
	core.Success("Certificate issued successfully")
	if err := setupBundledRegistryInternalTLSStep(kubectl, logger, plan); err != nil {
		return err
	}
	return nil
}

// setupTLSPrivateCA uses mcp-runtime-ca in cert-manager; bundled HTTPS setup
// generates it when missing, while other private-CA paths require it up front.
func setupTLSPrivateCA(kubectl core.KubectlRunner, logger *zap.Logger, plan setupplan.Plan) error {
	core.Info("Checking cert-manager installation")
	if err := certmanager.CheckCertManagerInstalledWithKubectl(kubectl); err != nil {
		err := core.WrapWithSentinel(core.ErrCertManagerNotInstalled, err, "cert-manager not installed. Install it first:\n  helm install cert-manager jetstack/cert-manager --namespace cert-manager --create-namespace --set crds.enabled=true\n  or run setup with --with-tls --acme-email <addr> to install cert-manager automatically")
		core.Error("Cert-manager not installed")
		if logger != nil {
			core.LogStructuredError(logger, err, "Cert-manager not installed")
		}
		return err
	}
	core.Info("cert-manager CRDs found")

	core.Info("Checking CA secret")
	if plan.RegistryMode == setupplan.RegistryModeBundledHTTPS {
		created, err := certmanager.EnsureCASecretWithKubectl(kubectl)
		if err != nil {
			err := core.WrapWithSentinel(core.ErrCASecretNotFound, err, "CA secret 'mcp-runtime-ca' could not be generated in cert-manager namespace. Create a private CA manually:\n  kubectl create secret tls mcp-runtime-ca --cert=ca.crt --key=ca.key -n cert-manager")
			core.Error("CA secret unavailable")
			if logger != nil {
				core.LogStructuredError(logger, err, "CA secret unavailable")
			}
			return err
		}
		if created {
			core.Info("Generated cert-manager/mcp-runtime-ca for bundled HTTPS registry TLS; configure every Kubernetes node to trust its tls.crt before pulling from the bundled HTTPS registry")
		}
	} else if err := certmanager.CheckCASecretWithKubectl(kubectl); err != nil {
		err := core.WrapWithSentinel(core.ErrCASecretNotFound, err, "CA secret 'mcp-runtime-ca' not found in cert-manager namespace. For Let's Encrypt use --acme-email, or create a private CA:\n  kubectl create secret tls mcp-runtime-ca --cert=ca.crt --key=ca.key -n cert-manager")
		core.Error("CA secret not found")
		if logger != nil {
			core.LogStructuredError(logger, err, "CA secret not found")
		}
		return err
	}
	core.Info("CA secret found")

	core.Info("Applying ClusterIssuer")
	if err := certmanager.ApplyClusterIssuerWithKubectl(kubectl); err != nil {
		wrappedErr := core.WrapWithSentinel(core.ErrClusterIssuerApplyFailed, err, fmt.Sprintf("failed to apply ClusterIssuer: %v", err))
		core.Error("Failed to apply ClusterIssuer")
		if logger != nil {
			core.LogStructuredError(logger, wrappedErr, "Failed to apply ClusterIssuer")
		}
		return wrappedErr
	}

	if err := kube.EnsureNamespace(kubectl.CommandArgs, core.NamespaceRegistry); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrCreateRegistryNamespaceFailed,
			err,
			fmt.Sprintf("failed to create registry namespace: %v", err),
			map[string]any{"namespace": core.NamespaceRegistry, "component": "setup"},
		)
		core.Error("Failed to create registry namespace")
		if logger != nil {
			core.LogStructuredError(logger, wrappedErr, "Failed to create registry namespace")
		}
		return wrappedErr
	}
	if err := ensureRegistryCertificateOwnership(kubectl, logger); err != nil {
		return err
	}

	core.Info("Applying Certificate for registry")
	var certErr error
	if plan.RegistryMode == setupplan.RegistryModeBundledHTTPS {
		dnsNames, ipAddresses := registryCertificateSANs(plan)
		certErr = certmanager.ApplyRegistryCertificate(kubectl, dnsNames, ipAddresses, certmanager.CertClusterIssuerName)
	} else {
		certErr = certmanager.ApplyRegistryCertificateWithKubectl(kubectl)
	}
	if certErr != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrApplyCertificateFailed,
			certErr,
			fmt.Sprintf("failed to apply Certificate: %v", certErr),
			map[string]any{"certificate": certmanager.RegistryCertificateName, "namespace": core.NamespaceRegistry, "component": "setup"},
		)
		core.Error("Failed to apply Certificate")
		if logger != nil {
			core.LogStructuredError(logger, wrappedErr, "Failed to apply Certificate")
		}
		return wrappedErr
	}

	certTimeout := core.GetCertTimeout()
	core.Info(fmt.Sprintf("Waiting for certificate to be issued (timeout: %s)", certTimeout))
	if err := certmanager.WaitForCertificateReadyWithKubectl(kubectl, certmanager.RegistryCertificateName, core.NamespaceRegistry, certTimeout); err != nil {
		err := core.NewWithSentinel(core.ErrCertificateNotReady, fmt.Sprintf("certificate not ready after %s. Check cert-manager logs: kubectl logs -n cert-manager deployment/cert-manager", certTimeout))
		core.Error("Certificate not ready")
		if logger != nil {
			core.LogStructuredError(logger, err, "Certificate not ready")
		}
		return err
	}
	core.Success("Certificate issued successfully")
	if err := setupBundledRegistryInternalTLSStep(kubectl, logger, plan); err != nil {
		return err
	}
	return nil
}

func setupBundledRegistryInternalTLSStep(kubectl core.KubectlRunner, logger *zap.Logger, plan setupplan.Plan) error {
	if plan.RegistryMode != setupplan.RegistryModeBundledHTTPS {
		return nil
	}
	issuerName, err := bundledRegistryInternalIssuerName(kubectl, plan)
	if err != nil {
		wrappedErr := core.WrapWithSentinel(core.ErrClusterIssuerNotFound, err, fmt.Sprintf("failed to inspect internal registry ClusterIssuer: %v", err))
		core.Error("Internal registry issuer unavailable")
		if logger != nil {
			core.LogStructuredError(logger, wrappedErr, "Internal registry issuer unavailable")
		}
		return wrappedErr
	}
	if issuerName == certmanager.CertClusterIssuerName {
		core.Info("Ensuring internal registry CA secret")
		created, err := certmanager.EnsureCASecretWithKubectl(kubectl)
		if err != nil {
			err := core.WrapWithSentinel(
				core.ErrCASecretNotFound,
				err,
				"bundled HTTPS registry pulls need an internal CA for registry-internal-tls. Setup could not create cert-manager/mcp-runtime-ca; pass --tls-cluster-issuer for an existing internal issuer or create the CA secret manually",
			)
			core.Error("Internal registry CA secret unavailable")
			if logger != nil {
				core.LogStructuredError(logger, err, "Internal registry CA secret unavailable")
			}
			return err
		}
		if created {
			core.Info("Generated cert-manager/mcp-runtime-ca for internal registry TLS; configure every Kubernetes node to trust its tls.crt before pulling from the bundled HTTPS registry")
		}
		core.Info("Applying internal registry ClusterIssuer")
		if err := certmanager.ApplyClusterIssuerWithKubectl(kubectl); err != nil {
			wrappedErr := core.WrapWithSentinel(core.ErrClusterIssuerApplyFailed, err, fmt.Sprintf("failed to apply internal registry ClusterIssuer: %v", err))
			core.Error("Failed to apply internal registry ClusterIssuer")
			if logger != nil {
				core.LogStructuredError(logger, wrappedErr, "Failed to apply internal registry ClusterIssuer")
			}
			return wrappedErr
		}
	}

	dnsNames, ipAddresses := registryInternalCertificateSANs(plan)
	core.Info("Applying Certificate for internal registry pod TLS")
	if err := certmanager.ApplyRegistryInternalCertificate(kubectl, dnsNames, ipAddresses, issuerName); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrApplyCertificateFailed,
			err,
			fmt.Sprintf("failed to apply internal registry Certificate: %v", err),
			map[string]any{"certificate": certmanager.RegistryInternalCertificateName, "namespace": core.NamespaceRegistry, "component": "setup"},
		)
		core.Error("Failed to apply internal registry Certificate")
		if logger != nil {
			core.LogStructuredError(logger, wrappedErr, "Failed to apply internal registry Certificate")
		}
		return wrappedErr
	}

	certTimeout := core.GetCertTimeout()
	if certTimeout < 2*time.Minute {
		certTimeout = 2 * time.Minute
	}
	core.Info(fmt.Sprintf("Waiting for internal registry certificate to be issued (timeout: %s)", certTimeout))
	if err := certmanager.WaitForCertificateReadyWithKubectl(kubectl, certmanager.RegistryInternalCertificateName, core.NamespaceRegistry, certTimeout); err != nil {
		err := core.NewWithSentinel(core.ErrCertificateNotReady, fmt.Sprintf("internal registry certificate not ready after %s. Check cert-manager and your internal issuer configuration", certTimeout))
		core.Error("Internal registry certificate not ready")
		if logger != nil {
			core.LogStructuredError(logger, err, "Internal registry certificate not ready")
		}
		return err
	}
	core.Success("Internal registry certificate issued successfully")
	return nil
}

func ensureRegistryCertificateOwnership(kubectl core.KubectlRunner, logger *zap.Logger) error {
	core.Info("Checking registry TLS Certificate ownership")
	if err := certmanager.RemoveRegistryIngressShimAnnotationWithKubectl(kubectl); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrTLSSetupFailed,
			err,
			err.Error(),
			map[string]any{"ingress": core.RegistryServiceName, "namespace": core.NamespaceRegistry, "component": "setup"},
		)
		core.Error("Failed to remove registry ingress-shim annotation")
		if logger != nil {
			core.LogStructuredError(logger, wrappedErr, "Failed to remove registry ingress-shim annotation")
		}
		return wrappedErr
	}
	if err := certmanager.CheckRegistryCertificateOwnershipWithKubectl(kubectl); err != nil {
		wrappedErr := core.WrapWithSentinelAndContext(
			core.ErrTLSSetupFailed,
			err,
			err.Error(),
			map[string]any{"resource_name": certmanager.RegistryTLSSecretName, "namespace": core.NamespaceRegistry, "component": "setup"},
		)
		core.Error("Registry TLS Certificate conflict")
		if logger != nil {
			core.LogStructuredError(logger, wrappedErr, "Registry TLS Certificate conflict")
		}
		return wrappedErr
	}
	return nil
}
