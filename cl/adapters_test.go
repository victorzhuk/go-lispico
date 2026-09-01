package cl_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
	"github.com/victorzhuk/go-lispico/runtime"
)

func listOf(vs ...core.Value) core.List {
	return core.NewList(vs)
}

func intList(vs ...int64) core.List {
	items := make([]core.Value, len(vs))
	for i, v := range vs {
		items[i] = core.Int{V: v}
	}
	return core.NewList(items)
}

func TestCLNth_Goldens(t *testing.T) {
	e := newEngine(t)
	ctx := context.Background()

	got, err := e.Eval(ctx, "nth", "(nth 1 '(10 20 30))")
	require.NoError(t, err)
	assert.True(t, (core.Int{V: 20}).Equals(got), "(nth 1 '(10 20 30)) = %v, want 20", got)

	got, err = e.Eval(ctx, "nth", "(nth 5 '(1 2))")
	require.NoError(t, err)
	assert.True(t, (core.Nil{}).Equals(got), "(nth 5 '(1 2)) = %v, want nil (out of range is nil, not an error)", got)

	got, err = e.Eval(ctx, "nth", "(nth 0 nil)")
	require.NoError(t, err)
	assert.True(t, (core.Nil{}).Equals(got), "(nth 0 nil) = %v, want nil", got)
}

func TestCLMapcar_Goldens(t *testing.T) {
	e := newEngine(t)
	ctx := context.Background()

	got, err := e.Eval(ctx, "mapcar", "(mapcar #'+ '(1 2 3))")
	require.NoError(t, err)
	assert.True(t, intList(1, 2, 3).Equals(got), "(mapcar #'+ '(1 2 3)) = %v, want (1 2 3)", got)

	got, err = e.Eval(ctx, "mapcar", "(mapcar #'+ '(1 2) '(10 20))")
	require.NoError(t, err)
	assert.True(t, intList(11, 22).Equals(got), "(mapcar #'+ '(1 2) '(10 20)) = %v, want (11 22)", got)

	got, err = e.Eval(ctx, "mapcar", "(mapcar #'+ '(1 2 3) '(10 20))")
	require.NoError(t, err)
	assert.True(t, intList(11, 22).Equals(got), "(mapcar #'+ '(1 2 3) '(10 20)) = %v, want (11 22): shortest list terminates", got)
}

