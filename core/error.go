package core

import (
	"context"
	"errors"
	"fmt"
)

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

// IsTerminalEvalError returns true if err represents an uncatchable terminal
// error: context.Canceled, context.DeadlineExceeded (matched via errors.Is,
// including wrapped forms), or a *LispicoError with Code == CodeResourceLimit
// (matched via errors.As).
func IsTerminalEvalError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	var target **LispicoError
	lerr, ok := asLispicoError(err, &target)
	return ok && lerr.Code == CodeResourceLimit
}

func asLispicoError(err error, target ***LispicoError) (*LispicoError, bool) {
	for err != nil {
		if lerr, ok := err.(*LispicoError); ok {
			if *target != nil {
				**target = lerr
			}
			return lerr, true
		}
		if _, ok := err.(interface{ As(any) bool }); ok {
			if *target == nil {
				*target = new(*LispicoError)
			}
			ok := errors.As(err, *target)
			return **target, ok
		}
		switch x := err.(type) {
		case interface{ Unwrap() error }:
			err = x.Unwrap()
		case interface{ Unwrap() []error }:
			for _, child := range x.Unwrap() {
				if lerr, ok := asLispicoError(child, target); ok {
					return lerr, true
				}
			}
			return nil, false
		default:
			return nil, false
		}
	}
	return nil, false
}

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

// CodeResourceLimit classifies a *LispicoError reporting that a resource
// ceiling (reader depth, structural depth, collection length, cache size)
// was exceeded.
const CodeResourceLimit = "ResourceLimitError"

// NewResourceLimitError builds a LispicoError reporting a resource ceiling
// was exceeded.
func NewResourceLimitError(msg string) *LispicoError {
	return &LispicoError{Code: CodeResourceLimit, Message: msg}
}

// CodeConcurrentUse classifies a *LispicoError reporting that a stateful
// runtime handle (currently a PinnedFn) was used in a way that violates its
// single-owner contract: concurrent entry from another goroutine, or a
// re-entrant call from within the handle's own execution. The handle stays
// usable; the offending call is rejected without mutating the handle.
const CodeConcurrentUse = "ConcurrentUseError"

// NewConcurrentUseError builds a LispicoError reporting that handle name was
// used in violation of its single-owner contract.
func NewConcurrentUseError(name string) *LispicoError {
	return &LispicoError{Code: CodeConcurrentUse, Message: fmt.Sprintf("concurrent use of handle %q: each handle is owned by exactly one goroutine and must not be re-entered from its own execution", name)}
}

// CodePanic classifies a *LispicoError reporting that a user-supplied GoFunc
// panicked inside a runtime entry point. The panic is recovered, boundary state
// is reset, and the panic value is wrapped so the caller observes a typed error
// rather than a propagated panic.
const CodePanic = "PanicError"

// NewPanicError builds a LispicoError reporting that panicValue was recovered
// at runtime boundary name.
func NewPanicError(name string, panicValue any) *LispicoError {
	if name != "" {
		return &LispicoError{Code: CodePanic, Message: fmt.Sprintf("recovered panic in %q: %v", name, panicValue)}
	}
	return &LispicoError{Code: CodePanic, Message: fmt.Sprintf("recovered panic: %v", panicValue)}
}

// CodeVMState classifies a *LispicoError reporting that a handle's private VM
// was left in a dirty state after an apply (the steady-state ResetIncremental
// invariant was violated). The handle runs a full Reset before returning so
// the next call sees a clean VM.
const CodeVMState = "VMStateError"

// NewVMStateError builds a LispicoError reporting that handle name's private
// VM was left in a dirty state after an apply.
func NewVMStateError(name string, cause error) *LispicoError {
	return &LispicoError{Code: CodeVMState, Message: fmt.Sprintf("pinned call %q left the VM in a dirty state; full reset applied: %v", name, cause), Cause: cause}
}
