package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
)

// callOutcome is one Call's observable result: the value and the exact error
// string. The parity matrix diffs these — plus per-name Stats() counts —
// between a fast-condition engine and a callback-registered engine; the lean
// and general boundaries must be indistinguishable to a caller.
type callOutcome struct {
	value core.Value
	err   string
}

func recordCall(eng Engine, ctx context.Context, name string, args ...core.Value) callOutcome {
	v, err := eng.Call(ctx, name, args...)
	out := callOutcome{value: v}
	if err != nil {
		out.err = err.Error()
	}
	return out
}

// callTarget is one handle invocation the parity matrix diffs: a Fn.Call and
// a PinnedFn.Call of the same binding, run on both engine conditions.
type callTarget struct {
	fn     *Fn
	pinned *PinnedFn
	args   []core.Value
}

func recordTargetCall(t *testing.T, ct callTarget, ctx context.Context) []callOutcome {
	t.Helper()
	out := make([]callOutcome, 0, 2)
	for _, call := range []func(context.Context, ...core.Value) (core.Value, error){ct.fn.Call, ct.pinned.Call} {
		v, err := call(ctx, ct.args...)
		o := callOutcome{value: v}
		if err != nil {
			o.err = err.Error()
		}
		out = append(out, o)
	}
	return out
}

func TestCallBoundary_FastAndGeneralPathsIdentical(t *testing.T) {
	one := core.Int{V: 1}
	two := core.Int{V: 2}

	cases := []struct {
		name string
		run  func(t *testing.T, eng Engine, ctx context.Context) []callOutcome
	}{
		{
			name: "defined GoFunc-free body",
			run: func(t *testing.T, eng Engine, ctx context.Context) []callOutcome {
				_, err := eng.Eval(ctx, "def-pick", "(defun pick (a b) a)")
				require.NoError(t, err)
				return []callOutcome{
					recordCall(eng, ctx, "pick", one, two),
					recordCall(eng, ctx, "pick", one, two),
				}
			},
		},
		{
			name: "canonical-op body with stdlib",
			run: func(t *testing.T, eng Engine, ctx context.Context) []callOutcome {
				require.NoError(t, eng.Use(stdlib.New()))
				_, err := eng.Eval(ctx, "def-add", "(defun add (a b) (+ a b))")
				require.NoError(t, err)
				return []callOutcome{
					recordCall(eng, ctx, "add", one, two),
					recordCall(eng, ctx, "add", one, two),
				}
			},
		},
		{
			name: "arity mismatch",
			run: func(t *testing.T, eng Engine, ctx context.Context) []callOutcome {
				require.NoError(t, eng.Use(stdlib.New()))
				_, err := eng.Eval(ctx, "def-add2", "(defun add2 (a b) (+ a b))")
				require.NoError(t, err)
				return []callOutcome{recordCall(eng, ctx, "add2", one)}
			},
		},
		{
			name: "undefined name",
			run: func(t *testing.T, eng Engine, ctx context.Context) []callOutcome {
				return []callOutcome{
					recordCall(eng, ctx, "missing"),
					recordCall(eng, ctx, "missing"),
				}
			},
		},
		{
			name: "lisp2 value-cell-only fallback is never cached",
			run: func(t *testing.T, eng Engine, ctx context.Context) []callOutcome {
				_, err := eng.Eval(ctx, "def-f-value", "(def f (fn () :value-cell))")
				require.NoError(t, err)
				out := []callOutcome{
					recordCall(eng, ctx, "f"),
					recordCall(eng, ctx, "f"),
				}
				impl := eng.(*engineImpl)
				assert.Nil(t, impl.callCache.lookup("f", impl.rootEnv),
					"a Lisp-2 value-cell fallback must never be cached")
				return out
			},
		},
		{
			name: "tombstone mid-sequence via Delete",
			run: func(t *testing.T, eng Engine, ctx context.Context) []callOutcome {
				impl := eng.(*engineImpl)
				_, err := eng.Eval(ctx, "def-g", "(defun g () :first)")
				require.NoError(t, err)
				first := recordCall(eng, ctx, "g") // warms the cache
				impl.rootEnv.Delete("g")
				deleted := recordCall(eng, ctx, "g")
				impl.rootEnv.Rebuild()
				_, err = eng.Eval(ctx, "redef-g", "(defun g () :second)")
				require.NoError(t, err)
				second := recordCall(eng, ctx, "g")
				return []callOutcome{first, deleted, second}
			},
		},
		{
			name: "panic-in-body GoFunc is recovered, engine stays usable",
			run: func(t *testing.T, eng Engine, ctx context.Context) []callOutcome {
				_, err := eng.Eval(ctx, "def-pick2", "(defun pick (a b) a)")
				require.NoError(t, err)
				require.NoError(t, eng.Bind("boom", core.GoFunc{
					Name: "boom",
					Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
						panic("kaboom")
					},
				}))
				return []callOutcome{
					recordCall(eng, ctx, "boom"),
					recordCall(eng, ctx, "pick", one, two),
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fast, err := New(nil, WithBytecode())
			require.NoError(t, err)
			t.Cleanup(func() { _ = fast.Close() })

			general, err := New(nil, WithBytecode())
			require.NoError(t, err)
			t.Cleanup(func() { _ = general.Close() })
			general.OnPluginCall(func(PluginCallEvent) {})
			require.False(t, general.(*engineImpl).fastPath.Load())

			ctx := t.Context()
			fastOut := tc.run(t, fast, ctx)
			generalOut := tc.run(t, general, ctx)

			require.Equal(t, fastOut, generalOut, "lean and general paths must return identical outcomes")
			assert.Equal(t, fast.Stats().PluginCallCounts, general.Stats().PluginCallCounts,
				"per-name stats must match across paths")
		})
	}
}

