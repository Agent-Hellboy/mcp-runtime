package apierr

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestConstructors(t *testing.T) {
	cause := errors.New("cause")
	tests := []struct {
		name   string
		status int
		build  func(string, string, ...error) *Error
	}{
		{name: "BadRequest", status: http.StatusBadRequest, build: BadRequest},
		{name: "Unauthorized", status: http.StatusUnauthorized, build: Unauthorized},
		{name: "Forbidden", status: http.StatusForbidden, build: Forbidden},
		{name: "NotFound", status: http.StatusNotFound, build: NotFound},
		{name: "Conflict", status: http.StatusConflict, build: Conflict},
		{name: "Internal", status: http.StatusInternalServerError, build: Internal},
		{name: "ServiceUnavailable", status: http.StatusServiceUnavailable, build: ServiceUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.build("test_code", "test message", cause)
			if err.Status != tt.status {
				t.Fatalf("Status = %d, want %d", err.Status, tt.status)
			}
			if err.Code != "test_code" {
				t.Fatalf("Code = %q, want test_code", err.Code)
			}
			if err.Message != "test message" {
				t.Fatalf("Message = %q, want test message", err.Message)
			}
			if err.Error() != "test message" {
				t.Fatalf("Error() = %q, want test message", err.Error())
			}
			if !errors.Is(err, cause) {
				t.Fatalf("Unwrap() does not expose cause")
			}
		})
	}
}

func TestWriteTypedError(t *testing.T) {
	core, logs := observer.New(zapcore.ErrorLevel)
	logger := zap.New(core)
	recorder := httptest.NewRecorder()
	cause := errors.New("database unavailable")

	Write(recorder, logger, ServiceUnavailable("store_unavailable", "store unavailable", cause))

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	if got, want := recorder.Body.String(), `{"error":"store_unavailable","message":"store unavailable"}`; got != want {
		t.Fatalf("body = %s, want %s", got, want)
	}
	if logs.Len() != 1 {
		t.Fatalf("logged entries = %d, want 1", logs.Len())
	}
}

func TestWriteGenericError(t *testing.T) {
	core, logs := observer.New(zapcore.ErrorLevel)
	logger := zap.New(core)
	recorder := httptest.NewRecorder()

	Write(recorder, logger, errors.New("sensitive backend detail"))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if got, want := recorder.Body.String(), `{"error":"internal_error","message":"internal server error"}`; got != want {
		t.Fatalf("body = %s, want %s", got, want)
	}
	if logs.Len() != 1 {
		t.Fatalf("logged entries = %d, want 1", logs.Len())
	}
}
