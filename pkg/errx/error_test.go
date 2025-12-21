package errx

import (
	"errors"
	"reflect"
	"testing"
)

func TestError_New(t *testing.T) {
	t.Run("with valid inputs", func(t *testing.T) {
		err := New("70000", "CLI error", "test")

		if err.Code() != "70000" {
			t.Errorf("Code() = %q, want %q", err.Code(), "70000")
		}
		if err.Description() != "CLI error" {
			t.Errorf("Description() = %q, want %q", err.Description(), "CLI error")
		}
		if err.Message() != "test" {
			t.Errorf("Message() = %q, want %q", err.Message(), "test")
		}
	})

	t.Run("with empty description and message", func(t *testing.T) {
		err := New("70000", "", "")
		if err.Code() != "70000" {
			t.Errorf("Code() = %q, want %q", err.Code(), "70000")
		}
	})

	t.Run("panics on empty code", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("New() with empty code should panic")
			}
		}()
		_ = New("", "CLI error", "test")
	})
}

func TestError_Code(t *testing.T) {
	t.Run("with code", func(t *testing.T) {
		err := New("70000", "CLI error", "test")
		if err.Code() != "70000" {
			t.Errorf("Code() = %q, want %q", err.Code(), "70000")
		}
	})

	t.Run("nil receiver", func(t *testing.T) {
		var nilErr *Error
		if nilErr.Code() != "" {
			t.Errorf("Code() on nil receiver = %q, want empty string", nilErr.Code())
		}
	})
}

func TestError_Description(t *testing.T) {
	t.Run("with description", func(t *testing.T) {
		err := New("70000", "CLI error", "test")
		if err.Description() != "CLI error" {
			t.Errorf("Description() = %q, want %q", err.Description(), "CLI error")
		}
	})

	t.Run("nil receiver", func(t *testing.T) {
		var nilErr *Error
		if nilErr.Description() != "" {
			t.Errorf("Description() on nil receiver = %q, want empty string", nilErr.Description())
		}
	})
}

func TestError_Message(t *testing.T) {
	t.Run("with message", func(t *testing.T) {
		err := New("70000", "CLI error", "test")
		if err.Message() != "test" {
			t.Errorf("Message() = %q, want %q", err.Message(), "test")
		}
	})

	t.Run("nil receiver", func(t *testing.T) {
		var nilErr *Error
		if nilErr.Message() != "" {
			t.Errorf("Message() on nil receiver = %q, want empty string", nilErr.Message())
		}
	})
}

func TestError_Context(t *testing.T) {
	t.Run("with context", func(t *testing.T) {
		err := New("70000", "CLI error", "test").WithContext("key", "value")
		ctx := err.Context()
		if ctx == nil {
			t.Fatal("Context() returned nil, expected non-nil map")
		}
		if ctx["key"] != "value" {
			t.Errorf("Context()[key] = %v, want %v", ctx["key"], "value")
		}
	})

	t.Run("without context", func(t *testing.T) {
		err := New("70000", "CLI error", "test")
		ctx := err.Context()
		if ctx != nil {
			t.Errorf("Context() = %v, want nil", ctx)
		}
	})

	t.Run("nil receiver", func(t *testing.T) {
		var nilErr *Error
		if nilErr.Context() != nil {
			t.Errorf("Context() on nil receiver = %v, want nil", nilErr.Context())
		}
	})
}

func TestError_Cause(t *testing.T) {
	cause := errors.New("cause")

	t.Run("with cause", func(t *testing.T) {
		err := Wrap("70000", "CLI error", "test", cause)
		if err.Cause() != cause {
			t.Errorf("Cause() = %v, want %v", err.Cause(), cause)
		}
	})

	t.Run("without cause", func(t *testing.T) {
		err := New("70000", "CLI error", "test")
		if err.Cause() != nil {
			t.Errorf("Cause() = %v, want nil", err.Cause())
		}
	})

	t.Run("nil receiver", func(t *testing.T) {
		var nilErr *Error
		if nilErr.Cause() != nil {
			t.Errorf("Cause() on nil receiver = %v, want nil", nilErr.Cause())
		}
	})
}

func TestError_Wrap(t *testing.T) {
	base := errors.New("base")
	cause := errors.New("cause")

	t.Run("with valid inputs", func(t *testing.T) {
		err := Wrap("70000", "CLI error", "test", cause).WithBase(base)

		if !errors.Is(err, base) {
			t.Errorf("errors.Is(err, base) = %v, want %v", errors.Is(err, base), true)
		}
		if !errors.Is(err, cause) {
			t.Errorf("errors.Is(err, cause) = %v, want %v", errors.Is(err, cause), true)
		}
		if err.Cause() != cause {
			t.Errorf("Cause() = %v, want %v", err.Cause(), cause)
		}
		if err.Base() != base {
			t.Errorf("Base() = %v, want %v", err.Base(), base)
		}
		// Unwrap() should return the cause (immediate wrapped error), not the base
		if err.Unwrap() != cause {
			t.Errorf("Unwrap() = %v, want %v (should return cause, not base)", err.Unwrap(), cause)
		}
	})

	t.Run("with nil cause", func(t *testing.T) {
		err := Wrap("70000", "CLI error", "test", nil)
		if err.Cause() != nil {
			t.Errorf("Cause() = %v, want nil", err.Cause())
		}
		if err.Unwrap() != nil {
			t.Errorf("Unwrap() = %v, want nil", err.Unwrap())
		}
	})

	t.Run("panics on empty code", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("Wrap() with empty code should panic")
			}
		}()
		_ = Wrap("", "CLI error", "test", cause)
	})
}

