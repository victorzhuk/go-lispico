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

			budget := core.NewBuiltinWorkBudget(ctx)
			// seqInput copied the whole subject before the loop is entered, so
			// that copy is billed here on top of the per-element work.
			for range items {
				if err := budget.Step(); err != nil {
					return finishBuiltin(budget, nil, err)
				}
			}

			var results []core.Value
			for _, item := range items {
				if err := budget.Step(); err != nil {
					return finishBuiltin(budget, nil, err)
				}
				r, err := eval.Apply(ctx, args[0], []core.Value{item}, env)
				if err != nil {
					return finishBuiltin(budget, nil, err)
				}
				if isTruthy(r) {
					if err := budget.Step(); err != nil {
						return finishBuiltin(budget, nil, err)
					}
					results = append(results, item)
				}
			}

			// Every retained element is borrowed from the subject; only the list
			// holding them is new.
			if err := chargeFreshContainer(ctx, core.ListShallowBytes(len(results))); err != nil {
				return finishBuiltin(budget, nil, err)
			}
			return finishBuiltin(budget, core.NewList(results), nil)
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

			budget := core.NewBuiltinWorkBudget(ctx)
			// seqInput copied the whole subject before the fold is entered, so
			// that copy is billed here on top of the per-element work.
			for range items {
				if err := budget.Step(); err != nil {
					return finishBuiltin(budget, nil, err)
				}
			}

			for _, item := range items[startIdx:] {
				if err := budget.Step(); err != nil {
					return finishBuiltin(budget, nil, err)
				}
				var err error
				acc, err = eval.Apply(ctx, args[0], []core.Value{acc, item}, env)
				if err != nil {
					return finishBuiltin(budget, nil, err)
				}
			}

			// The accumulator is either an element of the subject or what the
			// last callback dispatch already accounted for.
			if err := chargeBorrowedResult(ctx); err != nil {
				return finishBuiltin(budget, nil, err)
			}
			return finishBuiltin(budget, acc, nil)
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

			budget := core.NewBuiltinWorkBudget(ctx)
			// seqInput copied the tail out of the subject, and the assembly
			// below copies it once more into the callee's own slice.
			for range tail {
				if err := budget.Step(); err != nil {
					return finishBuiltin(budget, nil, err)
				}
			}
			for range len(args) - 2 + len(tail) {
				if err := budget.Step(); err != nil {
					return finishBuiltin(budget, nil, err)
				}
			}

			// The caller still owns the backing array behind args — under the VM
			// it is a window into the value stack — so the call arguments are
			// copied at exact capacity rather than appended onto a reslice of it.
			callArgs := make([]core.Value, 0, len(args)-2+len(tail))
			callArgs = append(callArgs, args[1:len(args)-1]...)
			callArgs = append(callArgs, tail...)

			res, err := eval.Apply(ctx, fn, callArgs, env)
			if err != nil {
				return finishBuiltin(budget, nil, err)
			}
			// The callee's dispatch already accounted for what it returned.
			if err := chargeBorrowedResult(ctx); err != nil {
				return finishBuiltin(budget, nil, err)
			}
			return finishBuiltin(budget, res, nil)
		},
	}, false); err != nil {
		return err
	}
	return nil
}
