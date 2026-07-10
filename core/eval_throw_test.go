package core

import (
	"errors"
	"testing"
)

func TestEvalThrow_TypedError(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	err := evalStrErr(env, `(throw "boom")`)
	if err == nil {
		t.Fatal("expected error from throw")
	}
	var lispicoErr *LispicoError
	if !errors.As(err, &lispicoErr) {
		t.Fatalf("expected *LispicoError, got %T", err)
	}
	if lispicoErr.Code != "ThrowError" {
		t.Errorf("expected ThrowError code, got %q", lispicoErr.Code)
	}
	if lispicoErr.Message != "boom" {
		t.Errorf("expected message 'boom', got %q", lispicoErr.Message)
	}
}
