package runtimeapi

import (
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"mcp-runtime/pkg/svcboot"
)

// slowMultipartBody streams a registry push form whose image part arrives in
// chunks over roughly total, simulating a large upload on a slow uplink.
func slowMultipartBody(t *testing.T, total time.Duration) (io.Reader, string) {
	t.Helper()
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)
	go func() {
		var err error
		defer func() {
			if err == nil {
				err = writer.Close()
			}
			_ = pw.CloseWithError(err)
		}()
		if err = writer.WriteField("target", "registry.example.com/acme/demo:v1"); err != nil {
			return
		}
		var part io.Writer
		if part, err = writer.CreateFormFile("image_tar", "image.tar"); err != nil {
			return
		}
		const chunks = 6
		for i := 0; i < chunks; i++ {
			if _, err = part.Write([]byte(strings.Repeat("x", 1024))); err != nil {
				return
			}
			time.Sleep(total / chunks)
		}
	}()
	return pr, writer.FormDataContentType()
}

func TestRegistryPushRouteOutlivesServiceReadTimeoutOtherRoutesDoNot(t *testing.T) {
	t.Setenv(registryPushTempDirEnv, t.TempDir())
	// Short stand-ins for the svcboot 15s defaults; the push route must lift
	// them for itself only.
	const serviceTimeout = 300 * time.Millisecond
	const slowUpload = 1200 * time.Millisecond

	mux := http.NewServeMux()
	mux.HandleFunc("/runtime/registry/push", func(w http.ResponseWriter, r *http.Request) {
		upload := 10 * time.Second
		extendRegistryPushDeadlines(w, upload)
		r.Body = http.MaxBytesReader(w, r.Body, registryPushMaxBytes)
		req, err := readRegistryPushRequestWithTimeout(r, upload)
		if err != nil {
			writeRegistryPushRequestError(w, err)
			return
		}
		_ = os.Remove(req.TarPath)
		writeJSON(w, http.StatusOK, map[string]any{"success": true})
	})
	mux.HandleFunc("/runtime/other", func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true})
	})

	srv := httptest.NewUnstartedServer(mux)
	srv.Config = svcboot.NewHTTPServer("", mux)
	srv.Config.ReadTimeout = serviceTimeout
	srv.Config.WriteTimeout = serviceTimeout
	srv.Start()
	defer srv.Close()

	post := func(path string) (int, error) {
		body, contentType := slowMultipartBody(t, slowUpload)
		req, err := http.NewRequest(http.MethodPost, srv.URL+path, body)
		if err != nil {
			return 0, err
		}
		req.Header.Set("content-type", contentType)
		resp, err := srv.Client().Do(req)
		if err != nil {
			return 0, err
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		return resp.StatusCode, nil
	}

	status, err := post("/runtime/registry/push")
	if err != nil || status != http.StatusOK {
		t.Fatalf("slow push upload: status=%d err=%v, want 200", status, err)
	}
	status, err = post("/runtime/other")
	if err == nil && status == http.StatusOK {
		t.Fatalf("slow body on another route succeeded; the service read timeout must still apply there")
	}
}

func TestClassifyRegistryPushUploadError(t *testing.T) {
	timeoutErr := classifyRegistryPushUploadError(fmt.Errorf("read: %w", os.ErrDeadlineExceeded), 20*time.Minute)
	var reqErr *registryPushRequestError
	if !errors.As(timeoutErr, &reqErr) || reqErr.status != http.StatusRequestTimeout || !strings.Contains(reqErr.message, registryPushUploadTimeoutEnv) {
		t.Fatalf("deadline error = %#v, want 408 naming %s", timeoutErr, registryPushUploadTimeoutEnv)
	}
	tooLarge := classifyRegistryPushUploadError(&http.MaxBytesError{Limit: registryPushMaxBytes}, time.Minute)
	if !errors.As(tooLarge, &reqErr) || reqErr.status != http.StatusRequestEntityTooLarge {
		t.Fatalf("max bytes error = %#v, want 413", tooLarge)
	}
	if got := classifyRegistryPushUploadError(errors.New("boom"), time.Minute); got != nil {
		t.Fatalf("unrelated error classified as %v", got)
	}
}

func TestRegistryPushUploadTimeoutEnv(t *testing.T) {
	t.Setenv(registryPushUploadTimeoutEnv, "")
	if got := registryPushUploadTimeout(); got != defaultRegistryPushUploadTimeout {
		t.Fatalf("default = %s", got)
	}
	t.Setenv(registryPushUploadTimeoutEnv, "45m")
	if got := registryPushUploadTimeout(); got != 45*time.Minute {
		t.Fatalf("override = %s", got)
	}
	t.Setenv(registryPushUploadTimeoutEnv, "nope")
	if got := registryPushUploadTimeout(); got != defaultRegistryPushUploadTimeout {
		t.Fatalf("invalid override = %s", got)
	}
}
