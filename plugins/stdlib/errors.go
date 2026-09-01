package stdlib

import "github.com/victorzhuk/go-lispico/core"

// arityErrorf builds the sole ArityError shape for this package: exact,
// ranged, variadic and parity wordings all go through it, because the
// existing wordings are not uniform per shape and core.NewArityError cannot
// express a range.
func arityErrorf(format string, args ...any) *core.LispicoError {
	panic("not implemented")
}

// typeErrorf builds a TypeError.
func typeErrorf(format string, args ...any) *core.LispicoError {
	panic("not implemented")
}

// domainErrorf builds an EvalError for a value that is well-typed but outside
// the operation's domain.
func domainErrorf(format string, args ...any) *core.LispicoError {
	panic("not implemented")
}

// wrapCause builds an EvalError naming the operation and carrying cause on the
// Cause field, which is the single edge errors.Is/errors.As traverse. It never
// uses %w, and never wraps a cause that is already a *core.LispicoError.
func wrapCause(name string, cause error) *core.LispicoError {
	panic("not implemented")
}
