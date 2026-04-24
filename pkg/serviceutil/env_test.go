// Package serviceutil provides tests for environment utilities.
package serviceutil

import (
	"os"
	"testing"
)

func TestEnvOr(t *testing.T) {
	// Test with unset variable
	os.Unsetenv("TEST_VAR")
	result := EnvOr("TEST_VAR", "default")
	if result != "default" {
		t.Errorf("Expected 'default', got %q", result)
	}

	// Test with set variable
	os.Setenv("TEST_VAR", "value")
	defer os.Unsetenv("TEST_VAR")

	result = EnvOr("TEST_VAR", "default")
	if result != "value" {
		t.Errorf("Expected 'value', got %q", result)
	}

	// Test with whitespace-only value
	os.Setenv("TEST_VAR", "   ")
	result = EnvOr("TEST_VAR", "default")
	if result != "default" {
		t.Errorf("Expected 'default' for whitespace, got %q", result)
	}
}

func TestBoolEnv(t *testing.T) {
	tests := []struct {
		value    string
		expected bool
		ok       bool
	}{
		{"true", true, true},
		{"TRUE", true, true},
		{"True", true, true},
		{"1", true, true},
		{"false", false, true},
		{"FALSE", false, true},
		{"False", false, true},
		{"0", false, true},
		{"", false, false},
		{"invalid", false, false},
	}

	for _, tc := range tests {
		t.Run(tc.value, func(t *testing.T) {
			os.Setenv("TEST_BOOL", tc.value)
			defer os.Unsetenv("TEST_BOOL")

			val, ok := BoolEnv("TEST_BOOL")
			if val != tc.expected || ok != tc.ok {
				t.Errorf("BoolEnv() = (%v, %v), expected (%v, %v)", val, ok, tc.expected, tc.ok)
			}
		})
	}

	// Test unset variable
	os.Unsetenv("TEST_BOOL_UNSET")
	val, ok := BoolEnv("TEST_BOOL_UNSET")
	if val != false || ok != false {
		t.Errorf("BoolEnv(unset) = (%v, %v), expected (false, false)", val, ok)
	}
}
