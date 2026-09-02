package stdlib

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

func TestNilSequenceBoundary_ConsConj(t *testing.T) {
	env := setupEnv(t)
	one, two, three := core.Int{V: 1}, core.Int{V: 2}, core.Int{V: 3}
	list := func(vs ...core.Value) core.List { return core.NewList(vs) }

	rows := []boundaryRow{
		{name: "cons onto nil", input: `(cons 1 nil)`, want: list(one)},
		{name: "conj onto nil", input: `(conj nil 1)`, want: list(one)},
		{name: "conj onto nil keeps written order", input: `(conj nil 1 2 3)`, want: list(one, two, three)},
		{name: "conj nil keyword value takes the list branch", input: `(conj nil :a 1)`, want: list(core.Keyword{V: "a"}, one)},
		{name: "cons nil element onto empty list", input: `(cons nil '())`, want: list(core.Nil{})},
		{name: "conj nil element onto empty list", input: `(conj '() nil)`, want: list(core.Nil{})},
		{name: "cons onto scalar", input: `(cons 1 5)`, code: "TypeError", msg: "cons: expected collection, got core.Int"},
		{name: "conj onto scalar", input: `(conj 5 1)`, code: "TypeError", msg: "conj: expected collection, got core.Int"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) { assertRow(t, env, row) })
	}

	t.Run("output types", func(t *testing.T) {
		for _, input := range []string{`(cons 1 nil)`, `(conj nil 1)`, `(conj nil :a 1)`} {
			if got := eval(t, env, input); fmt.Sprintf("%T", got) != "core.List" {
				t.Errorf("%s: expected core.List, got %T", input, got)
			}
		}
	})

	t.Run("conj nil identical to conj empty list", func(t *testing.T) {
		got := eval(t, env, `(conj nil 1 2 3)`)
		want := eval(t, env, `(conj '() 1 2 3)`)
		if !want.Equals(got) {
			t.Errorf("(conj nil 1 2 3) = %v, want %v", got, want)
		}
	})
}

func TestNilSequence_ConsConjLimitsAndLedger(t *testing.T) {
	root := setupEnv(t)
	child := root.Child()
	child.SetEvaluator(nil)
	require.Nil(t, child.Evaluator())

	one, two, three := core.Int{V: 1}, core.Int{V: 2}, core.Int{V: 3}
	list := func(vs ...core.Value) core.List { return core.NewList(vs) }
	cons := collectionGoFunc(t, root, "cons")
	conj := collectionGoFunc(t, root, "conj")

	t.Run("cons within collection limit", func(t *testing.T) {
		got, err := cons.Fn(t.Context(), collectionLimitEvaluator{limit: 1}, []core.Value{one, core.Nil{}}, child)
		require.NoError(t, err)
		require.True(t, list(one).Equals(got), "got %v", got)
	})

	t.Run("conj length forced", func(t *testing.T) {
		_, err := conj.Fn(t.Context(), collectionLimitEvaluator{limit: 1}, []core.Value{core.Nil{}, one, two}, child)
		requireResourceLimit(t, err)
		require.ErrorContains(t, err, "conj length 2 exceeds collection limit 1")
	})

	t.Run("cons nested depth forced", func(t *testing.T) {
		_, err := cons.Fn(t.Context(), depthLimitEvaluator{limit: 1}, []core.Value{list(one), core.Nil{}}, child)
		requireResourceLimit(t, err)
		require.ErrorContains(t, err, "structural depth limit 1 exceeded")
	})

	t.Run("ledger charge equals empty-list path", func(t *testing.T) {
		charged := func(base core.Value) int64 {
			ctx := core.WithEvalResourceLimits(t.Context(), 1<<20, 4096)
			_, err := cons.Fn(ctx, nil, []core.Value{one, base}, child)
			require.NoError(t, err)
			return core.EvalMeterFrom(ctx).Snapshot().AllocationBytes
		}
		nilBytes, listBytes := charged(core.Nil{}), charged(list())
		require.Equal(t, listBytes, nilBytes, "nil path charged %d bytes, empty-list path charged %d", nilBytes, listBytes)
	})

	t.Run("conj order matches empty-list path", func(t *testing.T) {
		got, err := conj.Fn(t.Context(), nil, []core.Value{core.Nil{}, one, two, three}, child)
		require.NoError(t, err)
		want, err := conj.Fn(t.Context(), nil, []core.Value{list(), one, two, three}, child)
		require.NoError(t, err)
		require.True(t, want.Equals(got), "conj nil = %v, conj '() = %v", got, want)
		require.True(t, list(one, two, three).Equals(got), "got %v", got)
	})

	t.Run("loop conses onto nil under default limits", func(t *testing.T) {
		env := setupEnv(t)
		got := eval(t, env, `(count (loop [i 0 acc nil] (if (< i 100000) (recur (+ i 1) (cons i acc)) acc)))`)
		if want := (core.Int{V: 100000}); !want.Equals(got) {
			t.Errorf("count = %v, want %v", got, want)
		}
	})
}
