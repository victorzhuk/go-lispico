package core

import "fmt"

// LispicoError is the error type returned by reader, eval, and type-checking
// failures. Code identifies the error class; Source/Line/Col are set when the
// error can be tied to a location in the input.
type LispicoError struct {
	Code    string
	Message string
	Source  string
	Line    int
	Col     int
	Cause   error
}

// Error implements the error interface.
func (e *LispicoError) Error() string {
	if e.Source != "" {
		return fmt.Sprintf("%s at %s:%d:%d: %s", e.Code, e.Source, e.Line, e.Col, e.Message)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap returns the wrapped cause, if any, for errors.Is/errors.As support.
func (e *LispicoError) Unwrap() error { return e.Cause }

// NewReadError builds a LispicoError for a tokenizer/parser failure at the
// given line and column.
func NewReadError(msg string, line, col int) *LispicoError {
	return &LispicoError{Code: "ReadError", Message: msg, Line: line, Col: col}
}

// NewEvalError builds a LispicoError for a failure evaluating form.
func NewEvalError(msg string, form Value) *LispicoError {
	return &LispicoError{Code: "EvalError", Message: fmt.Sprintf("%s: %v", msg, form)}
}

// NewTypeError builds a LispicoError reporting that a value of the expected
// type was required but got did not match.
func NewTypeError(expected string, got Value) *LispicoError {
	return &LispicoError{Code: "TypeError", Message: fmt.Sprintf("expected %s, got %T", expected, got)}
}

// NewArityError builds a LispicoError reporting a call with the wrong number
// of arguments.
func NewArityError(expected, got int) *LispicoError {
	return &LispicoError{Code: "ArityError", Message: fmt.Sprintf("expected %d args, got %d", expected, got)}
}

// NewUndefinedError builds a LispicoError reporting a reference to an
// undefined symbol.
func NewUndefinedError(name string) *LispicoError {
	return &LispicoError{Code: "UndefinedError", Message: fmt.Sprintf("undefined: %s", name)}
}
