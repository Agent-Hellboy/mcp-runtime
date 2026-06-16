package openapi

import (
	"net/http/httptest"
	"testing"
)

const sampleSpec = `
openapi: 3.1.0
info:
  title: sample
  version: 1.0.0
paths:
  /health:
    get:
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                type: object
                required: [ok]
                properties:
                  ok:
                    type: boolean
components: {}
`

func TestLoadAndValidateResponse(t *testing.T) {
	doc, err := Load([]byte(sampleSpec))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := ValidateResponse(doc, "GET", "/health", 200, []byte(`{"ok":true}`), "application/json"); err != nil {
		t.Fatalf("ValidateResponse() error = %v", err)
	}
	if err := ValidateResponse(doc, "GET", "/health", 200, []byte(`{"missing":true}`), "application/json"); err == nil {
		t.Fatal("expected schema validation error for mismatched body")
	}
}

func TestServeYAMLContentType(t *testing.T) {
	rec := httptest.NewRecorder()
	ServeYAML(rec, []byte("openapi: 3.1.0\n"))
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/yaml" {
		t.Fatalf("Content-Type = %q, want application/yaml", got)
	}
}
