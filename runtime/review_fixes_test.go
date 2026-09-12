package runtime

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/victorzhuk/go-lispico/core"
)

// TestReloadMacroInvalidatesChunkCache pins the reload contract: a reloaded
// defmacro must invalidate cached chunks whose expansions embedded the old
// definition. reloadFile merges child-scope cells into the root without
// advancing the root's macro epoch (the chunk-cache key), so cached (m) kept
// returning the stale expansion unless the merge bump is explicit.
func TestReloadMacroInvalidatesChunkCache(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	eng, err := New(slog.Default())
	require.NoError(t, err)
	defer eng.Close()

	_, err = eng.Eval(t.Context(), "seed", "(defmacro m () 1) (def reload-version 1)")
	require.NoError(t, err)

	v, err := eng.Eval(t.Context(), "cached", "(m)")
	require.NoError(t, err)
	require.Equal(t, int64(1), v.(core.Int).V, "primed cache must serve the original expansion")

	file := filepath.Join(dir, "macros.lisp")
	require.NoError(t, os.WriteFile(file, []byte("(defmacro m () 2) (def reload-version 2)"), 0o644))

	impl := eng.(*engineImpl)
	w := newFileWatcher(impl, dir, 10*time.Millisecond)
	w.ctx = context.Background()
	w.reloadFile(file)

	rv, ok := eng.RootEnv().Get("reload-version")
	require.True(t, ok, "setup: reload never published")
	require.Equal(t, int64(2), rv.(core.Int).V, "setup: reload never published")

	v, err = eng.Eval(t.Context(), "cached", "(m)")
	require.NoError(t, err)
	assert.Equal(t, int64(2), v.(core.Int).V, "cached form must re-expand after the macro reload")
	t.Run("fresh-nested-use", func(t *testing.T) {
		v, err := eng.Eval(t.Context(), "fresh", "(progn (m))")
		require.NoError(t, err)
		assert.Equal(t, int64(2), v.(core.Int).V, "nested use must serve the reloaded expansion")
	})
}

// TestInheritedDeadlinePreserved pins that EvalWithBindings/LoadScope never
// write their own engine deadline over an inherited one: the eval state is
// shared, so an unconditional arm extends (inner timeout 30s) or clears
// (inner timeout 0) the outer run's remaining budget.
func TestInheritedDeadlinePreserved(t *testing.T) {
	t.Parallel()

	type entry struct {
		name   string
		invoke func(ctx context.Context, eng Engine, src string) (core.Value, error)
	}
	entries := []entry{
		{
			name: "EvalWithBindings",
			invoke: func(ctx context.Context, eng Engine, src string) (core.Value, error) {
				return eng.EvalWithBindings(ctx, src, map[string]core.Value{"x": core.Int{V: 1}})
			},
		},
		{
			name: "LoadScope",
			invoke: func(ctx context.Context, eng Engine, src string) (core.Value, error) {
				v, _, err := eng.LoadScope(ctx, src, map[string]core.Value{"x": core.Int{V: 1}})
				return v, err
			},
		},
	}

	outer, err := New(slog.Default(), WithTimeout(5*time.Minute))
	require.NoError(t, err)
	defer outer.Close()

	for _, innerTimeout := range []time.Duration{30 * time.Second, 0} {
		for _, e := range entries {
			name := e.name + "/inner-timeout-" + innerTimeout.String()
			t.Run(name, func(t *testing.T) {
				var inner Engine
				if innerTimeout > 0 {
					inner, err = New(slog.Default(), WithTimeout(innerTimeout))
				} else {
					inner, err = New(slog.Default())
				}
				require.NoError(t, err)
				defer inner.Close()

				var ran, innerDone bool
				var before, after, innerRes time.Time
				var innerErr error

				require.NoError(t, outer.Bind("probe", core.GoFunc{
					Name: "probe",
					Fn: func(ctx context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
						ran = true
						before = core.EvalDeadlineFrom(ctx)
						v, err := e.invoke(ctx, inner, "1")
						after = core.EvalDeadlineFrom(ctx)
						innerRes = core.EvalDeadlineFrom(ctx)
						innerDone, innerErr = v != nil, err
						return core.Int{V: 7}, nil
					},
				}))

				_, err := outer.Eval(t.Context(), "outer", "(probe)")
				require.NoError(t, err)

				// Preconditions first: only a proven inner success with a
				// recorded inherited bound can convict drift.
				require.True(t, ran, "setup: host callback never executed")
				require.NoError(t, innerErr, "setup: inner evaluation failed")
				require.True(t, innerDone, "setup: inner returned no value")
				require.False(t, before.IsZero(), "setup: no non-zero inherited deadline was armed")

				assert.Equal(t, before, after, "%s: inner call mutated the inherited deadline", name)
				assert.Equal(t, before, innerRes, "%s: deadline differs after inner settle", name)
			})
		}
	}
}

// TestOnPluginCallPanicIsContained pins that a panicking OnPluginCall
// observer never reaches the caller and never changes the call's own
// outcome, on both the undefined-name tail and a successful call.
func TestOnPluginCallPanicIsContained(t *testing.T) {
	cases := []struct {
		name    string
		def     string
		invoke  func(Engine) (core.Value, error)
		want    core.Value
		wantErr string
	}{
		{
			name:    "undefined",
			invoke:  func(e Engine) (core.Value, error) { return e.Call(t.Context(), "no-such-fn") },
			wantErr: "undefined function: no-such-fn",
		},
		{
			name: "success",
			def:  "(defn f () 42)",
			invoke: func(e Engine) (core.Value, error) {
				return e.Call(t.Context(), "f")
			},
			want: core.Int{V: 42},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eng, err := New(slog.Default())
			require.NoError(t, err)
			defer eng.Close()
			if tc.def != "" {
				_, err := eng.Eval(t.Context(), "def", tc.def)
				require.NoError(t, err)
			}
			eng.OnPluginCall(func(PluginCallEvent) { panic("observer failure") })

			var result core.Value
			var callErr error
			escaped := catchPanic(func() {
				result, callErr = tc.invoke(eng)
			})
			require.Nil(t, escaped, "observer panic escaped Engine.Call")
			if tc.wantErr != "" {
				require.EqualError(t, callErr, tc.wantErr)
				assert.Nil(t, result)
				return
			}
			require.NoError(t, callErr)
			assert.Equal(t, tc.want, result)
		})
	}
}

// TestPluginCallStatsBounded proves the per-name call counters stop growing
// past the bound even when a caller spams distinct undefined names, the only
// unbounded key source.
func TestPluginCallStatsBounded(t *testing.T) {
	t.Parallel()

	eng, err := New(slog.Default())
	require.NoError(t, err)
	defer eng.Close()

	const n = maxCallCacheEntries*2 + 10
	for i := range n {
		_, err := eng.Call(t.Context(), "spam-name") // keeps one counter hot
		require.Error(t, err)
		_, err = eng.Call(t.Context(), "other-name")
		require.Error(t, err)
		_ = i
	}
	// Distinct names are the growth vector; the two above would cap at 2.
	for i := range n {
		_, err := eng.Call(t.Context(), uniqueCallName(i))
		require.Error(t, err)
	}

	snap := eng.Stats()
	assert.LessOrEqual(t, len(snap.PluginCallCounts), maxCallCacheEntries,
		"plugin call counters must flush at the bound")
}

func uniqueCallName(i int) string {
	return "f" + string(rune('a'+i%26)) + string(rune('a'+(i/26)%26)) + string(rune('a'+(i/676)%26)) + "!"
}
