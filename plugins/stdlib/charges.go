package stdlib

import (
	"context"

	"github.com/victorzhuk/go-lispico/core"
)

// finishBuiltin settles the budget before anything leaves the builtin. A
// terminal sync error outranks a pending non-terminal one, so cancellation and
// deadline expiry cannot be masked by an ordinary builtin failure.
func finishBuiltin(b *core.BuiltinWorkBudget, v core.Value, err error) (core.Value, error) {
	if ferr := b.Flush(); ferr != nil && (err == nil || (core.IsTerminalEvalError(ferr) && !core.IsTerminalEvalError(err))) {
		return nil, ferr
	}
	if err != nil {
		return nil, err
	}
	return v, nil
}

// chargeFreshContainer charges the apply site for a container the builtin
// allocated itself.
func chargeFreshContainer(ctx context.Context, bytes int64) error {
	return core.ChargeGoFuncResultBytes(ctx, bytes)
}

// chargeBorrowedResult marks a result the caller borrowed from its subject
// rather than allocated, so the apply site does not charge its shallow size.
func chargeBorrowedResult(ctx context.Context) error {
	return core.ChargeGoFuncResultBytes(ctx, 0)
}
