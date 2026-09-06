package core

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// budgetCtx returns a context carrying a fresh eval state with the given
// reduction ceiling and an effectively unlimited allocation ceiling.
func budgetCtx(parent context.Context, maxReductions int64) context.Context {
	return WithEvalResourceLimits(parent, int(maxReductions), 1<<30)
}

func TestBuiltinWorkBudget_InstalledDeadlineReadAtNextSync(t *testing.T) {
	base := time.Now()
	var calls int
	restore := nowFunc
	nowFunc = func() time.Time {
		calls++
		return base
	}
	t.Cleanup(func() { nowFunc = restore })

	ctx := WithEvalDeadline(budgetCtx(t.Context(), 1_000_000), base.Add(time.Hour))
	for range 3 {
		b := NewBuiltinWorkBudget(ctx)
		stepN(t, b, 1)
		if err := b.Flush(); err != nil {
			t.Fatalf("sync before reinstalling deadline: %v", err)
		}
	}
	if calls != 1 {
		t.Fatalf("3 synchronizations: want 1 clock read, got %d", calls)
	}

	ctx = WithEvalDeadline(ctx, base.Add(2*time.Hour))
	b := NewBuiltinWorkBudget(ctx)
	stepN(t, b, 1)
	before := calls
	if err := b.Flush(); err != nil {
		t.Fatalf("first sync after reinstalling deadline: %v", err)
	}
	if got := calls - before; got != 1 {
		t.Fatalf("first sync after reinstalling deadline: want 1 clock read, got %d", got)
	}
}

func TestBuiltinWorkBudget_FreshEvalStateReadsClockAtFirstSync(t *testing.T) {
	base := time.Now()
	deadline := base.Add(time.Hour)
	var calls int
	restore := nowFunc
	nowFunc = func() time.Time {
		calls++
		return base
	}
	t.Cleanup(func() { nowFunc = restore })

	ctx, _, _ := AdoptEvalStateWithMeter(context.Background(), deadline, 0, EvalMeterSnapshot{})
	b := NewBuiltinWorkBudget(ctx)
	stepN(t, b, 1)
	before := calls
	if err := b.Flush(); err != nil {
		t.Fatalf("first sync with fresh eval state: %v", err)
	}
	if got := calls - before; got != 1 {
		t.Fatalf("first sync with fresh eval state: want 1 clock read, got %d", got)
	}
}

func TestBuiltinWorkBudget_ShortCallsShareClockCadence(t *testing.T) {
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

			ctx := WithEvalDeadline(budgetCtx(t.Context(), 1_000_000), base.Add(time.Hour))
			for i := range n {
				b := NewBuiltinWorkBudget(ctx)
				stepN(t, b, 1)
				if err := b.Flush(); err != nil {
					t.Fatalf("short call %d: Flush: %v", i+1, err)
				}
			}

			if want := (n + 7) / 8; calls != want {
				t.Fatalf("%d short calls: want %d clock reads, got %d", n, want, calls)
			}
		})
	}
}

func TestBuiltinWorkBudget_DeadlineCrossingBoundedBySynchronizations(t *testing.T) {
	for _, units := range []int{1, 128} {
		t.Run(fmt.Sprintf("units=%d", units), func(t *testing.T) {
			base := time.Now()
			deadline := base.Add(time.Hour)
			now := base
			restore := nowFunc
			nowFunc = func() time.Time { return now }
			t.Cleanup(func() { nowFunc = restore })

			ctx := WithEvalDeadline(budgetCtx(t.Context(), 1_000_000), deadline)
			b := NewBuiltinWorkBudget(ctx)
			stepN(t, b, 1)
			if err := b.Flush(); err != nil {
				t.Fatalf("sync before expiry: %v", err)
			}

			now = deadline
			for sync := 1; sync <= 8; sync++ {
				b = NewBuiltinWorkBudget(ctx)
				stepN(t, b, units-1)
				err := b.Step()
				if err == nil {
					err = b.Flush()
				}
				if err == nil {
					continue
				}
				if err != context.DeadlineExceeded {
					t.Fatalf("sync %d after expiry: want bare context.DeadlineExceeded, got %T: %v", sync, err, err)
				}
				return
			}
			t.Fatal("expired deadline not observed within 8 synchronizations")
		})
	}
}

