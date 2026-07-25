package core

import (
	"context"
	"fmt"
	"maps"
	"sync/atomic"
	"time"
)

// tailCall is returned by special forms to signal that the trampoline should
// apply fn to args in env without growing the Go call stack.
type tailCall struct {
	fn   Value
	args []Value
	env  *Env
}

func (t tailCall) Type() Keyword       { return Keyword{V: "tail-call"} }
func (t tailCall) String() string      { return "#<tail-call>" }
func (t tailCall) Equals(_ Value) bool { return false }

// recurVal carries the arguments for the next loop iteration.
type recurVal struct{ args []Value }

func (r recurVal) Type() Keyword       { return Keyword{V: "recur"} }
func (r recurVal) String() string      { return "#<recur>" }
func (r recurVal) Equals(_ Value) bool { return false }

// engine is the concrete tree-walking evaluator.
// It implements the Evaluator interface from types.go.
type engine struct {
	maxMacroDepth      int
	MaxDepth           int
	MaxStructuralDepth int
	MaxCollectionLen   int
	// forms is this Engine's effective special-form dispatch table, resolved
	// from its Dialect at construction. It is read-only after construction, so
	// evaluated code cannot change which forms are available.
	forms map[string]formFn
	// truthy is the Dialect's falsy rule — the single hook every conditional
	// special form consults instead of hardcoding IsTruthy.
	truthy func(Value) bool
	// lisp2 selects the namespace axis. When true, head symbols resolve against
	// the environment's function cell and definition forms bind functions there.
	lisp2 bool
	// dialect is the Dialect this engine was constructed with. It is zero for
	// NewEvaluator (identity dialect), set by NewEvaluatorWithDialect.
	dialect Dialect
	meter   sessionMeter
}

// NewEvaluator constructs a tree-walking evaluator running the identity
func NewEvaluator() *engine {
	return &engine{maxMacroDepth: 100, MaxDepth: 1000, MaxStructuralDepth: DefaultMaxStructuralDepth, forms: copyKernel(), truthy: IsTruthy}
}

// NewEvaluatorWithDialect constructs a tree-walking evaluator whose special
// forms are the resolved effective table of d. It fails if d references a
// canonical form absent from the kernel.
func NewEvaluatorWithDialect(d Dialect) (*engine, error) {
	forms, err := d.resolve()
	if err != nil {
		return nil, err
	}
	return &engine{maxMacroDepth: 100, MaxDepth: 1000, MaxStructuralDepth: DefaultMaxStructuralDepth, forms: forms, truthy: d.isTruthy, lisp2: d.isLisp2(), dialect: d}, nil
}

func copyKernel() map[string]formFn {
	forms := make(map[string]formFn, len(kernel))
	maps.Copy(forms, kernel)
	return forms
}

type pendingCellAlloc struct {
	env   *Env
	cell  *Cell
	meter sessionMeter
	bytes int64
	slots int64
}

// evalState holds the counters for a single top-level evaluation. It is
// carried in the context so concurrent evaluations on one engine never share
// depth, reduction, or allocation state.
type evalState struct {
	callDepth         atomic.Int64
	loopDepth         atomic.Int64
	macroDepth        atomic.Int64
	structDepth       atomic.Int64
	reductions        atomic.Int64
	allocBytes        atomic.Int64
	evalDepth         atomic.Int64
	maxReductions     int64
	maxAllocBytes     int64
	meter             atomic.Pointer[sessionMeter]
	leasedReductions  int64
	leasedAllocBytes  int64
	retainedBytes     int64
	retainedSlots     int64
	pendingCellAllocs []pendingCellAlloc
	// shared lets lazy states alias wrapper-owned counters without a second allocation per evalState.
	shared          *atomic.Int64
	sharedCallDepth *atomic.Int64
	// deadline is the engine-owned evaluation deadline enforced by pollCancel.
	// Zero means no engine deadline is set.
	deadline time.Time
	// budget counts nodes until the next batched cancellation check. Atomic
	// to match structDepth and stay race-safe if this state is shared across
	// sequential evaluator hops (e.g. VM GoFunc callbacks into the tree-walker).
	budget atomic.Int64
}

func (st *evalState) counter() *atomic.Int64 {
	if st.shared != nil {
		return st.shared
	}
	return &st.structDepth
}

func (st *evalState) callCounter() *atomic.Int64 {
	if st.sharedCallDepth != nil {
		return st.sharedCallDepth
	}
	return &st.callDepth
}

// lazyEvalStateCtx wraps a parent context and materializes an evalState on demand
// only when the state key is read. This avoids allocations on non-re-entrant GoFunc
// dispatch while preserving shared state across re-entrant evaluator calls.
type lazyEvalStateCtx struct {
	context.Context
	deadline      time.Time
	counter       atomic.Int64
	callCounter   atomic.Int64
	state         atomic.Pointer[evalState]
	maxReductions int64
	maxAllocBytes int64
	reductions    int64
	allocBytes    int64
}

// Value returns either the current eval state or the parent context value.
func (c *lazyEvalStateCtx) Value(key any) any {
	if _, ok := key.(evalStateKey); ok {
		if existing, ok := c.Context.Value(key).(*evalState); ok {
			return existing
		}

		if st := c.state.Load(); st != nil {
			return st
		}
		st := newEvalStateWithLimits(c.maxReductions, c.maxAllocBytes)
		st.deadline = c.deadline
		st.shared = &c.counter
		st.sharedCallDepth = &c.callCounter
		st.reductions.Store(c.reductions)
		st.allocBytes.Store(c.allocBytes)
		st.attachMeter(sessionMeterFromContext(c.Context))
		if c.state.CompareAndSwap(nil, st) {
			return st
		}
		return c.state.Load()
	}

	return c.Context.Value(key)
}

// checkInterval bounds how many nodes run between batched cancellation
// checks. A fresh budget starts at 0 (its zero value), so the first check
// (force=false) fires immediately without charging reductions, then every
// checkInterval thereafter.
const checkInterval int64 = 128

// pollCancel checks the engine deadline and ctx for cancellation. A batched
// (force=false) check only runs once every checkInterval calls; a forced
// check always runs, for latency-bounding call/loop boundaries.
func (st *evalState) pollCancel(ctx context.Context, force bool) error {
	if force {
		if err := st.flushReductions(); err != nil {
			return err
		}
		if err := st.chargeReductions(1); err != nil {
			return err
		}
	} else {
		remaining := st.budget.Add(-1)
		if remaining > 0 {
			return nil
		}
		if remaining == 0 {
			if err := st.chargeReductions(checkInterval); err != nil {
				return err
			}
		}
	}
	st.budget.Store(checkInterval)
	if !st.deadline.IsZero() && !time.Now().Before(st.deadline) {
		return context.DeadlineExceeded
	}
	return ctx.Err()
}

