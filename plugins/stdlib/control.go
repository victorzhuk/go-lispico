package stdlib

import (
	"context"
	"fmt"

	"github.com/victorzhuk/go-lispico/core"
)

func (p *Plugin) registerControl(env *core.Env) {
	env.Set("assert", core.GoFunc{
		Name: "assert",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) < 1 {
				return nil, fmt.Errorf("assert: requires at least 1 argument")
			}

			cond, err := eval.Eval(ctx, args[0], env)
			if err != nil {
				return nil, err
			}

			if !isTruthy(cond) {
				if len(args) > 1 {
					msg, err := eval.Eval(ctx, args[1], env)
					if err != nil {
						return nil, err
					}
					if s, ok := msg.(core.String); ok {
						return nil, fmt.Errorf("assertion failed: %s", s.V)
					}
					return nil, fmt.Errorf("assertion failed: %v", msg)
				}
				return nil, fmt.Errorf("assertion failed")
			}

			return core.Nil{}, nil
		},
	})
}
