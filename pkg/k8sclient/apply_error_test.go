package k8sclient

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestHTTPStatusFromK8sErrorMapsStatusError(t *testing.T) {
	wrapped := fmt.Errorf("outer: %w", &apierrors.StatusError{ErrStatus: metav1.Status{Code: 409, Message: "the conflict message"}})
	code, msg := HTTPStatusFromK8sError(wrapped)
	if code != http.StatusConflict {
		t.Fatalf("code = %d, want %d", code, http.StatusConflict)
	}
	if !strings.Contains(msg, "conflict") {
		t.Fatalf("msg = %q", msg)
	}
}