func TestBuiltinWorkBudget_CancellationUnconditionalMidCadence(t *testing.T) {
	base := time.Now()
	restore := nowFunc
	nowFunc = func() time.Time { return base }
	t.Cleanup(func() { nowFunc = restore })

	for phase := 1; phase < 8; phase++ {
		t.Run(fmt.Sprintf("phase=%d", phase), func(t *testing.T) {
			parent, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			ctx := WithEvalDeadline(budgetCtx(parent, 1_000_000), base.Add(time.Hour))
			for range phase {
				b := NewBuiltinWorkBudget(ctx)
				stepN(t, b, 1)
				if err := b.Flush(); err != nil {
					t.Fatalf("sync before cancellation: %v", err)
				}
			}

			cancel()
			b := NewBuiltinWorkBudget(ctx)
			stepN(t, b, 1)
			if err := b.Flush(); err != context.Canceled {
				t.Fatalf("sync after cancellation at phase %d: want context.Canceled, got %v", phase, err)
			}
		})
	}
}

func TestBuiltinWorkBudget_ReductionChargeUnconditionalMidCadence(t *testing.T) {
	base := time.Now()
	restore := nowFunc
	nowFunc = func() time.Time { return base }
	t.Cleanup(func() { nowFunc = restore })

	for phase := 1; phase < 8; phase++ {
		t.Run(fmt.Sprintf("phase=%d", phase), func(t *testing.T) {
			ctx := WithEvalDeadline(budgetCtx(t.Context(), int64(phase)), base.Add(time.Hour))
			for range phase {
				b := NewBuiltinWorkBudget(ctx)
				stepN(t, b, 1)
				if err := b.Flush(); err != nil {
					t.Fatalf("sync within reduction limit %d: %v", phase, err)
				}
			}

			b := NewBuiltinWorkBudget(ctx)
			stepN(t, b, 1)
			err := b.Flush()
			if !IsTerminalEvalError(err) || errCode(t, err) != CodeResourceLimit {
				t.Fatalf("sync exceeding reduction limit at phase %d: want terminal %s, got %v", phase, CodeResourceLimit, err)
			}
		})
	}
}

// errCode returns the LispicoError code of err via errors.As.
func errCode(t *testing.T, err error) string {
	t.Helper()
	var le *LispicoError
	if !errors.As(err, &le) {
		t.Fatalf("expected *LispicoError, got %v", err)
	}
	return le.Code
}

// stepN performs n Steps, requiring each to return nil.
func stepN(t *testing.T, b *BuiltinWorkBudget, n int) {
	t.Helper()
	for i := range n {
		if err := b.Step(); err != nil {
			t.Fatalf("Step %d of %d: unexpected error before sync point: %v", i+1, n, err)
		}
	}
}

// TestBuiltinWorkBudget_StepsLocalUntil128thUnit pins the batching contract:
// 127 Steps touch no shared state (nil under a limit below the batch size),
// and the 128-unit sync on the 128th Step or on Flush raises the terminal
// ResourceLimitError once the batch crosses maxReductions.
func TestBuiltinWorkBudget_StepsLocalUntil128thUnit(t *testing.T) {
	t.Parallel()
	limit := int64(100) // below the 128-unit batch

	t.Run("flush", func(t *testing.T) {
		t.Parallel()
		b := NewBuiltinWorkBudget(budgetCtx(context.Background(), limit))
		stepN(t, b, 127)
		err := b.Flush()
		if !IsTerminalEvalError(err) || errCode(t, err) != CodeResourceLimit {
			t.Fatalf("Flush of 127 pending units under limit %d: want terminal %s, got %v", limit, CodeResourceLimit, err)
		}
	})

	t.Run("step128", func(t *testing.T) {
		t.Parallel()
		b := NewBuiltinWorkBudget(budgetCtx(context.Background(), limit))
		stepN(t, b, 127)
		err := b.Step()
		if !IsTerminalEvalError(err) || errCode(t, err) != CodeResourceLimit {
			t.Fatalf("128th Step (batch sync) under limit %d: want terminal %s, got %v", limit, CodeResourceLimit, err)
		}
	})

	t.Run("stepsStayLocalUnderGenerousLimit", func(t *testing.T) {
		t.Parallel()
		b := NewBuiltinWorkBudget(budgetCtx(context.Background(), 1_000_000))
		stepN(t, b, 127)
		if err := b.Flush(); err != nil {
			t.Fatalf("Flush of 127 units under generous limit: %v", err)
		}
	})
}

