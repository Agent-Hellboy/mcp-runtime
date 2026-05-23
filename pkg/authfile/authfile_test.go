package authfile

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResolveToken_PrefersEnv(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "config.json")
	_ = os.MkdirAll(d, 0o700)
	_ = os.WriteFile(p, []byte(`{"api_url":"https://file.example","token":"filetok"}`), 0o600)
	t.Setenv("MCP_RUNTIME_CONFIG_DIR", d)
	t.Setenv(EnvAPIToken, "envkey")
	t.Setenv(EnvAPIURL, "https://env.example")

	tok, api, src, err := ResolveToken()
	if err != nil {
		t.Fatal(err)
	}
	if tok != "envkey" || api != "https://env.example" || src != EnvAPIToken {
		t.Fatalf("got %q %q %q", tok, api, src)
	}
}

func TestConfigDir_RespectsEnv(t *testing.T) {
	d := t.TempDir()
	t.Setenv("MCP_RUNTIME_CONFIG_DIR", d)
	got, err := ConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != d {
		t.Fatalf("ConfigDir: got %q want %q", got, d)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	t.Parallel()
	d := t.TempDir()
	p := filepath.Join(d, "config.json")
	orig := &Credentials{
		APIBaseURL:   "https://platform.example.com",
		Token:        "secret-token-value",
		Role:         "user",
		RegistryHost: "registry.example.com",
		UpdatedAt:    time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := Save(p, orig); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(p); err != nil {
		t.Fatal(err)
	} else if st.Mode()&0o777 != 0o600 {
		t.Fatalf("file mode = %o, want 0600", st.Mode()&0o777)
	}
	loaded, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	// UpdatedAt is overwritten on Save; compare other fields
	if loaded.APIBaseURL != orig.APIBaseURL {
		t.Fatalf("APIBaseURL: %q", loaded.APIBaseURL)
	}
	if loaded.Token != orig.Token {
		t.Fatalf("Token mismatch")
	}
	if loaded.Role != orig.Role {
		t.Fatalf("Role: %q", loaded.Role)
	}
	if loaded.RegistryHost != orig.RegistryHost {
		t.Fatalf("RegistryHost: %q", loaded.RegistryHost)
	}
	account, profile, err := loaded.SelectedAccount("")
	if err != nil {
		t.Fatal(err)
	}
	if profile != "default" {
		t.Fatalf("profile = %q, want default", profile)
	}
	if account.Token != orig.Token {
		t.Fatalf("account token mismatch")
	}
}

func TestSaveProfilePreservesMultipleAccounts(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "config.json")
	if err := SaveProfile(p, "admin", CredentialAccount{APIBaseURL: "https://platform.example.com", Token: "admin-token", Role: "admin"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveProfile(p, "acme-owner", CredentialAccount{APIBaseURL: "https://platform.example.com", Token: "acme-token", Role: "user", RegistryHost: "registry.example.com"}); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Current != "acme-owner" {
		t.Fatalf("current = %q, want acme-owner", loaded.Current)
	}
	if names := loaded.ProfileNames(); len(names) != 2 || names[0] != "acme-owner" || names[1] != "admin" {
		t.Fatalf("profiles = %#v, want acme-owner/admin", names)
	}
	account, profile, err := loaded.SelectedAccount("admin")
	if err != nil {
		t.Fatal(err)
	}
	if profile != "admin" || account.Token != "admin-token" || account.Role != "admin" {
		t.Fatalf("admin account = %#v profile=%q", account, profile)
	}
	if loaded.Token != "acme-token" || loaded.APIBaseURL != "https://platform.example.com" {
		t.Fatalf("top-level active fields not mirrored: token=%q api=%q", loaded.Token, loaded.APIBaseURL)
	}
}

func TestResolveToken_UsesSelectedProfile(t *testing.T) {
	d := t.TempDir()
	t.Setenv("MCP_RUNTIME_CONFIG_DIR", d)
	t.Setenv(EnvAPIProfile, "admin")
	p := filepath.Join(d, "config.json")
	if err := SaveProfile(p, "admin", CredentialAccount{APIBaseURL: "https://platform.example.com", Token: "admin-token"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveProfile(p, "globex", CredentialAccount{APIBaseURL: "https://platform.example.com", Token: "globex-token"}); err != nil {
		t.Fatal(err)
	}
	tok, api, src, err := ResolveToken()
	if err != nil {
		t.Fatal(err)
	}
	if tok != "admin-token" || api != "https://platform.example.com" || src != "credentials file profile admin" {
		t.Fatalf("got token=%q api=%q src=%q", tok, api, src)
	}
}

func TestLoad_Missing(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "nope.json")
	_, err := Load(p)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Load() = %v, want ErrNotFound", err)
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	t.Parallel()
	d := t.TempDir()
	p := filepath.Join(d, "config.json")
	if err := os.WriteFile(p, []byte(`{"api_url"`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(p)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("Load() = %v, want ErrInvalid", err)
	}
	var cause *json.SyntaxError
	if !errors.As(err, &cause) {
		t.Fatalf("Load() = %v, want json syntax error in chain", err)
	}
}

func TestLoad_IncompleteCredentials(t *testing.T) {
	t.Parallel()
	d := t.TempDir()
	p := filepath.Join(d, "config.json")
	if err := os.WriteFile(p, []byte(`{"api_url":"https://platform.example.com"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(p)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("Load() = %v, want ErrInvalid", err)
	}
}

func TestMaskToken(t *testing.T) {
	t.Parallel()
	if g := MaskToken(""); g != "(empty)" {
		t.Fatalf("empty: %q", g)
	}
	if g := MaskToken("ab"); g != "****" {
		t.Fatalf("short2: %q", g)
	}
	if g := MaskToken("hello"); g != "****ello" {
		t.Fatalf("hello: %q", g)
	}
	if g := MaskToken("abcdefghijklmnop"); g != "****mnop" {
		t.Fatalf("long: %q", g)
	}
}
