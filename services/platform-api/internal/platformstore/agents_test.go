package platformstore

import (
	"bytes"
	"regexp"
	"testing"
	"time"
)

func TestNewAgentIDUsesLowercaseULID(t *testing.T) {
	entropy := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	id, err := NewAgentID(time.UnixMilli(1_700_000_000_000).UTC(), entropy)
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := regexp.MatchString(`^agt_[0-9a-hjkmnp-tv-z]{26}$`, id); !ok {
		t.Fatalf("ID %q does not have the canonical format", id)
	}
	if id[:4] != "agt_" {
		t.Fatalf("ID prefix = %q", id[:4])
	}
	if bytes.Equal([]byte(id), []byte("agt_")) {
		t.Fatal("ID suffix missing")
	}
}

func TestNormalizeAgentName(t *testing.T) {
	name, normalized, err := NormalizeAgentName("  Release   Planner ")
	if err != nil {
		t.Fatal(err)
	}
	if name != "Release Planner" || normalized != "release planner" {
		t.Fatalf("got name=%q normalized=%q", name, normalized)
	}
	_, folded, err := NormalizeAgentName("Straße")
	if err != nil || folded != "strasse" {
		t.Fatalf("case-fold = %q, err=%v", folded, err)
	}
	for _, invalid := range []string{"", "   ", string(bytes.Repeat([]byte{'a'}, 65))} {
		if _, _, err := NormalizeAgentName(invalid); err != ErrInvalidAgentName {
			t.Errorf("NormalizeAgentName(%q) error = %v", invalid, err)
		}
	}
}

func TestAgentCursorRoundTripAndValidation(t *testing.T) {
	wantTime := time.Date(2026, 9, 26, 12, 34, 56, 789123000, time.UTC)
	wantID := "agt_01arz3ndektsv4rrffq69g5fav"
	cursor := encodeAgentCursor(wantTime, wantID)
	gotTime, gotID, err := decodeAgentCursor(cursor)
	if err != nil {
		t.Fatal(err)
	}
	if !gotTime.Equal(wantTime) || gotID != wantID {
		t.Fatalf("decoded (%s, %q), want (%s, %q)", gotTime, gotID, wantTime, wantID)
	}
	if _, _, err := decodeAgentCursor("bad"); err == nil {
		t.Fatal("malformed cursor unexpectedly accepted")
	}
}
