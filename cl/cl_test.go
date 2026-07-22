package cl_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/cl"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
	"github.com/victorzhuk/go-lispico/runtime"
)

func newEngine(t *testing.T, opts ...runtime.EngineOption) runtime.Engine {
	t.Helper()
	options := append([]runtime.EngineOption{runtime.WithDialect(cl.Dialect())}, opts...)
	e, err := runtime.New(nil, options...)
	require.NoError(t, err)
	t.Cleanup(func() { e.Close() })

	require.NoError(t, e.Use(stdlib.New()))
	return e
}

// TestCL_IsNotIdentity asserts that the CL dialect is non-identity because of
// its non-default axes (Lisp-2, CL reader flags).
func TestCL_IsNotIdentity(t *testing.T) {
	assert.False(t, cl.Dialect().IsIdentity(), "CL dialect must be non-identity")
}

// TestCL_Vocab_DrivesDialect exercises the spec scenarios from the dialect spec.
func TestCL_Vocab_DrivesDialect(t *testing.T) {
	e := newEngine(t)

	t.Run("defun defines a function", func(t *testing.T) {
		got, err := e.Eval(context.Background(), "cl", "(defun f (x) x)")
		require.NoError(t, err)
		require.True(t, core.Keyword{V: "fn"}.Equals(got.Type()), "defun must return :fn, got %s", got.Type())

		got, err = e.Eval(context.Background(), "cl", "(f 42)")
		require.NoError(t, err)
		assert.True(t, core.Int{V: 42}.Equals(got), "defun call should work, got %v", got)
	})

	t.Run("false is falsy", func(t *testing.T) {
		got, err := e.Eval(context.Background(), "cl", "(if false :y :n)")
		require.NoError(t, err)
		assert.True(t, core.Keyword{V: "n"}.Equals(got), "false is falsy, got %v", got)
	})

	t.Run("funcall applies a function", func(t *testing.T) {
		_, err := e.Eval(context.Background(), "cl", "(defun f (x) x)")
		require.NoError(t, err)
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
	got, err := e.Eval(context.Background(), "cl", "(defun square (x) (* x x))")
	require.NoError(t, err)
	t.Logf("defun square => %s", got)

	// "(if false :y :n)" evaluates to :n because false is falsy.
	got, err = e.Eval(context.Background(), "cl", "(if false :y :n)")
	require.NoError(t, err)
	assert.True(t, core.Keyword{V: "n"}.Equals(got), "(if false :y :n) => %v", got)

	// "(funcall #'f args...) SHALL apply f"
	_, err = e.Eval(context.Background(), "cl", "(defun id (x) x)")
	require.NoError(t, err)
	got, err = e.Eval(context.Background(), "cl", "(funcall #'id 7)")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 7}.Equals(got), "funcall #'id => %v", got)
}

func TestCL_ConditionalTruthiness_Evaluators(t *testing.T) {
	modes := []struct {
		name string
		opts []runtime.EngineOption
	}{
		{name: "tree-walker", opts: []runtime.EngineOption{runtime.WithTreeWalker()}},
		{name: "vm", opts: []runtime.EngineOption{runtime.WithBytecode()}},
	}
	cases := []struct {
		name string
		src  string
		want core.Value
	}{
		{name: "if predicate false", src: "(if (= 1 2) :a :b)", want: core.Keyword{V: "b"}},
		{name: "if false", src: "(if false :a :b)", want: core.Keyword{V: "b"}},
		{name: "when predicate false", src: "(when (= 1 2) :x)", want: core.Nil{}},
		{name: "unless predicate false", src: "(unless (= 1 2) :x)", want: core.Keyword{V: "x"}},
		{name: "cond predicate false", src: "(cond ((= 1 2) :a) (:else :b))", want: core.Keyword{V: "b"}},
		{name: "and predicate false", src: "(and (= 1 2) :x)", want: core.Bool{V: false}},
		{name: "or predicate false", src: "(or (= 1 2) :y)", want: core.Keyword{V: "y"}},
		{name: "not predicate false", src: "(not (= 1 2))", want: core.Bool{V: true}},
	}

	for _, mode := range modes {
		t.Run(mode.name, func(t *testing.T) {
			e := newEngine(t, mode.opts...)
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					got, err := e.Eval(context.Background(), "cl-truthiness", tc.src)
					require.NoError(t, err)
					require.True(t, tc.want.Equals(got), "%s => %v, want %v", tc.src, got, tc.want)
				})
			}
		})
	}
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
