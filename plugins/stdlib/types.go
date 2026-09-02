package stdlib

import (
	"context"

	"github.com/victorzhuk/go-lispico/core"
)

func (p *Plugin) registerTypes(env *core.Env) error {
	if err := env.RegisterValue("type", core.GoFunc{
		Name: "type",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, arityErrorf("type: requires 1 argument")
			}
			return args[0].Type(), nil
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("nil?", core.GoFunc{
		Name: "nil?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, arityErrorf("nil?: requires 1 argument")
			}
			_, ok := args[0].(core.Nil)
			return core.Bool{V: ok}, nil
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("bool?", core.GoFunc{
		Name: "bool?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, arityErrorf("bool?: requires 1 argument")
			}
			_, ok := args[0].(core.Bool)
			return core.Bool{V: ok}, nil
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("int?", core.GoFunc{
		Name: "int?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, arityErrorf("int?: requires 1 argument")
			}
			_, ok := args[0].(core.Int)
			return core.Bool{V: ok}, nil
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("float?", core.GoFunc{
		Name: "float?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, arityErrorf("float?: requires 1 argument")
			}
			_, ok := args[0].(core.Float)
			return core.Bool{V: ok}, nil
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("string?", core.GoFunc{
		Name: "string?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, arityErrorf("string?: requires 1 argument")
			}
			_, ok := args[0].(core.String)
			return core.Bool{V: ok}, nil
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("keyword?", core.GoFunc{
		Name: "keyword?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, arityErrorf("keyword?: requires 1 argument")
			}
			_, ok := args[0].(core.Keyword)
			return core.Bool{V: ok}, nil
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("symbol?", core.GoFunc{
		Name: "symbol?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, arityErrorf("symbol?: requires 1 argument")
			}
			_, ok := args[0].(core.Symbol)
			return core.Bool{V: ok}, nil
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("list?", core.GoFunc{
		Name: "list?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, arityErrorf("list?: requires 1 argument")
			}
			_, ok := args[0].(core.List)
			return core.Bool{V: ok}, nil
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("vector?", core.GoFunc{
		Name: "vector?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, arityErrorf("vector?: requires 1 argument")
			}
			_, ok := args[0].(core.Vector)
			return core.Bool{V: ok}, nil
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("map?", core.GoFunc{
		Name: "map?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, arityErrorf("map?: requires 1 argument")
			}
			_, ok := args[0].(*core.HashMap)
			return core.Bool{V: ok}, nil
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("fn?", core.GoFunc{
		Name: "fn?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, arityErrorf("fn?: requires 1 argument")
			}
			return core.Bool{V: args[0].Type().V == "fn"}, nil
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("macro?", core.GoFunc{
		Name: "macro?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, arityErrorf("macro?: requires 1 argument")
			}
			_, ok := args[0].(core.Macro)
			return core.Bool{V: ok}, nil
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("str->keyword", core.GoFunc{
		Name: "str->keyword",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, arityErrorf("str->keyword: requires 1 argument")
			}
			s, ok := args[0].(core.String)
			if !ok {
				return nil, typeErrorf("str->keyword: requires string argument")
			}
			if err := chargeBorrowedResult(ctx); err != nil {
				return nil, err
			}
			return core.Keyword(s), nil
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("keyword->str", core.GoFunc{
		Name: "keyword->str",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, arityErrorf("keyword->str: requires 1 argument")
			}
			k, ok := args[0].(core.Keyword)
			if !ok {
				return nil, typeErrorf("keyword->str: requires keyword argument")
			}
			if err := chargeBorrowedResult(ctx); err != nil {
				return nil, err
			}
			return core.String(k), nil
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("int->float", core.GoFunc{
		Name: "int->float",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, arityErrorf("int->float: requires 1 argument")
			}
			i, ok := args[0].(core.Int)
			if !ok {
				return nil, typeErrorf("int->float: requires integer argument")
			}
			return core.Float{V: float64(i.V)}, nil
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("float->int", core.GoFunc{
		Name: "float->int",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, arityErrorf("float->int: requires 1 argument")
			}
			f, ok := args[0].(core.Float)
			if !ok {
				return nil, typeErrorf("float->int: requires float argument")
			}
			return core.Int{V: int64(f.V)}, nil
		},
	}, false); err != nil {
		return err
	}
	return nil
}