// TestBuiltinWorkBudget_FlushIdempotentEmpty pins that an empty successful
// flush returns nil, does no shared work, and stays nil when repeated.
func TestBuiltinWorkBudget_FlushIdempotentEmpty(t *testing.T) {
	t.Parallel()
	b := NewBuiltinWorkBudget(budgetCtx(context.Background(), 1_000_000))
	if err := b.Flush(); err != nil {
		t.Fatalf("first empty Flush: %v", err)
	}
	if err := b.Flush(); err != nil {
		t.Fatalf("second empty Flush must stay a no-op, got %v", err)
	}
}

// TestBuiltinWorkBudget_LatchesFirstSyncError pins that the first sync error
// is latched and replayed by reference: later Step and Flush calls return the
// identical error value and perform no further sync.
func TestBuiltinWorkBudget_LatchesFirstSyncError(t *testing.T) {
	t.Parallel()
	b := NewBuiltinWorkBudget(budgetCtx(context.Background(), 1)) // any sync crosses
	var first error
	for i := range 128 {
		err := b.Step()
		if i == 127 {
			first = err
		} else if err != nil {
			t.Fatalf("Step %d: unexpected early error %v", i+1, err)
		}
	}
	if first == nil || !IsTerminalEvalError(first) {
		t.Fatalf("first sync: want terminal ResourceLimitError, got %v", first)
	}
	if err := b.Step(); !errors.Is(err, first) {
		t.Fatalf("Step after latch: want replayed %v, got %v", first, err)
	}
	if err := b.Flush(); err != first {
		t.Fatalf("Flush after latch: want identical error value %v, got %v", first, err)
	}
}

// TestBuiltinWorkBudget_DeadlineObservedAtSyncWithLiveParentCtx pins that an
// engine-owned absolute deadline (WithEvalDeadline, parent ctx live) is only
// observed at the sync: the 127 local Steps before it pass even with the
// deadline already passed, and the sync returns context.DeadlineExceeded.
func TestBuiltinWorkBudget_DeadlineObservedAtSyncWithLiveParentCtx(t *testing.T) {
	t.Parallel()
	ctx := budgetCtx(context.Background(), 1_000_000)
	ctx = WithEvalDeadline(ctx, time.Now().Add(-time.Millisecond))
	b := NewBuiltinWorkBudget(ctx)
	stepN(t, b, 127)
	if err := b.Step(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("sync with passed engine deadline and live parent ctx: want context.DeadlineExceeded, got %v", err)
	}
}

// TestBuiltinWorkBudget_CallerCancellationObservedAtSync pins that caller
// cancellation from a cancellable parent context is observed at the sync, not
// per unit: 127 local Steps pass after cancellation, the 128th returns the
// caller's ctx.Err().
func TestBuiltinWorkBudget_CallerCancellationObservedAtSync(t *testing.T) {
	t.Parallel()
	parent, cancel := context.WithCancel(context.Background())
	b := NewBuiltinWorkBudget(budgetCtx(parent, 1_000_000))
	cancel()
	stepN(t, b, 127)
	if err := b.Step(); !errors.Is(err, context.Canceled) {
		t.Fatalf("sync with cancelled caller ctx: want context.Canceled, got %v", err)
	}
}

