package k8sclient

import (
	"errors"
	"net/http"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// HTTPStatusFromK8sError maps a Kubernetes client/API error to an HTTP status and
// a short message suitable for JSON API responses.
func HTTPStatusFromK8sError(err error) (int, string) {
	if err == nil {
		return http.StatusOK, ""
	}
	var statusErr *apierrors.StatusError
	if errors.As(err, &statusErr) {
		code := int(statusErr.Status().Code)
		if code < 400 || code > 599 {
			code = http.StatusInternalServerError
		}
		msg := statusErr.Status().Message
		if msg == "" {
			msg = statusErr.Error()
		}
		return code, msg
	}
	switch {
	case apierrors.IsNotFound(err):
		return http.StatusNotFound, err.Error()
	case apierrors.IsInvalid(err):
		return http.StatusUnprocessableEntity, err.Error()
	case apierrors.IsAlreadyExists(err):
		return http.StatusConflict, err.Error()
	case apierrors.IsConflict(err):
		return http.StatusConflict, err.Error()
	case apierrors.IsForbidden(err):
		return http.StatusForbidden, err.Error()
	default:
		return http.StatusInternalServerError, err.Error()
	}
}
