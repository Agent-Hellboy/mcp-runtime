package plan

import "testing"

func TestBuildMTLSIssuerResolution(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		testMode   bool
		issuerIn   string
		wantIssuer string
	}{
		{
			name:       "test mode defaults the bundled CA",
			testMode:   true,
			issuerIn:   "",
			wantIssuer: DefaultTestMTLSClusterIssuer,
		},
		{
			name:       "external issuer is preserved",
			issuerIn:   "corp-spiffe-issuer",
			wantIssuer: "corp-spiffe-issuer",
		},
		{
			name:       "explicit bundled name is preserved (prod managed)",
			issuerIn:   DefaultTestMTLSClusterIssuer,
			wantIssuer: DefaultTestMTLSClusterIssuer,
		},
		{
			name:       "no test mode, no issuer leaves mtls off",
			issuerIn:   "",
			wantIssuer: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := Build(Input{
				TestMode:          tc.testMode,
				MTLSClusterIssuer: tc.issuerIn,
			})
			if p.MTLSClusterIssuer != tc.wantIssuer {
				t.Fatalf("MTLSClusterIssuer = %q, want %q", p.MTLSClusterIssuer, tc.wantIssuer)
			}
		})
	}
}