// TestBuiltinWorkBudget_TerminalFlushWins_OriginalErrorPreserved pins the
// consumer-side error precedence through a real engine dispatch: a GoFunc
// accumulates a pending non-Terminal validation error mid-loop, then applies
// the mandatory final Flush. When the Flush succeeds, the dispatch surfaces
// the original error unchanged; when the Flush raises a Terminal
// ResourceLimitError, the Terminal error wins and the pending one is dropped.
func TestBuiltinWorkBudget_TerminalFlushWins_OriginalErrorPreserved(t *testing.T) {
	t.Parallel()

	// spin steps the budget 50 times, accumulates a non-Terminal validation
	// error at iteration 25, and returns it after the mandatory final Flush.
	// The budget is built from the dispatched ctx (the production path), so
	// the Flush charge lands on the eval state the evaluator enforces.
	spin := func() *Env {
		env := newTestEnv()
		env.Set("spin", GoFunc{Name: "spin", Fn: func(ctx context.Context, _ Evaluator, _ []Value, _ *Env) (Value, error) {
			b := NewBuiltinWorkBudget(ctx)
			var pending error
			for i := range 50 {
				if err := b.Step(); err != nil {
					return Nil{}, err
				}
				if i == 25 {
					pending = NewTypeError("int", Int{V: 1})
				}
			}
			if ferr := b.Flush(); ferr != nil && (pending == nil || IsTerminalEvalError(ferr)) {
				pending = ferr
			}
			return Nil{}, pending
		}})
		return env
	}

	t.Run("flushSuccessPreservesPending", func(t *testing.T) {
		t.Parallel()
		ctx := budgetCtx(context.Background(), 1_000_000) // 50 units fit: Flush succeeds
		_, err := NewEvaluator().Eval(ctx, Read1(t, "(spin)"), spin())
		if err == nil {
			t.Fatal("dispatch must surface the GoFunc's pending non-Terminal error, got nil")
		}
		if IsTerminalEvalError(err) {
			t.Fatalf("successful Flush must keep the original non-Terminal error, got terminal %v", err)
		}
		if errCode(t, err) != "TypeError" {
			t.Fatalf("successful Flush must return the original TypeError unchanged, got code %q: %v", errCode(t, err), err)
		}
	})

	t.Run("terminalFlushReplacesPending", func(t *testing.T) {
		t.Parallel()
		ctx := budgetCtx(context.Background(), 40) // 50 units exceed 40: Flush raises Terminal
		_, err := NewEvaluator().Eval(ctx, Read1(t, "(spin)"), spin())
		if !IsTerminalEvalError(err) || errCode(t, err) != CodeResourceLimit {
			t.Fatalf("Terminal flush error must win over the pending non-Terminal error: want terminal %s, got %v", CodeResourceLimit, err)
		}
	})
}

// TestBuiltinWorkBudget_EvaluatorGoFunc_TerminalUnderLowReductions exercises
// the tree-walker GoFunc dispatch site: a GoFunc that steps a budget through a
// long uninterrupted loop under a low Reduction limit surfaces the terminal
// ResourceLimitError from its dispatch.
func TestBuiltinWorkBudget_EvaluatorGoFunc_TerminalUnderLowReductions(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	env.Set("spin", GoFunc{Name: "spin", Fn: func(ctx context.Context, _ Evaluator, _ []Value, _ *Env) (Value, error) {
		b := NewBuiltinWorkBudget(ctx)
		for range 10_000 {
			if err := b.Step(); err != nil {
				return Nil{}, err
			}
		}
		return Nil{}, b.Flush()
	}})

	ctx := budgetCtx(context.Background(), 200)
	_, err := NewEvaluator().Eval(ctx, Read1(t, "(spin)"), env)
	if !IsTerminalEvalError(err) || errCode(t, err) != CodeResourceLimit {
		t.Fatalf("GoFunc loop under low Reduction limit: want terminal %s, got %v", CodeResourceLimit, err)
	}
}

// Read1 reads exactly one form from src.
func Read1(t *testing.T, src string) Value {
	t.Helper()
	forms, err := Read(src)
	if err != nil {
		t.Fatalf("Read(%q): %v", src, err)
	}
	if len(forms) != 1 {
		t.Fatalf("Read(%q): want 1 form, got %d", src, len(forms))
	}
	return forms[0]
}

