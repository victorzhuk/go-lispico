package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
)

// TestDialect_Bytecode_NormalizeRename verifies that a dialect rename normalizes
// to the canonical kernel form before compilation, so (progn 1 2) under a dialect
// that renames do→progn compiles and runs identically to (do 1 2) under the
// identity dialect (spec scenario).
func TestDialect_Bytecode_NormalizeRename(t *testing.T) {
	renamed := core.FullDialect().Rename("do", "progn")
	e, err := New(nil, WithBytecode(), WithDialect(renamed))
	require.NoError(t, err)
	defer e.Close()

	got, err := e.Eval(context.Background(), "progn", "(progn 1 2)")
	require.NoError(t, err, "(progn 1 2) must compile and eval under renamed dialect")
	assert.True(t, core.Int{V: 2}.Equals(got), "progn returns last form = %v", got)
}

// TestDialect_Bytecode_RemoveRejected verifies that a fail-closed dialect that
// removes a form produces an undefined-form compile error when running on the
// bytecode VM (spec scenario).
func TestDialect_Bytecode_RemoveRejected(t *testing.T) {
	restricted := core.FullDialect().Remove("set!")
	e, err := New(nil, WithBytecode(), WithDialect(restricted))
	require.NoError(t, err)
	defer e.Close()

	_, err = e.Eval(context.Background(), "set!", "(set! x 1)")
	require.Error(t, err, "removed set! must fail at compile time")
	assert.Contains(t, err.Error(), "undefined")
}

// TestDialect_Bytecode_CL_Default verifies that the default CL dialect works
// under WithBytecode(): CL truthiness (nil-only), defun/Lisp-2 function cell,
// and basic evaluation all succeed through the bytecode path.
func TestDialect_Bytecode_CL_Default(t *testing.T) {
	e, err := New(nil, WithBytecode())
	require.NoError(t, err, "default CL + bytecode must construct")
	defer e.Close()

	// CL truthiness: only nil is falsy, false is true.
	got, err := e.Eval(context.Background(), "cl-truthy", "(if false :y :n)")
	require.NoError(t, err)
	assert.True(t, core.Keyword{V: "y"}.Equals(got), "CL truthiness: (if false :y :n) => :y")

	// defun (Lisp-2) defines a function callable from head position.
	// Note: under bytecode, defun returns a vm.Closure, not core.Lambda.
	_, err = e.Eval(context.Background(), "cl-defun", "(defun id (x) x)")
	require.NoError(t, err, "defun must compile under CL bytecode")

	got, err = e.Eval(context.Background(), "cl-call", "(id 42)")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 42}.Equals(got), "(id 42) => 42")
}

// TestDialect_Bytecode_Clojure_DefaultTruthiness verifies that the Clojure
// (identity) dialect works under WithBytecode() with nil+false truthiness.
func TestDialect_Bytecode_Clojure_DefaultTruthiness(t *testing.T) {
	e, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	defer e.Close()

	// Clojure truthiness: nil and false are both falsy.
	got, err := e.Eval(context.Background(), "clj-truthy", "(if false :y :n)")
	require.NoError(t, err)
	assert.True(t, core.Keyword{V: "n"}.Equals(got), "Clojure truthiness: (if false :y :n) => :n")
}

// TestDialect_Bytecode_RemoveThenEvalOtherForms verifies that a restricted
// dialect under bytecode can still evaluate forms it didn't remove.
func TestDialect_Bytecode_RemoveThenEvalOtherForms(t *testing.T) {
	restricted := core.FullDialect().Remove("def")
	e, err := New(nil, WithBytecode(), WithDialect(restricted))
	require.NoError(t, err)
	defer e.Close()

	// def is removed — must fail.
	_, err = e.Eval(context.Background(), "def-removed", "(def x 1)")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "undefined")

	// if is still callable.
	got, err := e.Eval(context.Background(), "if-still-works", "(if true 1 2)")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 1}.Equals(got))
}

// TestDialect_Bytecode_Lisp2_Funcall verifies that the Lisp-2 function cell
// works through the bytecode path: (funcall (function id) 42) resolves.
func TestDialect_Bytecode_Lisp2_Funcall(t *testing.T) {
	lisp2 := core.FullDialect().Lisp2()
	e, err := New(nil, WithBytecode(), WithDialect(lisp2))
	require.NoError(t, err)
	defer e.Close()

	got, err := e.Eval(context.Background(), "lisp2", "(do (defn f [x] x) (funcall (function f) 42))")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 42}.Equals(got), "funcall #'f: got %v", got)
}

// TestDialect_Bytecode_NilOnlyTruthiness verifies that the nil-only truthiness
// axis works through the bytecode path.
func TestDialect_Bytecode_NilOnlyTruthiness(t *testing.T) {
	nilOnly := core.FullDialect().NilOnlyFalsy()
	e, err := New(nil, WithBytecode(), WithDialect(nilOnly))
	require.NoError(t, err)
	defer e.Close()

	// nil is falsy.
	got, err := e.Eval(context.Background(), "nil-falsy", "(if nil :y :n)")
	require.NoError(t, err)
	assert.True(t, core.Keyword{V: "n"}.Equals(got), "nil must be falsy: (if nil :y :n) => :n")

	// false is truthy under nil-only.
	got, err = e.Eval(context.Background(), "false-truthy", "(if false :y :n)")
	require.NoError(t, err)
	assert.True(t, core.Keyword{V: "y"}.Equals(got), "false must be truthy: (if false :y :n) => :y")

	// non-nil, non-false values are truthy.
	got, err = e.Eval(context.Background(), "int-truthy", "(if 0 :y :n)")
	require.NoError(t, err)
	assert.True(t, core.Keyword{V: "y"}.Equals(got), "0 must be truthy: (if 0 :y :n) => :y")
}

// TestDialect_Bytecode_EmptyBase verifies that an empty-base dialect with
// WithBytecode() works: only explicitly added forms compile.
func TestDialect_Bytecode_EmptyBase(t *testing.T) {
	empty := core.EmptyDialect().Add("if", "if")
	e, err := New(nil, WithBytecode(), WithDialect(empty))
	require.NoError(t, err)
	defer e.Close()

	got, err := e.Eval(context.Background(), "if-works", "(if true 7 8)")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 7}.Equals(got))

	_, err = e.Eval(context.Background(), "def-undefined", "(def x 1)")
	require.Error(t, err, "def must be undefined in empty-base dialect")
}
