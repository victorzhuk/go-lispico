package data

import (
	"context"
	"encoding/json"
	"fmt"

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
	env.Set("json/encode", core.GoFunc{Name: "json/encode", Fn: p.encode})
	env.Set("json/decode", core.GoFunc{Name: "json/decode", Fn: p.decode})
	env.Set("json/pretty-encode", core.GoFunc{Name: "json/pretty-encode", Fn: p.prettyEncode})
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
	b, err := json.Marshal(goVal)
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
	var raw any
	if err := json.Unmarshal([]byte(s.V), &raw); err != nil {
		return nil, fmt.Errorf("json/decode: %w", err)
	}
	return fromJSONValue(raw)
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
	b, err := json.MarshalIndent(goVal, "", "  ")
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
	case float64:
		if x == float64(int64(x)) && x >= -9007199254740991 && x <= 9007199254740991 {
			return core.Int{V: int64(x)}, nil
		}
		return core.Float{V: x}, nil
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
		return core.Vector{Items: items}, nil
	default:
		return nil, fmt.Errorf("json/decode: unsupported JSON type %T", v)
	}
}
