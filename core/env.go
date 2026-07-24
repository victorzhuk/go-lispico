package core

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
)

// Cell is a binding's storage cell, shared by every resolver holding a
// reference to it so a rebind through one path is visible through all others
// without a re-lookup. Its fields are guarded by the owning Env's lock: writes
// go through Env.Set/SetCanonical/SetFunc/SetFuncCanonical/Delete under the
// write lock, reads through the Env under the read lock. Storing the value
// inline keeps a rebind allocation-free — the VM caches the cell pointer, not
// the value, so it still avoids the map walk without boxing every write.
type Cell struct {
	v             Value // nil == tombstoned/unbound; guarded by the owning Env's lock
	canonical     bool  // guarded by the owning Env's lock
	version       atomic.Uint64
	retainedMeter sessionMeter
	retainedBytes int64
	rebuilt       bool
}

// Version returns the cell mutation version.
func (c *Cell) Version() uint64 { return c.version.Load() }

// LazyLayer is an optional per-env miss-path fallback consulted when a name
// is not bound in this scope (and its ancestors). The runtime installs one
// to defer stdlib binding creation behind a per-name first touch.
//
// LookupAndMaterialize returns (value, found, canonical) and is responsible
// for installing the binding into env (via Set/SetCanonical/SetFunc/
// SetFuncCanonical) before returning true, so later lookups hit the cell
// directly and the layer is never consulted again for that name.
//
// TombstoneForDelete is called from env.Delete so a later miss does not
// resurrect a name the caller explicitly removed. It is per-env state.
//
// RegisterValue asks the layer to defer a Go binding instead of binding it
// now; RegisterSource does the same for a pure-Lisp definition and reports
// whether the layer accepted it (false means: bind/eval eagerly).
//
// ForceAll materializes every deferred name; enumeration surfaces
// (VarNames/FuncNames) call it so they observe the full plugin surface,
// paying a one-time cost comparable to eager load.
//
// The layer must be safe for concurrent use.
type LazyLayer interface {
	LookupAndMaterialize(env *Env, name string) (Value, bool, bool)
	TombstoneForDelete(env *Env, name string)
	RegisterValue(env *Env, name string, val Value, canonical bool) error
	RegisterSource(env *Env, name, source string, reusable bool) bool
	ForceAll(env *Env)
}

// Env is a lexical scope: an immutable parent chain with a thread-safe local binding map.
// Reads walk up the chain; writes are local-only.
type Env struct {
	mu               sync.RWMutex
	parent           *Env
	vars             map[string]*Cell
	cell0            Cell // first binding's cell, inline to save a heap alloc per scope
	cell0Used        bool
	funcs            map[string]*Cell // function cell; nil until first SetFunc (Lisp-2 only)
	eval             Evaluator
	retainedMeter    sessionMeter
	macroEpoch       int           // bumped on each defmacro in this scope; used in bytecode cache key
	newNameGen       atomic.Uint64 // bumped whenever a name is newly bound (or revived from tombstone) in vars
	lazyLayer        atomic.Pointer[LazyLayer]
	retainedBytes    int64
	retainedSlots    int64
	maxRetainedBytes int64
	maxRetainedSlots int64
}

// SetLazyLayer installs (or clears, on nil) the env's miss-path fallback.
func (e *Env) SetLazyLayer(layer LazyLayer) {
	if layer == nil {
		e.lazyLayer.Store(nil)
		return
	}
	e.lazyLayer.Store(&layer)
}

// LazyLayer returns the installed miss-path fallback, or nil.
func (e *Env) LazyLayer() LazyLayer {
	if p := e.lazyLayer.Load(); p != nil {
		return *p
	}
	return nil
}

// SetRetainedMeter binds the meter that owns this scope's retained capacity.
func (e *Env) SetRetainedMeter(m any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.retainedMeter, _ = m.(sessionMeter)
}

// RetainedMeter returns the retained-capacity meter bound to this scope.
func (e *Env) RetainedMeter() any {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.retainedMeter
}

// RegisterValue binds name through the lazy layer when one is installed,
// deferring the binding behind first touch; otherwise it binds the value
// cell immediately (applyVocabulary's bridge mirrors the function cell).
func (e *Env) RegisterValue(name string, val Value, canonical bool) error {
	return e.RegisterValueWithContext(context.Background(), name, val, canonical)
}

