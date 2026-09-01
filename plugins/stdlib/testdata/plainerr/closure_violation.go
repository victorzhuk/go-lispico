// Package plainerr holds deliberate plain-error constructions that exercise
// the scan in plain_error_ban_test.go. It lives under testdata, so the go tool
// never builds it, and nothing imports it.
package plainerr

import (
	"context"
	"fmt"

	"github.com/victorzhuk/go-lispico/core"
)

// registerClosureViolation builds the plain error inside the registered
// GoFunc body itself.
func registerClosureViolation(env *core.Env) error {
	return env.RegisterValue("closure-violation", core.GoFunc{
		Name: "closure-violation",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("closure-violation: requires exactly 1 argument")
			}
			return args[0], nil
		},
	}, true)
}
