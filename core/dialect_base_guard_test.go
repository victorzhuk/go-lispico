package core

import (
	"strings"
	"testing"
)

// TestNewDialect_RejectsUnknownBase pins the guard: a DialectSpec whose Base
// is neither BaseFull nor BaseEmpty is refused with an error naming the base,
// and the returned Dialect is the zero value. The same spec always reports
// the same error.
func TestNewDialect_RejectsUnknownBase(t *testing.T) {
	spec := DialectSpec{Base: DialectBase(2)}

	d, err := NewDialect(spec)
	if err == nil {
		t.Fatal("NewDialect(DialectSpec{Base: 2}): want error, got nil")
	}
	if d != (Dialect{}) {
		t.Fatalf("NewDialect on unknown base: want zero Dialect, got %+v", d)
	}
	if got := err.Error(); !strings.Contains(got, "unknown base") {
		t.Fatalf("error %q: want message containing %q", got, "unknown base")
	}
	if got := err.Error(); got != "dialect: unknown base 2" {
		t.Fatalf("error = %q, want %q", got, "dialect: unknown base 2")
	}

	// Deterministic: the same spec reports the same error.
	_, err2 := NewDialect(spec)
	if err2 == nil || err2.Error() != err.Error() {
		t.Fatalf("second call: error %v, want same as %v", err2, err)
	}
}
