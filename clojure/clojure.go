// Package clojure provides the Clojure dialect of the Lispico interpreter.
//
// The Clojure dialect is the full kernel with flat-pair cond (Clojure-style):
// Lisp-1, nil+false truthiness, bracket literals enabled, #' and #(...)
// disabled, and the cond clause-shape axis set to flat test/expression pairs.
// IsIdentity() returns true because form-shape rules are excluded from the
// identity check (they are a separate axis concept, and ADR 0006 removed the
// IsIdentity VM-gate).
package clojure

import "github.com/victorzhuk/go-lispico/core"

// Dialect returns the Clojure dialect — the full kernel with flat cond
// (Clojure-style (cond t1 e1 t2 e2 ...)).  IsIdentity() returns true because
// form-shape rules are excluded from the identity check.
func Dialect() core.Dialect { return core.FullDialect().FlatCond() }
