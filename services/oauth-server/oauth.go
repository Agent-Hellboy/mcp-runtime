package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"html"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

const (
	authorizationCodeTTL = time.Minute
	accessTokenTTL       = 10 * time.Minute
	refreshTokenTTL      = 30 * 24 * time.Hour
	maxCIMDBody          = 64 << 10
)

type oauthHandler struct {
	issuer                 *url.URL
	issuerStr              string
	store                  *oauthStore
	private                *rsa.PrivateKey
	keyID                  string
	client                 *http.Client
	allowedRedirectSchemes map[string]struct{}
}

type authorizeRequest struct {
	ClientID            string
	RedirectURI         string
	ResponseType        string
	Scope               string
	State               string
	Resource            string
	CodeChallenge       string
	CodeChallengeMethod string
}

type registrationRequest struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	ClientURI               string   `json:"client_uri,omitempty"`
	SoftwareID              string   `json:"software_id,omitempty"`
}

type tokenError struct {
	Error       string
	Description string
	Status      int
}

func newOAuthHandler(issuer *url.URL, key *rsa.PrivateKey, store *oauthStore) *oauthHandler {
	return newOAuthHandlerWithRedirectSchemes(issuer, key, store, nil)
}

func newOAuthHandlerWithRedirectSchemes(issuer *url.URL, key *rsa.PrivateKey, store *oauthStore, allowedSchemes map[string]struct{}) *oauthHandler {
	issuerCopy := *issuer
	issuerCopy.RawQuery = ""
	issuerCopy.Fragment = ""
	issuerCopy.Path = strings.TrimRight(issuerCopy.Path, "/")
	issuerCopy.RawPath = ""
	return &oauthHandler{
		issuer:                 &issuerCopy,
		issuerStr:              issuerCopy.String(),
		store:                  store,
		private:                key,
		keyID:                  "mcp-runtime-oauth-1",
		allowedRedirectSchemes: allowedSchemes,
		client: &http.Client{
			Timeout:   5 * time.Second,
			Transport: &http.Transport{DialContext: safeCIMDDialContext},
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (h *oauthHandler) endpoint(name string) string {
	base := strings.TrimRight(h.issuerStr, "/")
	return base + "/" + name
}

func (h *oauthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/health" || r.URL.Path == "/live" || r.URL.Path == "/ready" {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
		return
	}
	if r.URL.Path == h.discoveryPath("oauth-authorization-server") || r.URL.Path == h.discoveryPath("openid-configuration") || r.URL.Path == h.discoveryPath("openid-configuration-append") {
		h.handleMetadata(w, r)
		return
	}
	switch r.URL.Path {
	case h.endpointPath("authorize"), "/authorize":
		h.handleAuthorize(w, r)
	case h.endpointPath("token"), "/token":
		h.handleToken(w, r)
	case h.endpointPath("register"), "/register":
		h.handleRegister(w, r)
	case h.endpointPath("jwks.json"), "/jwks.json":
		h.handleJWKS(w, r)
	default:
		h.writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
	}
}

func (h *oauthHandler) endpointPath(name string) string {
	return h.issuer.EscapedPath() + "/" + name
}

func (h *oauthHandler) discoveryPath(kind string) string {
	path := strings.Trim(h.issuer.EscapedPath(), "/")
	if path == "" {
		switch kind {
		case "oauth-authorization-server":
			return "/.well-known/oauth-authorization-server"
		case "openid-configuration":
			return "/.well-known/openid-configuration"
		}
	}
	if kind == "oauth-authorization-server" {
		return "/.well-known/oauth-authorization-server/" + path
	}
	if kind == "openid-configuration" {
		return "/.well-known/openid-configuration/" + path
	}
	return "/" + path + "/.well-known/openid-configuration"
}

func (h *oauthHandler) metadata() map[string]any {
	return map[string]any{
		"issuer":                                         h.issuerStr,
		"authorization_endpoint":                         h.endpoint("authorize"),
		"token_endpoint":                                 h.endpoint("token"),
		"jwks_uri":                                       h.endpoint("jwks.json"),
		"registration_endpoint":                          h.endpoint("register"),
		"response_types_supported":                       []string{"code"},
		"grant_types_supported":                          []string{"authorization_code", "refresh_token"},
		"token_endpoint_auth_methods_supported":          []string{"none", "client_secret_basic", "client_secret_post"},
		"code_challenge_methods_supported":               []string{"S256"},
		"scopes_supported":                               []string{"mcp"},
		"client_id_metadata_document_supported":          true,
		"authorization_response_iss_parameter_supported": true,
	}
}

func (h *oauthHandler) handleMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		h.writeError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	h.writeJSON(w, http.StatusOK, h.metadata())
}

func (h *oauthHandler) handleJWKS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		h.writeError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
		return
	}
	n := base64.RawURLEncoding.EncodeToString(h.private.PublicKey.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(h.private.PublicKey.E)).Bytes())
	h.writeJSON(w, http.StatusOK, map[string]any{"keys": []any{map[string]string{
		"kty": "RSA", "n": n, "e": e, "use": "sig", "alg": "RS256", "kid": h.keyID,
	}}})
}