func TestBuiltinWorkBudget_FinishForcesDeadlineAfterError(t *testing.T) {
	for phase := 1; phase < 8; phase++ {
		for _, units := range []int{0, 3} {
			for _, after := range []time.Duration{0, time.Nanosecond} {
				t.Run(fmt.Sprintf("phase=%d/pending=%d/after=%s", phase, units, after), func(t *testing.T) {
					base := time.Now()
					deadline := base.Add(time.Hour)
					now := base
					var calls int
					restore := nowFunc
					nowFunc = func() time.Time {
						calls++
						return now
					}
					t.Cleanup(func() { nowFunc = restore })

					ctx := WithEvalDeadline(budgetCtx(t.Context(), 1_000_000), deadline)
					for range phase {
						b := NewBuiltinWorkBudget(ctx)
						stepN(t, b, 1)
						if err := b.Flush(); err != nil {
							t.Fatalf("sync before expiry: %v", err)
						}
					}

					b := NewBuiltinWorkBudget(ctx)
					stepN(t, b, units)
					now = deadline.Add(after)
					before := calls
					if err := b.Finish(errors.New("operation failed")); err != context.DeadlineExceeded {
						t.Errorf("Finish after expiry: want bare context.DeadlineExceeded, got %v", err)
					}
					if got := calls - before; got != 1 {
						t.Errorf("Finish after expiry: want 1 clock read, got %d", got)
					}
				})
			}
		}
	}
}

func TestBuiltinWorkBudget_FinishPreservesSuccessfulCadence(t *testing.T) {
	for _, n := range []int{1, 8, 9, 16, 17, 37} {
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			base := time.Now()
			now := base
			var calls int
			restore := nowFunc
			nowFunc = func() time.Time {
				calls++
				return now
			}
			t.Cleanup(func() { nowFunc = restore })

			parent, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			deadline := base.Add(time.Hour)
			ctx := WithEvalDeadline(budgetCtx(parent, 1_000_000), deadline)
			for range n {
				b := NewBuiltinWorkBudget(ctx)
				stepN(t, b, 1)
				if err := b.Finish(nil); err != nil {
					t.Fatalf("Finish(nil): %v", err)
				}
				if err := b.Finish(nil); err != nil {
					t.Fatalf("empty Finish(nil): %v", err)
				}
			}
			if want := (n + 7) / 8; calls != want {
				t.Errorf("%d synchronizations: want %d clock reads, got %d", n, want, calls)
			}

			now = deadline
			cancel()
			before := calls
			if err := NewBuiltinWorkBudget(ctx).Finish(nil); err != nil {
				t.Errorf("empty Finish(nil) after expiry and cancellation: want nil, got %v", err)
			}
			if calls != before {
				t.Errorf("empty Finish(nil): want no clock read, got %d", calls-before)
			}
		})
	}
}