func (e *Env) RegisterValueWithContext(ctx context.Context, name string, val Value, canonical bool) error {
	if layer := e.LazyLayer(); layer != nil {
		return layer.RegisterValue(e, name, val, canonical)
	}
	if canonical {
		return e.SetCanonicalWithContext(ctx, name, val)
	}
	return e.SetWithContext(ctx, name, val)
}

// RegisterSource asks the lazy layer to defer a pure-Lisp definition of
// name behind first touch and reports whether the layer accepted it. With
// no layer (or a disabled one) it returns false and the caller evaluates
// the source eagerly.
func (e *Env) RegisterSource(name, source string, reusable bool) bool {
	if layer := e.LazyLayer(); layer != nil {
		return layer.RegisterSource(e, name, source, reusable)
	}
	return false
}

// HasLive reports whether name has a live (non-tombstoned) binding in this
// scope's value cell, without consulting the lazy layer or parent scopes.
func (e *Env) HasLive(name string) bool {
	e.mu.RLock()
	cell, ok := e.vars[name]
	live := ok && cell.v != nil
	e.mu.RUnlock()
	return live
}

// HasLiveFunc is HasLive for the function cell (Lisp-2 only).
func (e *Env) HasLiveFunc(name string) bool {
	e.mu.RLock()
	cell, ok := e.funcs[name]
	live := ok && cell.v != nil
	e.mu.RUnlock()
	return live
}

func NewEnv(parent *Env) *Env {
	e := &Env{
		parent: parent,
		vars:   make(map[string]*Cell),
	}
	if parent != nil {
		e.eval = parent.eval
		e.maxRetainedBytes = parent.maxRetainedBytes
		e.maxRetainedSlots = parent.maxRetainedSlots
	}
	return e
}

// NewEnvWithRetainedLimits creates an Env with retained-state capacity limits.
func NewEnvWithRetainedLimits(parent *Env, maxRetainedBytes, maxRetainedSlots int64) *Env {
	e := NewEnv(parent)
	e.maxRetainedBytes = maxRetainedBytes
	e.maxRetainedSlots = maxRetainedSlots
	return e
}

func retainedBindingBytes(name string, val Value) int64 {
	return MeterEnvMapEntryBytes + MeterEnvCellBytes + StringShallowBytes(len(name)) + ValueShallowBytes(val)
}

// RetainedBindingBytes returns the shallow retained-size of a single env binding.
func RetainedBindingBytes(name string, val Value) int64 {
	return retainedBindingBytes(name, val)
}

func (e *Env) reserveRetainedBindings(bytes, slots int64) error {
	nextBytes := e.retainedBytes + bytes
	nextSlots := e.retainedSlots + slots
	if e.maxRetainedBytes > 0 && nextBytes > e.maxRetainedBytes {
		return NewResourceLimitError("retained state capacity limit exceeded")
	}
	if e.maxRetainedSlots > 0 && nextSlots > e.maxRetainedSlots {
		return NewResourceLimitError("retained state capacity limit exceeded")
	}
	return nil
}

func (e *Env) chargeRetainedMeter(meter sessionMeter, bytes, slots int64) error {
	if meter == nil || (bytes <= 0 && slots <= 0) {
		return nil
	}
	if err := meter.ChargeRetained(bytes, slots); err != nil {
		return NewResourceLimitError(fmt.Sprintf("retained meter: %v", err))
	}
	return nil
}

func (e *Env) activeRetainedMeter(ctx context.Context) sessionMeter {
	if meter := sessionMeterFromContext(ctx); meter != nil {
		return meter
	}
	return e.retainedMeter
}

func (e *Env) prepareFreshRetained(ctx context.Context, bytes, slots int64) (sessionMeter, *evalState, bool, error) {
	if bytes == 0 && slots == 0 {
		return nil, nil, false, nil
	}
	if err := e.reserveRetainedBindings(bytes, slots); err != nil {
		return nil, nil, false, err
	}
	meter := e.activeRetainedMeter(ctx)
	st, hasState := ctx.Value(evalStateKey{}).(*evalState)
	pending := hasState && meter != nil
	if !pending {
		if err := e.chargeRetainedMeter(meter, bytes, slots); err != nil {
			return nil, nil, false, err
		}
	}
	e.retainedBytes += bytes
	e.retainedSlots += slots
	return meter, st, pending, nil
}