func TestError_WithContext(t *testing.T) {
	key := "key"
	value := "value"
	err := New("70000", "CLI error", "test")

	t.Run("with context", func(t *testing.T) {
		errWithContext := err.WithContext(key, value)
		if errWithContext == nil {
			t.Fatal("WithContext() returned nil, expected non-nil error")
		}
		if errWithContext.Context()[key] != value {
			t.Errorf("Context()[%s] = %v, want %v", key, errWithContext.Context()[key], value)
		}
	})

	t.Run("nil receiver panics", func(t *testing.T) {
		var nilErr *Error
		defer func() {
			if r := recover(); r == nil {
				t.Error("WithContext() on nil receiver should panic")
			}
		}()
		_ = nilErr.WithContext(key, value)
	})

	t.Run("empty key panics", func(t *testing.T) {
		err := New("70000", "CLI error", "test")
		defer func() {
			if r := recover(); r == nil {
				t.Error("WithContext() with empty key should panic")
			}
		}()
		_ = err.WithContext("", value)
	})

	t.Run("nil value allowed", func(t *testing.T) {
		err := New("70000", "CLI error", "test")
		errWithContext := err.WithContext("key", nil)
		if errWithContext.Context()["key"] != nil {
			t.Errorf("Context()[key] = %v, want nil", errWithContext.Context()["key"])
		}
	})
}

func TestError_WithContextMap(t *testing.T) {
	context := map[string]any{
		"key": "value",
	}
	err := New("70000", "CLI error", "test")

	t.Run("with context map", func(t *testing.T) {
		errWithContext := err.WithContextMap(context)
		if errWithContext == nil {
			t.Fatal("WithContextMap() returned nil, expected non-nil error")
		}
		if !reflect.DeepEqual(errWithContext.Context(), context) {
			t.Errorf("Context() = %v, want %v", errWithContext.Context(), context)
		}
	})

	t.Run("with empty context map", func(t *testing.T) {
		emptyContext := map[string]any{}
		errWithEmptyContext := err.WithContextMap(emptyContext)
		if errWithEmptyContext == nil {
			t.Fatal("WithContextMap(empty) returned nil, expected non-nil error")
		}
		// Should return a clone even with empty context
		if errWithEmptyContext == err {
			t.Error("WithContextMap(empty) returned same instance, expected clone")
		}
	})

	t.Run("with nil context map", func(t *testing.T) {
		errWithNilContext := err.WithContextMap(nil)
		if errWithNilContext == nil {
			t.Fatal("WithContextMap(nil) returned nil, expected non-nil error")
		}
		// Should return a clone even with nil context
		if errWithNilContext == err {
			t.Error("WithContextMap(nil) returned same instance, expected clone")
		}
	})

	t.Run("nil receiver with context map panics", func(t *testing.T) {
		var nilErr *Error
		defer func() {
			if r := recover(); r == nil {
				t.Error("WithContextMap() on nil receiver should panic")
			}
		}()
		_ = nilErr.WithContextMap(context)
	})

	t.Run("nil receiver with nil context map panics", func(t *testing.T) {
		var nilErr *Error
		defer func() {
			if r := recover(); r == nil {
				t.Error("WithContextMap(nil) on nil receiver should panic")
			}
		}()
		_ = nilErr.WithContextMap(nil)
	})

	t.Run("empty key in context map panics", func(t *testing.T) {
		contextWithEmptyKey := map[string]any{
			"key":  "value",
			"":     "empty key",
			"key2": "value2",
		}
		defer func() {
			if r := recover(); r == nil {
				t.Error("WithContextMap() with empty key should panic")
			}
		}()
		_ = err.WithContextMap(contextWithEmptyKey)
	})

	t.Run("nil values in context map allowed", func(t *testing.T) {
		contextWithNilValues := map[string]any{
			"key1": "value",
			"key2": nil,
			"key3": 123,
		}
		errWithContext := err.WithContextMap(contextWithNilValues)
		if errWithContext.Context()["key2"] != nil {
			t.Errorf("Context()[key2] = %v, want nil", errWithContext.Context()["key2"])
		}
	})
}

