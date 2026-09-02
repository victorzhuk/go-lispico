package collections

import (
	"context"

	"github.com/victorzhuk/go-lispico/core"
)

// chargeFreshList charges the apply site for a list the kernel allocated
// itself, so the shallow cost is owned here rather than by the caller.
func chargeFreshList(ctx context.Context, n int) error {
	return core.ChargeGoFuncResultBytes(ctx, core.ListShallowBytes(n))
}
