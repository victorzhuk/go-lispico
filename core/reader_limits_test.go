package core

import (
	"errors"
	"strings"
	"testing"
)

func TestReader_DeepParensReturnsResourceLimit(t *testing.T) {
	t.Parallel()
	src := strings.Repeat("(", 100000) + "1" + strings.Repeat(")", 100000)
	_, err := Read(src)
	if err == nil {
		t.Fatal("expected error from deeply nested parens")
	}
	var le *LispicoError
	if !errors.As(err, &le) {
		t.Fatalf("expected *LispicoError, got %T: %v", err, err)
	}
	if le.Code != CodeResourceLimit {
		t.Fatalf("expected Code=%q, got %q", CodeResourceLimit, le.Code)
	}
	if !strings.Contains(le.Message, "depth") {
		t.Fatalf("error message %q does not mention depth", le.Message)
	}
}

func TestReader_BareDeepParensProcessSurvives(t *testing.T) {
	t.Parallel()
	src := strings.Repeat("(", 100000)
	_, err := Read(src)
	if err == nil {
		t.Fatal("expected error")
	}
	var le *LispicoError
	if !errors.As(err, &le) {
		t.Fatalf("expected *LispicoError, got %T: %v", err, err)
	}
	if le.Code != CodeResourceLimit {
		t.Fatalf("expected Code=%q, got %q", CodeResourceLimit, le.Code)
	}
}

func TestReader_NestedVectorUnderDefaultLimitOK(t *testing.T) {
	t.Parallel()
	n := 1000
	src := strings.Repeat("[", n) + "1" + strings.Repeat("]", n)
	forms, err := Read(src)
	if err != nil {
		t.Fatalf("nested vector depth %d should parse OK: %v", n, err)
	}
	if len(forms) != 1 {
		t.Fatalf("expected 1 form, got %d", len(forms))
	}
	v, ok := forms[0].(Vector)
	if !ok {
		t.Fatalf("expected Vector, got %T", forms[0])
	}
	if len(v.Items) != 1 {
		t.Fatalf("expected 1-item vector, got %d items", len(v.Items))
	}
}

func TestReader_NestedVectorJustOverDefaultLimitFails(t *testing.T) {
	t.Parallel()
	n := 1100
	src := strings.Repeat("[", n) + "1" + strings.Repeat("]", n)
	_, err := Read(src)
	if err == nil {
		t.Fatal("expected error from deeply nested vector")
	}
	var le *LispicoError
	if !errors.As(err, &le) {
		t.Fatalf("expected *LispicoError, got %T: %v", err, err)
	}
	if le.Code != CodeResourceLimit {
		t.Fatalf("expected Code=%q, got %q", CodeResourceLimit, le.Code)
	}
}

func TestReader_ReadWithMaxDepthLowCeiling(t *testing.T) {
	t.Parallel()
	n := 20
	src := strings.Repeat("(", n) + "1" + strings.Repeat(")", n)
	_, err := FullDialect().ReadWithMaxDepth(src, 10)
	if err == nil {
		t.Fatal("expected error with maxDepth=10 on depth-20 input")
	}
	var le *LispicoError
	if !errors.As(err, &le) {
		t.Fatalf("expected *LispicoError, got %T: %v", err, err)
	}
	if le.Code != CodeResourceLimit {
		t.Fatalf("expected Code=%q, got %q", CodeResourceLimit, le.Code)
	}
}

func TestReader_ReadWithMaxDepthAboveDefaultAccepts(t *testing.T) {
	t.Parallel()
	n := 2000
	src := strings.Repeat("[", n) + "1" + strings.Repeat("]", n)
	forms, err := FullDialect().ReadWithMaxDepth(src, 5000)
	if err != nil {
		t.Fatalf("depth 2000 with maxDepth=5000 should parse OK: %v", err)
	}
	if len(forms) != 1 {
		t.Fatalf("expected 1 form, got %d", len(forms))
	}
}
