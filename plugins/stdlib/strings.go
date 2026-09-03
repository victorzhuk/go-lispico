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
			budget := core.NewBuiltinWorkBudget(ctx)
			var buf strings.Builder
			for _, arg := range args {
				if err := budget.Step(); err != nil {
					return finishBuiltin(budget, nil, err)
				}
				buf.WriteString(toString(arg))
			}
			out := buf.String()
			if err := chargeFreshString(ctx, len(out)); err != nil {
				return finishBuiltin(budget, nil, err)
			}
			return finishBuiltin(budget, core.String{V: out}, nil)
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("format", core.GoFunc{
		Name: "format",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) < 1 {
				return nil, arityErrorf("format: requires at least 1 argument")
			}

			fmtStr, ok := args[0].(core.String)
			if !ok {
				return nil, typeErrorf("format: first argument must be string")
			}

			if fmtStr.V == "" {
				return core.String{V: ""}, nil
			}
			estimate := estimateFormatAllocBytes(fmtStr.V, args[1:])
			if err := core.ChargeEvalAllocBytes(ctx, estimate); err != nil {
				return nil, err
			}

			fmtArgs := make([]any, len(args)-1)
			for i, arg := range args[1:] {
				fmtArgs[i] = toAny(arg)
			}

			out := fmt.Sprintf(fmtStr.V, fmtArgs...)
			// The estimate counts one byte per source byte, while %q renders a
			// byte that is not valid UTF-8 as four. Charging the shortfall
			// makes the total max(estimate, shallow): the pre-charge still
			// guards Sprintf, the finished string is never billed twice, and a
			// render that outran its estimate is never billed short.
			if err := chargeFormatShortfall(ctx, len(out), estimate); err != nil {
				return nil, err
			}
			return core.String{V: out}, nil
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("string/join", core.GoFunc{
		Name: "string/join",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 2 {
				return nil, arityErrorf("string/join: requires 2 arguments")
			}

			sep, ok := args[0].(core.String)
			if !ok {
				return nil, typeErrorf("string/join: separator must be string")
			}
			coll := args[1]

			items, ok := seqInput(coll)
			if !ok {
				return nil, typeErrorf("string/join: expected collection, got %T", coll)
			}

			budget := core.NewBuiltinWorkBudget(ctx)
			parts := make([]string, 0, len(items))
			var outBytes int64
			for _, item := range items {
				if err := budget.Step(); err != nil {
					return finishBuiltin(budget, nil, err)
				}
				part := toString(item)
				if len(parts) > 0 {
					outBytes = addFormatEstimate(outBytes, int64(len(sep.V)))
				}
				outBytes = addFormatEstimate(outBytes, int64(len(part)))
				parts = append(parts, part)
			}

			joined, err := joinPrecharged(ctx, parts, sep.V, outBytes)
			return finishBuiltin(budget, core.String{V: joined}, err)
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("string/split", core.GoFunc{
		Name: "string/split",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 2 {
				return nil, arityErrorf("string/split: requires 2 arguments")
			}

			s, ok1 := args[0].(core.String)
			sep, ok2 := args[1].(core.String)

			if !ok1 || !ok2 {
				return nil, typeErrorf("string/split: requires string arguments")
			}

			parts := strings.Split(s.V, sep.V)
			budget := core.NewBuiltinWorkBudget(ctx)
			items := make([]core.Value, len(parts))
			for i, p := range parts {
				if err := budget.Step(); err != nil {
					return finishBuiltin(budget, nil, err)
				}
				items[i] = core.String{V: p}
			}

			if err := chargeFreshContainer(ctx, splitResultBytes(len(items))); err != nil {
				return finishBuiltin(budget, nil, err)
			}
			return finishBuiltin(budget, core.NewList(items), nil)
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("string/trim", core.GoFunc{
		Name: "string/trim",
		Fn:   unaryStringFunc("string/trim", strings.TrimSpace),
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("string/upper", core.GoFunc{
		Name: "string/upper",
		Fn:   unaryStringFunc("string/upper", strings.ToUpper),
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("string/lower", core.GoFunc{
		Name: "string/lower",
		Fn:   unaryStringFunc("string/lower", strings.ToLower),
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("string/replace", core.GoFunc{
		Name: "string/replace",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 3 {
				return nil, arityErrorf("string/replace: requires 3 arguments")
			}

			s, ok1 := args[0].(core.String)
			old, ok2 := args[1].(core.String)
			new, ok3 := args[2].(core.String)

			if !ok1 || !ok2 || !ok3 {
				return nil, typeErrorf("string/replace: requires string arguments")
			}

			out, err := replacePrecharged(ctx, s.V, old.V, new.V)
			if err != nil {
				return nil, err
			}
			return core.String{V: out}, nil
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("string/contains?", core.GoFunc{
		Name: "string/contains?",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 2 {
				return nil, arityErrorf("string/contains?: requires 2 arguments")
			}

			s, ok1 := args[0].(core.String)
			substr, ok2 := args[1].(core.String)

			if !ok1 || !ok2 {
				return nil, typeErrorf("string/contains?: requires string arguments")
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
				return nil, arityErrorf("string/starts-with?: requires 2 arguments")
			}

			s, ok1 := args[0].(core.String)
			prefix, ok2 := args[1].(core.String)

			if !ok1 || !ok2 {
				return nil, typeErrorf("string/starts-with?: requires string arguments")
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
				return nil, arityErrorf("string/ends-with?: requires 2 arguments")
			}

			s, ok1 := args[0].(core.String)
			suffix, ok2 := args[1].(core.String)

			if !ok1 || !ok2 {
				return nil, typeErrorf("string/ends-with?: requires string arguments")
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
				return nil, arityErrorf("string/length: requires 1 argument")
			}

			s, ok := args[0].(core.String)
			if !ok {
				return nil, typeErrorf("string/length: requires string argument")
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
				return nil, arityErrorf("string/lines: requires 1 argument")
			}

			s, ok := args[0].(core.String)
			if !ok {
				return nil, typeErrorf("string/lines: requires string argument")
			}

			lines := strings.Split(s.V, "\n")
			budget := core.NewBuiltinWorkBudget(ctx)
			items := make([]core.Value, len(lines))
			for i, line := range lines {
				if err := budget.Step(); err != nil {
					return finishBuiltin(budget, nil, err)
				}
				items[i] = core.String{V: line}
			}

			if err := chargeFreshContainer(ctx, splitResultBytes(len(items))); err != nil {
				return finishBuiltin(budget, nil, err)
			}
			return finishBuiltin(budget, core.NewList(items), nil)
		},
	}, false); err != nil {
		return err
	}

	if err := env.RegisterValue("string->int", core.GoFunc{
		Name: "string->int",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 1 {
				return nil, arityErrorf("string->int: requires 1 argument")
			}

			s, ok := args[0].(core.String)
			if !ok {
				return nil, typeErrorf("string->int: requires string argument")
			}

			i, err := strconv.ParseInt(s.V, 10, 64)
			if err != nil {
				if cerr := core.ChargeEvalAllocBytes(ctx, parseFailureMessageBytes(len(s.V))); cerr != nil {
					return nil, cerr
				}
				return nil, wrapCause("string->int", err)
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
				return nil, arityErrorf("string->float: requires 1 argument")
			}

			s, ok := args[0].(core.String)
			if !ok {
				return nil, typeErrorf("string->float: requires string argument")
			}

			f, err := strconv.ParseFloat(s.V, 64)
			if err != nil {
				if cerr := core.ChargeEvalAllocBytes(ctx, parseFailureMessageBytes(len(s.V))); cerr != nil {
					return nil, cerr
				}
				return nil, wrapCause("string->float", err)
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
	// maxFormatParseNum is fmt's own ceiling on a literal width or precision,
	// tested before each digit rather than after, so a literal is honoured up
	// to maxFormatParseNum*10+9 and refused past it.
	maxFormatParseNum = int64(1_000_000)
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
				var honoured bool
				width, i, honoured = parseFormatInt(format, i)
				hasWidth = honoured && i > start
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
					var honoured bool
					precision, i, honoured = parseFormatInt(format, i)
					if i == start {
						precision = 0
					}
					if !honoured {
						hasPrecision = false
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

// parseFormatInt reads a literal width or precision the way fmt's parsenum
// does: the running value is tested against maxFormatParseNum before each
// digit, so the largest literal fmt honours is 10000009. Past that fmt refuses
// the whole verb and renders %!(NOVERB) instead of the field, which is why a
// refused literal reports ok false: the directive cannot expand, so treating
// its digits as a field width estimates a few dozen rendered bytes at the
// int64 ceiling. The digits are still consumed so the caller lands on the verb.
func parseFormatInt(format string, i int) (int64, int, bool) {
	var n int64
	refused := false
	for i < len(format) && isFormatDigit(format[i]) {
		if n > maxFormatParseNum {
			refused = true
		}
		if !refused {
			n = n*10 + int64(format[i]-'0')
		}
		i++
	}
	if refused {
		return 0, i, false
	}
	return n, i, true
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

// mulFormatEstimate multiplies two estimate terms under addFormatEstimate's
// saturation, so a product that cannot fit an int64 pins at the ceiling instead
// of wrapping into a small charge.
func mulFormatEstimate(a, b int64) int64 {
	if a <= 0 || b <= 0 {
		return 0
	}
	if a > maxFormatEstimate/b {
		return maxFormatEstimate
	}
	return a * b
}

func unaryStringFunc(name string, fn func(string) string) func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
	return func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
		if len(args) != 1 {
			return nil, arityErrorf("%s: requires 1 argument", name)
		}

		s, ok := args[0].(core.String)
		if !ok {
			return nil, typeErrorf("%s: expected string, got %T", name, args[0])
		}

		out := fn(s.V)
		if err := chargeStringResult(ctx, s.V, out); err != nil {
			return nil, err
		}
		return core.String{V: out}, nil
	}
}

// joinPrecharged charges what string/join is about to build before
// strings.Join writes it. The separator lands between every part, so the output
// is a product of the part count and the separator's length rather than a
// maximum over the operands; outBytes is that size, summed by the caller's
// budgeted pass over the parts.
func joinPrecharged(ctx context.Context, parts []string, sep string, outBytes int64) (string, error) {
	if err := chargeSizedString(ctx, addFormatEstimate(core.MeterStringHeaderBytes, outBytes)); err != nil {
		return "", err
	}
	return strings.Join(parts, sep), nil
}

// replacePrecharged charges what string/replace is about to build before
// strings.ReplaceAll writes it.
func replacePrecharged(ctx context.Context, s, old, new string) (string, error) {
	if err := chargeSizedString(ctx, replaceOutputBytes(s, old, new)); err != nil {
		return "", err
	}
	return strings.ReplaceAll(s, old, new), nil
}

// replaceOutputBytes sizes string/replace's output from its operands. An empty
// old inserts the replacement before every rune and once more at the end, so
// the output is a product of the subject's rune count and the replacement's
// length, not a maximum over the two. It is 0 when the primitive hands the
// subject straight back - old equal to new, or no occurrence to rewrite -
// because that result owns no new bytes.
func replaceOutputBytes(s, old, new string) int64 {
	count := int64(strings.Count(s, old))
	if old == new || count == 0 {
		return 0
	}
	outLen := int64(len(s))
	if grow := int64(len(new) - len(old)); grow > 0 {
		outLen = addFormatEstimate(outLen, mulFormatEstimate(count, grow))
	} else {
		outLen += count * grow
	}
	return addFormatEstimate(core.MeterStringHeaderBytes, outLen)
}

// chargeStringResult bills a Go string primitive's output. A primitive that
// found nothing to change hands back the subject itself, so the result owns no
// new bytes; identical content means an identical charge either way, so the
// content comparison decides ownership without reaching for pointer identity.
func chargeStringResult(ctx context.Context, subject, out string) error {
	if out == subject {
		return chargeBorrowedResult(ctx)
	}
	return chargeFreshString(ctx, len(out))
}

// splitResultBytes is what a split owes the ledger: the list plus one
// core.String header per part. The part contents alias the subject's backing
// array, which the ledger already holds, so billing them would charge the
// subject twice.
func splitResultBytes(parts int) int64 {
	return core.ListShallowBytes(parts) + int64(parts)*core.MeterStringHeaderBytes
}

// parseQuoteFactor is what strconv.Quote expands a byte that is not valid
// UTF-8 into: \xNN, four characters per source byte.
const parseQuoteFactor = 4

// parseFailureMessageBytes bounds what a failed parse materializes.
// strconv.NumError.Error quotes the whole subject and wrapCause renders that
// text again through %s: %v, so the escaped form is built twice.
func parseFailureMessageBytes(subjectLen int) int64 {
	return 2 * core.StringShallowBytes(subjectLen*parseQuoteFactor)
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