func TestError_WithBase(t *testing.T) {
	base := errors.New("base")
	err := New("70000", "CLI error", "test")

	t.Run("with base", func(t *testing.T) {
		errWithBase := err.WithBase(base)
		if errWithBase == nil {
			t.Fatal("WithBase() returned nil, expected non-nil error")
		}
		if errWithBase.Base() != base {
			t.Errorf("Base() = %v, want %v", errWithBase.Base(), base)
		}
	})

	t.Run("with nil base", func(t *testing.T) {
		errWithNilBase := err.WithBase(nil)
		if errWithNilBase == nil {
			t.Fatal("WithBase(nil) returned nil, expected non-nil error")
		}
		if errWithNilBase.Base() != nil {
			t.Errorf("Base() = %v, want nil", errWithNilBase.Base())
		}
	})

	t.Run("nil receiver with base panics", func(t *testing.T) {
		var nilErr *Error
		defer func() {
			if r := recover(); r == nil {
				t.Error("WithBase() on nil receiver should panic")
			}
		}()
		_ = nilErr.WithBase(base)
	})

	t.Run("nil receiver with nil base panics", func(t *testing.T) {
		var nilErr *Error
		defer func() {
			if r := recover(); r == nil {
				t.Error("WithBase(nil) on nil receiver should panic")
			}
		}()
		_ = nilErr.WithBase(nil)
	})

	t.Run("Base() on nil receiver", func(t *testing.T) {
		var nilErr *Error
		if nilErr.Base() != nil {
			t.Errorf("Base() on nil receiver = %v, want nil", nilErr.Base())
		}
	})
}

func TestError_Is(t *testing.T) {
	base := errors.New("base")
	cause := errors.New("cause")
	err := Wrap("70000", "CLI error", "test", cause).WithBase(base)

	t.Run("with base and cause", func(t *testing.T) {
		if !errors.Is(err, base) {
			t.Errorf("errors.Is(err, base) = %v, want %v", errors.Is(err, base), true)
		}
		if !errors.Is(err, cause) {
			t.Errorf("errors.Is(err, cause) = %v, want %v", errors.Is(err, cause), true)
		}
		if err.Cause() != cause {
			t.Errorf("Cause() = %v, want %v", err.Cause(), cause)
		}
		if err.Unwrap() != cause {
			t.Errorf("Unwrap() = %v, want %v (should return cause, not base)", err.Unwrap(), cause)
		}
	})

	t.Run("nil receiver", func(t *testing.T) {
		var nilErr *Error
		if nilErr.Is(base) {
			t.Errorf("Is() on nil receiver = true, want false")
		}
		if nilErr.Is(cause) {
			t.Errorf("Is() on nil receiver = true, want false")
		}
		if nilErr.Is(nil) {
			t.Errorf("Is(nil) on nil receiver = true, want false")
		}
	})
}

func TestError_Error(t *testing.T) {
	t.Run("with message", func(t *testing.T) {
		err := New("70000", "CLI error", "message")
		if err.Error() != "message" {
			t.Errorf("Error() = %v, want %v", err.Error(), "message")
		}
	})

	t.Run("without message, with description", func(t *testing.T) {
		err := New("70000", "CLI error", "")
		if err.Error() != "CLI error" {
			t.Errorf("Error() = %v, want %v", err.Error(), "CLI error")
		}
	})

	t.Run("without message and description, with code", func(t *testing.T) {
		err := New("70000", "", "")
		if err.Error() != "70000" {
			t.Errorf("Error() = %v, want %v", err.Error(), "70000")
		}
	})

	t.Run("nil receiver", func(t *testing.T) {
		var nilErr *Error
		if nilErr.Error() != "" {
			t.Errorf("Error() on nil receiver = %q, want empty string", nilErr.Error())
		}
	})
	t.Run("with nil error", func(t *testing.T) {
		var nilErr = &Error{}
		if nilErr.Error() != "error" {
			t.Errorf("Error() on nil receiver = %q, want %q", nilErr.Error(), "error")
		}
	})
}

func TestError_Unwrap(t *testing.T) {
	base := errors.New("base")
	cause := errors.New("cause")

	t.Run("with cause", func(t *testing.T) {
		err := Wrap("70000", "CLI error", "test", cause).WithBase(base)
		if err.Unwrap() != cause {
			t.Errorf("Unwrap() = %v, want %v (should return cause, not base)", err.Unwrap(), cause)
		}
	})

	t.Run("without cause", func(t *testing.T) {
		err := New("70000", "CLI error", "test")
		if err.Unwrap() != nil {
			t.Errorf("Unwrap() = %v, want nil", err.Unwrap())
		}
	})

	t.Run("nil receiver", func(t *testing.T) {
		var nilErr *Error
		if nilErr.Unwrap() != nil {
			t.Errorf("Unwrap() on nil receiver = %v, want nil", nilErr.Unwrap())
		}
	})
}
