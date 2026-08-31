package vm

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/victorzhuk/go-lispico/core"
)

// TestVM_ApplyPooled_ReentrantCtxReuseOneOuterCtx proves a GoFunc-dispatching
// ApplyPooled call reuses its reentrant ctx wrapper in place across separate
// top-level runs when the outer ctx is unchanged — the per-call wrapper
// allocation this change removes. Styled after
// TestVM_ApplyPooled_NoWrapperChunkAllocs (apply_test.go).
func TestVM_ApplyPooled_ReentrantCtxReuseOneOuterCtx(t *testing.T) {
	if raceEnabled {
		t.Skip("alloc counts are unreliable under the race detector")
	}

	env := core.NewEnv(nil)
	v := New(env, WithEvaluator(core.NewEvaluator()))

	// fn is declared as core.Value (not core.GoFunc) so passing it to
	// ApplyPooled never re-boxes it into the interface parameter — that
	// boxing, not reentrantCtx, is what a wrongly-typed fn would allocate on
	// every call, masking the measurement this test exists to make.
	var fn core.Value = core.GoFunc{
		Name: "noop",
		Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
			return args[0], nil
		},
	}
	args := []core.Value{core.Int{V: 1}}
	ctx := context.Background()

	// Warm up: the first dispatch always builds the wrapper.
	v.Reset()
	v.SetTimeout(time.Second)
	if _, err := v.ApplyPooled(ctx, fn, args, env); err != nil {
		t.Fatal(err)
	}

	allocs := testing.AllocsPerRun(200, func() {
		v.Reset()
		v.SetTimeout(time.Second)
		result, err := v.ApplyPooled(ctx, fn, args, env)
		if err != nil {
			panic(err)
		}
		testSink = result
	})

	t.Logf("ApplyPooled reentrant reuse AllocsPerRun: %.1f", allocs)
	if allocs > 0 {
		t.Fatalf("ApplyPooled with one outer ctx allocates %.1f/run, want 0 (reentrant ctx reused in place)", allocs)
	}
}

// TestVM_ApplyPooled_ReentrantCtxReuseStdlibContextVariants extends
// TestVM_ApplyPooled_ReentrantCtxReuseOneOuterCtx to every stdlib context
// constructor the reuse fast path must keep hitting: comparableKind's
// stricter-than-reflect.Comparable check (see core/eval.go's ctxComparable)
// must not silently disable reuse for any of them, or the allocation win
// this change exists for evaporates for ordinary callers.
func TestVM_ApplyPooled_ReentrantCtxReuseStdlibContextVariants(t *testing.T) {
	if raceEnabled {
		t.Skip("alloc counts are unreliable under the race detector")
	}

	cases := []struct {
		name string
		ctx  func() (context.Context, func())
	}{
		{"Background", func() (context.Context, func()) { return context.Background(), func() {} }},
		{"TODO", func() (context.Context, func()) { return context.TODO(), func() {} }},
		{"WithValue", func() (context.Context, func()) {
			return context.WithValue(context.Background(), ctxKeyType{}, 1), func() {}
		}},
		{"WithCancel", func() (context.Context, func()) { return context.WithCancel(context.Background()) }},
		{"WithTimeout", func() (context.Context, func()) {
			return context.WithTimeout(context.Background(), time.Hour)
		}},
		{"WithDeadline", func() (context.Context, func()) {
			return context.WithDeadline(context.Background(), time.Now().Add(time.Hour))
		}},
	}

	var fn core.Value = core.GoFunc{
		Name: "noop",
		Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
			return args[0], nil
		},
	}
	args := []core.Value{core.Int{V: 1}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := tc.ctx()
			defer cancel()

			env := core.NewEnv(nil)
			v := New(env, WithEvaluator(core.NewEvaluator()))

			v.Reset()
			v.SetTimeout(time.Second)
			if _, err := v.ApplyPooled(ctx, fn, args, env); err != nil {
				t.Fatal(err)
			}

			allocs := testing.AllocsPerRun(50, func() {
				v.Reset()
				v.SetTimeout(time.Second)
				result, err := v.ApplyPooled(ctx, fn, args, env)
				if err != nil {
					panic(err)
				}
				testSink = result
			})

			t.Logf("%s AllocsPerRun: %.1f", tc.name, allocs)
			if allocs > 0 {
				t.Fatalf("%s allocates %.1f/run, want 0 (reentrant ctx reused in place)", tc.name, allocs)
			}
		})
	}
}

