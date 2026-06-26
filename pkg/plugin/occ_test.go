// pkg/plugin/occ_test.go
package plugin

import (
	"testing"
	"time"
)

func TestParseOcc(t *testing.T) {
	got, err := ParseOcc("AAPL 17JAN25 150 C")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got.Underlying != "AAPL" {
		t.Errorf("underlying = %q, want AAPL", got.Underlying)
	}
	want := time.Date(2025, 1, 17, 0, 0, 0, 0, time.UTC)
	if !got.Expiry.Equal(want) {
		t.Errorf("expiry = %v, want %v", got.Expiry, want)
	}
	if got.Strike != 150 {
		t.Errorf("strike = %v, want 150", got.Strike)
	}
	if got.Right != "C" {
		t.Errorf("right = %q, want C", got.Right)
	}
}

func TestParseOccFractionalStrikeAndPut(t *testing.T) {
	got, err := ParseOcc("spy 03MAR25 512.5 p")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got.Underlying != "SPY" || got.Strike != 512.5 || got.Right != "P" {
		t.Errorf("got %+v", got)
	}
}

func TestParseOccRejectsNonOption(t *testing.T) {
	if _, err := ParseOcc("AAPL"); err == nil {
		t.Fatal("expected error for non-OCC id")
	}
}