// TestCallBoundary_HandlesFastAndGeneralPathsIdentical runs the same handle
// call matrix — every case through Fn.Call and PinnedFn.Call — on a
// fast-condition engine and a callback-registered engine, diffing outcomes
// and per-name stats: the lean handle spines must be indistinguishable from
// the general ones.
func TestCallBoundary_HandlesFastAndGeneralPathsIdentical(t *testing.T) {
	one := core.Int{V: 1}
	two := core.Int{V: 2}

	cases := []struct {
		name string
		run  func(t *testing.T, eng Engine, ctx context.Context) [][]callOutcome
	}{
		{
			name: "defined GoFunc-free body",
			run: func(t *testing.T, eng Engine, ctx context.Context) [][]callOutcome {
				_, err := eng.Eval(ctx, "def-pick", "(defun pick (a b) a)")
				require.NoError(t, err)
				fn, err := eng.Func("pick")
				require.NoError(t, err)
				pinned := fn.Pin()
				require.NotNil(t, pinned)
				ct := callTarget{fn: fn, pinned: pinned, args: []core.Value{one, two}}
				return [][]callOutcome{
					recordTargetCall(t, ct, ctx),
					recordTargetCall(t, ct, ctx),
				}
			},
		},
		{
			name: "canonical-op body with stdlib",
			run: func(t *testing.T, eng Engine, ctx context.Context) [][]callOutcome {
				require.NoError(t, eng.Use(stdlib.New()))
				_, err := eng.Eval(ctx, "def-add", "(defun add (a b) (+ a b))")
				require.NoError(t, err)
				fn, err := eng.Func("add")
				require.NoError(t, err)
				pinned := fn.Pin()
				require.NotNil(t, pinned)
				ct := callTarget{fn: fn, pinned: pinned, args: []core.Value{one, two}}
				return [][]callOutcome{
					recordTargetCall(t, ct, ctx),
					recordTargetCall(t, ct, ctx),
				}
			},
		},
		{
			name: "arity mismatch",
			run: func(t *testing.T, eng Engine, ctx context.Context) [][]callOutcome {
				require.NoError(t, eng.Use(stdlib.New()))
				_, err := eng.Eval(ctx, "def-add2", "(defun add2 (a b) (+ a b))")
				require.NoError(t, err)
				fn, err := eng.Func("add2")
				require.NoError(t, err)
				pinned := fn.Pin()
				require.NotNil(t, pinned)
				return [][]callOutcome{recordTargetCall(t, callTarget{fn: fn, pinned: pinned, args: []core.Value{one}}, ctx)}
			},
		},
		{
			name: "tombstone mid-sequence via Delete",
			run: func(t *testing.T, eng Engine, ctx context.Context) [][]callOutcome {
				impl := eng.(*engineImpl)
				_, err := eng.Eval(ctx, "def-g", "(defun g () :first)")
				require.NoError(t, err)
				fn, err := eng.Func("g")
				require.NoError(t, err)
				pinned := fn.Pin()
				require.NotNil(t, pinned)
				ct := callTarget{fn: fn, pinned: pinned}
				first := recordTargetCall(t, ct, ctx) // warms both handle paths
				impl.rootEnv.Delete("g")
				deleted := recordTargetCall(t, ct, ctx)
				impl.rootEnv.Rebuild()
				_, err = eng.Eval(ctx, "redef-g", "(defun g () :second)")
				require.NoError(t, err)
				second := recordTargetCall(t, ct, ctx)
				return [][]callOutcome{first, deleted, second}
			},
		},
		{
			name: "panic-in-body GoFunc is recovered, handle stays usable",
			run: func(t *testing.T, eng Engine, ctx context.Context) [][]callOutcome {
				_, err := eng.Eval(ctx, "def-pick2", "(defun pick (a b) a)")
				require.NoError(t, err)
				require.NoError(t, eng.Bind("boom", core.GoFunc{
					Name: "boom",
					Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
						panic("kaboom")
					},
				}))
				boom, err := eng.Func("boom")
				require.NoError(t, err)
				boomPinned := boom.Pin()
				require.NotNil(t, boomPinned)
				pick, err := eng.Func("pick")
				require.NoError(t, err)
				pickPinned := pick.Pin()
				require.NotNil(t, pickPinned)
				return [][]callOutcome{
					recordTargetCall(t, callTarget{fn: boom, pinned: boomPinned}, ctx),
					recordTargetCall(t, callTarget{fn: pick, pinned: pickPinned, args: []core.Value{one, two}}, ctx),
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fast, err := New(nil, WithBytecode())
			require.NoError(t, err)
			t.Cleanup(func() { _ = fast.Close() })

			general, err := New(nil, WithBytecode())
			require.NoError(t, err)
			t.Cleanup(func() { _ = general.Close() })
			general.OnPluginCall(func(PluginCallEvent) {})
			require.False(t, general.(*engineImpl).fastPath.Load())

			ctx := t.Context()
			fastOut := tc.run(t, fast, ctx)
			generalOut := tc.run(t, general, ctx)

			require.Equal(t, fastOut, generalOut, "lean and general handle paths must return identical outcomes")
			assert.Equal(t, fast.Stats().PluginCallCounts, general.Stats().PluginCallCounts,
				"per-name stats must match across paths")
		})
	}
}
