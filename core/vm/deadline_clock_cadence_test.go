package vm

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestVM_PollCancel_ClockReadCadence proves pollCancel reads the wall clock
// only every deadlineClockCadence-th call once a deadline is armed, not on
// every call. Modeled on TestVM_SetTimeoutArmsDeadlineLazily. n values at and
// just past cadence boundaries (8, 9, 16, 17) discriminate a reset-to-K vs.
// the correct reset-to-K-1 off-by-one, which n=37 alone cannot: both give the
// same ceil(37/8) count either way.
func TestVM_PollCancel_ClockReadCadence(t *testing.T) {
	for _, n := range []int{1, 8, 9, 16, 17, 37} {
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			base := time.Now()
			var calls int
			restore := nowFunc
			nowFunc = func() time.Time {
				calls++
				return base
			}
			t.Cleanup(func() { nowFunc = restore })

			v := New(nil)
			v.SetDeadline(base.Add(time.Hour))
			ctx := context.Background()

			for range n {
				if err := v.pollCancel(ctx); err != nil {
					t.Fatalf("pollCancel returned error: %v", err)
				}
			}

			want := (n + deadlineClockCadence - 1) / deadlineClockCadence
			assert.Equal(t, want, calls, "nowFunc call count for %d polls at cadence %d", n, deadlineClockCadence)
		})
	}
}

// TestVM_DeadlineCrossing_BoundedPollsAfterExpiry proves a deadline crossing
// is detected within deadlineClockCadence polls of the true expiry, not
// immediately — the bound the "Deadline crossing terminates within the
// documented bound" spec scenario requires.
func TestVM_DeadlineCrossing_BoundedPollsAfterExpiry(t *testing.T) {
	base := time.Now()
	deadline := base.Add(time.Hour)
	crossAt := deadlineClockCadence + 3

	restore := nowFunc
	var pollIdx int
	nowFunc = func() time.Time {
		if pollIdx >= crossAt {
			return deadline.Add(time.Millisecond)
		}
		return base
	}
	t.Cleanup(func() { nowFunc = restore })

	v := New(nil)
	v.SetDeadline(deadline)
	ctx := context.Background()

	var err error
	i := 0
	for i = 1; i <= crossAt+deadlineClockCadence; i++ {
		pollIdx = i
		err = v.pollCancel(ctx)
		if err != nil {
			break
		}
	}

	require.Error(t, err, "deadline crossing at poll %d never detected", crossAt)
	assert.True(t, errors.Is(err, context.DeadlineExceeded))
	assert.LessOrEqual(t, i-crossAt, deadlineClockCadence,
		"crossing at poll %d detected at poll %d, want within %d polls", crossAt, i, deadlineClockCadence)
}

// TestVM_ArmedDeadline_CtxCancellationEveryCheckpoint proves ctx cancellation
// is observed on the very next poll even mid-cadence (deadlineClockPolls != 0,
// i.e. the poll that would otherwise skip its clock read) — the clock-read
// cadence gate must never delay the ctx.Err() check, which stays
// unconditional every poll. Deterministic (no wall-clock timing): drives one
// poll to land mid-cadence, then asserts cancellation on the following poll.
func TestVM_ArmedDeadline_CtxCancellationEveryCheckpoint(t *testing.T) {
	base := time.Now()
	restore := nowFunc
	nowFunc = func() time.Time { return base }
	t.Cleanup(func() { nowFunc = restore })

	v := New(nil)
	v.SetDeadline(base.Add(time.Hour)) // far future; deadlineClockPolls starts at 0 ("due now")
	ctx, cancel := context.WithCancel(context.Background())

	require.NoError(t, v.pollCancel(ctx))
	require.NotZero(t, v.deadlineClockPolls,
		"expected the first poll to consume the due-now phase and land mid-cadence")

	cancel()
	err := v.pollCancel(ctx)
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled),
		"ctx cancellation must be observed on the very next poll regardless of the clock-read cadence phase")
}

// TestVM_PooledReuse_CadenceCounterResetsPerRun proves Reset zeroes the
// cadence counter: a pooled VM run across two Reset-separated segments reads
// the clock exactly as often as two fresh VMs each run once, proving the
// counter's phase never survives a reset.
func TestVM_PooledReuse_CadenceCounterResetsPerRun(t *testing.T) {
	base := time.Now()
	restore := nowFunc
	t.Cleanup(func() { nowFunc = restore })

	freshRunCalls := func(n int) int {
		var calls int
		nowFunc = func() time.Time {
			calls++
			return base
		}
		v := New(nil)
		v.SetDeadline(base.Add(time.Hour))
		ctx := context.Background()
		for range n {
			if err := v.pollCancel(ctx); err != nil {
				t.Fatalf("unexpected pollCancel error: %v", err)
			}
		}
		return calls
	}

	const seg1, seg2 = 5, 11

	var pooledCalls int
	nowFunc = func() time.Time {
		pooledCalls++
		return base
	}
	pooled := New(nil)
	pooled.SetDeadline(base.Add(time.Hour))
	ctx := context.Background()
	for range seg1 {
		if err := pooled.pollCancel(ctx); err != nil {
			t.Fatalf("unexpected pollCancel error: %v", err)
		}
	}
	pooled.Reset()
	pooled.SetDeadline(base.Add(time.Hour))
	for range seg2 {
		if err := pooled.pollCancel(ctx); err != nil {
			t.Fatalf("unexpected pollCancel error: %v", err)
		}
	}

	want := freshRunCalls(seg1) + freshRunCalls(seg2)
	assert.Equal(t, want, pooledCalls,
		"pooled Reset-separated runs of %d and %d polls should read the clock as often as two fresh VMs", seg1, seg2)
}