func TestCLSort_Goldens(t *testing.T) {
	e := newEngine(t)
	ctx := context.Background()

	got, err := e.Eval(ctx, "sort", "(sort '(3 1 2) #'<)")
	require.NoError(t, err)
	assert.True(t, intList(1, 2, 3).Equals(got), "(sort '(3 1 2) #'<) = %v, want (1 2 3)", got)

	got, err = e.Eval(ctx, "sort", "(sort '(3 1 2) #'>)")
	require.NoError(t, err)
	assert.True(t, intList(3, 2, 1).Equals(got), "(sort '(3 1 2) #'>) = %v, want (3 2 1)", got)

	got, err = e.Eval(ctx, "sort", "(sort '(\"bb\" \"a\" \"ccc\") #'< :key #'length)")
	require.NoError(t, err)
	assert.True(t, listOf(core.String{V: "a"}, core.String{V: "bb"}, core.String{V: "ccc"}).Equals(got),
		"(sort with :key #'length) = %v, want (\"a\" \"bb\" \"ccc\")", got)

	got, err = e.Eval(ctx, "sort", "(sort '(\"bb\" \"a\") #'> :key #'length)")
	require.NoError(t, err)
	assert.True(t, listOf(core.String{V: "bb"}, core.String{V: "a"}).Equals(got),
		"(sort with predicate and :key) = %v, want (\"bb\" \"a\")", got)

	_, err = e.Eval(ctx, "sort", "(sort '(3 1 2))")
	assert.Error(t, err, "sort without a predicate must be rejected by the CL grammar")

	_, err = e.Eval(ctx, "sort", "(sort '(3 1 2) :key #'length)")
	assert.Error(t, err, "sort with :key but no predicate must be rejected by the CL grammar")
}
// TestCLAdapters_Lisp2Callbacks: callbacks named in head position resolve
// through the function cell — lambda, defun, and GoFunc targets.
func TestCLAdapters_Lisp2Callbacks(t *testing.T) {
	e := newEngine(t)
	ctx := context.Background()

	_, err := e.Eval(ctx, "defun", "(defun dbl (x) (* x 2))")
	require.NoError(t, err)

	got, err := e.Eval(ctx, "mapcar", "(mapcar #'dbl '(1 2 3))")
	require.NoError(t, err)
	assert.True(t, intList(2, 4, 6).Equals(got), "(mapcar #'dbl '(1 2 3)) = %v, want (2 4 6)", got)

	got, err = e.Eval(ctx, "funcall", "(funcall #'dbl 5)")
	require.NoError(t, err)
	assert.True(t, (core.Int{V: 10}).Equals(got), "(funcall #'dbl 5) = %v, want 10", got)

	_, err = e.Eval(ctx, "defun", "(defun lt (a b) (< a b))")
	require.NoError(t, err)

	got, err = e.Eval(ctx, "sort", "(sort '(3 1 2) #'lt)")
	require.NoError(t, err)
	assert.True(t, intList(1, 2, 3).Equals(got), "(sort '(3 1 2) #'lt) = %v, want (1 2 3)", got)

	got, err = e.Eval(ctx, "sort", "(sort '(3 1 2) (lambda (a b) (< a b)))")
	require.NoError(t, err)
	assert.True(t, intList(1, 2, 3).Equals(got), "(sort '(3 1 2) (lambda (a b) (< a b))) = %v, want (1 2 3)", got)

	require.NoError(t, e.RootEnv().SetBoth("rec", core.GoFunc{
		Name: "rec",
		Fn: func(ctx context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
			return core.Int{V: args[0].(core.Int).V * 2}, nil
		},
	}))

	got, err = e.Eval(ctx, "mapcar", "(mapcar #'rec '(1 2 3))")
	require.NoError(t, err)
	assert.True(t, intList(2, 4, 6).Equals(got), "(mapcar #'rec '(1 2 3)) = %v, want (2 4 6)", got)

	require.NoError(t, e.RootEnv().SetBoth("ltrec", core.GoFunc{
		Name: "ltrec",
		Fn: func(ctx context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
			return core.Bool{V: args[0].(core.Int).V < args[1].(core.Int).V}, nil
		},
	}))

	got, err = e.Eval(ctx, "sort", "(sort '(3 1 2) #'ltrec)")
	require.NoError(t, err)
	assert.True(t, intList(1, 2, 3).Equals(got), "(sort '(3 1 2) #'ltrec) = %v, want (1 2 3)", got)
}

// TestCLAdapters_EmptyAndNil: empty and nil subjects are valid inputs for
// every collection adapter; no callback runs over an empty sequence.
func TestCLAdapters_EmptyAndNil(t *testing.T) {
	e := newEngine(t)
	ctx := context.Background()

	var calls int
	require.NoError(t, e.RootEnv().SetBoth("f", core.GoFunc{
		Name: "f",
		Fn: func(ctx context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
			calls++
			return args[0], nil
		},
	}))

	got, err := e.Eval(ctx, "nth", "(nth 0 nil)")
	require.NoError(t, err)
	assert.True(t, (core.Nil{}).Equals(got), "(nth 0 nil) = %v, want nil", got)

	got, err = e.Eval(ctx, "mapcar", "(mapcar #'f nil)")
	require.NoError(t, err)
	assert.True(t, (core.Nil{}).Equals(got), "(mapcar #'f nil) = %v, want nil", got)
	assert.Zero(t, calls, "mapcar over nil must not call the callback")

	got, err = e.Eval(ctx, "sort", "(sort nil #'f)")
	require.NoError(t, err)
	assert.True(t, (core.Nil{}).Equals(got), "(sort nil #'f) = %v, want nil", got)
	assert.Zero(t, calls, "sort of nil must not call the predicate")
}

