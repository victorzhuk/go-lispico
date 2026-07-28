package stdlib

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/victorzhuk/go-lispico/core"
)

func (p *Plugin) registerStrings(env *core.Env) error {
	if err := env.RegisterValue("str", core.GoFunc{
		Name: "str",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			var buf strings.Builder
			for _, arg := range args {
				buf.WriteString(toString(arg))
			}
			return core.String{V: buf.String()}, nil
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("format", core.GoFunc{
		Name: "format",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) < 1 {
				return nil, fmt.Errorf("format: requires at least 1 argument")
			}

			fmtStr, ok := args[0].(core.String)
			if !ok {
				return nil, fmt.Errorf("format: first argument must be string")
			}

			if fmtStr.V == "" {
				return core.String{V: ""}, nil
			}
			if err := core.ChargeEvalAllocBytes(ctx, estimateFormatAllocBytes(fmtStr.V, args[1:])); err != nil {
				return nil, err
			}

			fmtArgs := make([]any, len(args)-1)
			for i, arg := range args[1:] {
				fmtArgs[i] = toAny(arg)
			}

			return core.String{V: fmt.Sprintf(fmtStr.V, fmtArgs...)}, nil
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("string/join", core.GoFunc{
		Name: "string/join",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("string/join: requires 2 arguments")
			}

			sep, ok := args[0].(core.String)
			if !ok {
				return nil, fmt.Errorf("string/join: separator must be string")
			}
			coll := args[1]

			var parts []string
			switch c := coll.(type) {
			case core.List:
				for _, item := range c.ToSlice() {
					parts = append(parts, toString(item))
				}
			case core.Vector:
				for _, item := range c.ToSlice() {
					parts = append(parts, toString(item))
				}
			default:
				return nil, fmt.Errorf("string/join: expected collection, got %T", coll)
			}

			return core.String{V: strings.Join(parts, sep.V)}, nil
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("string/split", core.GoFunc{
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

			return core.NewList(items), nil
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("string/trim", core.GoFunc{
		Name: "string/trim",
		Fn:   unaryStringFunc(strings.TrimSpace),
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("string/upper", core.GoFunc{
		Name: "string/upper",
		Fn:   unaryStringFunc(strings.ToUpper),
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("string/lower", core.GoFunc{
		Name: "string/lower",
		Fn:   unaryStringFunc(strings.ToLower),
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("string/replace", core.GoFunc{
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
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("string/contains?", core.GoFunc{
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
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("string/starts-with?", core.GoFunc{
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
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("string/ends-with?", core.GoFunc{
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
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("string/length", core.GoFunc{
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
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("string/lines", core.GoFunc{
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

			return core.NewList(items), nil
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("string->int", core.GoFunc{
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
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("string->float", core.GoFunc{
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
	}, false); err != nil {
		return err
	}
	return nil
}

const (
	maxFormatEstimate          = int64(^uint64(0) >> 1)
	minFormatValueStringScalar = 24
	maxDefaultFloatFormatBytes = 512
)

func estimateFormatAllocBytes(format string, args []core.Value) int64 {
	var out int64
	arg := 0

	for i := 0; i < len(format); {
		if format[i] != '%' {
			out = addFormatEstimate(out, 1)
			i++
			continue
		}

		i++
		if i >= len(format) {
			out = addFormatEstimate(out, 1)
			break
		}
		if format[i] == '%' {
			out = addFormatEstimate(out, 1)
			i++
			continue
		}

		estimateOne := func(i, arg int) (int64, int, int) {
			hasSpace, hasAlt := false, false
			for i < len(format) {
				switch format[i] {
				case '#', '0', '+', '-', ' ':
					switch format[i] {
					case ' ':
						hasSpace = true
					case '#':
						hasAlt = true
					}
					i++
				default:
					goto flagsDone
				}
			}
		flagsDone:

			arg, i, afterIndex := formatArgIndex(format, arg, i, len(args))
			valueArgIndexed := afterIndex

			width, hasWidth := int64(0), false
			indexedDynamicWidth := false
			if i < len(format) && format[i] == '*' {
				width = absFormatInt(formatDynamicInt(args, arg))
				hasWidth = true
				indexedDynamicWidth = afterIndex
				valueArgIndexed = false
				if arg < len(args) {
					arg++
				}
				afterIndex = false
				i++
			} else {
				start := i
				width, i = parseFormatInt(format, i)
				hasWidth = i > start
			}

			goodArgNum := !afterIndex || !hasWidth

			precision, hasPrecision := int64(0), false
			if i < len(format) && format[i] == '.' {
				hasPrecision = true
				i++

				if afterIndex {
					goodArgNum = false
				}
				arg, i, afterIndex = formatArgIndex(format, arg, i, len(args))
				if i < len(format) && format[i] == '*' {
					precision = formatDynamicInt(args, arg)
					if precision < 0 {
						hasPrecision = false
						precision = 0
					}
					if arg < len(args) {
						arg++
					}
					afterIndex = false
					i++
				} else {
					start := i
					precision, i = parseFormatInt(format, i)
					if i == start {
						precision = 0
					}
				}
			}

			if !afterIndex {
				arg, i, afterIndex = formatArgIndex(format, arg, i, len(args))
				valueArgIndexed = afterIndex
			}

			if i >= len(format) {
				return 1, i, arg
			}

			verb := format[i]
			i++
			chargeValue := verb != '%' && (goodArgNum || valueArgIndexed)
			if !chargeValue && verb != '%' && indexedDynamicWidth && !afterIndex {
				chargeValue = true
			}

			field := int64(1)
			if chargeValue && arg < len(args) {
				field = estimateFormatValueBytes(args[arg], verb, precision, hasPrecision, hasSpace, hasAlt)
			}
			if hasWidth && width > field {
				field = width
			}

			nextArg := arg
			if chargeValue && arg < len(args) {
				nextArg++
			}
			return field, i, nextArg
		}

		var field int64
		field, i, arg = estimateOne(i, arg)
		out = addFormatEstimate(out, field)
	}

	return addFormatEstimate(core.MeterStringHeaderBytes, out)
}

func estimateFormatValueBytes(v core.Value, verb byte, precision int64, hasPrecision bool, hasSpace bool, hasAlt bool) int64 {
	switch verb {
	case 's':
		n := formatStringBytes(v)
		if _, ok := v.(core.String); !ok && n < minFormatValueStringScalar {
			n = minFormatValueStringScalar
		}
		if hasPrecision && precision < n {
			return precision
		}
		return n
	case 'q':
		return addFormatEstimate(2, formatStringBytes(v)*4)
	case 'x', 'X':
		if _, ok := v.(core.String); ok {
			n := formatStringBytes(v)
			if hasPrecision && precision < n {
				n = precision
			}
			field := addFormatEstimate(0, n)
			if n > 0 {
				field = addFormatEstimate(field, n)
				if hasSpace {
					field = addFormatEstimate(field, n-1)
				}
				if hasAlt {
					if hasSpace {
						field = addFormatEstimate(field, 2*n)
					} else {
						field = addFormatEstimate(field, 2)
					}
				}
				return field
			}
			if hasAlt {
				field = addFormatEstimate(field, 2)
				if hasSpace {
					field = addFormatEstimate(field, 1)
				}
			}
			return field
		}
		n := int64(64)
		if hasPrecision && precision > n {
			return precision
		}
		return n
	case 'd', 'b', 'o', 'O', 'U':
		n := int64(64)
		if hasPrecision && precision > n {
			return precision
		}
		return n
	case 'f', 'F', 'g':
		n := int64(64)
		if hasPrecision {
			n = addFormatEstimate(n, precision)
		} else {
			n = maxDefaultFloatFormatBytes
		}
		return n
	case 'e', 'E':
		n := int64(64)
		if hasPrecision {
			n = addFormatEstimate(n, precision)
		}
		return n
	case 't':
		return 5
	case 'c':
		return 4
	case 'T':
		return 128
	default:
		return core.ValueDeepBytes(v)
	}
}

func formatArgIndex(format string, arg int, i int, numArgs int) (int, int, bool) {
	index, consumed, ok := parseFormatArgIndex(format[i:])
	if !ok {
		return arg, i + consumed, false
	}
	if index < 0 || index >= int64(numArgs) {
		return arg, i + consumed, true
	}
	return int(index), i + consumed, true
}

func parseFormatArgIndex(format string) (int64, int, bool) {
	if len(format) == 0 || format[0] != '[' {
		return 0, 0, false
	}
	if len(format) < 3 {
		return 0, 1, false
	}

	var index int64
	i := 1
	for i < len(format) && isFormatDigit(format[i]) {
		index = index*10 + int64(format[i]-'0')
		i++
	}
	if i == 1 || i >= len(format) || format[i] != ']' {
		return 0, 1, false
	}

	if index == 0 {
		return -1, i + 1, true
	}
	return index - 1, i + 1, true
}

func parseFormatInt(format string, i int) (int64, int) {
	var n int64
	for i < len(format) && isFormatDigit(format[i]) {
		d := int64(format[i] - '0')
		if n > (maxFormatEstimate-d)/10 {
			n = maxFormatEstimate
		} else {
			n = n*10 + d
		}
		i++
	}
	return n, i
}

func isFormatDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

func formatDynamicInt(args []core.Value, i int) int64 {
	if i >= len(args) {
		return 0
	}
	if v, ok := args[i].(core.Int); ok {
		return v.V
	}
	return 0
}

func absFormatInt(n int64) int64 {
	if n >= 0 {
		return n
	}
	if n == -maxFormatEstimate-1 {
		return maxFormatEstimate
	}
	return -n
}

func formatStringBytes(v core.Value) int64 {
	if s, ok := v.(core.String); ok {
		return int64(len(s.V))
	}
	return core.ValueDeepBytes(v)
}

func addFormatEstimate(a, b int64) int64 {
	if b <= 0 {
		return a
	}
	if a > maxFormatEstimate-b {
		return maxFormatEstimate
	}
	return a + b
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
