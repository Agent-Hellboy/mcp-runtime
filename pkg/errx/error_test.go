package errx

import (
	"errors"
	"reflect"
	"testing"
)

func TestError_New(t *testing.T) {
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
}

func TestError_Wrap(t *testing.T) {
	base := errors.New("base")
	cause := errors.New("cause")
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
}

func TestError_WithContext(t *testing.T) {
	key := "key"
	value := "value"
	err := New("70000", "CLI error", "test").WithContext(key, value)

	if err.Context()[key] != value {
		t.Errorf("Context()[%s] = %v, want %v", key, err.Context()[key], value)
	}
}

func TestError_WithContextMap(t *testing.T) {
	context := map[string]any{
		"key": "value",
	}
	err := New("70000", "CLI error", "test").WithContextMap(context)

	if !reflect.DeepEqual(err.Context(), context) {
		t.Errorf("Context() = %v, want %v", err.Context(), context)
	}
}

func TestError_WithBase(t *testing.T) {
	base := errors.New("base")
	err := New("70000", "CLI error", "test").WithBase(base)

	if err.Base() != base {
		t.Errorf("Base() = %v, want %v", err.Base(), base)
	}
	if !errors.Is(err, base) {
		t.Errorf("errors.Is(err, base) = %v, want %v", errors.Is(err, base), true)
	}
	if err.Unwrap() != nil {
		t.Errorf("Unwrap() = %v, want %v (should return nil)", err.Unwrap(), nil)
	}
}

func TestError_Is(t *testing.T) {
	base := errors.New("base")
	cause := errors.New("cause")
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
	if err.Unwrap() != cause {
		t.Errorf("Unwrap() = %v, want %v (should return cause, not base)", err.Unwrap(), cause)
	}
}

func TestError_Error(t *testing.T) {
	err := New("70000", "CLI error", "test")

	if err.Error() != "test" {
		t.Errorf("Error() = %v, want %v", err.Error(), "test")
	}
}

func TestError_Unwrap(t *testing.T) {
	base := errors.New("base")
	cause := errors.New("cause")
	err := Wrap("70000", "CLI error", "test", cause).WithBase(base)

	if err.Unwrap() != cause {
		t.Errorf("Unwrap() = %v, want %v (should return cause, not base)", err.Unwrap(), cause)
	}
}