// TestCLAdapters_CallbackErrors: a callback error propagates unchanged and no
// callback runs after the first error.
func TestCLAdapters_CallbackErrors(t *testing.T) {
	e := newEngine(t)
	ctx := context.Background()

	t.Run("typed predicate error", func(t *testing.T) {
		var calls int
		require.NoError(t, e.RootEnv().SetBoth("bad", core.GoFunc{
			Name: "bad",
			Fn: func(ctx context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
				calls++
				if calls == 2 {
					return nil, core.NewTypeError("int", core.Int{V: 1})
				}
				return core.Bool{V: true}, nil
			},
		}))

		_, err := e.Eval(ctx, "sort", "(sort '(3 1 2) #'bad)")
		require.Error(t, err)
		var typeErr *core.LispicoError
		require.ErrorAs(t, err, &typeErr)
		assert.Equal(t, "TypeError", typeErr.Code, "typed predicate error must propagate unchanged")
		assert.Equal(t, 2, calls, "no predicate call may follow the first error")
	})

	t.Run("typed callback error", func(t *testing.T) {
		var calls int
		require.NoError(t, e.RootEnv().SetBoth("badmap", core.GoFunc{
			Name: "badmap",
			Fn: func(ctx context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
				calls++
				if calls == 2 {
					return nil, core.NewTypeError("int", core.Int{V: 1})
				}
				return args[0], nil
			},
		}))

		_, err := e.Eval(ctx, "mapcar", "(mapcar #'badmap '(1 2 3))")
		require.Error(t, err)
		var typeErr *core.LispicoError
		require.ErrorAs(t, err, &typeErr)
		assert.Equal(t, "TypeError", typeErr.Code, "typed callback error must propagate unchanged")
		assert.Equal(t, 2, calls, "no callback may run after the first error")
	})

	t.Run("terminal predicate error", func(t *testing.T) {
		var calls int
		require.NoError(t, e.RootEnv().SetBoth("boom", core.GoFunc{
			Name: "boom",
			Fn: func(ctx context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
				calls++
				if calls == 2 {
					return nil, core.NewResourceLimitError("boom")
				}
				return core.Bool{V: true}, nil
			},
		}))

		_, err := e.Eval(ctx, "sort", "(sort '(3 1 2) #'boom)")
		require.Error(t, err)
		assert.True(t, core.IsTerminalEvalError(err), "resource-limit callback error must be terminal, got %v", err)
		assert.Equal(t, 2, calls, "no predicate call may follow the first error")
	})
}
// TestCLSort_CallbackOrderAndCount: key projections run exactly once per
// element, in original order, before any comparison; equivalent elements
// keep their input order; the first predicate error stops the sort.
func TestCLSort_CallbackOrderAndCount(t *testing.T) {
	e := newEngine(t)
	ctx := context.Background()

	var events []string
	keyFn := core.GoFunc{
		Name: "k",
		Fn: func(ctx context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
			events = append(events, "k:"+args[0].String())
			return args[0], nil
		},
	}
	predFn := core.GoFunc{
		Name: "p",
		Fn: func(ctx context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
			events = append(events, "p:"+args[0].String()+","+args[1].String())
			return core.Bool{V: args[0].(core.Int).V <= args[1].(core.Int).V}, nil
		},
	}
	require.NoError(t, e.RootEnv().SetBoth("k", keyFn))
	require.NoError(t, e.RootEnv().SetBoth("p", predFn))

	split := func() (keys, preds []string) {
		seenPred := false
		for _, ev := range events {
			if strings.HasPrefix(ev, "k:") {
				if seenPred {
					t.Errorf("key projection %q ran after a predicate call: %v", ev, events)
				}
				keys = append(keys, ev)
				continue
			}
			preds = append(preds, ev)
			seenPred = true
		}
		return keys, preds
	}

	// Without :key the key function must never run (nil key is identity).
	got, err := e.Eval(ctx, "sort", "(sort '(3 1 2) #'p)")
	require.NoError(t, err)
	assert.True(t, intList(1, 2, 3).Equals(got), "(sort '(3 1 2) #'p) = %v, want (1 2 3)", got)
	if keys, _ := split(); len(keys) != 0 {
		t.Errorf("key function must not run without :key, got %v", keys)
	}

	// Each element is projected exactly once, in original order, all before
	// the first comparison.
	events = nil
	got, err = e.Eval(ctx, "sort", "(sort '(3 1 2) #'p :key #'k)")
	require.NoError(t, err)
	assert.True(t, intList(1, 2, 3).Equals(got), "(sort '(3 1 2) #'p :key #'k) = %v, want (1 2 3)", got)
	keys, preds := split()
	assert.Equal(t, []string{"k:3", "k:1", "k:2"}, keys, "key projections must be once per element in original order")
	assert.NotEmpty(t, preds, "comparisons must still run")

	// Generalized truthiness: a keyword is truthy in predicate position.
	got, err = e.Eval(ctx, "sort", "(sort '(3 1 2) (lambda (a b) (if (< a b) :yes nil)))")
	require.NoError(t, err)
	assert.True(t, intList(1, 2, 3).Equals(got), "keyword-returning predicate = %v, want (1 2 3)", got)

	// Equivalent projected keys keep input order, and the predicate receives
	// the projected keys, not the elements.
	got, err = e.Eval(ctx, "sort", "(sort '((1 :a) (2 :b) (1 :c)) #'p :key #'car)")
	require.NoError(t, err)
	want := listOf(
		listOf(core.Int{V: 1}, core.Keyword{V: "a"}),
		listOf(core.Int{V: 1}, core.Keyword{V: "c"}),
		listOf(core.Int{V: 2}, core.Keyword{V: "b"}),
	)
	assert.True(t, want.Equals(got), "stable sort of ((1 :a) (2 :b) (1 :c)) = %v, want ((1 :a) (1 :c) (2 :b))", got)

	// The first predicate error propagates unchanged and stops the sort; key
	// projections still all precede the first comparison.
	var pCalls int
	require.NoError(t, e.RootEnv().SetBoth("pstop", core.GoFunc{
		Name: "pstop",
		Fn: func(ctx context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
			pCalls++
			if pCalls == 2 {
				return nil, core.NewTypeError("int", core.Int{V: 1})
			}
			return core.Bool{V: true}, nil
		},
	}))
	events = nil
	_, err = e.Eval(ctx, "sort", "(sort '(3 1 2) #'pstop :key #'k)")
	require.Error(t, err)
	var typeErr *core.LispicoError
	require.ErrorAs(t, err, &typeErr)
	assert.Equal(t, "TypeError", typeErr.Code, "predicate error must propagate unchanged")
	assert.Equal(t, 2, pCalls, "no comparison may follow the first error")
	keys, preds = split()
	assert.Equal(t, []string{"k:3", "k:1", "k:2"}, keys, "all key projections run before the first comparison")
	assert.Len(t, preds, 2, "two comparisons precede the erroring call")
}

