package stdlib

import (
	"context"
	"fmt"

	"github.com/victorzhuk/go-lispico/core"
)

func (p *Plugin) registerTypes(env *core.Env) {
	env.RegisterValue("type", core.GoFunc{
		Name: "type",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("type: requires 1 argument")
			}
			return args[0].Type(), nil
		},
	}, false)

	env.RegisterValue("nil?", core.GoFunc{
		Name: "nil?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("nil?: requires 1 argument")
			}
			_, ok := args[0].(core.Nil)
			return core.Bool{V: ok}, nil
		},
	}, false)

	env.RegisterValue("bool?", core.GoFunc{
		Name: "bool?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("bool?: requires 1 argument")
			}
			_, ok := args[0].(core.Bool)
			return core.Bool{V: ok}, nil
		},
	}, false)

	env.RegisterValue("int?", core.GoFunc{
		Name: "int?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("int?: requires 1 argument")
			}
			_, ok := args[0].(core.Int)
			return core.Bool{V: ok}, nil
		},
	}, false)

	env.RegisterValue("float?", core.GoFunc{
		Name: "float?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("float?: requires 1 argument")
			}
			_, ok := args[0].(core.Float)
			return core.Bool{V: ok}, nil
		},
	}, false)

	env.RegisterValue("string?", core.GoFunc{
		Name: "string?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("string?: requires 1 argument")
			}
			_, ok := args[0].(core.String)
			return core.Bool{V: ok}, nil
		},
	}, false)

	env.RegisterValue("keyword?", core.GoFunc{
		Name: "keyword?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("keyword?: requires 1 argument")
			}
			_, ok := args[0].(core.Keyword)
			return core.Bool{V: ok}, nil
		},
	}, false)

	env.RegisterValue("symbol?", core.GoFunc{
		Name: "symbol?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("symbol?: requires 1 argument")
			}
			_, ok := args[0].(core.Symbol)
			return core.Bool{V: ok}, nil
		},
	}, false)

	env.RegisterValue("list?", core.GoFunc{
		Name: "list?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("list?: requires 1 argument")
			}
			_, ok := args[0].(core.List)
			return core.Bool{V: ok}, nil
		},
	}, false)

	env.RegisterValue("vector?", core.GoFunc{
		Name: "vector?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("vector?: requires 1 argument")
			}
			_, ok := args[0].(core.Vector)
			return core.Bool{V: ok}, nil
		},
	}, false)

	env.RegisterValue("map?", core.GoFunc{
		Name: "map?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("map?: requires 1 argument")
			}
			_, ok := args[0].(*core.HashMap)
			return core.Bool{V: ok}, nil
		},
	}, false)

	env.RegisterValue("fn?", core.GoFunc{
		Name: "fn?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("fn?: requires 1 argument")
			}
			return core.Bool{V: args[0].Type().V == "fn"}, nil
		},
	}, false)

	env.RegisterValue("macro?", core.GoFunc{
		Name: "macro?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("macro?: requires 1 argument")
			}
			_, ok := args[0].(core.Macro)
			return core.Bool{V: ok}, nil
		},
	}, false)

	env.RegisterValue("str->keyword", core.GoFunc{
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
	}, false)

	env.RegisterValue("keyword->str", core.GoFunc{
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
	}, false)

	env.RegisterValue("int->float", core.GoFunc{
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
	}, false)

	env.RegisterValue("float->int", core.GoFunc{
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
	}, false)
}