type evalStateKey struct{}

// ensureEvalState attaches a fresh evalState to ctx on the first (top-level)
// call and returns the existing one on nested calls.
func ensureEvalState(ctx context.Context) context.Context {
	if st, ok := ctx.Value(evalStateKey{}).(*evalState); ok {
		st.attachMeter(sessionMeterFromContext(ctx))
		return ctx
	}
	st := newEvalState()
	st.attachMeter(sessionMeterFromContext(ctx))
	return context.WithValue(ctx, evalStateKey{}, st)
}

func evalStateFrom(ctx context.Context) *evalState {
	if st, ok := ctx.Value(evalStateKey{}).(*evalState); ok {
		st.attachMeter(sessionMeterFromContext(ctx))
		return st
	}
	st := newEvalState()
	st.attachMeter(sessionMeterFromContext(ctx))
	return st
}

// DetachEvalState returns a copy of ctx with a fresh evalState attached,
// preserving cancellation and any other context values. Use this when a new
// goroutine should evaluate with its own depth counters so it cannot race or
// trip MaxDepth against another evaluation that shares the same ancestor ctx.
func DetachEvalState(ctx context.Context) context.Context {
	st := newEvalState()
	st.attachMeter(sessionMeterFromContext(ctx))
	return context.WithValue(ctx, evalStateKey{}, st)
}

// EnsureEvalState returns a context with a fresh evalState attached if one
// is not already present. Callers that propagate ctx through evaluator
// callbacks (VM GoFuncs, Apply) should call this at every entry point so
// the shared structural-depth counter is available.
func EnsureEvalState(ctx context.Context) context.Context { return ensureEvalState(ctx) }

// PollEvalState runs the shared batched cancellation check carried by ctx.
func PollEvalState(ctx context.Context) error {
	return evalStateFrom(ctx).pollCancel(ctx, false)
}

// EvalStructCounter returns the shared structDepth atomic from the eval state
// in ctx. Returns a private zero-valued atomic when ctx has no eval state (the
// pointer is never nil), enabling the VM to share a single structural depth
// counter with the tree-walker across Apply callbacks.
func EvalStructCounter(ctx context.Context) *atomic.Int64 {
	return evalStateFrom(ctx).counter()
}

// EvalCallCounter returns the shared call-depth atomic from the eval state in
// ctx. Returns a private zero-valued atomic when ctx has no eval state.
func EvalCallCounter(ctx context.Context) *atomic.Int64 {
	if w, ok := ctx.(*lazyEvalStateCtx); ok {
		return &w.callCounter
	}
	return evalStateFrom(ctx).callCounter()
}

// WithEvalDeadline attaches an engine-owned evaluation deadline instant to
// ctx's eval state (creating it if absent), enforced by the evaluators' batched
// cancellation checks instead of a per-call timer context. A zero deadline
// leaves the caller's context as the only bound.
func WithEvalDeadline(ctx context.Context, deadline time.Time) context.Context {
	ctx = ensureEvalState(ctx)
	evalStateFrom(ctx).deadline = deadline
	return ctx
}

// EvalDeadlineFrom returns the engine-owned deadline instant carried in ctx's
// eval state, or the zero Time when none is set. The VM reads it to enforce the
// deadline without a timer context.
func EvalDeadlineFrom(ctx context.Context) time.Time {
	return evalStateFrom(ctx).deadline
}

// AdoptEvalState adopts or creates eval state from ctx and returns a pointer to
// the shared structural-depth counter. Reusing the same counter across
// evaluator boundaries keeps budget limits consistent for nested VM -> GoFunc ->
// evaluator calls. The state is lazy: evalState + context.WithValue allocation
// are deferred until a GoFunc actually reads evalStateKey.
func AdoptEvalState(ctx context.Context, deadline time.Time, seed int64) (context.Context, *atomic.Int64) {
	adopted, counter, _ := AdoptEvalStateWithMeter(ctx, deadline, seed, EvalMeterSnapshot{})
	return adopted, counter
}

func AdoptEvalStateWithMeter(ctx context.Context, deadline time.Time, structSeed int64, snap EvalMeterSnapshot, callSeed ...int64) (context.Context, *atomic.Int64, EvalMeter) {
	if st, ok := ctx.Value(evalStateKey{}).(*evalState); ok {
		if len(callSeed) > 0 && callSeed[0] > 0 {
			counter := st.callCounter()
			counter.CompareAndSwap(0, callSeed[0])
		}
		return ctx, st.counter(), EvalMeter{st: st}
	}

	maxReductions := snap.MaxReductions
	if maxReductions <= 0 {
		maxReductions = DefaultMaxReductions
	}
	maxAllocBytes := snap.MaxAllocationBytes
	if maxAllocBytes <= 0 {
		maxAllocBytes = DefaultMaxAllocationBytes
	}
	callDepth := int64(0)
	if len(callSeed) > 0 {
		callDepth = callSeed[0]
	}
	w := &lazyEvalStateCtx{Context: ctx, deadline: deadline, maxReductions: maxReductions, maxAllocBytes: maxAllocBytes, reductions: snap.Reductions, allocBytes: snap.AllocationBytes}
	w.counter.Store(structSeed)
	w.callCounter.Store(callDepth)
	return w, &w.counter, EvalMeter{}
}

// HasEvalState reports whether ctx already carries evaluation state from an
// enclosing evaluation. The Call fast path uses it to share the enclosing
// structural-depth counter and deadline on a re-entrant call instead of
// starting a fresh, independent resource budget (which would let nested calls
// escape the ADR-0007 limits). On a lazily-adopted context (see AdoptEvalState)
// the check materializes the state as a side effect — acceptable on re-entrant
// paths, where the state is needed anyway.
func HasEvalState(ctx context.Context) bool {
	_, ok := ctx.Value(evalStateKey{}).(*evalState)
	return ok
}

func (e *engine) SetFallbackEvalMeter(m any) {
	e.meter, _ = m.(sessionMeter)
}

func (e *engine) evalContext(ctx context.Context) context.Context {
	if sessionMeterFromContext(ctx) == nil && e.meter != nil {
		ctx = WithEvalMeter(ctx, e.meter)
	}
	return ensureEvalState(ctx)
}