type ctxKeyType struct{}

// TestVM_ApplyPooled_ReentrantCtxFreshPerDistinctOuterCtx is the companion to
// TestVM_ApplyPooled_ReentrantCtxReuseOneOuterCtx: a distinct outer ctx per
// call must force a fresh wrapper, proving reuse triggers only on a matching
// outer ctx, not unconditionally.
func TestVM_ApplyPooled_ReentrantCtxFreshPerDistinctOuterCtx(t *testing.T) {
	if raceEnabled {
		t.Skip("alloc counts are unreliable under the race detector")
	}

	env := core.NewEnv(nil)
	v := New(env, WithEvaluator(core.NewEvaluator()))

	var fn core.Value = core.GoFunc{
		Name: "noop",
		Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
			return args[0], nil
		},
	}
	args := []core.Value{core.Int{V: 1}}
	type ctxKey struct{}

	// AllocsPerRun below runs the closure n+1 times (one warm-up plus n
	// measured), on top of the explicit warm-up call at index 0, so this
	// needs n+2 distinct contexts.
	const n = 200
	ctxs := make([]context.Context, n+2)
	for i := range ctxs {
		ctxs[i] = context.WithValue(context.Background(), ctxKey{}, i)
	}

	v.Reset()
	v.SetTimeout(time.Second)
	if _, err := v.ApplyPooled(ctxs[0], fn, args, env); err != nil {
		t.Fatal(err)
	}

	i := 0
	allocs := testing.AllocsPerRun(n, func() {
		i++
		v.Reset()
		v.SetTimeout(time.Second)
		result, err := v.ApplyPooled(ctxs[i], fn, args, env)
		if err != nil {
			panic(err)
		}
		testSink = result
	})

	t.Logf("ApplyPooled distinct outer ctx AllocsPerRun: %.1f", allocs)
	if allocs < 1 {
		t.Fatalf("ApplyPooled with a distinct outer ctx each call allocates %.1f/run, want >= 1 (fresh reentrant ctx built, proving reuse is not unconditional)", allocs)
	}
}

// TestVM_ReentrantCtx_DeadlineResolvedOnceAtFirstDispatch proves a run's
// absolute deadline is resolved exactly once, at the first GoFunc dispatch
// (reentrantCtx arms it before installing), and every observation in that
// run — however late — reads the same instant instead of deriving a fresh
// now+timeout bound.
func TestVM_ReentrantCtx_DeadlineResolvedOnceAtFirstDispatch(t *testing.T) {
	var calls atomic.Int64
	restore := nowFunc
	nowFunc = func() time.Time {
		calls.Add(1)
		return time.Now()
	}
	t.Cleanup(func() { nowFunc = restore })

	env := core.NewEnv(nil)
	v := New(env, WithEvaluator(core.NewEvaluator()))

	var observed []time.Time
	fn := core.GoFunc{
		Name: "probe",
		Fn: func(ctx context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
			observed = append(observed, core.EvalDeadlineFrom(ctx))
			return args[0], nil
		},
	}
	args := []core.Value{core.Int{V: 1}}
	ctx := context.Background()

	for range 5 {
		v.Reset()
		v.SetTimeout(time.Second)
		if _, err := v.ApplyPooled(ctx, fn, args, env); err != nil {
			t.Fatal(err)
		}
	}

	if got := calls.Load(); got != 5 {
		t.Fatalf("nowFunc called %d times, want 5 (one armDeadline read per run's first dispatch)", got)
	}
	for i, d := range observed {
		if d.IsZero() {
			t.Fatalf("dispatch %d observed a zero deadline", i)
		}
	}
	for i := 1; i < len(observed); i++ {
		if observed[i].Equal(observed[i-1]) {
			t.Fatalf("runs %d and %d observed the same deadline %v — each run must arm its own instant", i-1, i, observed[i])
		}
	}
}
