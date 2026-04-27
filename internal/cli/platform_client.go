// HTTP client for the Sentinel platform API using auth from authfile.
// User-facing (non-kubeconfig) path for access, server list, and policy.

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
	mcpv1alpha1 "mcp-runtime/api/v1alpha1"
	sentinelaccess "mcp-runtime/pkg/access"
	"mcp-runtime/pkg/authfile"
)

const maxAPIBodyRead = 4 << 20

// errPlatformNoBaseURL is returned when a token exists but the API base URL is missing.
var errPlatformNoBaseURL = errors.New("set MCP_PLATFORM_API_URL or run mcp-runtime auth login with --api-url to use the platform API")

// platformClient calls the mcp-sentinel API with an API key.
type platformClient struct {
	baseURL   string
	token     string
	http      *http.Client
	apiPrefix string
}

// newPlatformClient returns a client when platform credentials and API base URL are configured.
// If the user is not logged in, returns [authfile.ErrNotFound] so the caller can fall back to kubectl.
func newPlatformClient() (*platformClient, error) {
	tok, base, _, err := authfile.ResolveToken()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(base) == "" {
		if strings.TrimSpace(tok) != "" {
			return nil, errPlatformNoBaseURL
		}
		return nil, authfile.ErrNotFound
	}
	return &platformClient{
		baseURL:   normalizePlatformAPIBaseURL(base),
		token:     tok,
		http:      &http.Client{Timeout: 2 * time.Minute},
		apiPrefix: "/api",
	}, nil
}

func (c *platformClient) do(ctx context.Context, method, relPath, query string, body io.Reader) (*http.Response, error) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, err
	}
	rel, err := url.Parse(c.apiPrefix + relPath)
	if err != nil {
		return nil, err
	}
	joined := u.ResolveReference(rel)
	if query != "" {
		joined.RawQuery = query
	}
	req, err := http.NewRequestWithContext(ctx, method, joined.String(), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", c.token)
	req.Header.Set("authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("content-type", "application/json")
	}
	return c.http.Do(req)
}

func listQuery(namespace string) string {
	v := url.Values{}
	if strings.TrimSpace(namespace) != "" {
		v.Set("namespace", namespace)
	}
	return v.Encode()
}

type grantsListResponse struct {
	Grants []sentinelaccess.GrantSummary `json:"grants"`
}

type sessionsListResponse struct {
	Sessions []sentinelaccess.SessionSummary `json:"sessions"`
}

type grantGetResponse struct {
	Grant sentinelaccess.GrantSummary `json:"grant"`
}

type sessionGetResponse struct {
	Session sentinelaccess.SessionSummary `json:"session"`
}

type grantAPIBody struct {
	Name          string                         `json:"name"`
	Namespace     string                         `json:"namespace"`
	ServerRef     sentinelaccess.ServerReference `json:"serverRef"`
	Subject       sentinelaccess.SubjectRef      `json:"subject"`
	MaxTrust      sentinelaccess.TrustLevel      `json:"maxTrust"`
	PolicyVersion string                         `json:"policyVersion,omitempty"`
	Disabled      *bool                          `json:"disabled,omitempty"`
	ToolRules     []sentinelaccess.ToolRule      `json:"toolRules"`
}

type sessionAPIBody struct {
	Name           string                         `json:"name"`
	Namespace      string                         `json:"namespace"`
	ServerRef      sentinelaccess.ServerReference `json:"serverRef"`
	Subject        sentinelaccess.SubjectRef      `json:"subject"`
	ConsentedTrust sentinelaccess.TrustLevel      `json:"consentedTrust"`
	ExpiresAt      *metav1.Time                   `json:"expiresAt,omitempty"`
	Revoked        *bool                          `json:"revoked,omitempty"`
	PolicyVersion  string                         `json:"policyVersion"`
}

