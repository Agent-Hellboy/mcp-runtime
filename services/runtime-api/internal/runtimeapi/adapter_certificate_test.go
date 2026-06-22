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

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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

func TestAdapterCertificateRequestFailure(t *testing.T) {
	t.Parallel()
	request := &unstructured.Unstructured{Object: map[string]any{
		"status": map[string]any{
			"conditions": []any{map[string]any{
				"type":    "Ready",
				"status":  "False",
				"reason":  "Denied",
				"message": "issuer rejected the request",
			}},
		},
	}}

	err := adapterCertificateRequestFailure(request)
	if err == nil {
		t.Fatal("expected failed CertificateRequest condition to return an error")
	}
	if !strings.Contains(err.Error(), "Denied") || !strings.Contains(err.Error(), "issuer rejected the request") {
		t.Fatalf("unexpected error: %v", err)
	}
}
