package runtimeapi

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	mcpv1alpha1 "mcp-runtime/api/v1alpha1"
	"mcp-runtime/pkg/certauth"
	"mcp-runtime/pkg/controlplane"
)

const (
	liveInventoryProbeSessionEnv = "MCP_LIVE_INVENTORY_PROBE_SESSION"
	defaultLiveInventorySession  = "live-inventory-probe"

	liveInventoryClientCertEnv = "MCP_LIVE_INVENTORY_CLIENT_CERT_FILE"
	liveInventoryClientKeyEnv  = "MCP_LIVE_INVENTORY_CLIENT_KEY_FILE"
	liveInventoryClientCAEnv   = "MCP_LIVE_INVENTORY_CLIENT_CA_FILE"
)

func serverUsesMTLSAuth(server controlplane.ServerInfo) bool {
	return server.AuthMode == mcpv1alpha1.AuthModeMTLS
}

func liveInventoryProbeSessionName() string {
	if name := strings.TrimSpace(os.Getenv(liveInventoryProbeSessionEnv)); name != "" {
		return name
	}
	return defaultLiveInventorySession
}

func mtlsLiveInventoryEndpoint(server controlplane.ServerInfo) (string, error) {
	if endpoint := liveInventoryPublicEndpoint(server.Endpoint); endpoint != "" {
		if strings.HasPrefix(strings.ToLower(endpoint), "https://") {
			return endpoint, nil
		}
	}
	return "", fmt.Errorf("mTLS live inventory requires a public https MCP endpoint")
}

// cachedMTLSClient holds a probe client and the expiry of its leaf certificate
// so we can reuse the client (and its connection pool) until the certificate is
// close to expiring.
type cachedMTLSClient struct {
	client    *http.Client
	expiresAt time.Time
}

// mtlsClientRenewBefore is how long before certificate expiry we discard a cached
// client and mint a fresh one.
const mtlsClientRenewBefore = 2 * time.Minute

// mtlsProbeClient returns an mTLS-capable HTTP client for the server, reusing a
// cached client (and its underlying connection pool) until the client
// certificate nears expiry. Caching avoids recreating the transport — and, for
// issuer-backed certs, re-issuing a CertificateRequest — on every probe.
func (p *mcpLiveInventoryProber) mtlsProbeClient(ctx context.Context, server controlplane.ServerInfo) (*http.Client, error) {
	cacheKey := server.Namespace + "/" + server.Name

	p.mu.Lock()
	if p.mtlsClients == nil {
		p.mtlsClients = make(map[string]*cachedMTLSClient)
	} else if cached, ok := p.mtlsClients[cacheKey]; ok &&
		(cached.expiresAt.IsZero() || p.currentTime().Add(mtlsClientRenewBefore).Before(cached.expiresAt)) {
		p.mu.Unlock()
		return cached.client, nil
	}
	p.mu.Unlock()

	client, expiresAt, err := p.newMTLSProbeClient(ctx, server)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	p.mtlsClients[cacheKey] = &cachedMTLSClient{client: client, expiresAt: expiresAt}
	p.mu.Unlock()
	return client, nil
}

// newMTLSProbeClient builds a fresh mTLS client, either from configured client
// cert files or by issuing a short-lived session certificate via the issuer.
func (p *mcpLiveInventoryProber) newMTLSProbeClient(ctx context.Context, server controlplane.ServerInfo) (*http.Client, time.Time, error) {
	if certFile := strings.TrimSpace(os.Getenv(liveInventoryClientCertEnv)); certFile != "" {
		keyFile := strings.TrimSpace(os.Getenv(liveInventoryClientKeyEnv))
		if keyFile == "" {
			return nil, time.Time{}, fmt.Errorf("mTLS live inventory client key file is not configured")
		}
		return tlsHTTPClientFromFiles(certFile, keyFile, strings.TrimSpace(os.Getenv(liveInventoryClientCAEnv)), liveInventoryProbeTimeout)
	}
	if p.access == nil {
		return nil, time.Time{}, fmt.Errorf("mTLS live inventory certificate issuer is not configured")
	}
	keyPEM, csrPEM, _, err := certauth.BuildSessionCSR(server.TrustDomain, server.Namespace, liveInventoryProbeSessionName())
	if err != nil {
		return nil, time.Time{}, err
	}
	certPEM, caPEM, err := p.access.issueSessionCertificate(
		ctx,
		server.Namespace,
		liveInventoryProbeSessionName(),
		server.TrustDomain,
		string(csrPEM),
	)
	if err != nil {
		return nil, time.Time{}, err
	}
	return tlsHTTPClientFromPEM(keyPEM, []byte(certPEM), []byte(caPEM), liveInventoryProbeTimeout)
}

// leafNotAfter returns the NotAfter of the keypair's leaf certificate, or the
// zero time if it cannot be parsed.
func leafNotAfter(cert tls.Certificate) time.Time {
	if len(cert.Certificate) == 0 {
		return time.Time{}
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return time.Time{}
	}
	return leaf.NotAfter
}

func tlsHTTPClientFromFiles(certFile, keyFile, caFile string, timeout time.Duration) (*http.Client, time.Time, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("load mTLS client key pair: %w", err)
	}
	tlsConfig := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}
	if caFile != "" {
		caPEM, err := os.ReadFile(caFile)
		if err != nil {
			return nil, time.Time{}, fmt.Errorf("read mTLS client CA bundle: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, time.Time{}, fmt.Errorf("parse mTLS client CA bundle")
		}
		tlsConfig.RootCAs = pool
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}, leafNotAfter(cert), nil
}

func tlsHTTPClientFromPEM(keyPEM, certPEM, caPEM []byte, timeout time.Duration) (*http.Client, time.Time, error) {
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("load mTLS client key pair: %w", err)
	}
	tlsConfig := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}
	if len(caPEM) > 0 {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, time.Time{}, fmt.Errorf("parse mTLS client CA bundle")
		}
		tlsConfig.RootCAs = pool
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}, leafNotAfter(cert), nil
}
