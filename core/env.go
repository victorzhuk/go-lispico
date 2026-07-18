package core

import (
	"sync"
	"sync/atomic"
)

// Cell is a binding's storage cell, shared by every resolver holding a
// reference to it so a rebind through one path is visible through all others
// without a re-lookup. Its fields are guarded by the owning Env's lock: writes
// go through Env.Set/SetCanonical/Delete under the write lock, reads through
// the Env (Get/GetCanonical/ReadCell) under the read lock. Storing the value
// inline keeps a rebind allocation-free — the VM caches the cell pointer, not
// the value, so it still avoids the map walk without boxing every write.
type Cell struct {
	v         Value // nil == tombstoned/unbound; guarded by the owning Env's lock
	canonical bool  // guarded by the owning Env's lock
}

// Env is a lexical scope: an immutable parent chain with a thread-safe local binding map.
// Reads walk up the chain; writes are local-only.
type Env struct {
	mu         sync.RWMutex
	parent     *Env
	vars       map[string]*Cell
	cell0      Cell // first binding's cell, inline to save a heap alloc per scope
	cell0Used  bool
	funcs      map[string]Value // function cell; nil until first SetFunc (Lisp-2 only)
	eval       Evaluator
	macroEpoch int           // bumped on each defmacro in this scope; used in bytecode cache key
	newNameGen atomic.Uint64 // bumped whenever a name is newly bound (or revived from tombstone) in vars
}

func NewEnv(parent *Env) *Env {
	e := &Env{
		parent: parent,
		vars:   make(map[string]*Cell),
	}
	if parent != nil {
		e.eval = parent.eval
	}
	return e
}

// localCell returns the cell owning name in this scope, creating it if absent.
// The first cell in each scope lives inline in the Env, so a scope that binds a
// single name (a per-call closure env, a one-binding let) needs no separate
// heap allocation. Caller holds the write lock.
func (e *Env) localCell(name string) *Cell {
	if cell, ok := e.vars[name]; ok {
		return cell
	}
	var cell *Cell
	if e.cell0Used {
		cell = &Cell{}
	} else {
		cell, e.cell0Used = &e.cell0, true
	}
	e.vars[name] = cell
	return cell
}

// Set binds name in this (local) scope. Overwriting a canonical binding
// removes the canonical marker, so a root-env rebind is detected as non-canonical.
func (e *Env) Set(name string, val Value) {
	e.mu.Lock()
	defer e.mu.Unlock()
	cell := e.localCell(name)
	if cell.v == nil {
		e.newNameGen.Add(1)
	}
	cell.v = val
	cell.canonical = false
}

// SetCanonical binds name as a canonical operator in this scope.
// It is intended ONLY for the stdlib plugin to register its builtins during
// engine initialization. Marking an arbitrary custom GoFunc as canonical will
// cause the bytecode VM to execute native opcode semantics for name instead of
// calling the provided function.
func (e *Env) SetCanonical(name string, val Value) {
	e.mu.Lock()
	defer e.mu.Unlock()
	cell := e.localCell(name)
	if cell.v == nil {
		e.newNameGen.Add(1)
	}
	cell.v = val
	cell.canonical = true
}

// GetCanonical resolves name like Get but also returns whether it is a canonical
// binding in its owning scope (any scope in the chain). Returns (value, found, canonical).
func (e *Env) GetCanonical(name string) (Value, bool, bool) {
	e.mu.RLock()
	var v Value
	var canon bool
	if cell, ok := e.vars[name]; ok {
		v, canon = cell.v, cell.canonical
	}
	e.mu.RUnlock()
	if v != nil {
		return v, true, canon
	}
	if e.parent != nil {
		return e.parent.GetCanonical(name)
	}
	return nil, false, false
}

// ReadCell returns the value, liveness, and canonical flag of a cell resolved
// from this env, read coherently under the read lock. The cell must be owned
// by this env — the VM caches only depth-0 (locally owned) resolutions, so the
// site's env is the cell's owner.
func (e *Env) ReadCell(c *Cell) (Value, bool, bool) {
	e.mu.RLock()
	v, canon := c.v, c.canonical
	e.mu.RUnlock()
	return v, v != nil, canon
}

// Cell resolves name to its owning cell by walking the scope chain, skipping
// tombstoned (deleted) bindings as if they were absent. Used by the VM to
// cache a global's storage cell across executions.
func (e *Env) Cell(name string) (*Cell, bool) {
	e.mu.RLock()
	cell, ok := e.vars[name]
	live := ok && cell.v != nil
	e.mu.RUnlock()
	if live {
		return cell, true
	}
	if e.parent != nil {
		return e.parent.Cell(name)
	}
	return nil, false
}

// CellLocal resolves name to its owning cell in this scope only, without
// walking to the parent. Used by the VM to guard a cache site: only a
// locally-owned cell is safe to cache by env identity, since a cell owned by
// an ancestor could later be shadowed by a new local binding of the same name.
func (e *Env) CellLocal(name string) (*Cell, bool) {
	e.mu.RLock()
	cell, ok := e.vars[name]
	live := ok && cell.v != nil
	e.mu.RUnlock()
	if live {
		return cell, true
	}
	return nil, false
}