func (c *platformClient) listGrants(ctx context.Context, namespace string) ([]sentinelaccess.GrantSummary, error) {
	resp, err := c.do(ctx, http.MethodGet, "/runtime/grants", listQuery(namespace), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := readBody(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, httpAPIError(resp.StatusCode, b)
	}
	var out grantsListResponse
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out.Grants, nil
}

func (c *platformClient) listSessions(ctx context.Context, namespace string) ([]sentinelaccess.SessionSummary, error) {
	resp, err := c.do(ctx, http.MethodGet, "/runtime/sessions", listQuery(namespace), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := readBody(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, httpAPIError(resp.StatusCode, b)
	}
	var out sessionsListResponse
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out.Sessions, nil
}

func (c *platformClient) getGrant(ctx context.Context, namespace, name string) (sentinelaccess.GrantSummary, error) {
	p := fmt.Sprintf("/runtime/grants/%s/%s", url.PathEscape(namespace), url.PathEscape(name))
	resp, err := c.do(ctx, http.MethodGet, p, "", nil)
	if err != nil {
		return sentinelaccess.GrantSummary{}, err
	}
	defer resp.Body.Close()
	b, err := readBody(resp.Body)
	if err != nil {
		return sentinelaccess.GrantSummary{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return sentinelaccess.GrantSummary{}, httpAPIError(resp.StatusCode, b)
	}
	var out grantGetResponse
	if err := json.Unmarshal(b, &out); err != nil {
		return sentinelaccess.GrantSummary{}, err
	}
	return out.Grant, nil
}

func (c *platformClient) getSession(ctx context.Context, namespace, name string) (sentinelaccess.SessionSummary, error) {
	p := fmt.Sprintf("/runtime/sessions/%s/%s", url.PathEscape(namespace), url.PathEscape(name))
	resp, err := c.do(ctx, http.MethodGet, p, "", nil)
	if err != nil {
		return sentinelaccess.SessionSummary{}, err
	}
	defer resp.Body.Close()
	b, err := readBody(resp.Body)
	if err != nil {
		return sentinelaccess.SessionSummary{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return sentinelaccess.SessionSummary{}, httpAPIError(resp.StatusCode, b)
	}
	var out sessionGetResponse
	if err := json.Unmarshal(b, &out); err != nil {
		return sentinelaccess.SessionSummary{}, err
	}
	return out.Session, nil
}

func (c *platformClient) postGrant(ctx context.Context, body grantAPIBody) error {
	js, err := json.Marshal(body)
	if err != nil {
		return err
	}
	resp, err := c.do(ctx, http.MethodPost, "/runtime/grants", "", bytes.NewReader(js))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := readBody(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return httpAPIError(resp.StatusCode, b)
	}
	return nil
}

func (c *platformClient) postSession(ctx context.Context, body sessionAPIBody) error {
	js, err := json.Marshal(body)
	if err != nil {
		return err
	}
	resp, err := c.do(ctx, http.MethodPost, "/runtime/sessions", "", bytes.NewReader(js))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := readBody(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return httpAPIError(resp.StatusCode, b)
	}
	return nil
}

func (c *platformClient) deleteGrant(ctx context.Context, namespace, name string) error {
	p := fmt.Sprintf("/runtime/grants/%s/%s", url.PathEscape(namespace), url.PathEscape(name))
	resp, err := c.do(ctx, http.MethodDelete, p, "", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := readBody(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return httpAPIError(resp.StatusCode, b)
	}
	return nil
}

func (c *platformClient) deleteSession(ctx context.Context, namespace, name string) error {
	p := fmt.Sprintf("/runtime/sessions/%s/%s", url.PathEscape(namespace), url.PathEscape(name))
	resp, err := c.do(ctx, http.MethodDelete, p, "", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := readBody(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return httpAPIError(resp.StatusCode, b)
	}
	return nil
}

func (c *platformClient) postGrantToggle(ctx context.Context, namespace, name, action string) error {
	p := fmt.Sprintf("/runtime/grants/%s/%s/%s", url.PathEscape(namespace), url.PathEscape(name), action)
	resp, err := c.do(ctx, http.MethodPost, p, "", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := readBody(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return httpAPIError(resp.StatusCode, b)
	}
	return nil
}

func (c *platformClient) postSessionToggle(ctx context.Context, namespace, name, action string) error {
	p := fmt.Sprintf("/runtime/sessions/%s/%s/%s", url.PathEscape(namespace), url.PathEscape(name), action)
	resp, err := c.do(ctx, http.MethodPost, p, "", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := readBody(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return httpAPIError(resp.StatusCode, b)
	}
	return nil
}

func (c *platformClient) applyAccessFromYAMLFile(ctx context.Context, path string) error {
	b, err := readFileAtPath(path)
	if err != nil {
		return err
	}
	decoder := k8syaml.NewYAMLOrJSONDecoder(bytes.NewReader(b), 4096)
	docIndex := 0
	for {
		var rawDoc map[string]any
		if err := decoder.Decode(&rawDoc); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return fmt.Errorf("decode %s document %d: %w", path, docIndex+1, err)
		}
		if len(rawDoc) == 0 {
			continue
		}
		docIndex++
		metaBytes, err := json.Marshal(rawDoc)
		if err != nil {
			return fmt.Errorf("encode %s document %d: %w", path, docIndex, err)
		}
		var meta struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(metaBytes, &meta); err != nil {
			return fmt.Errorf("parse %s document %d metadata: %w", path, docIndex, err)
		}
		switch strings.TrimSpace(meta.Kind) {
		case "MCPAccessGrant":
			var g mcpv1alpha1.MCPAccessGrant
			if err := json.Unmarshal(metaBytes, &g); err != nil {
				return fmt.Errorf("parse %s document %d grant: %w", path, docIndex, err)
			}
			if err := c.postGrant(ctx, grantFromV1(&g)); err != nil {
				return fmt.Errorf("apply %s document %d grant: %w", path, docIndex, err)
			}
		case "MCPAgentSession":
			var s mcpv1alpha1.MCPAgentSession
			if err := json.Unmarshal(metaBytes, &s); err != nil {
				return fmt.Errorf("parse %s document %d session: %w", path, docIndex, err)
			}
			if err := c.postSession(ctx, sessionFromV1(&s)); err != nil {
				return fmt.Errorf("apply %s document %d session: %w", path, docIndex, err)
			}
		default:
			return newWithSentinel(ErrFieldRequired, fmt.Sprintf("manifest document %d kind %q is not supported for platform apply (use MCPAccessGrant or MCPAgentSession)", docIndex, meta.Kind))
		}
	}
	if docIndex == 0 {
		return newWithSentinel(ErrFieldRequired, "manifest does not contain MCPAccessGrant or MCPAgentSession")
	}
	return nil
}

func grantFromV1(g *mcpv1alpha1.MCPAccessGrant) grantAPIBody {
	ns := g.Namespace
	if ns == "" {
		ns = sentinelaccess.DefaultMCPResourceNamespace
	}
	trust := sentinelaccess.TrustLevel(g.Spec.MaxTrust)
	rules := make([]sentinelaccess.ToolRule, 0, len(g.Spec.ToolRules))
	for _, tr := range g.Spec.ToolRules {
		rules = append(rules, sentinelaccess.ToolRule{
			Name:          tr.Name,
			Decision:      sentinelaccess.PolicyDecision(tr.Decision),
			RequiredTrust: sentinelaccess.TrustLevel(tr.RequiredTrust),
		})
	}
	dis := g.Spec.Disabled
	return grantAPIBody{
		Name:          g.Name,
		Namespace:     ns,
		ServerRef:     sentinelaccess.ServerReference{Name: g.Spec.ServerRef.Name, Namespace: g.Spec.ServerRef.Namespace},
		Subject:       sentinelaccess.SubjectRef{HumanID: g.Spec.Subject.HumanID, AgentID: g.Spec.Subject.AgentID},
		MaxTrust:      trust,
		PolicyVersion: g.Spec.PolicyVersion,
		Disabled:      &dis,
		ToolRules:     rules,
	}
}

func sessionFromV1(s *mcpv1alpha1.MCPAgentSession) sessionAPIBody {
	ns := s.Namespace
	if ns == "" {
		ns = sentinelaccess.DefaultMCPResourceNamespace
	}
	rev := s.Spec.Revoked
	return sessionAPIBody{
		Name:           s.Name,
		Namespace:      ns,
		ServerRef:      sentinelaccess.ServerReference{Name: s.Spec.ServerRef.Name, Namespace: s.Spec.ServerRef.Namespace},
		Subject:        sentinelaccess.SubjectRef{HumanID: s.Spec.Subject.HumanID, AgentID: s.Spec.Subject.AgentID},
		ConsentedTrust: sentinelaccess.TrustLevel(s.Spec.ConsentedTrust),
		PolicyVersion:  s.Spec.PolicyVersion,
		Revoked:        &rev,
		ExpiresAt:      s.Spec.ExpiresAt,
	}
}

func readBody(r io.Reader) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, maxAPIBodyRead))
}

func httpAPIError(status int, body []byte) error {
	var m map[string]string
	if err := json.Unmarshal(body, &m); err == nil {
		if e := m["error"]; e != "" {
			return fmt.Errorf("API %d: %s", status, e)
		}
	}
	s := strings.TrimSpace(string(body))
	if s == "" {
		return fmt.Errorf("API returned HTTP %d", status)
	}
	return fmt.Errorf("API %d: %s", status, s)
}

// --- runtime / servers (GET) ------------------------------------------------

type serverListItem struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	Ready     string            `json:"ready"`
	Status    string            `json:"status"`
	Labels    map[string]string `json:"labels"`
	Age       string            `json:"age"`
}

type serverListResponse struct {
	Servers []serverListItem `json:"servers"`
}

func (c *platformClient) listRuntimeServers(ctx context.Context, namespace string) ([]serverListItem, error) {
	v := url.Values{}
	if strings.TrimSpace(namespace) != "" {
		v.Set("namespace", namespace)
	} else {
		v.Set("namespace", NamespaceMCPServers)
	}
	resp, err := c.do(ctx, http.MethodGet, "/runtime/servers", v.Encode(), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := readBody(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, httpAPIError(resp.StatusCode, b)
	}
	var out serverListResponse
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out.Servers, nil
}

func (c *platformClient) getRuntimePolicy(ctx context.Context, namespace, server string) ([]byte, error) {
	v := url.Values{}
	v.Set("namespace", namespace)
	v.Set("server", server)
	resp, err := c.do(ctx, http.MethodGet, "/runtime/policy", v.Encode(), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := readBody(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, httpAPIError(resp.StatusCode, b)
	}
	return b, nil
}

func (m *AccessManager) platformOrKube() (plat *platformClient, useKubectl bool, err error) {
	if m.useKube {
		return nil, true, nil
	}
	cl, e := newPlatformClient()
	if e == nil {
		return cl, false, nil
	}
	if errors.Is(e, authfile.ErrNotFound) {
		return nil, true, nil
	}
	return nil, false, e
}

func (m *ServerManager) platformOrKube() (plat *platformClient, useKubectl bool, err error) {
	if m.useKube {
		return nil, true, nil
	}
	cl, e := newPlatformClient()
	if e == nil {
		return cl, false, nil
	}
	if errors.Is(e, authfile.ErrNotFound) {
		return nil, true, nil
	}
	return nil, false, e
}

// requireKubectlForMutation returns an error when only platform API credentials are active (no kube path).
func (m *ServerManager) requireKubectlForMutation() error {
	_, useK, err := m.platformOrKube()
	if err != nil {
		return err
	}
	if !useK {
		return newWithSentinel(nil, "this command requires kubectl and a cluster kubeconfig, or set --use-kube when you use kubectl alongside platform auth. Use mcp-runtime auth for API-backed list, status, and policy when kubeconfig is not used.")
	}
	return nil
}