func (h *oauthHandler) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		h.writeError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
		return
	}
	var req registrationRequest
	decoder := json.NewDecoder(io.LimitReader(r.Body, 128<<10))
	if err := decoder.Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_client_metadata", "request body must be JSON")
		return
	}
	if req.ClientName == "" {
		req.ClientName = "MCP client"
	}
	if len(req.RedirectURIs) == 0 || len(req.RedirectURIs) > 20 {
		h.writeError(w, http.StatusBadRequest, "invalid_redirect_uri", "redirect_uris is required")
		return
	}
	if req.ResponseTypes == nil {
		req.ResponseTypes = []string{"code"}
	}
	if req.GrantTypes == nil {
		req.GrantTypes = []string{"authorization_code", "refresh_token"}
	}
	if req.TokenEndpointAuthMethod == "" {
		req.TokenEndpointAuthMethod = "none"
	}
	if err := h.validateRegistration(req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_client_metadata", err.Error())
		return
	}
	clientID := "mcp_" + randomString(24)
	clientSecret := ""
	secretHash := ""
	if req.TokenEndpointAuthMethod != "none" {
		clientSecret = randomString(32)
		hash, err := bcrypt.GenerateFromPassword([]byte(clientSecret), bcrypt.DefaultCost)
		if err != nil {
			h.writeError(w, http.StatusInternalServerError, "server_error", "could not create client")
			return
		}
		secretHash = string(hash)
	}
	client := oauthClient{ID: clientID, SecretHash: secretHash, Name: req.ClientName, RedirectURIs: req.RedirectURIs, GrantTypes: req.GrantTypes, ResponseTypes: req.ResponseTypes, TokenEndpointAuthMode: req.TokenEndpointAuthMethod}
	if err := h.store.saveClient(r.Context(), client); err != nil {
		h.writeError(w, http.StatusInternalServerError, "server_error", "could not persist client")
		return
	}
	response := map[string]any{
		"client_id":                  clientID,
		"client_name":                req.ClientName,
		"redirect_uris":              req.RedirectURIs,
		"grant_types":                req.GrantTypes,
		"response_types":             req.ResponseTypes,
		"token_endpoint_auth_method": req.TokenEndpointAuthMethod,
		"client_id_issued_at":        time.Now().Unix(),
	}
	if clientSecret != "" {
		response["client_secret"] = clientSecret
		response["client_secret_expires_at"] = int64(0)
	}
	w.Header().Set("Cache-Control", "no-store")
	h.writeJSON(w, http.StatusCreated, response)
}

func validateRegistration(req registrationRequest) error {
	return validateRegistrationWithRedirectSchemes(req, nil)
}

func (h *oauthHandler) validateRegistration(req registrationRequest) error {
	return validateRegistrationWithRedirectSchemes(req, h.allowedRedirectSchemes)
}

