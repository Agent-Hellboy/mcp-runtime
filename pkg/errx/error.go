package errx

import "errors"

// Error is the base error type for MCP runtime errors.
type Error struct {
	code        string
	description string
	message     string
	context     map[string]any
	cause       error
	base        error
}

// New creates a new Error with the provided code, description, and message.
func New(code, description, message string) *Error {
	return &Error{
		code:        code,
		description: description,
		message:     message,
	}
}

// Wrap creates a new Error and attaches a cause error.
func Wrap(code, description, message string, cause error) *Error {
	return &Error{
		code:        code,
		description: description,
		message:     message,
		cause:       cause,
	}
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.message != "" {
		return e.message
	}
	if e.description != "" {
		return e.description
	}
	if e.code != "" {
		return e.code
	}
	return "error"
}

// Unwrap returns the immediate wrapped error (cause).
// This follows Go's error wrapping convention where Unwrap() returns
// the direct cause, not the base sentinel.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// Is implements error matching for sentinel errors.
// This allows errors.Is(err, sentinel) to match the base sentinel
// even though Unwrap() returns the cause.
func (e *Error) Is(target error) bool {
	if e == nil {
		return false
	}
	// Check if target matches the base sentinel
	if e.base != nil && errors.Is(e.base, target) {
		return true
	}
	// Also check if target matches the error itself (for direct comparison)
	return errors.Is(e.cause, target)
}

// Code returns the stable error code.
func (e *Error) Code() string {
	if e == nil {
		return ""
	}
	return e.code
}

// Description returns the category description.
func (e *Error) Description() string {
	if e == nil {
		return ""
	}
	return e.description
}

// Message returns the user-facing message.
func (e *Error) Message() string {
	if e == nil {
		return ""
	}
	return e.message
}

// Context returns a copy of the structured context.
func (e *Error) Context() map[string]any {
	if e == nil || len(e.context) == 0 {
		return nil
	}
	return cloneContext(e.context)
}

// Cause returns the wrapped error, if any.
func (e *Error) Cause() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// Base returns the sentinel base error, if any.
func (e *Error) Base() error {
	if e == nil {
		return nil
	}
	return e.base
}

// WithContext adds a context key/value pair.
// Returns a new error with the added context to avoid mutating the original.
func (e *Error) WithContext(key string, value any) *Error {
	if e == nil {
		return nil
	}
	// Clone the error to avoid mutating the original
	clone := &Error{
		code:        e.code,
		description: e.description,
		message:     e.message,
		cause:       e.cause,
		base:        e.base,
		context:     cloneContext(e.context),
	}
	if clone.context == nil {
		clone.context = make(map[string]any)
	}
	clone.context[key] = value
	return clone
}

// WithContextMap merges a context map into the error context.
// Returns a new error with the merged context to avoid mutating the original.
// Always returns a clone to maintain immutability, even if ctx is empty.
func (e *Error) WithContextMap(ctx map[string]any) *Error {
	if e == nil {
		return nil
	}
	// Clone the error to avoid mutating the original
	clone := &Error{
		code:        e.code,
		description: e.description,
		message:     e.message,
		cause:       e.cause,
		base:        e.base,
		context:     cloneContext(e.context),
	}
	// Only merge context if ctx is not empty
	if len(ctx) > 0 {
		if clone.context == nil {
			clone.context = make(map[string]any, len(ctx))
		}
		for key, value := range ctx {
			clone.context[key] = value
		}
	}
	return clone
}

// WithBase sets the sentinel base error used for errors.Is matching.
// Returns a new error with the base set to avoid mutating the original.
func (e *Error) WithBase(base error) *Error {
	if e == nil {
		return nil
	}
	// Clone the error to avoid mutating the original
	return &Error{
		code:        e.code,
		description: e.description,
		message:     e.message,
		cause:       e.cause,
		base:        base,
		context:     cloneContext(e.context),
	}
}

func cloneContext(ctx map[string]any) map[string]any {
	clone := make(map[string]any, len(ctx))
	for key, value := range ctx {
		clone[key] = value
	}
	return clone
}
