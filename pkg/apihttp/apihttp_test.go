package apihttp_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"mcp-runtime/pkg/apihttp"
)

func TestWriteEnvelope(t *testing.T) {
	rec := httptest.NewRecorder()
	apihttp.WriteEnvelope(rec, http.StatusBadRequest, apihttp.CodeInvalidQueryParam, "limit must be an integer")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["error"] != apihttp.CodeInvalidQueryParam {
		t.Fatalf("error = %q", body["error"])
	}
	if body["message"] == "" {
		t.Fatal("expected message")
	}
}

func TestQueryIntStrict(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?limit=abc", nil)
	if _, err := apihttp.QueryInt(req, "limit", 10, 1, 50); err == nil {
		t.Fatal("expected error for non-integer limit")
	}

	req = httptest.NewRequest(http.MethodGet, "/?limit=999", nil)
	if _, err := apihttp.QueryInt(req, "limit", 10, 1, 50); err == nil {
		t.Fatal("expected error for out-of-range limit")
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	got, err := apihttp.QueryInt(req, "limit", 10, 1, 50)
	if err != nil || got != 10 {
		t.Fatalf("got (%d, %v), want (10, nil)", got, err)
	}
}

func TestCursorRoundTrip(t *testing.T) {
	cursor := apihttp.EncodeCursor(100)
	offset, err := apihttp.ParseCursor(cursor)
	if err != nil || offset != 100 {
		t.Fatalf("got (%d, %v), want (100, nil)", offset, err)
	}
}

func TestListMeta(t *testing.T) {
	meta := apihttp.ListMeta(50, 0, 50)
	if !meta.HasMore || meta.NextCursor == "" {
		t.Fatalf("expected next page, got %+v", meta)
	}
	meta = apihttp.ListMeta(50, 0, 10)
	if meta.HasMore {
		t.Fatalf("expected no more pages, got %+v", meta)
	}
}
