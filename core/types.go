package core

import (
	"context"
	"fmt"
	"maps"
	"math"
	"sort"
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

// CollectionLimiter is implemented by an Evaluator whose Engine caps the
// length of collections built by builtins such as range. Read-only: the value
// is fixed at Engine construction.
type CollectionLimiter interface {
	CollectionLimit() int
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

// True and False are shared Bool instances. BoxBool returns one of them
// instead of allocating a fresh Bool on every boolean result.
var (
	True  Value = Bool{V: true}
	False Value = Bool{V: false}
)

// BoxBool returns the shared True or False instance for b.
func BoxBool(b bool) Value {
	if b {
		return True
	}
	return False
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

// minPreboxedInt and maxPreboxedInt bound the shared Int instances BoxInt
// returns for. 0..255 already boxes alloc-free via the Go runtime's own
// small-value interface cache; this range extends that to negatives and the
// rest of the common small-integer span (loop counters, small arithmetic).
const (
	minPreboxedInt = -128
	maxPreboxedInt = 1023
)

var preboxedInts [maxPreboxedInt - minPreboxedInt + 1]Value

func init() {
	for i := range preboxedInts {
		preboxedInts[i] = Int{V: int64(i) + minPreboxedInt}
	}
}

// BoxInt returns a Value wrapping v, reusing a shared instance when v is in
// [-128, 1023] to avoid a heap allocation on the hot arithmetic path. Outside
// that range it boxes a fresh Int as usual.
func BoxInt(v int64) Value {
	if v >= minPreboxedInt && v <= maxPreboxedInt {
		return preboxedInts[v-minPreboxedInt]
	}
	return Int{V: v}
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

func (s Symbol) Type() Keyword  { return Keyword{V: "symbol"} }
func (s Symbol) String() string { return s.V }
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
// across types (e.g. symbol "true" vs bool true). Numeric and bool keys are
// derived from their bit pattern, never formatted through strconv/fmt, so
// Get/Set/Assoc stay allocation-free on the hot path.
type hashKey struct {
	typ uint8
	num uint64
	str string
}

// Cross-type ordering group. Assigned in the old type-name strings' sort
// order (bool < float < int < keyword < nil < string < symbol) to minimize
// iteration-order churn from the rewrite.
const (
	hkBool uint8 = iota
	hkFloat
	hkInt
	hkKeyword
	hkNil
	hkString
	hkSymbol
)

// less orders hash keys deterministically: (typ, num, str). Within a numeric
// type this is bit-pattern order, not true numeric order — a negative Int or
// Float has its sign bit set, so e.g. -1 sorts after positive values. The
// spec only requires deterministic, evaluator-identical order, not true
// numeric order.
func (hk hashKey) less(other hashKey) bool {
	if hk.typ != other.typ {
		return hk.typ < other.typ
	}
	if hk.num != other.num {
		return hk.num < other.num
	}
	return hk.str < other.str
}

// negZeroBits is math.Float64bits(-0.0). toHashKey folds it to 0 so +0.0 and
// -0.0 hash to one key — key identity then matches Float.Equals, where
// 0.0 == -0.0. (The old string-formatted keys kept them distinct: "0" vs "-0".)
const negZeroBits = uint64(1) << 63

func toHashKey(v Value) (hashKey, error) {
	switch val := v.(type) {
	case Nil:
		return hashKey{typ: hkNil}, nil
	case Bool:
		var n uint64
		if val.V {
			n = 1
		}
		return hashKey{typ: hkBool, num: n}, nil
	case Int:
		return hashKey{typ: hkInt, num: uint64(val.V)}, nil
	case Float:
		bits := math.Float64bits(val.V)
		switch {
		case math.IsNaN(val.V):
			bits = math.Float64bits(math.NaN())
		case bits == negZeroBits:
			bits = 0
		}
		return hashKey{typ: hkFloat, num: bits}, nil
	case String:
		return hashKey{typ: hkString, str: val.V}, nil
	case Symbol:
		return hashKey{typ: hkSymbol, str: val.V}, nil
	case Keyword:
		return hashKey{typ: hkKeyword, str: val.V}, nil
	default:
		return hashKey{}, fmt.Errorf("unhashable type: %T", v)
	}
}

// entry is one key-value pair. It keeps the original key Value alongside its
// hashKey so both storage forms below can render and iterate without a
// second parallel map.
type entry struct {
	hk hashKey
	k  Value
	v  Value
}

// hashMapSmallLimit caps the sorted-slice form: Assoc/Set promote to the map
// form on the 9th distinct key. Frozen by BenchmarkHashMap_ScanVsMap — below
// this size, a linear scan beats a Go map lookup.
const hashMapSmallLimit = 8

// HashMap — immutable associative map. Keys must be comparable (Nil, Bool, Int,
// Float, String, Symbol, Keyword). Operations return new maps.
//
// Below hashMapSmallLimit distinct keys, entries holds them sorted by hashKey
// and Get is a linear scan — cheap at this size and already iteration-order.
// Past the limit, m takes over as storage. Promotion is one-way: a map that
// shrinks back below the limit through Dissoc stays in map form.
type HashMap struct {
	entries []entry
	m       map[hashKey]entry
}

func NewHashMap() *HashMap {
	return &HashMap{}
}

func (h *HashMap) Type() Keyword { return Keyword{V: "map"} }

// find locates hk in the sorted small-form entries, or the index it would
// need to be inserted at to keep entries sorted. Unused when h.m != nil.
func (h *HashMap) find(hk hashKey) (int, bool) {
	for i := range h.entries {
		if h.entries[i].hk == hk {
			return i, true
		}
		if hk.less(h.entries[i].hk) {
			return i, false
		}
	}
	return len(h.entries), false
}

func (h *HashMap) getByHashKey(hk hashKey) (Value, bool) {
	if h.m != nil {
		e, ok := h.m[hk]
		return e.v, ok
	}
	if i, ok := h.find(hk); ok {
		return h.entries[i].v, true
	}
	return nil, false
}

// eachRaw walks every entry in storage order — unsorted for the map form.
// For callers like Equals that only need membership, not display order.
func (h *HashMap) eachRaw(fn func(e entry)) {
	if h.m != nil {
		for _, e := range h.m {
			fn(e)
		}
		return
	}
	for _, e := range h.entries {
		fn(e)
	}
}

// sortedEntries returns every entry in deterministic (typ, num, str) order.
// The small form is already sorted and returned as-is; the map form re-sorts
// on each call, same cost as before the rewrite.
func (h *HashMap) sortedEntries() []entry {
	if h.m == nil {
		return h.entries
	}
	entries := make([]entry, 0, len(h.m))
	for _, e := range h.m {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].hk.less(entries[j].hk)
	})
	return entries
}

func (h *HashMap) String() string {
	entries := h.sortedEntries()
	parts := make([]string, 0, len(entries)*2)
	for _, e := range entries {
		parts = append(parts, e.k.String()+" "+e.v.String())
	}
	return "{" + strings.Join(parts, " ") + "}"
}

func (h *HashMap) Equals(o Value) bool {
	v, ok := o.(*HashMap)
	if !ok || h.Len() != v.Len() {
		return false
	}
	equal := true
	h.eachRaw(func(e entry) {
		if !equal {
			return
		}
		other, found := v.getByHashKey(e.hk)
		if !found || !e.v.Equals(other) {
			equal = false
		}
	})
	return equal
}

// newMapFromEntries builds map-form storage from small-form entries plus one
// more, used when Assoc/Set crosses hashMapSmallLimit.
func newMapFromEntries(entries []entry, extra entry) map[hashKey]entry {
	m := make(map[hashKey]entry, len(entries)+1)
	for _, e := range entries {
		m[e.hk] = e
	}
	m[extra.hk] = extra
	return m
}

func (h *HashMap) Assoc(key, val Value) (*HashMap, error) {
	hk, err := toHashKey(key)
	if err != nil {
		return nil, err
	}
	e := entry{hk: hk, k: key, v: val}
	if h.m != nil {
		out := make(map[hashKey]entry, len(h.m)+1)
		maps.Copy(out, h.m)
		out[hk] = e
		return &HashMap{m: out}, nil
	}
	i, found := h.find(hk)
	if found {
		entries := make([]entry, len(h.entries))
		copy(entries, h.entries)
		entries[i] = e
		return &HashMap{entries: entries}, nil
	}
	if len(h.entries) >= hashMapSmallLimit {
		return &HashMap{m: newMapFromEntries(h.entries, e)}, nil
	}
	entries := make([]entry, len(h.entries)+1)
	copy(entries, h.entries[:i])
	entries[i] = e
	copy(entries[i+1:], h.entries[i:])
	return &HashMap{entries: entries}, nil
}

func (h *HashMap) Dissoc(key Value) (*HashMap, error) {
	hk, err := toHashKey(key)
	if err != nil {
		return nil, err
	}
	if h.m != nil {
		out := make(map[hashKey]entry, len(h.m))
		for k, e := range h.m {
			if k != hk {
				out[k] = e
			}
		}
		return &HashMap{m: out}, nil
	}
	entries := make([]entry, 0, len(h.entries))
	for _, e := range h.entries {
		if e.hk != hk {
			entries = append(entries, e)
		}
	}
	return &HashMap{entries: entries}, nil
}

func (h *HashMap) Get(key Value) (Value, bool) {
	hk, err := toHashKey(key)
	if err != nil {
		return nil, false
	}
	v, ok := h.getByHashKey(hk)
	if !ok {
		return Nil{}, false
	}
	return v, true
}

func (h *HashMap) Len() int {
	if h.m != nil {
		return len(h.m)
	}
	return len(h.entries)
}

// Set mutably inserts a key-value pair. It is an in-place escape hatch for
// building a fresh map before it is shared; callers holding a HashMap that may
// already be referenced elsewhere must use the copy-on-write Assoc/Dissoc
// instead to preserve immutability.
func (h *HashMap) Set(key, val Value) error {
	hk, err := toHashKey(key)
	if err != nil {
		return err
	}
	e := entry{hk: hk, k: key, v: val}
	if h.m != nil {
		h.m[hk] = e
		return nil
	}
	i, found := h.find(hk)
	if found {
		h.entries[i] = e
		return nil
	}
	if len(h.entries) >= hashMapSmallLimit {
		h.m = newMapFromEntries(h.entries, e)
		h.entries = nil
		return nil
	}
	h.entries = append(h.entries, entry{})
	copy(h.entries[i+1:], h.entries[i:len(h.entries)-1])
	h.entries[i] = e
	return nil
}

// Each calls fn for every key-value pair in the map, in deterministic order.
func (h *HashMap) Each(fn func(k, v Value)) {
	for _, e := range h.sortedEntries() {
		fn(e.k, e.v)
	}
}

// Pairs returns all key-value pairs as [2]Value arrays, in deterministic order.
func (h *HashMap) Pairs() [][2]Value {
	entries := h.sortedEntries()
	pairs := make([][2]Value, len(entries))
	for i, e := range entries {
		pairs[i] = [2]Value{e.k, e.v}
	}
	return pairs
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

// IsTruthy returns true for all values except Nil and false.
func IsTruthy(v Value) bool {
	switch val := v.(type) {
	case Nil:
		return false
	case Bool:
		return val.V
	default:
		return true
	}
}

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
