package plan

import "testing"

func TestBuildMTLSIssuerResolution(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		withMTLS   bool
		testMode   bool
		issuerIn   string
		wantIssuer string
		wantMTLS   bool
	}{
		{
			name:       "with-mtls managed defaults to bundled CA",
			withMTLS:   true,
			issuerIn:   "",
			wantIssuer: DefaultTestMTLSClusterIssuer,
			wantMTLS:   true,
		},
		{
			name:       "with-mtls keeps an external issuer",
			withMTLS:   true,
			issuerIn:   "corp-spiffe-issuer",
			wantIssuer: "corp-spiffe-issuer",
			wantMTLS:   true,
		},
		{
			name:       "test mode still defaults the bundled CA",
			testMode:   true,
			issuerIn:   "",
			wantIssuer: DefaultTestMTLSClusterIssuer,
			wantMTLS:   false,
		},
		{
			name:       "no mtls, no test mode leaves issuer empty",
			issuerIn:   "",
			wantIssuer: "",
			wantMTLS:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := Build(Input{
				WithMTLS:          tc.withMTLS,
				TestMode:          tc.testMode,
				MTLSClusterIssuer: tc.issuerIn,
			})
			if p.MTLSClusterIssuer != tc.wantIssuer {
				t.Fatalf("MTLSClusterIssuer = %q, want %q", p.MTLSClusterIssuer, tc.wantIssuer)
			}
			if p.WithMTLS != tc.wantMTLS {
				t.Fatalf("WithMTLS = %v, want %v", p.WithMTLS, tc.wantMTLS)
			}
		})
	}
}
