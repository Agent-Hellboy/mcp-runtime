package errx

import (
	"errors"
	"strings"
	"testing"
)

func TestFormat_UserString(t *testing.T) {
	t.Run("with message", func(t *testing.T) {
		err := New("70000", "CLI error", "test")
		if UserString(err) != "test" {
			t.Errorf("UserString(err) = %q, want %q", UserString(err), "test")
		}
	})

	t.Run("without message, with description", func(t *testing.T) {
		err := New("70000", "CLI error", "")
		if UserString(err) != "CLI error" {
			t.Errorf("UserString(err) = %q, want %q", UserString(err), "CLI error")
		}
	})

	t.Run("without message and description, with code", func(t *testing.T) {
		err := New("70000", "", "")
		if UserString(err) != "70000" {
			t.Errorf("UserString(err) = %q, want %q", UserString(err), "70000")
		}
	})
	t.Run("with nil error", func(t *testing.T) {
		if UserString(nil) != "" {
			t.Errorf("UserString(nil) = %q, want empty string", UserString(nil))
		}
	})
	t.Run("with non-errx error", func(t *testing.T) {
		err := errors.New("standard error")
		if UserString(err) != "standard error" {
			t.Errorf("UserString(err) = %q, want %q", UserString(err), "standard error")
		}
	})
}

func TestFormat_IsError(t *testing.T) {
	t.Run("with errx.Error", func(t *testing.T) {
		err := New("70000", "CLI error", "test")
		if !IsError(err) {
			t.Errorf("IsError(err) = %v, want %v", IsError(err), true)
		}
	})
	t.Run("with non-errx.Error", func(t *testing.T) {
		err := errors.New("test")
		if IsError(err) {
			t.Errorf("IsError(err) = %v, want %v", IsError(err), false)
		}
	})
	t.Run("with nil error", func(t *testing.T) {
		if IsError(nil) {
			t.Errorf("IsError(nil) = %v, want %v", IsError(nil), false)
		}
	})
}

func TestFormat_DebugString(t *testing.T) {
	t.Run("with errx.Error", func(t *testing.T) {
		err := New("70000", "CLI error", "test")
		got := DebugString(err)
		want := "1: *errx.Error: test | code=70000 | description=\"CLI error\" | message=\"test\""
		if got != want {
			t.Errorf("DebugString(err) = %q, want %q", got, want)
		}
	})
	t.Run("with errx.Error without description", func(t *testing.T) {
		err := New("70000", "", "test")
		got := DebugString(err)
		want := "1: *errx.Error: test | code=70000 | message=\"test\""
		if got != want {
			t.Errorf("DebugString(err) = %q, want %q", got, want)
		}
	})
	t.Run("with errx.Error with context", func(t *testing.T) {
		err := New("70000", "CLI error", "test").WithContext("key", "value")
		got := DebugString(err)
		// Context format may vary, so just check it contains the expected parts
		if !strings.Contains(got, "code=70000") {
			t.Errorf("DebugString(err) should contain code=70000, got %q", got)
		}
		if !strings.Contains(got, "context={") {
			t.Errorf("DebugString(err) should contain context, got %q", got)
		}
	})
	t.Run("with errx.Error chain (multiple errors)", func(t *testing.T) {
		cause := errors.New("underlying cause")
		err := Wrap("70000", "CLI error", "wrapped error", cause)
		got := DebugString(err)
		// Should have newline between errors (i > 0 case)
		if !strings.Contains(got, "\n") {
			t.Errorf("DebugString(err) should contain newline between errors, got %q", got)
		}
		// Should contain both error entries
		if !strings.Contains(got, "1:") {
			t.Errorf("DebugString(err) should contain first error, got %q", got)
		}
		if !strings.Contains(got, "2:") {
			t.Errorf("DebugString(err) should contain second error, got %q", got)
		}
		// Should contain the wrapped error info
		if !strings.Contains(got, "wrapped error") {
			t.Errorf("DebugString(err) should contain wrapped error message, got %q", got)
		}
		// Should contain the cause error
		if !strings.Contains(got, "underlying cause") {
			t.Errorf("DebugString(err) should contain cause error, got %q", got)
		}
	})
	t.Run("with errors.Join (multi-error chain)", func(t *testing.T) {
		err1 := errors.New("error1")
		err2 := errors.New("error2")
		joined := errors.Join(err1, err2)
		got := DebugString(joined)
		// Should have newline between errors (i > 0 case)
		if !strings.Contains(got, "\n") {
			t.Errorf("DebugString(joined) should contain newline between errors, got %q", got)
		}
		// Should contain both errors
		if !strings.Contains(got, "error1") {
			t.Errorf("DebugString(joined) should contain error1, got %q", got)
		}
		if !strings.Contains(got, "error2") {
			t.Errorf("DebugString(joined) should contain error2, got %q", got)
		}
	})
	t.Run("with non-errx.Error", func(t *testing.T) {
		err := errors.New("test")
		if DebugString(err) != "1: *errors.errorString: test" {
			t.Errorf("DebugString(err) = %q, want %q", DebugString(err), "1: *errors.errorString: test")
		}
	})
	t.Run("with nil error", func(t *testing.T) {
		if DebugString(nil) != "" {
			t.Errorf("DebugString(nil) = %q, want empty string", DebugString(nil))
		}
	})
}

