package runtime

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/core/vm"
)

// Fn is a resolve-once handle to a named function in the engine's root
// environment. The zero value is not usable; construct it with Engine.Func.
// A Fn is safe for concurrent use.
type Fn struct {
	engine  *engineImpl
	name    string
	cell    *core.Cell
	counter *atomic.Int64
	value   core.Value
	cellVer uint64
}

// Func returns a reusable handle to name's current root binding cell. The
// cell's {value, version} snapshot is captured coherently now (under the env
// lock via Env.ReadCellSnapshot): Call serves it lock-free while the cell's
// atomic version still matches and re-reads under the lock on any mismatch.
func (e *engineImpl) Func(name string) (*Fn, error) {
	env := e.rootEnvPtr.Load()

	cell, ok := e.resolveFuncCell(env, name)
	if !ok {
		return nil, fmt.Errorf("undefined function: %s", name)
	}
	value, _, _, ver := env.ReadCellSnapshot(cell)
	return &Fn{engine: e, name: name, cell: cell, counter: e.stats.counterFor(name), value: value, cellVer: ver}, nil
}

// PinnedFn is a single-owner, zero-allocation handle to a named function in
// the engine's root environment. Unlike Fn, it owns a private VM created at
// Pin() time and bypasses the engine's VM pool on every Call: there is no
// per-call pool Get/Put, no per-call Reset, and no cross-goroutine sharing of
// the running VM. That trades concurrency for steady-state cost — a PinnedFn
// is built for a hot loop owned by one goroutine, not for fan-out across
// workers.
//
// Single-owner contract (enforced via an atomic Bool; misuse returns a typed
// *core.LispicoError with Code == core.CodeConcurrentUse, never a panic and
// never silent corruption):
//
//  1. NOT safe for concurrent use. A PinnedFn has one owner goroutine; if
//     another goroutine enters Call while a first Call is in flight, the
//     second entry is rejected with NewConcurrentUseError and the first call
//     runs to completion unaffected. The Go-Lisp boundary is shaped like
//     GopherLua's LState / goja's Runtime — one VM per goroutine, not one VM
//     shared by many.
//
//  2. NOT safe for re-entrant use through itself. A PinnedFn MUST NOT call
//     itself from within its own execution's GoFunc; nested calls must go
//     through Engine.Call (the pool path, which shares the resource budget
//     via HasEvalState). A re-entrant PinnedFn.Call is rejected with the same
//     typed error so the budget contract cannot be bypassed by accident.
//
// Observable semantics are otherwise identical to Fn.Call: the current
// binding resolves through the cell, a delete-then-call returns the same
// "undefined function: <name>" error shape, stats attribution and
// OnPluginCall callback events fire through the shared counter/callback
// gate, the lazy engine deadline arms the same way, and a re-entrant
// GoFunc shares the enclosing structural-depth and MaxEvalDepth budget
// across the VM boundary just like the pool path does.
//
// Lifecycle: a PinnedFn keeps the engine's root environment alive (the
// private VM holds a *core.Env pointer) while the handle lives. Engine.Close
// stops the file watcher but leaves rootEnv accessible, mirroring Fn.Call's
// post-Close behavior — calling Call on a PinnedFn after Close observes the
// same root-env state the engine did before Close. Callers that need
// guaranteed teardown should drop their PinnedFn references before Close.
type PinnedFn struct {
	engine  *engineImpl
	name    string
	cell    *core.Cell
	counter *atomic.Int64
	vm      *vm.VM
	inUse   atomic.Bool
	value   core.Value
	cellVer uint64
}

// Pin returns a single-owner PinnedFn for name's current root binding cell.
// The private VM is allocated NOW (the same New(globals, WithMaxDepth,
// WithEvaluator, WithMaxStructuralDepth) options the engine's pool uses) so
// Pin's steady-state allocation cost is paid once, not lazily on first Call,
// and Pin's caller never races with pool reuse of shared VM state.
// Pin returns nil when the engine was built without WithBytecode() — there
// is no VM to pin and callers on the tree-walker path keep using Fn.Call.
//
// Each Pin() call allocates an independent handle and VM, so multiple Pins
// from the same Fn can run on different goroutines without sharing state.
// Once handed out, a PinnedFn MUST be driven by exactly one goroutine — see
// the type doc for the misuse contract.
func (f *Fn) Pin() *PinnedFn {
	be := f.engine.bytecodeEvaluator
	if be == nil {
		// Without bytecode there is no VM to pin; callers can still use Fn.Call.
		return nil
	}
	v := vm.New(
		be.globals,
		vm.WithMaxDepth(be.maxDepth),
		vm.WithEvaluator(be),
		vm.WithMaxStructuralDepth(be.maxStructuralDepth),
	)
	return &PinnedFn{
		engine:  f.engine,
		name:    f.name,
		cell:    f.cell,
		counter: f.counter,
		vm:      v,
		value:   f.value,
		cellVer: f.cellVer,
	}
}

