# Design Document: Standard Library Plugin

**Change ID:** 002-stdlib-plugin  
**Status:** Design  
**Created:** 2026-02-23  
**Author:** AI Assistant

---

## 1. Plugin Structure

```go
package stdlib

import (
    "context"
    "fmt"
    "math"
    "sort"
    "strconv"
    "strings"

    "github.com/victorzhuk/go-lispico/core"
)

type Plugin struct{}

func New() *Plugin {
    return &Plugin{}
}

func (p *Plugin) Name() string {
    return ""
} // stdlib has no namespace prefix

func (p *Plugin) Metadata() core.PluginMeta {
    return core.PluginMeta{
        Version:     "1.0.0",
        Description: "Standard library for go-lispico",
        Author:      "go-lispico team",
    }
}

func (p *Plugin) Init(env *core.Env) error {
    // Register all stdlib functions
    p.registerArithmetic(env)
    p.registerStrings(env)
    p.registerCollections(env)
    p.registerHigherOrder(env)
    p.registerControl(env)
    p.registerTypes(env)
    
    // Load bootstrap.lisp for macros
    return p.loadBootstrap(env)
}
```

---

## 2. Arithmetic Functions

```go
func (p *Plugin) registerArithmetic(env *core.Env) {
    // +
    env.Set("+", core.GoFunc{
        Name: "+",
        Fn: func(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
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
    
    // - (binary or n-ary) — uses integer arithmetic to preserve int64 precision
    env.Set("-", core.GoFunc{
        Name: "-",
        Fn: func(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
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

            // Negate if unary
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
    
    // * (similar to +)
    env.Set("*", core.GoFunc{
        Name: "*",
        Fn: func(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
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
    
    // /
    env.Set("/", core.GoFunc{
        Name: "/",
        Fn: func(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
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
    
    // mod
    env.Set("mod", core.GoFunc{
        Name: "mod",
        Fn: func(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
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
    
    // pow
    env.Set("pow", core.GoFunc{
        Name: "pow",
        Fn: func(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
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
    
    // abs, sqrt, floor, ceil
    env.Set("abs", core.GoFunc{
        Name: "abs",
        Fn: unaryMathFunc(math.Abs),
    })
    
    env.Set("sqrt", core.GoFunc{
        Name: "sqrt",
        Fn: unaryMathFunc(math.Sqrt),
    })
    
    env.Set("floor", core.GoFunc{
        Name: "floor",
        Fn: unaryMathFunc(math.Floor),
    })
    
    env.Set("ceil", core.GoFunc{
        Name: "ceil",
        Fn: unaryMathFunc(math.Ceil),
    })
    
    // Predicates
    env.Set("zero?", core.GoFunc{
        Name: "zero?",
        Fn: func(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
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
        Fn: func(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
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
        Fn: func(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
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
    
    // max, min
    env.Set("max", core.GoFunc{
        Name: "max",
        Fn: minMaxFunc(true),
    })
    
    env.Set("min", core.GoFunc{
        Name: "min",
        Fn: minMaxFunc(false),
    })
}

// Helper for unary math functions
func unaryMathFunc(fn func(float64) float64) func(context.Context, *core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
    return func(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
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

// Helper for min/max
func minMaxFunc(isMax bool) func(context.Context, *core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
    return func(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
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
                if x > result { result = x }
            } else {
                if x < result { result = x }
            }
        }
        
        if hasFloat {
            return core.Float{V: result}, nil
        }
        return core.Int{V: int64(result)}, nil
    }
}
```

---

## 3. String Functions

