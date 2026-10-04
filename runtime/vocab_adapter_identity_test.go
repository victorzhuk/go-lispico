package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
)

// TestEngineAdapterValues_FollowTheirOwnDialect pins the adapter-identity
// contract: two dialects whose adapter maps declare the same visible name and
// the same semantic ID but different GoFunc values must each engine install
// and call its own adapter value — in either construction order. The
// fingerprint-keyed process-wide shape cache keys on the semantic ID, so a
// cached shape that carries the adapter value itself leaks the first engine's
// value into the second engine.
func TestEngineAdapterValues_FollowTheirOwnDialect(t *testing.T) {
	const id = "probe/identity@1"
	const visible = "probe"

	probeAdapter := func(marker int64) core.GoFunc {
		return core.GoFunc{
			Name: visible,
			Fn: func(_ context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
				return core.Int{V: marker}, nil
			},
		}
	}

	mkDialect := func(t *testing.T, marker int64) core.Dialect {
		t.Helper()
		d := mustDialect(t, spec{
			Adapters: map[string]core.Adapter{
				visible: {ID: id, Value: probeAdapter(marker)},
			},
		})
		return d
	}

	newProbeEngine := func(t *testing.T, d core.Dialect) Engine {
		t.Helper()
		e, err := New(nil, WithDialect(d))
		require.NoError(t, err)
		t.Cleanup(func() { e.Close() })
		require.NoError(t, e.Use(stdlib.New()))
		return e
	}

	call := func(t *testing.T, e Engine, want int64, label string) {
		t.Helper()
		got, err := e.Eval(context.Background(), "probe", "(probe)")
		require.NoError(t, err, "%s: evaluating (probe)", label)
		assert.True(t, core.Int{V: want}.Equals(got),
			"%s: (probe) = %v, want the marker %d of this engine's own dialect adapter", label, got, want)

		bound, ok := e.RootEnv().Get(visible)
		require.True(t, ok, "%s: adapter %q must be bound in the root env", label, visible)
		fn, ok := bound.(core.GoFunc)
		require.True(t, ok, "%s: %q = %T, want core.GoFunc", label, visible, bound)
		val, err := fn.Fn(context.Background(), nil, nil, e.RootEnv())
		require.NoError(t, err, "%s: calling the bound adapter value", label)
		assert.True(t, core.Int{V: want}.Equals(val),
			"%s: bound adapter value = %v, want marker %d", label, val, want)
	}

	t.Run("first engine then second", func(t *testing.T) {
		dA := mkDialect(t, 1)
		dB := mkDialect(t, 2)

		eA := newProbeEngine(t, dA)
		call(t, eA, 1, "engine A")
		eB := newProbeEngine(t, dB)
		call(t, eB, 2, "engine B")
		call(t, eA, 1, "engine A after B")
	})

	t.Run("second dialect first", func(t *testing.T) {
		dA := mkDialect(t, 1)
		dB := mkDialect(t, 2)

		eB := newProbeEngine(t, dB)
		call(t, eB, 2, "engine B first")
		eA := newProbeEngine(t, dA)
		call(t, eA, 1, "engine A second")
		call(t, eB, 2, "engine B after A")
	})
}
