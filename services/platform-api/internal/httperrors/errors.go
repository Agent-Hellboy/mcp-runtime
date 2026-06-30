package httperrors

import (
	"net/http"

	"mcp-runtime/pkg/apihttp"
)

func Unauthorized(w http.ResponseWriter) {
	apihttp.WriteEnvelope(w, http.StatusUnauthorized, apihttp.CodeUnauthorized, "unauthorized")
}

func Forbidden(w http.ResponseWriter, message string) {
	apihttp.WriteEnvelope(w, http.StatusForbidden, apihttp.CodeForbidden, message)
}

func NotFound(w http.ResponseWriter, message string) {
	apihttp.WriteEnvelope(w, http.StatusNotFound, apihttp.CodeNotFound, message)
}

func BadRequest(w http.ResponseWriter, message string) {
	apihttp.WriteEnvelope(w, http.StatusBadRequest, apihttp.CodeInvalidRequestBody, message)
}

func InvalidQuery(w http.ResponseWriter, message string) {
	apihttp.WriteEnvelope(w, http.StatusBadRequest, apihttp.CodeInvalidQueryParam, message)
}

func MethodNotAllowed(w http.ResponseWriter, allow string) {
	if allow != "" {
		w.Header().Set("allow", allow)
	}
	apihttp.WriteEnvelope(w, http.StatusMethodNotAllowed, apihttp.CodeMethodNotAllowed, "method not allowed")
}

func PlatformUnavailable(w http.ResponseWriter) {
	apihttp.WriteEnvelope(w, http.StatusServiceUnavailable, apihttp.CodeServiceUnavailable, "platform identity database not configured")
}

func UserKeyStoreUnavailable(w http.ResponseWriter) {
	apihttp.WriteEnvelope(w, http.StatusServiceUnavailable, apihttp.CodeServiceUnavailable, "user api key store not configured")
}

func OIDCNotConfigured(w http.ResponseWriter) {
	apihttp.WriteEnvelope(w, http.StatusServiceUnavailable, apihttp.CodeServiceUnavailable, "oidc not configured")
}

func AuthFailed(w http.ResponseWriter) {
	apihttp.WriteEnvelope(w, http.StatusInternalServerError, apihttp.CodeAuthFailed, "auth failed")
}

func LoginFailed(w http.ResponseWriter) {
	apihttp.WriteEnvelope(w, http.StatusInternalServerError, apihttp.CodeAuthFailed, "login failed")
}

func QueryFailed(w http.ResponseWriter, message string) {
	apihttp.WriteEnvelope(w, http.StatusInternalServerError, apihttp.CodeQueryFailed, message)
}

func Internal(w http.ResponseWriter, message string) {
	apihttp.WriteEnvelope(w, http.StatusInternalServerError, apihttp.CodeInternalError, message)
}

func TooManyRequests(w http.ResponseWriter) {
	apihttp.WriteEnvelope(w, http.StatusTooManyRequests, apihttp.CodeTooManyRequests, "too many requests")
}

func InvalidCredentials(w http.ResponseWriter) {
	apihttp.WriteEnvelope(w, http.StatusUnauthorized, apihttp.CodeUnauthorized, "invalid credentials")
}
