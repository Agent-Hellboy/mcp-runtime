package runtimeapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"

	"mcp-runtime/pkg/k8sclient"
	"mcp-runtime/pkg/registrypush"
)

func TestRegisterAndServeRegistryPushTransfer(t *testing.T) {
	t.Setenv("POD_IP", "10.0.0.5")
	t.Setenv("PORT", "8080")

	dir := t.TempDir()
	tarPath := filepath.Join(dir, "image.tar")
	if err := os.WriteFile(tarPath, []byte("docker-tar-bytes"), 0o600); err != nil {
		t.Fatalf("write tar: %v", err)
	}

	client := kubernetesfake.NewSimpleClientset()
	server := &RuntimeServer{
		k8sClients: &k8sclient.Clients{Clientset: client},
	}

	token, fetchURL, err := server.registerRegistryPushTransfer(context.Background(), tarPath, time.Minute)
	if err != nil {
		t.Fatalf("registerRegistryPushTransfer() error = %v", err)
	}
	if fetchURL != "http://10.0.0.5:8080/internal/registry-push/tar" {
		t.Fatalf("fetchURL = %q", fetchURL)
	}

	req := httptest.NewRequest(http.MethodGet, registryPushTransferPath, nil)
	req.Header.Set(registrypush.TransferTokenHeader, token)
	rec := httptest.NewRecorder()
	server.HandleRegistryPushTransfer(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	body, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != "docker-tar-bytes" {
		t.Fatalf("body = %q", string(body))
	}
	if _, err := os.Stat(tarPath); !os.IsNotExist(err) {
		t.Fatalf("expected tar removed after transfer, stat err = %v", err)
	}
	secrets, err := client.CoreV1().Secrets(registryPushTransferNamespace()).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list secrets: %v", err)
	}
	if len(secrets.Items) != 0 {
		t.Fatalf("expected transfer secret deleted, found %d", len(secrets.Items))
	}
}

func TestHandleRegistryPushTransferRejectsMissingToken(t *testing.T) {
	server := &RuntimeServer{}
	req := httptest.NewRequest(http.MethodGet, registryPushTransferPath, nil)
	rec := httptest.NewRecorder()
	server.HandleRegistryPushTransfer(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestHandleRegistryPushTransferRejectsWrongPod(t *testing.T) {
	t.Setenv("POD_IP", "10.0.0.9")

	dir := t.TempDir()
	tarPath := filepath.Join(dir, "image.tar")
	if err := os.WriteFile(tarPath, []byte("payload"), 0o600); err != nil {
		t.Fatalf("write tar: %v", err)
	}

	token := "abc123"
	expires := time.Now().Add(time.Minute).UTC().Format(time.RFC3339)
	client := kubernetesfake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      registryPushTransferSecretName(token),
			Namespace: registryPushTransferNamespace(),
			Labels:    map[string]string{registryPushTransferLabelKey: "true"},
		},
		Data: map[string][]byte{
			"path":      []byte(tarPath),
			"podIP":     []byte("10.0.0.5"),
			"expiresAt": []byte(expires),
		},
	})
	server := &RuntimeServer{
		k8sClients: &k8sclient.Clients{Clientset: client},
	}

	req := httptest.NewRequest(http.MethodGet, registryPushTransferPath, nil)
	req.Header.Set(registrypush.TransferTokenHeader, token)
	rec := httptest.NewRecorder()
	server.HandleRegistryPushTransfer(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestRevokeRegistryPushTransfer(t *testing.T) {
	t.Setenv("POD_IP", "10.0.0.5")

	dir := t.TempDir()
	tarPath := filepath.Join(dir, "image.tar")
	if err := os.WriteFile(tarPath, []byte("payload"), 0o600); err != nil {
		t.Fatalf("write tar: %v", err)
	}

	client := kubernetesfake.NewSimpleClientset()
	server := &RuntimeServer{
		k8sClients: &k8sclient.Clients{Clientset: client},
	}
	token, _, err := server.registerRegistryPushTransfer(context.Background(), tarPath, time.Minute)
	if err != nil {
		t.Fatalf("registerRegistryPushTransfer() error = %v", err)
	}
	server.revokeRegistryPushTransfer(context.Background(), token)
	if _, err := os.Stat(tarPath); !os.IsNotExist(err) {
		t.Fatalf("expected tar removed on revoke, stat err = %v", err)
	}
}

func TestTeamIngressAllowNamespacesDisabledDefaultsKubeSystem(t *testing.T) {
	t.Setenv("PLATFORM_TRAEFIK_NAMESPACE", "")
	got := teamIngressAllowNamespaces(teamTraefikWatchConfig{mode: "disabled"})
	if len(got) != 1 || got[0] != "kube-system" {
		t.Fatalf("teamIngressAllowNamespaces() = %#v, want [kube-system]", got)
	}
}
