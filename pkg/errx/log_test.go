package errx

import "testing"

func TestLogrKV(t *testing.T) {
	kv, ok := LogrKV(nil)
	if ok || kv != nil {
		t.Fatal("expected no fields for nil error")
	}

	err := New("CODE", "Category", "message").WithContext("key", "value")
	kv, ok = LogrKV(err)
	if !ok {
		t.Fatal("expected ok for errx error")
	}
	if len(kv) < 6 {
		t.Fatalf("expected structured fields, got %v", kv)
	}
}
