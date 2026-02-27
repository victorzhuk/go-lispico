package core

import (
	"testing"
)

func TestNil(t *testing.T) {
	t.Parallel()
	n := Nil{}
	if n.String() != "nil" {
		t.Errorf("String() = %q, want nil", n.String())
	}
	if n.Type().V != "nil" {
		t.Errorf("Type().V = %q, want nil", n.Type().V)
	}
	if !n.Equals(Nil{}) {
		t.Error("Nil should equal Nil")
	}
	if n.Equals(Bool{V: false}) {
		t.Error("Nil should not equal Bool{false}")
	}
}

func TestBool(t *testing.T) {
	t.Parallel()
	tests := []struct {
		val  Bool
		want string
	}{
		{Bool{V: true}, "true"},
		{Bool{V: false}, "false"},
	}
	for _, tt := range tests {
		if tt.val.String() != tt.want {
			t.Errorf("Bool{%v}.String() = %q, want %q", tt.val.V, tt.val.String(), tt.want)
		}
	}
	bt := Bool{V: true}
	if !bt.Equals(Bool{V: true}) {
		t.Error("Bool{true} should equal Bool{true}")
	}
	if bt.Equals(Bool{V: false}) {
		t.Error("Bool{true} should not equal Bool{false}")
	}
}

func TestInt(t *testing.T) {
	t.Parallel()
	i := Int{V: 42}
	if i.String() != "42" {
		t.Errorf("String() = %q, want 42", i.String())
	}
	if !i.Equals(Int{V: 42}) {
		t.Error("Int{42} should equal Int{42}")
	}
	if i.Equals(Float{V: 42.0}) {
		t.Error("Int should not equal Float with same value")
	}
}

func TestFloat(t *testing.T) {
	t.Parallel()
	f := Float{V: 3.14}
	if f.String() != "3.14" {
		t.Errorf("String() = %q, want 3.14", f.String())
	}
	if !f.Equals(Float{V: 3.14}) {
		t.Error("Float{3.14} should equal Float{3.14}")
	}
}

func TestString(t *testing.T) {
	t.Parallel()
	s := String{V: "hello"}
	if s.String() != `"hello"` {
		t.Errorf("String() = %q, want \"hello\"", s.String())
	}
	if !s.Equals(String{V: "hello"}) {
		t.Error("String{hello} should equal String{hello}")
	}
	if s.Equals(String{V: "world"}) {
		t.Error("String{hello} should not equal String{world}")
	}
}

func TestSymbol(t *testing.T) {
	t.Parallel()
	s := Symbol{V: "foo"}
	if s.String() != "foo" {
		t.Errorf("String() = %q, want foo", s.String())
	}
	if !s.Equals(Symbol{V: "foo"}) {
		t.Error("Symbol{foo} should equal Symbol{foo}")
	}
}

func TestKeyword(t *testing.T) {
	t.Parallel()
	k := Keyword{V: "model"}
	if k.String() != ":model" {
		t.Errorf("String() = %q, want :model", k.String())
	}
	if !k.Equals(Keyword{V: "model"}) {
		t.Error("Keyword{model} should equal Keyword{model}")
	}
	if k.Equals(Symbol{V: "model"}) {
		t.Error("Keyword should not equal Symbol with same value")
	}
}

func TestList(t *testing.T) {
	t.Parallel()
	l := List{Items: []Value{Int{V: 1}, Int{V: 2}, Int{V: 3}}}
	if l.String() != "(1 2 3)" {
		t.Errorf("String() = %q, want (1 2 3)", l.String())
	}
	if !l.Equals(List{Items: []Value{Int{V: 1}, Int{V: 2}, Int{V: 3}}}) {
		t.Error("equal lists should be equal")
	}
	if l.Equals(List{Items: []Value{Int{V: 1}, Int{V: 2}}}) {
		t.Error("lists of different length should not be equal")
	}
	empty := List{}
	if empty.String() != "()" {
		t.Errorf("empty list String() = %q, want ()", empty.String())
	}
}

func TestVector(t *testing.T) {
	t.Parallel()
	v := Vector{Items: []Value{Int{V: 1}, Int{V: 2}}}
	if v.String() != "[1 2]" {
		t.Errorf("String() = %q, want [1 2]", v.String())
	}
	if !v.Equals(Vector{Items: []Value{Int{V: 1}, Int{V: 2}}}) {
		t.Error("equal vectors should be equal")
	}
}

