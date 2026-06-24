package identity

import "testing"

func TestSessionSPIFFEID(t *testing.T) {
	t.Parallel()
	got := SessionSPIFFEID("example.org", "team-a", "session-1")
	want := "spiffe://example.org/ns/team-a/session/session-1"
	if got != want {
		t.Fatalf("SessionSPIFFEID() = %q, want %q", got, want)
	}
}

func TestParseSessionSPIFFE(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		raw      string
		trust    string
		wantNS   string
		wantSess string
		wantOK   bool
	}{
		{
			name:     "valid",
			raw:      "spiffe://example.org/ns/team-a/session/session-1",
			trust:    "example.org",
			wantNS:   "team-a",
			wantSess: "session-1",
			wantOK:   true,
		},
		{
			name:   "wrong trust domain",
			raw:    "spiffe://attacker.org/ns/team-a/session/session-1",
			trust:  "example.org",
			wantOK: false,
		},
		{
			name:   "wrong path shape",
			raw:    "spiffe://example.org/user/alice/agent/codex",
			trust:  "example.org",
			wantOK: false,
		},
		{
			name:   "empty",
			raw:    "",
			trust:  "example.org",
			wantOK: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ns, sess, ok := ParseSessionSPIFFE(tc.raw, tc.trust)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if ns != tc.wantNS || sess != tc.wantSess {
				t.Fatalf("ParseSessionSPIFFE() = (%q, %q), want (%q, %q)", ns, sess, tc.wantNS, tc.wantSess)
			}
		})
	}
}
