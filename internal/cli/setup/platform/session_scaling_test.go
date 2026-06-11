package platform

import "testing"

func TestParseReplicaCount(t *testing.T) {
	got, err := parseReplicaCount("3")
	if err != nil || got != 3 {
		t.Fatalf("parseReplicaCount() = (%d, %v), want (3, nil)", got, err)
	}
	if _, err := parseReplicaCount("-1"); err == nil {
		t.Fatal("expected error for negative replica count")
	}
}

func TestReplicaCount(t *testing.T) {
	value := int32(2)
	if got := replicaCount(&value); got != 2 {
		t.Fatalf("replicaCount() = %d, want 2", got)
	}
	if got := replicaCount(nil); got != 1 {
		t.Fatalf("replicaCount(nil) = %d, want 1", got)
	}
}
