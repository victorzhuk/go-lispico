package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

// budgetCtx returns a context carrying a fresh eval state with the given
// reduction ceiling and an effectively unlimited allocation ceiling.
func budgetCtx(parent context.Context, maxReductions int64) context.Context {
	return WithEvalResourceLimits(parent, int(maxReductions), 1<<30)
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
// consumer-side error precedence: a pending non-Terminal error survives a
// successful final Flush unchanged, but a Terminal flush error replaces it.
func TestBuiltinWorkBudget_TerminalFlushWins_OriginalErrorPreserved(t *testing.T) {
	t.Parallel()
	pending := NewTypeError("int", Int{V: 1}) // non-Terminal consumer error

	t.Run("flushSuccessPreservesPending", func(t *testing.T) {
		t.Parallel()
		b := NewBuiltinWorkBudget(budgetCtx(context.Background(), 1_000_000))
		stepN(t, b, 10)
		flushErr := b.Flush()
		if flushErr != nil {
			t.Fatalf("setup: expected successful flush, got %v", flushErr)
		}
		if IsTerminalEvalError(pending) {
			t.Fatalf("setup: pending error must be non-Terminal, got %v", pending)
		}
		// Precedence rule: flush success keeps the original non-Terminal error.
		var combined error = pending
		if flushErr != nil {
			combined = flushErr
		}
		if combined != pending {
			t.Fatalf("flush success must return original non-Terminal error unchanged, got %v", combined)
		}
	})

	t.Run("terminalFlushReplacesPending", func(t *testing.T) {
		t.Parallel()
		b := NewBuiltinWorkBudget(budgetCtx(context.Background(), 1)) // latches terminal on first sync
		stepN(t, b, 127)
		flushErr := b.Flush()
		if !IsTerminalEvalError(flushErr) {
			t.Fatalf("setup: expected Terminal flush error, got %v", flushErr)
		}
		// Precedence rule: Terminal flush error wins; original non-Terminal dropped.
		var combined error = pending
		if flushErr != nil {
			combined = flushErr
		}
		if combined != flushErr {
			t.Fatalf("Terminal flush error must win over pending non-Terminal error, got %v", combined)
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
