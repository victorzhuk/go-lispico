package vm

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/victorzhuk/go-lispico/core"
)

func TestVM_SetTimeoutArmsDeadlineLazily(t *testing.T) {
	base := time.Now().Add(time.Hour)
	var calls atomic.Int64
	restore := nowFunc
	nowFunc = func() time.Time {
		calls.Add(1)
		return base
	}
	t.Cleanup(func() { nowFunc = restore })

	v := New(nil)
	v.SetTimeout(time.Second)
	assert.Equal(t, int64(0), calls.Load())

	v.armDeadline(context.Background())
	assert.Equal(t, int64(1), calls.Load())
	assert.True(t, v.deadlineArmed)
	assert.Equal(t, base.Add(time.Second), v.deadline)
}

func TestVM_SetTimeoutCallerEarlierDeadlineSuppressesEngineDeadline(t *testing.T) {
	base := time.Now().Add(time.Hour)
	restore := nowFunc
	nowFunc = func() time.Time { return base }
	t.Cleanup(func() { nowFunc = restore })

	ctx, cancel := context.WithDeadline(context.Background(), base.Add(time.Millisecond))
	defer cancel()

	v := New(nil)
	v.SetTimeout(time.Second)
	v.armDeadline(ctx)

	assert.True(t, v.deadlineArmed)
	assert.True(t, v.deadline.IsZero())
}

// TestVM_ReentrantCtx_LazyDeadlineRespectsCallerTighterDeadline proves the
// arm-only-if-looser rule still holds once deadline resolution moves behind
// a GoFunc's first observation: a caller deadline tighter than the engine
// timeout must keep suppressing the engine deadline, exercised through the
// relocated lazy path instead of the direct armDeadline call above.
//
// The reentrant ctx's own deadline resolution (resolveReentrantDeadline) is
// deliberately independent of vm.deadline/vm.deadlineArmed — it must be
// callable from a goroutine other than the one that owns the VM (a GoFunc
// that stashes its ctx for a background reader), so it cannot touch VM
// fields at all. This test only checks what the reentrant ctx itself
// reports; vm.armDeadline's own arm-only-if-looser behavior is covered
// separately by TestVM_SetTimeoutCallerEarlierDeadlineSuppressesEngineDeadline
// above, unaffected by this change.
func TestVM_ReentrantCtx_LazyDeadlineRespectsCallerTighterDeadline(t *testing.T) {
	base := time.Now().Add(time.Hour)
	restore := nowFunc
	nowFunc = func() time.Time { return base }
	t.Cleanup(func() { nowFunc = restore })

	outerCtx, cancel := context.WithDeadline(context.Background(), base.Add(time.Millisecond))
	defer cancel()

	env := core.NewEnv(nil)
	v := New(env, WithEvaluator(core.NewEvaluator()))
	v.SetTimeout(time.Hour)

	var observed time.Time
	fn := core.GoFunc{
		Name: "observe",
		Fn: func(ctx context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			observed = core.EvalDeadlineFrom(ctx)
			return core.Nil{}, nil
		},
	}

	_, err := v.ApplyPooled(outerCtx, fn, nil, env)
	assert.NoError(t, err)

	assert.True(t, observed.IsZero())
}
