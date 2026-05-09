package serviceutil

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

type flushResponseRecorder struct {
	http.ResponseWriter
	flushed bool
}

func (r *flushResponseRecorder) Flush() {
	r.flushed = true
}

func TestLogRequestsPreservesFlusher(t *testing.T) {
	recorder := &flushResponseRecorder{ResponseWriter: httptest.NewRecorder()}
	handler := LogRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("wrapped response writer does not implement http.Flusher")
		}
		flusher.Flush()
		w.WriteHeader(http.StatusNoContent)
	}))

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/events", nil))

	if !recorder.flushed {
		t.Fatal("Flush was not delegated to the underlying response writer")
	}
}