func recordFreshRetained(st *evalState, pending bool, env *Env, cell *Cell, meter sessionMeter, bytes int64) {
	if bytes == 0 {
		return
	}
	cell.rebuilt = false
	if pending {
		st.pendingCellAllocs = append(st.pendingCellAllocs, pendingCellAlloc{
			env:   env,
			cell:  cell,
			meter: meter,
			bytes: bytes,
			slots: 1,
		})
		st.retainedBytes += bytes
		st.retainedSlots++
		return
	}
	cell.retainedMeter = meter
	cell.retainedBytes = bytes
}

// SetBoth binds name in both value and function cells.
func (e *Env) SetBoth(name string, val Value) error {
	return e.SetBothWithContext(context.Background(), name, val)
}

// SetBothCanonical binds name in both value and function cells as canonical.
func (e *Env) SetBothCanonical(name string, val Value) error {
	return e.SetBothCanonicalWithContext(context.Background(), name, val)
}

// SetBothWithContext binds name in both value and function cells.
func (e *Env) SetBothWithContext(ctx context.Context, name string, val Value) error {
	return e.setBoth(ctx, name, val, false)
}

// SetBothCanonicalWithContext binds name in both value and function cells as canonical.
func (e *Env) SetBothCanonicalWithContext(ctx context.Context, name string, val Value) error {
	return e.setBoth(ctx, name, val, true)
}

func (e *Env) setBoth(ctx context.Context, name string, val Value, canonical bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	varCell, varCellExists := e.vars[name]
	funcCell, funcCellExists := e.funcs[name]
	newSlots := int64(0)
	if !varCellExists {
		newSlots++
	}
	if !funcCellExists {
		newSlots++
	}
	b := retainedBindingBytes(name, val)
	meter, st, pending, err := e.prepareFreshRetained(ctx, b*newSlots, newSlots)
	if err != nil {
		return err
	}

	if !varCellExists {
		varCell = e.localCell(name)
	}
	if !funcCellExists {
		funcCell = e.localFuncCell(name)
	}

	if varCell.v == nil {
		e.newNameGen.Add(1)
	}
	varCell.v = val
	varCell.canonical = canonical
	varCell.version.Add(1)
	if !varCellExists {
		recordFreshRetained(st, pending, e, varCell, meter, b)
	}

	funcCell.v = val
	funcCell.canonical = canonical
	funcCell.version.Add(1)
	if !funcCellExists {
		recordFreshRetained(st, pending, e, funcCell, meter, b)
	}

	return nil
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

// localFuncCell returns the function-cell owning name in this scope, creating it if
// absent. Caller holds the write lock.
func (e *Env) localFuncCell(name string) *Cell {
	if cell, ok := e.funcs[name]; ok {
		return cell
	}
	if e.funcs == nil {
		e.funcs = make(map[string]*Cell)
	}
	cell := &Cell{}
	e.funcs[name] = cell
	return cell
}

// Set binds name in this (local) scope. Overwriting a canonical binding
// removes the canonical marker, so a root-env rebind is detected as non-canonical.
func (e *Env) Set(name string, val Value) error {
	return e.SetWithContext(context.Background(), name, val)
}

func (e *Env) SetWithContext(ctx context.Context, name string, val Value) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	cell, ok := e.vars[name]
	b := retainedBindingBytes(name, val)
	var meter sessionMeter
	var st *evalState
	var pending bool
	if !ok {
		var err error
		meter, st, pending, err = e.prepareFreshRetained(ctx, b, 1)
		if err != nil {
			return err
		}
		cell = e.localCell(name)
	}
	if cell.v == nil {
		e.newNameGen.Add(1)
	}
	cell.v = val
	cell.canonical = false
	cell.version.Add(1)
	if !ok {
		recordFreshRetained(st, pending, e, cell, meter, b)
	}
	return nil
}

// ReplaceCell installs a fresh local value cell for name.
func (e *Env) ReplaceCell(name string, val Value) error {
	return e.ReplaceCellWithContext(context.Background(), name, val)
}

