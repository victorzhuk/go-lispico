package stdlib

import (
	"context"
	"fmt"

	"github.com/victorzhuk/go-lispico/core"
)

func (p *Plugin) registerHigherOrder(env *core.Env) {
	env.Set("map", core.GoFunc{
		Name: "map",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("map: requires 2 arguments")
			}

			var items []core.Value
			switch c := args[1].(type) {
			case core.List:
				items = c.Items
			case core.Vector:
				items = c.Items
			default:
				return nil, fmt.Errorf("map: second argument must be collection")
			}

			results := make([]core.Value, len(items))
			for i, item := range items {
				r, err := eval.Apply(ctx, args[0], []core.Value{item}, env)
				if err != nil {
					return nil, err
				}
				results[i] = r
			}

			return core.List{Items: results}, nil
		},
	})

	env.Set("filter", core.GoFunc{
		Name: "filter",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("filter: requires 2 arguments")
			}

			var items []core.Value
			switch c := args[1].(type) {
			case core.List:
				items = c.Items
			case core.Vector:
				items = c.Items
			default:
				return nil, fmt.Errorf("filter: second argument must be collection")
			}

			var results []core.Value
			for _, item := range items {
				r, err := eval.Apply(ctx, args[0], []core.Value{item}, env)
				if err != nil {
					return nil, err
				}
				if isTruthy(r) {
					results = append(results, item)
				}
			}

			return core.List{Items: results}, nil
		},
	})

	env.Set("reduce", core.GoFunc{
		Name: "reduce",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) < 2 || len(args) > 3 {
				return nil, fmt.Errorf("reduce: requires 2 or 3 arguments")
			}

			var items []core.Value
			collIdx := 1
			if len(args) == 3 {
				collIdx = 2
			}

			switch c := args[collIdx].(type) {
			case core.List:
				items = c.Items
			case core.Vector:
				items = c.Items
			default:
				return nil, fmt.Errorf("reduce: last argument must be collection")
			}

			var acc core.Value
			startIdx := 0
			if len(args) == 3 {
				acc = args[1]
			} else if len(items) == 0 {
				return core.Nil{}, nil
			} else {
				acc = items[0]
				startIdx = 1
			}

			for _, item := range items[startIdx:] {
				var err error
				acc, err = eval.Apply(ctx, args[0], []core.Value{acc, item}, env)
				if err != nil {
					return nil, err
				}
			}

			return acc, nil
		},
	})

	env.Set("apply", core.GoFunc{
		Name: "apply",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) < 2 {
				return nil, fmt.Errorf("apply: requires at least 2 arguments")
			}

			fn := args[0]
			last := args[len(args)-1]

			var tail []core.Value
			switch c := last.(type) {
			case core.List:
				tail = c.Items
			case core.Vector:
				tail = c.Items
			default:
				return nil, fmt.Errorf("apply: last argument must be collection, got %T", last)
			}

			callArgs := append(args[1:len(args)-1], tail...)
			return eval.Apply(ctx, fn, callArgs, env)
		},
	})
}
