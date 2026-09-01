package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"golang.org/x/crypto/bcrypt"
)

func testOAuthHandler(t *testing.T) (*oauthHandler, *oauthStore) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	store := newMemoryOAuthStore(oauthUser{ID: "user-1", Email: "test@example.com"})
	hash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	store.password["test@example.com"] = string(hash)
	issuer, _ := url.Parse("https://auth.example.com/oauth")
	return newOAuthHandler(issuer, key, store), store
}

func TestMetadataAndJWKS(t *testing.T) {
	h, _ := testOAuthHandler(t)
	server := httptest.NewServer(h)
	defer server.Close()

	request := httptest.NewRequest(http.MethodGet, "https://auth.example.com/.well-known/oauth-authorization-server/oauth", nil)
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("metadata status = %d", recorder.Code)
	}
	var metadata map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata["issuer"] != "https://auth.example.com/oauth" ||
		metadata["client_id_metadata_document_supported"] != true ||
		metadata["registration_endpoint"] != "https://auth.example.com/oauth/register" ||
		metadata["authorization_response_iss_parameter_supported"] != true {
		t.Fatalf("unexpected metadata: %#v", metadata)
	}

	request = httptest.NewRequest(http.MethodGet, "https://auth.example.com/oauth/jwks.json", nil)
	recorder = httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"kty":"RSA"`) {
		t.Fatalf("unexpected JWKS response: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestMetadataDiscoveryPathOrderIsServed(t *testing.T) {
	h, _ := testOAuthHandler(t)
	for _, path := range []string{
		"/.well-known/oauth-authorization-server/oauth",
		"/.well-known/openid-configuration/oauth",
		"/oauth/.well-known/openid-configuration",
	} {
		recorder := httptest.NewRecorder()
		h.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "https://auth.example.com"+path, nil))
		if recorder.Code != http.StatusOK {
			t.Errorf("discovery path %s status = %d", path, recorder.Code)
		}
	}
}

func TestPublicClientAuthorizationCodePKCEAndRefresh(t *testing.T) {
	h, store := testOAuthHandler(t)
	store.teamIDs["user-1"] = []string{"team-acme"}
	registerBody := `{"client_name":"Example","redirect_uris":["http://127.0.0.1:9000/callback"],"token_endpoint_auth_method":"none"}`
	register := httptest.NewRequest(http.MethodPost, "https://auth.example.com/oauth/register", strings.NewReader(registerBody))
	register.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, register)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("registration status = %d: %s", recorder.Code, recorder.Body.String())
	}
	var client struct {
		ID string `json:"client_id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &client); err != nil {
		t.Fatal(err)
	}
	verifier := "test-verifier-with-enough-entropy-123456789"
	challenge := pkceChallenge(verifier)
	params := url.Values{"client_id": {client.ID}, "redirect_uri": {"http://127.0.0.1:9000/callback"}, "response_type": {"code"}, "resource": {"https://mcp.example.com/server"}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}, "scope": {"mcp"}, "state": {"state-1"}}
	get := httptest.NewRequest(http.MethodGet, "https://auth.example.com/oauth/authorize?"+params.Encode(), nil)
	loginPage := httptest.NewRecorder()
	h.ServeHTTP(loginPage, get)
	if loginPage.Code != http.StatusOK || !strings.Contains(loginPage.Body.String(), "Authorize MCP client") {
		t.Fatalf("login page status = %d: %s", loginPage.Code, loginPage.Body.String())
	}
	rootAlias := httptest.NewRequest(http.MethodGet, "https://auth.example.com/authorize?"+params.Encode(), nil)
	rootLoginPage := httptest.NewRecorder()
	h.ServeHTTP(rootLoginPage, rootAlias)
	if rootLoginPage.Code != http.StatusOK || !strings.Contains(rootLoginPage.Body.String(), "Authorize MCP client") {
		t.Fatalf("root authorize alias status = %d: %s", rootLoginPage.Code, rootLoginPage.Body.String())
	}
	cookies := loginPage.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected authorization transaction cookie, got %v", cookies)
	}
	form := url.Values{}
	for key, values := range params {
		form.Set(key, values[0])
	}
	form.Set("email", "test@example.com")
	form.Set("password", "password")
	form.Set("consent", "approve")
	form.Set("oauth_transaction", cookies[0].Value)
	post := httptest.NewRequest(http.MethodPost, "https://auth.example.com/oauth/authorize", strings.NewReader(form.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.AddCookie(cookies[0])
	approval := httptest.NewRecorder()
	h.ServeHTTP(approval, post)
	if approval.Code != http.StatusFound {
		t.Fatalf("approval status = %d: %s", approval.Code, approval.Body.String())
	}
	redirect, err := url.Parse(approval.Header().Get("Location"))
	if err != nil || redirect.Query().Get("state") != "state-1" || redirect.Query().Get("iss") != "https://auth.example.com/oauth" {
		t.Fatalf("unexpected redirect: %s", approval.Header().Get("Location"))
	}
	tokenForm := url.Values{"grant_type": {"authorization_code"}, "client_id": {client.ID}, "code": {redirect.Query().Get("code")}, "redirect_uri": {"http://127.0.0.1:9000/callback"}, "code_verifier": {verifier}}
	tokenRequest := httptest.NewRequest(http.MethodPost, "https://auth.example.com/oauth/token", strings.NewReader(tokenForm.Encode()))
	tokenRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokenResponse := httptest.NewRecorder()
	h.ServeHTTP(tokenResponse, tokenRequest)
	if tokenResponse.Code != http.StatusOK {
		t.Fatalf("token status = %d: %s", tokenResponse.Code, tokenResponse.Body.String())
	}
	var tokens map[string]any
	if err := json.Unmarshal(tokenResponse.Body.Bytes(), &tokens); err != nil {
		t.Fatal(err)
	}
	if tokens["token_type"] != "Bearer" || tokens["access_token"] == nil || tokens["refresh_token"] == nil {
		t.Fatalf("unexpected token response: %#v", tokens)
	}
	parsed, _, err := new(jwt.Parser).ParseUnverified(tokens["access_token"].(string), jwt.MapClaims{})
	if err != nil || parsed == nil || parsed.Claims.(jwt.MapClaims)["team_id"] != "team-acme" {
		t.Fatalf("access token team claim = %#v, want team-acme", parsed)
	}

	refresh := url.Values{"grant_type": {"refresh_token"}, "client_id": {client.ID}, "refresh_token": {tokens["refresh_token"].(string)}}
	refreshRequest := httptest.NewRequest(http.MethodPost, "https://auth.example.com/oauth/token", strings.NewReader(refresh.Encode()))
	refreshRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	refreshResponse := httptest.NewRecorder()
	h.ServeHTTP(refreshResponse, refreshRequest)
	if refreshResponse.Code != http.StatusOK {
		t.Fatalf("refresh status = %d: %s", refreshResponse.Code, refreshResponse.Body.String())
	}
	secondUse := httptest.NewRecorder()
	h.ServeHTTP(secondUse, refreshRequest)
	if secondUse.Code != http.StatusBadRequest {
		t.Fatalf("refresh token reuse status = %d", secondUse.Code)
	}
}

func TestInvalidRedirectDoesNotReceiveAuthorizationResponse(t *testing.T) {
	h, _ := testOAuthHandler(t)
	body := `{"redirect_uris":["https://client.example/callback"]}`
	request := httptest.NewRequest(http.MethodPost, "https://auth.example.com/oauth/register", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatal(recorder.Body.String())
	}
	var client struct {
		ID string `json:"client_id"`
	}
	_ = json.Unmarshal(recorder.Body.Bytes(), &client)
	query := url.Values{"client_id": {client.ID}, "redirect_uri": {"https://attacker.example/callback"}, "response_type": {"code"}}
	request = httptest.NewRequest(http.MethodGet, "https://auth.example.com/oauth/authorize?"+query.Encode(), nil)
	recorder = httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	if recorder.Code == http.StatusFound || recorder.Header().Get("Location") != "" {
		t.Fatalf("untrusted redirect was used: %d %s", recorder.Code, recorder.Header().Get("Location"))
	}
}

func TestDCRReportsRejectedHTTPRedirect(t *testing.T) {
	h, _ := testOAuthHandler(t)
	redirect := "http://192.0.2.10:61584/callback"
	request := httptest.NewRequest(http.MethodPost, "https://auth.example.com/oauth/register", strings.NewReader(`{"redirect_uris":["`+redirect+`"]}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), redirect) {
		t.Fatalf("unexpected rejected redirect response: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestCustomRedirectSchemesRequireExplicitAllowList(t *testing.T) {
	redirect := "cursor://anysphere.cursor-mcp/oauth/callback"
	if err := validateRedirectURI(redirect); err == nil {
		t.Fatal("custom redirect scheme was accepted without an allow-list")
	}
	if err := validateRedirectURIWithSchemes(redirect, parseRedirectSchemes("cursor")); err != nil {
		t.Fatalf("allow-listed Cursor redirect was rejected: %v", err)
	}
	if err := validateRedirectURIWithSchemes("vscode://anysphere.cursor-mcp/oauth/callback", parseRedirectSchemes("cursor")); err == nil {
		t.Fatal("non-allow-listed custom redirect scheme was accepted")
	}
	if allowed := parseRedirectSchemes("cursor,http,https,not a scheme"); len(allowed) != 1 {
		t.Fatalf("unexpected redirect scheme allow-list: %#v", allowed)
	}
}

func TestDCRAcceptsAllowListedCursorRedirect(t *testing.T) {
	h, _ := testOAuthHandler(t)
	h.allowedRedirectSchemes = parseRedirectSchemes("cursor")
	request := httptest.NewRequest(http.MethodPost, "https://auth.example.com/oauth/register", strings.NewReader(`{"redirect_uris":["cursor://anysphere.cursor-mcp/oauth/callback"],"application_type":"native"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("Cursor registration status = %d: %s", recorder.Code, recorder.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["application_type"] != "native" {
		t.Fatalf("application_type = %#v, want native", response["application_type"])
	}
}

func TestDCRRejectsUnknownApplicationType(t *testing.T) {
	h, _ := testOAuthHandler(t)
	request := httptest.NewRequest(http.MethodPost, "https://auth.example.com/oauth/register", strings.NewReader(`{"redirect_uris":["http://127.0.0.1:9000/callback"],"application_type":"desktop"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "application_type") {
		t.Fatalf("unexpected application_type rejection: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestLoopbackHTTPRedirectsAllowLocalhostNames(t *testing.T) {
	for _, host := range []string{"localhost", "LOCALHOST", "localhost.", "app.localhost", "127.0.0.1", "[::1]"} {
		if !isLoopbackHost(strings.Trim(host, "[]")) {
			t.Errorf("isLoopbackHost(%q) = false", host)
		}
	}
	for _, host := range []string{"client.example.com", "192.168.1.10", "0.0.0.0"} {
		if isLoopbackHost(host) {
			t.Errorf("isLoopbackHost(%q) = true", host)
		}
	}
}

func TestLoginAttemptLimiterBlocksAndResets(t *testing.T) {
	limiter := newLoginAttemptLimiter()
	keys := []string{"email:test@example.com", "ip:127.0.0.1"}
	for i := 0; i < loginFailureLimit; i++ {
		if !limiter.allowed(keys...) {
			t.Fatalf("attempt %d was blocked before the limit", i+1)
		}
		limiter.failure(keys...)
	}
	if limiter.allowed(keys...) {
		t.Fatal("attempt was not blocked after the failure limit")
	}
	limiter.success(keys...)
	if !limiter.allowed(keys...) {
		t.Fatal("successful authentication did not reset the limiter")
	}
}

func TestLoopbackRedirectURIAllowsDynamicPortOnly(t *testing.T) {
	tests := []struct {
		name       string
		registered string
		requested  string
		want       bool
	}{
		{name: "ipv4 dynamic port", registered: "http://127.0.0.1/callback/instance", requested: "http://127.0.0.1:61584/callback/instance", want: true},
		{name: "ipv6 dynamic port", registered: "http://[::1]/callback/instance", requested: "http://[::1]:61584/callback/instance", want: true},
		{name: "localhost dynamic port", registered: "http://localhost/callback/instance", requested: "http://localhost:61584/callback/instance", want: true},
		{name: "path mismatch", registered: "http://127.0.0.1/callback/instance", requested: "http://127.0.0.1:61584/callback/other", want: false},
		{name: "host mismatch", registered: "http://127.0.0.1/callback/instance", requested: "http://localhost:61584/callback/instance", want: false},
		{name: "public host", registered: "https://client.example/callback", requested: "https://client.example:61584/callback", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := redirectURIRegistered([]string{test.registered}, test.requested); got != test.want {
				t.Fatalf("redirectURIRegistered(%q, %q) = %v, want %v", test.registered, test.requested, got, test.want)
			}
		})
	}
}

func TestCIMDClientIsResolvedForAuthorizationAndToken(t *testing.T) {
	h, _ := testOAuthHandler(t)
	clientID := "https://client.example/.well-known/oauth-client"
	h.client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"client_id":"https://client.example/.well-known/oauth-client","client_name":"CIMD","redirect_uris":["https://client.example/callback"],"response_types":["code"],"grant_types":["authorization_code","refresh_token"],"token_endpoint_auth_method":"none"}`)), Request: request}, nil
	}), Timeout: 5 * time.Second}
	challenge := pkceChallenge("cimd-verifier")
	query := url.Values{"client_id": {clientID}, "redirect_uri": {"https://client.example/callback"}, "response_type": {"code"}, "resource": {"https://mcp.example.com/server"}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}}
	request := httptest.NewRequest(http.MethodGet, "https://auth.example.com/oauth/authorize?"+query.Encode(), nil)
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("CIMD status = %d: %s", recorder.Code, recorder.Body.String())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
