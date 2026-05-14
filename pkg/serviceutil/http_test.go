package serviceutil

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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

func TestMetricsHandlerServesHealth(t *testing.T) {
	recorder := httptest.NewRecorder()
	metricsHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if recorder.Body.String() != "ok" {
		t.Fatalf("body = %q, want ok", recorder.Body.String())
	}
}

func TestStartMetricsServerReportsListenError(t *testing.T) {
	_, errs := StartMetricsServer("127.0.0.1:-1")

	select {
	case err, ok := <-errs:
		if !ok {
			t.Fatal("error channel closed without a listen error")
		}
		if err == nil {
			t.Fatal("listen error is nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for metrics server listen error")
	}
}
