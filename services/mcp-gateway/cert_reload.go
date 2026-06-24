package main

import (
	"context"
	"crypto/tls"
	"log"
	"os"
	"sync/atomic"
	"time"
)

// defaultCertReloadInterval is how often the gateway checks its TLS certificate
// for rotation. cert-manager renews ~8h before a 24h expiry, so minute-level
// polling reloads the new certificate long before the old one expires.
const defaultCertReloadInterval = time.Minute

// certReloader keeps the server TLS certificate fresh as cert-manager rotates it
// on disk. http.Server.ListenAndServeTLS loads the keypair only once at startup,
// so without this a long-running gateway would keep serving the old certificate
// after rotation and eventually present an expired one. It polls the certificate
// file's modtime — more reliable than fsnotify for Kubernetes secret mounts,
// which rotate via an atomic symlink swap — and atomically swaps the cached
// certificate. GetCertificate is wired into the server's tls.Config.
type certReloader struct {
	certFile string
	keyFile  string
	cert     atomic.Pointer[tls.Certificate]
}

// newCertReloader loads the initial keypair; it errors if the files are missing
// or invalid so startup fails fast rather than serving without a certificate.
func newCertReloader(certFile, keyFile string) (*certReloader, error) {
	r := &certReloader{certFile: certFile, keyFile: keyFile}
	if err := r.reload(); err != nil {
		return nil, err
	}
	return r, nil
}

// reload re-reads the keypair and atomically swaps it in. On error the previously
// loaded certificate is left untouched.
func (r *certReloader) reload() error {
	cert, err := tls.LoadX509KeyPair(r.certFile, r.keyFile)
	if err != nil {
		return err
	}
	r.cert.Store(&cert)
	return nil
}

// GetCertificate returns the current certificate; wire it into tls.Config.
func (r *certReloader) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	return r.cert.Load(), nil
}

// watch reloads the certificate whenever the cert file's modtime changes, until
// ctx is cancelled.
func (r *certReloader) watch(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	lastMod, _ := r.modTime()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			mod, err := r.modTime()
			if err != nil {
				log.Printf("gateway TLS certificate watch: failed to stat certificate file: %v", err)
				continue
			}
			if mod.Equal(lastMod) {
				continue
			}
			if err := r.reload(); err != nil {
				log.Printf("gateway TLS certificate reload failed; keeping previous certificate: %v", err)
				continue
			}
			lastMod = mod
			log.Printf("gateway TLS certificate reloaded after rotation")
		}
	}
}

func (r *certReloader) modTime() (time.Time, error) {
	fi, err := os.Stat(r.certFile)
	if err != nil {
		return time.Time{}, err
	}
	return fi.ModTime(), nil
}
