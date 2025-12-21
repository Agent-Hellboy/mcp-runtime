package errx

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// UserString returns a user-safe error message.
// It extracts the most user-friendly message from an errx.Error,
// falling back to the standard error message for non-errx errors.
func UserString(err error) string {
	if err == nil {
		return ""
	}
	var e *Error
	if errors.As(err, &e) {
		if e.message != "" {
			return e.message
		}
		if e.description != "" {
			return e.description
		}
		if e.code != "" {
			return e.code
		}
	}
	return err.Error()
}

// IsError checks if the given error is an errx.Error.
func IsError(err error) bool {
	if err == nil {
		return false
	}
	var e *Error
	return errors.As(err, &e)
}

// DebugString returns a verbose error string with codes, context, and chain.
func DebugString(err error) string {
	if err == nil {
		return ""
	}
	chain := flattenChain(err)
	var b strings.Builder
	for i, item := range chain {
		if i > 0 {
			b.WriteByte('\n')
		}
		switch typed := item.(type) {
		case *Error:
			b.WriteString(fmt.Sprintf("%d: %T: %s", i+1, typed, typed.Error()))
			if typed.code != "" {
				b.WriteString(fmt.Sprintf(" | code=%s", typed.code))
			}
			if typed.description != "" {
				b.WriteString(fmt.Sprintf(" | description=%q", typed.description))
			}
			if typed.message != "" {
				b.WriteString(fmt.Sprintf(" | message=%q", typed.message))
			}
			if len(typed.context) > 0 {
				b.WriteString(" | context={")
				b.WriteString(formatContext(typed.context))
				b.WriteByte('}')
			}
		default:
			b.WriteString(fmt.Sprintf("%d: %T: %s", i+1, item, item.Error()))
		}
	}
	return b.String()
}

func flattenChain(err error) []error {
	var out []error
	queue := []error{err}
	const maxEntries = 64
	for len(queue) > 0 && len(out) < maxEntries {
		current := queue[0]
		queue = queue[1:]
		if current == nil {
			continue
		}
		out = append(out, current)
		queue = append(queue, unwrapAll(current)...)
	}
	return out
}

func unwrapAll(err error) []error {
	switch unwrapped := err.(type) {
	case interface{ Unwrap() []error }:
		return unwrapped.Unwrap()
	case interface{ Unwrap() error }:
		if next := unwrapped.Unwrap(); next != nil {
			return []error{next}
		}
	}
	return nil
}

func formatContext(ctx map[string]any) string {
	keys := make([]string, 0, len(ctx))
	for key := range ctx {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", key, ctx[key]))
	}
	return strings.Join(parts, ", ")
}
