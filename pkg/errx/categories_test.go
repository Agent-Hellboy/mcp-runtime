package errx

import (
	"errors"
	"testing"
)

func TestCategories_Operator(t *testing.T) {
	err := Operator("test")

	if err.Code() != CodeOperator {
		t.Errorf("Code() = %q, want %q", err.Code(), CodeOperator)
	}
}

func TestCategories_WrapOperator(t *testing.T) {
	cause := errors.New("cause")
	err := WrapOperator("test", cause)

	if err.Code() != CodeOperator {
		t.Errorf("Code() = %q, want %q", err.Code(), CodeOperator)
	}
	if err.Cause() != cause {
		t.Errorf("Cause() = %v, want %v", err.Cause(), cause)
	}
}

func TestCategories_CreateByCode(t *testing.T) {
	err := CreateByCode(CodeCLI, DescCLI, "test", nil)

	if err.Code() != CodeCLI {
		t.Errorf("Code() = %q, want %q", err.Code(), CodeCLI)
	}
}

func TestCategories_CLI(t *testing.T) {
	err := CLI("test message")

	if err.Code() != CodeCLI {
		t.Errorf("Code() = %q, want %q", err.Code(), CodeCLI)
	}
	if err.Description() != DescCLI {
		t.Errorf("Description() = %q, want %q", err.Description(), DescCLI)
	}
	if err.Message() != "test message" {
		t.Errorf("Message() = %q, want %q", err.Message(), "test message")
	}
}

func TestCategories_WrapCLI(t *testing.T) {
	cause := errors.New("underlying error")
	err := WrapCLI("test message", cause)

	if err.Code() != CodeCLI {
		t.Errorf("Code() = %q, want %q", err.Code(), CodeCLI)
	}
	if err.Description() != DescCLI {
		t.Errorf("Description() = %q, want %q", err.Description(), DescCLI)
	}
	if err.Message() != "test message" {
		t.Errorf("Message() = %q, want %q", err.Message(), "test message")
	}
	if err.Cause() != cause {
		t.Errorf("Cause() = %v, want %v", err.Cause(), cause)
	}
	if err.Unwrap() != cause {
		t.Errorf("Unwrap() = %v, want %v", err.Unwrap(), cause)
	}
}

func TestCategories_FromSentinel(t *testing.T) {
	t.Run("with valid lookup", func(t *testing.T) {
		sentinel := errors.New("sentinel error")
		lookupSpec := func(err error) (code, description string) {
			return CodeCLI, DescCLI
		}
		err := FromSentinel(sentinel, lookupSpec, "test message", nil)

		if err.Code() != CodeCLI {
			t.Errorf("Code() = %q, want %q", err.Code(), CodeCLI)
		}
		if err.Description() != DescCLI {
			t.Errorf("Description() = %q, want %q", err.Description(), DescCLI)
		}
		if err.Message() != "test message" {
			t.Errorf("Message() = %q, want %q", err.Message(), "test message")
		}
		if !errors.Is(err, sentinel) {
			t.Errorf("errors.Is(err, sentinel) = %v, want %v", errors.Is(err, sentinel), true)
		}
		if err.Base() != sentinel {
			t.Errorf("Base() = %v, want %v", err.Base(), sentinel)
		}
	})

	t.Run("with empty lookup result (fallback to CLI)", func(t *testing.T) {
		sentinel := errors.New("unknown sentinel")
		lookupSpec := func(err error) (code, description string) {
			return "", "" // Empty lookup result
		}
		err := FromSentinel(sentinel, lookupSpec, "test message", nil)

		if err.Code() != CodeCLI {
			t.Errorf("Code() = %q, want %q (should fallback to CLI)", err.Code(), CodeCLI)
		}
		if err.Description() != DescCLI {
			t.Errorf("Description() = %q, want %q (should fallback to CLI)", err.Description(), DescCLI)
		}
		if !errors.Is(err, sentinel) {
			t.Errorf("errors.Is(err, sentinel) = %v, want %v", errors.Is(err, sentinel), true)
		}
	})

	t.Run("with cause error", func(t *testing.T) {
		sentinel := errors.New("sentinel error")
		cause := errors.New("underlying cause")
		lookupSpec := func(err error) (code, description string) {
			return CodeCLI, DescCLI
		}
		err := FromSentinel(sentinel, lookupSpec, "test message", cause)

		if err.Cause() != cause {
			t.Errorf("Cause() = %v, want %v", err.Cause(), cause)
		}
		if err.Unwrap() != cause {
			t.Errorf("Unwrap() = %v, want %v", err.Unwrap(), cause)
		}
		if !errors.Is(err, sentinel) {
			t.Errorf("errors.Is(err, sentinel) = %v, want %v", errors.Is(err, sentinel), true)
		}
	})

	t.Run("with different category lookup", func(t *testing.T) {
		sentinel := errors.New("operator sentinel")
		lookupSpec := func(err error) (code, description string) {
			return CodeOperator, DescOperator
		}
		err := FromSentinel(sentinel, lookupSpec, "operator error", nil)

		if err.Code() != CodeOperator {
			t.Errorf("Code() = %q, want %q", err.Code(), CodeOperator)
		}
		if err.Description() != DescOperator {
			t.Errorf("Description() = %q, want %q", err.Description(), DescOperator)
		}
		if !errors.Is(err, sentinel) {
			t.Errorf("errors.Is(err, sentinel) = %v, want %v", errors.Is(err, sentinel), true)
		}
	})

	t.Run("panics on nil sentinel", func(t *testing.T) {
		lookupSpec := func(err error) (code, description string) {
			return CodeCLI, DescCLI
		}
		defer func() {
			if r := recover(); r == nil {
				t.Error("FromSentinel() with nil sentinel should panic")
			}
		}()
		_ = FromSentinel(nil, lookupSpec, "test message", nil)
	})
}
