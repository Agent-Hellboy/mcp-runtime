package runtimeapi

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const adapterCertificateRequestMaxBytes = 64 << 10

var certificateRequestGVR = schema.GroupVersionResource{
	Group: "cert-manager.io", Version: "v1", Resource: "certificaterequests",
}

var adapterMCPServerGVR = schema.GroupVersionResource{
	Group: "mcpruntime.org", Version: "v1alpha1", Resource: "mcpservers",
}

type adapterCertificateRequest struct {
	Namespace string `json:"namespace"`
	Session   string `json:"session"`
	CSR       string `json:"csr"`
}

type adapterCertificateResponse struct {
	Certificate string    `json:"certificate"`
	CABundle    string    `json:"caBundle"`
	SPIFFEID    string    `json:"spiffeID"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

// HandleAdapterCertificate signs a client-generated CSR after verifying that
// its SPIFFE URI names a session owned by the authenticated principal.
func (s *AccessService) HandleAdapterCertificate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.k8sClients == nil || s.k8sClients.Dynamic == nil || s.accessMgr == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "kubernetes not available")
		return
	}
	issuer := strings.TrimSpace(os.Getenv("MCP_MTLS_CLUSTER_ISSUER"))
	if issuer == "" {
		writeAPIError(w, http.StatusServiceUnavailable, "workload certificate issuer is not configured")
		return
	}

	var req adapterCertificateRequest
	r.Body = http.MaxBytesReader(w, r.Body, adapterCertificateRequestMaxBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBodyDecodeError(w, err)
		return
	}
	req.Namespace = strings.TrimSpace(req.Namespace)
	req.Session = strings.TrimSpace(req.Session)
	if req.Namespace == "" || req.Session == "" || strings.TrimSpace(req.CSR) == "" {
		writeAPIError(w, http.StatusBadRequest, "namespace, session, and csr are required")
		return
	}

	principal, ok := principalFromContext(r.Context())
	if !ok {
		writeAPIError(w, http.StatusUnauthorized, "no principal on request")
		return
	}
	session, err := s.accessMgr.GetSession(r.Context(), req.Session, req.Namespace)
	if err != nil || session == nil {
		writeAPIError(w, http.StatusNotFound, "adapter session not found")
		return
	}
	humanID := strings.TrimSpace(principal.Subject)
	if humanID == "" {
		humanID = strings.TrimSpace(principal.Email)
	}
	if humanID == "" || humanID != string(session.Spec.Subject.HumanID) {
		writeAPIError(w, http.StatusForbidden, "adapter session is not owned by the authenticated principal")
		return
	}
	if session.Spec.ExpiresAt == nil || !session.Spec.ExpiresAt.After(time.Now()) {
		writeAPIError(w, http.StatusForbidden, "adapter session is expired")
		return
	}
	serverName := string(session.Spec.ServerRef.Name)
	serverNamespace := string(session.Spec.ServerRef.Namespace)
	if serverNamespace == "" {
		serverNamespace = req.Namespace
	}
	server, err := s.k8sClients.Dynamic.Resource(adapterMCPServerGVR).Namespace(serverNamespace).Get(
		r.Context(), serverName, metav1.GetOptions{},
	)
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "read target MCPServer", err)
		return
	}
	authMode, _, _ := unstructured.NestedString(server.Object, "spec", "auth", "mode")
	trustDomain, _, _ := unstructured.NestedString(server.Object, "spec", "auth", "trustDomain")
	trustDomain = strings.TrimSpace(trustDomain)
	if authMode != "mtls" || trustDomain == "" {
		writeAPIError(w, http.StatusBadRequest, "target MCPServer is not configured for mTLS")
		return
	}

	expectedSPIFFEID := fmt.Sprintf("spiffe://%s/ns/%s/session/%s", trustDomain, req.Namespace, req.Session)
	csrDER, err := validateAdapterCSR(req.CSR, expectedSPIFFEID)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	expiresAt := session.Spec.ExpiresAt.Time.UTC()
	duration := time.Until(expiresAt)
	if duration > adapterSessionMaxTTL {
		duration = adapterSessionMaxTTL
	}
	if duration < time.Minute {
		writeAPIError(w, http.StatusForbidden, "adapter session is too close to expiry")
		return
	}
	resource := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "cert-manager.io/v1",
		"kind":       "CertificateRequest",
		"metadata": map[string]any{
			"generateName": "adapter-" + req.Session + "-",
			"namespace":    req.Namespace,
			"labels": map[string]any{
				"app.kubernetes.io/managed-by": "mcp-runtime",
				"mcpruntime.org/session":       req.Session,
			},
		},
		"spec": map[string]any{
			"request":  base64.StdEncoding.EncodeToString(csrDER),
			"duration": duration.Round(time.Second).String(),
			"usages":   []any{"digital signature", "client auth"},
			"issuerRef": map[string]any{
				"group": "cert-manager.io",
				"kind":  "ClusterIssuer",
				"name":  issuer,
			},
		},
	}}
	created, err := s.k8sClients.Dynamic.Resource(certificateRequestGVR).Namespace(req.Namespace).Create(
		r.Context(), resource, metav1.CreateOptions{},
	)
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "create certificate request", err)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	certificate, caBundle, err := waitForIssuedAdapterCertificate(ctx, s, req.Namespace, created.GetName())
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "issue adapter certificate", err)
		return
	}
	writeJSON(w, http.StatusCreated, adapterCertificateResponse{
		Certificate: certificate,
		CABundle:    caBundle,
		SPIFFEID:    expectedSPIFFEID,
		ExpiresAt:   expiresAt,
	})
}

func validateAdapterCSR(raw, expectedSPIFFEID string) ([]byte, error) {
	block, _ := pem.Decode([]byte(raw))
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		return nil, fmt.Errorf("csr must be a PEM CERTIFICATE REQUEST")
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil || csr.CheckSignature() != nil {
		return nil, fmt.Errorf("csr is invalid or has an invalid signature")
	}
	if len(csr.URIs) != 1 || csr.URIs[0].String() != expectedSPIFFEID {
		return nil, fmt.Errorf("csr must contain exactly the SPIFFE URI %q", expectedSPIFFEID)
	}
	if len(csr.DNSNames) != 0 || len(csr.EmailAddresses) != 0 || len(csr.IPAddresses) != 0 {
		return nil, fmt.Errorf("csr may not contain DNS, email, or IP subject alternative names")
	}
	return block.Bytes, nil
}

func waitForIssuedAdapterCertificate(ctx context.Context, s *AccessService, namespace, name string) (string, string, error) {
	if s == nil || s.accessMgr == nil || s.k8sClients == nil {
		return "", "", fmt.Errorf("kubernetes not available")
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		request, err := s.k8sClients.Dynamic.Resource(certificateRequestGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return "", "", err
		}
		if err := adapterCertificateRequestFailure(request); err != nil {
			return "", "", err
		}
		certificate, _, _ := unstructured.NestedString(request.Object, "status", "certificate")
		ca, _, _ := unstructured.NestedString(request.Object, "status", "ca")
		if certificate != "" {
			certPEM, err := base64.StdEncoding.DecodeString(certificate)
			if err != nil {
				return "", "", fmt.Errorf("decode issued certificate: %w", err)
			}
			caPEM, err := base64.StdEncoding.DecodeString(ca)
			if err != nil {
				return "", "", fmt.Errorf("decode issued CA bundle: %w", err)
			}
			return string(certPEM), string(caPEM), nil
		}
		select {
		case <-ctx.Done():
			return "", "", ctx.Err()
		case <-ticker.C:
		}
	}
}

func adapterCertificateRequestFailure(request *unstructured.Unstructured) error {
	conditions, found, _ := unstructured.NestedSlice(request.Object, "status", "conditions")
	if !found {
		return nil
	}
	for _, condition := range conditions {
		conditionMap, ok := condition.(map[string]any)
		if !ok {
			continue
		}
		conditionType, _, _ := unstructured.NestedString(conditionMap, "type")
		conditionStatus, _, _ := unstructured.NestedString(conditionMap, "status")
		if conditionType != "Ready" || conditionStatus != "False" {
			continue
		}
		reason, _, _ := unstructured.NestedString(conditionMap, "reason")
		if reason != "Failed" && reason != "Denied" {
			continue
		}
		message, _, _ := unstructured.NestedString(conditionMap, "message")
		if strings.TrimSpace(message) == "" {
			return fmt.Errorf("certificate request failed: %s", reason)
		}
		return fmt.Errorf("certificate request failed: %s (%s)", reason, message)
	}
	return nil
}