func TestBuiltinWorkBudget_FinishPreservesErrorPrecedence(t *testing.T) {
	ordinary := errors.New("operation failed")
	wrappedCancel := fmt.Errorf("callback: %w", context.Canceled)
	wrappedDeadline := fmt.Errorf("callback: %w", context.DeadlineExceeded)
	resource := NewResourceLimitError("callback reduction limit")
	for _, tc := range []struct {
		name     string
		input    error
		armed    bool
		expired  bool
		canceled bool
		cross    bool
		units    int
		want     error
		reads    int
	}{
		{name: "nil", units: 3},
		{name: "ordinary-no-deadline", input: ordinary, units: 3, want: ordinary},
		{name: "ordinary-live-deadline", input: ordinary, armed: true, units: 3, want: ordinary, reads: 1},
		{name: "nil-keeps-cadence", armed: true, expired: true, units: 3},
		{name: "nil-observes-cancellation", armed: true, expired: true, canceled: true, units: 3, want: context.Canceled},
		{name: "empty-cancellation", input: ordinary, canceled: true, want: context.Canceled},
		{name: "pending-cancellation", input: ordinary, canceled: true, units: 3, want: context.Canceled},
		{name: "deadline-before-cancellation", input: ordinary, armed: true, expired: true, canceled: true, units: 3, want: context.DeadlineExceeded, reads: 1},
		{name: "reductions-before-deadline", input: ordinary, armed: true, expired: true, canceled: true, cross: true, units: 3},
		{name: "terminal-before-reductions", input: wrappedCancel, armed: true, expired: true, canceled: true, cross: true, units: 3, want: wrappedCancel},
		{name: "terminal-keeps-cadence", input: wrappedDeadline, armed: true, units: 3, want: wrappedDeadline},
		{name: "empty-terminal", input: resource, armed: true, expired: true, want: resource},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := time.Now()
			deadline := base.Add(time.Hour)
			now := base
			var calls int
			restore := nowFunc
			nowFunc = func() time.Time {
				calls++
				return now
			}
			t.Cleanup(func() { nowFunc = restore })

			parent, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			limit := int64(1_000_000)
			if tc.cross {
				limit = 1
			}
			ctx := budgetCtx(parent, limit)
			if tc.armed {
				ctx = WithEvalDeadline(ctx, deadline)
				b := NewBuiltinWorkBudget(ctx)
				stepN(t, b, 1)
				if err := b.Flush(); err != nil {
					t.Fatalf("prime cadence: %v", err)
				}
			}
			if tc.expired {
				now = deadline
			}
			if tc.canceled {
				cancel()
			}

			b := NewBuiltinWorkBudget(ctx)
			stepN(t, b, tc.units)
			before := calls
			got := b.Finish(tc.input)
			if tc.cross && tc.want == nil {
				if !IsTerminalEvalError(got) || errCode(t, got) != CodeResourceLimit {
					t.Errorf("Finish: want terminal %s, got %v", CodeResourceLimit, got)
				}
			} else if got != tc.want {
				t.Errorf("Finish: want original %v, got %v", tc.want, got)
			}
			if got := calls - before; got != tc.reads {
				t.Errorf("Finish: want %d clock reads, got %d", tc.reads, got)
			}
			if tc.cross {
				latched := b.Flush()
				if !IsTerminalEvalError(latched) || errCode(t, latched) != CodeResourceLimit {
					t.Fatalf("Flush after Finish: want latched %s, got %v", CodeResourceLimit, latched)
				}
				if err := b.Step(); err != latched {
					t.Errorf("Step after Finish: want identical latched error, got %v", err)
				}
			}
		})
	}
}

func TestBuiltinWorkBudget_FinishChargesPendingOnce(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input error
	}{
		{name: "nil"},
		{name: "ordinary", input: errors.New("operation failed")},
		{name: "terminal", input: fmt.Errorf("callback: %w", context.Canceled)},
	} {
		for _, units := range []int{0, 3, 128, 131} {
			t.Run(fmt.Sprintf("%s/units=%d", tc.name, units), func(t *testing.T) {
				ctx := budgetCtx(t.Context(), 1_000_000)
				b := NewBuiltinWorkBudget(ctx)
				before := EvalMeterFrom(ctx).Snapshot()
				stepN(t, b, units)
				for attempt := 1; attempt <= 2; attempt++ {
					if err := b.Finish(tc.input); err != tc.input {
						t.Errorf("Finish attempt %d: want original %v, got %v", attempt, tc.input, err)
					}
					after := EvalMeterFrom(ctx).Snapshot()
					if got := after.Reductions - before.Reductions; got != int64(units) {
						t.Errorf("Finish attempt %d: want %d reductions, got %d", attempt, units, got)
					}
					if after.AllocationBytes != before.AllocationBytes {
						t.Errorf("Finish attempt %d: allocation charge changed by %d", attempt, after.AllocationBytes-before.AllocationBytes)
					}
				}

				stepN(t, b, 1)
				if err := b.Flush(); err != nil {
					t.Fatalf("Flush after supplied operation error: %v", err)
				}
				if got := EvalMeterFrom(ctx).Snapshot().Reductions - before.Reductions; got != int64(units+1) {
					t.Errorf("continued work: want %d reductions, got %d", units+1, got)
				}
			})
		}
	}
}

