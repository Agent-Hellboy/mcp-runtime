package apierr

import (
	"errors"
	"net/http"

	"go.uber.org/zap"

	"mcp-runtime/pkg/serviceutil"
)

// Error is a safe, typed HTTP error for API responses.
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

func Unauthorized(code, message string, cause ...error) *Error {
	return newError(http.StatusUnauthorized, code, message, cause...)
}

func Forbidden(code, message string, cause ...error) *Error {
	return newError(http.StatusForbidden, code, message, cause...)
}

func NotFound(code, message string, cause ...error) *Error {
	return newError(http.StatusNotFound, code, message, cause...)
}

func Conflict(code, message string, cause ...error) *Error {
	return newError(http.StatusConflict, code, message, cause...)
}

func Internal(code, message string, cause ...error) *Error {
	return newError(http.StatusInternalServerError, code, message, cause...)
}

func ServiceUnavailable(code, message string, cause ...error) *Error {
	return newError(http.StatusServiceUnavailable, code, message, cause...)
}

// Write renders err as a JSON API error response.
func Write(w http.ResponseWriter, logger *zap.Logger, err error) {
	var apiError *Error
	if errors.As(err, &apiError) {
		if logger != nil && apiError.Cause != nil {
			logger.Error("api error", zap.Int("status", apiError.Status), zap.String("code", apiError.Code), zap.Error(apiError.Cause))
		}
		serviceutil.WriteJSON(w, apiError.Status, map[string]string{
			"error":   apiError.Code,
			"message": apiError.Message,
		})
		return
	}

	if logger != nil && err != nil {
		logger.Error("api error", zap.Error(err))
	}
	serviceutil.WriteJSON(w, http.StatusInternalServerError, map[string]string{
		"error":   "internal_error",
		"message": "internal server error",
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
