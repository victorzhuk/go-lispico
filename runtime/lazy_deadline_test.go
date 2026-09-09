package runtime

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
)

func TestEngineDeadline_BytecodeLazyTimeoutStillFires(t *testing.T) {
	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()), WithTimeout(10*time.Millisecond))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	ctx := t.Context()
	_, err = eng.Eval(ctx, "def-slow", "(defn slow [] (loop [n 100000000] (if (= n 0) n (recur (- n 1)))))")
	require.NoError(t, err)
	bindBuiltin(t, eng, "=")
	bindBuiltin(t, eng, "-")

	_, err = eng.Call(context.Background(), "slow")
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded), "got %v", err)
}

func TestEngineDeadline_UnobservedBoundaryReadsNoClock(t *testing.T) {
	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	ctx := t.Context()
	_, err = eng.Eval(ctx, "def-pick", "(defn pick [a b] a)")
	require.NoError(t, err)
	fn, err := eng.Func("pick")
	require.NoError(t, err)

	var ticks atomic.Int64
	restore := nowFunc
	nowFunc = func() time.Time { return time.Unix(0, ticks.Add(1)) }
	t.Cleanup(func() { nowFunc = restore })

	_, err = eng.Call(ctx, "pick", core.Int{V: 1}, core.Int{V: 2})
	require.NoError(t, err)
	_, err = fn.Call(ctx, core.Int{V: 1}, core.Int{V: 2})
	require.NoError(t, err)
	assert.Equal(t, int64(0), ticks.Load())

	eng.OnPluginCall(func(PluginCallEvent) {})
	_, err = eng.Call(ctx, "pick", core.Int{V: 1}, core.Int{V: 2})
	require.NoError(t, err)
	assert.Greater(t, ticks.Load(), int64(0))
}

// fakeDeadlineClock pins the runtime boundary clock to a fixed instant and
// counts every read, so tests can assert both the exact deadline instant the
// boundary derives and how many clock reads it performed. The VM and core
// evaluators keep their own clocks and are unaffected.
func fakeDeadlineClock(t *testing.T) (fixed time.Time, ticks *atomic.Int64) {
	t.Helper()
	fixed = time.Now()
	var count atomic.Int64
	restore := nowFunc
	nowFunc = func() time.Time { count.Add(1); return fixed }
	t.Cleanup(func() { nowFunc = restore })
	return fixed, &count
}

// bindEvalDeadlineProbe binds a GoFunc that records the engine-owned eval
// deadline visible at its dispatch (core.EvalDeadlineFrom) — the observable
// the call boundary must keep non-zero on metered dispatches.
func bindEvalDeadlineProbe(t *testing.T, eng Engine, name string, observed *time.Time) {
	t.Helper()
	require.NoError(t, eng.Bind(name, core.GoFunc{
		Name: name,
		Fn: func(ctx context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			*observed = core.EvalDeadlineFrom(ctx)
			return core.Nil{}, nil
		},
	}))
}

func TestEngineDeadline_ContextMeterRetainsBound(t *testing.T) {
	const timeout = 250 * time.Millisecond
	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()), WithTimeout(timeout))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	var observed time.Time
	bindEvalDeadlineProbe(t, eng, "probe", &observed)
	_, err = eng.Eval(context.Background(), "def-run", "(defn run [] (probe))")
	require.NoError(t, err)

	fixed, ticks := fakeDeadlineClock(t)
	before := ticks.Load()
	_, err = eng.Call(WithMeter(context.Background(), &recordingMeter{}), "run")
	require.NoError(t, err)

	assert.True(t, observed.Equal(fixed.Add(timeout)),
		"a context meter must retain the engine bound: want %v, got %v", fixed.Add(timeout), observed)
	assert.Equal(t, int64(1), ticks.Load()-before,
		"a metered top-level call must perform exactly one boundary clock read")
}

