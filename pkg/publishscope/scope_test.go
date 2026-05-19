package publishscope

import "testing"

func TestNormalize(t *testing.T) {
	got, err := Normalize(" Public ")
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if got != Public {
		t.Fatalf("Normalize() = %q, want %q", got, Public)
	}
	if _, err := Normalize("internet"); err == nil {
		t.Fatal("expected invalid scope error")
	}
}

func TestRegistryAlias(t *testing.T) {
	if got, ok := RegistryAlias(Public); !ok || got != PublicRegistryAlias {
		t.Fatalf("RegistryAlias(public) = %q %v", got, ok)
	}
	if _, ok := RegistryAlias(Tenant); ok {
		t.Fatal("tenant should not have a shared registry alias")
	}
}
