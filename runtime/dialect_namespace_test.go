package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
)

// Under Lisp-2 a symbol may name a value and a function at once: def binds the
// value cell, defn binds the function cell. Argument position reads the value
// cell, head position reads the function cell.
func TestDialect_Lisp2_HeadVsArgumentNamespace(t *testing.T) {
	e, err := New(nil, WithDialect(core.FullDialect().Lisp2()))
	require.NoError(t, err)
	defer e.Close()

	got, err := e.Eval(context.Background(), "lisp2", "(do (def f :val) (defn f [] :fn) [f (f)])")
	require.NoError(t, err)
	want := core.Vector{Items: []core.Value{core.Keyword{V: "val"}, core.Keyword{V: "fn"}}}
	assert.True(t, want.Equals(got), "want [:val :fn], got %v", got)
}

// (function f) is the #'f form: it yields the function-cell binding. funcall
// applies a function value taken from value position.
func TestDialect_Lisp2_FuncallAndFunctionRef(t *testing.T) {
	e, err := New(nil, WithDialect(core.FullDialect().Lisp2()))
	require.NoError(t, err)
	defer e.Close()

	got, err := e.Eval(context.Background(), "lisp2", "(do (defn f [x] x) (funcall (function f) 42))")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 42}.Equals(got), "funcall #'f: got %v", got)

	got, err = e.Eval(context.Background(), "lisp2", "(do (def g (fn [x] x)) (funcall g 7))")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 7}.Equals(got), "funcall value-cell fn: got %v", got)
}

// Pinned to Clojure dialect (Lisp-1); the default flips to Common Lisp (Lisp-2) in shard-C.
func TestDialect_Lisp1_FuncallAndFunctionUndefined(t *testing.T) {
	e, err := New(nil, WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	defer e.Close()

	_, err = e.Eval(context.Background(), "lisp1", "(funcall (fn [x] x) 1)")
	require.Error(t, err, "funcall must be undefined under Lisp-1")
	assert.Contains(t, err.Error(), "undefined")

	_, err = e.Eval(context.Background(), "lisp1", "(do (defn f [x] x) (function f))")
	require.Error(t, err, "function (#') must be undefined under Lisp-1")
	assert.Contains(t, err.Error(), "undefined")
}

// Under Lisp-2 a macro is an operator, so defmacro binds the function cell and
// head position dispatches it there.
func TestDialect_Lisp2_MacroDispatchesFromFunctionCell(t *testing.T) {
	e, err := New(nil, WithDialect(core.FullDialect().Lisp2()))
	require.NoError(t, err)
	defer e.Close()

	got, err := e.Eval(context.Background(), "lisp2", "(do (defmacro m [] :expanded) (m))")
	require.NoError(t, err)
	assert.True(t, core.Keyword{V: "expanded"}.Equals(got), "got %v", got)
}

// Head-position resolution walks the scope chain, so a function defined in an
// outer scope is callable from an inner one.
func TestDialect_Lisp2_FunctionCellWalksScopeChain(t *testing.T) {
	e, err := New(nil, WithDialect(core.FullDialect().Lisp2()))
	require.NoError(t, err)
	defer e.Close()

	got, err := e.Eval(context.Background(), "lisp2", "(do (defn f [] :outer) (let [x 1] (f)))")
	require.NoError(t, err)
	assert.True(t, core.Keyword{V: "outer"}.Equals(got), "got %v", got)
}

// A Lisp-2 Dialect is non-identity, so the bytecode VM rejects it at
// construction and evaluation runs on the tree-walker.
func TestDialect_Lisp2_RejectsBytecode(t *testing.T) {
	_, err := New(nil, WithBytecode(), WithDialect(core.FullDialect().Lisp2()))
	require.Error(t, err, "Lisp-2 is non-identity; bytecode VM must reject it")

	e, err := New(nil, WithDialect(core.FullDialect().Lisp2()))
	require.NoError(t, err)
	defer e.Close()
	got, err := e.Eval(context.Background(), "lisp2", "(do (defn id [x] x) (funcall (function id) 5))")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 5}.Equals(got))
}
