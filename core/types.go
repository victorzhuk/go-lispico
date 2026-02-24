package core

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// Value is the universal Lisp value interface.
type Value interface {
	Type() Keyword
	String() string
	Equals(other Value) bool
}

// Evaluator allows GoFunc implementations to recursively evaluate Lisp forms.
// Defined here (not in eval.go) to avoid circular imports — GoFunc needs it.
type Evaluator interface {
	Eval(ctx context.Context, form Value, env *Env) (Value, error)
	Apply(ctx context.Context, fn Value, args []Value, env *Env) (Value, error)
}

// Nil — the empty value; the only falsy value besides false.
type Nil struct{}

func (n Nil) Type() Keyword       { return Keyword{V: "nil"} }
func (n Nil) String() string      { return "nil" }
func (n Nil) Equals(o Value) bool { _, ok := o.(Nil); return ok }

// Bool — true or false.
type Bool struct{ V bool }

func (b Bool) Type() Keyword { return Keyword{V: "bool"} }
func (b Bool) String() string {
	if b.V {
		return "true"
	}
	return "false"
}
func (b Bool) Equals(o Value) bool {
	if v, ok := o.(Bool); ok {
		return b.V == v.V
	}
	return false
}

// Int — fixed-precision signed 64-bit integer.
type Int struct{ V int64 }

func (i Int) Type() Keyword { return Keyword{V: "int"} }
func (i Int) String() string {
	return strconv.FormatInt(i.V, 10)
}
func (i Int) Equals(o Value) bool {
	if v, ok := o.(Int); ok {
		return i.V == v.V
	}
	return false
}

// Float — IEEE 754 double.
type Float struct{ V float64 }

func (f Float) Type() Keyword { return Keyword{V: "float"} }
func (f Float) String() string {
	return strconv.FormatFloat(f.V, 'f', -1, 64)
}
func (f Float) Equals(o Value) bool {
	if v, ok := o.(Float); ok {
		return f.V == v.V
	}
	return false
}

// String — UTF-8 immutable string.
type String struct{ V string }

func (s String) Type() Keyword { return Keyword{V: "string"} }
func (s String) String() string {
	return fmt.Sprintf("%q", s.V)
}
func (s String) Equals(o Value) bool {
	if v, ok := o.(String); ok {
		return s.V == v.V
	}
	return false
}

// Symbol — resolves to a value in the environment.
type Symbol struct{ V string }

func (s Symbol) Type() Keyword    { return Keyword{V: "symbol"} }
func (s Symbol) String() string   { return s.V }
func (s Symbol) Equals(o Value) bool {
	if v, ok := o.(Symbol); ok {
		return s.V == v.V
	}
	return false
}

// Keyword — self-evaluating named constant; used as map keys and option flags.
type Keyword struct{ V string }

func (k Keyword) Type() Keyword { return Keyword{V: "keyword"} }
func (k Keyword) String() string {
	return ":" + k.V
}
func (k Keyword) Equals(o Value) bool {
	if v, ok := o.(Keyword); ok {
		return k.V == v.V
	}
	return false
}

// List — immutable sequence (slice implementation).
type List struct{ Items []Value }

func (l List) Type() Keyword { return Keyword{V: "list"} }
func (l List) String() string {
	parts := make([]string, len(l.Items))
	for i, item := range l.Items {
		parts[i] = item.String()
	}
	return "(" + strings.Join(parts, " ") + ")"
}
func (l List) Equals(o Value) bool {
	v, ok := o.(List)
	if !ok || len(l.Items) != len(v.Items) {
		return false
	}
	for i := range l.Items {
		if !l.Items[i].Equals(v.Items[i]) {
			return false
		}
	}
	return true
}

// Vector — random-access sequence.
type Vector struct{ Items []Value }

