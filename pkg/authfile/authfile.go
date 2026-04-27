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
	"strings"
	"time"
)

const subDir = "mcp-runtime"

// ErrNotFound is returned when no credentials file exists or it is empty.
var ErrNotFound = errors.New("not logged in: no saved credentials")

// ErrInvalid is returned when a credentials file exists but is malformed.
var ErrInvalid = errors.New("saved credentials are invalid")

// ConfigDir is the per-user mcp-runtime configuration directory. If the environment
// variable MCP_RUNTIME_CONFIG_DIR is set, that path is used (useful in tests).
func ConfigDir() (string, error) {
	if d := os.Getenv("MCP_RUNTIME_CONFIG_DIR"); d != "" {
		return d, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, subDir), nil
}

// FilePath returns the default path to credentials.json.
func FilePath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "credentials.json"), nil
}

// Credentials holds platform API identity saved after `mcp-runtime auth login`.
type Credentials struct {
	APIBaseURL   string    `json:"api_url"`
	Token        string    `json:"token"`
	Role         string    `json:"role,omitempty"`
	RegistryHost string    `json:"registry_host,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
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
		return nil, fmt.Errorf("%w: parse credentials: %v", ErrInvalid, err)
	}
	if c.Token == "" || c.APIBaseURL == "" {
		return nil, fmt.Errorf("%w: api_url and token are required", ErrInvalid)
	}
	return &c, nil
}

// Save writes credentials to path with restrictive permissions (0600).
func Save(path string, c *Credentials) error {
	if c == nil {
		return errors.New("nil credentials")
	}
	if c.Token == "" || c.APIBaseURL == "" {
		return errors.New("api_url and token are required")
	}
	c.UpdatedAt = time.Now().UTC()
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
	return c.Token, c.APIBaseURL, "credentials file", nil
}
