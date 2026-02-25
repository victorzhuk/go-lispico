package core

import (
	"context"
	"fmt"
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

// specialForms is the dispatch table for all built-in syntax.
// Populated in init() to break the initialization cycle:
// specialForms → evalDef → engine.Eval → evalList → specialForms.
var specialForms map[string]func(context.Context, *engine, []Value, *Env) (Value, error)

func init() {
	specialForms = map[string]func(context.Context, *engine, []Value, *Env) (Value, error){
		"def":        evalDef,
		"defn":       evalDefn,
		"defmacro":   evalDefmacro,
		"fn":         evalFn,
		"if":         evalIf,
		"cond":       evalCond,
		"when":       evalWhen,
		"unless":     evalUnless,
		"let":        evalLet,
		"let*":       evalLetStar,
		"do":         evalDo,
		"quote":      evalQuote,
		"quasiquote": evalQuasiquote,
		"set!":       evalSet,
		"loop":       evalLoop,
		"recur":      evalRecur,
		"try":        evalTry,
		"catch":      evalCatch,
		"throw":      evalThrow,
		"and":        evalAnd,
		"or":         evalOr,
		"not":        evalNot,
	}
}

// engine is the concrete tree-walking evaluator.
// It implements the Evaluator interface from types.go.
type engine struct {
	macroDepth    int
	maxMacroDepth int
	MaxDepth      int
	callDepth     atomic.Int64
}

// NewEvaluator constructs a new tree-walking evaluator.
func NewEvaluator() *engine {
	return &engine{maxMacroDepth: 100, MaxDepth: 1000}
}

// Apply is the public entry point for calling a Lisp value as a function.
// Used by the runtime API and plugins that invoke Lambdas from Go.
func (e *engine) Apply(ctx context.Context, fn Value, args []Value, env *Env) (Value, error) {
	return e.apply(ctx, fn, args, env)
}

// Eval evaluates form in env, returning the result.
func (e *engine) Eval(ctx context.Context, v Value, env *Env) (Value, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	switch val := v.(type) {
	case Nil, Bool, Int, Float, String, Keyword, *HashMap, Vector, GoFunc, Lambda, Macro:
		return val, nil
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

func (e *engine) evalList(ctx context.Context, items []Value, env *Env) (Value, error) {
	head := items[0]

	if sym, ok := head.(Symbol); ok {
		if fn, ok := specialForms[sym.V]; ok {
			return fn(ctx, e, items[1:], env)
		}
	}

	fn, err := e.Eval(ctx, head, env)
	if err != nil {
		return nil, err
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
	depth := e.callDepth.Add(1)
	defer e.callDepth.Add(-1)

	if e.MaxDepth > 0 && int(depth) > e.MaxDepth {
		return nil, fmt.Errorf("max call depth %d exceeded", e.MaxDepth)
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
			tc, ok := result.(tailCall)
			if !ok {
				return result, nil
			}
			fn, args, env = tc.fn, tc.args, tc.env
		case Keyword:
			if len(args) != 1 {
				return nil, fmt.Errorf("keyword lookup requires exactly 1 argument, got %d", len(args))
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
	if e.macroDepth >= e.maxMacroDepth {
		return nil, fmt.Errorf("macro expansion depth %d exceeded", e.maxMacroDepth)
	}
	e.macroDepth++
	defer func() { e.macroDepth-- }()

	macroEnv, err := m.Env.ChildVariadic(m.Params, args, m.Variadic)
	if err != nil {
		return nil, fmt.Errorf("macro %s: %w", m.Name, err)
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
						return nil, fmt.Errorf("unquote requires 1 argument")
					}
					return e.Eval(ctx, val.Items[1], env)
				case "unquote-splicing":
					return nil, fmt.Errorf("unquote-splicing used outside of list context")
				}
			}
		}
		var result []Value
		for _, item := range val.Items {
			if list, ok := item.(List); ok && len(list.Items) > 0 {
				if sym, ok := list.Items[0].(Symbol); ok && sym.V == "unquote-splicing" {
					if len(list.Items) != 2 {
						return nil, fmt.Errorf("unquote-splicing requires 1 argument")
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
						return nil, fmt.Errorf("unquote-splicing requires a sequence, got %T", expanded)
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
		result := make([]Value, len(val.Items))
		for i, item := range val.Items {
			expanded, err := e.expandQuasiquote(ctx, item, env)
			if err != nil {
				return nil, err
			}
			result[i] = expanded
		}
		return Vector{Items: result}, nil
	default:
		return val, nil
	}
}

func isTruthy(v Value) bool {
	if _, ok := v.(Nil); ok {
		return false
	}
	if b, ok := v.(Bool); ok {
		return b.V
	}
	return true
}

// ── Special Form Implementations ─────────────────────────────────────────────

func evalDef(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("def requires 2 arguments, got %d", len(args))
	}
	name, ok := args[0].(Symbol)
	if !ok {
		return nil, fmt.Errorf("def: first argument must be a symbol, got %T", args[0])
	}
	val, err := e.Eval(ctx, args[1], env)
	if err != nil {
		return nil, err
	}
	env.Set(name.V, val)
	return val, nil
}

func evalDefn(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
	if len(args) < 3 {
		return nil, fmt.Errorf("defn requires at least 3 arguments (name params body...)")
	}
	name, ok := args[0].(Symbol)
	if !ok {
		return nil, fmt.Errorf("defn: first argument must be a symbol")
	}
	params, ok := args[1].(Vector)
	if !ok {
		return nil, fmt.Errorf("defn: second argument must be a parameter vector")
	}
	fixed, variadic, err := parseParams(params)
	if err != nil {
		return nil, fmt.Errorf("defn %s: %w", name.V, err)
	}
	lambda := Lambda{
		Name:     name.V,
		Params:   fixed,
		Variadic: variadic,
		Body:     args[2:],
		Env:      env,
	}
	env.Set(name.V, lambda)
	return lambda, nil
}

func evalDefmacro(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
	if len(args) < 3 {
		return nil, fmt.Errorf("defmacro requires at least 3 arguments (name params body...)")
	}
	name, ok := args[0].(Symbol)
	if !ok {
		return nil, fmt.Errorf("defmacro: first argument must be a symbol")
	}
	params, ok := args[1].(Vector)
	if !ok {
		return nil, fmt.Errorf("defmacro: second argument must be a parameter vector")
	}
	fixed, variadic, err := parseParams(params)
	if err != nil {
		return nil, fmt.Errorf("defmacro %s: %w", name.V, err)
	}
	macro := Macro{
		Name:     name.V,
		Params:   fixed,
		Variadic: variadic,
		Body:     args[2:],
		Env:      env,
	}
	env.Set(name.V, macro)
	return macro, nil
}

func evalFn(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("fn requires at least 2 arguments (params body...)")
	}
	params, ok := args[0].(Vector)
	if !ok {
		return nil, fmt.Errorf("fn: first argument must be a parameter vector")
	}
	fixed, variadic, err := parseParams(params)
	if err != nil {
		return nil, fmt.Errorf("fn: %w", err)
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
		return nil, fmt.Errorf("if requires 2 or 3 arguments")
	}
	cond, err := e.Eval(ctx, args[0], env)
	if err != nil {
		return nil, err
	}
	if isTruthy(cond) {
		return e.Eval(ctx, args[1], env)
	}
	if len(args) == 3 {
		return e.Eval(ctx, args[2], env)
	}
	return Nil{}, nil
}

func evalCond(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
	for _, clause := range args {
		list, ok := clause.(List)
		if !ok || len(list.Items) != 2 {
			return nil, fmt.Errorf("cond: clauses must be (test expr) pairs")
		}
		test := list.Items[0]
		if sym, ok := test.(Symbol); ok && (sym.V == "else" || sym.V == ":else") {
			return e.Eval(ctx, list.Items[1], env)
		}
		if kw, ok := test.(Keyword); ok && kw.V == "else" {
			return e.Eval(ctx, list.Items[1], env)
		}
		result, err := e.Eval(ctx, test, env)
		if err != nil {
			return nil, err
		}
		if isTruthy(result) {
			return e.Eval(ctx, list.Items[1], env)
		}
	}
	return Nil{}, nil
}

func evalWhen(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("when requires at least 2 arguments")
	}
	cond, err := e.Eval(ctx, args[0], env)
	if err != nil {
		return nil, err
	}
	if !isTruthy(cond) {
		return Nil{}, nil
	}
	return e.evalBody(ctx, args[1:], env)
}

func evalUnless(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("unless requires at least 2 arguments")
	}
	cond, err := e.Eval(ctx, args[0], env)
	if err != nil {
		return nil, err
	}
	if isTruthy(cond) {
		return Nil{}, nil
	}
	return e.evalBody(ctx, args[1:], env)
}

func evalLet(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("let requires at least 2 arguments")
	}
	bindings, ok := args[0].(Vector)
	if !ok || len(bindings.Items)%2 != 0 {
		return nil, fmt.Errorf("let: first argument must be an even-length binding vector")
	}
	child := env.Child()
	for i := 0; i < len(bindings.Items); i += 2 {
		name, ok := bindings.Items[i].(Symbol)
		if !ok {
			return nil, fmt.Errorf("let: binding names must be symbols")
		}
		val, err := e.Eval(ctx, bindings.Items[i+1], env) // evaluate in parent env
		if err != nil {
			return nil, err
		}
		child.Set(name.V, val)
	}
	return e.evalBody(ctx, args[1:], child)
}

func evalLetStar(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("let* requires at least 2 arguments")
	}
	bindings, ok := args[0].(Vector)
	if !ok || len(bindings.Items)%2 != 0 {
		return nil, fmt.Errorf("let*: first argument must be an even-length binding vector")
	}
	child := env.Child()
	for i := 0; i < len(bindings.Items); i += 2 {
		name, ok := bindings.Items[i].(Symbol)
		if !ok {
			return nil, fmt.Errorf("let*: binding names must be symbols")
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
		return nil, fmt.Errorf("quote requires exactly 1 argument")
	}
	return args[0], nil
}

func evalQuasiquote(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("quasiquote requires exactly 1 argument")
	}
	return e.expandQuasiquote(ctx, args[0], env)
}

