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
	cases := []struct {
		name          string
		testMode, tls bool
		issuer        string
		wantErr       bool
	}{
		{name: "no mtls requested", wantErr: false},
		{name: "issuer alone needs tls", issuer: "corp-issuer", wantErr: true},
		{name: "issuer + tls enables mtls", issuer: "corp-issuer", tls: true, wantErr: false},
		{name: "issuer + test-mode is exempt from tls", issuer: "mcp-runtime-ca", testMode: true, wantErr: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateMTLSSetupCLIFlags(tc.testMode, tc.tls, tc.issuer)
			if tc.wantErr {
				if err == nil || !errors.Is(err, core.ErrFieldRequired) {
					t.Fatalf("want ErrFieldRequired, got %v", err)
				}
			} else if err != nil {
				t.Fatalf("want nil, got %v", err)
			}
		})
	}
}
