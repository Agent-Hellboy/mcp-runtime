package platform

import (
	"errors"
	"testing"

	"mcp-runtime/internal/cli/core"
)

func TestValidateTLSSetupCLIFlags(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		tls         bool
		acme, tlsCI string
		staging     bool
		skipCM      bool
		wantErr     bool
		wantIsField bool
	}{
		{"ok disabled", false, "", "", false, false, false, false},
		{"ok with-tls acme", true, "a@b.com", "", false, false, false, false},
		{"mutual exclusivity", false, "a@b.com", "issuer", false, false, true, true},
		{"acme without with-tls", false, "a@b.com", "", false, false, true, true},
		{"tls-cluster-issuer without with-tls", false, "", "issuer", false, false, true, true},
		{"staging without with-tls", false, "", "", true, false, true, true},
		{"skip-cm without with-tls", false, "", "", false, true, true, true},
		{"with-tls staging no email", true, "", "", true, true, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateTLSSetupCLIFlags(tc.tls, tc.acme, tc.tlsCI, tc.staging, tc.skipCM)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if tc.wantIsField && !errors.Is(err, core.ErrFieldRequired) {
					t.Fatalf("expected ErrFieldRequired, got %v", err)
				}
			} else if err != nil {
				t.Fatalf("unexpected: %v", err)
			}
		})
	}
}

func TestValidateMTLSSetupCLIFlags(t *testing.T) {
	t.Parallel()
	// withMTLS, testMode, tlsEnabled, issuer
	if err := ValidateMTLSSetupCLIFlags(true, false, false, ""); err == nil || !errors.Is(err, core.ErrFieldRequired) {
		t.Fatalf("--with-mtls without --with-tls should fail with ErrFieldRequired, got %v", err)
	}
	if err := ValidateMTLSSetupCLIFlags(true, false, true, ""); err != nil {
		t.Fatalf("--with-mtls with --with-tls should pass, got %v", err)
	}
	if err := ValidateMTLSSetupCLIFlags(false, false, false, ""); err != nil {
		t.Fatalf("mtls disabled should pass, got %v", err)
	}
	// --mtls-cluster-issuer without an explicit opt-in is a silent misconfig.
	if err := ValidateMTLSSetupCLIFlags(false, false, true, "corp-issuer"); err == nil || !errors.Is(err, core.ErrFieldRequired) {
		t.Fatalf("--mtls-cluster-issuer without --with-mtls/--test-mode should fail, got %v", err)
	}
	// ...but it is fine alongside --with-mtls or --test-mode.
	if err := ValidateMTLSSetupCLIFlags(true, false, true, "corp-issuer"); err != nil {
		t.Fatalf("issuer + --with-mtls should pass, got %v", err)
	}
	if err := ValidateMTLSSetupCLIFlags(false, true, false, "mcp-runtime-ca"); err != nil {
		t.Fatalf("issuer + --test-mode should pass, got %v", err)
	}
}
