package stdlib

import (
	"context"
	"fmt"

	"github.com/victorzhuk/go-lispico/core"
)

func (p *Plugin) registerTypes(env *core.Env) {
	env.Set("type", core.GoFunc{
		Name: "type",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("type: requires 1 argument")
			}
			return args[0].Type(), nil
		},
	})

	env.Set("nil?", core.GoFunc{
		Name: "nil?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("nil?: requires 1 argument")
			}
			_, ok := args[0].(core.Nil)
			return core.Bool{V: ok}, nil
		},
	})

	env.Set("bool?", core.GoFunc{
		Name: "bool?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("bool?: requires 1 argument")
			}
			_, ok := args[0].(core.Bool)
			return core.Bool{V: ok}, nil
		},
	})

	env.Set("int?", core.GoFunc{
		Name: "int?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("int?: requires 1 argument")
			}
			_, ok := args[0].(core.Int)
			return core.Bool{V: ok}, nil
		},
	})

	env.Set("float?", core.GoFunc{
		Name: "float?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("float?: requires 1 argument")
			}
			_, ok := args[0].(core.Float)
			return core.Bool{V: ok}, nil
		},
	})

	env.Set("string?", core.GoFunc{
		Name: "string?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("string?: requires 1 argument")
			}
			_, ok := args[0].(core.String)
			return core.Bool{V: ok}, nil
		},
	})

	env.Set("keyword?", core.GoFunc{
		Name: "keyword?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("keyword?: requires 1 argument")
			}
			_, ok := args[0].(core.Keyword)
			return core.Bool{V: ok}, nil
		},
	})

	env.Set("symbol?", core.GoFunc{
		Name: "symbol?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("symbol?: requires 1 argument")
			}
			_, ok := args[0].(core.Symbol)
			return core.Bool{V: ok}, nil
		},
	})

	env.Set("list?", core.GoFunc{
		Name: "list?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("list?: requires 1 argument")
			}
			_, ok := args[0].(core.List)
			return core.Bool{V: ok}, nil
		},
	})

	env.Set("vector?", core.GoFunc{
		Name: "vector?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("vector?: requires 1 argument")
			}
			_, ok := args[0].(core.Vector)
			return core.Bool{V: ok}, nil
		},
	})

	env.Set("map?", core.GoFunc{
		Name: "map?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("map?: requires 1 argument")
			}
			_, ok := args[0].(*core.HashMap)
			return core.Bool{V: ok}, nil
		},
	})

	env.Set("fn?", core.GoFunc{
		Name: "fn?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("fn?: requires 1 argument")
			}
			switch args[0].(type) {
			case core.GoFunc, core.Lambda:
				return core.Bool{V: true}, nil
			default:
				return core.Bool{V: false}, nil
			}
		},
	})

	env.Set("macro?", core.GoFunc{
		Name: "macro?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("macro?: requires 1 argument")
			}
			_, ok := args[0].(core.Macro)
			return core.Bool{V: ok}, nil
		},
	})

	env.Set("str->keyword", core.GoFunc{
		Name: "str->keyword",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("str->keyword: requires 1 argument")
			}
			s, ok := args[0].(core.String)
			if !ok {
				return nil, fmt.Errorf("str->keyword: requires string argument")
			}
			return core.Keyword(s), nil
		},
	})

	env.Set("keyword->str", core.GoFunc{
		Name: "keyword->str",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("keyword->str: requires 1 argument")
			}
			k, ok := args[0].(core.Keyword)
			if !ok {
				return nil, fmt.Errorf("keyword->str: requires keyword argument")
			}
			return core.String(k), nil
		},
	})

	env.Set("int->float", core.GoFunc{
		Name: "int->float",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("int->float: requires 1 argument")
			}
			i, ok := args[0].(core.Int)
			if !ok {
				return nil, fmt.Errorf("int->float: requires integer argument")
			}
			return core.Float{V: float64(i.V)}, nil
		},
	})

	env.Set("float->int", core.GoFunc{
		Name: "float->int",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("float->int: requires 1 argument")
			}
			f, ok := args[0].(core.Float)
			if !ok {
				return nil, fmt.Errorf("float->int: requires float argument")
			}
			return core.Int{V: int64(f.V)}, nil
		},
	})
}
