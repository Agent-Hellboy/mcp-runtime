package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeKeyPair writes a fresh self-signed cert/key to certPath/keyPath and
// returns the leaf DER so callers can assert which cert is being served.
func writeKeyPair(t *testing.T, certPath, keyPath, cn string) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("cert: %v", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return der
}

func servedLeaf(t *testing.T, r *certReloader) []byte {
	t.Helper()
	cert, err := r.GetCertificate(&tls.ClientHelloInfo{})
	if err != nil {
		t.Fatalf("GetCertificate: %v", err)
	}
	if cert == nil || len(cert.Certificate) == 0 {
		t.Fatal("no certificate served")
	}
	return cert.Certificate[0]
}

func TestCertReloaderServesAndReloads(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "tls.crt")
	keyPath := filepath.Join(dir, "tls.key")

	first := writeKeyPair(t, certPath, keyPath, "cert-1")
	r, err := newCertReloader(certPath, keyPath)
	if err != nil {
		t.Fatalf("newCertReloader: %v", err)
	}
	if string(servedLeaf(t, r)) != string(first) {
		t.Fatal("initial GetCertificate did not serve the loaded cert")
	}

	// Simulate cert-manager rotating the mounted files.
	second := writeKeyPair(t, certPath, keyPath, "cert-2")
	if string(second) == string(first) {
		t.Fatal("test setup: rotated cert is identical")
	}
	if err := r.reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if string(servedLeaf(t, r)) != string(second) {
		t.Fatal("after reload, GetCertificate still serves the old cert")
	}
}

func TestNewCertReloaderFailsFastOnMissingFiles(t *testing.T) {
	if _, err := newCertReloader(filepath.Join(t.TempDir(), "nope.crt"), filepath.Join(t.TempDir(), "nope.key")); err == nil {
		t.Fatal("expected an error for missing certificate files")
	}
}

func TestCertReloaderWatchReloadsOnChange(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "tls.crt")
	keyPath := filepath.Join(dir, "tls.key")
	writeKeyPair(t, certPath, keyPath, "cert-1")
	r, err := newCertReloader(certPath, keyPath)
	if err != nil {
		t.Fatalf("newCertReloader: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.watch(ctx, 20*time.Millisecond)

	// Bump modtime into the future so the poll reliably detects the change.
	second := writeKeyPair(t, certPath, keyPath, "cert-2")
	future := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(certPath, future, future)

	// Generous deadline so the 20ms poll is detected even when CI runs the
	// service test suites in parallel under a constrained CPU quota.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if string(servedLeaf(t, r)) == string(second) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("watch did not reload the rotated certificate within the deadline")
}