// ReplaceCellWithContext installs a fresh local value cell for name.
func (e *Env) ReplaceCellWithContext(ctx context.Context, name string, val Value) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	old, ok := e.vars[name]
	b := retainedBindingBytes(name, val)
	var meter sessionMeter
	var st *evalState
	var pending bool
	if !ok {
		var err error
		meter, st, pending, err = e.prepareFreshRetained(ctx, b, 1)
		if err != nil {
			return err
		}
	}
	cell := &Cell{v: val}
	cell.version.Add(1)
	e.vars[name] = cell
	if !ok || old.v == nil {
		e.newNameGen.Add(1)
	}
	if !ok {
		recordFreshRetained(st, pending, e, cell, meter, b)
	}
	return nil
}

func (e *Env) forkCells(parent *Env, names []Symbol) *Env {
	next := NewEnv(parent)
	next.vars = make(map[string]*Cell, len(names))
	e.mu.RLock()
	next.mu.Lock()
	for _, name := range names {
		if cell, ok := e.vars[name.V]; ok && cell.v != nil {
			next.vars[name.V] = cell
		}
	}
	next.mu.Unlock()
	e.mu.RUnlock()
	return next
}

// SetCanonical binds name as a canonical operator in this scope.
// It is intended ONLY for the stdlib plugin to register its builtins during
// engine initialization. Marking an arbitrary custom GoFunc as canonical will
// cause the bytecode VM to execute native opcode semantics for name instead of
// calling the provided function.
func (e *Env) SetCanonical(name string, val Value) error {
	return e.SetCanonicalWithContext(context.Background(), name, val)
}

