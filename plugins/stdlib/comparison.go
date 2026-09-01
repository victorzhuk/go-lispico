package stdlib

import (
	"context"
	"fmt"

	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/internal/collections"
)

func (p *Plugin) registerComparison(env *core.Env) error {
	if err := env.RegisterValue("=", core.GoFunc{
		Name: "=",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) == 0 {
				return nil, fmt.Errorf("=: requires at least 1 argument")
			}
			for _, arg := range args[1:] {
				if !args[0].Equals(arg) {
					return core.BoxBool(false), nil
				}
			}
			return core.BoxBool(true), nil
		},
	}, true); err != nil {
		return err
	}

	if err := env.RegisterValue("<", core.GoFunc{Name: "<", Fn: orderingFunc("<", func(c int) bool { return c < 0 })}, true); err != nil {
		return err
	}
	if err := env.RegisterValue(">", core.GoFunc{Name: ">", Fn: orderingFunc(">", func(c int) bool { return c > 0 })}, true); err != nil {
		return err
	}
	if err := env.RegisterValue("<=", core.GoFunc{Name: "<=", Fn: orderingFunc("<=", func(c int) bool { return c <= 0 })}, true); err != nil {
		return err
	}
	if err := env.RegisterValue(">=", core.GoFunc{Name: ">=", Fn: orderingFunc(">=", func(c int) bool { return c >= 0 })}, true); err != nil {
		return err
	}
	return nil
}

// orderingFunc builds a variadic monotonic chain: every adjacent pair must
// satisfy ok, so (< 1 2 3) is true and (< 1 3 2) is false.
func orderingFunc(name string, ok func(cmp int) bool) func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
	return func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
		if len(args) == 0 {
			return nil, fmt.Errorf("%s: requires at least 1 argument", name)
		}
		if _, err := collections.ToFloat(name, args[0]); err != nil {
			return nil, err
		}
		for i := 1; i < len(args); i++ {
			cmp, err := collections.NumCmp(name, args[i-1], args[i])
			if err != nil {
				return nil, err
			}
			if !ok(cmp) {
				return core.BoxBool(false), nil
			}
		}
		return core.BoxBool(true), nil
	}
}

