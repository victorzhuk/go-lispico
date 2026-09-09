package core

import (
	"context"
	"fmt"
	"time"
)

// readerBudget is the per-read state the guarded reader entry point installs
// on its scratch. It batches reader work against the evaluation ledger carried
// by ctx and settles it at explicit checkpoints. It deliberately does not go
// through PollEvalState or pollCancel: those batch and charge evaluator work of
// their own, which would both delay observation here and add reductions the
// reader's charge table does not document.
//
// A nil *readerBudget is the absent budget: the context-free reader keeps its
// exact current behavior because every guarded site is a no-op without one.
type readerBudget struct {
	ctx      context.Context
	meter    EvalMeter
	deadline time.Time
	pending  int64
	failed   error
}

func newReaderBudget(ctx context.Context) *readerBudget {
	return &readerBudget{
		ctx:      ctx,
		meter:    EvalMeterFrom(ctx),
		deadline: EvalDeadlineFrom(ctx),
	}
}

// checkpoint settles the work accumulated since the previous checkpoint and
// re-reads the terminal state. Pending work is charged exactly once; the
// deadline and the caller's context are consulted even when nothing is
// pending, so a read that starts cancelled — empty input included — fails
// before doing any work. The first terminal failure is sticky: once observed
// it is reported by every later checkpoint without charging again.
func (b *readerBudget) checkpoint() error {
	if b == nil {
		return nil
	}
	if b.failed != nil {
		return b.failed
	}

	n := b.pending
	b.pending = 0
	if err := b.meter.ChargeReductions(n); err != nil {
		b.failed = err
		return err
	}
	if !b.deadline.IsZero() && !nowFunc().Before(b.deadline) {
		b.failed = context.DeadlineExceeded
		return b.failed
	}
	if err := b.ctx.Err(); err != nil {
		b.failed = err
		return err
	}
	return nil
}

// work records n reduction units of interruptible reader work. It settles at
// every checkInterval boundary rather than at the end of the run, so a span
// far larger than the interval — a long copy, a long token, a long comment —
// is still interrupted within checkInterval units of work.
func (b *readerBudget) work(n int64) error {
	if b == nil {
		return nil
	}
	if b.failed != nil {
		return b.failed
	}
	for n > 0 {
		room := checkInterval - b.pending
		if n < room {
			b.pending += n
			return nil
		}
		b.pending += room
		n -= room
		if err := b.checkpoint(); err != nil {
			return err
		}
	}
	return nil
}

// err reports the terminal failure already observed, without charging or
// re-reading anything.
func (b *readerBudget) err() error {
	if b == nil {
		return nil
	}
	return b.failed
}

// settle closes out a read that is returning err. The terminal state is read
// one last time first and outranks err: a cancelled, expired or exhausted read
// truncates its own input, and the syntax error that truncation produces must
// never be what the caller sees.
func (b *readerBudget) settle(err error) error {
	if b == nil {
		return err
	}
	if failed := b.checkpoint(); failed != nil {
		return failed
	}
	return err
}

// admitConversion pre-admits an opaque numeric conversion of n bytes. strconv
// runs uninterrupted once entered, so the token is charged before entry and
// refused outright when it alone would claim more than a third of the reduction
// budget; earlier reader work tightens that further, since the charge below has
// to fit in what the budget has left. The diagnostic reports lengths only —
// the token that provoked it can be arbitrarily long.
func (b *readerBudget) admitConversion(n int64) error {
	if b == nil {
		return nil
	}
	if b.failed != nil {
		return b.failed
	}
	if limit := b.meter.Snapshot().MaxReductions / 3; limit > 0 && n > limit {
		b.failed = NewResourceLimitError(
			fmt.Sprintf("numeric token of %d bytes exceeds the reader conversion limit of %d", n, limit),
		)
		return b.failed
	}
	return b.work(n)
}
