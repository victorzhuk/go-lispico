package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
)

// --- 1.1 Clojure flat cond ---

func TestCondShape_ClojureFlatCond_TreeWalker(t *testing.T) {
	t.Parallel()
	e, err := New(nil, WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })

	got, err := e.Eval(context.Background(), "test", "(cond true :pos :else :none)")
	require.NoError(t, err)
	assert.Equal(t, core.Keyword{V: "pos"}, got)
}

func TestCondShape_ClojureFlatCond_Bytecode(t *testing.T) {
	t.Parallel()
	e, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })

	got, err := e.Eval(context.Background(), "test", "(cond true :pos :else :none)")
	require.NoError(t, err)
	assert.Equal(t, core.Keyword{V: "pos"}, got)
}

func TestCondShape_ClojureFlatCond_NoMatch_TreeWalker(t *testing.T) {
	t.Parallel()
	e, err := New(nil, WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })

	got, err := e.Eval(context.Background(), "test", "(cond false :a false :b)")
	require.NoError(t, err)
	assert.Equal(t, core.Nil{}, got)
}

func TestCondShape_ClojureFlatCond_NoMatch_Bytecode(t *testing.T) {
	t.Parallel()
	e, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })

	got, err := e.Eval(context.Background(), "test", "(cond false :a false :b)")
	require.NoError(t, err)
	assert.Equal(t, core.Nil{}, got)
}

// --- 1.2 CL nested multi-body ---

func TestCondShape_CL_MultiBody_TreeWalker(t *testing.T) {
	t.Parallel()
	e, err := New(nil) // default CL dialect
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })

	got, err := e.Eval(context.Background(), "test", "(cond (true 1 2 3))")
	require.NoError(t, err)
	assert.Equal(t, core.Int{V: 3}, got)
}

func TestCondShape_CL_MultiBody_Bytecode(t *testing.T) {
	t.Parallel()
	e, err := New(nil, WithBytecode())
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })

	got, err := e.Eval(context.Background(), "test", "(cond (true 1 2 3))")
	require.NoError(t, err)
	assert.Equal(t, core.Int{V: 3}, got)
}

func TestCondShape_CL_MultiBody_NoMatch_TreeWalker(t *testing.T) {
	t.Parallel()
	e, err := New(nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })

	got, err := e.Eval(context.Background(), "test", "(cond (nil 1 2 3))")
	require.NoError(t, err)
	assert.Equal(t, core.Nil{}, got)
}

// --- 1.3 Quoted cond round-trips ---

func TestCondShape_QuotedCond_RoundTrip_CL_TreeWalker(t *testing.T) {
	t.Parallel()
	e, err := New(nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })

	got, err := e.Eval(context.Background(), "test", "(quote (cond (a b)))")
	require.NoError(t, err)
	expected := core.List{Items: []core.Value{
		core.Symbol{V: "cond"},
		core.List{Items: []core.Value{core.Symbol{V: "a"}, core.Symbol{V: "b"}}},
	}}
	assert.True(t, got.Equals(expected), "got %v, want %v", got, expected)
}

func TestCondShape_QuotedCond_RoundTrip_CL_Bytecode(t *testing.T) {
	t.Parallel()
	e, err := New(nil, WithBytecode())
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })

	got, err := e.Eval(context.Background(), "test", "(quote (cond (a b)))")
	require.NoError(t, err)
	expected := core.List{Items: []core.Value{
		core.Symbol{V: "cond"},
		core.List{Items: []core.Value{core.Symbol{V: "a"}, core.Symbol{V: "b"}}},
	}}
	assert.True(t, got.Equals(expected), "got %v, want %v", got, expected)
}

func TestCondShape_QuotedCond_RoundTrip_Clojure_TreeWalker(t *testing.T) {
	t.Parallel()
	e, err := New(nil, WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })

	got, err := e.Eval(context.Background(), "test", "(quote (cond a b c d))")
	require.NoError(t, err)
	expected := core.List{Items: []core.Value{
		core.Symbol{V: "cond"},
		core.Symbol{V: "a"},
		core.Symbol{V: "b"},
		core.Symbol{V: "c"},
		core.Symbol{V: "d"},
	}}
	assert.True(t, got.Equals(expected), "got %v, want %v", got, expected)
}

