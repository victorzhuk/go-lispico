package vm

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

// rearmProbe captures the evaluation state a GoFunc dispatch observes, so a
// test can assert what a rearm installed without reaching into the wrapper.
type rearmProbe struct {
	maxReductions      int64
	maxAllocationBytes int64
	deadline           time.Time
	structSeed         int64
	callSeed           int64
}

// rearmProbeFn dispatches a GoFunc that materializes its ctx's evaluation
// state and records the limits, deadline, and depth seeds it observes.
func rearmProbeFn(p *rearmProbe) core.Value {
	return core.GoFunc{
		Name: "probe",
		Fn: func(c context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			snap := core.EvalMeterFrom(c).Snapshot()
			p.maxReductions = snap.MaxReductions
			p.maxAllocationBytes = snap.MaxAllocationBytes
			p.deadline = core.EvalDeadlineFrom(c)
			p.structSeed = core.EvalStructCounter(c).Load()
			p.callSeed = core.EvalCallCounter(c).Load()
			return core.Int{V: 1}, nil
		},
	}
}

// TestVM_ReentrantRearm_ConfigChangeFullyRearmed drives the delta rearm
// through a reused wrapper (first dispatch builds it, the second — after a
// Reset, which leaves reentryCtx in place — rearms it) and changes exactly
// one configuration input between the two dispatches: each case must observe
// the new configuration exactly as a freshly built wrapper would. This is
// the end-to-end guard for the remembered-config fast path skipping a store
// it should not.
func TestVM_ReentrantRearm_ConfigChangeFullyRearmed(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(t *testing.T, v *VM)
		check   func(t *testing.T, before, after rearmProbe)
	}{
		{
			name:    "max reductions",
			prepare: func(_ *testing.T, v *VM) { v.SetResourceLimits(100, 0) },
			check: func(t *testing.T, _, after rearmProbe) {
				require.Equal(t, int64(100), after.maxReductions)
				require.Equal(t, core.DefaultMaxAllocationBytes, after.maxAllocationBytes)
			},
		},
		{
			name:    "max allocation bytes",
			prepare: func(_ *testing.T, v *VM) { v.SetResourceLimits(0, 200) },
			check: func(t *testing.T, _, after rearmProbe) {
				require.Equal(t, core.DefaultMaxReductions, after.maxReductions)
				require.Equal(t, int64(200), after.maxAllocationBytes)
			},
		},
		{
			name:    "timeout",
			prepare: func(_ *testing.T, v *VM) { v.SetTimeout(time.Hour) },
			check: func(t *testing.T, _, after rearmProbe) {
				want := core.ResolveDeadlineBound(context.Background(), time.Hour, time.Now())
				require.InDelta(t, float64(want.UnixNano()), float64(after.deadline.UnixNano()), float64(5*time.Second),
					"deadline must reflect the new timeout")
			},
		},
		{
			name: "meter attach",
			prepare: func(t *testing.T, v *VM) {
				meterCtx := core.WithEvalResourceLimits(context.Background(), 1_000_000, 2_000_000)
				v.SetEvalMeter(core.EvalMeterFrom(meterCtx))
			},
			check: func(t *testing.T, before, after rearmProbe) {
				require.Equal(t, core.DefaultMaxReductions, before.maxReductions, "baseline must start at defaults")
				require.Equal(t, int64(1_000_000), after.maxReductions)
				require.Equal(t, int64(2_000_000), after.maxAllocationBytes)
			},
		},
		{
			name: "meter detach",
			prepare: func(t *testing.T, v *VM) {
				meterCtx := core.WithEvalResourceLimits(context.Background(), 1_000_000, 2_000_000)
				v.SetEvalMeter(core.EvalMeterFrom(meterCtx))
				v.SetEvalMeter(core.EvalMeter{})
			},
			check: func(t *testing.T, _, after rearmProbe) {
				require.Equal(t, core.DefaultMaxReductions, after.maxReductions)
				require.Equal(t, core.DefaultMaxAllocationBytes, after.maxAllocationBytes)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := core.NewEnv(nil)
			v := New(env, WithEvaluator(core.NewEvaluator()))
			ctx := context.Background()

			var before, after rearmProbe
			fn := rearmProbeFn(&before)
			v.Reset()
			v.SetTimeout(time.Minute)
			_, err := v.ApplyPooled(ctx, fn, nil, env)
			require.NoError(t, err)

			fn = rearmProbeFn(&after)
			v.Reset()
			if tc.name != "timeout" {
				v.SetTimeout(time.Minute)
			}
			tc.prepare(t, v)
			_, err = v.ApplyPooled(ctx, fn, nil, env)
			require.NoError(t, err)

			tc.check(t, before, after)
		})
	}
}

// TestVM_ReentrantRearm_SameConfigFreshBudget proves two adjacent dispatches
// under identical configuration observe exactly the same limits, deadline
// posture, and fresh depth seeds — the delta rearm's observable contract is
// indistinguishable from a full rearm, and the second run gets a fresh
// budget rather than the first run's residue.
func TestVM_ReentrantRearm_SameConfigFreshBudget(t *testing.T) {
	env := core.NewEnv(nil)
	v := New(env, WithEvaluator(core.NewEvaluator()))
	ctx := context.Background()

	var first, second rearmProbe
	v.Reset()
	v.SetTimeout(time.Minute)
	_, err := v.ApplyPooled(ctx, rearmProbeFn(&first), nil, env)
	require.NoError(t, err)

	v.Reset()
	v.SetTimeout(time.Minute)
	_, err = v.ApplyPooled(ctx, rearmProbeFn(&second), nil, env)
	require.NoError(t, err)

	require.Equal(t, first.maxReductions, second.maxReductions)
	require.Equal(t, first.maxAllocationBytes, second.maxAllocationBytes)
	require.InDelta(t, float64(first.deadline.UnixNano()), float64(second.deadline.UnixNano()), float64(5*time.Second),
		"same timeout must resolve to the same deadline posture")
	require.Equal(t, first.structSeed, second.structSeed, "struct depth must re-seed to the same boundary value")
	require.Equal(t, first.callSeed, second.callSeed, "call depth must re-seed to the same boundary value")
	require.Equal(t, int64(0), second.structSeed, "top-level boundary struct seed must be zero")
	require.Equal(t, int64(1), second.callSeed, "top-level boundary call seed must be one")
}
