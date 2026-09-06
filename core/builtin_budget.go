package core

import "context"

// BuiltinWorkBudget batches local work units performed inside a GoFunc so the
// shared eval state is touched only once every 128 units instead of per unit.
type BuiltinWorkBudget struct {
	st      *evalState
	ctx     context.Context
	pending int
	latched error
}

// NewBuiltinWorkBudget builds a budget over the eval state carried by ctx. It
// reads no clock and computes no deadline.
func NewBuiltinWorkBudget(ctx context.Context) *BuiltinWorkBudget {
	return &BuiltinWorkBudget{st: evalStateFrom(ctx), ctx: ctx}
}

// Step records one unit of local work, synchronizing with the shared eval
// state (reduction charge, deadline, caller cancellation) only when the 128th
// pending unit is reached.
func (b *BuiltinWorkBudget) Step() error {
	if b.latched != nil {
		return b.latched
	}
	b.pending++
	if b.pending < int(checkInterval) {
		return nil
	}
	return b.flushPending()
}

// Flush synchronizes any pending remainder (exactly once) and returns the
// latched sync error, if any. An empty successful flush is a no-op.
func (b *BuiltinWorkBudget) Flush() error {
	if b.latched != nil {
		return b.latched
	}
	if b.pending == 0 {
		return nil
	}
	return b.flushPending()
}

func (b *BuiltinWorkBudget) flushPending() error {
	n := int64(b.pending)
	b.pending = 0
	if err := b.st.chargeReductions(n); err != nil {
		b.latched = err
		return err
	}
	if !b.st.deadline.IsZero() && !nowFunc().Before(b.st.deadline) {
		b.latched = context.DeadlineExceeded
		return b.latched
	}
	if err := b.ctx.Err(); err != nil {
		b.latched = err
		return err
	}
	return nil
}