func evalSet(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("set! requires exactly 2 arguments")
	}
	name, ok := args[0].(Symbol)
	if !ok {
		return nil, fmt.Errorf("set!: first argument must be a symbol")
	}
	defEnv, ok := env.Find(name.V)
	if !ok {
		return nil, fmt.Errorf("set!: cannot mutate undefined variable %q", name.V)
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
		return nil, fmt.Errorf("loop requires at least 2 arguments")
	}
	bindings, ok := args[0].(Vector)
	if !ok || len(bindings.Items)%2 != 0 {
		return nil, fmt.Errorf("loop: first argument must be an even-length binding vector")
	}

	loopEnv := env.Child()
	loopVars := make([]Symbol, 0, len(bindings.Items)/2)

	for i := 0; i < len(bindings.Items); i += 2 {
		name, ok := bindings.Items[i].(Symbol)
		if !ok {
			return nil, fmt.Errorf("loop: binding names must be symbols")
		}
		val, err := e.Eval(ctx, bindings.Items[i+1], env)
		if err != nil {
			return nil, err
		}
		loopEnv.Set(name.V, val)
		loopVars = append(loopVars, name)
	}

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
			return nil, fmt.Errorf("recur: expected %d args, got %d", len(loopVars), len(rv.args))
		}
		for i, v := range loopVars {
			loopEnv.Set(v.V, rv.args[i])
		}
	}
}