func TestEngineDeadline_EngineMeterRetainsBound(t *testing.T) {
	const timeout = 250 * time.Millisecond
	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()), WithTimeout(timeout),
		WithEngineMeter(&recordingMeter{}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	var observed time.Time
	bindEvalDeadlineProbe(t, eng, "probe", &observed)
	_, err = eng.Eval(context.Background(), "def-run", "(defn run [] (probe))")
	require.NoError(t, err)

	fixed, _ := fakeDeadlineClock(t)
	_, err = eng.Call(context.Background(), "run")
	require.NoError(t, err)

	assert.True(t, observed.Equal(fixed.Add(timeout)),
		"an engine meter must retain the engine bound: want %v, got %v", fixed.Add(timeout), observed)
}

func TestEngineDeadline_ExistingStateWithoutDeadlineAcquiresBound(t *testing.T) {
	const timeout = 250 * time.Millisecond
	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()), WithTimeout(timeout))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	var observed time.Time
	bindEvalDeadlineProbe(t, eng, "probe", &observed)
	_, err = eng.Eval(context.Background(), "def-run", "(defn run [] (probe))")
	require.NoError(t, err)

	fixed, _ := fakeDeadlineClock(t)
	ctx := core.EnsureEvalState(context.Background())
	_, err = eng.Call(ctx, "run")
	require.NoError(t, err)

	assert.True(t, observed.Equal(fixed.Add(timeout)),
		"existing state without a deadline must acquire the engine bound: want %v, got %v", fixed.Add(timeout), observed)
}

func TestEngineDeadline_HandlesRetainEngineBound(t *testing.T) {
	const timeout = 250 * time.Millisecond
	shapes := []struct {
		name        string
		engineMeter bool
		ctx         func() context.Context
	}{
		{name: "context meter", ctx: func() context.Context {
			return WithMeter(context.Background(), &recordingMeter{})
		}},
		{name: "engine meter", engineMeter: true, ctx: func() context.Context {
			return context.Background()
		}},
		{name: "existing state without deadline", ctx: func() context.Context {
			return core.EnsureEvalState(context.Background())
		}},
	}
	entries := []struct {
		name   string
		invoke func(eng Engine, fn *Fn, ctx context.Context) error
	}{
		{name: "Engine.Call", invoke: func(eng Engine, _ *Fn, ctx context.Context) error {
			_, err := eng.Call(ctx, "run")
			return err
		}},
		{name: "Fn.Call", invoke: func(_ Engine, fn *Fn, ctx context.Context) error {
			_, err := fn.Call(ctx)
			return err
		}},
		{name: "PinnedFn.Call", invoke: func(_ Engine, fn *Fn, ctx context.Context) error {
			_, err := fn.Pin().Call(ctx)
			return err
		}},
	}
	for _, shape := range shapes {
		for _, entry := range entries {
			t.Run(shape.name+" via "+entry.name, func(t *testing.T) {
				opts := []EngineOption{WithBytecode(), WithDialect(clojure.Dialect()), WithTimeout(timeout)}
				if shape.engineMeter {
					opts = append(opts, WithEngineMeter(&recordingMeter{}))
				}
				eng, err := New(nil, opts...)
				require.NoError(t, err)
				t.Cleanup(func() { _ = eng.Close() })

				var observed time.Time
				bindEvalDeadlineProbe(t, eng, "probe", &observed)
				_, err = eng.Eval(context.Background(), "def-run", "(defn run [] (probe))")
				require.NoError(t, err)
				fn, err := eng.Func("run")
				require.NoError(t, err)

				require.NoError(t, entry.invoke(eng, fn, shape.ctx()))
				assert.False(t, observed.IsZero(),
					"%s via %s loses the engine deadline bound (observed zero)", shape.name, entry.name)
			})
		}
	}
}

