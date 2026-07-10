package core

import (
	"errors"
	"testing"
)

func TestParser_InvalidNumberReportsPosition(t *testing.T) {
	t.Parallel()
	_, err := ReadOne("\n\n\n1.2.3")
	if err == nil {
		t.Fatal("expected error for invalid number")
	}
	var lispicoErr *LispicoError
	if !errors.As(err, &lispicoErr) {
		t.Fatalf("expected *LispicoError, got %T", err)
	}
	if lispicoErr.Line != 4 {
		t.Errorf("expected line 4, got %d", lispicoErr.Line)
	}
	if lispicoErr.Col <= 0 {
		t.Errorf("expected col > 0, got %d", lispicoErr.Col)
	}
}

func TestParser_UnexpectedEOFReportsEndPosition(t *testing.T) {
	t.Parallel()
	_, err := ReadOne("\n\n(+ 1")
	if err == nil {
		t.Fatal("expected error for unexpected EOF")
	}
	var lispicoErr *LispicoError
	if !errors.As(err, &lispicoErr) {
		t.Fatalf("expected *LispicoError, got %T", err)
	}
	if lispicoErr.Line != 3 {
		t.Errorf("expected line 3, got %d", lispicoErr.Line)
	}
	if lispicoErr.Col <= 0 {
		t.Errorf("expected col > 0, got %d", lispicoErr.Col)
	}
}
