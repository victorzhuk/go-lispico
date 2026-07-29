package core

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// TestRearmReentrantEvalState_FieldByFieldInvalidation pins the delta
// rearm's configuration comparison one input at a time: for every value
// RearmReentrantEvalState receives, changing exactly one of them between two
// rearms of the same wrapper must install exactly what a full rearm would —
// the remembered-config fast path must never skip a store the change
// invalidates. Each case rearms once with a baseline configuration, then
// again with one input mutated, and asserts every atomic field holds the
// mutated configuration's values.
func TestRearmReentrantEvalState_FieldByFieldInvalidation(t *testing.T) {
	ctx := context.Background()

	baseSnap := EvalMeterSnapshot{MaxReductions: 111, MaxAllocationBytes: 222, Reductions: 10, AllocationBytes: 20}
	const baseTimeout = time.Minute

	cases := []struct {
		name   string
		mutate func(structSeed, callSeed *int64, snap *EvalMeterSnapshot, timeout *time.Duration)
	}{
		{"max reductions", func(_, _ *int64, snap *EvalMeterSnapshot, _ *time.Duration) { snap.MaxReductions = 999 }},
		{"max allocation bytes", func(_, _ *int64, snap *EvalMeterSnapshot, _ *time.Duration) { snap.MaxAllocationBytes = 888 }},
		{"timeout", func(_, _ *int64, _ *EvalMeterSnapshot, timeout *time.Duration) { *timeout = time.Hour }},
		{"struct seed", func(structSeed, _ *int64, _ *EvalMeterSnapshot, _ *time.Duration) { *structSeed = 7 }},
		{"call seed", func(_, callSeed *int64, _ *EvalMeterSnapshot, _ *time.Duration) { *callSeed = 9 }},
		{"reductions seed", func(_, _ *int64, snap *EvalMeterSnapshot, _ *time.Duration) { snap.Reductions = 30 }},
		{"allocation seed", func(_, _ *int64, snap *EvalMeterSnapshot, _ *time.Duration) { snap.AllocationBytes = 40 }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gen atomic.Uint64
			wrapped, _, _ := AdoptReentrantEvalState(ctx, baseTimeout, nil, 0, 0, baseSnap, &gen)
			gen.Add(1) // end the building run so the rearm is a legitimate stale-to-live transition

			structSeed, callSeed := int64(0), int64(0)
			snap := baseSnap
			timeout := baseTimeout
			tc.mutate(&structSeed, &callSeed, &snap, &timeout)

			if _, ok := RearmReentrantEvalState(wrapped, ctx, structSeed, callSeed, snap, timeout); !ok {
				t.Fatal("RearmReentrantEvalState: want ok=true for the same outer ctx")
			}

			w := wrapped.(*lazyEvalStateCtx)
			if want := normalizeEvalLimit(snap.MaxReductions, DefaultMaxReductions); w.maxReductions.Load() != want {
				t.Errorf("maxReductions = %d, want %d", w.maxReductions.Load(), want)
			}
			if want := normalizeEvalLimit(snap.MaxAllocationBytes, DefaultMaxAllocationBytes); w.maxAllocBytes.Load() != want {
				t.Errorf("maxAllocBytes = %d, want %d", w.maxAllocBytes.Load(), want)
			}
			if got := time.Duration(w.timeout.Load()); got != timeout {
				t.Errorf("timeout = %v, want %v", got, timeout)
			}
			if w.counter.Load() != structSeed {
				t.Errorf("struct counter = %d, want %d", w.counter.Load(), structSeed)
			}
			if w.callCounter.Load() != callSeed {
				t.Errorf("call counter = %d, want %d", w.callCounter.Load(), callSeed)
			}
			if w.reductions.Load() != snap.Reductions {
				t.Errorf("reductions = %d, want %d", w.reductions.Load(), snap.Reductions)
			}
			if w.allocBytes.Load() != snap.AllocationBytes {
				t.Errorf("allocBytes = %d, want %d", w.allocBytes.Load(), snap.AllocationBytes)
			}
		})
	}
}