func TestEngineDeadline_CooperativeGoFuncObservesEngineBound(t *testing.T) {
	const timeout = 250 * time.Millisecond
	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()), WithTimeout(timeout))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	var observed time.Time
	bindEvalDeadlineProbe(t, eng, "probe", &observed)

	var expiredObserved time.Time
	var flushErr error
	require.NoError(t, eng.Bind("probe-expired", core.GoFunc{
		Name: "probe-expired",
		Fn: func(ctx context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			expiredObserved = core.EvalDeadlineFrom(ctx)
			b := core.NewBuiltinWorkBudget(ctx)
			if err := b.Step(); err != nil {
				flushErr = err
				return core.Nil{}, err
			}
			if err := b.Flush(); err != nil {
				flushErr = err
				return core.Nil{}, err
			}
			return core.Nil{}, nil
		},
	}))
	_, err = eng.Eval(context.Background(), "def-run", "(defn run [] (probe))")
	require.NoError(t, err)
	_, err = eng.Eval(context.Background(), "def-run-expired", "(defn run-expired [] (probe-expired))")
	require.NoError(t, err)

	fixed, _ := fakeDeadlineClock(t)
	_, err = eng.Call(WithMeter(context.Background(), &recordingMeter{}), "run")
	require.NoError(t, err)
	assert.True(t, observed.Equal(fixed.Add(timeout)),
		"a cooperative GoFunc must observe the engine bound on a metered call: want %v, got %v", fixed.Add(timeout), observed)

	expired := time.Now().Add(-time.Minute)
	ctx := WithMeter(core.WithEvalDeadline(context.Background(), expired), &recordingMeter{})
	_, callErr := eng.Call(ctx, "run-expired")
	assert.True(t, expiredObserved.Equal(expired),
		"an inherited pre-expired deadline must govern exactly: want %v, got %v", expired, expiredObserved)
	assert.True(t, errors.Is(flushErr, context.DeadlineExceeded),
		"BuiltinWorkBudget flush must fire DeadlineExceeded against the expired bound, got %v", flushErr)
	assert.True(t, errors.Is(callErr, context.DeadlineExceeded),
		"the expiry error must propagate to the caller, got %v", callErr)
}

func TestEngineDeadline_CallerEarlierDeadlineGoverns(t *testing.T) {
	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()), WithTimeout(10*time.Second))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	var observed time.Time
	bindEvalDeadlineProbe(t, eng, "probe", &observed)
	_, err = eng.Eval(context.Background(), "def-run", "(defn run [] (probe))")
	require.NoError(t, err)

	callerCtx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	t.Cleanup(cancel)
	_, err = eng.Call(WithMeter(callerCtx, &recordingMeter{}), "run")
	require.NoError(t, err)

	assert.True(t, observed.IsZero(),
		"an earlier caller deadline must suppress the engine bound (its own ctx enforces it), got %v", observed)
}

func TestEngineDeadline_CallerLaterDeadlineDoesNotWeakenBound(t *testing.T) {
	const timeout = 250 * time.Millisecond
	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()), WithTimeout(timeout))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	var observed time.Time
	bindEvalDeadlineProbe(t, eng, "probe", &observed)
	_, err = eng.Eval(context.Background(), "def-run", "(defn run [] (probe))")
	require.NoError(t, err)

	fixed, _ := fakeDeadlineClock(t)
	callerCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	t.Cleanup(cancel)
	_, err = eng.Call(WithMeter(callerCtx, &recordingMeter{}), "run")
	require.NoError(t, err)

	assert.True(t, observed.Equal(fixed.Add(timeout)),
		"a later caller deadline must not suppress the engine bound: want %v, got %v", fixed.Add(timeout), observed)
}

func TestEngineDeadline_DisabledTimeoutAddsNoBound(t *testing.T) {
	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()), WithTimeout(0))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	var observed time.Time
	var flushErr error
	require.NoError(t, eng.Bind("probe", core.GoFunc{
		Name: "probe",
		Fn: func(ctx context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			observed = core.EvalDeadlineFrom(ctx)
			flushErr = core.NewBuiltinWorkBudget(ctx).Flush()
			return core.Nil{}, nil
		},
	}))
	_, err = eng.Eval(context.Background(), "def-run", "(defn run [] (probe))")
	require.NoError(t, err)

	_, err = eng.Call(WithMeter(context.Background(), &recordingMeter{}), "run")
	require.NoError(t, err)

	assert.True(t, observed.IsZero(), "WithTimeout(0) must add no engine bound, got %v", observed)
	assert.NoError(t, flushErr, "budget flush must succeed with no bound armed")
}
