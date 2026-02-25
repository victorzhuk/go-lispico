package stdlib

import (
	"context"
	"testing"

	"github.com/victorzhuk/go-lispico/core"
)

func setupEnv(t *testing.T) *core.Env {
	t.Helper()
	env := core.NewEnv(nil)
	p := New()
	if err := p.Init(env); err != nil {
		t.Fatalf("failed to init stdlib: %v", err)
	}
	return env
}

func eval(t *testing.T, env *core.Env, code string) core.Value {
	t.Helper()
	forms, err := core.Read(code)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if len(forms) == 0 {
		t.Fatal("empty input")
	}
	eval := core.NewEvaluator()
	result, err := eval.Eval(context.Background(), forms[0], env)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	return result
}

func evalErr(t *testing.T, env *core.Env, code string) error {
	t.Helper()
	forms, err := core.Read(code)
	if err != nil {
		return err
	}
	if len(forms) == 0 {
		t.Fatal("empty input")
	}
	eval := core.NewEvaluator()
	_, err = eval.Eval(context.Background(), forms[0], env)
	return err
}

func TestArithmetic_Plus(t *testing.T) {
	env := setupEnv(t)

	tests := []struct {
		name     string
		input    string
		expected core.Value
	}{
		{"empty", "(+)", core.Int{V: 0}},
		{"single int", "(+ 5)", core.Int{V: 5}},
		{"two ints", "(+ 1 2)", core.Int{V: 3}},
		{"multiple ints", "(+ 1 2 3 4)", core.Int{V: 10}},
		{"int and float", "(+ 1 2.5)", core.Float{V: 3.5}},
		{"all floats", "(+ 1.5 2.5)", core.Float{V: 4.0}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := eval(t, env, tt.input)
			if !result.Equals(tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestArithmetic_Minus(t *testing.T) {
	env := setupEnv(t)

	tests := []struct {
		name     string
		input    string
		expected core.Value
	}{
		{"unary negation", "(- 5)", core.Int{V: -5}},
		{"binary", "(- 10 3)", core.Int{V: 7}},
		{"multiple", "(- 10 1 2 3)", core.Int{V: 4}},
		{"float negation", "(- 3.5)", core.Float{V: -3.5}},
		{"int float mix", "(- 10 2.5)", core.Float{V: 7.5}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := eval(t, env, tt.input)
			if !result.Equals(tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestArithmetic_Multiply(t *testing.T) {
	env := setupEnv(t)

	tests := []struct {
		name     string
		input    string
		expected core.Value
	}{
		{"empty", "(*)", core.Int{V: 1}},
		{"single", "(* 5)", core.Int{V: 5}},
		{"two ints", "(* 3 4)", core.Int{V: 12}},
		{"multiple", "(* 2 3 4)", core.Int{V: 24}},
		{"int float", "(* 3 2.5)", core.Float{V: 7.5}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := eval(t, env, tt.input)
			if !result.Equals(tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestArithmetic_Divide(t *testing.T) {
	env := setupEnv(t)

	tests := []struct {
		name     string
		input    string
		expected core.Value
	}{
		{"simple", "(/ 10 2)", core.Float{V: 5.0}},
		{"multiple", "(/ 100 2 5)", core.Float{V: 10.0}},
		{"float result", "(/ 5 2)", core.Float{V: 2.5}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := eval(t, env, tt.input)
			if !result.Equals(tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestArithmetic_Mod(t *testing.T) {
	env := setupEnv(t)

	tests := []struct {
		name     string
		input    string
		expected core.Value
	}{
		{"basic", "(mod 10 3)", core.Int{V: 1}},
		{"zero remainder", "(mod 9 3)", core.Int{V: 0}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := eval(t, env, tt.input)
			if !result.Equals(tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestArithmetic_Quot(t *testing.T) {
	env := setupEnv(t)

	tests := []struct {
		name     string
		input    string
		expected core.Value
	}{
		{"basic", "(quot 10 3)", core.Int{V: 3}},
		{"exact", "(quot 12 4)", core.Int{V: 3}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := eval(t, env, tt.input)
			if !result.Equals(tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestArithmetic_MathFuncs(t *testing.T) {
	env := setupEnv(t)

	tests := []struct {
		name     string
		input    string
		expected core.Value
	}{
		{"pow", "(pow 2 3)", core.Float{V: 8.0}},
		{"sqrt", "(sqrt 16)", core.Float{V: 4.0}},
		{"abs positive", "(abs 5)", core.Float{V: 5.0}},
		{"abs negative", "(abs -5)", core.Float{V: 5.0}},
		{"floor", "(floor 3.7)", core.Float{V: 3.0}},
		{"ceil", "(ceil 3.2)", core.Float{V: 4.0}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := eval(t, env, tt.input)
			if !result.Equals(tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestArithmetic_Predicates(t *testing.T) {
	env := setupEnv(t)

	tests := []struct {
		name     string
		input    string
		expected core.Value
	}{
		{"zero? true", "(zero? 0)", core.Bool{V: true}},
		{"zero? false", "(zero? 1)", core.Bool{V: false}},
		{"pos? true", "(pos? 5)", core.Bool{V: true}},
		{"pos? false", "(pos? -5)", core.Bool{V: false}},
		{"neg? true", "(neg? -5)", core.Bool{V: true}},
		{"neg? false", "(neg? 5)", core.Bool{V: false}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := eval(t, env, tt.input)
			if !result.Equals(tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestArithmetic_MinMax(t *testing.T) {
	env := setupEnv(t)

	tests := []struct {
		name     string
		input    string
		expected core.Value
	}{
		{"max ints", "(max 1 5 3)", core.Int{V: 5}},
		{"min ints", "(min 1 5 3)", core.Int{V: 1}},
		{"max with float", "(max 1 5.5 3)", core.Float{V: 5.5}},
		{"min with float", "(min 1.5 5 3)", core.Float{V: 1.5}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := eval(t, env, tt.input)
			if !result.Equals(tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestStrings_Basic(t *testing.T) {
	env := setupEnv(t)

	tests := []struct {
		name     string
		input    string
		expected core.Value
	}{
		{"str empty", `(str)`, core.String{V: ""}},
		{"str single", `(str "hello")`, core.String{V: "hello"}},
		{"str concat", `(str "hello" " " "world")`, core.String{V: "hello world"}},
		{"str with number", `(str "value: " 42)`, core.String{V: "value: 42"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := eval(t, env, tt.input)
			if !result.Equals(tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestStrings_Transformations(t *testing.T) {
	env := setupEnv(t)

	tests := []struct {
		name     string
		input    string
		expected core.Value
	}{
		{"upper", `(string/upper "hello")`, core.String{V: "HELLO"}},
		{"lower", `(string/lower "HELLO")`, core.String{V: "hello"}},
		{"trim", `(string/trim "  hello  ")`, core.String{V: "hello"}},
		{"replace", `(string/replace "hello world" "world" "there")`, core.String{V: "hello there"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := eval(t, env, tt.input)
			if !result.Equals(tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestStrings_SplitJoin(t *testing.T) {
	env := setupEnv(t)

	t.Run("split", func(t *testing.T) {
		result := eval(t, env, `(string/split "a,b,c" ",")`)
		list, ok := result.(core.List)
		if !ok {
			t.Fatalf("expected list, got %T", result)
		}
		if len(list.Items) != 3 {
			t.Errorf("expected 3 items, got %d", len(list.Items))
		}
	})

	t.Run("join", func(t *testing.T) {
		result := eval(t, env, `(string/join (list "a" "b" "c") "-")`)
		expected := core.String{V: "a-b-c"}
		if !result.Equals(expected) {
			t.Errorf("expected %v, got %v", expected, result)
		}
	})
}

func TestStrings_Predicates(t *testing.T) {
	env := setupEnv(t)

	tests := []struct {
		name     string
		input    string
		expected core.Value
	}{
		{"contains? true", `(string/contains? "hello" "ell")`, core.Bool{V: true}},
		{"contains? false", `(string/contains? "hello" "xyz")`, core.Bool{V: false}},
		{"starts-with? true", `(string/starts-with? "hello" "hel")`, core.Bool{V: true}},
		{"starts-with? false", `(string/starts-with? "hello" "xyz")`, core.Bool{V: false}},
		{"ends-with? true", `(string/ends-with? "hello" "llo")`, core.Bool{V: true}},
		{"ends-with? false", `(string/ends-with? "hello" "xyz")`, core.Bool{V: false}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := eval(t, env, tt.input)
			if !result.Equals(tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestStrings_Utilities(t *testing.T) {
	env := setupEnv(t)

	tests := []struct {
		name     string
		input    string
		expected core.Value
	}{
		{"length", `(string/length "hello")`, core.Int{V: 5}},
		{"length unicode", `(string/length "日本語")`, core.Int{V: 3}},
		{"string->int", `(string->int "42")`, core.Int{V: 42}},
		{"string->float", `(string->float "3.14")`, core.Float{V: 3.14}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := eval(t, env, tt.input)
			if !result.Equals(tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestCollections_Constructors(t *testing.T) {
	env := setupEnv(t)

	t.Run("list", func(t *testing.T) {
		result := eval(t, env, `(list 1 2 3)`)
		list, ok := result.(core.List)
		if !ok {
			t.Fatalf("expected list, got %T", result)
		}
		if len(list.Items) != 3 {
			t.Errorf("expected 3 items, got %d", len(list.Items))
		}
	})

	t.Run("vector", func(t *testing.T) {
		result := eval(t, env, `(vector 1 2 3)`)
		vec, ok := result.(core.Vector)
		if !ok {
			t.Fatalf("expected vector, got %T", result)
		}
		if len(vec.Items) != 3 {
			t.Errorf("expected 3 items, got %d", len(vec.Items))
		}
	})

	t.Run("hash-map", func(t *testing.T) {
		result := eval(t, env, `(hash-map :a 1 :b 2)`)
		m, ok := result.(*core.HashMap)
		if !ok {
			t.Fatalf("expected hashmap, got %T", result)
		}
		if m.Len() != 2 {
			t.Errorf("expected 2 entries, got %d", m.Len())
		}
	})
}

func TestCollections_Access(t *testing.T) {
	env := setupEnv(t)

	tests := []struct {
		name     string
		input    string
		expected core.Value
	}{
		{"first", `(first (list 1 2 3))`, core.Int{V: 1}},
		{"first empty", `(first (list))`, core.Nil{}},
		{"rest", `(rest (list 1 2 3))`, core.List{Items: []core.Value{core.Int{V: 2}, core.Int{V: 3}}}},
		{"rest empty", `(rest (list))`, core.List{Items: []core.Value{}}},
		{"last", `(last (list 1 2 3))`, core.Int{V: 3}},
		{"nth", `(nth (list 1 2 3) 1)`, core.Int{V: 2}},
		{"nth with default", `(nth (list 1 2 3) 10 :not-found)`, core.Keyword{V: "not-found"}},
		{"count list", `(count (list 1 2 3))`, core.Int{V: 3}},
		{"count vector", `(count [1 2 3])`, core.Int{V: 3}},
		{"count string", `(count "hello")`, core.Int{V: 5}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := eval(t, env, tt.input)
			if !result.Equals(tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestCollections_ConsConj(t *testing.T) {
	env := setupEnv(t)

	t.Run("cons", func(t *testing.T) {
		result := eval(t, env, `(cons 0 (list 1 2 3))`)
		list, ok := result.(core.List)
		if !ok {
			t.Fatalf("expected list, got %T", result)
		}
		if len(list.Items) != 4 {
			t.Errorf("expected 4 items, got %d", len(list.Items))
		}
		if !list.Items[0].Equals(core.Int{V: 0}) {
			t.Errorf("expected first item to be 0")
		}
	})

	t.Run("conj list", func(t *testing.T) {
		result := eval(t, env, `(conj (list 2 3) 1)`)
		list, ok := result.(core.List)
		if !ok {
			t.Fatalf("expected list, got %T", result)
		}
		if !list.Items[0].Equals(core.Int{V: 1}) {
			t.Errorf("expected first item to be 1")
		}
	})

	t.Run("conj vector", func(t *testing.T) {
		result := eval(t, env, `(conj [1 2] 3)`)
		vec, ok := result.(core.Vector)
		if !ok {
			t.Fatalf("expected vector, got %T", result)
		}
		if !vec.Items[2].Equals(core.Int{V: 3}) {
			t.Errorf("expected last item to be 3")
		}
	})
}

func TestCollections_MapOps(t *testing.T) {
	env := setupEnv(t)

	t.Run("get", func(t *testing.T) {
		result := eval(t, env, `(get (hash-map :a 1 :b 2) :a)`)
		if !result.Equals(core.Int{V: 1}) {
			t.Errorf("expected 1, got %v", result)
		}
	})

	t.Run("get missing", func(t *testing.T) {
		result := eval(t, env, `(get (hash-map :a 1) :missing)`)
		if _, ok := result.(core.Nil); !ok {
			t.Errorf("expected nil, got %v", result)
		}
	})

	t.Run("get with default", func(t *testing.T) {
		result := eval(t, env, `(get (hash-map :a 1) :missing :not-found)`)
		if !result.Equals(core.Keyword{V: "not-found"}) {
			t.Errorf("expected :not-found, got %v", result)
		}
	})

	t.Run("assoc", func(t *testing.T) {
		result := eval(t, env, `(assoc (hash-map :a 1) :b 2)`)
		m, ok := result.(*core.HashMap)
		if !ok {
			t.Fatalf("expected hashmap, got %T", result)
		}
		if m.Len() != 2 {
			t.Errorf("expected 2 entries, got %d", m.Len())
		}
	})

	t.Run("keys", func(t *testing.T) {
		result := eval(t, env, `(keys (hash-map :a 1 :b 2))`)
		list, ok := result.(core.List)
		if !ok {
			t.Fatalf("expected list, got %T", result)
		}
		if len(list.Items) != 2 {
			t.Errorf("expected 2 keys, got %d", len(list.Items))
		}
	})

	t.Run("vals", func(t *testing.T) {
		result := eval(t, env, `(vals (hash-map :a 1 :b 2))`)
		list, ok := result.(core.List)
		if !ok {
			t.Fatalf("expected list, got %T", result)
		}
		if len(list.Items) != 2 {
			t.Errorf("expected 2 vals, got %d", len(list.Items))
		}
	})
}

func TestHigherOrder_Map(t *testing.T) {
	env := setupEnv(t)

	t.Run("map inc", func(t *testing.T) {
		result := eval(t, env, `(map (fn [x] (+ x 1)) (list 1 2 3))`)
		list, ok := result.(core.List)
		if !ok {
			t.Fatalf("expected list, got %T", result)
		}
		if len(list.Items) != 3 {
			t.Errorf("expected 3 items, got %d", len(list.Items))
		}
		if !list.Items[0].Equals(core.Int{V: 2}) {
			t.Errorf("expected first item to be 2, got %v", list.Items[0])
		}
	})
}

func TestHigherOrder_Filter(t *testing.T) {
	env := setupEnv(t)

	t.Run("filter pos?", func(t *testing.T) {
		result := eval(t, env, `(filter pos? (list -1 2 -3 4))`)
		list, ok := result.(core.List)
		if !ok {
			t.Fatalf("expected list, got %T", result)
		}
		if len(list.Items) != 2 {
			t.Errorf("expected 2 items, got %d", len(list.Items))
		}
	})
}

func TestHigherOrder_Reduce(t *testing.T) {
	env := setupEnv(t)

	t.Run("reduce sum", func(t *testing.T) {
		result := eval(t, env, `(reduce (fn [acc x] (+ acc x)) (list 1 2 3))`)
		if !result.Equals(core.Int{V: 6}) {
			t.Errorf("expected 6, got %v", result)
		}
	})

	t.Run("reduce with init", func(t *testing.T) {
		result := eval(t, env, `(reduce (fn [acc x] (+ acc x)) 10 (list 1 2 3))`)
		if !result.Equals(core.Int{V: 16}) {
			t.Errorf("expected 16, got %v", result)
		}
	})
}

func TestHigherOrder_Apply(t *testing.T) {
	env := setupEnv(t)

	t.Run("apply +", func(t *testing.T) {
		result := eval(t, env, `(apply + (list 1 2 3))`)
		if !result.Equals(core.Int{V: 6}) {
			t.Errorf("expected 6, got %v", result)
		}
	})

	t.Run("apply with extra args", func(t *testing.T) {
		result := eval(t, env, `(apply + 10 (list 1 2 3))`)
		if !result.Equals(core.Int{V: 16}) {
			t.Errorf("expected 16, got %v", result)
		}
	})
}

func TestControl_Assert(t *testing.T) {
	env := setupEnv(t)

	t.Run("assert true", func(t *testing.T) {
		result := eval(t, env, `(assert true)`)
		if _, ok := result.(core.Nil); !ok {
			t.Errorf("expected nil, got %v", result)
		}
	})

	t.Run("assert with message", func(t *testing.T) {
		err := evalErr(t, env, `(assert false "should fail")`)
		if err == nil {
			t.Error("expected error from assert")
		}
	})
}

func TestControl_IfLet(t *testing.T) {
	env := setupEnv(t)

	t.Run("if-let truthy", func(t *testing.T) {
		result := eval(t, env, `(if-let [x 5] (+ x 1) 0)`)
		if !result.Equals(core.Int{V: 6}) {
			t.Errorf("expected 6, got %v", result)
		}
	})

	t.Run("if-let falsy", func(t *testing.T) {
		result := eval(t, env, `(if-let [x nil] (+ x 1) 0)`)
		if !result.Equals(core.Int{V: 0}) {
			t.Errorf("expected 0, got %v", result)
		}
	})
}

func TestControl_WhenLet(t *testing.T) {
	env := setupEnv(t)

	t.Run("when-let truthy", func(t *testing.T) {
		result := eval(t, env, `(when-let [x 5] (+ x 1) (* x 2))`)
		if !result.Equals(core.Int{V: 10}) {
			t.Errorf("expected 10, got %v", result)
		}
	})

	t.Run("when-let falsy", func(t *testing.T) {
		result := eval(t, env, `(when-let [x nil] (+ x 1))`)
		if _, ok := result.(core.Nil); !ok {
			t.Errorf("expected nil, got %v", result)
		}
	})
}

func TestTypes_Predicates(t *testing.T) {
	env := setupEnv(t)

	tests := []struct {
		name     string
		input    string
		expected core.Value
	}{
		{"nil?", `(nil? nil)`, core.Bool{V: true}},
		{"nil? false", `(nil? 1)`, core.Bool{V: false}},
		{"bool?", `(bool? true)`, core.Bool{V: true}},
		{"int?", `(int? 42)`, core.Bool{V: true}},
		{"float?", `(float? 3.14)`, core.Bool{V: true}},
		{"string?", `(string? "hello")`, core.Bool{V: true}},
		{"keyword?", `(keyword? :foo)`, core.Bool{V: true}},
		{"symbol?", `(symbol? (quote foo))`, core.Bool{V: true}},
		{"list?", `(list? (list 1 2 3))`, core.Bool{V: true}},
		{"vector?", `(vector? [1 2 3])`, core.Bool{V: true}},
		{"map?", `(map? (hash-map :a 1))`, core.Bool{V: true}},
		{"fn?", `(fn? +)`, core.Bool{V: true}},
		{"fn? lambda", `(fn? (fn [x] x))`, core.Bool{V: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := eval(t, env, tt.input)
			if !result.Equals(tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestTypes_Conversions(t *testing.T) {
	env := setupEnv(t)

	tests := []struct {
		name     string
		input    string
		expected core.Value
	}{
		{"str->keyword", `(str->keyword "foo")`, core.Keyword{V: "foo"}},
		{"keyword->str", `(keyword->str :foo)`, core.String{V: "foo"}},
		{"int->float", `(int->float 42)`, core.Float{V: 42.0}},
		{"float->int", `(float->int 3.7)`, core.Int{V: 3}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := eval(t, env, tt.input)
			if !result.Equals(tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestBootstrap_ThreadFirst(t *testing.T) {
	env := setupEnv(t)

	result := eval(t, env, `(-> 1 (+ 2) (* 3))`)
	if !result.Equals(core.Int{V: 9}) {
		t.Errorf("expected 9, got %v", result)
	}
}

func TestBootstrap_ThreadLast(t *testing.T) {
	env := setupEnv(t)

	result := eval(t, env, `(->> [1 2 3] (map (fn [x] (+ x 1))) (filter pos?))`)
	list, ok := result.(core.List)
	if !ok {
		t.Fatalf("expected list, got %T", result)
	}
	if len(list.Items) != 3 {
		t.Errorf("expected 3 items, got %d", len(list.Items))
	}
}

func TestBootstrap_GetIn(t *testing.T) {
	env := setupEnv(t)

	result := eval(t, env, `(get-in (hash-map :a (hash-map :b 1)) (list :a :b))`)
	if !result.Equals(core.Int{V: 1}) {
		t.Errorf("expected 1, got %v", result)
	}
}
