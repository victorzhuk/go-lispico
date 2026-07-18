package vm_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/core/compiler"
	"github.com/victorzhuk/go-lispico/core/vm"
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

// Family 5: keyword application, i.e. (:key m), must produce identical
// results on the VM and the tree-walker — key hit, key miss, and a non-map
// argument all resolve without error; only arity != 1 is an error.
func TestVMVsTreeWalker_KeywordApplication(t *testing.T) {
	t.Parallel()

	env := newCrossValEnv()
	tests := []struct {
		name string
		src  string
	}{
		{"key present", `(:name {:name "Alice" :age 30})`},
		{"key absent", `(:missing {:name "Alice"})`},
		{"non-map argument", `(:name 42)`},
		{"non-map argument nil", `(:name nil)`},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compare(t, env, tt.src)
		})
	}
}

// TestVMVsTreeWalker_KeywordApplication_ArityError proves both the
// tree-walker and the compiled VM reject a keyword call with an arity other
// than 1 as the SAME *core.LispicoError (Code "EvalError"), never a panic or
// a mismatched error shape.
func TestVMVsTreeWalker_KeywordApplication_ArityError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		src  string
	}{
		{"zero args", "(:name)"},
		{"two args", `(:name {:name "Alice"} "default")`},
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
			require.Error(t, treeErr, "tree-walker must reject wrong keyword arity")

			vmEnv := newCrossValEnv()
			chunks, err := compiler.CompileAll(forms)
			require.NoError(t, err, "compile")
			v := vm.New(vmEnv)
			_, vmErr := v.Run(context.Background(), chunks[0])
			require.Error(t, vmErr, "VM must reject wrong keyword arity")

			var treeLE, vmLE *core.LispicoError
			require.ErrorAs(t, treeErr, &treeLE, "tree-walker error must be *core.LispicoError")
			require.ErrorAs(t, vmErr, &vmLE, "VM error must be *core.LispicoError")
			assert.Equal(t, treeLE.Code, vmLE.Code, "error Code must match between tree-walker and VM")
			assert.Equal(t, treeLE.Message, vmLE.Message, "error Message must match between tree-walker and VM")
		})
	}
}

// Family 6: kernel `let` binds in parallel — every init resolves in the scope
// enclosing the `let`, never in a sibling binding — while `let*` stays
// sequential. Both modes must match the pinned expected value, not merely each
// other; a shared wrong answer would still be a defect.
func TestVMVsTreeWalker_LetParallelBindingScope(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		src      string
		expected core.Value
	}{
		{"let sibling resolves enclosing", "(def a 10) (let [a 1 b a] b)", core.Int{V: 10}},
		{"let* sibling resolves earlier", "(def a 10) (let* [a 1 b a] b)", core.Int{V: 1}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			forms, err := core.Read(tc.src)
			require.NoError(t, err, "read source")

			treeEnv := newCrossValEnv()
			treeEval := core.NewEvaluator()
			var treeResult core.Value = core.Nil{}
			for _, form := range forms {
				treeResult, err = treeEval.Eval(context.Background(), form, treeEnv)
				require.NoError(t, err, "tree-walker eval")
			}

			vmEnv := newCrossValEnv()
			chunks, err := compiler.CompileAll(forms)
			require.NoError(t, err, "compile")
			v := vm.New(vmEnv)
			var vmResult core.Value = core.Nil{}
			for _, chunk := range chunks {
				vmResult, err = v.Run(context.Background(), chunk)
				require.NoError(t, err, "vm run")
			}

			assert.True(t, treeResult.Equals(tc.expected),
				"tree-walker result %v != expected %v", treeResult, tc.expected)
			assert.True(t, vmResult.Equals(tc.expected),
				"VM result %v != expected %v", vmResult, tc.expected)
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
		{"let non-vector bindings", "(let 5 1)"},
		{"let odd binding count", "(let [x] 1)"},
		{"let* non-vector bindings", "(let* 5 1)"},
		{"let* odd binding count", "(let* [x] 1)"},
		{"recur outside loop", "(recur 1)"},
		{"not no args", "(not)"},
		{"not two args", "(not 1 2)"},
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