func TestCondShape_QuotedCond_RoundTrip_Clojure_Bytecode(t *testing.T) {
	t.Parallel()
	e, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })

	got, err := e.Eval(context.Background(), "test", "(quote (cond a b c d))")
	require.NoError(t, err)
	expected := core.List{Items: []core.Value{
		core.Symbol{V: "cond"},
		core.Symbol{V: "a"},
		core.Symbol{V: "b"},
		core.Symbol{V: "c"},
		core.Symbol{V: "d"},
	}}
	assert.True(t, got.Equals(expected), "got %v, want %v", got, expected)
}

// --- 1.4 Malformed shapes (bytecode path) ---

func TestCondShape_Malformed_CL_NonList_Bytecode(t *testing.T) {
	t.Parallel()
	e, err := New(nil, WithBytecode())
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })

	_, err = e.Eval(context.Background(), "test", "(cond 42)")
	var lispErr *core.LispicoError
	require.True(t, errors.As(err, &lispErr), "expected *core.LispicoError, got %T", err)
	assert.NotEmpty(t, lispErr.Code)
}

func TestCondShape_Malformed_CL_EmptyClause_Bytecode(t *testing.T) {
	t.Parallel()
	e, err := New(nil, WithBytecode())
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })

	_, err = e.Eval(context.Background(), "test", "(cond ())")
	var lispErr *core.LispicoError
	require.True(t, errors.As(err, &lispErr), "expected *core.LispicoError, got %T", err)
	assert.NotEmpty(t, lispErr.Code)
}

func TestCondShape_Malformed_Clojure_OddPair_Bytecode(t *testing.T) {
	t.Parallel()
	e, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })

	_, err = e.Eval(context.Background(), "test", "(cond a)")
	var lispErr *core.LispicoError
	require.True(t, errors.As(err, &lispErr), "expected *core.LispicoError, got %T", err)
	assert.NotEmpty(t, lispErr.Code)
}

func TestCondShape_Malformed_CL_NonList_TreeWalker(t *testing.T) {
	t.Parallel()
	e, err := New(nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })

	_, err = e.Eval(context.Background(), "test", "(cond 42)")
	var lispErr *core.LispicoError
	require.True(t, errors.As(err, &lispErr), "expected *core.LispicoError, got %T", err)
	assert.NotEmpty(t, lispErr.Code)
}

func TestCondShape_Malformed_CL_EmptyClause_TreeWalker(t *testing.T) {
	t.Parallel()
	e, err := New(nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })

	_, err = e.Eval(context.Background(), "test", "(cond ())")
	var lispErr *core.LispicoError
	require.True(t, errors.As(err, &lispErr), "expected *core.LispicoError, got %T", err)
	assert.NotEmpty(t, lispErr.Code)
}

func TestCondShape_Malformed_Clojure_OddPair_TreeWalker(t *testing.T) {
	t.Parallel()
	e, err := New(nil, WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })

	_, err = e.Eval(context.Background(), "test", "(cond a)")
	var lispErr *core.LispicoError
	require.True(t, errors.As(err, &lispErr), "expected *core.LispicoError, got %T", err)
	assert.NotEmpty(t, lispErr.Code)
}

// --- 1.3b Quasiquoted cond round-trips ---

func TestCondShape_QuasiquotedCond_RoundTrip_CL_TreeWalker(t *testing.T) {
	t.Parallel()
	e, err := New(nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })

	got, err := e.Eval(context.Background(), "test", "`(cond (a b))")
	require.NoError(t, err)
	expected := core.List{Items: []core.Value{
		core.Symbol{V: "cond"},
		core.List{Items: []core.Value{core.Symbol{V: "a"}, core.Symbol{V: "b"}}},
	}}
	assert.True(t, got.Equals(expected), "got %v, want %v", got, expected)
}

