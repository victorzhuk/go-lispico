package lio

import (
	"context"
	"fmt"
	"os"

	"github.com/victorzhuk/go-lispico/core"
)

func (p *Plugin) envGet(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
	if ctx != nil && ctx.Err() != nil {
		return nil, fmt.Errorf("io/env-get: %w", ctx.Err())
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("io/env-get: requires 1 argument (key)")
	}

	keyArg, ok := args[0].(core.String)
	if !ok {
		return nil, fmt.Errorf("io/env-get: key must be string")
	}

	val, exists := os.LookupEnv(keyArg.V)
	if !exists {
		return core.Nil{}, nil
	}

	return core.String{V: val}, nil
}

func (p *Plugin) envSet(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
	if ctx != nil && ctx.Err() != nil {
		return nil, fmt.Errorf("io/env-set: %w", ctx.Err())
	}
	if len(args) != 2 {
		return nil, fmt.Errorf("io/env-set: requires 2 arguments (key, value)")
	}

	keyArg, ok := args[0].(core.String)
	if !ok {
		return nil, fmt.Errorf("io/env-set: key must be string")
	}

	valArg, ok := args[1].(core.String)
	if !ok {
		return nil, fmt.Errorf("io/env-set: value must be string")
	}

	if err := os.Setenv(keyArg.V, valArg.V); err != nil {
		return nil, fmt.Errorf("io/env-set: set %s: %w", keyArg.V, err)
	}

	return core.Nil{}, nil
}