```go
func (p *Plugin) registerStrings(env *core.Env) {
    // str - concatenation
    env.Set("str", core.GoFunc{
        Name: "str",
        Fn: func(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
            var buf strings.Builder
            for _, arg := range args {
                buf.WriteString(toString(arg))
            }
            return core.String{V: buf.String()}, nil
        },
    })
    
    // format
    env.Set("format", core.GoFunc{
        Name: "format",
        Fn: func(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
            if len(args) < 1 {
                return nil, fmt.Errorf("format: requires at least 1 argument")
            }
            
            fmtStr, ok := args[0].(core.String)
            if !ok {
                return nil, fmt.Errorf("format: first argument must be string")
            }
            
            // Convert args to any
            fmtArgs := make([]any, len(args)-1)
            for i, arg := range args[1:] {
                fmtArgs[i] = toAny(arg)
            }
            
            return core.String{V: fmt.Sprintf(fmtStr.V, fmtArgs...)}, nil
        },
    })
    
    // string/join
    env.Set("string/join", core.GoFunc{
        Name: "string/join",
        Fn: func(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
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
    
    // string/split
    env.Set("string/split", core.GoFunc{
        Name: "string/split",
        Fn: func(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
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
    
    // string/trim
    env.Set("string/trim", core.GoFunc{
        Name: "string/trim",
        Fn: unaryStringFunc(strings.TrimSpace),
    })
    
    // string/upper
    env.Set("string/upper", core.GoFunc{
        Name: "string/upper",
        Fn: unaryStringFunc(strings.ToUpper),
    })
    
    // string/lower
    env.Set("string/lower", core.GoFunc{
        Name: "string/lower",
        Fn: unaryStringFunc(strings.ToLower),
    })
    
    // string/replace
    env.Set("string/replace", core.GoFunc{
        Name: "string/replace",
        Fn: func(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
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
    
    // string/contains?
    env.Set("string/contains?", core.GoFunc{
        Name: "string/contains?",
        Fn: func(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
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
    
    // string/starts-with?
    env.Set("string/starts-with?", core.GoFunc{
        Name: "string/starts-with?",
        Fn: func(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
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
    
    // string/ends-with?
    env.Set("string/ends-with?", core.GoFunc{
        Name: "string/ends-with?",
        Fn: func(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
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
    
    // string/length
    env.Set("string/length", core.GoFunc{
        Name: "string/length",
        Fn: func(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
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
    
    // string/lines
    env.Set("string/lines", core.GoFunc{
        Name: "string/lines",
        Fn: func(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
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
    
    // string->int
    env.Set("string->int", core.GoFunc{
        Name: "string->int",
        Fn: func(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
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
    
    // string->float
    env.Set("string->float", core.GoFunc{
        Name: "string->float",
        Fn: func(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
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

func unaryStringFunc(fn func(string) string) func(context.Context, *core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
    return func(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
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
	if _, ok := v.(core.Nil); ok { return false }
	if b, ok := v.(core.Bool); ok { return b.V }
	return true
}
```

---

## 4. Collection Functions

