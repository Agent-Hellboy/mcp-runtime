package pii_redactor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
)

// Config holds middleware configuration.
type Config struct {
	MaskReplacement string   `json:"maskReplacement,omitempty"`
	BypassHeaders   []string `json:"bypassHeaders,omitempty"`
	MaxBodyBytes    int64    `json:"maxBodyBytes,omitempty"`
	HashIDs         bool     `json:"hashIDs,omitempty"`
}

// CreateConfig returns the default plugin configuration.
func CreateConfig() *Config {
	return &Config{
		MaskReplacement: "[redacted]",
		BypassHeaders:   []string{"Authorization", "X-Internal-Auth", "X-Api-Key"},
		MaxBodyBytes:    1 << 20, // 1 MiB
		HashIDs:         true,
	}
}

// New creates a new redaction middleware.
func New(_ context.Context, next http.Handler, cfg *Config, name string) (http.Handler, error) {
	if next == nil {
		return nil, errors.New("next handler is required")
	}

	mask := cfg.MaskReplacement
	if mask == "" {
		mask = "[redacted]"
	}

	maxBody := cfg.MaxBodyBytes
	if maxBody <= 0 {
		maxBody = 1 << 20
	}

	return &Middleware{
		next:      next,
		name:      name,
		mask:      mask,
		hashIDs:   cfg.HashIDs,
		maxBody:   maxBody,
		bypassSet: toLowerSet(cfg.BypassHeaders),
	}, nil
}

// Middleware implements http.Handler and performs redaction.
type Middleware struct {
	next      http.Handler
	name      string
	mask      string
	hashIDs   bool
	maxBody   int64
	bypassSet map[string]struct{}
}

func (m *Middleware) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	m.redactRequestHeaders(req.Header)
	m.redactRequestBody(req)

	rec := httptest.NewRecorder()
	m.next.ServeHTTP(rec, req)

	m.redactHeaders(rec.Header())
	body := m.redactBody(rec.Body.Bytes())

	copyHeader(rw.Header(), rec.Header())
	rw.Header().Set("Content-Length", intToString(len(body)))
	rw.WriteHeader(rec.Code)
	_, _ = rw.Write(body)
}

func (m *Middleware) redactRequestBody(req *http.Request) {
	if req.Body == nil {
		return
	}
	limited := io.LimitReader(req.Body, m.maxBody+1)
	raw, _ := io.ReadAll(limited)
	_ = req.Body.Close()
	if int64(len(raw)) > m.maxBody {
		// Too large: drop body to avoid unbounded memory usage.
		req.Body = io.NopCloser(bytes.NewReader([]byte{}))
		req.ContentLength = 0
		return
	}
	redacted := m.redactBody(raw)
	req.Body = io.NopCloser(bytes.NewReader(redacted))
	req.ContentLength = int64(len(redacted))
}

func (m *Middleware) redactRequestHeaders(h http.Header) {
	m.redactHeaders(h)
}

func (m *Middleware) redactHeaders(h http.Header) {
	for key, values := range h {
		if m.isBypassed(key) {
			continue
		}
		for i, v := range values {
			if isSensitiveHeader(key) {
				h[key][i] = m.mask
				continue
			}
			h[key][i] = m.redactText(v)
		}
	}
}

func (m *Middleware) redactBody(body []byte) []byte {
	if len(body) == 0 {
		return body
	}
	redacted := m.redactText(string(body))
	return []byte(redacted)
}

func (m *Middleware) redactText(s string) string {
	out := s
	if m.hashIDs {
		out = uuidRegex.ReplaceAllStringFunc(out, func(val string) string {
			return "hash:" + hashPrefix(val)
		})
	}

	out = emailRegex.ReplaceAllString(out, m.mask)
	out = phoneRegex.ReplaceAllString(out, m.mask)
	out = ssnRegex.ReplaceAllString(out, m.mask)
	out = secretRegex.ReplaceAllString(out, "${1}"+m.mask)
	out = bearerRegex.ReplaceAllString(out, "Bearer "+m.mask)

	return out
}

func (m *Middleware) isBypassed(header string) bool {
	_, ok := m.bypassSet[strings.ToLower(header)]
	return ok
}

func isSensitiveHeader(header string) bool {
	h := strings.ToLower(header)
	return strings.Contains(h, "token") ||
		strings.Contains(h, "secret") ||
		strings.Contains(h, "api-key") ||
		strings.Contains(h, "apikey") ||
		strings.Contains(h, "authorization")
}

func toLowerSet(items []string) map[string]struct{} {
	set := make(map[string]struct{}, len(items))
	for _, it := range items {
		if it == "" {
			continue
		}
		set[strings.ToLower(it)] = struct{}{}
	}
	return set
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		dst[k] = append([]string(nil), vv...)
	}
}

func intToString(v int) string {
	return strconv.Itoa(v)
}

// hashPrefix returns a deterministic short hash for correlating redacted IDs.
func hashPrefix(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:])[:12]
}

var (
	emailRegex  = regexp.MustCompile(`(?i)[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}`)
	phoneRegex  = regexp.MustCompile(`(?i)(\+?\d{1,3}[\s.-]?)?(\(\d{2,3}\)|\d{2,3})[\s.-]?\d{3}[\s.-]?\d{4}`)
	ssnRegex    = regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)
	bearerRegex = regexp.MustCompile(`(?i)Bearer\s+[A-Za-z0-9._\-]{6,}`)
	secretRegex = regexp.MustCompile(`(?i)((?:api[_-]?key|token|secret|password|passcode)"?\s*[:=]\s*"?)\s*([A-Za-z0-9/+._\-]{6,})`)
	uuidRegex   = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\b`)
)
