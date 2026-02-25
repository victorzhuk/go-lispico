package stdlib

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/victorzhuk/go-lispico/core"
)

func (p *Plugin) registerStrings(env *core.Env) {
	env.Set("str", core.GoFunc{
		Name: "str",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			var buf strings.Builder
			for _, arg := range args {
				buf.WriteString(toString(arg))
			}
			return core.String{V: buf.String()}, nil
		},
	})

	env.Set("format", core.GoFunc{
		Name: "format",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) < 1 {
				return nil, fmt.Errorf("format: requires at least 1 argument")
			}

			fmtStr, ok := args[0].(core.String)
			if !ok {
				return nil, fmt.Errorf("format: first argument must be string")
			}

			fmtArgs := make([]any, len(args)-1)
			for i, arg := range args[1:] {
				fmtArgs[i] = toAny(arg)
			}

			return core.String{V: fmt.Sprintf(fmtStr.V, fmtArgs...)}, nil
		},
	})

	env.Set("string/join", core.GoFunc{
		Name: "string/join",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("string/join: requires 2 arguments")
			}

			coll := args[0]
			sep, ok := args[1].(core.String)
			if !ok {
				return nil, fmt.Errorf("string/join: separator must be string")
			}

			var parts []string
			switch c := coll.(type) {
			case core.List:
				for _, item := range c.Items {
					parts = append(parts, toString(item))
				}
			case core.Vector:
				for _, item := range c.Items {
					parts = append(parts, toString(item))
				}
			default:
				return nil, fmt.Errorf("string/join: expected collection, got %T", coll)
			}

			return core.String{V: strings.Join(parts, sep.V)}, nil
		},
	})

	env.Set("string/split", core.GoFunc{
		Name: "string/split",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("string/split: requires 2 arguments")
			}

			s, ok1 := args[0].(core.String)
			sep, ok2 := args[1].(core.String)

			if !ok1 || !ok2 {
				return nil, fmt.Errorf("string/split: requires string arguments")
			}

			parts := strings.Split(s.V, sep.V)
			items := make([]core.Value, len(parts))
			for i, p := range parts {
				items[i] = core.String{V: p}
			}

			return core.List{Items: items}, nil
		},
	})

	env.Set("string/trim", core.GoFunc{
		Name: "string/trim",
		Fn:   unaryStringFunc(strings.TrimSpace),
	})

	env.Set("string/upper", core.GoFunc{
		Name: "string/upper",
		Fn:   unaryStringFunc(strings.ToUpper),
	})

	env.Set("string/lower", core.GoFunc{
		Name: "string/lower",
		Fn:   unaryStringFunc(strings.ToLower),
	})

	env.Set("string/replace", core.GoFunc{
		Name: "string/replace",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 3 {
				return nil, fmt.Errorf("string/replace: requires 3 arguments")
			}

			s, ok1 := args[0].(core.String)
			old, ok2 := args[1].(core.String)
			new, ok3 := args[2].(core.String)

			if !ok1 || !ok2 || !ok3 {
				return nil, fmt.Errorf("string/replace: requires string arguments")
			}

			return core.String{V: strings.ReplaceAll(s.V, old.V, new.V)}, nil
		},
	})

	env.Set("string/contains?", core.GoFunc{
		Name: "string/contains?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("string/contains?: requires 2 arguments")
			}

			s, ok1 := args[0].(core.String)
			substr, ok2 := args[1].(core.String)

			if !ok1 || !ok2 {
				return nil, fmt.Errorf("string/contains?: requires string arguments")
			}

			return core.Bool{V: strings.Contains(s.V, substr.V)}, nil
		},
	})

	env.Set("string/starts-with?", core.GoFunc{
		Name: "string/starts-with?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("string/starts-with?: requires 2 arguments")
			}

			s, ok1 := args[0].(core.String)
			prefix, ok2 := args[1].(core.String)

			if !ok1 || !ok2 {
				return nil, fmt.Errorf("string/starts-with?: requires string arguments")
			}

			return core.Bool{V: strings.HasPrefix(s.V, prefix.V)}, nil
		},
	})

	env.Set("string/ends-with?", core.GoFunc{
		Name: "string/ends-with?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("string/ends-with?: requires 2 arguments")
			}

			s, ok1 := args[0].(core.String)
			suffix, ok2 := args[1].(core.String)

			if !ok1 || !ok2 {
				return nil, fmt.Errorf("string/ends-with?: requires string arguments")
			}

			return core.Bool{V: strings.HasSuffix(s.V, suffix.V)}, nil
		},
	})

	env.Set("string/length", core.GoFunc{
		Name: "string/length",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("string/length: requires 1 argument")
			}

			s, ok := args[0].(core.String)
			if !ok {
				return nil, fmt.Errorf("string/length: requires string argument")
			}

			return core.Int{V: int64(len([]rune(s.V)))}, nil
		},
	})

	env.Set("string/lines", core.GoFunc{
		Name: "string/lines",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("string/lines: requires 1 argument")
			}

			s, ok := args[0].(core.String)
			if !ok {
				return nil, fmt.Errorf("string/lines: requires string argument")
			}

			lines := strings.Split(s.V, "\n")
			items := make([]core.Value, len(lines))
			for i, line := range lines {
				items[i] = core.String{V: line}
			}

			return core.List{Items: items}, nil
		},
	})

	env.Set("string->int", core.GoFunc{
		Name: "string->int",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("string->int: requires 1 argument")
			}

			s, ok := args[0].(core.String)
			if !ok {
				return nil, fmt.Errorf("string->int: requires string argument")
			}

			i, err := strconv.ParseInt(s.V, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("string->int: %w", err)
			}

			return core.Int{V: i}, nil
		},
	})

	env.Set("string->float", core.GoFunc{
		Name: "string->float",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("string->float: requires 1 argument")
			}

			s, ok := args[0].(core.String)
			if !ok {
				return nil, fmt.Errorf("string->float: requires string argument")
			}

			f, err := strconv.ParseFloat(s.V, 64)
			if err != nil {
				return nil, fmt.Errorf("string->float: %w", err)
			}

			return core.Float{V: f}, nil
		},
	})
}

func unaryStringFunc(fn func(string) string) func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
	return func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("requires 1 argument")
		}

		s, ok := args[0].(core.String)
		if !ok {
			return nil, fmt.Errorf("expected string, got %T", args[0])
		}

		return core.String{V: fn(s.V)}, nil
	}
}

func toString(v core.Value) string {
	if s, ok := v.(core.String); ok {
		return s.V
	}
	return v.String()
}

func toAny(v core.Value) any {
	switch val := v.(type) {
	case core.Nil:
		return nil
	case core.Bool:
		return val.V
	case core.Int:
		return val.V
	case core.Float:
		return val.V
	case core.String:
		return val.V
	default:
		return v.String()
	}
}

func isTruthy(v core.Value) bool {
	if _, ok := v.(core.Nil); ok {
		return false
	}
	if b, ok := v.(core.Bool); ok {
		return b.V
	}
	return true
}
