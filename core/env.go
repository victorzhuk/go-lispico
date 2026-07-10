package core

import "sync"

// Env is a lexical scope: an immutable parent chain with a thread-safe local binding map.
// Reads walk up the chain; writes are local-only.
type Env struct {
	mu         sync.RWMutex
	parent     *Env
	vars       map[string]Value
	funcs      map[string]Value // function cell; nil until first SetFunc (Lisp-2 only)
	eval       Evaluator
	canonical  map[string]struct{} // set of canonical operator names; nil for child envs
	macroEpoch int                 // bumped on each defmacro in this scope; used in bytecode cache key
}

func NewEnv(parent *Env) *Env {
	e := &Env{
		parent: parent,
		vars:   make(map[string]Value),
	}
	if parent != nil {
		e.eval = parent.eval
	}
	return e
}

// Set binds name in this (local) scope. Overwriting a canonical binding
// removes the canonical marker, so a root-env rebind is detected as non-canonical.
func (e *Env) Set(name string, val Value) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.vars[name] = val
	if e.canonical != nil {
		delete(e.canonical, name)
	}
}

// SetCanonical binds name as a canonical operator in this scope.
// It is intended ONLY for the stdlib plugin to register its builtins during
// engine initialization. Marking an arbitrary custom GoFunc as canonical will
// cause the bytecode VM to execute native opcode semantics for name instead of
// calling the provided function. Canonical markers are NOT inherited by child
// scopes (nil map for children).
func (e *Env) SetCanonical(name string, val Value) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.vars[name] = val
	if e.canonical == nil {
		e.canonical = make(map[string]struct{})
	}
	e.canonical[name] = struct{}{}
}

// GetCanonical resolves name like Get but also returns whether it is a canonical
// binding in its owning scope (any scope in the chain). Child envs have nil
// canonical map so a child-env rebind is trivially non-canonical; root-env rebind
// is detected because Set removes the marker. Returns (value, found, canonical).
func (e *Env) GetCanonical(name string) (Value, bool, bool) {
	e.mu.RLock()
	val, ok := e.vars[name]
	isCanon := false
	if ok && e.canonical != nil {
		_, isCanon = e.canonical[name]
	}
	e.mu.RUnlock()
	if ok {
		return val, true, isCanon
	}
	if e.parent != nil {
		return e.parent.GetCanonical(name)
	}
	return nil, false, false
}

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
	val, ok := e.vars[name]
	e.mu.RUnlock()
	if ok {
		return val, true
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
	_, ok := e.vars[name]
	e.mu.RUnlock()
	if ok {
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
// No-op if name is not bound locally. Also clears canonical marker.
func (e *Env) Delete(name string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.vars, name)
	delete(e.funcs, name)
	if e.canonical != nil {
		delete(e.canonical, name)
	}
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
// The order is unspecified. Parent bindings are not included.
func (e *Env) VarNames() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	names := make([]string, 0, len(e.vars))
	for name := range e.vars {
		names = append(names, name)
	}
	return names
}

// MergeInto copies all bindings from this env into target.
// Does NOT copy parent bindings. Target is locked during merge.
func (e *Env) MergeInto(target *Env) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	for name, val := range e.vars {
		target.Set(name, val)
	}
	for name, val := range e.funcs {
		target.SetFunc(name, val)
	}
}