func (v Vector) Type() Keyword { return Keyword{V: "vector"} }
func (v Vector) String() string {
	parts := make([]string, len(v.Items))
	for i, item := range v.Items {
		parts[i] = item.String()
	}
	return "[" + strings.Join(parts, " ") + "]"
}
func (v Vector) Equals(o Value) bool {
	other, ok := o.(Vector)
	if !ok || len(v.Items) != len(other.Items) {
		return false
	}
	for i := range v.Items {
		if !v.Items[i].Equals(other.Items[i]) {
			return false
		}
	}
	return true
}

// hashKey is the internal map key — disambiguates equal string representations
// across types (e.g. symbol "true" vs bool true).
type hashKey struct {
	typ string
	val string
}

func toHashKey(v Value) (hashKey, error) {
	switch val := v.(type) {
	case Nil:
		return hashKey{"nil", ""}, nil
	case Bool:
		return hashKey{"bool", fmt.Sprintf("%t", val.V)}, nil
	case Int:
		return hashKey{"int", strconv.FormatInt(val.V, 10)}, nil
	case Float:
		return hashKey{"float", strconv.FormatFloat(val.V, 'f', -1, 64)}, nil
	case String:
		return hashKey{"string", val.V}, nil
	case Symbol:
		return hashKey{"symbol", val.V}, nil
	case Keyword:
		return hashKey{"keyword", val.V}, nil
	default:
		return hashKey{}, fmt.Errorf("unhashable type: %T", v)
	}
}

// HashMap — immutable associative map. Keys must be comparable (Nil, Bool, Int,
// Float, String, Symbol, Keyword). Operations return new maps.
type HashMap struct {
	m    map[hashKey]Value // internal storage
	keys map[hashKey]Value // original key Values for display/iteration
}

func NewHashMap() *HashMap {
	return &HashMap{
		m:    make(map[hashKey]Value),
		keys: make(map[hashKey]Value),
	}
}

func (h *HashMap) Type() Keyword { return Keyword{V: "map"} }
func (h *HashMap) String() string {
	parts := make([]string, 0, len(h.m)*2)
	for hk, v := range h.m {
		parts = append(parts, h.keys[hk].String()+" "+v.String())
	}
	return "{" + strings.Join(parts, " ") + "}"
}
func (h *HashMap) Equals(o Value) bool {
	v, ok := o.(*HashMap)
	if !ok || len(h.m) != len(v.m) {
		return false
	}
	for hk, val := range h.m {
		other, ok := v.m[hk]
		if !ok || !val.Equals(other) {
			return false
		}
	}
	return true
}

func (h *HashMap) Assoc(key, val Value) (*HashMap, error) {
	hk, err := toHashKey(key)
	if err != nil {
		return nil, err
	}
	out := NewHashMap()
	for k, v := range h.m {
		out.m[k] = v
		out.keys[k] = h.keys[k]
	}
	out.m[hk] = val
	out.keys[hk] = key
	return out, nil
}

func (h *HashMap) Dissoc(key Value) (*HashMap, error) {
	hk, err := toHashKey(key)
	if err != nil {
		return nil, err
	}
	out := NewHashMap()
	for k, v := range h.m {
		if k != hk {
			out.m[k] = v
			out.keys[k] = h.keys[k]
		}
	}
	return out, nil
}

func (h *HashMap) Get(key Value) (Value, bool) {
	hk, err := toHashKey(key)
	if err != nil {
		return nil, false
	}
	v, ok := h.m[hk]
	if !ok {
		return Nil{}, false
	}
	return v, true
}

func (h *HashMap) Len() int { return len(h.m) }

// Each calls fn for every key-value pair in the map.
func (h *HashMap) Each(fn func(k, v Value)) {
	for hk, v := range h.m {
		fn(h.keys[hk], v)
	}
}

// GoFunc — native Go function callable from Lisp.
// Receives context, the evaluator (for recursive eval), args, and the current env.
type GoFunc struct {
	Name string
	Fn   func(ctx context.Context, eval Evaluator, args []Value, env *Env) (Value, error)
}

