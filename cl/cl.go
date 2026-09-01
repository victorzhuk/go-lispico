// Package cl provides the Common Lisp dialect of the Lispico interpreter.
//
// The Common Lisp dialect is composed from the full kernel with:
//   - Lisp-2 namespace axis (separate function and value cells)
//   - CL reader flags (#' and #(...) enabled, [..]/{..} disabled)
//   - Delta renames for special forms (defun→defn, setq→set!, progn→do)
//   - CL vocabulary renaming core GoFuncs (car→first, cdr→rest, etc.)
//   - Adapters binding nth, mapcar, and sort to their CL argument shapes
//     over the shared collection kernels
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

import (
	"context"
	"fmt"
	"sync"

	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
)

const (
	clNthID    = "cl/nth@1"
	clMapcarID = "cl/mapcar@1"
	clSortID   = "cl/sort@1"
)

var clNth = sync.OnceValue(func() core.Value {
	return core.GoFunc{
		Name: "nth",
		Fn: func(ctx context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("nth: requires 2 arguments")
			}
			idx, ok := args[0].(core.Int)
			if !ok {
				return nil, core.NewTypeError("integer", args[0])
			}
			if _, isNil := args[1].(core.Nil); isNil {
				return core.Nil{}, nil
			}
			val, outcome, err := stdlib.IndexedAccess(ctx, args[1], idx.V)
			if err != nil {
				return nil, err
			}
			switch outcome {
			case stdlib.AccessHit:
				return val, nil
			case stdlib.AccessOutOfRange:
				return core.Nil{}, nil
			default:
				return nil, fmt.Errorf("nth: expected collection, got %T", args[1])
			}
		},
	}
})

var clMapcar = sync.OnceValue(func() core.Value {
	return core.GoFunc{
		Name: "mapcar",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) < 2 {
				return nil, fmt.Errorf("mapcar: requires a function and at least one sequence")
			}
			return stdlib.MapSequences(ctx, eval, env, args[0], args[1:])
		},
	}
})

var clSort = sync.OnceValue(func() core.Value {
	return core.GoFunc{
		Name: "sort",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) < 2 {
				return nil, fmt.Errorf("sort: requires a sequence and a predicate")
			}
			var keyFn core.Value
			for rest := args[2:]; len(rest) > 0; rest = rest[2:] {
				kw, ok := rest[0].(core.Keyword)
				if !ok || kw.V != "key" || len(rest) < 2 {
					return nil, fmt.Errorf("sort: unsupported argument %v", rest[0])
				}
				keyFn = rest[1]
			}

			seq := args[0]
			var items []core.Value
			switch c := seq.(type) {
			case core.List:
				items = c.ToSlice()
			case core.Vector:
				items = c.ToSlice()
			case core.Nil:
				items = nil
			default:
				return nil, fmt.Errorf("sort: expected collection, got %T", seq)
			}

			var key stdlib.SortKeyFunc
			if keyFn != nil {
				key = func(v core.Value) (core.Value, error) {
					return eval.Apply(ctx, keyFn, []core.Value{v}, env)
				}
			}
			sorted, err := stdlib.StableSort(ctx, items, key, func(a, b core.Value) (bool, error) {
				r, err := eval.Apply(ctx, args[1], []core.Value{a, b}, env)
				if err != nil {
					return false, err
				}
				return core.IsTruthy(r), nil
			})
			if err != nil {
				return nil, err
			}
			switch seq.(type) {
			case core.Vector:
				return core.NewVector(sorted), nil
			default:
				return core.NewList(sorted), nil
			}
		},
	}
})

var stockDialect = sync.OnceValue(func() core.Dialect {
	return core.FullDialect().
		Lisp2().
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
			"apply":   "apply",
			"type":    "type",
		}).
		WithAdapter("nth", clNthID, clNth()).
		WithAdapter("mapcar", clMapcarID, clMapcar()).
		WithAdapter("sort", clSortID, clSort()).
		Memoized()
})

// Dialect returns the Common Lisp dialect — a non-identity composition over
// the full kernel. The returned value is a process-wide singleton: resolution
// and fingerprinting run once, on first call, and every caller shares the
// same resolved dispatch table and hash.
func Dialect() core.Dialect { return stockDialect() }