func TestCondShape_QuasiquotedCond_RoundTrip_CL_Bytecode(t *testing.T) {
	t.Parallel()
	e, err := New(nil, WithBytecode())
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })

	got, err := e.Eval(context.Background(), "test", "`(cond (a b))")
	require.NoError(t, err)
	expected := core.List{Items: []core.Value{
		core.Symbol{V: "cond"},
		core.List{Items: []core.Value{core.Symbol{V: "a"}, core.Symbol{V: "b"}}},
	}}
	assert.True(t, got.Equals(expected), "got %v, want %v", got, expected)
}

func TestCondShape_QuasiquotedCond_RoundTrip_Clojure_TreeWalker(t *testing.T) {
	t.Parallel()
	e, err := New(nil, WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })

	got, err := e.Eval(context.Background(), "test", "`(cond a b c d)")
	require.NoError(t, err)
	expected := core.List{Items: []core.Value{
		core.Symbol{V: "cond"},
		core.Symbol{V: "a"},
		core.Symbol{V: "b"},
		core.Symbol{V: "c"},
		core.Symbol{V: "d"},
	}}
	assert.True(t, got.Equals(expected), "got %v, want %v", got, expected)
}

func TestCondShape_QuasiquotedCond_RoundTrip_Clojure_Bytecode(t *testing.T) {
	t.Parallel()
	e, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })

	got, err := e.Eval(context.Background(), "test", "`(cond a b c d)")
	require.NoError(t, err)
	expected := core.List{Items: []core.Value{
		core.Symbol{V: "cond"},
		core.Symbol{V: "a"},
		core.Symbol{V: "b"},
		core.Symbol{V: "c"},
		core.Symbol{V: "d"},
	}}
	assert.True(t, got.Equals(expected), "got %v, want %v", got, expected)
}

func TestCondShape_QuasiquotedCond_Unquote_RoundTrip_CL_TreeWalker(t *testing.T) {
	t.Parallel()
	e, err := New(nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })
	bindBuiltin(t, e, "+")

	got, err := e.Eval(context.Background(), "test", "`(cond (~(+ 1 2) x))")
	require.NoError(t, err)
	expected := core.List{Items: []core.Value{
		core.Symbol{V: "cond"},
		core.List{Items: []core.Value{core.Int{V: 3}, core.Symbol{V: "x"}}},
	}}
	assert.True(t, got.Equals(expected), "got %v, want %v", got, expected)
}

func TestCondShape_QuasiquotedCond_Unquote_RoundTrip_CL_Bytecode(t *testing.T) {
	t.Parallel()
	e, err := New(nil, WithBytecode())
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })
	bindBuiltin(t, e, "+")

	got, err := e.Eval(context.Background(), "test", "`(cond (~(+ 1 2) x))")
	require.NoError(t, err)
	expected := core.List{Items: []core.Value{
		core.Symbol{V: "cond"},
		core.List{Items: []core.Value{core.Int{V: 3}, core.Symbol{V: "x"}}},
	}}
	assert.True(t, got.Equals(expected), "got %v, want %v", got, expected)
}

func TestCondShape_QuasiquotedCond_Unquote_RoundTrip_Clojure_TreeWalker(t *testing.T) {
	t.Parallel()
	e, err := New(nil, WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })
	bindBuiltin(t, e, "+")

	got, err := e.Eval(context.Background(), "test", "`(cond ~(+ 1 2) x)")
	require.NoError(t, err)
	expected := core.List{Items: []core.Value{
		core.Symbol{V: "cond"},
		core.Int{V: 3},
		core.Symbol{V: "x"},
	}}
	assert.True(t, got.Equals(expected), "got %v, want %v", got, expected)
}

func TestCondShape_QuasiquotedCond_Unquote_RoundTrip_Clojure_Bytecode(t *testing.T) {
	t.Parallel()
	e, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })
	bindBuiltin(t, e, "+")

	got, err := e.Eval(context.Background(), "test", "`(cond ~(+ 1 2) x)")
	require.NoError(t, err)
	expected := core.List{Items: []core.Value{
		core.Symbol{V: "cond"},
		core.Int{V: 3},
		core.Symbol{V: "x"},
	}}
	assert.True(t, got.Equals(expected), "got %v, want %v", got, expected)
}
