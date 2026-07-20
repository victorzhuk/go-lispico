package runtime

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/victorzhuk/go-lispico/core"
)

// Fn is a resolve-once handle to a named function in the engine's root
// environment. The zero value is not usable; construct it with Engine.Func.
// A Fn is safe for concurrent use.
type Fn struct {
	engine  *engineImpl
	name    string
	cell    *core.Cell
	counter *atomic.Int64
}

// Func returns a reusable handle to name's current root binding cell.
func (e *engineImpl) Func(name string) (*Fn, error) {
	e.mu.RLock()
	env := e.rootEnv
	e.mu.RUnlock()

	var cell *core.Cell
	var ok bool
	if e.config.dialect.IsLisp2() {
		cell, ok = env.FuncCell(name)
	} else {
		cell, ok = env.Cell(name)
	}
	if !ok {
		return nil, fmt.Errorf("undefined function: %s", name)
	}
	return &Fn{engine: e, name: name, cell: cell, counter: e.stats.counterFor(name)}, nil
}

// Call invokes the function currently stored in the handle's binding cell.
func (f *Fn) Call(ctx context.Context, args ...core.Value) (core.Value, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	f.engine.mu.RLock()
	env := f.engine.rootEnv
	f.engine.mu.RUnlock()

	fn, live, _ := env.ReadCell(f.cell)
	if !live {
		f.counter.Add(1)
		if f.engine.callbacksActive.Load() {
			f.engine.firePluginCallbacks(PluginCallEvent{Function: f.name, Duration: 0})
		}
		return nil, fmt.Errorf("undefined function: %s", f.name)
	}
	return f.engine.callBoundary(ctx, f.name, fn, env, f.counter, args)
}
