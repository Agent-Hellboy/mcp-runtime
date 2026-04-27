// Package serviceutil provides HTTP routing utilities for MCP services.
package serviceutil

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

// Predefined errors for routing validation.
var (
	ErrMethodNotAllowed = errors.New("method not allowed")
	ErrInvalidPath      = errors.New("invalid path")
	ErrInvalidAction    = errors.New("invalid action")
)

// RouteParams extracts path parameters from HTTP request paths.
// It provides a structured way to handle path-based routing without manual string manipulation.
type RouteParams struct {
	Namespace string
	Name      string
	Action    string
}

// validResourceName validates a Kubernetes resource name.
// Names must consist of lowercase alphanumeric characters, '-', or '.',
// and must start and end with an alphanumeric character.
func validResourceName(name string) bool {
	if name == "" {
		return false
	}
	// Kubernetes DNS-1123 subdomain format with some relaxations for resource names
	validName := regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)
	return validName.MatchString(name) && len(name) <= 253
}

// ExtractGrantActionParams extracts parameters from grant toggle paths.
// Expected path format: /api/runtime/grants/{namespace}/{name}/{action}
// where action is either "disable" or "enable".
func ExtractGrantActionParams(r *http.Request, prefix string) (RouteParams, error) {
	var params RouteParams

	if r.Method != http.MethodPost {
		return params, ErrMethodNotAllowed
	}

	parts, err := splitNamespacedPath(r, prefix, 3, "expected {namespace}/{name}/{action}")
	if err != nil {
		return params, err
	}

	params.Namespace = parts[0]
	params.Name = parts[1]
	params.Action = parts[2]

	// Validate action
	switch params.Action {
	case "disable", "enable":
		return params, nil
	default:
		return params, fmt.Errorf("%w: expected 'disable' or 'enable', got %q", ErrInvalidAction, params.Action)
	}
}

// ExtractSessionActionParams extracts parameters from session toggle paths.
// Expected path format: /api/runtime/sessions/{namespace}/{name}/{action}
// where action is either "revoke" or "unrevoke".
func ExtractSessionActionParams(r *http.Request, prefix string) (RouteParams, error) {
	var params RouteParams

	if r.Method != http.MethodPost {
		return params, ErrMethodNotAllowed
	}

	parts, err := splitNamespacedPath(r, prefix, 3, "expected {namespace}/{name}/{action}")
	if err != nil {
		return params, err
	}

	params.Namespace = parts[0]
	params.Name = parts[1]
	params.Action = parts[2]

	// Validate action
	switch params.Action {
	case "revoke", "unrevoke":
		return params, nil
	default:
		return params, fmt.Errorf("%w: expected 'revoke' or 'unrevoke', got %q", ErrInvalidAction, params.Action)
	}
}

// IsActionEnabled returns true for "enable" and "unrevoke" actions, false for "disable" and "revoke".
func IsActionEnabled(action string) bool {
	switch action {
	case "enable", "unrevoke":
		return true
	default:
		return false
	}
}

// ExtractNamespacedResourceDelete validates DELETE /{prefix}{namespace}/{name} (two path segments after prefix).
func ExtractNamespacedResourceDelete(r *http.Request, prefix string) (namespace, name string, err error) {
	if r.Method != http.MethodDelete {
		return "", "", ErrMethodNotAllowed
	}
	parts, err := splitNamespacedPath(r, prefix, 2, "expected {namespace}/{name} for DELETE")
	if err != nil {
		return "", "", err
	}
	namespace, name = parts[0], parts[1]
	return namespace, name, nil
}

func splitNamespacedPath(r *http.Request, prefix string, expectedParts int, expectedShape string) ([]string, error) {
	path := strings.TrimPrefix(r.URL.Path, prefix)
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != expectedParts {
		return nil, fmt.Errorf("%w: %s, got %d path parts", ErrInvalidPath, expectedShape, len(parts))
	}
	for _, part := range parts {
		if !validResourceName(part) {
			return nil, fmt.Errorf("%w: invalid path segment %q", ErrInvalidPath, part)
		}
	}
	return parts, nil
}
