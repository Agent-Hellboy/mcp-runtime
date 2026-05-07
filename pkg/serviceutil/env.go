// Package serviceutil provides shared utilities for MCP services.
// This package centralizes common helper functions to avoid duplication
// across service implementations.
package serviceutil

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// EnvOr returns the value of an environment variable or a fallback if not set.
// If the environment variable is set to a non-empty value, it returns that value.
// Otherwise, it returns the provided fallback value.
func EnvOr(key, fallback string) string {
	if val := strings.TrimSpace(os.Getenv(key)); val != "" {
		return val
	}
	return fallback
}

// BoolEnv parses a boolean environment variable.
// It returns the parsed boolean value and true if parsing succeeded.
// Returns false, false if the variable is not set or parsing failed.
func BoolEnv(key string) (bool, bool) {
	if val := strings.TrimSpace(os.Getenv(key)); val != "" {
		switch strings.ToLower(val) {
		case "yes", "on":
			return true, true
		case "no", "off":
			return false, true
		}
		parsed, err := strconv.ParseBool(val)
		if err == nil {
			return parsed, true
		}
	}
	return false, false
}

// EnvInt parses an integer environment variable, falling back when unset or invalid.
func EnvInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

// EnvDuration parses a duration environment variable, falling back when unset or invalid.
func EnvDuration(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}
	return value
}