```go
func (p *Plugin) registerCollections(env *core.Env) {
    // Constructors
    env.Set("list", core.GoFunc{
        Name: "list",
        Fn: func(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
            return core.List{Items: args}, nil
        },
    })
    
    env.Set("vector", core.GoFunc{
        Name: "vector",
        Fn: func(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
            return core.Vector{Items: args}, nil
        },
    })
    
    env.Set("hash-map", core.GoFunc{
        Name: "hash-map",
        Fn: func(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
            if len(args)%2 != 0 {
                return nil, fmt.Errorf("hash-map: requires even number of arguments")
            }
            
            m := core.NewHashMap()
            for i := 0; i < len(args); i += 2 {
                m.M[args[i]] = args[i+1]
            }
            return m, nil
        },
    })
    
    // Access
    env.Set("first", core.GoFunc{
        Name: "first",
        Fn: func(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
            if len(args) != 1 {
                return nil, fmt.Errorf("first: requires 1 argument")
            }
            
            switch c := args[0].(type) {
            case core.List:
                if len(c.Items) == 0 { return core.Nil{}, nil }
                return c.Items[0], nil
            case core.Vector:
                if len(c.Items) == 0 { return core.Nil{}, nil }
                return c.Items[0], nil
            case core.Nil:
                return core.Nil{}, nil
            default:
                return nil, fmt.Errorf("first: expected collection, got %T", args[0])
            }
        },
    })
    
    env.Set("rest", core.GoFunc{
        Name: "rest",
        Fn: func(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
            if len(args) != 1 {
                return nil, fmt.Errorf("rest: requires 1 argument")
            }
            
            switch c := args[0].(type) {
            case core.List:
                if len(c.Items) <= 1 { return core.List{Items: []core.Value{}}, nil }
                return core.List{Items: c.Items[1:]}, nil
            case core.Vector:
                if len(c.Items) <= 1 { return core.List{Items: []core.Value{}}, nil }
                return core.List{Items: c.Items[1:]}, nil
            case core.Nil:
                return core.List{Items: []core.Value{}}, nil
            default:
                return nil, fmt.Errorf("rest: expected collection, got %T", args[0])
            }
        },
    })
    
    env.Set("last", core.GoFunc{
        Name: "last",
        Fn: func(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
            if len(args) != 1 {
                return nil, fmt.Errorf("last: requires 1 argument")
            }
            
            switch c := args[0].(type) {
            case core.List:
                if len(c.Items) == 0 { return core.Nil{}, nil }
                return c.Items[len(c.Items)-1], nil
            case core.Vector:
                if len(c.Items) == 0 { return core.Nil{}, nil }
                return c.Items[len(c.Items)-1], nil
            case core.Nil:
                return core.Nil{}, nil
            default:
                return nil, fmt.Errorf("last: expected collection, got %T", args[0])
            }
        },
    })
    
    env.Set("nth", core.GoFunc{
        Name: "nth",
        Fn: func(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
            if len(args) < 2 || len(args) > 3 {
                return nil, fmt.Errorf("nth: requires 2 or 3 arguments")
            }
            
            idx, ok := args[1].(core.Int)
            if !ok {
                return nil, fmt.Errorf("nth: index must be integer")
            }
            
            var items []core.Value
            switch c := args[0].(type) {
            case core.List:
                items = c.Items
            case core.Vector:
                items = c.Items
            default:
                return nil, fmt.Errorf("nth: expected collection, got %T", args[0])
            }
            
            if idx.V < 0 || int(idx.V) >= len(items) {
                if len(args) == 3 {
                    return args[2], nil // return not-found value
                }
                return nil, fmt.Errorf("nth: index out of bounds")
            }
            
            return items[idx.V], nil
        },
    })
    
    env.Set("count", core.GoFunc{
        Name: "count",
        Fn: func(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
            if len(args) != 1 {
                return nil, fmt.Errorf("count: requires 1 argument")
            }
            
            switch c := args[0].(type) {
            case core.List:
                return core.Int{V: int64(len(c.Items))}, nil
            case core.Vector:
                return core.Int{V: int64(len(c.Items))}, nil
            case *core.HashMap:
                return core.Int{V: int64(len(c.M))}, nil
            case core.String:
                return core.Int{V: int64(len([]rune(c.V)))}, nil
            case core.Nil:
                return core.Int{V: 0}, nil
            default:
                return nil, fmt.Errorf("count: expected collection, got %T", args[0])
            }
        },
    })
    
    // Construction
    env.Set("cons", core.GoFunc{
        Name: "cons",
        Fn: func(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
            if len(args) != 2 {
                return nil, fmt.Errorf("cons: requires 2 arguments")
            }
            
            switch c := args[1].(type) {
            case core.List:
                items := append([]core.Value{args[0]}, c.Items...)
                return core.List{Items: items}, nil
            case core.Vector:
                items := append([]core.Value{args[0]}, c.Items...)
                return core.List{Items: items}, nil
            default:
                return nil, fmt.Errorf("cons: expected collection, got %T", args[1])
            }
        },
    })
    
    env.Set("conj", core.GoFunc{
        Name: "conj",
        Fn: func(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
            if len(args) < 2 {
                return nil, fmt.Errorf("conj: requires at least 2 arguments")
            }
            
            switch c := args[0].(type) {
            case core.List:
                // conj on list adds to front
                items := make([]core.Value, len(args)-1)
                copy(items, args[1:])
                items = append(items, c.Items...)
                return core.List{Items: items}, nil
            case core.Vector:
                // conj on vector adds to end
                items := make([]core.Value, len(c.Items)+len(args)-1)
                copy(items, c.Items)
                copy(items[len(c.Items):], args[1:])
                return core.Vector{Items: items}, nil
            case *core.HashMap:
                if len(args) != 3 {
                    return nil, fmt.Errorf("conj on map requires key and value")
                }
                return c.Assoc(args[1], args[2]), nil
            default:
                return nil, fmt.Errorf("conj: expected collection, got %T", args[0])
            }
        },
    })
    
    // Predicates
    env.Set("empty?", core.GoFunc{
        Name: "empty?",
        Fn: func(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
            if len(args) != 1 {
                return nil, fmt.Errorf("empty?: requires 1 argument")
            }
            
            switch c := args[0].(type) {
            case core.List:
                return core.Bool{V: len(c.Items) == 0}, nil
            case core.Vector:
                return core.Bool{V: len(c.Items) == 0}, nil
            case *core.HashMap:
                return core.Bool{V: len(c.M) == 0}, nil
            case core.Nil:
                return core.Bool{V: true}, nil
            default:
                return core.Bool{V: false}, nil
            }
        },
    })
    
    // Map operations
    env.Set("get", core.GoFunc{
        Name: "get",
        Fn: func(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
            if len(args) < 2 || len(args) > 3 {
                return nil, fmt.Errorf("get: requires 2 or 3 arguments")
            }
            
            m, ok := args[0].(*core.HashMap)
            if !ok {
                return nil, fmt.Errorf("get: expected map, got %T", args[0])
            }
            
            if v, ok := m.M[args[1]]; ok {
                return v, nil
            }
            
            if len(args) == 3 {
                return args[2], nil
            }
            
            return core.Nil{}, nil
        },
    })
    
    env.Set("assoc", core.GoFunc{
        Name: "assoc",
        Fn: func(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
            if len(args) < 3 || len(args)%2 == 0 {
                return nil, fmt.Errorf("assoc: requires odd number of arguments (map + keyvals)")
            }
            
            m, ok := args[0].(*core.HashMap)
            if !ok {
                return nil, fmt.Errorf("assoc: expected map, got %T", args[0])
            }
            
            result := m
            for i := 1; i < len(args); i += 2 {
                result = result.Assoc(args[i], args[i+1])
            }
            
            return result, nil
        },
    })
    
    env.Set("keys", core.GoFunc{
        Name: "keys",
        Fn: func(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
            if len(args) != 1 {
                return nil, fmt.Errorf("keys: requires 1 argument")
            }
            
            m, ok := args[0].(*core.HashMap)
            if !ok {
                return nil, fmt.Errorf("keys: expected map, got %T", args[0])
            }
            
            items := make([]core.Value, 0, len(m.M))
            for k := range m.M {
                items = append(items, k)
            }
            
            return core.List{Items: items}, nil
        },
    })
    
    env.Set("vals", core.GoFunc{
        Name: "vals",
        Fn: func(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
            if len(args) != 1 {
                return nil, fmt.Errorf("vals: requires 1 argument")
            }
            
            m, ok := args[0].(*core.HashMap)
            if !ok {
                return nil, fmt.Errorf("vals: expected map, got %T", args[0])
            }
            
            items := make([]core.Value, 0, len(m.M))
            for _, v := range m.M {
                items = append(items, v)
            }
            
            return core.List{Items: items}, nil
        },
    })
}
```

