package json

import (
	"context"

	"github.com/victorzhuk/go-lispico/core"
)

// chargeDecodedResult marks the freshly-decoded result as self-accounted
// so the apply-site fallback shallow charge is skipped.
func chargeDecodedResult(ctx context.Context, deep int64) error {
	return core.ChargeGoFuncResultBytes(ctx, deep)
}
