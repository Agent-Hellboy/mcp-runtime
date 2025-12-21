package errx

import (
	"testing"
)

func TestRegistry_ErrorRegistry(t *testing.T) {
	entries := ErrorRegistry()
	if len(entries) != len(registryEntries) {
		t.Errorf("ErrorRegistry() = %v, want %v", len(entries), len(registryEntries))
	}
	for i, entry := range entries {
		if entry.Code != registryEntries[i].Code || entry.Description != registryEntries[i].Description {
			t.Errorf("ErrorRegistry()[%d] = %v, want %v", i, entry, registryEntries[i])
		}
	}
}

func TestRegistry_DescriptionFor(t *testing.T) {
	desc, ok := DescriptionFor(CodeCLI)
	if !ok || desc != DescCLI {
		t.Errorf("DescriptionFor(%q) = %q, want %q", CodeCLI, desc, DescCLI)
	}
}

func TestRegistry_IsValidCode(t *testing.T) {
	if !IsValidCode(CodeCLI) {
		t.Errorf("IsValidCode(%q) = %v, want %v", CodeCLI, IsValidCode(CodeCLI), true)
	}
}
