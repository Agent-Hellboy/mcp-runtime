package main

import (
	"crypto/hmac"
	"net/http"
	"net/url"
	"strings"
)

const csrfHeaderName = "X-CSRF-Token"

func unsafeHTTPMethod(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return false
	default:
		return true
	}
}

func csrfTokenValid(sess uiSession, presented string) bool {
	expected := strings.TrimSpace(sess.CSRFToken)
	presented = strings.TrimSpace(presented)
	return expected != "" && presented != "" && hmac.Equal([]byte(expected), []byte(presented))
}

func sameOriginRequest(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	return err == nil && parsed.Host != "" && strings.EqualFold(parsed.Host, r.Host)
}

func verifyCSRF(r *http.Request, sess uiSession) int {
	if !unsafeHTTPMethod(r.Method) {
		return 0
	}
	if !sameOriginRequest(r) || !csrfTokenValid(sess, r.Header.Get(csrfHeaderName)) {
		return http.StatusForbidden
	}
	return 0
}
