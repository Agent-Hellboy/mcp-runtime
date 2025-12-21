package errx

import (
	"testing"
)

func TestFormat_UserString(t *testing.T) {
	err := New("70000", "CLI error", "test")
	if UserString(err) != "test" {
		t.Errorf("UserString(err) = %q, want %q", UserString(err), "test")
	}
}

func TestFormat_DebugString(t *testing.T) {
	err := New("70000", "CLI error", "test")
	if DebugString(err) != "1: *errx.Error: test | code=70000 | description=\"CLI error\" | message=\"test\"" {
		t.Errorf("DebugString(err) = %q, want %q", DebugString(err), "1: *errx.Error: test | code=70000 | description=\"CLI error\" | message=\"test\"")
	}
}
