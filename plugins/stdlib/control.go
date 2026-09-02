package stdlib

import (
	"context"

	"github.com/victorzhuk/go-lispico/core"
)

func (p *Plugin) registerControl(env *core.Env) error {
	if err := env.RegisterValue("assert", core.GoFunc{
		Name: "assert",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) < 1 {
				return nil, arityErrorf("assert: requires at least 1 argument")
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
						return nil, domainErrorf("assertion failed: %.200s", s.V)
					}
					return nil, domainErrorf("assertion failed: %.200v", msg)
				}
				return nil, domainErrorf("assertion failed")
			}

			return core.Nil{}, nil
		},
	}, false); err != nil {
		return err
	}
	return nil
}