---

## 5. Higher-Order Functions

```go
func (p *Plugin) registerHigherOrder(env *core.Env) {
    // map - applies fn to each element, returning a list of results
    env.Set("map", core.GoFunc{
        Name: "map",
        Fn: func(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
            if len(args) != 2 {
                return nil, fmt.Errorf("map: requires 2 arguments")
            }

            var items []core.Value
            switch c := args[1].(type) {
            case core.List:
                items = c.Items
            case core.Vector:
                items = c.Items
            default:
                return nil, fmt.Errorf("map: second argument must be collection")
            }

            results := make([]core.Value, len(items))
            for i, item := range items {
                r, err := eval.Apply(ctx, args[0], []core.Value{item}, env)
                if err != nil { return nil, err }
                results[i] = r
            }

            return core.List{Items: results}, nil
        },
    })
    
    // filter
    env.Set("filter", core.GoFunc{
        Name: "filter",
        Fn: func(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
            if len(args) != 2 {
                return nil, fmt.Errorf("filter: requires 2 arguments")
            }

            var items []core.Value
            switch c := args[1].(type) {
            case core.List:
                items = c.Items
            case core.Vector:
                items = c.Items
            default:
                return nil, fmt.Errorf("filter: second argument must be collection")
            }

            var results []core.Value
            for _, item := range items {
                r, err := eval.Apply(ctx, args[0], []core.Value{item}, env)
                if err != nil { return nil, err }
                if isTruthy(r) {
                    results = append(results, item)
                }
            }

            return core.List{Items: results}, nil
        },
    })
    
    // reduce
    env.Set("reduce", core.GoFunc{
        Name: "reduce",
        Fn: func(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
            if len(args) < 2 || len(args) > 3 {
                return nil, fmt.Errorf("reduce: requires 2 or 3 arguments")
            }

            var items []core.Value
            collIdx := 1
            if len(args) == 3 {
                collIdx = 2
            }

            switch c := args[collIdx].(type) {
            case core.List:
                items = c.Items
            case core.Vector:
                items = c.Items
            default:
                return nil, fmt.Errorf("reduce: last argument must be collection")
            }

            var acc core.Value
            startIdx := 0
            if len(args) == 3 {
                acc = args[1]
            } else if len(items) == 0 {
                return core.Nil{}, nil
            } else {
                acc = items[0]
                startIdx = 1
            }

            for _, item := range items[startIdx:] {
                var err error
                acc, err = eval.Apply(ctx, args[0], []core.Value{acc, item}, env)
                if err != nil { return nil, err }
            }

            return acc, nil
        },
    })
    
    // apply - calls fn with args spread from final collection argument
    env.Set("apply", core.GoFunc{
        Name: "apply",
        Fn: func(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
            if len(args) < 2 {
                return nil, fmt.Errorf("apply: requires at least 2 arguments")
            }

            fn := args[0]
            // All middle args are prepended; last arg must be a collection
            last := args[len(args)-1]

            var tail []core.Value
            switch c := last.(type) {
            case core.List:
                tail = c.Items
            case core.Vector:
                tail = c.Items
            default:
                return nil, fmt.Errorf("apply: last argument must be collection, got %T", last)
            }

            callArgs := append(args[1:len(args)-1], tail...)
            return eval.Apply(ctx, fn, callArgs, env)
        },
    })
}
```

