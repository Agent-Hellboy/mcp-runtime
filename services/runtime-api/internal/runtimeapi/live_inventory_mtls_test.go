package runtimeapi

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mcp-runtime/pkg/controlplane"
)

// writeTestClientCert writes a self-signed client keypair to dir and returns the
// cert and key file paths. notAfter controls the leaf certificate expiry.
func writeTestClientCert(t *testing.T, dir string, notAfter time.Time) (certFile, keyFile string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "live-inventory-probe"},
		NotBefore:    notAfter.Add(-time.Hour),
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	certFile = filepath.Join(dir, "client.crt")
	keyFile = filepath.Join(dir, "client.key")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return certFile, keyFile
}

func TestMTLSProbeClientCachesUntilNearExpiry(t *testing.T) {
	now := time.Date(2026, time.June, 29, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	certFile, keyFile := writeTestClientCert(t, dir, now.Add(time.Hour))
	t.Setenv(liveInventoryClientCertEnv, certFile)
	t.Setenv(liveInventoryClientKeyEnv, keyFile)

	prober := &mcpLiveInventoryProber{
		mtlsClients: make(map[string]*cachedMTLSClient),
		now:         func() time.Time { return now },
	}
	server := controlplane.ServerInfo{Namespace: "team-a", Name: "demo"}

	first, err := prober.mtlsProbeClient(context.Background(), server)
	if err != nil {
		t.Fatalf("first mtlsProbeClient: %v", err)
	}
	second, err := prober.mtlsProbeClient(context.Background(), server)
	if err != nil {
		t.Fatalf("second mtlsProbeClient: %v", err)
	}
	if first != second {
		t.Fatalf("expected cached client to be reused while certificate is valid")
	}

	// Advance to within the renew window and confirm a fresh client is minted.
	now = now.Add(time.Hour - time.Minute)
	third, err := prober.mtlsProbeClient(context.Background(), server)
	if err != nil {
		t.Fatalf("third mtlsProbeClient: %v", err)
	}
	if third == second {
		t.Fatalf("expected a new client once the certificate nears expiry")
	}
}