func validateRegistrationWithRedirectSchemes(req registrationRequest, allowedSchemes map[string]struct{}) error {
	for _, redirect := range req.RedirectURIs {
		if err := validateRedirectURIWithSchemes(redirect, allowedSchemes); err != nil {
			return err
		}
	}
	if !contains(req.ResponseTypes, "code") || len(req.ResponseTypes) != 1 {
		return errors.New("only response_type=code is supported")
	}
	for _, grant := range req.GrantTypes {
		if grant != "authorization_code" && grant != "refresh_token" {
			return fmt.Errorf("unsupported grant_type %q", grant)
		}
	}
	if !contains(req.GrantTypes, "authorization_code") {
		return errors.New("authorization_code grant is required")
	}
	switch req.TokenEndpointAuthMethod {
	case "none", "client_secret_basic", "client_secret_post":
		return nil
	default:
		return fmt.Errorf("unsupported token endpoint auth method %q", req.TokenEndpointAuthMethod)
	}
}

func validateRedirectURI(value string) error {
	return validateRedirectURIWithSchemes(value, nil)
}

func validateRedirectURIWithSchemes(value string, allowedSchemes map[string]struct{}) error {
	u, err := url.Parse(value)
	if err != nil || !u.IsAbs() || u.Fragment != "" || u.User != nil {
		return fmt.Errorf("redirect URI must be absolute and must not contain a fragment")
	}
	if u.Scheme == "https" || (u.Scheme == "http" && isLoopbackHost(u.Hostname())) {
		return nil
	}
	if _, allowed := allowedSchemes[strings.ToLower(u.Scheme)]; allowed && u.Host != "" && u.Path != "" && u.Opaque == "" {
		return nil
	}
	if _, allowed := allowedSchemes[strings.ToLower(u.Scheme)]; allowed {
		return fmt.Errorf("redirect URI %q must include a host and path", value)
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && isLoopbackHost(u.Hostname())) {
		return fmt.Errorf("redirect URI %q must use HTTPS, except for loopback HTTP clients", value)
	}
	return nil
}

func (h *oauthHandler) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		h.writeError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
		return
	}
	request, client, err := h.authorizeRequest(r)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if r.Method == http.MethodGet {
		h.renderLogin(w, request)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.redirectError(w, request, "invalid_request", "invalid form")
		return
	}
	transactionCookie, cookieErr := r.Cookie("mcp_oauth_tx")
	if cookieErr != nil || transactionCookie.Value == "" || subtle.ConstantTimeCompare([]byte(transactionCookie.Value), []byte(r.Form.Get("oauth_transaction"))) != 1 {
		h.redirectError(w, request, "invalid_request", "authorization transaction expired")
		return
	}
	if r.Form.Get("consent") != "approve" {
		h.redirectError(w, request, "access_denied", "the resource owner denied the request")
		return
	}
	user, ok, err := h.store.authenticatePassword(r.Context(), r.Form.Get("email"), r.Form.Get("password"))
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "server_error", "authentication service unavailable")
		return
	}
	if !ok {
		h.renderLoginError(w, request, "Invalid email or password")
		return
	}
	codeValue := randomString(48)
	code := authorizationCode{Hash: hashOpaque(codeValue), ClientID: client.ID, RedirectURI: request.RedirectURI, UserID: user.ID, CodeChallenge: request.CodeChallenge, CodeChallengeMode: request.CodeChallengeMethod, Resource: request.Resource, Scope: request.Scope, ExpiresAt: time.Now().Add(authorizationCodeTTL)}
	if err := h.store.saveCode(r.Context(), code); err != nil {
		h.writeError(w, http.StatusInternalServerError, "server_error", "could not create authorization code")
		return
	}
	redirect := addQuery(request.RedirectURI, map[string]string{"code": codeValue, "state": request.State, "iss": h.issuerStr})
	h.redirect(w, redirect)
}

