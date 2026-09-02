package stdlib

import (
	"context"

	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/internal/collections"
)

func (p *Plugin) registerHigherOrder(env *core.Env) error {
	if err := env.RegisterValue("map", core.GoFunc{
		Name: "map",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 2 {
				return nil, arityErrorf("map: requires 2 arguments")
			}

			switch args[1].(type) {
			case core.List, core.Vector, core.Nil:
			default:
				return nil, typeErrorf("map: second argument must be collection")
			}

			return collections.MapSequences(ctx, eval, env, args[0], args[1:])
		},
	}, false); err != nil {
		return err
	}
	if err := env.RegisterValue("filter", core.GoFunc{
		Name: "filter",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 2 {
				return nil, arityErrorf("filter: requires 2 arguments")
			}

			items, ok := seqInput(args[1])
			if !ok {
				return nil, typeErrorf("filter: second argument must be collection")
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

			return core.NewList(results), nil
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("reduce", core.GoFunc{
		Name: "reduce",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) < 2 || len(args) > 3 {
				return nil, arityErrorf("reduce: requires 2 or 3 arguments")
			}

			collIdx := 1
			if len(args) == 3 {
				collIdx = 2
			}

			items, ok := seqInput(args[collIdx])
			if !ok {
				return nil, typeErrorf("reduce: last argument must be collection")
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
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("apply", core.GoFunc{
		Name: "apply",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) < 2 {
				return nil, arityErrorf("apply: requires at least 2 arguments")
			}

			fn := args[0]
			last := args[len(args)-1]

			tail, ok := seqInput(last)
			if !ok {
				return nil, typeErrorf("apply: last argument must be collection, got %T", last)
			}

			callArgs := append(args[1:len(args)-1], tail...)
			return eval.Apply(ctx, fn, callArgs, env)
		},
	}, false); err != nil {
		return err
	}
	return nil
}
