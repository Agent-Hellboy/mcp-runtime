package certauth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"net/url"
	"strings"
	"testing"
)

func TestBuildSessionCSR(t *testing.T) {
	t.Parallel()
	keyPEM, csrPEM, spiffeID, err := BuildSessionCSR("mcpruntime.org", "mcp-team-acme", "adapter-xyz")
	if err != nil {
		t.Fatalf("BuildSessionCSR: %v", err)
	}
	want := "spiffe://mcpruntime.org/ns/mcp-team-acme/session/adapter-xyz"
	if spiffeID != want {
		t.Fatalf("spiffeID = %q, want %q", spiffeID, want)
	}
	if _, err := ValidateCSRPEM(string(csrPEM), want); err != nil {
		t.Fatalf("ValidateCSRPEM() error = %v", err)
	}
	if block, _ := pem.Decode(keyPEM); block == nil || block.Type != "PRIVATE KEY" {
		t.Fatal("key PEM is not a PRIVATE KEY block")
	}
}

func TestValidateCSRPEM(t *testing.T) {
	t.Parallel()
	const expected = "spiffe://example.org/ns/team-a/session/session-1"
	_, csrPEM, _, err := BuildSessionCSR("example.org", "team-a", "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateCSRPEM(string(csrPEM), expected); err != nil {
		t.Fatalf("ValidateCSRPEM() error = %v", err)
	}
	if _, err := ValidateCSRPEM(string(csrPEM), strings.Replace(expected, "session-1", "session-2", 1)); err == nil {
		t.Fatal("ValidateCSRPEM() accepted a CSR for another session")
	}
}

func TestValidateCSRPEMRejectsAdditionalSANs(t *testing.T) {
	t.Parallel()
	const expected = "spiffe://example.org/ns/team-a/session/session-1"
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	uri, err := url.Parse(expected)
	if err != nil {
		t.Fatal(err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		URIs:     []*url.URL{uri},
		DNSNames: []string{"attacker.example"},
	}, key)
	if err != nil {
		t.Fatal(err)
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})
	if _, err := ValidateCSRPEM(string(csrPEM), expected); err == nil {
		t.Fatal("ValidateCSRPEM() accepted an additional DNS SAN")
	}
}

func TestWritePrivateFileRejectsTraversal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := WritePrivateFile(dir, "client.key", []byte("secret"), 0o600); err != nil {
		t.Fatalf("WritePrivateFile() error = %v", err)
	}
	if err := WritePrivateFile(dir, "../escape", []byte("nope"), 0o600); err == nil {
		t.Fatal("WritePrivateFile() allowed path traversal")
	}
}
