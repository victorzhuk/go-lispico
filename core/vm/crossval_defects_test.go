package vm_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/core/compiler"
)

// Defect-family cross-validation corpus. Families 1-3 (when/unless value
// positions, set! lexical-owner, try/catch locals) extend the slice-A tables
// (TestVMVsTreeWalker_WhenUnlessValuePosition / _SetLexical / _SetUndefined /
// _TryCatchLocals) with distinct syntactic positions; family 4 covers
// malformed-form error parity between the bytecode compiler and the
// tree-walker.

func TestVMVsTreeWalker_WhenUnless_NestedPositions(t *testing.T) {
	t.Parallel()

	env := newCrossValEnv()
	tests := []struct {
		name string
		src  string
	}{
		{"when false as if cond", "(if (when false 1) 2 3)"},
		{"unless true nested in when", "(when true (unless true 1))"},
		{"unless false value drives if", "(if (unless false 7) 1 0)"},
		{"when value bound and summed", "(let [x (when true 9)] (+ x 1))"},
		{"when false discarded in do", "(do 5 (when false 1))"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compare(t, env, tt.src)
		})
	}
}

func TestVMVsTreeWalker_SetLexical_NestedClosureAndLocal(t *testing.T) {
	t.Parallel()

	env := newCrossValEnv()
	tests := []struct {
		name string
		src  string
	}{
		// set! to an enclosing fn param from a nested closure: lexical owner is
		// the param's frame (distinct from slice-A's global set!).
		{"set! fn param from nested closure", `(def counter (fn [n] (fn [] (set! n (+ n 1)) n)))
                        ((counter 5))`},
		// set! to a let-bound local resolved as a local slot (OpSetLocal path).
		{"set! let local then read", "(let [a 1] (set! a (+ a 10)) a)"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compare(t, env, tt.src)
		})
	}
}

func TestVMVsTreeWalker_TryCatchLocals_InLoopAndFn(t *testing.T) {
	t.Parallel()

	env := newCrossValEnv()
	tests := []struct {
		name string
		src  string
	}{
		// try/catch inside a loop body, recur after catch on the throwing path.
		{"try in loop recur after catch", `(loop [i 0] (if (= i 2) i (do (try (throw "x") (catch e e)) (recur (+ i 1)))))`},
		// fn body try, no-throw path returns the body value.
		{"fn try no throw", `((fn [] (try (+ 1 2) (catch e 0))))`},
		// fn body try, throwing path returns the handler value.
		{"fn try caught", `((fn [] (try (throw "b") (catch e 99))))`},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compare(t, env, tt.src)
		})
	}
}

// Family 4: every malformed special form must be rejected by BOTH the
// tree-walker (at eval) and the bytecode compiler (at compile) — error parity,
// never a panic on either side.
func TestVMVsTreeWalker_MalformedFormParity(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		src  string
	}{
		{"if no args", "(if)"},
		{"if cond only", "(if x)"},
		{"let no args", "(let)"},
		{"when no args", "(when)"},
		{"unless no args", "(unless)"},
		{"fn no args", "(fn)"},
		{"fn non-vector params", "(fn 42)"},
		{"set! one arg", "(set! x)"},
		{"set! non-symbol name", "(set! 42 1)"},
		{"try no catch", "(try)"},
		{"try body no catch", "(try 5)"},
		{"defn no params", "(defn foo)"},
		{"def one arg", "(def 42)"},
		{"def non-symbol name", "(def 42 1)"},
		{"loop no body", "(loop [])"},
		{"loop no args", "(loop)"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			forms, err := core.Read(tc.src)
			require.NoError(t, err, "read source")

			treeEnv := newCrossValEnv()
			treeEval := core.NewEvaluator()
			_, treeErr := treeEval.Eval(context.Background(), forms[0], treeEnv)
			_, compileErr := compiler.CompileAll(forms)

			require.Error(t, treeErr, "tree-walker must reject malformed form, not panic")
			require.Error(t, compileErr, "bytecode compiler must reject malformed form, not panic")

			var le *core.LispicoError
			assert.ErrorAs(t, treeErr, &le, "tree-walker error must be *core.LispicoError")
			assert.ErrorAs(t, compileErr, &le, "compile error must be *core.LispicoError")
		})
	}
}
