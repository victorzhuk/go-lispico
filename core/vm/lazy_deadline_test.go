package vm

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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
