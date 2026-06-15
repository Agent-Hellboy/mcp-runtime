package platformauth

import (
	"errors"
	"net"
	"net/url"
	"strings"
)

var errDisallowedBaseURLHost = errors.New("base URL host is not allowed")

// resolveAuthURL builds the platform-api internal auth resolve endpoint from
// operator-configured service base URL (PLATFORM_API_URL), not request input.
func resolveAuthURL(base string) (string, error) {
	u, err := parseServiceBaseURL(base)
	if err != nil {
		return "", err
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/internal/auth/resolve"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func parseServiceBaseURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("base URL is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("base URL must use http or https")
	}
	if u.User != nil {
		return nil, errors.New("base URL must not include credentials")
	}
	host := strings.TrimSpace(u.Hostname())
	if host == "" {
		return nil, errors.New("base URL must include a host")
	}
	if err := validateServiceBaseURLHost(host); err != nil {
		return nil, err
	}
	return u, nil
}

func validateServiceBaseURLHost(host string) error {
	lower := strings.ToLower(strings.TrimSpace(host))
	switch lower {
	case "169.254.169.254", "metadata.google.internal":
		return errDisallowedBaseURLHost
	}
	if ip := net.ParseIP(lower); ip != nil && ip.Equal(net.ParseIP("169.254.169.254")) {
		return errDisallowedBaseURLHost
	}
	return nil
}
