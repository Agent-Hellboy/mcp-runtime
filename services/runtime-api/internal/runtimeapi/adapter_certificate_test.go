package runtimeapi

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

func TestValidateAdapterCSR(t *testing.T) {
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
		URIs: []*url.URL{uri},
	}, key)
	if err != nil {
		t.Fatal(err)
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})

	if _, err := validateAdapterCSR(string(csrPEM), expected); err != nil {
		t.Fatalf("validateAdapterCSR() error = %v", err)
	}
	if _, err := validateAdapterCSR(string(csrPEM), strings.Replace(expected, "session-1", "session-2", 1)); err == nil {
		t.Fatal("validateAdapterCSR() accepted a CSR for another session")
	}
}

func TestValidateAdapterCSRRejectsAdditionalSANs(t *testing.T) {
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
	if _, err := validateAdapterCSR(string(csrPEM), expected); err == nil {
		t.Fatal("validateAdapterCSR() accepted an additional DNS SAN")
	}
}
