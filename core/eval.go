package core

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sync/atomic"
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
}

const defaultMaxStructuralDepth = 1024

// NewEvaluator constructs a tree-walking evaluator running the identity
func NewEvaluator() *engine {
	return &engine{maxMacroDepth: 100, MaxDepth: 1000, MaxStructuralDepth: defaultMaxStructuralDepth, forms: copyKernel(), truthy: IsTruthy}
}

// NewEvaluatorWithDialect constructs a tree-walking evaluator whose special
// forms are the resolved effective table of d. It fails if d references a
// canonical form absent from the kernel.
func NewEvaluatorWithDialect(d Dialect) (*engine, error) {
	forms, err := d.resolve()
	if err != nil {
		return nil, err
	}
	return &engine{maxMacroDepth: 100, MaxDepth: 1000, MaxStructuralDepth: defaultMaxStructuralDepth, forms: forms, truthy: d.isTruthy, lisp2: d.isLisp2(), dialect: d}, nil
}

func copyKernel() map[string]formFn {
	forms := make(map[string]formFn, len(kernel))
	maps.Copy(forms, kernel)
	return forms
}

// evalState holds the depth counters for a single top-level evaluation. It is
// carried in the context so concurrent evaluations on one engine never share
// call/loop/macro state.
type evalState struct {
	callDepth   atomic.Int64
	loopDepth   atomic.Int64
	macroDepth  atomic.Int64
	structDepth atomic.Int64
}

type evalStateKey struct{}

// ensureEvalState attaches a fresh evalState to ctx on the first (top-level)
// call and returns the existing one on nested calls.
func ensureEvalState(ctx context.Context) context.Context {
	if _, ok := ctx.Value(evalStateKey{}).(*evalState); ok {
		return ctx
	}
	return context.WithValue(ctx, evalStateKey{}, &evalState{})
}

func evalStateFrom(ctx context.Context) *evalState {
	if st, ok := ctx.Value(evalStateKey{}).(*evalState); ok {
		return st
	}
	return &evalState{}
}

// DetachEvalState returns a copy of ctx with a fresh evalState attached,
// preserving cancellation and any other context values. Use this when a new
// goroutine should evaluate with its own depth counters so it cannot race or
// trip MaxDepth against another evaluation that shares the same ancestor ctx.
func DetachEvalState(ctx context.Context) context.Context {
	return context.WithValue(ctx, evalStateKey{}, &evalState{})
}

// EnsureEvalState returns a context with a fresh evalState attached if one
// is not already present. Callers that propagate ctx through evaluator
// callbacks (VM GoFuncs, Apply) should call this at every entry point so
// the shared structural-depth counter is available.
func EnsureEvalState(ctx context.Context) context.Context {
	return ensureEvalState(ctx)
}

// EvalStructCounter returns the shared structDepth atomic from the eval state
// in ctx. Returns a private zero-valued atomic when ctx has no eval state (the
// pointer is never nil), enabling the VM to share a single structural depth
// counter with the tree-walker across Apply callbacks.
func EvalStructCounter(ctx context.Context) *atomic.Int64 {
	st := evalStateFrom(ctx)
	return &st.structDepth
}

func evalErrorf(format string, args ...any) *LispicoError {
	return &LispicoError{Code: "EvalError", Message: fmt.Sprintf(format, args...)}
}

func resourceLimitErrorf(format string, args ...any) *LispicoError {
	return &LispicoError{Code: CodeResourceLimit, Message: fmt.Sprintf(format, args...)}
}

// Apply is the public entry point for calling a Lisp value as a function.
// Used by the runtime API and plugins that invoke Lambdas from Go.
func (e *engine) Apply(ctx context.Context, fn Value, args []Value, env *Env) (Value, error) {
	ctx = ensureEvalState(ctx)
	return e.apply(ctx, fn, args, env)
}

