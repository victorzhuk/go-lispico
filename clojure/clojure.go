// Package clojure provides the Clojure dialect of the Lispico interpreter.
//
// The Clojure dialect is the bare full kernel with no vocabulary map and no axis
// changes: Lisp-1, nil+false truthiness, bracket literals enabled, #' and #(...)
// disabled.  Because it carries no delta over [core.FullDialect], its
// [core.Dialect.IsIdentity] returns true, making it compatible with the bytecode
// VM.
package clojure

import "github.com/victorzhuk/go-lispico/core"

// Dialect returns the Clojure dialect — the identity dialect built on the full
// kernel.  The bytecode VM requires IsIdentity()==true, so this dialect MUST
// remain a bare FullDialect with no vocabulary, no adapters, and no axis
// changes.
func Dialect() core.Dialect { return core.FullDialect() }
