package openapi

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/gorillamux"
)

// Load parses and validates an OpenAPI 3 document.
func Load(data []byte) (*openapi3.T, error) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(data)
	if err != nil {
		return nil, fmt.Errorf("load openapi document: %w", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		return nil, fmt.Errorf("validate openapi document: %w", err)
	}
	return doc, nil
}

// ValidateResponse is a test/CI helper that checks an HTTP response body against
// the committed OpenAPI spec. It rebuilds routing state per call and is not
// intended for live request-path enforcement.
func ValidateResponse(doc *openapi3.T, method, path string, status int, body []byte, contentType string) error {
	if doc == nil {
		return fmt.Errorf("openapi document is nil")
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	path = strings.TrimSpace(path)
	if method == "" || path == "" {
		return fmt.Errorf("method and path are required")
	}

	router, err := gorillamux.NewRouter(doc)
	if err != nil {
		return fmt.Errorf("build openapi router: %w", err)
	}

	req, err := http.NewRequest(method, path, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	route, pathParams, err := router.FindRoute(req)
	if err != nil {
		return fmt.Errorf("find route %s %s: %w", method, path, err)
	}

	input := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{
			Request:    req,
			PathParams: pathParams,
			Route:      route,
		},
		Status: status,
		Header: http.Header{},
		Body:   ioNopCloser(body),
	}
	if contentType != "" {
		input.Header.Set("Content-Type", contentType)
	}

	if err := openapi3filter.ValidateResponse(context.Background(), input); err != nil {
		return fmt.Errorf("validate response %s %s status %d: %w", method, path, status, err)
	}
	return nil
}

type nopCloser struct {
	*bytes.Reader
}

func (nopCloser) Close() error { return nil }

func ioNopCloser(data []byte) *nopCloser {
	return &nopCloser{Reader: bytes.NewReader(data)}
}