func (e *Env) SetCanonicalWithContext(ctx context.Context, name string, val Value) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	cell, ok := e.vars[name]
	b := retainedBindingBytes(name, val)
	var meter sessionMeter
	var st *evalState
	var pending bool
	if !ok {
		var err error
		meter, st, pending, err = e.prepareFreshRetained(ctx, b, 1)
		if err != nil {
			return err
		}
		cell = e.localCell(name)
	}
	if cell.v == nil {
		e.newNameGen.Add(1)
	}
	cell.v = val
	cell.canonical = true
	cell.version.Add(1)
	if !ok {
		recordFreshRetained(st, pending, e, cell, meter, b)
	}
	return nil
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
	if layer := e.LazyLayer(); layer != nil {
		if v, ok, canon := layer.LookupAndMaterialize(e, name); ok {
			return v, true, canon
		}
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

// ReadCellSnapshot returns a coherent cell snapshot and its mutation version.
func (e *Env) ReadCellSnapshot(c *Cell) (Value, bool, bool, uint64) {
	e.mu.RLock()
	v, canon, ver := c.v, c.canonical, c.version.Load()
	e.mu.RUnlock()
	return v, v != nil, canon, ver
}

// Cell resolves name to its owning cell by walking the scope chain, skipping
// tombstoned (deleted) bindings as if they were absent. Used by the VM to
// cache a global's storage cell across executions.
func (e *Env) Cell(name string) (*Cell, bool) {
	if cell, ok := e.CellLocal(name); ok {
		return cell, true
	}
	if e.parent != nil {
		return e.parent.Cell(name)
	}
	return nil, false
}

// FuncCell resolves name to its owning function cell by walking the scope
// chain, skipping tombstoned bindings. Lisp-2 only. Mirrors Cell for the
// function namespace.
func (e *Env) FuncCell(name string) (*Cell, bool) {
	if cell, ok := e.FuncCellLocal(name); ok {
		return cell, true
	}
	if e.parent != nil {
		return e.parent.FuncCell(name)
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
	if layer := e.LazyLayer(); layer != nil {
		if _, ok, _ := layer.LookupAndMaterialize(e, name); ok {
			if cell, hit := e.CellLocal(name); hit {
				return cell, true
			}
		}
	}
	return nil, false
}

// FuncCellLocal is CellLocal for the function cell (Lisp-2 only).
func (e *Env) FuncCellLocal(name string) (*Cell, bool) {
	e.mu.RLock()
	cell, ok := e.funcs[name]
	live := ok && cell.v != nil
	e.mu.RUnlock()
	if live {
		return cell, true
	}
	if layer := e.LazyLayer(); layer != nil {
		if _, ok, _ := layer.LookupAndMaterialize(e, name); ok {
			if cell, hit := e.FuncCellLocal(name); hit {
				return cell, true
			}
		}
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
	if layer := e.LazyLayer(); layer != nil {
		if val, ok, _ := layer.LookupAndMaterialize(e, name); ok {
			return val, true
		}
	}
	if e.parent != nil {
		return e.parent.Get(name)
	}
	return nil, false
}

// SetFunc binds name in this scope's function cell (Lisp-2 only). The cell is
// allocated on first use so Lisp-1 scopes never carry it. Overwriting a
// canonical function binding removes the canonical marker, so a defun rebind
// is detected as non-canonical.
func (e *Env) SetFunc(name string, val Value) error {
	return e.SetFuncWithContext(context.Background(), name, val)
}

func (e *Env) SetFuncWithContext(ctx context.Context, name string, val Value) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	cell, ok := e.funcs[name]
	b := retainedBindingBytes(name, val)
	var meter sessionMeter
	var st *evalState
	var pending bool
	if !ok {
		var err error
		meter, st, pending, err = e.prepareFreshRetained(ctx, b, 1)
		if err != nil {
			return err
		}
		cell = e.localFuncCell(name)
	}
	cell.v = val
	cell.canonical = false
	cell.version.Add(1)
	if !ok {
		recordFreshRetained(st, pending, e, cell, meter, b)
	}
	return nil
}

// SetFuncCanonical binds name as a canonical operator in this scope's
// function cell (Lisp-2 only). It is intended ONLY for the engine's
// canonical-operator bridge, which mirrors a canonical value-cell binding
// into the function cell so Lisp-2 head resolution observes it too. Marking
// an arbitrary custom GoFunc as canonical will cause the bytecode VM to
// execute native opcode semantics for name instead of calling it.
func (e *Env) SetFuncCanonical(name string, val Value) error {
	return e.SetFuncCanonicalWithContext(context.Background(), name, val)
}

func (e *Env) SetFuncCanonicalWithContext(ctx context.Context, name string, val Value) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	cell, ok := e.funcs[name]
	b := retainedBindingBytes(name, val)
	var meter sessionMeter
	var st *evalState
	var pending bool
	if !ok {
		var err error
		meter, st, pending, err = e.prepareFreshRetained(ctx, b, 1)
		if err != nil {
			return err
		}
		cell = e.localFuncCell(name)
	}
	cell.v = val
	cell.canonical = true
	cell.version.Add(1)
	if !ok {
		recordFreshRetained(st, pending, e, cell, meter, b)
	}
	return nil
}

// GetFunc walks the scope chain reading the function cell (Lisp-2 only).
func (e *Env) GetFunc(name string) (Value, bool) {
	e.mu.RLock()
	var v Value
	if cell, ok := e.funcs[name]; ok {
		v = cell.v
	}
	e.mu.RUnlock()
	if v != nil {
		return v, true
	}
	if layer := e.LazyLayer(); layer != nil {
		if val, ok, _ := layer.LookupAndMaterialize(e, name); ok {
			return val, true
		}
	}
	if e.parent != nil {
		return e.parent.GetFunc(name)
	}
	return nil, false
}

// GetFuncCanonical resolves name like GetFunc but also returns whether it is
// a canonical binding in its owning scope (any scope in the chain). Returns
// (value, found, canonical).
func (e *Env) GetFuncCanonical(name string) (Value, bool, bool) {
	e.mu.RLock()
	var v Value
	var canon bool
	if cell, ok := e.funcs[name]; ok {
		v, canon = cell.v, cell.canonical
	}
	e.mu.RUnlock()
	if v != nil {
		return v, true, canon
	}
	if layer := e.LazyLayer(); layer != nil {
		if val, ok, canon := layer.LookupAndMaterialize(e, name); ok {
			return val, true, canon
		}
	}
	if e.parent != nil {
		return e.parent.GetFuncCanonical(name)
	}
	return nil, false, false
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
	if layer := e.LazyLayer(); layer != nil {
		if _, ok, _ := layer.LookupAndMaterialize(e, name); ok {
			if e.HasLive(name) {
				return e, true
			}
		}
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
			if err := child.Set(param.V, args[i]); err != nil {
				return nil, err
			}
		}
		if err := child.Set(variadic.V, List{Items: args[len(params):]}); err != nil {
			return nil, err
		}
	} else {
		if len(args) != len(params) {
			return nil, NewArityError(len(params), len(args))
		}
		for i, param := range params {
			if err := child.Set(param.V, args[i]); err != nil {
				return nil, err
			}
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
	if cell, ok := e.vars[name]; ok && (cell.v != nil || cell.canonical) {
		cell.v = nil
		cell.canonical = false
		cell.version.Add(1)
	}
	if cell, ok := e.funcs[name]; ok && (cell.v != nil || cell.canonical) {
		cell.v = nil
		cell.canonical = false
		cell.version.Add(1)
	}
	e.mu.Unlock()

	if layer := e.LazyLayer(); layer != nil {
		layer.TombstoneForDelete(e, name)
	}
}

// RetainedUsage returns this env's retained backing usage.
func (e *Env) RetainedUsage() (bytes, slots int64) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.retainedBytes, e.retainedSlots
}

// Rebuild compacts this scope's local binding maps, dropping tombstoned cells
// and recomputing retained backing usage from the remaining live bindings.
func (e *Env) Rebuild() (freedBytes, freedSlots int64) {
	e.mu.Lock()

	var releases []retainedRelease
	vars := make(map[string]*Cell, len(e.vars))
	funcs := make(map[string]*Cell, len(e.funcs))
	var bytes, slots int64

	for name, cell := range e.vars {
		if cell.v == nil {
			cell.rebuilt = true
			if cell.retainedMeter != nil {
				releases = append(releases, retainedRelease{meter: cell.retainedMeter, bytes: cell.retainedBytes, slots: 1})
			}
			freedBytes += cell.retainedBytes
			freedSlots++
			continue
		}
		vars[name] = cell
		if cell.retainedBytes > 0 {
			bytes += cell.retainedBytes
		} else {
			bytes += retainedBindingBytes(name, cell.v)
		}
		slots++
	}
	for name, cell := range e.funcs {
		if cell.v == nil {
			cell.rebuilt = true
			if cell.retainedMeter != nil {
				releases = append(releases, retainedRelease{meter: cell.retainedMeter, bytes: cell.retainedBytes, slots: 1})
			}
			freedBytes += cell.retainedBytes
			freedSlots++
			continue
		}
		funcs[name] = cell
		if cell.retainedBytes > 0 {
			bytes += cell.retainedBytes
		} else {
			bytes += retainedBindingBytes(name, cell.v)
		}
		slots++
	}

	e.vars = vars
	e.funcs = funcs
	e.retainedBytes = bytes
	e.retainedSlots = slots
	e.newNameGen.Add(1)
	e.mu.Unlock()

	for _, release := range releases {
		release.meter.ReleaseRetained(release.bytes, release.slots)
	}
	return
}

// function cell (Lisp-2 only). The order is unspecified. Parent bindings
// are not included. Like VarNames it forces deferred bindings first.
func (e *Env) FuncNames() []string {
	if layer := e.LazyLayer(); layer != nil {
		layer.ForceAll(e)
	}
	return e.LocalFuncNames()
}

// LocalFuncNames is FuncNames without consulting the lazy layer.
func (e *Env) LocalFuncNames() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.funcs == nil {
		return nil
	}
	names := make([]string, 0, len(e.funcs))
	for name, cell := range e.funcs {
		if cell.v != nil {
			names = append(names, name)
		}
	}
	return names
}

