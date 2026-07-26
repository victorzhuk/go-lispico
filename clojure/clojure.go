// Package clojure provides the Clojure dialect of the Lispico interpreter.
//
// The Clojure dialect is the full kernel with flat-pair cond (Clojure-style):
// Lisp-1, nil+false truthiness, bracket literals enabled, #' and #(...)
// disabled, and the cond clause-shape axis set to flat test/expression pairs.
// IsIdentity() returns true because form-shape rules are excluded from the
// identity check (they are a separate axis concept, and ADR 0006 removed the
// IsIdentity VM-gate).
package clojure

import (
	"sync"

	"github.com/victorzhuk/go-lispico/core"
)

var stockDialect = sync.OnceValue(func() core.Dialect {
	return core.FullDialect().FlatCond().Memoized()
})

// Dialect returns the Clojure dialect — the full kernel with flat cond
// (Clojure-style (cond t1 e1 t2 e2 ...)).  IsIdentity() returns true because
// form-shape rules are excluded from the identity check. The returned value
// is a process-wide singleton: resolution and fingerprinting run once, on
// first call, and every caller shares the same resolved dispatch table and
// hash.
func Dialect() core.Dialect { return stockDialect() }
