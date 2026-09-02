package cl

import (
	"context"

	"github.com/victorzhuk/go-lispico/core"
)

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