func TestBuiltinWorkBudget_FinishReplaysLatchedError(t *testing.T) {
	for _, kind := range []string{"reductions", "deadline", "cancellation"} {
		t.Run(kind, func(t *testing.T) {
			base := time.Now()
			var calls int
			restore := nowFunc
			nowFunc = func() time.Time {
				calls++
				return base
			}
			t.Cleanup(func() { nowFunc = restore })

			parent, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			limit := int64(1_000_000)
			deadline := base.Add(time.Hour)
			switch kind {
			case "reductions":
				limit = 1
			case "deadline":
				deadline = base
			case "cancellation":
				cancel()
			}
			ctx := WithEvalDeadline(budgetCtx(parent, limit), deadline)
			b := NewBuiltinWorkBudget(ctx)
			stepN(t, b, 2)
			first := b.Flush()
			switch kind {
			case "reductions":
				if !IsTerminalEvalError(first) || errCode(t, first) != CodeResourceLimit {
					t.Fatalf("first Flush: want terminal %s, got %v", CodeResourceLimit, first)
				}
			case "deadline":
				if first != context.DeadlineExceeded {
					t.Fatalf("first Flush: want context.DeadlineExceeded, got %v", first)
				}
			case "cancellation":
				if first != context.Canceled {
					t.Fatalf("first Flush: want context.Canceled, got %v", first)
				}
			}
			before := EvalMeterFrom(ctx).Snapshot()
			reads := calls
			for _, action := range []struct {
				name string
				run  func() error
			}{
				{name: "Step", run: b.Step},
				{name: "Flush", run: b.Flush},
				{name: "Finish(nil)", run: func() error { return b.Finish(nil) }},
				{name: "Finish(error)", run: func() error { return b.Finish(errors.New("operation failed")) }},
			} {
				if err := action.run(); err != first {
					t.Errorf("%s: want identical latched error, got %v", action.name, err)
				}
			}
			terminal := fmt.Errorf("callback: %w", context.DeadlineExceeded)
			if err := b.Finish(terminal); err != terminal {
				t.Errorf("Finish(terminal): want original terminal error, got %v", err)
			}
			if err := b.Flush(); err != first {
				t.Errorf("Flush after terminal input: want original latch, got %v", err)
			}
			after := EvalMeterFrom(ctx).Snapshot()
			if after.Reductions != before.Reductions || after.AllocationBytes != before.AllocationBytes {
				t.Errorf("latched settlement changed accounting: reductions %d -> %d, bytes %d -> %d", before.Reductions, after.Reductions, before.AllocationBytes, after.AllocationBytes)
			}
			if calls != reads {
				t.Errorf("latched settlement: want no clock reads, got %d", calls-reads)
			}
		})
	}
}

func TestBuiltinWorkBudget_FinishResetsDeadlineCadence(t *testing.T) {
	base := time.Now()
	var calls int
	restore := nowFunc
	nowFunc = func() time.Time {
		calls++
		return base
	}
	t.Cleanup(func() { nowFunc = restore })

	ctx := WithEvalDeadline(budgetCtx(t.Context(), 1_000_000), base.Add(time.Hour))
	for range 3 {
		b := NewBuiltinWorkBudget(ctx)
		stepN(t, b, 1)
		if err := b.Flush(); err != nil {
			t.Fatalf("prime cadence: %v", err)
		}
	}
	b := NewBuiltinWorkBudget(ctx)
	stepN(t, b, 1)
	before := calls
	ordinary := errors.New("operation failed")
	if err := b.Finish(ordinary); err != ordinary {
		t.Fatalf("Finish before expiry: want original error, got %v", err)
	}
	if got := calls - before; got != 1 {
		t.Fatalf("Finish before expiry: want 1 clock read, got %d", got)
	}

	before = calls
	for sync := 1; sync <= 8; sync++ {
		b = NewBuiltinWorkBudget(ctx)
		stepN(t, b, 1)
		if err := b.Flush(); err != nil {
			t.Fatalf("sync %d after Finish: %v", sync, err)
		}
		want := 0
		if sync == 8 {
			want = 1
		}
		if got := calls - before; got != want {
			t.Errorf("sync %d after Finish: want %d clock reads, got %d", sync, want, got)
		}
	}
}
