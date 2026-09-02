package stdlib

import (
	"context"
	"math"

	"github.com/victorzhuk/go-lispico/core"
)

func (p *Plugin) registerArithmetic(env *core.Env) error {
	if err := env.RegisterValue("+", core.GoFunc{
		Name: "+",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			return addNumbers(ctx, args)
		},
	}, true); err != nil {
		return err
	}

	if err := env.RegisterValue("-", core.GoFunc{
		Name: "-",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			return subtractNumbers(ctx, args)
		},
	}, true); err != nil {
		return err
	}

	if err := env.RegisterValue("*", core.GoFunc{
		Name: "*",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			return multiplyNumbers(ctx, args)
		},
	}, true); err != nil {
		return err
	}

	if err := env.RegisterValue("/", core.GoFunc{
		Name: "/",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			return divideNumbers(ctx, args)
		},
	}, true); err != nil {
		return err
	}

	if err := env.RegisterValue("mod", core.GoFunc{
		Name: "mod",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 2 {
				return nil, arityErrorf("mod: requires 2 arguments")
			}

			a, ok1 := args[0].(core.Int)
			b, ok2 := args[1].(core.Int)

			if !ok1 || !ok2 {
				return nil, typeErrorf("mod: requires integer arguments")
			}

			if b.V == 0 {
				return nil, domainErrorf("mod: division by zero")
			}

			return core.BoxInt(a.V % b.V), nil
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("quot", core.GoFunc{
		Name: "quot",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 2 {
				return nil, arityErrorf("quot: requires 2 arguments")
			}

			a, ok1 := args[0].(core.Int)
			b, ok2 := args[1].(core.Int)

			if !ok1 || !ok2 {
				return nil, typeErrorf("quot: requires integer arguments")
			}

			if b.V == 0 {
				return nil, domainErrorf("quot: division by zero")
			}

			return core.BoxInt(a.V / b.V), nil
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("pow", core.GoFunc{
		Name: "pow",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 2 {
				return nil, arityErrorf("pow: requires 2 arguments")
			}

			var base, exp float64

			switch v := args[0].(type) {
			case core.Int:
				base = float64(v.V)
			case core.Float:
				base = v.V
			default:
				return nil, typeErrorf("pow: expected number, got %T", args[0])
			}

			switch v := args[1].(type) {
			case core.Int:
				exp = float64(v.V)
			case core.Float:
				exp = v.V
			default:
				return nil, typeErrorf("pow: expected number, got %T", args[1])
			}

			return core.Float{V: math.Pow(base, exp)}, nil
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("sqrt", core.GoFunc{
		Name: "sqrt",
		Fn:   unaryMathFunc("sqrt", math.Sqrt),
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("abs", core.GoFunc{
		Name: "abs",
		Fn:   unaryMathFunc("abs", math.Abs),
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("floor", core.GoFunc{
		Name: "floor",
		Fn:   unaryMathFunc("floor", math.Floor),
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("ceil", core.GoFunc{
		Name: "ceil",
		Fn:   unaryMathFunc("ceil", math.Ceil),
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("zero?", core.GoFunc{
		Name: "zero?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, arityErrorf("zero?: requires 1 argument")
			}

			switch v := args[0].(type) {
			case core.Int:
				return core.BoxBool(v.V == 0), nil
			case core.Float:
				return core.BoxBool(v.V == 0), nil
			default:
				return core.BoxBool(false), nil
			}
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("pos?", core.GoFunc{
		Name: "pos?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, arityErrorf("pos?: requires 1 argument")
			}

			switch v := args[0].(type) {
			case core.Int:
				return core.BoxBool(v.V > 0), nil
			case core.Float:
				return core.BoxBool(v.V > 0), nil
			default:
				return core.BoxBool(false), nil
			}
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("neg?", core.GoFunc{
		Name: "neg?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, arityErrorf("neg?: requires 1 argument")
			}

			switch v := args[0].(type) {
			case core.Int:
				return core.BoxBool(v.V < 0), nil
			case core.Float:
				return core.BoxBool(v.V < 0), nil
			default:
				return core.BoxBool(false), nil
			}
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("max", core.GoFunc{
		Name: "max",
		Fn:   minMaxFunc("max", true),
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("min", core.GoFunc{
		Name: "min",
		Fn:   minMaxFunc("min", false),
	}, false); err != nil {
		return err
	}
	return nil
}

func addNumbers(ctx context.Context, args []core.Value) (core.Value, error) {
	budget := core.NewBuiltinWorkBudget(ctx)

	var intSum int64
	var floatSum float64
	hasFloat := false

	for _, arg := range args {
		if err := budget.Step(); err != nil {
			return finishBuiltin(budget, nil, err)
		}
		switch v := arg.(type) {
		case core.Int:
			if hasFloat {
				floatSum += float64(v.V)
			} else {
				intSum += v.V
			}
		case core.Float:
			if !hasFloat {
				floatSum = float64(intSum)
				hasFloat = true
			}
			floatSum += v.V
		default:
			return finishBuiltin(budget, nil, typeErrorf("+: expected number, got %T", arg))
		}
	}

	if hasFloat {
		return finishBuiltin(budget, core.Float{V: floatSum}, nil)
	}
	return finishBuiltin(budget, core.BoxInt(intSum), nil)
}

func subtractNumbers(ctx context.Context, args []core.Value) (core.Value, error) {
	budget := core.NewBuiltinWorkBudget(ctx)

	if len(args) == 0 {
		return finishBuiltin(budget, nil, arityErrorf("-: requires at least 1 argument"))
	}

	var intResult int64
	var floatResult float64
	hasFloat := false

	switch v := args[0].(type) {
	case core.Int:
		intResult = v.V
	case core.Float:
		floatResult = v.V
		hasFloat = true
	default:
		return finishBuiltin(budget, nil, typeErrorf("-: expected number, got %T", args[0]))
	}

	if len(args) == 1 {
		if hasFloat {
			return finishBuiltin(budget, core.Float{V: -floatResult}, nil)
		}
		return finishBuiltin(budget, core.BoxInt(-intResult), nil)
	}

	for _, arg := range args[1:] {
		if err := budget.Step(); err != nil {
			return finishBuiltin(budget, nil, err)
		}
		switch v := arg.(type) {
		case core.Int:
			if hasFloat {
				floatResult -= float64(v.V)
			} else {
				intResult -= v.V
			}
		case core.Float:
			if !hasFloat {
				floatResult = float64(intResult)
				hasFloat = true
			}
			floatResult -= v.V
		default:
			return finishBuiltin(budget, nil, typeErrorf("-: expected number, got %T", arg))
		}
	}

	if hasFloat {
		return finishBuiltin(budget, core.Float{V: floatResult}, nil)
	}
	return finishBuiltin(budget, core.BoxInt(intResult), nil)
}

func multiplyNumbers(ctx context.Context, args []core.Value) (core.Value, error) {
	budget := core.NewBuiltinWorkBudget(ctx)

	if len(args) == 0 {
		return finishBuiltin(budget, core.BoxInt(1), nil)
	}

	var intProd int64 = 1
	var floatProd float64 = 1
	hasFloat := false

	for _, arg := range args {
		if err := budget.Step(); err != nil {
			return finishBuiltin(budget, nil, err)
		}
		switch v := arg.(type) {
		case core.Int:
			if hasFloat {
				floatProd *= float64(v.V)
			} else {
				intProd *= v.V
			}
		case core.Float:
			if !hasFloat {
				floatProd = float64(intProd)
				hasFloat = true
			}
			floatProd *= v.V
		default:
			return finishBuiltin(budget, nil, typeErrorf("*: expected number, got %T", arg))
		}
	}

	if hasFloat {
		return finishBuiltin(budget, core.Float{V: floatProd}, nil)
	}
	return finishBuiltin(budget, core.BoxInt(intProd), nil)
}

func divideNumbers(ctx context.Context, args []core.Value) (core.Value, error) {
	budget := core.NewBuiltinWorkBudget(ctx)

	if len(args) < 2 {
		return finishBuiltin(budget, nil, arityErrorf("/: requires at least 2 arguments"))
	}

	var dividend float64
	switch v := args[0].(type) {
	case core.Int:
		dividend = float64(v.V)
	case core.Float:
		dividend = v.V
	default:
		return finishBuiltin(budget, nil, typeErrorf("/: expected number, got %T", args[0]))
	}

	for _, arg := range args[1:] {
		if err := budget.Step(); err != nil {
			return finishBuiltin(budget, nil, err)
		}
		var divisor float64
		switch v := arg.(type) {
		case core.Int:
			if v.V == 0 {
				return finishBuiltin(budget, nil, domainErrorf("/: division by zero"))
			}
			divisor = float64(v.V)
		case core.Float:
			if v.V == 0 {
				return finishBuiltin(budget, nil, domainErrorf("/: division by zero"))
			}
			divisor = v.V
		default:
			return finishBuiltin(budget, nil, typeErrorf("/: expected number, got %T", arg))
		}
		dividend /= divisor
	}

	return finishBuiltin(budget, core.Float{V: dividend}, nil)
}

func unaryMathFunc(name string, fn func(float64) float64) func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
	return func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
		if len(args) != 1 {
			return nil, arityErrorf("%s: requires 1 argument", name)
		}

		var x float64
		switch v := args[0].(type) {
		case core.Int:
			x = float64(v.V)
		case core.Float:
			x = v.V
		default:
			return nil, typeErrorf("%s: expected number, got %T", name, args[0])
		}

		return core.Float{V: fn(x)}, nil
	}
}

func minMaxFunc(name string, isMax bool) func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
	return func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
		budget := core.NewBuiltinWorkBudget(ctx)

		if len(args) == 0 {
			return finishBuiltin(budget, nil, arityErrorf("%s: requires at least 1 argument", name))
		}

		var result float64
		hasFloat := false

		switch v := args[0].(type) {
		case core.Int:
			result = float64(v.V)
		case core.Float:
			result = v.V
			hasFloat = true
		default:
			return finishBuiltin(budget, nil, typeErrorf("%s: expected number, got %T", name, args[0]))
		}

		for _, arg := range args[1:] {
			if err := budget.Step(); err != nil {
				return finishBuiltin(budget, nil, err)
			}
			var x float64
			switch v := arg.(type) {
			case core.Int:
				x = float64(v.V)
			case core.Float:
				x = v.V
				hasFloat = true
			default:
				return finishBuiltin(budget, nil, typeErrorf("%s: expected number, got %T", name, arg))
			}

			if isMax {
				if x > result {
					result = x
				}
			} else {
				if x < result {
					result = x
				}
			}
		}

		if hasFloat {
			return finishBuiltin(budget, core.Float{V: result}, nil)
		}
		return finishBuiltin(budget, core.BoxInt(int64(result)), nil)
	}
}