---

## 6. Bootstrap Lisp (Threading Macros)

```go
func (p *Plugin) loadBootstrap(env *core.Env) error {
	// Threading macro -> (thread-first): inserts x as second element of each form
	bootstrapCode := []string{
		// -> and ->> are defined using loop/recur so they only need primitives
		`(defmacro ->
  [x & forms]
  (loop [acc x
         fs forms]
    (if (empty? fs)
      acc
      (let [form (first fs)
            threaded (if (list? form)
                       (cons (first form) (cons acc (rest form)))
                       (list form acc))]
        (recur threaded (rest fs))))))`,

		// ->> thread-last: appends x as last element using conj
		`(defmacro ->>
  [x & forms]
  (loop [acc x
         fs forms]
    (if (empty? fs)
      acc
      (let [form (first fs)
            threaded (if (list? form)
                       (conj form acc)
                       (list form acc))]
        (recur threaded (rest fs))))))`,

		// as->: thread with named binding so position is explicit
		`(defmacro as-> [expr name & forms]
  (let [~name ~expr
        ~@(loop [acc [] forms forms]
            (if (empty? forms)
              acc
              (recur (conj acc name (first forms)) (rest forms))))]
    ~name))`,

		// get-in: nested key access via reduce over get
		`(defn get-in [m ks]
  (reduce get m ks))`,

		// if-let: bind and branch on truthiness
		`(defmacro if-let
  [bindings then else]
  (let [name (first bindings)
        val (first (rest bindings))]
    (list (quote let) (vector name val)
      (list (quote if) name then else))))`,

		// when-let: bind and execute body if truthy
		`(defmacro when-let
  [bindings & body]
  (let [name (first bindings)
        val (first (rest bindings))]
    (list (quote let) (vector name val)
      (cons (quote when) (cons name body)))))`,
	}

	evaluator := core.NewEvaluator()
	for _, code := range bootstrapCode {
		reader := core.NewReader(code)
		tokens, err := reader.Tokenize()
		if err != nil { return fmt.Errorf("bootstrap tokenize: %w", err) }

		parser := core.NewParser(tokens)
		form, err := parser.Parse()
		if err != nil { return fmt.Errorf("bootstrap parse: %w", err) }
		if form == nil { continue }

		ctx := context.Background()
		if _, err = evaluator.Eval(ctx, form, env); err != nil {
			return fmt.Errorf("bootstrap eval: %w", err)
		}
	}
	return nil
}
```

---

## 7. File Organization

```
plugins/stdlib/
├── plugin.go         # Main plugin implementation
├── arithmetic.go     # +, -, *, /, mod, pow, etc.
├── strings.go        # str, format, split, join, etc.
├── collections.go    # list, vector, map, first, rest, etc.
├── higher_order.go   # map, filter, reduce, apply, etc.
├── control.go        # try/catch, assert
├── types.go          # type predicates
├── bootstrap.lisp    # Threading macros
└── stdlib_test.go    # Test suite
```

---

**Next Step:** Create tasks document (03-tasks.md) with implementation phases and test strategy.
