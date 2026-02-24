package core

import "fmt"

type LispicoError struct {
	Code    string
	Message string
	Source  string
	Line    int
	Col     int
	Cause   error
}

func (e *LispicoError) Error() string {
	if e.Source != "" {
		return fmt.Sprintf("%s at %s:%d:%d: %s", e.Code, e.Source, e.Line, e.Col, e.Message)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *LispicoError) Unwrap() error { return e.Cause }

func NewReadError(msg string, line, col int) *LispicoError {
	return &LispicoError{Code: "ReadError", Message: msg, Line: line, Col: col}
}

func NewEvalError(msg string, form Value) *LispicoError {
	return &LispicoError{Code: "EvalError", Message: fmt.Sprintf("%s: %v", msg, form)}
}

func NewTypeError(expected string, got Value) *LispicoError {
	return &LispicoError{Code: "TypeError", Message: fmt.Sprintf("expected %s, got %T", expected, got)}
}

func NewArityError(expected, got int) *LispicoError {
	return &LispicoError{Code: "ArityError", Message: fmt.Sprintf("expected %d args, got %d", expected, got)}
}

func NewUndefinedError(name string) *LispicoError {
	return &LispicoError{Code: "UndefinedError", Message: fmt.Sprintf("undefined: %s", name)}
}
