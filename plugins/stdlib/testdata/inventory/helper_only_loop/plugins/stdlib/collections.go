package stdlib

import "github.com/victorzhuk/go-lispico/core"

// countAll delegates the whole walk to countElements, so its budgeted row names
// a function holding neither the loop nor a budget.
func countAll(args []core.Value, budget *core.BuiltinWorkBudget) (core.Value, error) {
	return countElements(args, budget)
}

func countElements(args []core.Value, budget *core.BuiltinWorkBudget) (core.Value, error) {
	total := 0
	for range args {
		if err := budget.Step(); err != nil {
			return nil, err
		}
		total++
	}
	return core.Int(total), nil
}
