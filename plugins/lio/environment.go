package lio

import (
	"context"
	"fmt"
	"os"

	"github.com/victorzhuk/go-lispico/core"
)

// envGet and envSet keep script-visible environment changes scoped to this
// Plugin instance: writes land in envOverlay only, never in the real process
// environment, so two engines (or concurrent scripts sharing one) cannot
// clobber each other's or the host's variables. Reads check the overlay
// first and fall through to the real process environment.

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

	p.envMu.RLock()
	val, overlaid := p.envOverlay[keyArg.V]
	p.envMu.RUnlock()

	if overlaid {
		return core.String{V: val}, nil
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

	p.envMu.Lock()
	p.envOverlay[keyArg.V] = valArg.V
	p.envMu.Unlock()

	return core.Nil{}, nil
}
