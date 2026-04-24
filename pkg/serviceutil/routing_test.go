// Package serviceutil provides tests for routing utilities.
package serviceutil

import (
	"errors"
	"net/http"
	"net/url"
	"testing"
)

func TestExtractGrantActionParams(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		prefix     string
		wantErr    error
		wantParams RouteParams
	}{
		{
			name:    "valid disable",
			method:  http.MethodPost,
			path:    "/api/runtime/grants/default/my-grant/disable",
			prefix:  "/api/runtime/grants/",
			wantErr: nil,
			wantParams: RouteParams{
				Namespace: "default",
				Name:      "my-grant",
				Action:    "disable",
			},
		},
		{
			name:    "valid enable",
			method:  http.MethodPost,
			path:    "/api/runtime/grants/my-namespace/my-grant/enable",
			prefix:  "/api/runtime/grants/",
			wantErr: nil,
			wantParams: RouteParams{
				Namespace: "my-namespace",
				Name:      "my-grant",
				Action:    "enable",
			},
		},
		{
			name:    "wrong method",
			method:  http.MethodGet,
			path:    "/api/runtime/grants/default/my-grant/disable",
			prefix:  "/api/runtime/grants/",
			wantErr: ErrMethodNotAllowed,
		},
		{
			name:    "invalid action",
			method:  http.MethodPost,
			path:    "/api/runtime/grants/default/my-grant/invalid",
			prefix:  "/api/runtime/grants/",
			wantErr: ErrInvalidAction,
		},
		{
			name:    "too few path parts",
			method:  http.MethodPost,
			path:    "/api/runtime/grants/default/my-grant",
			prefix:  "/api/runtime/grants/",
			wantErr: ErrInvalidPath,
		},
		{
			name:    "invalid namespace",
			method:  http.MethodPost,
			path:    "/api/runtime/grants/INVALID_NAMESPACE/my-grant/disable",
			prefix:  "/api/runtime/grants/",
			wantErr: ErrInvalidPath,
		},
		{
			name:    "invalid name",
			method:  http.MethodPost,
			path:    "/api/runtime/grants/default/INVALID_NAME/disable",
			prefix:  "/api/runtime/grants/",
			wantErr: ErrInvalidPath,
		},
		{
			name:    "too many path parts",
			method:  http.MethodPost,
			path:    "/api/runtime/grants/default/my-grant/extra/disable",
			prefix:  "/api/runtime/grants/",
			wantErr: ErrInvalidPath,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := &http.Request{
				Method: tc.method,
				URL:    &url.URL{Path: tc.path},
			}

			params, err := ExtractGrantActionParams(req, tc.prefix)

			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Errorf("ExtractGrantActionParams() error = %v, wantErr %v", err, tc.wantErr)
				}
				return
			}

			if err != nil {
				t.Errorf("ExtractGrantActionParams() unexpected error = %v", err)
				return
			}

			if params != tc.wantParams {
				t.Errorf("ExtractGrantActionParams() = %v, want %v", params, tc.wantParams)
			}
		})
	}
}

func TestExtractSessionActionParams(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		prefix     string
		wantErr    error
		wantParams RouteParams
	}{
		{
			name:    "valid revoke",
			method:  http.MethodPost,
			path:    "/api/runtime/sessions/default/my-session/revoke",
			prefix:  "/api/runtime/sessions/",
			wantErr: nil,
			wantParams: RouteParams{
				Namespace: "default",
				Name:      "my-session",
				Action:    "revoke",
			},
		},
		{
			name:    "valid unrevoke",
			method:  http.MethodPost,
			path:    "/api/runtime/sessions/my-namespace/my-session/unrevoke",
			prefix:  "/api/runtime/sessions/",
			wantErr: nil,
			wantParams: RouteParams{
				Namespace: "my-namespace",
				Name:      "my-session",
				Action:    "unrevoke",
			},
		},
		{
			name:    "wrong method",
			method:  http.MethodGet,
			path:    "/api/runtime/sessions/default/my-session/revoke",
			prefix:  "/api/runtime/sessions/",
			wantErr: ErrMethodNotAllowed,
		},
		{
			name:    "invalid action",
			method:  http.MethodPost,
			path:    "/api/runtime/sessions/default/my-session/invalid",
			prefix:  "/api/runtime/sessions/",
			wantErr: ErrInvalidAction,
		},
		{
			name:    "too many path parts",
			method:  http.MethodPost,
			path:    "/api/runtime/sessions/default/my-session/extra/revoke",
			prefix:  "/api/runtime/sessions/",
			wantErr: ErrInvalidPath,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := &http.Request{
				Method: tc.method,
				URL:    &url.URL{Path: tc.path},
			}

			params, err := ExtractSessionActionParams(req, tc.prefix)

			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Errorf("ExtractSessionActionParams() error = %v, wantErr %v", err, tc.wantErr)
				}
				return
			}

			if err != nil {
				t.Errorf("ExtractSessionActionParams() unexpected error = %v", err)
				return
			}

			if params != tc.wantParams {
				t.Errorf("ExtractSessionActionParams() = %v, want %v", params, tc.wantParams)
			}
		})
	}
}

func TestIsActionEnabled(t *testing.T) {
	tests := []struct {
		action   string
		expected bool
	}{
		{"enable", true},
		{"unrevoke", true},
		{"disable", false},
		{"revoke", false},
		{"unknown", false},
		{"", false},
	}

	for _, tc := range tests {
		t.Run(tc.action, func(t *testing.T) {
			result := IsActionEnabled(tc.action)
			if result != tc.expected {
				t.Errorf("IsActionEnabled(%q) = %v, expected %v", tc.action, result, tc.expected)
			}
		})
	}
}
