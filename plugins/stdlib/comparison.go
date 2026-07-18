package stdlib

import (
	"context"
	"fmt"

	"github.com/victorzhuk/go-lispico/core"
)

func (p *Plugin) registerComparison(env *core.Env) {
	env.SetCanonical("=", core.GoFunc{
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
	})

	env.SetCanonical("<", core.GoFunc{Name: "<", Fn: orderingFunc("<", func(c int) bool { return c < 0 })})
	env.SetCanonical(">", core.GoFunc{Name: ">", Fn: orderingFunc(">", func(c int) bool { return c > 0 })})
	env.SetCanonical("<=", core.GoFunc{Name: "<=", Fn: orderingFunc("<=", func(c int) bool { return c <= 0 })})
	env.SetCanonical(">=", core.GoFunc{Name: ">=", Fn: orderingFunc(">=", func(c int) bool { return c >= 0 })})
}

// orderingFunc builds a variadic monotonic chain: every adjacent pair must
// satisfy ok, so (< 1 2 3) is true and (< 1 3 2) is false.
func orderingFunc(name string, ok func(cmp int) bool) func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
	return func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
		if len(args) == 0 {
			return nil, fmt.Errorf("%s: requires at least 1 argument", name)
		}
		if _, err := toFloat(name, args[0]); err != nil {
			return nil, err
		}
		for i := 1; i < len(args); i++ {
			cmp, err := numCmp(name, args[i-1], args[i])
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

// numCmp compares two numbers, returning -1, 0, or 1. An int-int pair is
// compared exactly; a mixed pair promotes to float like arithmetic does.
func numCmp(name string, a, b core.Value) (int, error) {
	ai, aInt := a.(core.Int)
	bi, bInt := b.(core.Int)
	if aInt && bInt {
		switch {
		case ai.V < bi.V:
			return -1, nil
		case ai.V > bi.V:
			return 1, nil
		}
		return 0, nil
	}

	af, err := toFloat(name, a)
	if err != nil {
		return 0, err
	}
	bf, err := toFloat(name, b)
	if err != nil {
		return 0, err
	}
	switch {
	case af < bf:
		return -1, nil
	case af > bf:
		return 1, nil
	}
	return 0, nil
}

func toFloat(name string, v core.Value) (float64, error) {
	switch n := v.(type) {
	case core.Int:
		return float64(n.V), nil
	case core.Float:
		return n.V, nil
	default:
		return 0, fmt.Errorf("%s: expected number, got %T", name, v)
	}
}