func (h *oauthHandler) authorizeRequest(r *http.Request) (authorizeRequest, oauthClient, error) {
	values := r.URL.Query()
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			return authorizeRequest{}, oauthClient{}, errors.New("invalid form")
		}
		values = r.Form
	}
	clientID := values.Get("client_id")
	client, ok, err := h.store.client(r.Context(), clientID)
	if err != nil {
		return authorizeRequest{}, oauthClient{}, errors.New("client lookup failed")
	}
	if !ok && strings.HasPrefix(clientID, "https://") {
		client, err = h.resolveCIMD(r.Context(), clientID)
		if err != nil {
			return authorizeRequest{}, oauthClient{}, err
		}
		ok = true
	}
	if !ok {
		return authorizeRequest{}, oauthClient{}, errors.New("unknown client_id")
	}
	request := authorizeRequest{ClientID: clientID, RedirectURI: values.Get("redirect_uri"), ResponseType: values.Get("response_type"), Scope: normalizeScope(values.Get("scope")), State: values.Get("state"), Resource: values.Get("resource"), CodeChallenge: values.Get("code_challenge"), CodeChallengeMethod: values.Get("code_challenge_method")}
	if request.ResponseType != "code" || !contains(client.ResponseTypes, "code") {
		return authorizeRequest{}, oauthClient{}, errors.New("response_type=code is required")
	}
	if !redirectURIRegistered(client.RedirectURIs, request.RedirectURI) {
		return authorizeRequest{}, oauthClient{}, errors.New("redirect_uri is not registered")
	}
	if err := validateResource(request.Resource); err != nil {
		return authorizeRequest{}, oauthClient{}, err
	}
	if err := validateScope(request.Scope); err != nil {
		return authorizeRequest{}, oauthClient{}, err
	}
	if !validPKCEChallenge(request.CodeChallenge) || request.CodeChallengeMethod != "S256" {
		return authorizeRequest{}, oauthClient{}, errors.New("S256 PKCE code_challenge is required")
	}
	return request, client, nil
}

// redirectURIRegistered applies OAuth's exact redirect URI matching rules,
// including the RFC 8252 exception for native-app loopback redirects: the
// port may be selected dynamically, but every other URI component must match.
func redirectURIRegistered(registered []string, requested string) bool {
	for _, candidate := range registered {
		if candidate == requested {
			return true
		}
		if loopbackRedirectURIsMatch(candidate, requested) {
			return true
		}
	}
	return false
}

func loopbackRedirectURIsMatch(registered, requested string) bool {
	registeredURL, registeredErr := url.Parse(registered)
	requestedURL, requestedErr := url.Parse(requested)
	if registeredErr != nil || requestedErr != nil ||
		registeredURL.Scheme != "http" || requestedURL.Scheme != "http" ||
		!isLoopbackHost(registeredURL.Hostname()) || !isLoopbackHost(requestedURL.Hostname()) ||
		!strings.EqualFold(registeredURL.Hostname(), requestedURL.Hostname()) {
		return false
	}
	return registeredURL.User == nil && requestedURL.User == nil &&
		registeredURL.Path == requestedURL.Path &&
		registeredURL.RawPath == requestedURL.RawPath &&
		registeredURL.RawQuery == requestedURL.RawQuery &&
		registeredURL.ForceQuery == requestedURL.ForceQuery &&
		registeredURL.Fragment == requestedURL.Fragment &&
		registeredURL.Opaque == requestedURL.Opaque
}

func (h *oauthHandler) renderLogin(w http.ResponseWriter, request authorizeRequest) {
	h.renderLoginPage(w, request, "")
}

func (h *oauthHandler) renderLoginError(w http.ResponseWriter, request authorizeRequest, message string) {
	h.renderLoginPage(w, request, message)
}

