package core

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestLazyEvalStateCtx_ValueNeverPublishesCrossGenerationState is a
// regression test for a value-consistency defect distinct from the data
// race core/vm's reentrant tests guard: every field lazyEvalStateCtx.Value
// reads is a proper atomic, so -race never flags this, but a rearm landing
// between the first of those reads and the eventual CompareAndSwap can still
// produce an evalState that mixes fields from two generations, or hand back
// an already-published state from a generation this call never observed.
//
// The reader goroutine is stopped deterministically (via resolveDeadline, a
// caller-supplied callback invoked mid-materialization) rather than relying
// on timing, so this reproduces the defect on every run, not occasionally:
// it reads maxReductions/maxAllocBytes for generation 0, blocks, generation
// 1's rearm installs a different ceiling while it waits, then it resumes and
// reads reductions/allocBytes — now generation 1's — before publishing.
// Fixed code must detect the generation changed underneath it and discard
// the read instead of publishing (or reusing) it.
func TestLazyEvalStateCtx_ValueNeverPublishesCrossGenerationState(t *testing.T) {
	var gen atomic.Uint64
	ctx := context.Background()

	reached := make(chan struct{})
	release := make(chan struct{})
	var resolveCalls atomic.Int64
	resolveDeadline := func(_ context.Context, _ time.Duration) time.Time {
		if resolveCalls.Add(1) == 1 {
			close(reached)
			<-release
		}
		return time.Time{}
	}

	gen0 := EvalMeterSnapshot{MaxReductions: 111, MaxAllocationBytes: 222}
	wrapped, _, _ := AdoptReentrantEvalState(ctx, time.Hour, resolveDeadline, 0, 0, gen0, &gen)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		EvalDeadlineFrom(wrapped) // enters Value, blocks inside resolveDeadline
	}()

	<-reached // reader has read gen 0's maxReductions/maxAllocBytes and is paused

	gen.Add(1)
	gen1 := EvalMeterSnapshot{MaxReductions: 999, MaxAllocationBytes: 888}
	if _, ok := RearmReentrantEvalState(wrapped, ctx, 0, 0, gen1, time.Minute); !ok {
		t.Fatal("RearmReentrantEvalState: want ok=true for the same outer ctx")
	}

	close(release) // let the paused reader finish materializing and (pre-fix) publish
	wg.Wait()

	// Whatever this generation's dispatch enforces must be exactly what
	// generation 1's rearm installed: charging up to it succeeds, charging
	// one more fails. Pre-fix, the paused reader publishes an evalState
	// ceilinged at generation 0's 111, so charging 999 wrongly fails here.
	if err := ChargeEvalReductions(wrapped, 999); err != nil {
		t.Fatalf("generation 1's own installed ceiling (999) rejected a 999-reduction charge — state is torn or stale: %v", err)
	}
	if err := ChargeEvalReductions(wrapped, 1); err == nil {
		t.Fatal("charging past generation 1's installed ceiling (999) unexpectedly succeeded")
	}
}
