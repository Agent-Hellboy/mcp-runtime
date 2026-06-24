package runtimeapi

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"mcp-runtime/pkg/certauth"
)

func TestValidateAdapterCSRUsesCertauth(t *testing.T) {
	t.Parallel()
	const expected = "spiffe://example.org/ns/team-a/session/session-1"
	_, csrPEM, _, err := certauth.BuildSessionCSR("example.org", "team-a", "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := certauth.ValidateCSRPEM(string(csrPEM), expected); err != nil {
		t.Fatalf("ValidateCSRPEM() error = %v", err)
	}
	if _, err := certauth.ValidateCSRPEM(string(csrPEM), strings.Replace(expected, "session-1", "session-2", 1)); err == nil {
		t.Fatal("ValidateCSRPEM() accepted a CSR for another session")
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