func (h *oauthHandler) renderLoginPage(w http.ResponseWriter, request authorizeRequest, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
	transaction := randomString(32)
	http.SetCookie(w, &http.Cookie{Name: "mcp_oauth_tx", Value: transaction, Path: h.issuer.EscapedPath(), HttpOnly: true, Secure: h.issuer.Scheme == "https", SameSite: http.SameSiteLaxMode, MaxAge: 300})
	fields := ""
	for key, value := range map[string]string{"client_id": request.ClientID, "redirect_uri": request.RedirectURI, "response_type": request.ResponseType, "scope": request.Scope, "state": request.State, "resource": request.Resource, "code_challenge": request.CodeChallenge, "code_challenge_method": request.CodeChallengeMethod, "oauth_transaction": transaction} {
		fields += `<input type="hidden" name="` + html.EscapeString(key) + `" value="` + html.EscapeString(value) + `">`
	}
	if message != "" {
		message = `<p role="alert">` + html.EscapeString(message) + `</p>`
	}
	_, _ = fmt.Fprintf(w, `<!doctype html><html><head><meta charset="utf-8"><title>MCP authorization</title></head><body><main><h1>Authorize MCP client</h1>%s<form method="post" action="%s">%s<label>Email <input name="email" type="email" autocomplete="username" required></label><label>Password <input name="password" type="password" autocomplete="current-password" required></label><button name="consent" value="approve" type="submit">Approve</button><button name="consent" value="deny" type="submit">Deny</button></form></main></body></html>`, message, html.EscapeString(h.endpoint("authorize")), fields)
}

func (h *oauthHandler) redirectError(w http.ResponseWriter, request authorizeRequest, code, description string) {
	params := map[string]string{"error": code, "error_description": description, "state": request.State, "iss": h.issuerStr}
	h.redirect(w, addQuery(request.RedirectURI, params))
}

func (h *oauthHandler) handleToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		h.writeTokenError(w, tokenError{"invalid_request", "token endpoint requires POST", http.StatusBadRequest})
		return
	}
	if err := r.ParseForm(); err != nil {
		h.writeTokenError(w, tokenError{"invalid_request", "invalid form", http.StatusBadRequest})
		return
	}
	client, err := h.authenticateClient(r)
	if err != nil {
		h.writeTokenError(w, tokenError{"invalid_client", "client authentication failed", http.StatusUnauthorized})
		return
	}
	switch r.Form.Get("grant_type") {
	case "authorization_code":
		h.exchangeCode(w, r, client)
	case "refresh_token":
		h.exchangeRefresh(w, r, client)
	default:
		h.writeTokenError(w, tokenError{"unsupported_grant_type", "grant_type is not supported", http.StatusBadRequest})
	}
}

func (h *oauthHandler) authenticateClient(r *http.Request) (oauthClient, error) {
	clientID, clientSecret := r.Form.Get("client_id"), r.Form.Get("client_secret")
	basic := false
	if username, password, ok := r.BasicAuth(); ok {
		basic = true
		if clientID != "" && clientID != username {
			return oauthClient{}, errors.New("conflicting client id")
		}
		clientID, clientSecret = username, password
	}
	client, ok, err := h.store.client(r.Context(), clientID)
	if err != nil {
		return oauthClient{}, errors.New("unknown client")
	}
	if !ok && strings.HasPrefix(clientID, "https://") {
		client, err = h.resolveCIMD(r.Context(), clientID)
		if err != nil {
			return oauthClient{}, err
		}
		ok = true
	}
	if !ok {
		return oauthClient{}, errors.New("unknown client")
	}
	if client.TokenEndpointAuthMode == "none" {
		if basic || clientSecret != "" {
			return oauthClient{}, errors.New("public client supplied a secret")
		}
		return client, nil
	}
	if client.TokenEndpointAuthMode == "client_secret_basic" && !basic {
		return oauthClient{}, errors.New("HTTP Basic client authentication is required")
	}
	if client.TokenEndpointAuthMode == "client_secret_post" && basic {
		return oauthClient{}, errors.New("client_secret_post authentication is required")
	}
	if clientSecret == "" || bcrypt.CompareHashAndPassword([]byte(client.SecretHash), []byte(clientSecret)) != nil {
		return oauthClient{}, errors.New("bad client secret")
	}
	return client, nil
}

func (h *oauthHandler) exchangeCode(w http.ResponseWriter, r *http.Request, client oauthClient) {
	codeValue := r.Form.Get("code")
	code, ok, err := h.store.consumeCode(r.Context(), hashOpaque(codeValue), client.ID, r.Form.Get("redirect_uri"), r.Form.Get("code_verifier"))
	if err != nil || !ok {
		h.writeTokenError(w, tokenError{"invalid_grant", "authorization code is invalid or expired", http.StatusBadRequest})
		return
	}
	h.issueTokens(w, r.Context(), client, code.UserID, code.Resource, code.Scope)
}