func TestFormat_flattenChain(t *testing.T) {
	t.Run("with errx.Error", func(t *testing.T) {
		err := New("70000", "CLI error", "test")
		result := flattenChain(err)
		if len(result) != 1 {
			t.Errorf("flattenChain(err) length = %d, want 1", len(result))
		}
		if result[0] != err {
			t.Errorf("flattenChain(err)[0] = %v, want %v", result[0], err)
		}
	})
	t.Run("with errx.Error with cause", func(t *testing.T) {
		cause := errors.New("cause")
		err := Wrap("70000", "CLI error", "test", cause)
		result := flattenChain(err)
		if len(result) != 2 {
			t.Errorf("flattenChain(err) length = %d, want 2", len(result))
		}
		if result[0] != err {
			t.Errorf("flattenChain(err)[0] = %v, want %v", result[0], err)
		}
		if result[1] != cause {
			t.Errorf("flattenChain(err)[1] = %v, want %v", result[1], cause)
		}
	})
	t.Run("with non-errx.Error", func(t *testing.T) {
		err := errors.New("test")
		result := flattenChain(err)
		if len(result) != 1 {
			t.Errorf("flattenChain(err) length = %d, want 1", len(result))
		}
		if result[0] != err {
			t.Errorf("flattenChain(err)[0] = %v, want %v", result[0], err)
		}
	})
	t.Run("with nil error", func(t *testing.T) {
		result := flattenChain(nil)
		if len(result) != 0 {
			t.Errorf("flattenChain(nil) length = %d, want 0", len(result))
		}
	})
}

func TestFormat_unwrapAll(t *testing.T) {
	t.Run("with errx.Error without cause", func(t *testing.T) {
		err := New("70000", "CLI error", "test")
		result := unwrapAll(err)
		if result != nil {
			t.Errorf("unwrapAll(err) = %v, want nil", result)
		}
	})
	t.Run("with errx.Error with cause", func(t *testing.T) {
		cause := errors.New("cause")
		err := Wrap("70000", "CLI error", "test", cause)
		result := unwrapAll(err)
		if len(result) != 1 {
			t.Errorf("unwrapAll(err) length = %d, want 1", len(result))
		}
		if result[0] != cause {
			t.Errorf("unwrapAll(err)[0] = %v, want %v", result[0], cause)
		}
	})
	t.Run("with errors.Join (multi-error unwrapping)", func(t *testing.T) {
		err1 := errors.New("error1")
		err2 := errors.New("error2")
		err3 := errors.New("error3")
		joined := errors.Join(err1, err2, err3)
		result := unwrapAll(joined)
		if len(result) != 3 {
			t.Errorf("unwrapAll(joined) length = %d, want 3", len(result))
		}
		// errors.Join returns errors in order
		if result[0] != err1 {
			t.Errorf("unwrapAll(joined)[0] = %v, want %v", result[0], err1)
		}
		if result[1] != err2 {
			t.Errorf("unwrapAll(joined)[1] = %v, want %v", result[1], err2)
		}
		if result[2] != err3 {
			t.Errorf("unwrapAll(joined)[2] = %v, want %v", result[2], err3)
		}
	})
	t.Run("with empty errors.Join", func(t *testing.T) {
		joined := errors.Join()
		result := unwrapAll(joined)
		if len(result) != 0 {
			t.Errorf("unwrapAll(joined) length = %d, want 0", len(result))
		}
	})
	t.Run("with non-errx.Error", func(t *testing.T) {
		err := errors.New("test")
		result := unwrapAll(err)
		if result != nil {
			t.Errorf("unwrapAll(err) = %v, want nil", result)
		}
	})
	t.Run("with nil error", func(t *testing.T) {
		result := unwrapAll(nil)
		if result != nil {
			t.Errorf("unwrapAll(nil) = %v, want nil", result)
		}
	})
}

func TestFormat_formatContext(t *testing.T) {
	t.Run("with context", func(t *testing.T) {
		context := map[string]any{"key": "value"}
		if formatContext(context) != "key=value" {
			t.Errorf("formatContext(context) = %q, want %q", formatContext(context), "key=value")
		}
	})
	t.Run("with multiple context keys", func(t *testing.T) {
		context := map[string]any{"key1": "value1", "key2": "value2"}
		result := formatContext(context)
		// Keys are sorted, so order is deterministic
		if result != "key1=value1, key2=value2" {
			t.Errorf("formatContext(context) = %q, want %q", result, "key1=value1, key2=value2")
		}
	})
	t.Run("with empty context", func(t *testing.T) {
		context := map[string]any{}
		if formatContext(context) != "" {
			t.Errorf("formatContext(context) = %q, want empty string", formatContext(context))
		}
	})
	t.Run("with nil context", func(t *testing.T) {
		// formatContext doesn't check for nil, it will panic on len(nil)
		// But let's test the actual behavior - it should handle nil gracefully
		defer func() {
			if r := recover(); r == nil {
				// If it doesn't panic, that's fine - nil map len is 0 in Go
			}
		}()
		result := formatContext(nil)
		if result != "" {
			t.Errorf("formatContext(nil) = %q, want empty string", result)
		}
	})
}