// NameGen returns this scope's name-binding generation counter, bumped each
// time a name is newly bound (or revived from a tombstone) in vars. The VM
// compares it against a cached value to detect a shadowing bind that
// invalidates a cached cell resolution.
func (e *Env) NameGen() uint64 { return e.newNameGen.Load() }

// BumpMacroEpoch increments the macro epoch counter for this scope.
// Called after defmacro to invalidate bytecode caches that depend on
// macros defined in this scope. Safe for concurrent use.
func (e *Env) BumpMacroEpoch() {
	e.mu.Lock()
	e.macroEpoch++
	e.mu.Unlock()
}

// MacroEpoch returns the current macro epoch counter for this scope.
func (e *Env) MacroEpoch() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.macroEpoch
}

// Get walks the scope chain from innermost to outermost.
func (e *Env) Get(name string) (Value, bool) {
	e.mu.RLock()
	var v Value
	if cell, ok := e.vars[name]; ok {
		v = cell.v
	}
	e.mu.RUnlock()
	if v != nil {
		return v, true
	}
	if e.parent != nil {
		return e.parent.Get(name)
	}
	return nil, false
}

// SetFunc binds name in this scope's function cell (Lisp-2 only). The cell is
// allocated on first use so Lisp-1 scopes never carry it.
func (e *Env) SetFunc(name string, val Value) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.funcs == nil {
		e.funcs = make(map[string]Value)
	}
	e.funcs[name] = val
}

// GetFunc walks the scope chain reading the function cell (Lisp-2 only).
func (e *Env) GetFunc(name string) (Value, bool) {
	e.mu.RLock()
	val, ok := e.funcs[name]
	e.mu.RUnlock()
	if ok {
		return val, true
	}
	if e.parent != nil {
		return e.parent.GetFunc(name)
	}
	return nil, false
}

// Find returns the scope that owns name (for set!).
func (e *Env) Find(name string) (*Env, bool) {
	e.mu.RLock()
	cell, ok := e.vars[name]
	live := ok && cell.v != nil
	e.mu.RUnlock()
	if live {
		return e, true
	}
	if e.parent != nil {
		return e.parent.Find(name)
	}
	return nil, false
}

// Child creates a child scope with this env as parent.
func (e *Env) Child() *Env {
	return NewEnv(e)
}

// ChildVariadic creates a child scope binding params to args, with optional variadic rest param.
func (e *Env) ChildVariadic(params []Symbol, args []Value, variadic Symbol) (*Env, error) {
	child := e.Child()

	if variadic.V != "" {
		if len(args) < len(params) {
			return nil, NewArityError(len(params), len(args))
		}
		for i, param := range params {
			child.Set(param.V, args[i])
		}
		child.Set(variadic.V, List{Items: args[len(params):]})
	} else {
		if len(args) != len(params) {
			return nil, NewArityError(len(params), len(args))
		}
		for i, param := range params {
			child.Set(param.V, args[i])
		}
	}

	return child, nil
}

// Evaluator returns the engine bound to this scope (used by plugins for recursive eval).
func (e *Env) Evaluator() Evaluator {
	return e.eval
}

// SetEvaluator binds the evaluator to this scope (called by the runtime after NewEvaluator).
func (e *Env) SetEvaluator(eval Evaluator) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.eval = eval
}

// Delete removes name from this scope's local value and function cells.
// No-op if name is not bound locally. The value cell is tombstoned rather
// than removed from the map: a cell already cached by identity elsewhere
// (e.g. the VM's site cache) must observe the binding disappear, not keep
// serving a stale value.
func (e *Env) Delete(name string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if cell, ok := e.vars[name]; ok {
		cell.v = nil
		cell.canonical = false
	}
	delete(e.funcs, name)
}

// FuncNames returns a snapshot of the names bound in this scope's local
// function cell (Lisp-2 only). The order is unspecified. Parent bindings
// are not included.
func (e *Env) FuncNames() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.funcs == nil {
		return nil
	}
	names := make([]string, 0, len(e.funcs))
	for name := range e.funcs {
		names = append(names, name)
	}
	return names
}

// VarNames returns a snapshot of the names bound in this scope's local frame.
// Tombstoned (deleted) names are skipped. The order is unspecified. Parent
// bindings are not included.
func (e *Env) VarNames() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	names := make([]string, 0, len(e.vars))
	for name, cell := range e.vars {
		if cell.v != nil {
			names = append(names, name)
		}
	}
	return names
}

// MergeInto copies all bindings from this env into target.
// Does NOT copy parent bindings. Target is locked during merge.
func (e *Env) MergeInto(target *Env) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	for name, cell := range e.vars {
		if cell.v != nil {
			target.Set(name, cell.v)
		}
	}
	for name, val := range e.funcs {
		target.SetFunc(name, val)
	}
}