func (h *oauthHandler) exchangeRefresh(w http.ResponseWriter, r *http.Request, client oauthClient) {
	if !contains(client.GrantTypes, "refresh_token") {
		h.writeTokenError(w, tokenError{"unauthorized_client", "client is not registered for refresh_token", http.StatusBadRequest})
		return
	}
	refreshValue := r.Form.Get("refresh_token")
	token, ok, err := h.store.consumeRefresh(r.Context(), hashOpaque(refreshValue), client.ID)
	if err != nil || !ok {
		h.writeTokenError(w, tokenError{"invalid_grant", "refresh token is invalid or expired", http.StatusBadRequest})
		return
	}
	h.issueTokens(w, r.Context(), client, token.UserID, token.Resource, token.Scope)
}

func (h *oauthHandler) issueTokens(w http.ResponseWriter, ctx context.Context, client oauthClient, userID, resource, scope string) {
	now := time.Now()
	access, err := h.signAccessToken(client.ID, userID, resource, scope, now)
	if err != nil {
		h.writeTokenError(w, tokenError{"server_error", "could not sign access token", http.StatusInternalServerError})
		return
	}
	refreshValue := randomString(48)
	response := map[string]any{"access_token": access, "token_type": "Bearer", "expires_in": int(accessTokenTTL.Seconds()), "scope": scope}
	if contains(client.GrantTypes, "refresh_token") {
		if err := h.store.saveRefresh(ctx, refreshToken{Hash: hashOpaque(refreshValue), ClientID: client.ID, UserID: userID, Resource: resource, Scope: scope, ExpiresAt: now.Add(refreshTokenTTL)}); err != nil {
			h.writeTokenError(w, tokenError{"server_error", "could not persist refresh token", http.StatusInternalServerError})
			return
		}
		response["refresh_token"] = refreshValue
	}
	w.Header().Set("Cache-Control", "no-store")
	h.writeJSON(w, http.StatusOK, response)
}

func (h *oauthHandler) signAccessToken(clientID, userID, resource, scope string, now time.Time) (string, error) {
	claims := jwt.MapClaims{"iss": h.issuerStr, "sub": userID, "aud": resource, "scope": scope, "client_id": clientID, "azp": clientID, "jti": uuid.NewString(), "iat": now.Unix(), "exp": now.Add(accessTokenTTL).Unix()}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = h.keyID
	return token.SignedString(h.private)
}

func (h *oauthHandler) resolveCIMD(ctx context.Context, clientID string) (oauthClient, error) {
	u, err := url.Parse(clientID)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || isPrivateHost(u.Hostname()) {
		return oauthClient{}, errors.New("client_id metadata document URL is not allowed")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, clientID, nil)
	if err != nil {
		return oauthClient{}, errors.New("invalid client_id metadata document URL")
	}
	req.Header.Set("Accept", "application/json")
	response, err := h.client.Do(req)
	if err != nil {
		return oauthClient{}, errors.New("could not fetch client metadata")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Request.URL.String() != clientID {
		return oauthClient{}, errors.New("client metadata document unavailable")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxCIMDBody+1))
	if err != nil || len(body) > maxCIMDBody {
		return oauthClient{}, errors.New("client metadata document is too large")
	}
	var metadata struct {
		ClientID              string   `json:"client_id"`
		ClientName            string   `json:"client_name"`
		RedirectURIs          []string `json:"redirect_uris"`
		GrantTypes            []string `json:"grant_types"`
		ResponseTypes         []string `json:"response_types"`
		TokenEndpointAuthMode string   `json:"token_endpoint_auth_method"`
	}
	if err := json.Unmarshal(body, &metadata); err != nil || metadata.ClientID != clientID {
		return oauthClient{}, errors.New("client metadata document has an invalid client_id")
	}
	if metadata.ClientName == "" {
		metadata.ClientName = clientID
	}
	if metadata.ResponseTypes == nil {
		metadata.ResponseTypes = []string{"code"}
	}
	if metadata.GrantTypes == nil {
		metadata.GrantTypes = []string{"authorization_code", "refresh_token"}
	}
	if metadata.TokenEndpointAuthMode == "" {
		metadata.TokenEndpointAuthMode = "none"
	}
	registration := registrationRequest{ClientName: metadata.ClientName, RedirectURIs: metadata.RedirectURIs, GrantTypes: metadata.GrantTypes, ResponseTypes: metadata.ResponseTypes, TokenEndpointAuthMethod: metadata.TokenEndpointAuthMode}
	if err := h.validateRegistration(registration); err != nil {
		return oauthClient{}, err
	}
	return oauthClient{ID: clientID, Name: metadata.ClientName, RedirectURIs: metadata.RedirectURIs, GrantTypes: metadata.GrantTypes, ResponseTypes: metadata.ResponseTypes, TokenEndpointAuthMode: "none"}, nil
}