func evalErrorf(format string, args ...any) *LispicoError {
	return &LispicoError{Code: "EvalError", Message: fmt.Sprintf(format, args...)}
}

func resourceLimitErrorf(format string, args ...any) *LispicoError {
	return &LispicoError{Code: CodeResourceLimit, Message: fmt.Sprintf(format, args...)}
}

// Apply is the public entry point for calling a Lisp value as a function.
// Used by the runtime API and plugins that invoke Lambdas from Go.
func (e *engine) Apply(ctx context.Context, fn Value, args []Value, env *Env) (result Value, err error) {
	ctx = e.evalContext(ctx)
	top, err := StartEval(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if ferr := FinishEval(ctx, top); ferr != nil && (err == nil || IsTerminalEvalError(ferr)) {
			result = nil
			err = ferr
		}
	}()
	return e.apply(ctx, fn, args, env)
}

// Eval evaluates form in env, returning the result.
func (e *engine) Eval(ctx context.Context, v Value, env *Env) (result Value, err error) {
	ctx = e.evalContext(ctx)

	top, err := StartEval(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if ferr := FinishEval(ctx, top); ferr != nil && (err == nil || IsTerminalEvalError(ferr)) {
			result = nil
			err = ferr
		}
	}()

	st := evalStateFrom(ctx)

	if err := st.pollCancel(ctx, false); err != nil {
		return nil, err
	}
	switch val := v.(type) {
	case Nil, Bool, Int, Float, String, Keyword, GoFunc, Lambda, Macro:
		return val, nil
	case Vector:
		counter := st.counter()
		counter.Add(1)
		defer func() { counter.Add(-1) }()
		if e.MaxStructuralDepth > 0 && int(counter.Load()) > e.MaxStructuralDepth {
			return nil, resourceLimitErrorf("structural depth limit %d exceeded", e.MaxStructuralDepth)
		}
		if err := st.chargeAllocBytes(VectorShallowBytes(len(val.Items))); err != nil {
			return nil, err
		}
		items := make([]Value, len(val.Items))
		for i, item := range val.Items {
			r, err := e.Eval(ctx, item, env)
			if err != nil {
				return nil, err
			}
			items[i] = r
		}
		return Vector{Items: items}, nil
	case *HashMap:
		return e.evalMap(ctx, val, env)
	case Symbol:
		r, ok := env.Get(val.V)
		if !ok {
			return nil, NewUndefinedError(val.V)
		}
		return r, nil
	case List:
		if len(val.Items) == 0 {
			return val, nil
		}
		return e.evalList(ctx, val.Items, env)
	default:
		return nil, NewTypeError("evaluable", v)
	}
}

