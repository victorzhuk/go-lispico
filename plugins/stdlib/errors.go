package stdlib

import (
	"fmt"

	"github.com/victorzhuk/go-lispico/core"
)

// arityErrorf builds the sole ArityError shape for this package: exact,
// ranged, variadic and parity wordings all go through it, because the
// existing wordings are not uniform per shape and core.NewArityError cannot
// express a range.
func arityErrorf(format string, args ...any) *core.LispicoError {
	return &core.LispicoError{Code: "ArityError", Message: fmt.Sprintf(format, args...)}
}

// typeErrorf builds a TypeError.
func typeErrorf(format string, args ...any) *core.LispicoError {
	return &core.LispicoError{Code: "TypeError", Message: fmt.Sprintf(format, args...)}
}

// domainErrorf builds an EvalError for a value that is well-typed but outside
// the operation's domain.
func domainErrorf(format string, args ...any) *core.LispicoError {
	return &core.LispicoError{Code: "EvalError", Message: fmt.Sprintf(format, args...)}
}

// wrapCause builds an EvalError naming the operation and carrying cause on the
// Cause field, which is the single edge errors.Is/errors.As traverse. It never
// uses %w, and never wraps a cause that is already a *core.LispicoError.
//
// Only for external, non-Lispico causes on parse/conversion paths (strconv and
// similar). It must never be called on a cause that can be terminal: nesting a
// LispicoError inside a LispicoError would put a generic EvalError outermost,
// and core.IsTerminalEvalError binds the first *LispicoError in the chain, so a
// wrapped ResourceLimitError would read as non-terminal. The identity return
// below is a defensive backstop for that, not the main path; in that case the
// message does not name the operation, because the inner error keeps its own.
func wrapCause(name string, cause error) *core.LispicoError {
	if lerr, ok := cause.(*core.LispicoError); ok {
		return lerr
	}
	return &core.LispicoError{
		Code:    "EvalError",
		Message: fmt.Sprintf("%s: %v", name, cause),
		Cause:   cause,
	}
}
