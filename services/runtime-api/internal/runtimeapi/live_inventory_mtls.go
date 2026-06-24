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

func (p *mcpLiveInventoryProber) mtlsProbeClient(ctx context.Context, server controlplane.ServerInfo) (*http.Client, error) {
	if certFile := strings.TrimSpace(os.Getenv(liveInventoryClientCertEnv)); certFile != "" {
		keyFile := strings.TrimSpace(os.Getenv(liveInventoryClientKeyEnv))
		if keyFile == "" {
			return nil, fmt.Errorf("mTLS live inventory client key file is not configured")
		}
		return tlsHTTPClientFromFiles(certFile, keyFile, strings.TrimSpace(os.Getenv(liveInventoryClientCAEnv)), liveInventoryProbeTimeout)
	}
	if p.access == nil {
		return nil, fmt.Errorf("mTLS live inventory certificate issuer is not configured")
	}
	keyPEM, csrPEM, _, err := certauth.BuildSessionCSR(server.TrustDomain, server.Namespace, liveInventoryProbeSessionName())
	if err != nil {
		return nil, err
	}
	certPEM, caPEM, err := p.access.issueSessionCertificate(
		ctx,
		server.Namespace,
		liveInventoryProbeSessionName(),
		server.TrustDomain,
		string(csrPEM),
	)
	if err != nil {
		return nil, err
	}
	return tlsHTTPClientFromPEM(keyPEM, []byte(certPEM), []byte(caPEM), liveInventoryProbeTimeout)
}

func tlsHTTPClientFromFiles(certFile, keyFile, caFile string, timeout time.Duration) (*http.Client, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load mTLS client key pair: %w", err)
	}
	tlsConfig := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}
	if caFile != "" {
		caPEM, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("read mTLS client CA bundle: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("parse mTLS client CA bundle")
		}
		tlsConfig.RootCAs = pool
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}, nil
}

func tlsHTTPClientFromPEM(keyPEM, certPEM, caPEM []byte, timeout time.Duration) (*http.Client, error) {
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("load mTLS client key pair: %w", err)
	}
	tlsConfig := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}
	if len(caPEM) > 0 {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("parse mTLS client CA bundle")
		}
		tlsConfig.RootCAs = pool
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}, nil
}