func verifyPKCE(verifier, challenge string) bool {
	if len(verifier) < 43 || len(verifier) > 128 || !isUnreserved(verifier) {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:]) == challenge
}

func validPKCEChallenge(value string) bool {
	return len(value) == 43 && isUnreserved(value)
}

func isUnreserved(value string) bool {
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') {
			continue
		}
		if strings.ContainsRune("-._~", character) {
			continue
		}
		return false
	}
	return value != ""
}

func validateResource(value string) error {
	u, err := url.Parse(value)
	if err != nil || !u.IsAbs() || u.Host == "" || u.Fragment != "" {
		return errors.New("resource must be an absolute URI without a fragment")
	}
	return nil
}

func normalizeScope(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func validateScope(value string) error {
	if len(value) > 1024 {
		return errors.New("scope is too long")
	}
	for _, token := range strings.Fields(value) {
		if !isOAuthScopeToken(token) {
			return errors.New("scope contains an invalid character")
		}
	}
	return nil
}

func isOAuthScopeToken(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || strings.ContainsRune("!#$%&'()*+-.^_`|~:", character) {
			continue
		}
		return false
	}
	return true
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func randomString(length int) string {
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz-._~"
	buffer := make([]byte, length)
	if _, err := rand.Read(buffer); err != nil {
		panic(err)
	}
	for i := range buffer {
		buffer[i] = alphabet[int(buffer[i])%len(alphabet)]
	}
	return string(buffer)
}

func addQuery(raw string, params map[string]string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	query := u.Query()
	for key, value := range params {
		if value != "" {
			query.Set(key, value)
		}
	}
	u.RawQuery = query.Encode()
	return u.String()
}

func (h *oauthHandler) redirect(w http.ResponseWriter, target string) {
	w.Header().Set("Location", target)
	w.WriteHeader(http.StatusFound)
}

func (h *oauthHandler) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if w.Header().Get("Content-Length") == "" {
		_ = json.NewEncoder(w).Encode(value)
	}
}

func (h *oauthHandler) writeError(w http.ResponseWriter, status int, code, description string) {
	h.writeJSON(w, status, map[string]string{"error": code, "error_description": description})
}

func (h *oauthHandler) writeTokenError(w http.ResponseWriter, errorValue tokenError) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(errorValue.Status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": errorValue.Error, "error_description": errorValue.Description})
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	ip := net.ParseIP(host)
	return host == "localhost" || strings.HasSuffix(host, ".localhost") || (ip != nil && ip.IsLoopback())
}

func isPrivateHost(host string) bool {
	if isLoopbackHost(host) || host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast())
}

func safeCIMDDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addresses, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	dialer := &net.Dialer{}
	for _, ip := range addresses {
		if isPrivateHost(ip.String()) {
			continue
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
	}
	return nil, errors.New("client metadata host resolves to a private address")
}

func parsePrivateKey(data []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("private key is not PEM encoded")
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if rsaKey, ok := key.(*rsa.PrivateKey); ok {
			return rsaKey, nil
		}
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	return nil, errors.New("private key is not an RSA PKCS#1 or PKCS#8 key")
}
