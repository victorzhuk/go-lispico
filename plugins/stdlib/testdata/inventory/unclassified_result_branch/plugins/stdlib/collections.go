package stdlib

import "github.com/victorzhuk/go-lispico/core"

// collectAll hands the caller a freshly built slice; its row classes the branch
// as fresh-container without naming the charge for it.
func collectAll(args []core.Value, budget *core.BuiltinWorkBudget) ([]core.Value, error) {
	res := make([]core.Value, 0, len(args))
	for _, arg := range args {
		if err := budget.Step(); err != nil {
			return nil, err
		}
		res = append(res, arg)
	}
	return res, nil
}
