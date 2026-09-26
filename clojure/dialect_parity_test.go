package clojure_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
	"github.com/victorzhuk/go-lispico/runtime"
)

type canonicalGolden struct {
	name      string
	canonical string
	ok        bool
}

func TestClojure_StockFormTable(t *testing.T) {
	d := clojure.Dialect()

	t.Run("canonical names", func(t *testing.T) {
		kernel := []string{
			"if", "def", "defn", "defmacro", "fn", "let", "let*", "do", "quote",
			"quasiquote", "set!", "when", "cond", "loop", "recur", "try", "catch",
			"throw", "and", "or", "not",
		}
		tests := make([]canonicalGolden, 0, len(kernel)+6)
		for _, name := range kernel {
			tests = append(tests, canonicalGolden{name: name, canonical: name, ok: true})
		}
		for _, name := range []string{"defun", "setq", "progn", "funcall", "function", "no-such-form"} {
			tests = append(tests, canonicalGolden{name: name})
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				canonical, ok := d.CanonicalName(tc.name)
				assert.Equal(t, tc, canonicalGolden{name: tc.name, canonical: canonical, ok: ok})
			})
		}
	})

	t.Run("vocab", func(t *testing.T) {
		assert.Nil(t, d.Vocab(), "Vocab")
		_, ok := d.VocabEntry("first")
		assert.False(t, ok, "VocabEntry(first)")
	})

	t.Run("axes", func(t *testing.T) {
		assert.False(t, d.IsLisp2(), "IsLisp2")
		assert.False(t, d.IsBaseEmpty(), "IsBaseEmpty")
		assert.True(t, d.IsIdentity(), "IsIdentity")
		assert.NotEmpty(t, d.Fingerprint(), "Fingerprint")
		assert.Equal(t, d.Fingerprint(), clojure.Dialect().Fingerprint(), "Fingerprint must be stable")
		assert.NotEqual(t, d.Fingerprint(), core.Dialect{}.Fingerprint(), "flat cond must reach the fingerprint")
	})

	t.Run("reader", func(t *testing.T) {
		tests := []struct {
			src     string
			wantErr bool
			want    string
		}{
			{src: "[1]", want: "[1]"},
			{src: "#'f", wantErr: true},
			{src: "#(1 2)", wantErr: true},
		}
		for _, tc := range tests {
			t.Run(tc.src, func(t *testing.T) {
				forms, err := d.Read(tc.src)
				if tc.wantErr {
					require.Error(t, err)
					return
				}
				require.NoError(t, err)
				require.Len(t, forms, 1)
				_, isVector := forms[0].(core.Vector)
				assert.True(t, isVector, "%s must read as a Vector, got %T", tc.src, forms[0])
				assert.Equal(t, tc.want, forms[0].String())
			})
		}
	})

	t.Run("normalize cond", func(t *testing.T) {
		args, err := d.Read("a 1 b (do 2 3)")
		require.NoError(t, err)
		clauses, err := d.NormalizeCond(args)
		require.NoError(t, err)
		got := make([]string, len(clauses))
		for i, c := range clauses {
			got[i] = c.String()
		}
		assert.Equal(t, []string{"(a 1)", "(b (do 2 3))"}, got)

		odd, err := d.Read("a 1 b")
		require.NoError(t, err)
		_, err = d.NormalizeCond(odd)
		require.Error(t, err, "odd flat cond must be rejected")
	})
}

func TestClojure_StockCorpusBothEvaluators(t *testing.T) {
	corpus := []struct {
		name string
		src  []string
		want core.Value
	}{
		{name: "defn", src: []string{"(defn sq [x] (* x x))", "(sq 7)"}, want: core.Int{V: 49}},
		{name: "def and set!", src: []string{"(def x 1)", "(set! x 5)", "x"}, want: core.Int{V: 5}},
		{name: "do", src: []string{"(do 1 2 3)"}, want: core.Int{V: 3}},
		{name: "cond flat", src: []string{"(cond false 1 true (do 2 3))"}, want: core.Int{V: 3}},
		{name: "cond fallthrough", src: []string{"(cond false 1)"}, want: core.Nil{}},
		{name: "let", src: []string{"(let [a 1 b 2] (+ a b))"}, want: core.Int{V: 3}},
		{name: "let*", src: []string{"(let* [a 1 b (+ a 2)] b)"}, want: core.Int{V: 3}},
		{name: "loop recur", src: []string{"(loop [i 0 acc 0] (if (< i 4) (recur (+ i 1) (+ acc i)) acc))"}, want: core.Int{V: 6}},
		{name: "when", src: []string{"(when true 1 2)"}, want: core.Int{V: 2}},
		{name: "try throw", src: []string{"(try (throw :boom) (catch e e))"}, want: core.Keyword{V: "boom"}},
		{name: "defmacro", src: []string{"(defmacro twice [x] (list 'do x x))", "(twice 4)"}, want: core.Int{V: 4}},
		{name: "fn", src: []string{"((fn [x] (+ x 1)) 1)"}, want: core.Int{V: 2}},
		{name: "quasiquote", src: []string{"(def y 2)", "(quasiquote (1 (unquote y)))"}, want: core.NewList([]core.Value{core.Int{V: 1}, core.Int{V: 2}})},
		{name: "vector literal", src: []string{"[1 (+ 1 1)]"}, want: core.NewVector([]core.Value{core.Int{V: 1}, core.Int{V: 2}})},
		{name: "first rest count", src: []string{"(list (first (rest [1 2 3])) (count [1 2 3]))"}, want: core.NewList([]core.Value{core.Int{V: 2}, core.Int{V: 3}})},
		{name: "map", src: []string{"(count (map (fn [x] (* x 2)) [1 2 3]))"}, want: core.Int{V: 3}},
		{name: "and or not", src: []string{"(list (and 1 2) (or nil 3) (not nil))"}, want: core.NewList([]core.Value{core.Int{V: 2}, core.Int{V: 3}, core.Bool{V: true}})},
	}

	modes := []struct {
		name string
		opt  runtime.EngineOption
	}{
		{name: "tree-walker", opt: runtime.WithTreeWalker()},
		{name: "vm", opt: runtime.WithBytecode()},
	}

	for _, tc := range corpus {
		t.Run(tc.name, func(t *testing.T) {
			results := make([]core.Value, len(modes))
			for i, mode := range modes {
				e, err := runtime.New(nil, runtime.WithDialect(clojure.Dialect()), mode.opt)
				require.NoError(t, err)
				t.Cleanup(func() { e.Close() })
				require.NoError(t, e.Use(stdlib.New()))

				var got core.Value
				for _, src := range tc.src {
					got, err = e.Eval(context.Background(), "clojure-parity", src)
					require.NoError(t, err, "%s: %s", mode.name, src)
				}
				require.True(t, tc.want.Equals(got), "%s: got %v, want %v", mode.name, got, tc.want)
				results[i] = got
			}
			assert.True(t, results[0].Equals(results[1]), "evaluators disagree: tree-walker %v, vm %v", results[0], results[1])
		})
	}
}
