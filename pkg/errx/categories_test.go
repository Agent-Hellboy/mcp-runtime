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

func TestCategories_FromSentinel(t *testing.T) {
	sentinel := errors.New("sentinel")
	lookupSpec := func(err error) (code, description string) {
		return CodeCLI, DescCLI
	}
	err := FromSentinel(sentinel, lookupSpec, "test", nil)

	if err.Code() != CodeCLI {
		t.Errorf("Code() = %q, want %q", err.Code(), CodeCLI)
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("errors.Is(err, sentinel) = %v, want %v", errors.Is(err, sentinel), true)
	}
}
