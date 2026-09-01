package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	issuerValue := strings.TrimRight(strings.TrimSpace(os.Getenv("OAUTH_ISSUER_URL")), "/")
	if issuerValue == "" {
		issuerValue = "http://localhost:8086"
	}
	issuer, err := url.Parse(issuerValue)
	if err != nil || !issuer.IsAbs() || issuer.Host == "" || issuer.RawQuery != "" || issuer.Fragment != "" {
		fatal("invalid OAUTH_ISSUER_URL")
	}
	if issuer.Scheme != "https" && !isLoopbackHost(issuer.Hostname()) && os.Getenv("OAUTH_ALLOW_INSECURE_HTTP") != "true" {
		fatal("OAUTH_ISSUER_URL must use HTTPS outside local development")
	}
	key, err := loadSigningKey()
	if err != nil {
		fatal(err.Error())
	}
	dsn := strings.TrimSpace(os.Getenv("POSTGRES_DSN"))
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("DATABASE_URL"))
	}
	if dsn == "" {
		fatal("POSTGRES_DSN is required")
	}
	store, err := openOAuthStore(context.Background(), dsn)
	if err != nil {
		fatal(fmt.Sprintf("open OAuth store: %v", err))
	}
	defer store.Close()

	handler := newOAuthHandlerWithRedirectSchemes(issuer, key, store, parseRedirectSchemes(os.Getenv("OAUTH_ALLOWED_REDIRECT_URI_SCHEMES")))
	listenAddr := envOr("PORT", ":8086")
	if !strings.Contains(listenAddr, ":") {
		listenAddr = ":" + listenAddr
	}
	server := &http.Server{Addr: listenAddr, Handler: handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-shutdown
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fatal(err.Error())
	}
}

func parseRedirectSchemes(value string) map[string]struct{} {
	allowed := make(map[string]struct{})
	for _, item := range strings.Split(value, ",") {
		scheme := strings.ToLower(strings.TrimSpace(item))
		if scheme != "" && scheme != "http" && scheme != "https" && validRedirectScheme(scheme) {
			allowed[scheme] = struct{}{}
		}
	}
	return allowed
}

func validRedirectScheme(value string) bool {
	for index, character := range value {
		if (character >= 'a' && character <= 'z') || (index > 0 && ((character >= '0' && character <= '9') || character == '+' || character == '-' || character == '.')) {
			continue
		}
		return false
	}
	return value != ""
}

func loadSigningKey() (*rsa.PrivateKey, error) {
	var data []byte
	var err error
	if path := strings.TrimSpace(os.Getenv("OAUTH_PRIVATE_KEY_FILE")); path != "" {
		data, err = os.ReadFile(path)
	} else if value := os.Getenv("OAUTH_PRIVATE_KEY"); value != "" {
		data = []byte(value)
	} else if os.Getenv("OAUTH_ALLOW_EPHEMERAL_KEYS") == "true" {
		return rsa.GenerateKey(rand.Reader, 2048)
	} else {
		return nil, errors.New("OAUTH_PRIVATE_KEY_FILE or OAUTH_PRIVATE_KEY is required")
	}
	if err != nil {
		return nil, err
	}
	return parsePrivateKey(data)
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func fatal(message string) {
	_, _ = fmt.Fprintln(os.Stderr, "oauth-server:", message)
	os.Exit(1)
}