// Eval evaluates form in env, returning the result.
func (e *engine) Eval(ctx context.Context, v Value, env *Env) (Value, error) {
	ctx = ensureEvalState(ctx)

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	switch val := v.(type) {
	case Nil, Bool, Int, Float, String, Keyword, GoFunc, Lambda, Macro:
		return val, nil
	case Vector:
		st := evalStateFrom(ctx)
		st.structDepth.Add(1)
		defer func() { st.structDepth.Add(-1) }()
		if e.MaxStructuralDepth > 0 && int(st.structDepth.Load()) > e.MaxStructuralDepth {
			return nil, resourceLimitErrorf("structural depth limit %d exceeded", e.MaxStructuralDepth)
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
	st.structDepth.Add(1)
	defer func() { st.structDepth.Add(-1) }()
	if e.MaxStructuralDepth > 0 && int(st.structDepth.Load()) > e.MaxStructuralDepth {
		return nil, resourceLimitErrorf("structural depth limit %d exceeded", e.MaxStructuralDepth)
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
	st.callDepth.Add(1)
	defer func() { st.callDepth.Add(-1) }()

	if e.MaxDepth > 0 && int(st.callDepth.Load()) > e.MaxDepth {
		return nil, evalErrorf("max call depth %d exceeded", e.MaxDepth)
	}

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		switch f := fn.(type) {
		case GoFunc:
			return f.Fn(ctx, e, args, env)
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
	ctx = ensureEvalState(ctx)

	list, ok := form.(List)
	if !ok || len(list.Items) == 0 {
		return form, nil
	}
	fn, err := e.Eval(ctx, list.Items[0], env)
	if err != nil {
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

// expandMacroForm runs the macro body with unevaluated args and returns the
// expansion as a Value. Does NOT evaluate the result — that is the caller's job.
func (e *engine) expandMacroForm(ctx context.Context, m Macro, args []Value) (Value, error) {
	st := evalStateFrom(ctx)
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
		st.structDepth.Add(1)
		defer func() { st.structDepth.Add(-1) }()
		if e.MaxStructuralDepth > 0 && int(st.structDepth.Load()) > e.MaxStructuralDepth {
			return nil, resourceLimitErrorf("structural depth limit %d exceeded", e.MaxStructuralDepth)
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
						result = append(result, seq.Items...)
					case Vector:
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
			result = append(result, expanded)
		}
		return List{Items: result}, nil
	case Vector:
		st := evalStateFrom(ctx)
		st.structDepth.Add(1)
		defer func() { st.structDepth.Add(-1) }()
		if e.MaxStructuralDepth > 0 && int(st.structDepth.Load()) > e.MaxStructuralDepth {
			return nil, resourceLimitErrorf("structural depth limit %d exceeded", e.MaxStructuralDepth)
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
		st.structDepth.Add(1)
		defer func() { st.structDepth.Add(-1) }()
		if e.MaxStructuralDepth > 0 && int(st.structDepth.Load()) > e.MaxStructuralDepth {
			return nil, resourceLimitErrorf("structural depth limit %d exceeded", e.MaxStructuralDepth)
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

func (e *engine) CollectionLimit() int { return e.MaxCollectionLen }

// ── Special Form Implementations ─────────────────────────────────────────────

// bindOperator binds a function-defining form's result. Under Lisp-2 it lands in
// the function cell so head position can find it; under Lisp-1 it shares the
// single value namespace, exactly as before.
func (e *engine) bindOperator(env *Env, name string, val Value) {
	if e.lisp2 {
		env.SetFunc(name, val)
		return
	}
	env.Set(name, val)
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
	env.Set(name.V, val)
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
	e.bindOperator(env, name.V, lambda)
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
	e.bindOperator(env, name.V, macro)
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
	bindings, ok := args[0].(Vector)
	if !ok || len(bindings.Items)%2 != 0 {
		return nil, evalErrorf("let: first argument must be an even-length binding vector")
	}
	child := env.Child()
	for i := 0; i < len(bindings.Items); i += 2 {
		name, ok := bindings.Items[i].(Symbol)
		if !ok {
			return nil, evalErrorf("let: binding names must be symbols")
		}
		val, err := e.Eval(ctx, bindings.Items[i+1], env)
		if err != nil {
			return nil, err
		}
		child.Set(name.V, val)
	}
	return e.evalBody(ctx, args[1:], child)
}

func evalLetStar(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
	if len(args) < 2 {
		return nil, evalErrorf("let* requires at least 2 arguments")
	}
	bindings, ok := args[0].(Vector)
	if !ok || len(bindings.Items)%2 != 0 {
		return nil, evalErrorf("let*: first argument must be an even-length binding vector")
	}
	child := env.Child()
	for i := 0; i < len(bindings.Items); i += 2 {
		name, ok := bindings.Items[i].(Symbol)
		if !ok {
			return nil, evalErrorf("let*: binding names must be symbols")
		}
		val, err := e.Eval(ctx, bindings.Items[i+1], child) // evaluate in child env (sees previous)
		if err != nil {
			return nil, err
		}
		child.Set(name.V, val)
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
	defEnv.Set(name.V, val)
	return val, nil
}

func evalLoop(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
	if len(args) < 2 {
		return nil, evalErrorf("loop requires at least 2 arguments")
	}
	bindings, ok := args[0].(Vector)
	if !ok || len(bindings.Items)%2 != 0 {
		return nil, evalErrorf("loop: first argument must be an even-length binding vector")
	}

	loopEnv := env.Child()
	loopVars := make([]Symbol, 0, len(bindings.Items)/2)

	for i := 0; i < len(bindings.Items); i += 2 {
		name, ok := bindings.Items[i].(Symbol)
		if !ok {
			return nil, evalErrorf("loop: binding names must be symbols")
		}
		val, err := e.Eval(ctx, bindings.Items[i+1], env)
		if err != nil {
			return nil, err
		}
		loopEnv.Set(name.V, val)
		loopVars = append(loopVars, name)
	}

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
		for i, v := range loopVars {
			loopEnv.Set(v.V, rv.args[i])
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
		// Resource-limit breaches are a hard safety boundary: never catchable,
		// so a program cannot recover from resource exhaustion and continue.
		// This matches the bytecode VM, whose structural-depth opcode returns
		// the error directly rather than routing it through throw.
		var lerr *LispicoError
		if errors.As(err, &lerr) && lerr.Code == CodeResourceLimit {
			return nil, err
		}
		catchEnv := env.Child()
		catchEnv.Set(errSym.V, String{V: err.Error()})
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
