package stdlib

import "github.com/victorzhuk/go-lispico/core"

// countAll charges once before the walk and once after it, never inside, while
// its row claims the loop itself is budgeted.
func countAll(args []core.Value, budget *core.BuiltinWorkBudget) (core.Value, error) {
	if err := budget.Step(); err != nil {
		return nil, err
	}

	total := 0
	for range args {
		total++
	}

	if err := budget.Step(); err != nil {
		return nil, err
	}
	return core.Int(total), nil
}
