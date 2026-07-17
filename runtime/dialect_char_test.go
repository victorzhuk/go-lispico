package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
)

// Pinned to Clojure dialect; the default flips to Common Lisp in shard-C.
// Characterization: pins the current special-form behavior at the Engine seam
// so the dialect refactor is provably behavior-preserving.
func TestDialect_Characterization_DefaultForms(t *testing.T) {
	eval := func(t *testing.T, setup func(Engine), src string) core.Value {
		t.Helper()
		e, err := New(nil, WithDialect(clojure.Dialect()))
		require.NoError(t, err)
		defer e.Close()
		if setup != nil {
			setup(e)
		}
		result, err := e.Eval(context.Background(), "char", src)
		require.NoError(t, err)
		return result
	}

	arith := func(e Engine) {
		bindBuiltin(t, e, "+")
		bindBuiltin(t, e, "-")
		bindBuiltin(t, e, "=")
	}

	t.Run("if truthy", func(t *testing.T) {
		assert.True(t, core.Int{V: 1}.Equals(eval(t, nil, "(if true 1 2)")))
	})
	t.Run("if falsy nil", func(t *testing.T) {
		assert.True(t, core.Int{V: 2}.Equals(eval(t, nil, "(if nil 1 2)")))
	})
	t.Run("def then read", func(t *testing.T) {
		assert.True(t, core.Int{V: 10}.Equals(eval(t, nil, "(do (def x 10) x)")))
	})
	t.Run("defn and call", func(t *testing.T) {
		assert.True(t, core.Int{V: 5}.Equals(eval(t, arith, "(do (defn add [a b] (+ a b)) (add 2 3))")))
	})
	t.Run("let binds locals", func(t *testing.T) {
		assert.True(t, core.Int{V: 3}.Equals(eval(t, arith, "(let [a 1 b 2] (+ a b))")))
	})
	t.Run("quote suppresses eval", func(t *testing.T) {
		got := eval(t, nil, "(quote (1 2 3))")
		lst, ok := got.(core.List)
		require.True(t, ok, "expected List, got %T", got)
		assert.Len(t, lst.Items, 3)
	})
	t.Run("cond picks matching branch", func(t *testing.T) {
		assert.True(t, core.Int{V: 2}.Equals(eval(t, nil, "(cond false 1 true 2)")))
	})
	t.Run("loop recur accumulates", func(t *testing.T) {
		src := "(loop [i 3 acc 0] (if (= i 0) acc (recur (- i 1) (+ acc i))))"
		assert.True(t, core.Int{V: 6}.Equals(eval(t, arith, src)))
	})
	t.Run("and short-circuits to nil", func(t *testing.T) {
		assert.True(t, core.Nil{}.Equals(eval(t, nil, "(and true nil 1)")))
	})
	t.Run("or returns first truthy", func(t *testing.T) {
		assert.True(t, core.Int{V: 7}.Equals(eval(t, nil, "(or nil 7 8)")))
	})
}