// VarNames returns a snapshot of the names bound in this scope's local frame.
// Tombstoned (deleted) names are skipped. The order is unspecified. Parent
// bindings are not included. On an env with a lazy layer it first forces
// materialization of every deferred binding so callers observe the full
// plugin surface (one-time cost, comparable to eager load).
func (e *Env) VarNames() []string {
	if layer := e.LazyLayer(); layer != nil {
		layer.ForceAll(e)
	}
	return e.LocalNames()
}

// LocalNames is VarNames without consulting the lazy layer: internal
// bookkeeping (plugin binding diffs at Use/Unload) must not force
// materialization or deferred loading would be pointless.
func (e *Env) LocalNames() []string {
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

type mergeCellCommit struct {
	cell      *Cell
	src       *Cell
	canonical bool
}

func (m mergeCellCommit) commit() {
	m.cell.v = m.src.v
	m.cell.canonical = m.canonical
	m.cell.retainedMeter = m.src.retainedMeter
	m.cell.retainedBytes = m.src.retainedBytes
	m.cell.rebuilt = m.src.rebuilt
	m.cell.version.Add(1)
}

func (e *Env) mergeCell(name string, src *Cell, canonical bool) (mergeCellCommit, retainedRelease, error) {
	cell, ok := e.vars[name]
	if !ok {
		if err := e.reserveRetainedBindings(src.retainedBytes, 1); err != nil {
			return mergeCellCommit{}, retainedRelease{}, err
		}
		cell = e.localCell(name)
		e.newNameGen.Add(1)
		e.retainedBytes += src.retainedBytes
		e.retainedSlots++
		return mergeCellCommit{
			cell:      cell,
			src:       src,
			canonical: canonical,
		}, retainedRelease{}, nil
	}

	release := retainedRelease{}
	if cell.retainedMeter != nil {
		release = retainedRelease{
			meter: cell.retainedMeter,
			bytes: cell.retainedBytes,
			slots: 1,
		}
	}
	return mergeCellCommit{
		cell:      cell,
		src:       src,
		canonical: canonical,
	}, release, nil
}

func (e *Env) mergeFuncCell(name string, src *Cell, canonical bool) (mergeCellCommit, retainedRelease, error) {
	cell, ok := e.funcs[name]
	if e.funcs == nil {
		ok = false
	}
	if !ok {
		if err := e.reserveRetainedBindings(src.retainedBytes, 1); err != nil {
			return mergeCellCommit{}, retainedRelease{}, err
		}
		cell = e.localFuncCell(name)
		e.newNameGen.Add(1)
		e.retainedBytes += src.retainedBytes
		e.retainedSlots++
		return mergeCellCommit{
			cell:      cell,
			src:       src,
			canonical: canonical,
		}, retainedRelease{}, nil
	}

	release := retainedRelease{}
	if cell.retainedMeter != nil {
		release = retainedRelease{
			meter: cell.retainedMeter,
			bytes: cell.retainedBytes,
			slots: 1,
		}
	}
	return mergeCellCommit{
		cell:      cell,
		src:       src,
		canonical: canonical,
	}, release, nil
}

// MergeInto copies all bindings from this env into target.
// Does NOT copy parent bindings. Target is locked during merge.
func (e *Env) MergeInto(target *Env) error {
	e.mu.RLock()
	defer e.mu.RUnlock()

	target.mu.Lock()
	var (
		commits  []mergeCellCommit
		releases []retainedRelease
	)

	for name, cell := range e.vars {
		if cell.v != nil {
			commit, release, err := target.mergeCell(name, cell, false)
			if err != nil {
				target.mu.Unlock()
				return err
			}
			commits = append(commits, commit)
			if release.meter != nil {
				releases = append(releases, release)
			}
		}
	}
	for name, cell := range e.funcs {
		if cell.v != nil {
			commit, release, err := target.mergeFuncCell(name, cell, false)
			if err != nil {
				target.mu.Unlock()
				return err
			}
			commits = append(commits, commit)
			if release.meter != nil {
				releases = append(releases, release)
			}
		}
	}
	target.mu.Unlock()

	for _, release := range releases {
		release.meter.ReleaseRetained(release.bytes, release.slots)
	}

	target.mu.Lock()
	for _, commit := range commits {
		commit.commit()
	}
	target.mu.Unlock()
	return nil
}

// MergeIntoCanonical copies all bindings from this env into target.
// Canonical value-cell binds are preserved on merge.
func (e *Env) MergeIntoCanonical(target *Env) error {
	e.mu.RLock()
	defer e.mu.RUnlock()

	target.mu.Lock()
	var (
		commits  []mergeCellCommit
		releases []retainedRelease
	)

	for name, cell := range e.vars {
		if cell.v != nil {
			commit, release, err := target.mergeCell(name, cell, true)
			if err != nil {
				target.mu.Unlock()
				return err
			}
			commits = append(commits, commit)
			if release.meter != nil {
				releases = append(releases, release)
			}
		}
	}
	for name, cell := range e.funcs {
		if cell.v != nil {
			commit, release, err := target.mergeFuncCell(name, cell, true)
			if err != nil {
				target.mu.Unlock()
				return err
			}
			commits = append(commits, commit)
			if release.meter != nil {
				releases = append(releases, release)
			}
		}
	}
	target.mu.Unlock()

	for _, release := range releases {
		release.meter.ReleaseRetained(release.bytes, release.slots)
	}

	target.mu.Lock()
	for _, commit := range commits {
		commit.commit()
	}
	target.mu.Unlock()
	return nil
}
