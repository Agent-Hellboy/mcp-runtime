package serviceutil

import (
	"context"
	"encoding/json"
	"errors"
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
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: refuseOIDCDowngrade}
	response, err := client.Do(request)
	if err != nil {
		return "", transientOIDCError{fmt.Errorf("fetch OIDC discovery metadata: %w", err)}
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		err := fmt.Errorf("OIDC discovery returned HTTP %d", response.StatusCode)
		if response.StatusCode >= http.StatusInternalServerError || response.StatusCode == http.StatusTooManyRequests {
			return "", transientOIDCError{err}
		}
		return "", err
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
	// Signing keys decide which tokens are trusted, so an HTTPS issuer must not
	// hand them out over plain HTTP. HTTP stays possible only for an HTTP
	// issuer, which is a local test setup.
	if issuerURL.Scheme == "https" && jwksURL.Scheme != "https" {
		return "", fmt.Errorf("OIDC discovery jwks_uri %q must use https for an https issuer", metadata.JWKSURI)
	}
	return jwksURL.String(), nil
}

// refuseOIDCDowngrade stops a discovery redirect that leaves https, which
// would let an on-path attacker substitute the metadata and its signing keys.
func refuseOIDCDowngrade(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 {
		return fmt.Errorf("OIDC discovery stopped after %d redirects", len(via))
	}
	if len(via) > 0 && via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
		return fmt.Errorf("OIDC discovery refused redirect from https to %s", req.URL.Scheme)
	}
	return nil
}

// transientOIDCError marks discovery failures worth retrying: the provider
// was unreachable or answered 5xx/429. A mismatched issuer or bad metadata is
// a configuration error and fails immediately.
type transientOIDCError struct{ err error }

func (e transientOIDCError) Error() string { return e.err.Error() }
func (e transientOIDCError) Unwrap() error { return e.err }

// DiscoverOIDCJWKSURLWithRetry retries transient discovery failures with
// exponential backoff, so a service starting while its identity provider is
// briefly unreachable does not crash-loop on the first attempt.
func DiscoverOIDCJWKSURLWithRetry(ctx context.Context, issuer string, attempts int, initialBackoff time.Duration) (string, error) {
	if attempts < 1 {
		attempts = 1
	}
	backoff := initialBackoff
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		jwksURL, err := DiscoverOIDCJWKSURL(ctx, issuer)
		if err == nil {
			return jwksURL, nil
		}
		lastErr = err
		var transient transientOIDCError
		if !errors.As(err, &transient) || attempt == attempts {
			break
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
	}
	return "", lastErr
}
