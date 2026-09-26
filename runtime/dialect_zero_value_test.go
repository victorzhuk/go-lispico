package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
)

func TestDialect_ZeroValueEngineIsIdentity(t *testing.T) {
	var d core.Dialect
	require.True(t, d.IsIdentity(), "zero Dialect must be the identity")
	require.False(t, d.IsLisp2(), "zero Dialect must be Lisp-1")
	require.False(t, d.IsBaseEmpty(), "zero Dialect must start from the full kernel")
	require.Nil(t, d.Vocab(), "zero Dialect must have no vocabulary")

	forms := []struct {
		name string
		src  []string
		want core.Value
	}{
		{name: "if", src: []string{"(if false 1 2)"}, want: core.Int{V: 2}},
		{name: "def", src: []string{"(def x 5)", "x"}, want: core.Int{V: 5}},
		{name: "defn", src: []string{"(defn id [x] x)", "(id 3)"}, want: core.Int{V: 3}},
		{name: "defmacro", src: []string{"(defmacro twice [x] (list 'do x x))", "(twice 4)"}, want: core.Int{V: 4}},
		{name: "fn", src: []string{"((fn [x] x) 6)"}, want: core.Int{V: 6}},
		{name: "let", src: []string{"(let [a 1] a)"}, want: core.Int{V: 1}},
		{name: "let*", src: []string{"(let* [a 1 b a] b)"}, want: core.Int{V: 1}},
		{name: "do", src: []string{"(do 1 2)"}, want: core.Int{V: 2}},
		{name: "quote", src: []string{"(quote a)"}, want: core.Symbol{V: "a"}},
		{name: "quasiquote", src: []string{"(def y 2)", "(quasiquote (1 (unquote y)))"}, want: core.NewList([]core.Value{core.Int{V: 1}, core.Int{V: 2}})},
		{name: "set!", src: []string{"(def z 1)", "(set! z 2)", "z"}, want: core.Int{V: 2}},
		{name: "when", src: []string{"(when true 7)"}, want: core.Int{V: 7}},
		{name: "cond", src: []string{"(cond (false 1) (true 2 3))"}, want: core.Int{V: 3}},
		{name: "loop recur", src: []string{"(loop [i 0] (if (= i 3) i (recur (+ i 1))))"}, want: core.Int{V: 3}},
		{name: "try catch throw", src: []string{"(try (throw :boom) (catch e e))"}, want: core.Keyword{V: "boom"}},
		{name: "and", src: []string{"(and 1 2)"}, want: core.Int{V: 2}},
		{name: "or", src: []string{"(or nil 3)"}, want: core.Int{V: 3}},
		{name: "not", src: []string{"(not nil)"}, want: core.Bool{V: true}},
		{name: "first", src: []string{"(first (quote (1 2)))"}, want: core.Int{V: 1}},
	}

	modes := []struct {
		name string
		opt  EngineOption
	}{
		{name: "tree-walker", opt: WithTreeWalker()},
		{name: "vm", opt: WithBytecode()},
	}

	for _, tc := range forms {
		t.Run(tc.name, func(t *testing.T) {
			results := make([]core.Value, len(modes))
			for i, mode := range modes {
				e, err := New(nil, WithDialect(core.Dialect{}), mode.opt)
				require.NoError(t, err)
				t.Cleanup(func() { _ = e.Close() })
				require.NoError(t, e.Use(stdlib.New()))

				var got core.Value
				for _, src := range tc.src {
					got, err = e.Eval(context.Background(), "zero-dialect", src)
					require.NoError(t, err, "%s: %s", mode.name, src)
				}
				require.True(t, tc.want.Equals(got), "%s: got %v, want %v", mode.name, got, tc.want)
				results[i] = got
			}
			assert.True(t, results[0].Equals(results[1]), "evaluators disagree: tree-walker %v, vm %v", results[0], results[1])
		})
	}

	for _, name := range []string{
		"if", "def", "defn", "defmacro", "fn", "let", "let*", "do", "quote",
		"quasiquote", "set!", "when", "cond", "loop", "recur", "try", "catch",
		"throw", "and", "or", "not",
	} {
		canonical, ok := d.CanonicalName(name)
		assert.True(t, ok && canonical == name, "CanonicalName(%q) = (%q, %v)", name, canonical, ok)
	}
	for _, name := range []string{"defun", "setq", "progn", "funcall", "function"} {
		_, ok := d.CanonicalName(name)
		assert.False(t, ok, "CanonicalName(%q) must be unknown", name)
	}
}
