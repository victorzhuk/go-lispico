package stdlib

import (
	"context"

	"github.com/victorzhuk/go-lispico/core"
)

// countAll opens a work budget and leaves through a bare return, so the pending
// steps are never settled on the success path.
func countAll(ctx context.Context, args []core.Value) (core.Value, error) {
	budget := core.NewBuiltinWorkBudget(ctx)

	total := 0
	for range args {
		if err := budget.Step(); err != nil {
			return nil, err
		}
		total++
	}
	return core.Int(total), nil
}
