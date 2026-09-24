package serviceutil

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DiscoverOIDCJWKSURL resolves an issuer's OIDC discovery document and returns
// its jwks_uri. The caller can use an explicitly configured URL instead when
// a provider does not publish standard discovery metadata.
func DiscoverOIDCJWKSURL(ctx context.Context, issuer string) (string, error) {
	issuer = strings.TrimSpace(issuer)
	issuerURL, err := url.Parse(issuer)
	if err != nil || issuerURL.Scheme == "" || issuerURL.Host == "" || issuerURL.RawQuery != "" || issuerURL.Fragment != "" || (issuerURL.Scheme != "https" && issuerURL.Scheme != "http") {
		return "", fmt.Errorf("OIDC issuer must be an absolute HTTP(S) URL")
	}
	discoveryURL := *issuerURL
	discoveryURL.Path = strings.TrimRight(discoveryURL.Path, "/") + "/.well-known/openid-configuration"
	discoveryURL.RawPath = ""
	discoveryURL.RawQuery = ""
	discoveryURL.Fragment = ""

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL.String(), nil)
	if err != nil {
		return "", fmt.Errorf("create OIDC discovery request: %w", err)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("fetch OIDC discovery metadata: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("OIDC discovery returned HTTP %d", response.StatusCode)
	}
	var metadata struct {
		Issuer  string `json:"issuer"`
		JWKSURI string `json:"jwks_uri"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&metadata); err != nil {
		return "", fmt.Errorf("decode OIDC discovery metadata: %w", err)
	}
	if strings.TrimSpace(metadata.Issuer) != issuer {
		return "", fmt.Errorf("OIDC discovery issuer %q does not match configured issuer %q", metadata.Issuer, issuer)
	}
	jwksURL, err := url.Parse(strings.TrimSpace(metadata.JWKSURI))
	if err != nil || jwksURL.Scheme == "" || jwksURL.Host == "" || (jwksURL.Scheme != "https" && jwksURL.Scheme != "http") {
		return "", fmt.Errorf("OIDC discovery metadata has no valid jwks_uri")
	}
	return jwksURL.String(), nil
}
