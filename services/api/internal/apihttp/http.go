package apihttp

import (
	"errors"
	"fmt"
	"net/http"

	"mcp-runtime/pkg/serviceutil"
)

const ApplyMaxBytes = 64 * 1024

func WriteBodyDecodeError(w http.ResponseWriter, err error) {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		serviceutil.WriteJSON(w, http.StatusRequestEntityTooLarge, map[string]string{
			"error": fmt.Sprintf("request body exceeds %d bytes", ApplyMaxBytes),
		})
		return
	}
	serviceutil.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
}
