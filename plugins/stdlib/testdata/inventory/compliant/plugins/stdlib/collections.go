package stdlib

import (
	"context"

	"github.com/victorzhuk/go-lispico/core"
)

// lastOf walks a sequence under its own budget and settles it on every path.
// Every phase it runs and every branch it returns carries a row.
func lastOf(ctx context.Context, args []core.Value) (core.Value, error) {
	budget := core.NewBuiltinWorkBudget(ctx)

	var last core.Value
	for _, arg := range args {
		if err := budget.Step(); err != nil {
			return nil, err
		}
		last = arg
	}

	if err := budget.Flush(); err != nil {
		return nil, err
	}
	return last, nil
}
