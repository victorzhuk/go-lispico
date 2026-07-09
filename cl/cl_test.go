package cl

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
	"github.com/victorzhuk/go-lispico/runtime"
)

// newEngine builds a CL engine with stdlib loaded and bridges the value →
// function cell for Lisp-2 head-position resolution.  Under Lisp-2,
// applyVocabulary sets the value cell (env.Set) but head position resolves
// through the function cell (env.GetFunc), so vocab entries and adapters that
// are GoFuncs need a copy into the function cell.
func newEngine(t *testing.T) runtime.Engine {
	t.Helper()
	e, err := runtime.New(nil, runtime.WithDialect(Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { e.Close() })

	require.NoError(t, e.Use(stdlib.New()))

	bridgeGoFuncsToFuncCells(t, e.RootEnv())
	return e
}

func bridgeGoFuncsToFuncCells(t *testing.T, env *core.Env) {
	t.Helper()
	for _, name := range env.VarNames() {
		if val, ok := env.Get(name); ok {
			if _, isGoFunc := val.(core.GoFunc); isGoFunc {
				env.SetFunc(name, val)
			}
		}
	}
}

// evalPreRead parses src with the identity reader (so [x] vector syntax works)
// and evaluates it through the engine's evaluator under the CL dialect.
func evalPreRead(t *testing.T, eng runtime.Engine, src string) core.Value {
	t.Helper()
	forms, err := core.Read(src)
	require.NoError(t, err, "core.Read: %s", src)
	require.Len(t, forms, 1, "expected one form")
	evaluator := eng.RootEnv().Evaluator()
	got, err := evaluator.Eval(context.Background(), forms[0], eng.RootEnv())
	require.NoError(t, err, "eval: %s", src)
	return got
}

// TestCL_IsNotIdentity asserts that the CL dialect is non-identity because of
// its non-default axes (Lisp-2, nil-only truthiness, CL reader flags).
func TestCL_IsNotIdentity(t *testing.T) {
	assert.False(t, Dialect().IsIdentity(), "CL dialect must be non-identity")
}

// TestCL_Vocab_DrivesDialect exercises the spec scenarios from the dialect spec.
func TestCL_Vocab_DrivesDialect(t *testing.T) {
	e := newEngine(t)

	t.Run("defun defines a function", func(t *testing.T) {
		got := evalPreRead(t, e, "(defun f [x] x)")
		assert.True(t, core.Keyword{V: "fn"}.Equals(got.Type()), "defun must return fn, got %T", got)
	})

	t.Run("nil-only truthiness: false is truthy", func(t *testing.T) {
		got, err := e.Eval(context.Background(), "cl", "(if false :y :n)")
		require.NoError(t, err)
		assert.True(t, core.Keyword{V: "y"}.Equals(got), "nil-only: false is truthy, got %v", got)
	})

	t.Run("funcall applies a function", func(t *testing.T) {
		evalPreRead(t, e, "(defun f [x] x)")
		got, err := e.Eval(context.Background(), "cl", "(progn (funcall #'f 42))")
		require.NoError(t, err)
		assert.True(t, core.Int{V: 42}.Equals(got), "funcall #'f: got %v", got)
	})

	t.Run("#'f parses", func(t *testing.T) {
		got, err := e.Eval(context.Background(), "cl", "(quote #'f)")
		require.NoError(t, err)
		want := core.List{Items: []core.Value{core.Symbol{V: "function"}, core.Symbol{V: "f"}}}
		assert.True(t, want.Equals(got), "#'f must read as (function f), got %v", got)
	})

	t.Run("#(1 2) parses as a vector", func(t *testing.T) {
		got, err := e.Eval(context.Background(), "cl", "(quote #(1 2))")
		require.NoError(t, err)
		want := core.Vector{Items: []core.Value{core.Int{V: 1}, core.Int{V: 2}}}
		assert.True(t, want.Equals(got), "#(1 2) must read as a vector, got %v", got)
	})

	t.Run("[1 2] produces a parse error", func(t *testing.T) {
		_, err := e.Eval(context.Background(), "cl", "(quote [1 2])")
		require.Error(t, err, "[..] must fail to read as a vector literal under CL")
	})
}

// TestCL_VocabMap asserts that the CL vocabulary renames work.
func TestCL_VocabMap(t *testing.T) {
	e := newEngine(t)

	t.Run("car reads first element", func(t *testing.T) {
		got, err := e.Eval(context.Background(), "cl", "(car '(1 2 3))")
		require.NoError(t, err)
		assert.True(t, core.Int{V: 1}.Equals(got), "car: got %v", got)
	})

	t.Run("cdr reads rest", func(t *testing.T) {
		got, err := e.Eval(context.Background(), "cl", "(cdr '(1 2 3))")
		require.NoError(t, err)
		want := core.List{Items: []core.Value{core.Int{V: 2}, core.Int{V: 3}}}
		assert.True(t, want.Equals(got), "cdr: got %v", got)
	})

	t.Run("null checks nil", func(t *testing.T) {
		got, err := e.Eval(context.Background(), "cl", "(null nil)")
		require.NoError(t, err)
		assert.True(t, core.Bool{V: true}.Equals(got), "null of nil: got %v", got)
	})
}

// TestCL_SpecScenario_SurfaceForms evaluates the exact scenario from the spec.
func TestCL_SpecScenario_SurfaceForms(t *testing.T) {
	e := newEngine(t)

	// "defun SHALL define a function"
	got := evalPreRead(t, e, "(defun square [x] (* x x))")
	t.Logf("defun square => %s", got)

	// "(if false :y :n) SHALL evaluate to :y" (nil-only truthiness)
	got, err := e.Eval(context.Background(), "cl", "(if false :y :n)")
	require.NoError(t, err)
	assert.True(t, core.Keyword{V: "y"}.Equals(got), "nil-only: (if false :y :n) => %v", got)

	// "(funcall #'f args...) SHALL apply f"
	evalPreRead(t, e, "(defun id [x] x)")
	got, err = e.Eval(context.Background(), "cl", "(funcall #'id 7)")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 7}.Equals(got), "funcall #'id => %v", got)
}

// TestCL_SpecScenario_ReaderAffordances exercises the reader spec scenario.
func TestCL_SpecScenario_ReaderAffordances(t *testing.T) {
	e := newEngine(t)

	// #'f SHALL parse
	got, err := e.Eval(context.Background(), "cl", "(quote #'f)")
	require.NoError(t, err)
	want := core.List{Items: []core.Value{core.Symbol{V: "function"}, core.Symbol{V: "f"}}}
	assert.True(t, want.Equals(got), "#'f => (function f)")

	// #(...) SHALL parse
	got, err = e.Eval(context.Background(), "cl", "(quote #(1 2 3))")
	require.NoError(t, err)
	wantVec := core.Vector{Items: []core.Value{core.Int{V: 1}, core.Int{V: 2}, core.Int{V: 3}}}
	assert.True(t, wantVec.Equals(got), "#(1 2 3) => vector")

	// [1 2] SHALL NOT read as a vector literal
	_, err = e.Eval(context.Background(), "cl", "(quote [1 2])")
	require.Error(t, err, "[1 2] must fail under CL")
}
