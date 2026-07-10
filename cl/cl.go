// Package cl provides the Common Lisp dialect of the Lispico interpreter.
//
// The Common Lisp dialect is composed from the full kernel with:
//   - Lisp-2 namespace axis (separate function and value cells)
//   - nil-only truthiness (false is a true value)
//   - CL reader flags (#' and #(...) enabled, [..]/{..} disabled)
//   - Delta renames for special forms (defun→defn, setq→set!, progn→do)
//   - CL vocabulary renaming core GoFuncs (car→first, cdr→rest, etc.)
//
// defun is registered as an alias for the kernel defn form via [Dialect.Add].
// defn/fn/defmacro accept both Vector and List params via paramsAsVector for
// dialect portability. The CL reader disables bracket literals, so a List
// is the only on-disk representation — forms typed in Lisp naturally use
// list-style parameters.
//
// Because it carries non-default axes and a vocabulary map, its
// [core.Dialect.IsIdentity] returns false.  The bytecode VM handles rename
// normalization ([core.Dialect.CanonicalName]) and all dialect axes, so CL
// evaluates on the bytecode VM when WithBytecode() is enabled (ADR 0006).
package cl

import "github.com/victorzhuk/go-lispico/core"

// Dialect returns the Common Lisp dialect — a non-identity composition over
// the full kernel.
func Dialect() core.Dialect {
	return core.FullDialect().
		Lisp2().
		NilOnlyFalsy().
		WithoutBracketLiterals().
		WithFunctionRef().
		WithReaderVector().
		Add("defun", "defn").
		Rename("set!", "setq").
		Rename("do", "progn").
		Vocabulary(map[string]string{
			"car":     "first",
			"cdr":     "rest",
			"null":    "nil?",
			"cons":    "cons",
			"list":    "list",
			"append":  "concat",
			"length":  "count",
			"reverse": "reverse",
			"nth":     "nth",
			"sort":    "sort",
			"mapcar":  "map",
			"apply":   "apply",
			"type":    "type",
		})
}