// evalMap evaluates every key and value of a map literal, producing a new map.
func (e *engine) evalMap(ctx context.Context, m *HashMap, env *Env) (Value, error) {
	st := evalStateFrom(ctx)
	counter := st.counter()
	counter.Add(1)
	defer func() { counter.Add(-1) }()
	if e.MaxStructuralDepth > 0 && int(counter.Load()) > e.MaxStructuralDepth {
		return nil, resourceLimitErrorf("structural depth limit %d exceeded", e.MaxStructuralDepth)
	}
	if err := st.chargeAllocBytes(HashMapShallowBytes(m.Len())); err != nil {
		return nil, err
	}
	result := NewHashMap()
	for _, pair := range m.Pairs() {
		k, err := e.Eval(ctx, pair[0], env)
		if err != nil {
			return nil, err
		}
		v, err := e.Eval(ctx, pair[1], env)
		if err != nil {
			return nil, err
		}
		err = result.Set(k, v)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (e *engine) evalList(ctx context.Context, items []Value, env *Env) (Value, error) {
	head := items[0]

	var fn Value
	resolved := false
	if sym, ok := head.(Symbol); ok {
		if form, ok := e.forms[sym.V]; ok {
			return form(ctx, e, items[1:], env)
		}
		if e.lisp2 {
			f, ok := env.GetFunc(sym.V)
			if !ok {
				return nil, NewUndefinedError(sym.V)
			}
			fn, resolved = f, true
		}
	}

	if !resolved {
		var err error
		fn, err = e.Eval(ctx, head, env)
		if err != nil {
			return nil, err
		}
	}

	if macro, ok := fn.(Macro); ok {
		return e.expandMacro(ctx, macro, items[1:], env)
	}

	args := make([]Value, len(items)-1)
	for i, item := range items[1:] {
		arg, err := e.Eval(ctx, item, env)
		if err != nil {
			return nil, err
		}
		args[i] = arg
	}

	return e.apply(ctx, fn, args, env)
}

// apply is the TCO trampoline. Each call represents one stack frame;
// Lambda tail-call returns a tailCall value which loops back without recursing.
func (e *engine) apply(ctx context.Context, fn Value, args []Value, env *Env) (Value, error) {
	st := evalStateFrom(ctx)
	counter := st.callCounter()
	counter.Add(1)
	defer func() { counter.Add(-1) }()

	if e.MaxDepth > 0 && int(counter.Load()) > e.MaxDepth {
		return nil, evalErrorf("maximum call depth exceeded")
	}

	for {
		if err := st.pollCancel(ctx, true); err != nil {
			return nil, err
		}

		switch f := fn.(type) {
		case GoFunc:
			if err := st.chargeReductions(1); err != nil {
				return nil, err
			}
			result, err := f.Fn(ctx, e, args, env)
			if err != nil {
				return nil, err
			}
			if err := st.chargeAllocBytes(ValueShallowBytes(result)); err != nil {
				return nil, err
			}
			return result, nil
		case Lambda:
			child, err := f.Env.ChildVariadic(f.Params, args, f.Variadic)
			if err != nil {
				return nil, err
			}
			result, err := e.evalBody(ctx, f.Body, child)
			if err != nil {
				return nil, err
			}
			if _, ok := result.(recurVal); ok {
				return nil, evalErrorf("recur outside loop")
			}
			tc, ok := result.(tailCall)
			if !ok {
				return result, nil
			}
			fn, args, env = tc.fn, tc.args, tc.env
		case Keyword:
			if len(args) != 1 {
				return nil, evalErrorf("keyword lookup requires exactly 1 argument, got %d", len(args))
			}
			m, ok := args[0].(*HashMap)
			if !ok {
				return Nil{}, nil
			}
			v, _ := m.Get(f)
			if v == nil {
				return Nil{}, nil
			}
			return v, nil
		default:
			return nil, NewTypeError("function", fn)
		}
	}
}

// evalBody evaluates all forms in body and returns the last result.
func (e *engine) evalBody(ctx context.Context, body []Value, env *Env) (Value, error) {
	var result Value = Nil{}
	for _, form := range body {
		var err error
		result, err = e.Eval(ctx, form, env)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

// MacroExpand fully expands all macros in form without evaluating the result.
// Used by tooling and the bytecode compiler (ch008).
func (e *engine) MacroExpand(ctx context.Context, form Value, env *Env) (Value, error) {
	ctx = e.evalContext(ctx)

	list, ok := form.(List)
	if !ok || len(list.Items) == 0 {
		return form, nil
	}
	fn, ok := e.resolveHead(ctx, list.Items[0], env)
	if !ok {
		return form, nil
	}
	macro, ok := fn.(Macro)
	if !ok {
		return form, nil
	}
	expanded, err := e.expandMacroForm(ctx, macro, list.Items[1:])
	if err != nil {
		return nil, err
	}
	return e.MacroExpand(ctx, expanded, env)
}

// resolveHead resolves a form's head the way evalList does: under Lisp-2 the
// function cell owns operator bindings, so a defmacro binding is invisible in
// value position and the form would compile as a plain call.
func (e *engine) resolveHead(ctx context.Context, head Value, env *Env) (Value, bool) {
	if sym, ok := head.(Symbol); ok && e.lisp2 {
		if fn, ok := env.GetFunc(sym.V); ok {
			return fn, true
		}
	}
	fn, err := e.Eval(ctx, head, env)
	if err != nil {
		return nil, false
	}
	return fn, true
}

// expandMacroForm runs the macro body with unevaluated args and returns the
// expansion as a Value. Does NOT evaluate the result — that is the caller's job.
func (e *engine) expandMacroForm(ctx context.Context, m Macro, args []Value) (Value, error) {
	st := evalStateFrom(ctx)
	if err := st.chargeReductions(1); err != nil {
		return nil, err
	}
	if int(st.macroDepth.Load()) >= e.maxMacroDepth {
		return nil, evalErrorf("macro expansion depth %d exceeded", e.maxMacroDepth)
	}
	st.macroDepth.Add(1)
	defer func() { st.macroDepth.Add(-1) }()

	macroEnv, err := m.Env.ChildVariadic(m.Params, args, m.Variadic)
	if err != nil {
		return nil, &LispicoError{Code: "EvalError", Message: fmt.Sprintf("macro %s: %s", m.Name, err), Cause: err}
	}
	return e.evalBody(ctx, m.Body, macroEnv)
}

// expandMacro expands macro m with unevaluated args, then evaluates the result in env.
// Called by evalList during normal evaluation.
func (e *engine) expandMacro(ctx context.Context, m Macro, args []Value, env *Env) (Value, error) {
	expanded, err := e.expandMacroForm(ctx, m, args)
	if err != nil {
		return nil, err
	}
	return e.Eval(ctx, expanded, env)
}

func (e *engine) expandQuasiquote(ctx context.Context, v Value, env *Env) (Value, error) {
	switch val := v.(type) {
	case List:
		if len(val.Items) > 0 {
			if sym, ok := val.Items[0].(Symbol); ok {
				switch sym.V {
				case "unquote":
					if len(val.Items) != 2 {
						return nil, evalErrorf("unquote requires 1 argument")
					}
					return e.Eval(ctx, val.Items[1], env)
				case "unquote-splicing":
					return nil, evalErrorf("unquote-splicing used outside of list context")
				}
			}
		}
		st := evalStateFrom(ctx)
		counter := st.counter()
		counter.Add(1)
		defer func() { counter.Add(-1) }()
		if e.MaxStructuralDepth > 0 && int(counter.Load()) > e.MaxStructuralDepth {
			return nil, resourceLimitErrorf("structural depth limit %d exceeded", e.MaxStructuralDepth)
		}
		if err := st.chargeAllocBytes(MeterCollectionHeaderBytes); err != nil {
			return nil, err
		}
		var result []Value
		for _, item := range val.Items {
			if list, ok := item.(List); ok && len(list.Items) > 0 {
				if sym, ok := list.Items[0].(Symbol); ok && sym.V == "unquote-splicing" {
					if len(list.Items) != 2 {
						return nil, evalErrorf("unquote-splicing requires 1 argument")
					}
					expanded, err := e.Eval(ctx, list.Items[1], env)
					if err != nil {
						return nil, err
					}
					switch seq := expanded.(type) {
					case List:
						if err := st.chargeAllocBytes(ValueSlotsBytes(len(seq.Items))); err != nil {
							return nil, err
						}
						result = append(result, seq.Items...)
					case Vector:
						if err := st.chargeAllocBytes(ValueSlotsBytes(len(seq.Items))); err != nil {
							return nil, err
						}
						result = append(result, seq.Items...)
					default:
						return nil, evalErrorf("unquote-splicing requires a sequence, got %T", expanded)
					}
					continue
				}
			}
			expanded, err := e.expandQuasiquote(ctx, item, env)
			if err != nil {
				return nil, err
			}
			if err := st.chargeAllocBytes(MeterValueSlotBytes); err != nil {
				return nil, err
			}
			result = append(result, expanded)
		}
		return List{Items: result}, nil
	case Vector:
		st := evalStateFrom(ctx)
		counter := st.counter()
		counter.Add(1)
		defer func() { counter.Add(-1) }()
		if e.MaxStructuralDepth > 0 && int(counter.Load()) > e.MaxStructuralDepth {
			return nil, resourceLimitErrorf("structural depth limit %d exceeded", e.MaxStructuralDepth)
		}
		if err := st.chargeAllocBytes(VectorShallowBytes(len(val.Items))); err != nil {
			return nil, err
		}
		result := make([]Value, len(val.Items))
		for i, item := range val.Items {
			expanded, err := e.expandQuasiquote(ctx, item, env)
			if err != nil {
				return nil, err
			}
			result[i] = expanded
		}
		return Vector{Items: result}, nil
	case *HashMap:
		st := evalStateFrom(ctx)
		counter := st.counter()
		counter.Add(1)
		defer func() { counter.Add(-1) }()
		if e.MaxStructuralDepth > 0 && int(counter.Load()) > e.MaxStructuralDepth {
			return nil, resourceLimitErrorf("structural depth limit %d exceeded", e.MaxStructuralDepth)
		}
		if err := st.chargeAllocBytes(HashMapShallowBytes(val.Len())); err != nil {
			return nil, err
		}
		result := NewHashMap()
		for _, pair := range val.Pairs() {
			k, err := e.expandQuasiquote(ctx, pair[0], env)
			if err != nil {
				return nil, err
			}
			v, err := e.expandQuasiquote(ctx, pair[1], env)
			if err != nil {
				return nil, err
			}
			err = result.Set(k, v)
			if err != nil {
				return nil, err
			}
		}
		return result, nil
	default:
		return val, nil
	}
}

func (e *engine) CollectionLimit() int        { return e.MaxCollectionLen }
func (e *engine) ConstructionDepthLimit() int { return e.MaxStructuralDepth }

// ── Special Form Implementations ─────────────────────────────────────────────

// bindOperator binds a function-defining form's result. Under Lisp-2 it lands in
// the function cell so head position can find it; under Lisp-1 it shares the
// single value namespace, exactly as before.
func (e *engine) bindOperator(ctx context.Context, env *Env, name string, val Value) error {
	if e.lisp2 {
		return env.SetFuncWithContext(ctx, name, val)
	}
	return env.SetWithContext(ctx, name, val)
}

func evalDef(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
	if len(args) != 2 {
		return nil, evalErrorf("def requires 2 arguments, got %d", len(args))
	}
	name, ok := args[0].(Symbol)
	if !ok {
		return nil, evalErrorf("def: first argument must be a symbol, got %T", args[0])
	}
	val, err := e.Eval(ctx, args[1], env)
	if err != nil {
		return nil, err
	}
	if err := env.SetWithContext(ctx, name.V, val); err != nil {
		return nil, err
	}
	return val, nil
}

// paramsAsVector accepts a parameter declaration as either a Vector
// (Clojure-style [a b & rest]) or a List (Common Lisp-style (a b & rest))
// and returns the canonical Vector form. defn/fn/defmacro accept both
// for dialect portability: the CL reader disables bracket literals, so
// a List is the only on-disk representation.
func paramsAsVector(v Value) (Vector, error) {
	switch p := v.(type) {
	case Vector:
		return p, nil
	case List:
		return Vector{Items: append([]Value(nil), p.Items...)}, nil
	default:
		return Vector{}, evalErrorf("parameters must be a vector or list, got %T", v)
	}
}

func evalDefn(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
	if len(args) < 3 {
		return nil, evalErrorf("defn requires at least 3 arguments (name params body...)")
	}
	name, ok := args[0].(Symbol)
	if !ok {
		return nil, evalErrorf("defn: first argument must be a symbol")
	}
	params, paramErr := paramsAsVector(args[1])
	if paramErr != nil {
		return nil, paramErr
	}
	fixed, variadic, err := parseParams(params)
	if err != nil {
		return nil, &LispicoError{Code: "EvalError", Message: fmt.Sprintf("defn %s: %s", name.V, err), Cause: err}
	}
	lambda := Lambda{
		Name:     name.V,
		Params:   fixed,
		Variadic: variadic,
		Body:     args[2:],
		Env:      env,
	}
	if err := e.bindOperator(ctx, env, name.V, lambda); err != nil {
		return nil, err
	}
	return lambda, nil
}

func evalDefmacro(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
	if len(args) < 3 {
		return nil, evalErrorf("defmacro requires at least 3 arguments (name params body...)")
	}
	name, ok := args[0].(Symbol)
	if !ok {
		return nil, evalErrorf("defmacro: first argument must be a symbol")
	}
	params, paramErr := paramsAsVector(args[1])
	if paramErr != nil {
		return nil, paramErr
	}
	fixed, variadic, err := parseParams(params)
	if err != nil {
		return nil, &LispicoError{Code: "EvalError", Message: fmt.Sprintf("defmacro %s: %s", name.V, err), Cause: err}
	}
	macro := Macro{
		Name:     name.V,
		Params:   fixed,
		Variadic: variadic,
		Body:     args[2:],
		Env:      env,
	}
	if err := e.bindOperator(ctx, env, name.V, macro); err != nil {
		return nil, err
	}
	env.BumpMacroEpoch()
	return macro, nil
}

func evalFn(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
	if len(args) < 2 {
		return nil, evalErrorf("fn requires at least 2 arguments (params body...)")
	}
	params, paramErr := paramsAsVector(args[0])
	if paramErr != nil {
		return nil, paramErr
	}
	fixed, variadic, err := parseParams(params)
	if err != nil {
		return nil, &LispicoError{Code: "EvalError", Message: fmt.Sprintf("fn: %s", err), Cause: err}
	}
	return Lambda{
		Params:   fixed,
		Variadic: variadic,
		Body:     args[1:],
		Env:      env,
	}, nil
}

func evalIf(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, evalErrorf("if requires 2 or 3 arguments")
	}
	cond, err := e.Eval(ctx, args[0], env)
	if err != nil {
		return nil, err
	}
	if e.truthy(cond) {
		return e.Eval(ctx, args[1], env)
	}
	if len(args) == 3 {
		return e.Eval(ctx, args[2], env)
	}
	return Nil{}, nil
}

func isCondElse(v Value) bool {
	switch x := v.(type) {
	case Symbol:
		return x.V == "else" || x.V == ":else"
	case Keyword:
		return x.V == "else"
	}
	return false
}

func evalCond(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
	clauses, err := e.dialect.NormalizeCond(args)
	if err != nil {
		return nil, err
	}
	for _, clause := range clauses {
		items := clause.(List).Items
		test, body := items[0], items[1]
		if isCondElse(test) {
			return e.Eval(ctx, body, env)
		}
		result, err := e.Eval(ctx, test, env)
		if err != nil {
			return nil, err
		}
		if e.truthy(result) {
			return e.Eval(ctx, body, env)
		}
	}
	return Nil{}, nil
}

func evalWhen(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
	if len(args) < 2 {
		return nil, evalErrorf("when requires at least 2 arguments")
	}
	cond, err := e.Eval(ctx, args[0], env)
	if err != nil {
		return nil, err
	}
	if !e.truthy(cond) {
		return Nil{}, nil
	}
	return e.evalBody(ctx, args[1:], env)
}

func evalUnless(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
	if len(args) < 2 {
		return nil, evalErrorf("unless requires at least 2 arguments")
	}
	cond, err := e.Eval(ctx, args[0], env)
	if err != nil {
		return nil, err
	}
	if e.truthy(cond) {
		return Nil{}, nil
	}
	return e.evalBody(ctx, args[1:], env)
}

func evalLet(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
	if len(args) < 2 {
		return nil, evalErrorf("let requires at least 2 arguments")
	}
	bindings, err := NormalizeBindings("let", args[0])
	if err != nil {
		return nil, evalErrorf("%s", err)
	}
	child := env.Child()
	for _, binding := range bindings {
		val, err := e.Eval(ctx, binding.Value, env)
		if err != nil {
			return nil, err
		}
		if err := child.Set(binding.Name.V, val); err != nil {
			return nil, err
		}
	}
	return e.evalBody(ctx, args[1:], child)
}

func evalLetStar(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
	if len(args) < 2 {
		return nil, evalErrorf("let* requires at least 2 arguments")
	}
	bindings, err := NormalizeBindings("let*", args[0])
	if err != nil {
		return nil, evalErrorf("%s", err)
	}
	child := env.Child()
	for _, binding := range bindings {
		val, err := e.Eval(ctx, binding.Value, child)
		if err != nil {
			return nil, err
		}
		if err := child.Set(binding.Name.V, val); err != nil {
			return nil, err
		}
	}
	return e.evalBody(ctx, args[1:], child)
}

func evalDo(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
	return e.evalBody(ctx, args, env)
}

func evalQuote(_ context.Context, _ *engine, args []Value, _ *Env) (Value, error) {
	if len(args) != 1 {
		return nil, evalErrorf("quote requires exactly 1 argument")
	}
	return args[0], nil
}

func evalQuasiquote(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
	if len(args) != 1 {
		return nil, evalErrorf("quasiquote requires exactly 1 argument")
	}
	return e.expandQuasiquote(ctx, args[0], env)
}

func evalSet(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
	if len(args) != 2 {
		return nil, evalErrorf("set! requires exactly 2 arguments")
	}
	name, ok := args[0].(Symbol)
	if !ok {
		return nil, evalErrorf("set!: first argument must be a symbol")
	}
	defEnv, ok := env.Find(name.V)
	if !ok {
		return nil, evalErrorf("set!: cannot mutate undefined variable %q", name.V)
	}
	val, err := e.Eval(ctx, args[1], env)
	if err != nil {
		return nil, err
	}
	if err := defEnv.SetWithContext(ctx, name.V, val); err != nil {
		return nil, err
	}
	return val, nil
}

func loopCapturedVars(ctx context.Context, e *engine, body []Value, vars []Symbol, env *Env) map[string]bool {
	if len(vars) == 0 || !formsMayCreateClosure(ctx, e, body, env) {
		return nil
	}
	targets := make(map[string]bool, len(vars))
	for _, v := range vars {
		targets[v.V] = true
	}
	var captured map[string]bool
	for _, form := range body {
		scanLoopCapture(ctx, e, env, form, targets, nil, &captured)
	}
	return captured
}

func formsMayCreateClosure(ctx context.Context, e *engine, forms []Value, env *Env) bool {
	for _, form := range forms {
		if formMayCreateClosure(ctx, e, form, env) {
			return true
		}
	}
	return false
}

func formMayCreateClosure(ctx context.Context, e *engine, form Value, env *Env) bool {
	switch v := form.(type) {
	case List:
		if len(v.Items) == 0 {
			return false
		}
		if head, ok := v.Items[0].(Symbol); ok {
			switch head.V {
			case "quote":
				return false
			case "fn", "defn", "defmacro":
				return true
			}
		}
		if expanded, ok := expandMacroForLoopScan(ctx, e, v, env); ok {
			return formMayCreateClosure(ctx, e, expanded, env)
		}
		for _, item := range v.Items {
			if formMayCreateClosure(ctx, e, item, env) {
				return true
			}
		}
	case Vector:
		for _, item := range v.Items {
			if formMayCreateClosure(ctx, e, item, env) {
				return true
			}
		}
	case *HashMap:
		found := false
		v.Each(func(k, val Value) {
			if found {
				return
			}
			found = formMayCreateClosure(ctx, e, k, env) || formMayCreateClosure(ctx, e, val, env)
		})
		return found
	}
	return false
}

func expandMacroForLoopScan(ctx context.Context, e *engine, list List, env *Env) (Value, bool) {
	if head, ok := list.Items[0].(Symbol); ok {
		if _, special := e.forms[head.V]; special {
			return nil, false
		}
	}
	fn, err := e.Eval(ctx, list.Items[0], env)
	if err != nil {
		return nil, false
	}
	macro, ok := fn.(Macro)
	if !ok {
		return nil, false
	}
	expanded, err := e.expandMacroForm(ctx, macro, list.Items[1:])
	if err != nil {
		return nil, false
	}
	return expanded, true
}

func scanLoopCapture(ctx context.Context, e *engine, env *Env, form Value, targets map[string]bool, shadow map[string]bool, captured *map[string]bool) {
	switch v := form.(type) {
	case List:
		if len(v.Items) == 0 {
			return
		}
		if expanded, ok := expandMacroForLoopScan(ctx, e, v, env); ok {
			scanLoopCapture(ctx, e, env, expanded, targets, shadow, captured)
			return
		}
		if head, ok := v.Items[0].(Symbol); ok {
			switch head.V {
			case "quote":
				return
			case "fn":
				if len(v.Items) >= 3 {
					params, err := paramsAsVector(v.Items[1])
					if err == nil {
						fixed, variadic, err := parseParams(params)
						if err != nil {
							return
						}
						nextShadow := extendShadow(shadow, fixed, variadic)
						for name := range targets {
							if shadow[name] {
								continue
							}
							if formsReferenceTargets(ctx, e, env, v.Items[2:], map[string]bool{name: true}, nextShadow) {
								markLoopCaptured(captured, name)
							}
						}
					}
				}
				return
			case "defn", "defmacro":
				if len(v.Items) >= 4 {
					params, err := paramsAsVector(v.Items[2])
					if err == nil {
						fixed, variadic, err := parseParams(params)
						if err != nil {
							return
						}
						nextShadow := extendShadow(shadow, fixed, variadic)
						for name := range targets {
							if shadow[name] {
								continue
							}
							if formsReferenceTargets(ctx, e, env, v.Items[3:], map[string]bool{name: true}, nextShadow) {
								markLoopCaptured(captured, name)
							}
						}
					}
				}
				return
			case "let", "loop":
				scanBindingFormCapture(ctx, e, env, v.Items[1:], targets, shadow, captured, false)
				return
			case "let*":
				scanBindingFormCapture(ctx, e, env, v.Items[1:], targets, shadow, captured, true)
				return
			}
		}
		for _, item := range v.Items {
			scanLoopCapture(ctx, e, env, item, targets, shadow, captured)
		}
	case Vector:
		for _, item := range v.Items {
			scanLoopCapture(ctx, e, env, item, targets, shadow, captured)
		}
	case *HashMap:
		v.Each(func(k, val Value) {
			scanLoopCapture(ctx, e, env, k, targets, shadow, captured)
			scanLoopCapture(ctx, e, env, val, targets, shadow, captured)
		})
	}
}

func scanBindingFormCapture(ctx context.Context, e *engine, env *Env, args []Value, targets, shadow map[string]bool, captured *map[string]bool, sequential bool) {
	if len(args) < 2 {
		for _, item := range args {
			scanLoopCapture(ctx, e, env, item, targets, shadow, captured)
		}
		return
	}
	bindings, err := NormalizeBindings("binding", args[0])
	if err != nil {
		for _, item := range args {
			scanLoopCapture(ctx, e, env, item, targets, shadow, captured)
		}
		return
	}
	bodyShadow := shadow
	for _, binding := range bindings {
		scanLoopCapture(ctx, e, env, binding.Value, targets, bodyShadow, captured)
		if sequential {
			bodyShadow = addShadow(bodyShadow, binding.Name.V)
		}
	}
	if !sequential {
		for _, binding := range bindings {
			bodyShadow = addShadow(bodyShadow, binding.Name.V)
		}
	}
	for _, item := range args[1:] {
		scanLoopCapture(ctx, e, env, item, targets, bodyShadow, captured)
	}
}

func markLoopCaptured(captured *map[string]bool, name string) {
	if *captured == nil {
		*captured = make(map[string]bool, 1)
	}
	(*captured)[name] = true
}

func formsReferenceTargets(ctx context.Context, e *engine, env *Env, forms []Value, targets, shadow map[string]bool) bool {
	for _, form := range forms {
		if formReferencesTarget(ctx, e, env, form, targets, shadow) {
			return true
		}
	}
	return false
}

func formReferencesTarget(ctx context.Context, e *engine, env *Env, form Value, targets, shadow map[string]bool) bool {
	switch v := form.(type) {
	case Symbol:
		return targets[v.V] && !shadow[v.V]
	case Vector:
		for _, item := range v.Items {
			if formReferencesTarget(ctx, e, env, item, targets, shadow) {
				return true
			}
		}
	case *HashMap:
		found := false
		v.Each(func(k, val Value) {
			if found {
				return
			}
			found = formReferencesTarget(ctx, e, env, k, targets, shadow) || formReferencesTarget(ctx, e, env, val, targets, shadow)
		})
		return found
	case List:
		return listReferencesTarget(ctx, e, env, v.Items, targets, shadow)
	}
	return false
}

func listReferencesTarget(ctx context.Context, e *engine, env *Env, items []Value, targets, shadow map[string]bool) bool {
	if len(items) == 0 {
		return false
	}
	list := List{Items: items}
	if expanded, ok := expandMacroForLoopScan(ctx, e, list, env); ok {
		return formReferencesTarget(ctx, e, env, expanded, targets, shadow)
	}
	head, special := items[0].(Symbol)
	if !special {
		return formsReferenceTargets(ctx, e, env, items, targets, shadow)
	}
	switch head.V {
	case "quote":
		return false
	case "fn":
		if len(items) < 3 {
			return false
		}
		params, err := paramsAsVector(items[1])
		if err != nil {
			return false
		}
		fixed, variadic, err := parseParams(params)
		if err != nil {
			return false
		}
		return formsReferenceTargets(ctx, e, env, items[2:], targets, extendShadow(shadow, fixed, variadic))
	case "defn", "defmacro":
		if len(items) < 4 {
			return false
		}
		params, err := paramsAsVector(items[2])
		if err != nil {
			return false
		}
		fixed, variadic, err := parseParams(params)
		if err != nil {
			return false
		}
		return formsReferenceTargets(ctx, e, env, items[3:], targets, extendShadow(shadow, fixed, variadic))
	case "let", "loop":
		return bindingFormReferencesTarget(ctx, e, env, items[1:], targets, shadow, false)
	case "let*":
		return bindingFormReferencesTarget(ctx, e, env, items[1:], targets, shadow, true)
	case "set!":
		if len(items) != 3 {
			return formsReferenceTargets(ctx, e, env, items[1:], targets, shadow)
		}
		if name, ok := items[1].(Symbol); ok && targets[name.V] && !shadow[name.V] {
			return true
		}
		return formReferencesTarget(ctx, e, env, items[2], targets, shadow)
	default:
		return formsReferenceTargets(ctx, e, env, items, targets, shadow)
	}
}

func bindingFormReferencesTarget(ctx context.Context, e *engine, env *Env, args []Value, targets, shadow map[string]bool, sequential bool) bool {
	if len(args) < 2 {
		return formsReferenceTargets(ctx, e, env, args, targets, shadow)
	}
	bindings, err := NormalizeBindings("binding", args[0])
	if err != nil {
		return formsReferenceTargets(ctx, e, env, args, targets, shadow)
	}
	bodyShadow := shadow
	for _, binding := range bindings {
		if formReferencesTarget(ctx, e, env, binding.Value, targets, bodyShadow) {
			return true
		}
		if sequential {
			bodyShadow = addShadow(bodyShadow, binding.Name.V)
		}
	}
	if !sequential {
		for _, binding := range bindings {
			bodyShadow = addShadow(bodyShadow, binding.Name.V)
		}
	}
	return formsReferenceTargets(ctx, e, env, args[1:], targets, bodyShadow)
}

func extendShadow(shadow map[string]bool, fixed []Symbol, variadic Symbol) map[string]bool {
	next := shadow
	for _, sym := range fixed {
		next = addShadow(next, sym.V)
	}
	if variadic.V != "" {
		next = addShadow(next, variadic.V)
	}
	return next
}

func addShadow(shadow map[string]bool, name string) map[string]bool {
	if shadow[name] {
		return shadow
	}
	next := make(map[string]bool, len(shadow)+1)
	maps.Copy(next, shadow)
	next[name] = true
	return next
}

func evalLoop(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
	if len(args) < 2 {
		return nil, evalErrorf("loop requires at least 2 arguments")
	}
	bindings, err := NormalizeBindings("loop", args[0])
	if err != nil {
		return nil, evalErrorf("%s", err)
	}

	loopEnv := env.Child()
	loopVars := make([]Symbol, 0, len(bindings))

	for _, binding := range bindings {
		val, err := e.Eval(ctx, binding.Value, env)
		if err != nil {
			return nil, err
		}
		if err := loopEnv.Set(binding.Name.V, val); err != nil {
			return nil, err
		}
		loopVars = append(loopVars, binding.Name)
	}
	captured := loopCapturedVars(ctx, e, args[1:], loopVars, loopEnv)
	st := evalStateFrom(ctx)
	st.loopDepth.Add(1)
	defer func() { st.loopDepth.Add(-1) }()

	for {
		result, err := e.evalBody(ctx, args[1:], loopEnv)
		if err != nil {
			return nil, err
		}
		rv, ok := result.(recurVal)
		if !ok {
			return result, nil
		}
		if len(rv.args) != len(loopVars) {
			return nil, evalErrorf("recur: expected %d args, got %d", len(loopVars), len(rv.args))
		}
		if len(captured) > 0 {
			next := loopEnv.forkCells(env, loopVars)
			for i, v := range loopVars {
				if captured[v.V] {
					if err := next.ReplaceCellWithContext(ctx, v.V, rv.args[i]); err != nil {
						return nil, err
					}
					continue
				}
				if err := next.SetWithContext(ctx, v.V, rv.args[i]); err != nil {
					return nil, err
				}
			}
			loopEnv = next
			continue
		}
		for i, v := range loopVars {
			if err := loopEnv.SetWithContext(ctx, v.V, rv.args[i]); err != nil {
				return nil, err
			}
		}
	}
}

func evalRecur(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
	st := evalStateFrom(ctx)
	if st.loopDepth.Load() == 0 {
		return nil, evalErrorf("recur outside loop")
	}
	vals := make([]Value, len(args))
	for i, arg := range args {
		v, err := e.Eval(ctx, arg, env)
		if err != nil {
			return nil, err
		}
		vals[i] = v
	}
	return recurVal{args: vals}, nil
}

func evalTry(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
	if len(args) < 2 {
		return nil, evalErrorf("try: requires a body and a (catch ...) clause")
	}

	catchClause, ok := args[len(args)-1].(List)
	if !ok || len(catchClause.Items) < 3 {
		return nil, evalErrorf("try: last argument must be (catch <sym> <handler>)")
	}
	catchSym, ok := catchClause.Items[0].(Symbol)
	if !ok || catchSym.V != "catch" {
		return nil, evalErrorf("try: expected catch clause, got %v", catchClause.Items[0])
	}
	errSymIndex := 1
	bodyStart := 2
	if len(catchClause.Items) >= 4 {
		errSymIndex = 2
		bodyStart = 3
	}
	errSym, ok := catchClause.Items[errSymIndex].(Symbol)
	if !ok {
		return nil, evalErrorf("catch: error binding must be a symbol")
	}

	body := args[:len(args)-1]
	result, err := e.evalBody(ctx, body, env)
	if err != nil {
		if IsTerminalEvalError(err) {
			return nil, err
		}
		catchEnv := env.Child()
		if err := catchEnv.Set(errSym.V, String{V: err.Error()}); err != nil {
			return nil, err
		}
		return e.evalBody(ctx, catchClause.Items[bodyStart:], catchEnv)
	}
	return result, nil
}

func evalCatch(_ context.Context, _ *engine, _ []Value, _ *Env) (Value, error) {
	return nil, evalErrorf("catch used outside of try")
}

// throwError wraps a LispicoError so errors.As can recover the typed error while
// preserving the original tree-walker behavior of exposing only the thrown value's
// text in err.Error() (used by catch binding).
type throwError struct {
	value Value
	cause *LispicoError
}

func (e *throwError) Error() string { return e.cause.Message }
func (e *throwError) Unwrap() error { return e.cause }

func evalThrow(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
	if len(args) != 1 {
		return nil, evalErrorf("throw requires exactly 1 argument")
	}
	val, err := e.Eval(ctx, args[0], env)
	if err != nil {
		return nil, err
	}
	if s, ok := val.(String); ok {
		return nil, &throwError{value: val, cause: &LispicoError{Code: "ThrowError", Message: s.V}}
	}
	return nil, &throwError{value: val, cause: &LispicoError{Code: "ThrowError", Message: fmt.Sprintf("%v", val)}}
}

func evalAnd(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
	if len(args) == 0 {
		return Bool{V: true}, nil
	}
	var last Value = Bool{V: true}
	for _, arg := range args {
		v, err := e.Eval(ctx, arg, env)
		if err != nil {
			return nil, err
		}
		last = v
		if !e.truthy(v) {
			return v, nil
		}
	}
	return last, nil
}

func evalOr(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
	if len(args) == 0 {
		return Nil{}, nil
	}
	var last Value = Nil{}
	for _, arg := range args {
		v, err := e.Eval(ctx, arg, env)
		if err != nil {
			return nil, err
		}
		last = v
		if e.truthy(v) {
			return v, nil
		}
	}
	return last, nil
}

func evalNot(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
	if len(args) != 1 {
		return nil, evalErrorf("not requires exactly 1 argument")
	}
	v, err := e.Eval(ctx, args[0], env)
	if err != nil {
		return nil, err
	}
	return Bool{V: !e.truthy(v)}, nil
}

// evalFuncall implements the Lisp-2 funcall form: it applies a function value
// taken from value position to the remaining, evaluated arguments.
func evalFuncall(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
	if len(args) < 1 {
		return nil, evalErrorf("funcall requires at least 1 argument")
	}
	fn, err := e.Eval(ctx, args[0], env)
	if err != nil {
		return nil, err
	}
	callArgs := make([]Value, len(args)-1)
	for i, arg := range args[1:] {
		v, err := e.Eval(ctx, arg, env)
		if err != nil {
			return nil, err
		}
		callArgs[i] = v
	}
	return e.apply(ctx, fn, callArgs, env)
}

// evalFunction implements the Lisp-2 function form — the #'name reference. It
// yields the function-cell binding of its symbol argument.
func evalFunction(_ context.Context, _ *engine, args []Value, env *Env) (Value, error) {
	if len(args) != 1 {
		return nil, evalErrorf("function requires exactly 1 argument")
	}
	name, ok := args[0].(Symbol)
	if !ok {
		return nil, evalErrorf("function: argument must be a symbol, got %T", args[0])
	}
	fn, ok := env.GetFunc(name.V)
	if !ok {
		return nil, NewUndefinedError(name.V)
	}
	return fn, nil
}
