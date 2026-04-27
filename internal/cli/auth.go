// This file implements `mcp-runtime auth` for platform (non-kubeconfig) identity:
// store API base URL and token for Sentinel API and optional registry host.

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"golang.org/x/term"

	"mcp-runtime/pkg/authfile"
)

// authAPITestHook, if set, runs instead of the default API probe (unit tests only).
var authAPITestHook func(ctx context.Context, apiBaseURL, token string) error

// authHTTPDoHook, if set, runs HTTP requests instead of the default client (unit tests only).
var authHTTPDoHook func(req *http.Request) (*http.Response, error)

// NewAuthCmd is the `auth` command (login, logout, status) for platform credentials.
func NewAuthCmd(logger *zap.Logger) *cobra.Command {
	m := &authManager{logger: logger}
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Log in to the platform API and manage saved credentials",
		Long: `Authenticate to the Sentinel platform using email/password or an API token (not Kubernetes).

Use this for day-to-day deploy and registry-related flows. Cluster install and admin work
use Kubernetes and the cluster commands, not this command.

The token is stored in a local file (mode 0600) under the user config directory, unless you set ` + authfile.EnvAPIToken + `.

Optional environment:
  ` + authfile.EnvAPIURL + `      default API base for login, e.g. https://platform.example.com
  ` + authfile.EnvAPIToken + `    use this token for API calls; overrides a saved file
  MCP_RUNTIME_CONFIG_DIR    override the config directory (mainly for tests)`,
	}

	cmd.AddCommand(m.newAuthLoginCmd())
	cmd.AddCommand(m.newAuthLogoutCmd())
	cmd.AddCommand(m.newAuthStatusCmd())

	return cmd
}

type authManager struct {
	logger *zap.Logger
}

type loginFlags struct {
	apiURL         string
	email          string
	password       string
	token          string
	tokenFromStdin bool
	registryHost   string
	skipVerify     bool
}

func (m *authManager) newAuthLoginCmd() *cobra.Command {
	var f loginFlags
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Save a platform API token and optional registry host",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return m.runAuthLogin(cmd, f)
		},
	}

	cmd.Flags().StringVar(&f.apiURL, "api-url", os.Getenv(authfile.EnvAPIURL), "Sentinel API base URL (scheme and host, no /api path)")
	cmd.Flags().StringVar(&f.email, "email", "", "Platform account email for password login")
	cmd.Flags().StringVar(&f.password, "password", "", "Platform account password (prefer interactive prompt or token auth in shared shells)")
	cmd.Flags().StringVar(&f.token, "token", "", "API token (or use --token-stdin, or the interactive prompt)")
	cmd.Flags().BoolVar(&f.tokenFromStdin, "token-stdin", false, "Read the token from stdin (non-interactive)")
	cmd.Flags().StringVar(&f.registryHost, "registry-host", "", "Optional host:port for the platform image registry for later use with docker")
	cmd.Flags().BoolVar(&f.skipVerify, "skip-verify", false, "Store credentials without calling the API to validate the token")

	return cmd
}

