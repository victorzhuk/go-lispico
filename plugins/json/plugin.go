package json

import (
	"context"
	stdjson "encoding/json"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/victorzhuk/go-lispico/core"
)

type Plugin struct{}

func New() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "json" }

func (p *Plugin) Metadata() core.PluginMeta {
	return core.PluginMeta{
		Version:     "1.0.0",
		Description: "JSON encoding/decoding for go-lispico",
		Author:      "go-lispico team",
	}
}

func (p *Plugin) Init(env *core.Env) error {
	for _, fn := range []core.GoFunc{
		{Name: "json/encode", Fn: p.encode},
		{Name: "json/decode", Fn: p.decode},
		{Name: "json/pretty-encode", Fn: p.prettyEncode},
	} {
		if err := env.Set(fn.Name, fn); err != nil {
			return err
		}
	}
	return nil
}

func (p *Plugin) encode(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("json/encode: requires 1 argument, got %d", len(args))
	}
	goVal, err := core.ToGoValue(args[0])
	if err != nil {
		return nil, fmt.Errorf("json/encode: %w", err)
	}
	b, err := stdjson.Marshal(goVal)
	if err != nil {
		return nil, fmt.Errorf("json/encode: %w", err)
	}
	return core.String{V: string(b)}, nil
}

func (p *Plugin) decode(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("json/decode: requires 1 argument, got %d", len(args))
	}
	s, ok := args[0].(core.String)
	if !ok {
		return nil, fmt.Errorf("json/decode: requires string argument, got %T", args[0])
	}
	dec := stdjson.NewDecoder(strings.NewReader(s.V))
	dec.UseNumber()
	var raw any
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("json/decode: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("json/decode: invalid character after top-level value")
	}
	res, err := fromJSONValue(raw)
	if err != nil {
		return nil, err
	}
	if err := core.CheckConstructionDepthContextEnv(ctx, res, env); err != nil {
		return nil, err
	}
	deep, err := core.ValueDeepBytesContext(ctx, res)
	if err != nil {
		return nil, err
	}
	if err := core.ChargeEvalAllocBytes(ctx, deep); err != nil {
		return nil, err
	}
	return res, nil
}

func (p *Plugin) prettyEncode(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("json/pretty-encode: requires 1 argument, got %d", len(args))
	}
	goVal, err := core.ToGoValue(args[0])
	if err != nil {
		return nil, fmt.Errorf("json/pretty-encode: %w", err)
	}
	b, err := stdjson.MarshalIndent(goVal, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("json/pretty-encode: %w", err)
	}
	return core.String{V: string(b)}, nil
}

func fromJSONValue(v any) (core.Value, error) {
	switch x := v.(type) {
	case nil:
		return core.Nil{}, nil
	case bool:
		return core.Bool{V: x}, nil
	case stdjson.Number:
		if n, ok := exactInt(x.String()); ok {
			return core.BoxInt(n), nil
		}
		f, err := x.Float64()
		if err != nil {
			return nil, fmt.Errorf("json/decode: %w", err)
		}
		if math.IsInf(f, 0) || math.IsNaN(f) {
			return nil, fmt.Errorf("json/decode: number %s is out of float64 range", x.String())
		}
		return core.Float{V: f}, nil
	case string:
		return core.String{V: x}, nil
	case map[string]any:
		m := core.NewHashMap()
		for k, val := range x {
			lv, ferr := fromJSONValue(val)
			if ferr != nil {
				return nil, ferr
			}
			if err := m.Set(core.Keyword{V: k}, lv); err != nil {
				return nil, fmt.Errorf("json/decode: %w", err)
			}
		}
		return m, nil
	case []any:
		items := make([]core.Value, len(x))
		for i, item := range x {
			lv, err := fromJSONValue(item)
			if err != nil {
				return nil, err
			}
			items[i] = lv
		}
		return core.NewVector(items), nil
	default:
		return nil, fmt.Errorf("json/decode: unsupported JSON type %T", v)
	}
}

// exactInt classifies a validated JSON number lexeme: it returns the value
// and true when the exact decimal is an integer in the signed int64 range.
// Digit count gates accumulation (max 19) and the exponent saturates at a
// token-length bound, so work and storage stay linear in the lexeme and no
// power of ten expands. Integration with the float fallback is the caller's:
// any false result falls through to Number.Float64.
func exactInt(s string) (int64, bool) {
	neg := false
	if s[0] == '-' {
		neg = true
		s = s[1:]
	}
	fracOff, expOff := -1, -1
	for i := range len(s) {
		switch s[i] {
		case '.':
			if fracOff < 0 {
				fracOff = i
			}
		case 'e', 'E':
			expOff = i
		}
	}
	mantEnd := len(s)
	if expOff >= 0 {
		mantEnd = expOff
	}
	fracDigits := 0
	digits := s[:mantEnd]
	if fracOff >= 0 {
		digits = s[:fracOff] + s[fracOff+1:mantEnd]
		fracDigits = mantEnd - fracOff - 1
	}
	exp := 0
	if expOff >= 0 {
		es := s[expOff+1:]
		esign := 1
		if es[0] == '-' || es[0] == '+' {
			if es[0] == '-' {
				esign = -1
			}
			es = es[1:]
		}
		// Saturate at len(s)+20 — a bound from token length and the fixed
		// int64 domain: the Int path needs scale at most 19 with fracDigits
		// below len(s), so an exponent at or past the bound forces the
		// float-fallback outcome however many digits it carries.
		bound := len(s) + 20
		for i := range len(es) {
			if exp >= bound {
				break
			}
			exp = exp*10 + int(es[i]-'0')
		}
		exp *= esign
	}
	for len(digits) > 0 && digits[0] == '0' {
		digits = digits[1:]
	}
	if len(digits) == 0 {
		return 0, true
	}
	scale := exp - fracDigits
	for scale < 0 && len(digits) > 0 && digits[len(digits)-1] == '0' {
		digits = digits[:len(digits)-1]
		scale++
	}
	if scale < 0 || len(digits)+scale > 19 {
		return 0, false
	}
	var mag uint64
	for i := range len(digits) {
		mag = mag*10 + uint64(digits[i]-'0')
	}
	for range scale {
		mag *= 10
	}
	const negLimit = uint64(-(math.MinInt64 + 1)) + 1
	if neg {
		if mag > negLimit {
			return 0, false
		}
		if mag == negLimit {
			return math.MinInt64, true
		}
		return -int64(mag), true
	}
	if mag > math.MaxInt64 {
		return 0, false
	}
	return int64(mag), true
}
