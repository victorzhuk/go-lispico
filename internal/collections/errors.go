package collections

import "github.com/victorzhuk/go-lispico/core"

// typeErrorf builds a TypeError.
func typeErrorf(format string, args ...any) *core.LispicoError {
	panic("not implemented")
}

// domainErrorf builds an EvalError for a value that is well-typed but outside
// the operation's domain.
func domainErrorf(format string, args ...any) *core.LispicoError {
	panic("not implemented")
}
