package plainerr

import (
	"context"
	"errors"

	"github.com/victorzhuk/go-lispico/core"
)

// registerHelperViolation constructs no error itself; the plain error is built
// one call deeper, which is the shape a closure-body-only check would miss.
func registerHelperViolation(env *core.Env) error {
	return env.RegisterValue("helper-violation", core.GoFunc{
		Name: "helper-violation",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if err := checkHelperArity(args); err != nil {
				return nil, err
			}
			return args[0], nil
		},
	}, true)
}

func checkHelperArity(args []core.Value) error {
	if len(args) != 1 {
		return errors.New("helper-violation: requires exactly 1 argument")
	}
	return nil
}
