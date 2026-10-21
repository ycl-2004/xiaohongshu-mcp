package main

import (
	"errors"
	"testing"
)

// A reuse check whose browser cannot even start (Must* panics on a starved
// machine) is reported as an error, i.e. "not checked", never a crash.
func TestReuseCheckTurnsPanicsIntoErrors(t *testing.T) {
	if err := reuseCheck(func() error { panic("launch timeout") }); err == nil {
		t.Fatal("a panic in the check should become an error")
	}
	want := errors.New("page load timeout")
	if err := reuseCheck(func() error { return want }); !errors.Is(err, want) {
		t.Fatalf("an ordinary error should pass through, got %v", err)
	}
	if err := reuseCheck(func() error { return nil }); err != nil {
		t.Fatalf("a finished check should not fail, got %v", err)
	}
}
