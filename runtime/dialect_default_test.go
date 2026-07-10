package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
)

// TestRuntime_DefaultIsCL (1.1) — runtime.New() with no dialect option
// resolves to the Common Lisp dialect. Asserts the spec scenarios:
//   - defun defines a function with List-style params (CL vocab over defn kernel form).
//   - (if false :y :n) evaluates to :y (nil-only truthiness).
//   - funcall and function (#') are available.
//   - [1 2] is a parse error (no bracket literals under CL).
//   - #'f and #(1 2) parse.
func TestRuntime_DefaultIsCL(t *testing.T) {
	eng, err := New(nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	ctx := context.Background()

	// (if false :y :n) is :y under CL (nil-only).
	got, err := eng.Eval(ctx, "if-false", "(if false :y :n)")
	require.NoError(t, err)
	require.True(t, core.Keyword{V: "y"}.Equals(got),
		"CL truthiness: only nil is falsy; (if false :y :n) must be :y")

	// defun defines a function with CL-style List params.
	got, err = eng.Eval(ctx, "defun", "(defun f (x) x)")
	require.NoError(t, err, "defun must be callable under CL")
	_, isLambda := got.(core.Lambda)
	require.True(t, isLambda, "defun must return a Lambda")

	// [1 2] is a parse error under CL (no bracket literals).
	_, err = eng.Eval(ctx, "brackets", "[1 2]")
	require.Error(t, err, "bracket literals must be off under CL")

	// #'f parses.
	_, err = eng.Eval(ctx, "funcref", "#'f")
	require.NoError(t, err, "#'f must parse under CL")

	// #(1 2) parses as a vector literal.
	got, err = eng.Eval(ctx, "reader-vec", "#(1 2)")
	require.NoError(t, err, "#(1 2) must parse under CL")
	_, isVector := got.(core.Vector)
	require.True(t, isVector, "#(1 2) must read as a Vector")
}

// TestRuntime_ClojureDialectReproducesPriorSurface (1.2) —
// WithDialect(clojure.Dialect()) reproduces the pre-flip behavior:
// (if false :y :n) is :n, [1 2] is a vector, defun is uncallable,
// funcall is undefined.
func TestRuntime_ClojureDialectReproducesPriorSurface(t *testing.T) {
	eng, err := New(nil, WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	ctx := context.Background()

	// (if false :y :n) is :n under Clojure (nil+false falsy).
	got, err := eng.Eval(ctx, "if-false", "(if false :y :n)")
	require.NoError(t, err)
	require.True(t, core.Keyword{V: "n"}.Equals(got),
		"Clojure truthiness: nil and false both falsy; (if false :y :n) must be :n")

	// [1 2] is a vector literal under Clojure.
	got, err = eng.Eval(ctx, "brackets", "[1 2]")
	require.NoError(t, err)
	_, isVector := got.(core.Vector)
	require.True(t, isVector, "[1 2] must read as a Vector under Clojure")

	// defun does NOT resolve under Clojure (no defun in the Clojure vocab).
	_, err = eng.Eval(ctx, "defun", "(defun f (x) x)")
	require.Error(t, err, "defun must NOT resolve under Clojure")

	// funcall is NOT callable under Clojure (Lisp-1, no function cell).
	_, err = eng.Eval(ctx, "funcall", "(funcall)")
	require.Error(t, err, "funcall must NOT be callable under Clojure")
}

// TestRuntime_DefaultCL_AllowsBytecode (1.3) — runtime.New() with
// WithBytecode() (no WithDialect) succeeds because bytecode now supports
// all dialects after normalization.
func TestRuntime_DefaultCL_AllowsBytecode(t *testing.T) {
	e, err := New(nil, WithBytecode())
	require.NoError(t, err, "default (CL) + bytecode must be allowed")
	e.Close()
}

// TestRuntime_BytecodeWithClojureWorks (1.4) —
// New(nil, WithBytecode(), WithDialect(clojure.Dialect())) succeeds
// because Clojure is identity. Also pins the Clojure-identity invariant
// directly: clojure.Dialect().IsIdentity() is true.
func TestRuntime_BytecodeWithClojureWorks(t *testing.T) {
	require.True(t, clojure.Dialect().IsIdentity(),
		"Clojure dialect must remain identity to keep bytecode usable")

	e, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err, "bytecode + Clojure (identity) must be allowed")
	e.Close()
}
