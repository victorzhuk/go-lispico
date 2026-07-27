package core

import (
	"context"
	"testing"
	"time"
)

// hostileCtx is the shape reflect.Type.Comparable answers true for but ==
// panics on: a by-value struct carrying an interface field. An embedder is
// free to hand one of these to Engine.Call, so the reuse check must reject
// it rather than compare it.
type hostileCtx struct {
	context.Context
	extra any
}

func TestCtxComparable_RejectsInterfaceCarryingStruct(t *testing.T) {
	hostile := hostileCtx{Context: context.Background(), extra: []int{1, 2, 3}}
	if ctxComparable(hostile) {
		t.Fatal("ctxComparable accepted a struct with an interface field; == on two of these panics")
	}
}

// TestCtxComparable_AcceptsStdlibContexts pins the reuse fast path: if any of
// these stops being comparable the wrapper is rebuilt every dispatch and the
// per-call allocation this change removes comes back.
func TestCtxComparable_AcceptsStdlibContexts(t *testing.T) {
	cancelCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deadlineCtx, cancelDeadline := context.WithDeadline(context.Background(), time.Now().Add(time.Hour))
	defer cancelDeadline()

	for name, ctx := range map[string]context.Context{
		"Background":   context.Background(),
		"TODO":         context.TODO(),
		"WithValue":    context.WithValue(context.Background(), evalStateKey{}, nil),
		"WithCancel":   cancelCtx,
		"WithDeadline": deadlineCtx,
	} {
		if !ctxComparable(ctx) {
			t.Errorf("ctxComparable(%s) = false, want true — reuse fast path would be lost", name)
		}
	}
}

// TestReentrantReuse_HostileCtxNeverPanics drives the reuse decision itself
// with a context whose == would panic, proving the guard holds where it
// matters rather than only in isolation.
func TestReentrantReuse_HostileCtxNeverPanics(t *testing.T) {
	hostile := hostileCtx{Context: context.Background(), extra: map[string]int{"a": 1}}

	wrapped, _, _ := AdoptReentrantEvalState(hostile, time.Hour, nil, 0, 0, EvalMeterSnapshot{}, nil)

	// A second dispatch under the same outer ctx is exactly where reuse is
	// decided; without the guard this comparison panics.
	if _, ok := RearmReentrantEvalState(wrapped, hostile, 0, 0, EvalMeterSnapshot{}, time.Hour); ok {
		t.Fatal("rearm reported reusable for a ctx that cannot be compared safely")
	}
}