func (g GoFunc) Type() Keyword { return Keyword{V: "fn"} }
func (g GoFunc) String() string {
	return "#<builtin:" + g.Name + ">"
}
func (g GoFunc) Equals(o Value) bool {
	v, ok := o.(GoFunc)
	return ok && g.Name == v.Name
}

// Lambda — user-defined closure.
type Lambda struct {
	Params   []Symbol
	Variadic Symbol // non-empty V = variadic; bound as List to remaining args
	Body     []Value
	Env      *Env
	Name     string // optional, enables self-recursion by name
}

func (l Lambda) Type() Keyword { return Keyword{V: "fn"} }
func (l Lambda) String() string {
	if l.Name != "" {
		return "#<fn:" + l.Name + ">"
	}
	return "#<fn>"
}
func (l Lambda) Equals(o Value) bool { return false }

// Macro — syntax transformer; body receives unevaluated forms.
type Macro struct {
	Params   []Symbol
	Variadic Symbol
	Body     []Value
	Env      *Env
	Name     string
}

func (m Macro) Type() Keyword { return Keyword{V: "macro"} }
func (m Macro) String() string {
	if m.Name != "" {
		return "#<macro:" + m.Name + ">"
	}
	return "#<macro>"
}
func (m Macro) Equals(o Value) bool { return false }

// FromGoValue converts a native Go value to a Lisp Value.
func FromGoValue(v any) (Value, error) {
	switch val := v.(type) {
	case nil:
		return Nil{}, nil
	case bool:
		return Bool{V: val}, nil
	case int:
		return Int{V: int64(val)}, nil
	case int64:
		return Int{V: val}, nil
	case float64:
		return Float{V: val}, nil
	case string:
		return String{V: val}, nil
	case []any:
		items := make([]Value, len(val))
		for i, item := range val {
			v, err := FromGoValue(item)
			if err != nil {
				return nil, err
			}
			items[i] = v
		}
		return Vector{Items: items}, nil
	case map[string]any:
		m := NewHashMap()
		var err error
		for k, v := range val {
			value, ferr := FromGoValue(v)
			if ferr != nil {
				return nil, ferr
			}
			m, err = m.Assoc(Keyword{V: k}, value)
			if err != nil {
				return nil, err
			}
		}
		return m, nil
	default:
		return nil, fmt.Errorf("unsupported Go type: %T", v)
	}
}

// ToGoValue converts a Lisp Value to a native Go value.
func ToGoValue(v Value) (any, error) {
	switch val := v.(type) {
	case Nil:
		return nil, nil
	case Bool:
		return val.V, nil
	case Int:
		return val.V, nil
	case Float:
		return val.V, nil
	case String:
		return val.V, nil
	case Keyword:
		return val.V, nil
	case Symbol:
		return val.V, nil
	case Vector:
		result := make([]any, len(val.Items))
		for i, item := range val.Items {
			v, err := ToGoValue(item)
			if err != nil {
				return nil, err
			}
			result[i] = v
		}
		return result, nil
	case List:
		result := make([]any, len(val.Items))
		for i, item := range val.Items {
			v, err := ToGoValue(item)
			if err != nil {
				return nil, err
			}
			result[i] = v
		}
		return result, nil
	case *HashMap:
		result := make(map[string]any)
		var convErr error
		val.Each(func(k, v Value) {
			if convErr != nil {
				return
			}
			keyVal, err := ToGoValue(k)
			if err != nil {
				convErr = err
				return
			}
			keyStr, ok := keyVal.(string)
			if !ok {
				convErr = fmt.Errorf("map key must convert to string, got %T", keyVal)
				return
			}
			value, err := ToGoValue(v)
			if err != nil {
				convErr = err
				return
			}
			result[keyStr] = value
		})
		if convErr != nil {
			return nil, convErr
		}
		return result, nil
	default:
		return nil, fmt.Errorf("unsupported Lisp type: %T", v)
	}
}