func (m *authManager) runAuthLogin(cmd *cobra.Command, f loginFlags) error {
	stdout := io.Writer(os.Stdout)
	stderr := io.Writer(os.Stderr)
	if cmd != nil {
		stdout = cmd.OutOrStdout()
		stderr = cmd.ErrOrStderr()
	}

	apiURL := strings.TrimSpace(f.apiURL)
	if apiURL == "" {
		return newWithSentinel(ErrFieldRequired, "api URL is required (set --api-url or "+authfile.EnvAPIURL+")")
	}
	apiURL = normalizePlatformAPIBaseURL(apiURL)
	if apiURL == "" {
		return newWithSentinel(ErrFieldRequired, "api URL must include scheme and host")
	}

	var token, loginRole string
	if strings.TrimSpace(f.email) != "" || strings.TrimSpace(f.password) != "" {
		if strings.TrimSpace(f.email) == "" || strings.TrimSpace(f.password) == "" {
			return newWithSentinel(ErrFieldRequired, "email and password are both required for password login")
		}
		tok, role, err := loginPlatformPassword(context.Background(), apiURL, f.email, f.password)
		if err != nil {
			return newWithSentinel(nil, fmt.Sprintf("platform login failed: %v", err))
		}
		token = tok
		loginRole = role
		f.skipVerify = true
	} else if f.tokenFromStdin {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return newWithSentinel(nil, fmt.Sprintf("read stdin: %v", err))
		}
		token = strings.TrimSpace(string(b))
	} else if strings.TrimSpace(f.token) != "" {
		token = strings.TrimSpace(f.token)
	} else {
		stdinFD, err := terminalFD(os.Stdin.Fd())
		if err != nil || !term.IsTerminal(stdinFD) {
			return newWithSentinel(ErrFieldRequired, "not a TTY: pass --token, --token-stdin, or run in an interactive terminal")
		}
		fmt.Fprint(stderr, "Enter platform API token: ")
		tok, err := term.ReadPassword(stdinFD)
		fmt.Fprintln(stderr)
		if err != nil {
			return newWithSentinel(nil, fmt.Sprintf("read token: %v", err))
		}
		token = strings.TrimSpace(string(tok))
	}
	if token == "" {
		return newWithSentinel(ErrFieldRequired, "token is required")
	}

	if !f.skipVerify {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var err error
		if authAPITestHook != nil {
			err = authAPITestHook(ctx, apiURL, token)
		} else {
			err = verifyPlatformAPIToken(ctx, apiURL, token)
		}
		if err != nil {
			return newWithSentinel(nil, fmt.Sprintf("API token could not be verified: %v", err))
		}
	}

	path, err := authfile.FilePath()
	if err != nil {
		return err
	}
	c := &authfile.Credentials{
		APIBaseURL:   apiURL,
		Token:        token,
		Role:         loginRole,
		RegistryHost: strings.TrimSpace(f.registryHost),
	}
	if err := authfile.Save(path, c); err != nil {
		return err
	}
	if m.logger != nil {
		m.logger.Info("saved platform credentials", zap.String("api", apiURL), zap.String("path", path))
	}
	fmt.Fprintf(stdout, "Platform credentials saved to %s\n", path)
	if c.RegistryHost != "" {
		fmt.Fprintf(stdout, "Registry host recorded: %s\n", c.RegistryHost)
	}
	return nil
}

func loginPlatformPassword(ctx context.Context, apiBaseURL, email, password string) (token, role string, err error) {
	body, err := json.Marshal(map[string]string{"email": strings.TrimSpace(email), "password": password})
	if err != nil {
		return "", "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	u := normalizePlatformAPIBaseURL(apiBaseURL) + "/api/auth/login"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("content-type", "application/json")
	var resp *http.Response
	if authHTTPDoHook != nil {
		resp, err = authHTTPDoHook(req)
	} else {
		resp, err = (&http.Client{Timeout: 30 * time.Second}).Do(req)
	}
	if err != nil {
		return "", "", err
	}
	defer drainAndCloseBody(resp.Body)
	var out struct {
		AccessToken string `json:"access_token"`
		User        struct {
			Role string `json:"role"`
		} `json:"user"`
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", "", err
	}
	if strings.TrimSpace(out.AccessToken) == "" {
		return "", "", errors.New("login response did not include access_token")
	}
	return strings.TrimSpace(out.AccessToken), strings.TrimSpace(out.User.Role), nil
}

func terminalFD(fd uintptr) (int, error) {
	if fd > uintptr(math.MaxInt) {
		return 0, errors.New("file descriptor out of range")
	}
	return int(fd), nil
}

func (m *authManager) newAuthLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Delete saved platform credentials on this machine",
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := authfile.FilePath()
			if err != nil {
				return err
			}
			if err := authfile.Remove(path); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Logged out from the platform (local credentials removed).")
			return nil
		},
	}
}