// Call invokes the function currently stored in the handle's binding cell.
//
// On the WithBytecode() path concurrent callers share the engine's VM pool,
// so a single Fn is safe for concurrent use across goroutines. On a
// fast-condition engine with a plain context Call takes the same lean shape
// Engine.Call does: the engine's private VM slot (pool fallback under
// contention), the lock-free versioned cell read, and a single recover-defer
// boundary with no eval-state or callback bookkeeping. Any other condition
// keeps the pool VM and the general boundary.
func (f *Fn) Call(ctx context.Context, args ...core.Value) (result core.Value, err error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	fast := f.engine.fastPath.Load() && !core.HasEvalState(ctx) && !core.HasEvalMeter(ctx)
	env := f.engine.rootEnvPtr.Load()

	var fn core.Value
	live := f.cell.Version() == f.cellVer && f.value != nil
	if live {
		fn = f.value
	} else {
		fn, live, _ = env.ReadCell(f.cell)
	}
	if !live {
		f.counter.Add(1)
		if f.engine.callbacksActive.Load() {
			f.engine.firePluginCallbacks(PluginCallEvent{Function: f.name, Duration: 0})
		}
		return nil, fmt.Errorf("undefined function: %s", f.name)
	}
	if be := f.engine.bytecodeEvaluator; be != nil {
		if fast {
			v, slot := f.engine.acquireCallVM(be)
			return f.engine.callBoundaryLean(ctx, f.name, fn, env, f.counter, args, v, be, slot)
		}
		v := be.vmPool.Get().(*vm.VM)
		v.Reset()
		defer func() {
			if r := recover(); r != nil {
				result = nil
				err = core.NewPanicError(f.name, r)
			}
			if err != nil {
				v.Reset()
			}
			be.vmPool.Put(v)
		}()
		return f.engine.callBoundary(ctx, f.name, fn, env, f.counter, args, v, false)
	}
	return f.engine.callBoundary(ctx, f.name, fn, env, f.counter, args, nil, false)
}

// Call invokes the function currently stored in the handle's binding cell,
// reusing the private VM the handle was constructed with. The preamble
// (ctx check, rootEnv read, cell live-check, counter bump on undefined,
// undefined-callback) is byte-identical to Fn.Call so both handle types
// observe the same observable contract — only the VM acquisition and reset
// strategy differ. On a fast-condition engine with a plain context the cell
// is read through the lock-free versioned snapshot and the boundary is a
// single recover-defer: no eval-state chain, no clock reads, no callback
// probes. Any other condition keeps the general boundary.
//
// Concurrent entry returns a typed *core.LispicoError with
// Code == core.CodeConcurrentUse; the offending second caller observes no
// mutation of the handle, and the first call runs to completion. A
// re-entrant Call from within the handle's own execution returns the same
// typed error so the re-entrancy budget contract cannot be bypassed by
// accident.
//
// A panicking user GoFunc is recovered, the handle's VM is reset fully
// (the steady-state ResetIncremental cannot be trusted after a panic), and
// the panic value is wrapped in a typed LispicoError that returns to the
// caller. The handle stays usable; the next Call succeeds.
func (p *PinnedFn) Call(ctx context.Context, args ...core.Value) (result core.Value, err error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if !p.inUse.CompareAndSwap(false, true) {
		return nil, core.NewConcurrentUseError(p.name)
	}

	fast := p.engine.fastPath.Load() && !core.HasEvalState(ctx) && !core.HasEvalMeter(ctx)
	if fast {
		defer func() {
			if r := recover(); r != nil {
				result = nil
				err = core.NewPanicError(p.name, r)
			}
			if err != nil {
				p.vm.Reset()
			} else if resetErr := p.vm.ResetIncremental(); resetErr != nil {
				p.vm.Reset()
			}
			p.counter.Add(1)
			p.inUse.Store(false)
		}()

		env := p.engine.rootEnvPtr.Load()
		var fn core.Value
		live := p.cell.Version() == p.cellVer && p.value != nil
		if live {
			fn = p.value
		} else {
			fn, live, _ = env.ReadCell(p.cell)
		}
		if !live {
			return nil, fmt.Errorf("undefined function: %s", p.name)
		}
		return p.engine.bytecodeEvaluator.applyOnVM(p.vm, ctx, fn, args, env, p.engine.config.timeout)
	}

	defer func() {
		r := recover()
		if r != nil {
			err = core.NewPanicError(p.name, r)
			result = nil
		}
		if err != nil {
			p.vm.Reset()
		} else if resetErr := p.vm.ResetIncremental(); resetErr != nil {
			err = core.NewVMStateError(p.name, resetErr)
			result = nil
		}
		p.inUse.Store(false)
	}()

	env := p.engine.rootEnvPtr.Load()
	var fn core.Value
	live := p.cell.Version() == p.cellVer && p.value != nil
	if live {
		fn = p.value
	} else {
		fn, live, _ = env.ReadCell(p.cell)
	}
	if !live {
		p.counter.Add(1)
		if p.engine.callbacksActive.Load() {
			p.engine.firePluginCallbacks(PluginCallEvent{Function: p.name, Duration: 0})
		}
		return nil, fmt.Errorf("undefined function: %s", p.name)
	}
	return p.engine.callBoundary(ctx, p.name, fn, env, p.counter, args, p.vm, false)
}
