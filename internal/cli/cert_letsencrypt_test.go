package cli

import "testing"

func TestValidateIngressManifestForACME(t *testing.T) {
	t.Parallel()
	if err := validateIngressManifestForACME("config/ingress/overlays/http"); err == nil {
		t.Fatal("expected error for dev http overlay")
	}
	if err := validateIngressManifestForACME("config/ingress/overlays/prod"); err != nil {
		t.Fatalf("prod overlay should be allowed: %v", err)
	}
	if err := validateIngressManifestForACME(""); err != nil {
		t.Fatalf("empty: %v", err)
	}
}
