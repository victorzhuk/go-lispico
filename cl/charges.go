package cl

import (
	"context"

	"github.com/victorzhuk/go-lispico/core"
)

// finishAdapter settles the budget before anything leaves the adapter. A
// terminal sync error outranks a pending non-terminal one, so cancellation and
// deadline expiry cannot be masked by an ordinary adapter failure.
func finishAdapter(b *core.BuiltinWorkBudget, v core.Value, err error) (core.Value, error) {
	if err != nil {
		return nil, b.Finish(err)
	}
	if ferr := b.Flush(); ferr != nil {
		return nil, ferr
	}
	return v, nil
}

// chargeFreshSequence charges the apply site for a sequence the adapter
// allocated itself. The concrete container decides the shallow size, so the
// caller states which one it built rather than the size it costs.
func chargeFreshSequence(ctx context.Context, n int, asVector bool) error {
	bytes := core.ListShallowBytes(n)
	if asVector {
		bytes = core.VectorShallowBytes(n)
	}
	return core.ChargeGoFuncResultBytes(ctx, bytes)
}

// chargeBorrowedResult marks a result the adapter borrowed from its subject
// rather than allocated, so the apply site does not charge its shallow size.
func chargeBorrowedResult(ctx context.Context) error {
	return core.ChargeGoFuncResultBytes(ctx, 0)
}
