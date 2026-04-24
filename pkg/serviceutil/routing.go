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

	path := strings.TrimPrefix(r.URL.Path, prefix)
	parts := strings.Split(strings.Trim(path, "/"), "/")

	if len(parts) != 3 {
		return params, fmt.Errorf("%w: expected {namespace}/{name}/{action}, got %d path parts", ErrInvalidPath, len(parts))
	}

	params.Namespace = parts[0]
	params.Name = parts[1]
	params.Action = parts[2]

	// Validate namespace and name
	if !validResourceName(params.Namespace) {
		return params, fmt.Errorf("%w: invalid namespace %q", ErrInvalidPath, params.Namespace)
	}
	if !validResourceName(params.Name) {
		return params, fmt.Errorf("%w: invalid name %q", ErrInvalidPath, params.Name)
	}

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

	path := strings.TrimPrefix(r.URL.Path, prefix)
	parts := strings.Split(strings.Trim(path, "/"), "/")

	if len(parts) != 3 {
		return params, fmt.Errorf("%w: expected {namespace}/{name}/{action}, got %d path parts", ErrInvalidPath, len(parts))
	}

	params.Namespace = parts[0]
	params.Name = parts[1]
	params.Action = parts[2]

	// Validate namespace and name
	if !validResourceName(params.Namespace) {
		return params, fmt.Errorf("%w: invalid namespace %q", ErrInvalidPath, params.Namespace)
	}
	if !validResourceName(params.Name) {
		return params, fmt.Errorf("%w: invalid name %q", ErrInvalidPath, params.Name)
	}

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
