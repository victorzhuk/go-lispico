# Design Document: Data Plugin

**Change ID:** 006d-data-plugin  
**Status:** Design Complete  
**Created:** 2026-02-24  
**Author:** AI Assistant

---

## 1. Plugin Structure

```go
package data

import "github.com/victorzhuk/go-lispico/core"

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
```

---

## 2. JSON Encode Implementation

```go
package data

import (
    "context"
    "encoding/json"
    "fmt"

    "github.com/victorzhuk/go-lispico/core"
)

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
```

---

## 3. JSON Decode Implementation

```go
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
        var err error
        for k, val := range x {
            lv, ferr := fromJSONValue(val)
            if ferr != nil {
                return nil, ferr
            }
            m, err = m.Assoc(core.Keyword{V: k}, lv)
            if err != nil {
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
```

---

## 4. File Organization

```
plugins/data/
├── plugin.go       # Plugin struct, New, Name, Metadata, Init
├── json.go         # encode, prettyEncode, decode, fromJSONValue
└── data_test.go    # Table-driven tests for all functions
```

---

## 5. Testing Strategy

### Test Setup

```go
func setupEnv(t *testing.T) *core.Env {
    t.Helper()
    env := core.NewEnv(nil)
    sp := stdlib.New()
    if err := sp.Init(env); err != nil {
        t.Fatalf("stdlib init: %v", err)
    }
    dp := New()
    if err := dp.Init(env); err != nil {
        t.Fatalf("data plugin init: %v", err)
    }
    return env
}
```

### Test Cases

| Test | Input | Expected |
|------|-------|----------|
| encode nil | `(json/encode nil)` | `"null"` |
| encode bool | `(json/encode true)` | `"true"` |
| encode int | `(json/encode 42)` | `"42"` |
| encode float | `(json/encode 3.14)` | `"3.14"` |
| encode string | `(json/encode "hello")` | `"\"hello\""` |
| encode vector | `(json/encode [1 2 3])` | `"[1,2,3]"` |
| encode list | `(json/encode '(1 2 3))` | `"[1,2,3]"` |
| encode map | `(json/encode {:a 1})` | `"{\"a\":1}"` |
| encode nested | `(json/encode {:a [1 {:b 2}]})` | contains `"a"`, `"b"` |
| decode null | `(json/decode "null")` | `nil` |
| decode bool | `(json/decode "true")` | `true` |
| decode int | `(json/decode "42")` | `42` (Int, not Float) |
| decode float | `(json/decode "3.14")` | `3.14` |
| decode string | `(json/decode "\"hello\"")` | `"hello"` |
| decode array | `(json/decode "[1,2,3]")` | `[1 2 3]` |
| decode object | `(json/decode "{\"a\":1}")` | `{:a 1}` |
| decode nested | `(json/decode "{\"a\":{\"b\":42}}")` | nested HashMap |
| round-trip map | encode then decode | structure preserved |
| round-trip vector | encode then decode | structure preserved |
| decode + get-in | `(get-in (json/decode ...) [:a :b])` | deep access works |
| decode + map? | `(map? (json/decode "{\"a\":1}"))` | `true` |
| decode + vector? | `(vector? (json/decode "[1]"))` | `true` |
| pretty-encode | `(json/pretty-encode {:a 1})` | contains newline + indent |
| error: bad JSON | `(json/decode "not json")` | error |
| error: wrong type | `(json/decode 42)` | error: requires string |
| error: unencodable | `(json/encode (fn [x] x))` | error: unsupported type |
| arity: encode 0 | `(json/encode)` | error |
| arity: decode 2 | `(json/decode "a" "b")` | error |

---

**Next Step:** Implement tasks from tasks.md.