func TestHashMap(t *testing.T) {
	t.Parallel()

	m := NewHashMap()
	if m.Len() != 0 {
		t.Errorf("new map len = %d, want 0", m.Len())
	}

	m2, err := m.Assoc(Keyword{V: "a"}, Int{V: 1})
	if err != nil {
		t.Fatalf("Assoc error: %v", err)
	}
	if m.Len() != 0 {
		t.Error("Assoc should return new map, not mutate original")
	}

	v, ok := m2.Get(Keyword{V: "a"})
	if !ok {
		t.Error("Get should find :a")
	}
	if !v.Equals(Int{V: 1}) {
		t.Errorf("Get :a = %v, want 1", v)
	}

	m3, err := m2.Dissoc(Keyword{V: "a"})
	if err != nil {
		t.Fatalf("Dissoc error: %v", err)
	}
	if m3.Len() != 0 {
		t.Errorf("after Dissoc len = %d, want 0", m3.Len())
	}

	_, notFound := m3.Get(Keyword{V: "a"})
	if notFound {
		t.Error("Dissoc should remove key")
	}
}

func TestHashMap_Equality(t *testing.T) {
	t.Parallel()
	m1 := NewHashMap()
	m1, _ = m1.Assoc(Keyword{V: "x"}, Int{V: 10})

	m2 := NewHashMap()
	m2, _ = m2.Assoc(Keyword{V: "x"}, Int{V: 10})

	if !m1.Equals(m2) {
		t.Error("maps with same content should be equal")
	}

	m3 := NewHashMap()
	m3, _ = m3.Assoc(Keyword{V: "x"}, Int{V: 99})
	if m1.Equals(m3) {
		t.Error("maps with different values should not be equal")
	}
}

func TestHashMap_UnhashableKey(t *testing.T) {
	t.Parallel()
	m := NewHashMap()
	_, err := m.Assoc(List{Items: []Value{Int{V: 1}}}, Int{V: 1})
	if err == nil {
		t.Error("Assoc with unhashable key should return error")
	}
}

func TestHashMap_TypeDisambiguation(t *testing.T) {
	t.Parallel()
	m := NewHashMap()
	m, _ = m.Assoc(Symbol{V: "true"}, Int{V: 1})
	m, _ = m.Assoc(Bool{V: true}, Int{V: 2})

	v1, _ := m.Get(Symbol{V: "true"})
	v2, _ := m.Get(Bool{V: true})

	if v1.Equals(v2) {
		t.Error("Symbol{true} and Bool{true} should be distinct keys")
	}
}

func TestGoFunc(t *testing.T) {
	t.Parallel()
	g := GoFunc{Name: "test-fn", Fn: nil}
	if g.String() != "#<builtin:test-fn>" {
		t.Errorf("String() = %q, want #<builtin:test-fn>", g.String())
	}
	if !g.Equals(GoFunc{Name: "test-fn", Fn: nil}) {
		t.Error("GoFunc equality by name")
	}
	if g.Equals(GoFunc{Name: "other", Fn: nil}) {
		t.Error("GoFunc with different names should not be equal")
	}
}

func TestLambda(t *testing.T) {
	t.Parallel()
	l := Lambda{Name: "my-fn"}
	if l.String() != "#<fn:my-fn>" {
		t.Errorf("String() = %q, want #<fn:my-fn>", l.String())
	}
	anon := Lambda{}
	if anon.String() != "#<fn>" {
		t.Errorf("anonymous lambda String() = %q, want #<fn>", anon.String())
	}
	if l.Equals(Lambda{Name: "my-fn"}) {
		t.Error("Lambda.Equals should always return false (closures are unique)")
	}
}

func TestMacro(t *testing.T) {
	t.Parallel()
	m := Macro{Name: "my-macro"}
	if m.String() != "#<macro:my-macro>" {
		t.Errorf("String() = %q, want #<macro:my-macro>", m.String())
	}
	anon := Macro{}
	if anon.String() != "#<macro>" {
		t.Errorf("anonymous macro String() = %q, want #<macro>", anon.String())
	}
	if m.Equals(Macro{Name: "my-macro"}) {
		t.Error("Macro.Equals should always return false")
	}
}

