package vm

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/victorzhuk/go-lispico/core"
)

// TestCallReentrancy_RearmInstallsNewRunDeadlineBeforeGeneration pins the
// ordering contract around re-arming a retained reentrant ctx for a new run:
// once the new generation is published (the wrapper reports live again), a
// goroutine still holding the retained ctx from the prior run must never
// materialize an evalState governed by that run's already-expired deadline —
// the new run's deadline must govern. The window between
// RearmReentrantEvalState's generation store and installReentrantDeadline is
// a race today; this test may pass on any given run but pins the contract
// the ordering fix must satisfy.
func TestCallReentrancy_RearmInstallsNewRunDeadlineBeforeGeneration(t *testing.T) {
	t.Parallel()

	for range 25 {
		env := core.NewEnv(nil)
		v := New(env, WithEvaluator(core.NewEvaluator()))

		var stashed context.Context
		stash := core.GoFunc{Name: "stash", Fn: func(ctx context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			stashed = ctx
			return core.Nil{}, nil
		}}
		// Dispatch-only probe: run 2's first GoFunc dispatch drives the
		// rearm path without materializing any eval state of its own.
		probe := core.GoFunc{Name: "probe", Fn: func(ctx context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			return core.Nil{}, nil
		}}
		args := []core.Value{}

		// Run 1 with a deadline that expires before run 2 begins; its dispatch
		// adopts the wrapper the retained observers will hold.
		v.SetTimeout(20 * time.Millisecond)
		if _, err := v.ApplyPooled(context.Background(), stash, args, env); err != nil {
			t.Fatal(err)
		}
		time.Sleep(60 * time.Millisecond) // run 1's deadline is now in the past

		var wg sync.WaitGroup
		runDone := make(chan struct{})
		observed := make(chan time.Time, 4)
		for range 4 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				// Retained-ctx observation: wait until the wrapper is
				// stamped for the new run, then materialize its eval state.
				// A reader that misses the window entirely (the common case
				// once the re-armed run has finished and re-staled the
				// wrapper) exits without observing; the assertion below only
				// judges readers that actually materialized.
				for !core.ReentrantEvalStateLive(stashed) {
					select {
					case <-runDone:
						return
					default:
					}
					runtime.Gosched()
				}
				observed <- core.EvalDeadlineFrom(stashed)
			}()
		}

		v.Reset()
		v.SetTimeout(time.Hour)
		if _, err := v.ApplyPooled(context.Background(), probe, args, env); err != nil {
			t.Fatal(err)
		}
		close(runDone)
		waitDone := make(chan struct{})
		go func() { wg.Wait(); close(waitDone) }()
		select {
		case <-waitDone:
		case <-time.After(5 * time.Second):
			t.Fatal("retained-ctx observers did not settle after the re-armed run")
		}
		close(observed)
		// A zero deadline means the wrapper re-staled (run 2 finished and
		// bumped the generation) between the live check and the read — the
		// read fell through to the empty state and is not an observation of
		// the defect. Only a materialized, non-zero, already-expired deadline
		// is the prior run's deadline governing after publication.
		now := time.Now()
		for d := range observed {
			if d.IsZero() {
				continue
			}
			if !d.After(now) {
				t.Fatalf("retained ctx observed the prior run's deadline %v (now %v): once the generation is published, the new run's deadline must govern", d, now)
			}
		}
	}
}
