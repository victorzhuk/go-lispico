package stdlib

import (
	"context"
	"fmt"
	"math"

	"github.com/victorzhuk/go-lispico/core"
)

func (p *Plugin) registerArithmetic(env *core.Env) {
	env.Set("+", core.GoFunc{
		Name: "+",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			var intSum int64
			var floatSum float64
			hasFloat := false

			for _, arg := range args {
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
					return nil, fmt.Errorf("+: expected number, got %T", arg)
				}
			}

			if hasFloat {
				return core.Float{V: floatSum}, nil
			}
			return core.Int{V: intSum}, nil
		},
	})

	env.Set("-", core.GoFunc{
		Name: "-",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) == 0 {
				return nil, fmt.Errorf("-: requires at least 1 argument")
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
				return nil, fmt.Errorf("-: expected number, got %T", args[0])
			}

			if len(args) == 1 {
				if hasFloat {
					return core.Float{V: -floatResult}, nil
				}
				return core.Int{V: -intResult}, nil
			}

			for _, arg := range args[1:] {
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
					return nil, fmt.Errorf("-: expected number, got %T", arg)
				}
			}

			if hasFloat {
				return core.Float{V: floatResult}, nil
			}
			return core.Int{V: intResult}, nil
		},
	})

	env.Set("*", core.GoFunc{
		Name: "*",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) == 0 {
				return core.Int{V: 1}, nil
			}

			var intProd int64 = 1
			var floatProd float64 = 1
			hasFloat := false

			for _, arg := range args {
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
					return nil, fmt.Errorf("*: expected number, got %T", arg)
				}
			}

			if hasFloat {
				return core.Float{V: floatProd}, nil
			}
			return core.Int{V: intProd}, nil
		},
	})

	env.Set("/", core.GoFunc{
		Name: "/",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) < 2 {
				return nil, fmt.Errorf("/: requires at least 2 arguments")
			}

			var dividend float64
			switch v := args[0].(type) {
			case core.Int:
				dividend = float64(v.V)
			case core.Float:
				dividend = v.V
			default:
				return nil, fmt.Errorf("/: expected number, got %T", args[0])
			}

			for _, arg := range args[1:] {
				var divisor float64
				switch v := arg.(type) {
				case core.Int:
					if v.V == 0 {
						return nil, fmt.Errorf("/: division by zero")
					}
					divisor = float64(v.V)
				case core.Float:
					if v.V == 0 {
						return nil, fmt.Errorf("/: division by zero")
					}
					divisor = v.V
				default:
					return nil, fmt.Errorf("/: expected number, got %T", arg)
				}
				dividend /= divisor
			}

			return core.Float{V: dividend}, nil
		},
	})

	env.Set("mod", core.GoFunc{
		Name: "mod",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("mod: requires 2 arguments")
			}

			a, ok1 := args[0].(core.Int)
			b, ok2 := args[1].(core.Int)

			if !ok1 || !ok2 {
				return nil, fmt.Errorf("mod: requires integer arguments")
			}

			if b.V == 0 {
				return nil, fmt.Errorf("mod: division by zero")
			}

			return core.Int{V: a.V % b.V}, nil
		},
	})

	env.Set("quot", core.GoFunc{
		Name: "quot",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("quot: requires 2 arguments")
			}

			a, ok1 := args[0].(core.Int)
			b, ok2 := args[1].(core.Int)

			if !ok1 || !ok2 {
				return nil, fmt.Errorf("quot: requires integer arguments")
			}

			if b.V == 0 {
				return nil, fmt.Errorf("quot: division by zero")
			}

			return core.Int{V: a.V / b.V}, nil
		},
	})

	env.Set("pow", core.GoFunc{
		Name: "pow",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("pow: requires 2 arguments")
			}

			var base, exp float64

			switch v := args[0].(type) {
			case core.Int:
				base = float64(v.V)
			case core.Float:
				base = v.V
			default:
				return nil, fmt.Errorf("pow: expected number, got %T", args[0])
			}

			switch v := args[1].(type) {
			case core.Int:
				exp = float64(v.V)
			case core.Float:
				exp = v.V
			default:
				return nil, fmt.Errorf("pow: expected number, got %T", args[1])
			}

			return core.Float{V: math.Pow(base, exp)}, nil
		},
	})

	env.Set("sqrt", core.GoFunc{
		Name: "sqrt",
		Fn:   unaryMathFunc(math.Sqrt),
	})

	env.Set("abs", core.GoFunc{
		Name: "abs",
		Fn:   unaryMathFunc(math.Abs),
	})

	env.Set("floor", core.GoFunc{
		Name: "floor",
		Fn:   unaryMathFunc(math.Floor),
	})

	env.Set("ceil", core.GoFunc{
		Name: "ceil",
		Fn:   unaryMathFunc(math.Ceil),
	})

	env.Set("zero?", core.GoFunc{
		Name: "zero?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("zero?: requires 1 argument")
			}

			switch v := args[0].(type) {
			case core.Int:
				return core.Bool{V: v.V == 0}, nil
			case core.Float:
				return core.Bool{V: v.V == 0}, nil
			default:
				return core.Bool{V: false}, nil
			}
		},
	})

	env.Set("pos?", core.GoFunc{
		Name: "pos?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("pos?: requires 1 argument")
			}

			switch v := args[0].(type) {
			case core.Int:
				return core.Bool{V: v.V > 0}, nil
			case core.Float:
				return core.Bool{V: v.V > 0}, nil
			default:
				return core.Bool{V: false}, nil
			}
		},
	})

	env.Set("neg?", core.GoFunc{
		Name: "neg?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("neg?: requires 1 argument")
			}

			switch v := args[0].(type) {
			case core.Int:
				return core.Bool{V: v.V < 0}, nil
			case core.Float:
				return core.Bool{V: v.V < 0}, nil
			default:
				return core.Bool{V: false}, nil
			}
		},
	})

	env.Set("max", core.GoFunc{
		Name: "max",
		Fn:   minMaxFunc(true),
	})

	env.Set("min", core.GoFunc{
		Name: "min",
		Fn:   minMaxFunc(false),
	})
}

func unaryMathFunc(fn func(float64) float64) func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
	return func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("requires 1 argument")
		}

		var x float64
		switch v := args[0].(type) {
		case core.Int:
			x = float64(v.V)
		case core.Float:
			x = v.V
		default:
			return nil, fmt.Errorf("expected number, got %T", args[0])
		}

		return core.Float{V: fn(x)}, nil
	}
}

func minMaxFunc(isMax bool) func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
	return func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
		if len(args) == 0 {
			return nil, fmt.Errorf("requires at least 1 argument")
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
			return nil, fmt.Errorf("expected number, got %T", args[0])
		}

		for _, arg := range args[1:] {
			var x float64
			switch v := arg.(type) {
			case core.Int:
				x = float64(v.V)
			case core.Float:
				x = v.V
				hasFloat = true
			default:
				return nil, fmt.Errorf("expected number, got %T", arg)
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
			return core.Float{V: result}, nil
		}
		return core.Int{V: int64(result)}, nil
	}
}