// TestRearmReentrantEvalState_SameConfigObservablyIdentical proves a
// same-configuration rearm installs exactly what a full rearm would, even
// when the finished run left residue: counters moved, meter seeds advanced,
// and an evalState materialized mid-run. The incoming zero seeds differ
// from the live atomic counters (which hold residue), so the elision
// check must write them back; the materialized state must be dropped; and
// a second same-config rearm with different meter consumption must refresh
// the seeds again.
func TestRearmReentrantEvalState_SameConfigObservablyIdentical(t *testing.T) {
	ctx := context.Background()
	var gen atomic.Uint64
	snap := EvalMeterSnapshot{MaxReductions: 500, MaxAllocationBytes: 4096}

	wrapped, _, _ := AdoptReentrantEvalState(ctx, time.Minute, nil, 3, 5, snap, &gen)
	w := wrapped.(*lazyEvalStateCtx)

	// Residue a finished run leaves behind.
	w.counter.Store(11)
	w.callCounter.Store(13)
	w.reductions.Store(77)
	w.allocBytes.Store(88)
	w.state.Store(newEvalState())
	gen.Add(1) // run ends: wrapper goes stale

	// Top-level boundary rearm under identical configuration: seeds back to
	// zero, meter flushed to a fresh snapshot.
	fresh := EvalMeterSnapshot{MaxReductions: 500, MaxAllocationBytes: 4096, Reductions: 1, AllocationBytes: 2}
	if _, ok := RearmReentrantEvalState(wrapped, ctx, 0, 0, fresh, time.Minute); !ok {
		t.Fatal("RearmReentrantEvalState: want ok=true for the same outer ctx")
	}

	if w.counter.Load() != 0 {
		t.Errorf("struct counter = %d, want re-seeded 0", w.counter.Load())
	}
	if w.callCounter.Load() != 0 {
		t.Errorf("call counter = %d, want re-seeded 0", w.callCounter.Load())
	}
	if w.reductions.Load() != 1 {
		t.Errorf("reductions = %d, want fresh seed 1", w.reductions.Load())
	}
	if w.allocBytes.Load() != 2 {
		t.Errorf("allocBytes = %d, want fresh seed 2", w.allocBytes.Load())
	}
	if w.state.Load() != nil {
		t.Error("materialized state from the previous run survived the rearm")
	}
	if !w.live() {
		t.Error("wrapper not live after rearm")
	}
	if got := w.maxReductions.Load(); got != 500 {
		t.Errorf("maxReductions = %d, want unchanged 500", got)
	}
	if got := w.maxAllocBytes.Load(); got != 4096 {
		t.Errorf("maxAllocBytes = %d, want unchanged 4096", got)
	}
	if got := time.Duration(w.timeout.Load()); got != time.Minute {
		t.Errorf("timeout = %v, want unchanged %v", got, time.Minute)
	}

	// A second same-config rearm with different meter consumption must
	// refresh the seeds even though nothing else changed.
	gen.Add(1)
	next := EvalMeterSnapshot{MaxReductions: 500, MaxAllocationBytes: 4096, Reductions: 9, AllocationBytes: 10}
	if _, ok := RearmReentrantEvalState(wrapped, ctx, 0, 0, next, time.Minute); !ok {
		t.Fatal("second same-config rearm: want ok=true")
	}
	if w.reductions.Load() != 9 {
		t.Errorf("reductions after second rearm = %d, want 9", w.reductions.Load())
	}
	if w.allocBytes.Load() != 10 {
		t.Errorf("allocBytes after second rearm = %d, want 10", w.allocBytes.Load())
	}
}

// TestRearmReentrantEvalState_SeedResidueLeak pins the case where the
// remembered seed matches the incoming seed but the atomic counter holds
// residue from a finished run: build with seed 0, mutate the counter to
// non-zero (simulating run-time depth changes), then rearm with seed 0
// under identical configuration. A naive remembered-seed comparison would
// see 0 == 0 and skip the store, leaking residue into the next run.
func TestRearmReentrantEvalState_SeedResidueLeak(t *testing.T) {
	ctx := context.Background()
	var gen atomic.Uint64
	snap := EvalMeterSnapshot{MaxReductions: 100, MaxAllocationBytes: 200}

	wrapped, _, _ := AdoptReentrantEvalState(ctx, time.Minute, nil, 0, 0, snap, &gen)
	w := wrapped.(*lazyEvalStateCtx)

	// Simulate a run that moved depth counters away from the seed.
	w.counter.Store(42)
	w.callCounter.Store(43)
	gen.Add(1)

	// Same config, same seeds (0, 0): the elision must compare the live
	// atomic value, not a remembered seed, and write the counters back.
	if _, ok := RearmReentrantEvalState(wrapped, ctx, 0, 0, snap, time.Minute); !ok {
		t.Fatal("RearmReentrantEvalState: want ok=true")
	}
	if got := w.counter.Load(); got != 0 {
		t.Errorf("struct counter = %d, want 0 (residue leaked)", got)
	}
	if got := w.callCounter.Load(); got != 0 {
		t.Errorf("call counter = %d, want 0 (residue leaked)", got)
	}
}
