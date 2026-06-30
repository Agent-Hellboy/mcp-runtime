package apihttp

// Stable machine-readable error codes. Wording in human messages may change;
// codes must not be derived from message text at runtime.
const (
	CodeInternalError         = "internal_error"
	CodeInvalidRequestBody    = "invalid_request_body"
	CodeRequestTooLarge       = "request_too_large"
	CodeInvalidQueryParam     = "invalid_query_param"
	CodeMethodNotAllowed      = "method_not_allowed"
	CodeUnauthorized          = "unauthorized"
	CodeForbidden             = "forbidden"
	CodeNotFound              = "not_found"
	CodeConflict              = "conflict"
	CodeServiceUnavailable    = "service_unavailable"
	CodeQueryFailed           = "query_failed"
	CodeAuthFailed            = "auth_failed"
	CodePlatformUnavailable   = "platform_unavailable"
	CodeKubernetesUnavailable = "kubernetes_unavailable"
	CodeInvalidVersionTag     = "invalid_version_tag"
	CodeTooManyRequests       = "too_many_requests"
)