func TestEquals_CrossType(t *testing.T) {
	t.Parallel()
	b := Bool{V: true}
	i := Int{V: 1}
	f := Float{V: 1.0}
	s := String{V: "x"}
	sym := Symbol{V: "x"}

	if b.Equals(i) {
		t.Error("Bool should not equal Int")
	}
	if i.Equals(f) {
		t.Error("Int should not equal Float")
	}
	if f.Equals(i) {
		t.Error("Float should not equal Int")
	}
	if s.Equals(sym) {
		t.Error("String should not equal Symbol")
	}
	if sym.Equals(s) {
		t.Error("Symbol should not equal String")
	}
}

func TestHashMap_Dissoc_UnhashableKey(t *testing.T) {
	t.Parallel()
	m := NewHashMap()
	_, err := m.Dissoc(List{Items: []Value{Int{V: 1}}})
	if err == nil {
		t.Error("Dissoc with unhashable key should error")
	}
}

func TestFromGoValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input any
		want  Value
	}{
		{nil, Nil{}},
		{true, Bool{V: true}},
		{false, Bool{V: false}},
		{42, Int{V: 42}},
		{int64(100), Int{V: 100}},
		{3.14, Float{V: 3.14}},
		{"hello", String{V: "hello"}},
	}
	for _, tt := range tests {
		got, err := FromGoValue(tt.input)
		if err != nil {
			t.Errorf("FromGoValue(%v) error: %v", tt.input, err)
			continue
		}
		if !got.Equals(tt.want) {
			t.Errorf("FromGoValue(%v) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestFromGoValue_Slice(t *testing.T) {
	t.Parallel()
	got, err := FromGoValue([]any{1, "two", true})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	vec, ok := got.(Vector)
	if !ok {
		t.Fatalf("expected Vector, got %T", got)
	}
	if len(vec.Items) != 3 {
		t.Errorf("len = %d, want 3", len(vec.Items))
	}
}

func TestFromGoValue_Map(t *testing.T) {
	t.Parallel()
	got, err := FromGoValue(map[string]any{"name": "alice"})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	m, ok := got.(*HashMap)
	if !ok {
		t.Fatalf("expected *HashMap, got %T", got)
	}
	v, found := m.Get(Keyword{V: "name"})
	if !found {
		t.Fatal("expected key :name")
	}
	if !v.Equals(String{V: "alice"}) {
		t.Errorf("value = %v, want \"alice\"", v)
	}
}

func TestFromGoValue_UnsupportedType(t *testing.T) {
	t.Parallel()
	_, err := FromGoValue(struct{}{})
	if err == nil {
		t.Error("expected error for unsupported type")
	}
}

func TestToGoValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input Value
		want  any
	}{
		{Nil{}, nil},
		{Bool{V: true}, true},
		{Int{V: 7}, int64(7)},
		{Float{V: 1.5}, float64(1.5)},
		{String{V: "hi"}, "hi"},
		{Keyword{V: "k"}, "k"},
		{Symbol{V: "s"}, "s"},
	}
	for _, tt := range tests {
		got, err := ToGoValue(tt.input)
		if err != nil {
			t.Errorf("ToGoValue(%v) error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ToGoValue(%v) = %v (%T), want %v (%T)", tt.input, got, got, tt.want, tt.want)
		}
	}
}

func TestToGoValue_UnsupportedType(t *testing.T) {
	t.Parallel()
	_, err := ToGoValue(Lambda{})
	if err == nil {
		t.Error("expected error for Lambda")
	}
}

func TestValue_Type(t *testing.T) {
	t.Parallel()
	cases := []struct {
		val  Value
		want string
	}{
		{Bool{V: true}, "bool"},
		{Int{V: 1}, "int"},
		{Float{V: 1.0}, "float"},
		{String{V: ""}, "string"},
		{Symbol{V: ""}, "symbol"},
		{Keyword{V: ""}, "keyword"},
		{List{}, "list"},
		{Vector{}, "vector"},
		{NewHashMap(), "map"},
		{GoFunc{}, "fn"},
		{Lambda{}, "fn"},
		{Macro{}, "macro"},
	}
	for _, tt := range cases {
		got := tt.val.Type().V
		if got != tt.want {
			t.Errorf("%T.Type().V = %q, want %q", tt.val, got, tt.want)
		}
	}
}

func TestHashMap_String(t *testing.T) {
	t.Parallel()
	m := NewHashMap()
	if m.String() != "{}" {
		t.Errorf("empty map String() = %q, want {}", m.String())
	}
	m, _ = m.Assoc(Keyword{V: "x"}, Int{V: 1})
	s := m.String()
	if s != "{:x 1}" {
		t.Errorf("map String() = %q, want {:x 1}", s)
	}
}

