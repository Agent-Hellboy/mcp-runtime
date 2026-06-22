package adapter

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"mcp-runtime/internal/cli/core"
	"mcp-runtime/internal/cli/platformapi"
)

func newEnrollCmd(_ *core.Runtime) *cobra.Command {
	var flags platformSessionFlags
	var outputDir string
	var trustDomain string

	cmd := &cobra.Command{
		Use:   "enroll",
		Short: "Issue platform-managed mTLS files for an external adapter",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !flags.enabled() || strings.TrimSpace(flags.agent) == "" {
				return fmt.Errorf("--server and --agent are required")
			}
			if u := strings.TrimSpace(flags.platformURL); u != "" {
				if err := os.Setenv(EnvPlatformURL, u); err != nil {
					return err
				}
			}
			client, err := platformapi.NewPlatformClient()
			if err != nil {
				return err
			}
			return enrollAdapterCertificate(cmd.Context(), client, flags, outputDir, trustDomain, cmd.OutOrStdout())
		},
	}
	bindPlatformSessionFlags(cmd, &flags)
	_ = cmd.Flags().MarkHidden("auto-refresh")
	cmd.Flags().StringVar(&outputDir, "output-dir", ".", "Directory for client.crt, client.key, and ca.crt")
	cmd.Flags().StringVar(&trustDomain, "trust-domain", envOrDefault("MCP_MTLS_TRUST_DOMAIN", "mcpruntime.org"), "SPIFFE trust domain configured on the platform")
	return cmd
}

func enrollAdapterCertificate(ctx context.Context, client *platformapi.PlatformClient, flags platformSessionFlags, outputDir, trustDomain string, out interface{ Write([]byte) (int, error) }) error {
	trustDomain = strings.TrimSpace(trustDomain)
	if trustDomain == "" {
		return fmt.Errorf("--trust-domain must not be empty")
	}
	session, err := client.CreateAdapterSession(ctx, platformapi.AdapterSessionRequest{
		ServerName: strings.TrimSpace(flags.server),
		Namespace:  strings.TrimSpace(flags.namespace),
		AgentID:    strings.TrimSpace(flags.agent),
	})
	if err != nil {
		return fmt.Errorf("create adapter session: %w", err)
	}

	spiffeID := &url.URL{
		Scheme: "spiffe",
		Host:   trustDomain,
		Path:   "/ns/" + session.Namespace + "/session/" + session.Name,
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate client key: %w", err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		URIs: []*url.URL{spiffeID},
	}, key)
	if err != nil {
		return fmt.Errorf("create CSR: %w", err)
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})
	issued, err := client.IssueAdapterCertificate(ctx, platformapi.AdapterCertificateRequest{
		Namespace: session.Namespace,
		Session:   session.Name,
		CSR:       string(csrPEM),
	})
	if err != nil {
		return fmt.Errorf("issue adapter certificate: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return fmt.Errorf("marshal client key: %w", err)
	}

	dir := filepath.Clean(outputDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	files := []struct {
		name string
		data []byte
		mode os.FileMode
	}{
		{"client.crt", []byte(issued.Certificate), 0o600},
		{"client.key", pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600},
		{"ca.crt", []byte(issued.CABundle), 0o644},
	}
	for _, file := range files {
		if err := writeCredentialFile(dir, file.name, file.data, file.mode); err != nil {
			return fmt.Errorf("write %s: %w", file.name, err)
		}
	}
	_, _ = fmt.Fprintf(out, "issued %s (expires %s)\n", issued.SPIFFEID, issued.ExpiresAt.Format("2006-01-02T15:04:05Z07:00"))
	_, _ = fmt.Fprintf(out, "use --tls-client-cert %s --tls-client-key %s --tls-ca-bundle %s\n",
		filepath.Join(dir, "client.crt"), filepath.Join(dir, "client.key"), filepath.Join(dir, "ca.crt"))
	return nil
}

func writeCredentialFile(dir, name string, data []byte, mode os.FileMode) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
