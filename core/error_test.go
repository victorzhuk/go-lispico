package core

import (
	"errors"
	"testing"
)

func TestLispicoError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  *LispicoError
		want string
	}{
		{
			name: "with source",
			err:  &LispicoError{Code: "ReadError", Message: "unexpected EOF", Source: "test.lisp", Line: 3, Col: 5},
			want: "ReadError at test.lisp:3:5: unexpected EOF",
		},
		{
			name: "without source",
			err:  &LispicoError{Code: "EvalError", Message: "undefined symbol"},
			want: "EvalError: undefined symbol",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLispicoError_Unwrap(t *testing.T) {
	cause := errors.New("underlying")
	e := &LispicoError{Code: "EvalError", Message: "wrapped", Cause: cause}
	if !errors.Is(e, cause) {
		t.Error("Unwrap should expose Cause via errors.Is")
	}
}

func TestNewReadError(t *testing.T) {
	e := NewReadError("unexpected EOF", 5, 10)
	if e.Code != "ReadError" {
		t.Errorf("Code = %q, want ReadError", e.Code)
	}
	if e.Line != 5 || e.Col != 10 {
		t.Errorf("Line/Col = %d/%d, want 5/10", e.Line, e.Col)
	}
}

func TestNewEvalError(t *testing.T) {
	e := NewEvalError("cannot call", Int{V: 42})
	if e.Code != "EvalError" {
		t.Errorf("Code = %q, want EvalError", e.Code)
	}
}

func TestNewTypeError(t *testing.T) {
	e := NewTypeError("symbol", Int{V: 1})
	if e.Code != "TypeError" {
		t.Errorf("Code = %q, want TypeError", e.Code)
	}
}

func TestNewArityError(t *testing.T) {
	e := NewArityError(2, 3)
	if e.Code != "ArityError" {
		t.Errorf("Code = %q, want ArityError", e.Code)
	}
}

func TestNewUndefinedError(t *testing.T) {
	e := NewUndefinedError("foo")
	if e.Code != "UndefinedError" {
		t.Errorf("Code = %q, want UndefinedError", e.Code)
	}
	if e.Message != "undefined: foo" {
		t.Errorf("Message = %q, want 'undefined: foo'", e.Message)
	}
}
