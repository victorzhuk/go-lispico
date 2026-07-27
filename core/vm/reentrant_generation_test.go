package vm

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

// TestVM_ReentrantCtx_StaleGenerationFailSafeAfterReset proves a reentrant
// ctx retained past its run's end (a GoFunc stashing ctx somewhere it
// outlives the call) reads back as carrying no evaluation state: never a
// later run's live counters or deadline, only its own zero-valued, private
// fallback.
func TestVM_ReentrantCtx_StaleGenerationFailSafeAfterReset(t *testing.T) {
	env := core.NewEnv(nil)
	v := New(env, WithEvaluator(core.NewEvaluator()))

	var stashed context.Context
	stashFn := core.GoFunc{
		Name: "stash",
		Fn: func(ctx context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			stashed = ctx
			return core.Nil{}, nil
		},
	}
	var liveCounter *atomic.Int64
	mutateFn := core.GoFunc{
		Name: "mutate",
		Fn: func(ctx context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			liveCounter = core.EvalStructCounter(ctx)
			liveCounter.Add(7)
			return core.Nil{}, nil
		},
	}

	_, err := v.ApplyPooled(context.Background(), stashFn, nil, env)
	require.NoError(t, err)
	require.NotNil(t, stashed)

	v.Reset()

	_, err = v.ApplyPooled(context.Background(), mutateFn, nil, env)
	require.NoError(t, err)
	require.NotNil(t, liveCounter)

	stashedCounter := core.EvalStructCounter(stashed)
	assert.NotSame(t, liveCounter, stashedCounter)
	assert.Equal(t, int64(0), stashedCounter.Load())
	assert.Equal(t, int64(7), liveCounter.Load())

	liveCounter.Add(11)
	assert.Equal(t, int64(0), stashedCounter.Load())
	assert.True(t, core.EvalDeadlineFrom(stashed).IsZero())
}

// TestVM_ReentrantCtx_StaleGenerationFreshBudgetOnReenter proves feeding a
// stale reentrant ctx back in as a later run's own outer ctx does not leak
// the run that built it: "deep" recurses through the tree-walker boundary
// with a MaxDepth ceiling that only a 2-deep recursion clears — it would
// trip immediately if it inherited "stash"'s call counter (already bumped to
// 10), so success proves the new run got a fresh, isolated budget instead.
func TestVM_ReentrantCtx_StaleGenerationFreshBudgetOnReenter(t *testing.T) {
	env := core.NewEnv(nil)
	tree := core.NewEvaluator()
	tree.MaxDepth = 3
	v := New(env, WithEvaluator(tree))

	var stashed context.Context
	stashFn := core.GoFunc{
		Name: "stash",
		Fn: func(ctx context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			stashed = ctx
			core.EvalCallCounter(ctx).Add(10)
			return core.Nil{}, nil
		},
	}

	var deep core.Value
	deep = core.GoFunc{
		Name: "deep",
		Fn: func(ctx context.Context, ev core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			n := args[0].(core.Int)
			if n.V <= 0 {
				return core.Int{V: 0}, nil
			}
			return ev.Apply(ctx, deep, []core.Value{core.Int{V: n.V - 1}}, env)
		},
	}

	_, err := v.ApplyPooled(context.Background(), stashFn, nil, env)
	require.NoError(t, err)
	require.NotNil(t, stashed)

	v.Reset()

	result, err := v.ApplyPooled(stashed, deep, []core.Value{core.Int{V: 2}}, env)
	require.NoError(t, err)
	assert.Equal(t, core.Int{V: 0}, result)
}

// TestVM_ReentrantCtx_ConcurrentStashedReadDuringRearmRaceFree is a
// regression test for a real data race: a GoFunc stashes its reentrant ctx,
// and a background goroutine reads it (EvalDeadlineFrom, EvalStructCounter)
// while the VM's own goroutine keeps dispatching with the same outer ctx,
// taking the rearm path on every dispatch after the first. Before the fix,
// RearmReentrantEvalState wrote lazyEvalStateCtx's resource-limit fields as
// plain int64s, racing with Value's read of those same fields on the
// background goroutine; only -race catches this, not the returned values.
func TestVM_ReentrantCtx_ConcurrentStashedReadDuringRearmRaceFree(t *testing.T) {
	env := core.NewEnv(nil)
	v := New(env, WithEvaluator(core.NewEvaluator()))

	var stashed context.Context
	stashFn := core.GoFunc{
		Name: "stash",
		Fn: func(ctx context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			stashed = ctx
			return core.Nil{}, nil
		},
	}
	noopFn := core.GoFunc{
		Name: "noop",
		Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
			return args[0], nil
		},
	}
	args := []core.Value{core.Int{V: 1}}
	ctx := context.Background()

	v.SetTimeout(time.Hour)
	_, err := v.ApplyPooled(ctx, stashFn, nil, env)
	require.NoError(t, err)
	require.NotNil(t, stashed)

	stop := make(chan struct{})
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = core.EvalDeadlineFrom(stashed)
			_ = core.EvalStructCounter(stashed).Load()
		}
	}()

	for range 2000 {
		v.Reset()
		v.SetTimeout(time.Hour)
		if _, err := v.ApplyPooled(ctx, noopFn, args, env); err != nil {
			close(stop)
			<-readerDone
			t.Fatal(err)
		}
	}

	close(stop)
	<-readerDone
}
