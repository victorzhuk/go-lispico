package collections

import (
	"fmt"

	"github.com/victorzhuk/go-lispico/core"
)

// typeErrorf builds a TypeError.
func typeErrorf(format string, args ...any) *core.LispicoError {
	return &core.LispicoError{Code: "TypeError", Message: fmt.Sprintf(format, args...)}
}

// domainErrorf builds an EvalError for a value that is well-typed but outside
// the operation's domain.
func domainErrorf(format string, args ...any) *core.LispicoError {
	return &core.LispicoError{Code: "EvalError", Message: fmt.Sprintf(format, args...)}
}
