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
	"strings"
	"time"

	"mcp-runtime/internal/cli/platformapi"
)

// issuedCredential is a platform-signed, session-bound mTLS credential. The
// certificate's sole SPIFFE URI SAN encodes the issuing MCPAgentSession
// (spiffe://<trustDomain>/ns/<namespace>/session/<name>), so the gateway maps
// the verified cert straight to the session binding — no governance headers
// are involved. It is produced both by `adapter enroll` (written to disk) and
// by `--auth mtls` (kept in memory and rotated before expiry).
type issuedCredential struct {
	CertPEM   []byte
	KeyPEM    []byte
	CABundle  []byte
	SPIFFEID  string
	Session   platformapi.AdapterSession
	ExpiresAt time.Time
}

// buildSessionCSR generates a fresh P-256 key and a CSR whose only SAN is the
// session's SPIFFE URI. It returns the PKCS#8 key PEM and the CSR PEM.
func buildSessionCSR(trustDomain, namespace, sessionName string) (keyPEM, csrPEM []byte, spiffe *url.URL, err error) {
	spiffe = &url.URL{
		Scheme: "spiffe",
		Host:   trustDomain,
		Path:   "/ns/" + namespace + "/session/" + sessionName,
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("generate client key: %w", err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		URIs: []*url.URL{spiffe},
	}, key)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create CSR: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("marshal client key: %w", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	csrPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})
	return keyPEM, csrPEM, spiffe, nil
}

// issueAdapterCredential issues (or reuses) an adapter session for the target
// MCPServer, then signs a session-bound client certificate for it. Each call
// produces a fresh keypair, so rotating callers never reuse private keys.
func issueAdapterCredential(ctx context.Context, client *platformapi.PlatformClient, flags platformSessionFlags, trustDomain string) (issuedCredential, error) {
	trustDomain = strings.TrimSpace(trustDomain)
	if trustDomain == "" {
		return issuedCredential{}, fmt.Errorf("trust domain must not be empty")
	}
	session, err := client.CreateAdapterSession(ctx, platformapi.AdapterSessionRequest{
		ServerName: strings.TrimSpace(flags.server),
		Namespace:  strings.TrimSpace(flags.namespace),
		AgentID:    strings.TrimSpace(flags.agent),
	})
	if err != nil {
		return issuedCredential{}, fmt.Errorf("create adapter session: %w", err)
	}

	keyPEM, csrPEM, _, err := buildSessionCSR(trustDomain, session.Namespace, session.Name)
	if err != nil {
		return issuedCredential{}, err
	}
	issued, err := client.IssueAdapterCertificate(ctx, platformapi.AdapterCertificateRequest{
		Namespace: session.Namespace,
		Session:   session.Name,
		CSR:       string(csrPEM),
	})
	if err != nil {
		return issuedCredential{}, fmt.Errorf("issue adapter certificate: %w", err)
	}
	return issuedCredential{
		CertPEM:   []byte(issued.Certificate),
		KeyPEM:    keyPEM,
		CABundle:  []byte(issued.CABundle),
		SPIFFEID:  issued.SPIFFEID,
		Session:   session,
		ExpiresAt: issued.ExpiresAt,
	}, nil
}
