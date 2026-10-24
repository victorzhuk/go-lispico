package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
)

// TestQuasiquotedMap_FallbackValuesAndEffects pins the CodeUnsupported rule
// for non-foldable quasiquoted maps: the bytecode compiler refuses the map,
// the tree-walker fallback evaluates it, unquoted positions run exactly once,
// and the produced map holds the evaluated keys and values.
func TestQuasiquotedMap_FallbackValuesAndEffects(t *testing.T) {
	t.Parallel()

	newEngine := func(t *testing.T, bytecode bool) Engine {
		t.Helper()
		opts := []EngineOption{WithDialect(clojure.Dialect())}
		if bytecode {
			opts = append(opts, WithBytecode())
		}
		eng, err := New(nil, opts...)
		require.NoError(t, err)
		t.Cleanup(func() { _ = eng.Close() })
		return eng
	}

	for _, mode := range []struct {
		name     string
		bytecode bool
	}{{"tree-walker", false}, {"bytecode-fallback", true}} {
		t.Run(mode.name, func(t *testing.T) {
			eng := newEngine(t, mode.bytecode)

			notes := 0
			require.NoError(t, eng.Bind("note", core.GoFunc{
				Name: "note",
				Fn: func(_ context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
					notes++
					return core.Keyword{V: "k"}, nil
				},
			}))

			_, err := eng.Eval(t.Context(), "defs", "(def v :val)")
			require.NoError(t, err)

			got, err := eng.Eval(t.Context(), "qq-map", "`{:key ~v :eff ~(note)}")
			require.NoError(t, err, "non-foldable quasiquoted map must fall back, not fail")
			assert.Equal(t, 1, notes, "unquoted position runs exactly once")

			m, ok := got.(*core.HashMap)
			require.True(t, ok, "result is a map, got %T", got)
			v, ok := m.Get(core.Keyword{V: "key"})
			require.True(t, ok, "literal key present")
			assert.Equal(t, core.Keyword{V: "val"}, v)
			eff, ok := m.Get(core.Keyword{V: "eff"})
			require.True(t, ok, "effect key present")
			assert.Equal(t, core.Keyword{V: "k"}, eff)

			// A second evaluation of the same source runs the effect once more.
			_, err = eng.Eval(t.Context(), "qq-map", "`{:key ~v :eff ~(note)}")
			require.NoError(t, err)
			assert.Equal(t, 2, notes, "each evaluation runs the unquoted effect once")
		})
	}
}
