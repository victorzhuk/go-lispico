package core

import (
	"context"
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
