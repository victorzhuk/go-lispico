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

			if !isTruthy(args[0]) {
				if len(args) > 1 {
					msg := args[1]
					if s, ok := msg.(core.String); ok {
						return nil, domainErrorf("assertion failed: %.200s", s.V)
					}
					rendered, err := core.ValueStringContext(ctx, msg)
					if err != nil {
						return nil, err
					}
					return nil, domainErrorf("assertion failed: %.200s", rendered)
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