func TestHashMap_Each(t *testing.T) {
	t.Parallel()
	m := NewHashMap()
	m, _ = m.Assoc(Keyword{V: "a"}, Int{V: 10})
	m, _ = m.Assoc(Keyword{V: "b"}, Int{V: 20})

	sum := int64(0)
	m.Each(func(_, v Value) {
		sum += v.(Int).V
	})
	if sum != 30 {
		t.Errorf("Each sum = %d, want 30", sum)
	}
}

func TestToGoValue_Collections(t *testing.T) {
	t.Parallel()

	list := List{Items: []Value{Int{V: 1}, Bool{V: true}}}
	got, err := ToGoValue(list)
	if err != nil {
		t.Fatalf("ToGoValue(List) error: %v", err)
	}
	slice, ok := got.([]any)
	if !ok || len(slice) != 2 {
		t.Errorf("ToGoValue(List) = %v (%T), want []any len 2", got, got)
	}

	vec := Vector{Items: []Value{String{V: "hi"}}}
	got2, err := ToGoValue(vec)
	if err != nil {
		t.Fatalf("ToGoValue(Vector) error: %v", err)
	}
	slice2, ok2 := got2.([]any)
	if !ok2 || len(slice2) != 1 {
		t.Errorf("ToGoValue(Vector) = %v, want []any len 1", got2)
	}

	hm := NewHashMap()
	hm, _ = hm.Assoc(Keyword{V: "k"}, Int{V: 5})
	got3, err := ToGoValue(hm)
	if err != nil {
		t.Fatalf("ToGoValue(HashMap) error: %v", err)
	}
	m3, ok3 := got3.(map[string]any)
	if !ok3 || m3["k"] != int64(5) {
		t.Errorf("ToGoValue(HashMap) = %v, want map[k:5]", got3)
	}
}

func TestHashMap_HashableKeyTypes(t *testing.T) {
	t.Parallel()
	m := NewHashMap()

	for _, key := range []Value{Nil{}, Float{V: 3.14}, Int{V: 99}, String{V: "s"}} {
		m2, err := m.Assoc(key, Bool{V: true})
		if err != nil {
			t.Errorf("Assoc(%v) error: %v", key, err)
			continue
		}
		_, found := m2.Get(key)
		if !found {
			t.Errorf("Get(%v) not found after Assoc", key)
		}
	}
}

func TestIsTruthy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		val  Value
		want bool
	}{
		{Nil{}, false},
		{Bool{V: false}, false},
		{Bool{V: true}, true},
		{Int{V: 0}, true},
		{Int{V: 1}, true},
		{Float{V: 0.0}, true},
		{String{V: ""}, true},
		{String{V: "hello"}, true},
		{List{}, true},
		{Vector{}, true},
		{NewHashMap(), true},
	}
	for _, tt := range tests {
		got := IsTruthy(tt.val)
		if got != tt.want {
			t.Errorf("IsTruthy(%v) = %v, want %v", tt.val, got, tt.want)
		}
	}
}

func TestHashMap_Set(t *testing.T) {
	t.Parallel()
	m := NewHashMap()
	if err := m.Set(Keyword{V: "a"}, Int{V: 1}); err != nil {
		t.Fatalf("Set error: %v", err)
	}
	v, ok := m.Get(Keyword{V: "a"})
	if !ok || !v.Equals(Int{V: 1}) {
		t.Errorf("Get after Set = %v, want 1", v)
	}
	if m.Len() != 1 {
		t.Errorf("Len after Set = %d, want 1", m.Len())
	}
}

func TestHashMap_Pairs(t *testing.T) {
	t.Parallel()
	m := NewHashMap()
	m.Set(Keyword{V: "a"}, Int{V: 1})
	m.Set(Keyword{V: "b"}, Int{V: 2})

	pairs := m.Pairs()
	if len(pairs) != 2 {
		t.Fatalf("len(Pairs) = %d, want 2", len(pairs))
	}

	found := make(map[string]int64)
	for _, p := range pairs {
		k := p[0].(Keyword).V
		v := p[1].(Int).V
		found[k] = v
	}
	if found["a"] != 1 || found["b"] != 2 {
		t.Errorf("Pairs content = %v, want {a:1, b:2}", found)
	}
}