func (m *authManager) newAuthStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show whether platform API credentials are configured",
		RunE: func(cmd *cobra.Command, _ []string) error {
			stdout := cmd.OutOrStdout()
			stderr := cmd.ErrOrStderr()
			if t := strings.TrimSpace(os.Getenv(authfile.EnvAPIToken)); t != "" {
				fmt.Fprintln(stdout, "A platform API token is set in "+authfile.EnvAPIToken+" and overrides any saved file.")
				if b := strings.TrimSpace(os.Getenv(authfile.EnvAPIURL)); b == "" {
					fmt.Fprintln(stderr, "Note: "+authfile.EnvAPIURL+" is not set. Commands that need a base URL require it (or a saved `mcp-runtime auth login`).")
				}
			} else {
				p, perr := authfile.FilePath()
				if perr == nil {
					if _, fErr := os.Stat(p); fErr == nil {
						fmt.Fprintln(stdout, "Credentials file: "+p)
					} else {
						fmt.Fprintln(stdout, "Credentials file: "+p+" (not present)")
					}
				}
			}
			tok, api, src, rerr := authfile.ResolveToken()
			if rerr != nil {
				if errors.Is(rerr, authfile.ErrNotFound) {
					fmt.Fprintln(stdout, "Not logged in. Run `mcp-runtime auth login` or set "+authfile.EnvAPIToken+".")
					return nil
				}
				return rerr
			}
			fmt.Fprintln(stdout, "Status: have platform API token")
			fmt.Fprintln(stdout, "  source:", src)
			if api != "" {
				fmt.Fprintln(stdout, "  API base URL:", api)
			} else {
				fmt.Fprintln(stdout, "  API base URL: (set --api-url on login or "+authfile.EnvAPIURL+" if using "+authfile.EnvAPIToken+" only)")
			}
			if c, cErr := fileCredentialsIfRelevant(); cErr == nil && c != nil {
				if c.RegistryHost != "" {
					fmt.Fprintln(stdout, "  saved registry host:", c.RegistryHost)
				}
				if c.Role != "" {
					fmt.Fprintln(stdout, "  role (from saved file):", c.Role)
				}
			}
			fmt.Fprintln(stdout, "  token (masked):", authfile.MaskToken(tok))
			return nil
		},
	}
}

// fileCredentialsIfRelevant returns saved-file credentials when not using the env override.
func fileCredentialsIfRelevant() (*authfile.Credentials, error) {
	if strings.TrimSpace(os.Getenv(authfile.EnvAPIToken)) != "" {
		return nil, nil
	}
	path, err := authfile.FilePath()
	if err != nil {
		return nil, err
	}
	return authfile.Load(path)
}

// verifyPlatformAPIToken issues GET /api/auth/me to confirm the key is accepted.
func verifyPlatformAPIToken(ctx context.Context, apiBaseURL, token string) error {
	s := normalizePlatformAPIBaseURL(apiBaseURL)
	u := s + "/api/auth/me"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("x-api-key", token)
	req.Header.Set("authorization", "Bearer "+token)
	var resp *http.Response
	if authHTTPDoHook != nil {
		resp, err = authHTTPDoHook(req)
	} else {
		client := &http.Client{Timeout: 30 * time.Second}
		resp, err = client.Do(req)
	}
	if err != nil {
		return err
	}
	defer drainAndCloseBody(resp.Body)
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("server rejected the token (HTTP %d)", resp.StatusCode)
	case http.StatusNotFound:
		return fmt.Errorf("API URL may be wrong (path returned HTTP 404, expected %q)", u)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("verify request failed: HTTP %d", resp.StatusCode)
}

func normalizePlatformAPIBaseURL(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimRight(s, "/")
	if strings.HasSuffix(strings.ToLower(s), "/api") {
		s = strings.TrimSpace(s[:len(s)-len("/api")])
		s = strings.TrimRight(s, "/")
	}
	return s
}

func drainAndCloseBody(body io.ReadCloser) {
	if body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, body)
	_ = body.Close()
}
