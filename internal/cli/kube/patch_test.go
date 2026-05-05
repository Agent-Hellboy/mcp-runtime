package kube

import "testing"

func TestNormalizePatchDocumentYAMLMap(t *testing.T) {
	raw := "foo: bar\nnested:\n  x: 1\n"
	out, err := NormalizePatchDocument(raw)
	if err != nil {
		t.Fatalf("NormalizePatchDocument: %v", err)
	}
	if out == "" || out[0] != '{' {
		t.Fatalf("expected JSON object, got %q", out)
	}
}