func evalRecur(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
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
		return nil, fmt.Errorf("try: requires a body and a (catch ...) clause")
	}

	catchClause, ok := args[len(args)-1].(List)
	if !ok || len(catchClause.Items) < 3 {
		return nil, fmt.Errorf("try: last argument must be (catch <sym> <handler>)")
	}
	catchSym, ok := catchClause.Items[0].(Symbol)
	if !ok || catchSym.V != "catch" {
		return nil, fmt.Errorf("try: expected catch clause, got %v", catchClause.Items[0])
	}
	errSym, ok := catchClause.Items[1].(Symbol)
	if !ok {
		return nil, fmt.Errorf("catch: error binding must be a symbol")
	}

	body := args[:len(args)-1]
	result, err := e.evalBody(ctx, body, env)
	if err != nil {
		catchEnv := env.Child()
		catchEnv.Set(errSym.V, String{V: err.Error()})
		return e.evalBody(ctx, catchClause.Items[2:], catchEnv)
	}
	return result, nil
}

func evalCatch(_ context.Context, _ *engine, _ []Value, _ *Env) (Value, error) {
	return nil, fmt.Errorf("catch used outside of try")
}

func evalThrow(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("throw requires exactly 1 argument")
	}
	val, err := e.Eval(ctx, args[0], env)
	if err != nil {
		return nil, err
	}
	if s, ok := val.(String); ok {
		return nil, fmt.Errorf("%s", s.V)
	}
	return nil, fmt.Errorf("%v", val)
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
		if !isTruthy(v) {
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
		if isTruthy(v) {
			return v, nil
		}
	}
	return last, nil
}

func evalNot(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("not requires exactly 1 argument")
	}
	v, err := e.Eval(ctx, args[0], env)
	if err != nil {
		return nil, err
	}
	return Bool{V: !isTruthy(v)}, nil
}
