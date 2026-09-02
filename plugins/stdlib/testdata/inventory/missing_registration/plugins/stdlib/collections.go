package stdlib

import "github.com/victorzhuk/go-lispico/core"

// countAll walks its arguments under the caller's budget. The walk is a real
// work phase and no WorkPhases row records it.
func countAll(args []core.Value, budget *core.BuiltinWorkBudget) (core.Value, error) {
	total := 0
	for range args {
		if err := budget.Step(); err != nil {
			return nil, err
		}
		total++
	}
	return core.Int(total), nil
}
