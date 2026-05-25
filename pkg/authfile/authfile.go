// Package authfile stores local platform login credentials for the mcp-runtime CLI
// (API base URL, token, optional registry host). It is the foundation for user-facing
// flows that do not use kubeconfig.
package authfile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"mcp-runtime/pkg/runtimeconfig"
)

// ErrNotFound is returned when no credentials file exists or it is empty.
var ErrNotFound = errors.New("not logged in: no saved credentials")

// ErrInvalid is returned when a credentials file exists but is malformed.
var ErrInvalid = errors.New("saved credentials are invalid")

// ConfigDir is the per-user MCP Runtime configuration directory.
func ConfigDir() (string, error) {
	return runtimeconfig.Dir()
}

// FilePath returns the default path to the MCP Runtime config file.
func FilePath() (string, error) {
	return runtimeconfig.DefaultFile()
}

// Credentials holds platform API identities saved after `mcp-runtime auth login`.
type Credentials struct {
	Current      string                       `json:"current,omitempty"`
	Accounts     map[string]CredentialAccount `json:"accounts,omitempty"`
	APIBaseURL   string                       `json:"api_url,omitempty"`
	Token        string                       `json:"token,omitempty"`
	Role         string                       `json:"role,omitempty"`
	RegistryHost string                       `json:"registry_host,omitempty"`
	UpdatedAt    time.Time                    `json:"updated_at"`
}

// CredentialAccount is one saved platform identity.
type CredentialAccount struct {
	APIBaseURL   string    `json:"api_url"`
	Token        string    `json:"token"`
	Role         string    `json:"role,omitempty"`
	RegistryHost string    `json:"registry_host,omitempty"`
	Username     string    `json:"username,omitempty"`
	UpdatedAt    time.Time `json:"updated_at,omitempty"`
}

// Load reads credentials from path. If the file is missing, returns [ErrNotFound].
func Load(path string) (*Credentials, error) {
	// #nosec G304 -- path is a direct CLI/user-configured credentials file location.
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if len(b) == 0 {
		return nil, ErrNotFound
	}
	var c Credentials
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("%w: parse credentials: %w", ErrInvalid, err)
	}
	if _, _, err := c.SelectedAccount(""); err != nil {
		return nil, fmt.Errorf("%w: api_url and token are required", ErrInvalid)
	}
	return &c, nil
}