// TestCLSort_Immutability: sort returns a fresh result and leaves the input
// sequence unchanged.
func TestCLSort_Immutability(t *testing.T) {
	e := newEngine(t)
	ctx := context.Background()

	_, err := e.Eval(ctx, "bind", "(def xs '(3 1 2))")
	require.NoError(t, err)

	got, err := e.Eval(ctx, "sort", "(sort xs #'<)")
	require.NoError(t, err)
	assert.True(t, intList(1, 2, 3).Equals(got), "(sort xs #'<) = %v, want (1 2 3)", got)

	got, err = e.Eval(ctx, "read", "xs")
	require.NoError(t, err)
	assert.True(t, intList(3, 1, 2).Equals(got), "sort must not mutate its input: xs = %v, want (3 1 2)", got)

	_, err = e.Eval(ctx, "bind", "(def vs #(3 1 2))")
	require.NoError(t, err)

	_, err = e.Eval(ctx, "sort", "(sort vs #'<)")
	require.NoError(t, err)

	got, err = e.Eval(ctx, "read", "vs")
	require.NoError(t, err)
	assert.True(t, core.NewVector([]core.Value{core.Int{V: 3}, core.Int{V: 1}, core.Int{V: 2}}).Equals(got),
		"sort must not mutate its vector input: vs = %v, want #(3 1 2)", got)
}

// TestCLAdapters_CanonicalParity: the CL adapters and the canonical stdlib
// names share one kernel — same inputs through each produce the same result.
func TestCLAdapters_CanonicalParity(t *testing.T) {
	canonical, err := runtime.New(nil, runtime.WithDialect(core.FullDialect()))
	require.NoError(t, err)
	defer canonical.Close()
	require.NoError(t, canonical.Use(stdlib.New()))

	clEng := newEngine(t)
	ctx := context.Background()
	_, err = clEng.Eval(ctx, "defun", "(defun sq (x) (* x x))")
	require.NoError(t, err)

	cases := []struct {
		name     string
		canonSrc string
		clSrc    string
		want     core.Value
	}{
		{"nth", "(nth '(10 20 30) 1)", "(nth 1 '(10 20 30))", core.Int{V: 20}},
		{"map", "(map (fn [x] (* x x)) '(1 2 3))", "(mapcar #'sq '(1 2 3))", intList(1, 4, 9)},
		{"sort", "(sort '(3 1 2))", "(sort '(3 1 2) #'<)", intList(1, 2, 3)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := canonical.Eval(ctx, "canonical", tc.canonSrc)
			require.NoError(t, err)
			assert.True(t, tc.want.Equals(got), "canonical %q = %v, want %v", tc.canonSrc, got, tc.want)

			got, err = clEng.Eval(ctx, "cl", tc.clSrc)
			require.NoError(t, err)
			assert.True(t, tc.want.Equals(got), "CL %q = %v, want %v", tc.clSrc, got, tc.want)
		})
	}
}
