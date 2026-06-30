package apihttp

import (
	"errors"
	"net/http"

	"go.uber.org/zap"

	"mcp-runtime/pkg/serviceutil"
)

// Error is a safe, typed HTTP error for JSON API responses.
type Error struct {
	Status  int
	Code    string
	Message string
	Cause   error
}

func (e *Error) Error() string {
	return e.Message
}

func (e *Error) Unwrap() error {
	return e.Cause
}

func BadRequest(code, message string, cause ...error) *Error {
	return newError(http.StatusBadRequest, code, message, cause...)
}

func Unauthorized(message string, cause ...error) *Error {
	return newError(http.StatusUnauthorized, CodeUnauthorized, message, cause...)
}

func Forbidden(message string, cause ...error) *Error {
	return newError(http.StatusForbidden, CodeForbidden, message, cause...)
}

func NotFound(message string, cause ...error) *Error {
	return newError(http.StatusNotFound, CodeNotFound, message, cause...)
}

func Conflict(message string, cause ...error) *Error {
	return newError(http.StatusConflict, CodeConflict, message, cause...)
}

func Internal(message string, cause ...error) *Error {
	return newError(http.StatusInternalServerError, CodeInternalError, message, cause...)
}

func ServiceUnavailable(code, message string, cause ...error) *Error {
	if code == "" {
		code = CodeServiceUnavailable
	}
	return newError(http.StatusServiceUnavailable, code, message, cause...)
}

// WriteError renders err as the standard JSON error envelope.
func WriteError(w http.ResponseWriter, logger *zap.Logger, err error) {
	var apiError *Error
	if errors.As(err, &apiError) {
		if logger != nil && apiError.Cause != nil {
			logger.Error("api error",
				zap.Int("status", apiError.Status),
				zap.String("code", apiError.Code),
				zap.Error(apiError.Cause),
			)
		}
		WriteEnvelope(w, apiError.Status, apiError.Code, apiError.Message)
		return
	}

	if logger != nil && err != nil {
		logger.Error("api error", zap.Error(err))
	}
	WriteEnvelope(w, http.StatusInternalServerError, CodeInternalError, "internal server error")
}

// WriteEnvelope writes {"error":"<code>","message":"<description>"}.
func WriteEnvelope(w http.ResponseWriter, status int, code, message string) {
	serviceutil.WriteJSON(w, status, map[string]string{
		"error":   code,
		"message": message,
	})
}

func newError(status int, code, message string, cause ...error) *Error {
	return &Error{
		Status:  status,
		Code:    code,
		Message: message,
		Cause:   errors.Join(cause...),
	}
}