// Save writes credentials to path with restrictive permissions (0600).
func Save(path string, c *Credentials) error {
	if c == nil {
		return errors.New("nil credentials")
	}
	c.UpdatedAt = time.Now().UTC()
	if c.Accounts == nil {
		c.Accounts = map[string]CredentialAccount{}
	}
	if len(c.Accounts) == 0 {
		profile := NormalizeProfileName(c.Current)
		if profile == "" {
			profile = "default"
		}
		c.Current = profile
		c.Accounts[profile] = CredentialAccount{
			APIBaseURL:   strings.TrimSpace(c.APIBaseURL),
			Token:        strings.TrimSpace(c.Token),
			Role:         strings.TrimSpace(c.Role),
			RegistryHost: strings.TrimSpace(c.RegistryHost),
			UpdatedAt:    c.UpdatedAt,
		}
	}
	account, profile, err := c.SelectedAccount(c.Current)
	if err != nil {
		return err
	}
	c.Current = profile
	c.APIBaseURL = account.APIBaseURL
	c.Token = account.Token
	c.Role = account.Role
	c.RegistryHost = account.RegistryHost
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "credentials-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

// SaveProfile saves one named identity and marks it as current.
func SaveProfile(path, profile string, account CredentialAccount) error {
	profile = NormalizeProfileName(profile)
	if profile == "" {
		profile = "default"
	}
	account.APIBaseURL = strings.TrimSpace(account.APIBaseURL)
	account.Token = strings.TrimSpace(account.Token)
	account.Role = strings.TrimSpace(account.Role)
	account.RegistryHost = strings.TrimSpace(account.RegistryHost)
	account.Username = strings.TrimSpace(account.Username)
	if account.APIBaseURL == "" || account.Token == "" {
		return errors.New("api_url and token are required")
	}
	c := &Credentials{}
	if existing, err := Load(path); err == nil && existing != nil {
		c = existing
	}
	if c.Accounts == nil {
		c.Accounts = map[string]CredentialAccount{}
	}
	if len(c.Accounts) == 0 && strings.TrimSpace(c.APIBaseURL) != "" && strings.TrimSpace(c.Token) != "" {
		existingProfile := NormalizeProfileName(c.Current)
		if existingProfile == "" {
			existingProfile = "default"
		}
		c.Accounts[existingProfile] = CredentialAccount{
			APIBaseURL:   strings.TrimSpace(c.APIBaseURL),
			Token:        strings.TrimSpace(c.Token),
			Role:         strings.TrimSpace(c.Role),
			RegistryHost: strings.TrimSpace(c.RegistryHost),
			UpdatedAt:    c.UpdatedAt,
		}
	}
	now := time.Now().UTC()
	account.UpdatedAt = now
	c.Accounts[profile] = account
	c.Current = profile
	c.UpdatedAt = now
	return Save(path, c)
}

// SelectProfile marks a saved identity as current.
func SelectProfile(path, profile string) error {
	c, err := Load(path)
	if err != nil {
		return err
	}
	profile = NormalizeProfileName(profile)
	if _, resolved, err := c.SelectedAccount(profile); err != nil {
		return err
	} else {
		c.Current = resolved
	}
	return Save(path, c)
}

// SelectedAccount returns the requested saved identity, or the current identity
// when profile is empty.
func (c *Credentials) SelectedAccount(profile string) (CredentialAccount, string, error) {
	if c == nil {
		return CredentialAccount{}, "", errors.New("nil credentials")
	}
	profile = NormalizeProfileName(profile)
	if profile == "" {
		profile = NormalizeProfileName(c.Current)
	}
	if profile != "" && len(c.Accounts) > 0 {
		if account, ok := c.Accounts[profile]; ok {
			if account.APIBaseURL == "" || account.Token == "" {
				return CredentialAccount{}, "", errors.New("api_url and token are required")
			}
			return account, profile, nil
		}
		return CredentialAccount{}, "", fmt.Errorf("profile %q not found", profile)
	}
	if profile == "" && len(c.Accounts) > 0 {
		names := c.ProfileNames()
		if len(names) == 1 {
			account := c.Accounts[names[0]]
			if account.APIBaseURL == "" || account.Token == "" {
				return CredentialAccount{}, "", errors.New("api_url and token are required")
			}
			return account, names[0], nil
		}
		return CredentialAccount{}, "", errors.New("current profile is not set")
	}
	account := CredentialAccount{
		APIBaseURL:   strings.TrimSpace(c.APIBaseURL),
		Token:        strings.TrimSpace(c.Token),
		Role:         strings.TrimSpace(c.Role),
		RegistryHost: strings.TrimSpace(c.RegistryHost),
		UpdatedAt:    c.UpdatedAt,
	}
	if account.APIBaseURL == "" || account.Token == "" {
		return CredentialAccount{}, "", errors.New("api_url and token are required")
	}
	if profile == "" {
		profile = "default"
	}
	return account, profile, nil
}

// ProfileNames returns saved profile names in stable order.
func (c *Credentials) ProfileNames() []string {
	if c == nil || len(c.Accounts) == 0 {
		return nil
	}
	names := make([]string, 0, len(c.Accounts))
	for name := range c.Accounts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// NormalizeProfileName converts a user-facing profile label to a stable key.
func NormalizeProfileName(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '@' || r == '.' || r == '_' || r == '-'
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-._@")
}

// Remove deletes the credentials file at path if it exists.
func Remove(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// MaskToken returns a non-reversible display form (last 4 runes, if any).
func MaskToken(s string) string {
	const minShown = 4
	r := []rune(s)
	if len(r) == 0 {
		return "(empty)"
	}
	if len(r) <= minShown {
		return "****"
	}
	return "****" + string(r[len(r)-minShown:])
}

// EnvAPIToken is the environment variable for a platform API token without using a saved file.
// #nosec G101 -- environment variable name only; no secret value is embedded.
const EnvAPIToken = "MCP_PLATFORM_API_TOKEN"

// EnvAPIURL is the default platform API base URL (e.g. https://platform.example.com).
const EnvAPIURL = "MCP_PLATFORM_API_URL"

// EnvAPIProfile selects a saved platform API profile from the MCP Runtime config file.
const EnvAPIProfile = "MCP_PLATFORM_API_PROFILE"

// ResolveToken returns a token and API base URL: first from the environment, then the default credentials file.
// If apiBase is empty, callers may still have a token from [EnvAPIToken] only.
func ResolveToken() (token, apiBase, source string, err error) {
	if t := strings.TrimSpace(os.Getenv(EnvAPIToken)); t != "" {
		return t, strings.TrimSpace(os.Getenv(EnvAPIURL)), EnvAPIToken, nil
	}
	path, err := FilePath()
	if err != nil {
		return "", "", "", err
	}
	c, err := Load(path)
	if err != nil {
		return "", "", "", err
	}
	account, profile, err := c.SelectedAccount(os.Getenv(EnvAPIProfile))
	if err != nil {
		return "", "", "", err
	}
	source = "credentials file"
	if profile != "" {
		source += " profile " + profile
	}
	return account.Token, account.APIBaseURL, source, nil
}

// CurrentRegistryHost returns the registry host saved with the active platform login.
func CurrentRegistryHost() string {
	path, err := FilePath()
	if err != nil {
		return ""
	}
	c, err := Load(path)
	if err != nil {
		return ""
	}
	account, _, err := c.SelectedAccount(os.Getenv(EnvAPIProfile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(account.RegistryHost)
}
