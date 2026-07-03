package core

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// evalStr is a test helper: read and evaluate a Lisp source string.
func evalStr(t *testing.T, env *Env, src string) Value {
	t.Helper()
	forms, err := Read(src)
	if err != nil {
		t.Fatalf("Read(%q): %v", src, err)
	}
	e := NewEvaluator()
	var result Value = Nil{}
	for _, form := range forms {
		result, err = e.Eval(context.Background(), form, env)
		if err != nil {
			t.Fatalf("Eval(%q): %v", src, err)
		}
	}
	return result
}

func evalStrErr(env *Env, src string) error {
	forms, err := Read(src)
	if err != nil {
		return err
	}
	e := NewEvaluator()
	for _, form := range forms {
		_, err = e.Eval(context.Background(), form, env)
		if err != nil {
			return err
		}
	}
	return nil
}

func newTestEnv() *Env {
	return NewEnv(nil)
}

func TestEval_SelfEvaluating(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	tests := []struct {
		src  string
		want Value
	}{
		{"42", Int{V: 42}},
		{"-7", Int{V: -7}},
		{"3.14", Float{V: 3.14}},
		{`"hello"`, String{V: "hello"}},
		{":key", Keyword{V: "key"}},
		{"nil", Nil{}},
		{"true", Bool{V: true}},
		{"false", Bool{V: false}},
		{"()", List{}},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			t.Parallel()
			got := evalStr(t, env, tt.src)
			if !got.Equals(tt.want) {
				t.Errorf("Eval(%q) = %v, want %v", tt.src, got, tt.want)
			}
		})
	}
}

func TestEval_SymbolLookup(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	env.Set("x", Int{V: 99})

	got := evalStr(t, env, "x")
	if !got.Equals(Int{V: 99}) {
		t.Errorf("symbol lookup = %v, want 99", got)
	}
}

func TestEval_SymbolUndefined(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	err := evalStrErr(env, "undefined-sym")
	if err == nil {
		t.Error("expected error for undefined symbol")
	}
}

func TestEval_Def(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	evalStr(t, env, "(def x 42)")

	got, ok := env.Get("x")
	if !ok || !got.Equals(Int{V: 42}) {
		t.Errorf("def should bind x to 42, got %v", got)
	}
}

func TestEval_Defn(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	evalStr(t, env, "(defn double [x] x)")

	got, ok := env.Get("double")
	if !ok {
		t.Fatal("defn should bind double")
	}
	if _, ok := got.(Lambda); !ok {
		t.Errorf("defn should bind Lambda, got %T", got)
	}
}

func TestEval_Fn(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	got := evalStr(t, env, "(fn [x] x)")
	if _, ok := got.(Lambda); !ok {
		t.Errorf("fn should return Lambda, got %T", got)
	}
}

func TestEval_FnCall(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	evalStr(t, env, "(def identity (fn [x] x))")
	got := evalStr(t, env, "(identity 42)")
	if !got.Equals(Int{V: 42}) {
		t.Errorf("(identity 42) = %v, want 42", got)
	}
}

func TestEval_If_True(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	got := evalStr(t, env, "(if true 1 2)")
	if !got.Equals(Int{V: 1}) {
		t.Errorf("(if true 1 2) = %v, want 1", got)
	}
}

func TestEval_If_False(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	got := evalStr(t, env, "(if false 1 2)")
	if !got.Equals(Int{V: 2}) {
		t.Errorf("(if false 1 2) = %v, want 2", got)
	}
}

func TestEval_If_Nil(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	got := evalStr(t, env, "(if nil 1 2)")
	if !got.Equals(Int{V: 2}) {
		t.Errorf("(if nil 1 2) = %v, want 2", got)
	}
}

func TestEval_If_Truthy(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	got := evalStr(t, env, `(if 0 "yes" "no")`)
	if !got.Equals(String{V: "yes"}) {
		t.Errorf("(if 0 ...) should be truthy, got %v", got)
	}
}

func TestEval_If_NoElse(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	got := evalStr(t, env, "(if false 1)")
	if !got.Equals(Nil{}) {
		t.Errorf("(if false 1) = %v, want nil", got)
	}
}

func TestEval_Cond(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	env.Set("x", Int{V: 2})
	got := evalStr(t, env, `(cond (false 1) (true 2) (:else 3))`)
	if !got.Equals(Int{V: 2}) {
		t.Errorf("cond = %v, want 2", got)
	}
}

func TestEval_Cond_Else(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	got := evalStr(t, env, `(cond (false 1) (:else 99))`)
	if !got.Equals(Int{V: 99}) {
		t.Errorf("cond else = %v, want 99", got)
	}
}

func TestEval_When_True(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	got := evalStr(t, env, "(when true 42)")
	if !got.Equals(Int{V: 42}) {
		t.Errorf("(when true 42) = %v, want 42", got)
	}
}

func TestEval_When_False(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	got := evalStr(t, env, "(when false 42)")
	if !got.Equals(Nil{}) {
		t.Errorf("(when false 42) = %v, want nil", got)
	}
}

func TestEval_Unless_True(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	got := evalStr(t, env, "(unless true 42)")
	if !got.Equals(Nil{}) {
		t.Errorf("(unless true 42) = %v, want nil", got)
	}
}

func TestEval_Unless_False(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	got := evalStr(t, env, "(unless false 42)")
	if !got.Equals(Int{V: 42}) {
		t.Errorf("(unless false 42) = %v, want 42", got)
	}
}

func TestEval_Let(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	got := evalStr(t, env, "(let [x 1 y 2] y)")
	if !got.Equals(Int{V: 2}) {
		t.Errorf("(let [x 1 y 2] y) = %v, want 2", got)
	}

	// let bindings are evaluated in parent env — parallel, not sequential
	env.Set("a", Int{V: 10})
	got = evalStr(t, env, "(let [a 1 b a] b)") // b should see parent a=10
	if !got.Equals(Int{V: 10}) {
		t.Errorf("let parallel binding: b = %v, want 10 (parent a)", got)
	}
}

func TestEval_LetStar(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	got := evalStr(t, env, "(let* [x 1 y x] y)")
	if !got.Equals(Int{V: 1}) {
		t.Errorf("(let* [x 1 y x] y) = %v, want 1", got)
	}
}

func TestEval_Do(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	got := evalStr(t, env, "(do 1 2 3)")
	if !got.Equals(Int{V: 3}) {
		t.Errorf("(do 1 2 3) = %v, want 3", got)
	}
}

func TestEval_Quote(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	got := evalStr(t, env, "(quote (1 2 3))")
	list, ok := got.(List)
	if !ok || len(list.Items) != 3 {
		t.Errorf("quote returned %v, want list of 3", got)
	}
}

func TestEval_QuoteSyntax(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	got := evalStr(t, env, "'(a b c)")
	list, ok := got.(List)
	if !ok || len(list.Items) != 3 {
		t.Errorf("'(a b c) = %v, want list of 3", got)
	}
}

func TestEval_Set(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	evalStr(t, env, "(def x 1)")
	evalStr(t, env, "(set! x 99)")
	got, _ := env.Get("x")
	if !got.Equals(Int{V: 99}) {
		t.Errorf("after set!, x = %v, want 99", got)
	}
}

func TestEval_Set_Undefined(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	err := evalStrErr(env, "(set! undefined-var 1)")
	if err == nil {
		t.Error("set! on undefined var should error")
	}
}

func TestEval_Defmacro(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	// Use quasiquote to avoid depending on stdlib's `list`
	evalStr(t, env, "(defmacro my-if [c t f] `(if ~c ~t ~f))")

	got := evalStr(t, env, "(my-if true 1 2)")
	if !got.Equals(Int{V: 1}) {
		t.Errorf("macro my-if true = %v, want 1", got)
	}

	got = evalStr(t, env, "(my-if false 1 2)")
	if !got.Equals(Int{V: 2}) {
		t.Errorf("macro my-if false = %v, want 2", got)
	}
}

func TestEval_QuasiquoteMacro(t *testing.T) {
	t.Parallel()
	env := newTestEnv()

	src := `
(defmacro swap [a b]
  ` + "`" + `(let [tmp ~a]
    (do (set! ~a ~b)
        (set! ~b tmp))))
`
	// Just check defmacro registers the macro
	forms, err := Read(src)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	e := NewEvaluator()
	for _, form := range forms {
		if _, err := e.Eval(context.Background(), form, env); err != nil {
			t.Fatalf("Eval error: %v", err)
		}
	}
	_, ok := env.Get("swap")
	if !ok {
		t.Error("swap macro should be registered")
	}
}

func TestEval_Quasiquote(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	env.Set("x", Int{V: 42})
	got := evalStr(t, env, "`(a ~x b)")
	list := got.(List)
	if len(list.Items) != 3 {
		t.Fatalf("quasiquote len = %d, want 3", len(list.Items))
	}
	if !list.Items[1].Equals(Int{V: 42}) {
		t.Errorf("unquoted x = %v, want 42", list.Items[1])
	}
}

func TestEval_QuasiquoteSplice(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	env.Set("xs", List{Items: []Value{Int{V: 1}, Int{V: 2}}})
	got := evalStr(t, env, "`(a ~@xs b)")
	list := got.(List)
	if len(list.Items) != 4 {
		t.Fatalf("splice len = %d, want 4", len(list.Items))
	}
}

func TestEval_Loop_Recur(t *testing.T) {
	t.Parallel()
	env := newTestEnv()

	// sum 1..10 using loop/recur
	src := `(loop [i 1 acc 0]
  (if (> i 10)
    acc
    (recur (+ i 1) (+ acc i))))`

	// We need a builtin + and > for this to work; let's use GoFunc
	env.Set("+", GoFunc{Name: "+", Fn: func(_ context.Context, _ Evaluator, args []Value, _ *Env) (Value, error) {
		sum := int64(0)
		for _, a := range args {
			i, ok := a.(Int)
			if !ok {
				return nil, fmt.Errorf("+ expects Int")
			}
			sum += i.V
		}
		return Int{V: sum}, nil
	}})
	env.Set(">", GoFunc{Name: ">", Fn: func(_ context.Context, _ Evaluator, args []Value, _ *Env) (Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("> expects 2 args")
		}
		a, b := args[0].(Int), args[1].(Int)
		return Bool{V: a.V > b.V}, nil
	}})

	got := evalStr(t, env, src)
	if !got.Equals(Int{V: 55}) {
		t.Errorf("sum 1..10 = %v, want 55", got)
	}
}

func TestEval_Loop_Recur_ArityError(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	src := `(loop [x 1] (recur 1 2))`
	err := evalStrErr(env, src)
	if err == nil {
		t.Error("recur with wrong arity should error")
	}
}

func TestEval_Recur_OutsideLoop(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"top level":  `(recur 1)`,
		"inside fn":  `((fn [] (recur 1)))`,
		"crosses fn": `(loop [] ((fn [] (recur 1))))`,
		"inside do":  `(do (recur 1))`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			err := evalStrErr(newTestEnv(), src)
			if err == nil {
				t.Fatalf("%s: expected error, got nil", src)
			}
			if !strings.Contains(err.Error(), "recur outside loop") {
				t.Fatalf("%s: want 'recur outside loop', got %v", src, err)
			}
		})
	}
}

func TestEval_TCO_DeepRecursion(t *testing.T) {
	t.Parallel()
	env := newTestEnv()

	env.Set("+", GoFunc{Name: "+", Fn: func(_ context.Context, _ Evaluator, args []Value, _ *Env) (Value, error) {
		return Int{V: args[0].(Int).V + args[1].(Int).V}, nil
	}})
	env.Set(">", GoFunc{Name: ">", Fn: func(_ context.Context, _ Evaluator, args []Value, _ *Env) (Value, error) {
		return Bool{V: args[0].(Int).V > args[1].(Int).V}, nil
	}})

	// O(1) stack — 10,000 iterations
	src := `(loop [i 0 acc 0]
  (if (> i 10000)
    acc
    (recur (+ i 1) (+ acc i))))`

	got := evalStr(t, env, src)
	if _, ok := got.(Int); !ok {
		t.Errorf("TCO loop result type %T, want Int", got)
	}
}

func TestEval_TryCatch(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	got := evalStr(t, env, `(try (throw "oops") (catch e e))`)
	if !got.Equals(String{V: "oops"}) {
		t.Errorf("(try (throw \"oops\") (catch e e)) = %v, want \"oops\"", got)
	}
}

func TestEval_TryCatch_NoError(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	got := evalStr(t, env, `(try 42 (catch e e))`)
	if !got.Equals(Int{V: 42}) {
		t.Errorf("try without error = %v, want 42", got)
	}
}

func TestEval_Throw(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	err := evalStrErr(env, `(throw "boom")`)
	if err == nil {
		t.Error("throw should produce an error")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("error message should contain 'boom', got %q", err.Error())
	}
}

func TestEval_Catch_OutsideTry(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	err := evalStrErr(env, `(catch e e)`)
	if err == nil {
		t.Error("catch outside try should error")
	}
}

func TestEval_Keyword_AsFunction(t *testing.T) {
	t.Parallel()
	env := newTestEnv()

	// Set up a HashMap via def
	m := NewHashMap()
	m, _ = m.Assoc(Keyword{V: "name"}, String{V: "alice"})
	env.Set("m", m)

	got := evalStr(t, env, "(:name m)")
	if !got.Equals(String{V: "alice"}) {
		t.Errorf("(:name m) = %v, want \"alice\"", got)
	}
}

func TestEval_Keyword_AsFunction_Missing(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	m := NewHashMap()
	env.Set("m", m)
	got := evalStr(t, env, "(:missing m)")
	if !got.Equals(Nil{}) {
		t.Errorf("(:missing m) = %v, want nil", got)
	}
}

func TestEval_GoFunc(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	env.Set("add", GoFunc{Name: "add", Fn: func(_ context.Context, _ Evaluator, args []Value, _ *Env) (Value, error) {
		return Int{V: args[0].(Int).V + args[1].(Int).V}, nil
	}})

	got := evalStr(t, env, "(add 3 4)")
	if !got.Equals(Int{V: 7}) {
		t.Errorf("(add 3 4) = %v, want 7", got)
	}
}

func TestEval_VariadicFn(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	evalStr(t, env, "(defn variadic [a & rest] rest)")

	forms, _ := Read("(variadic 1 2 3)")
	e := NewEvaluator()
	got, err := e.Eval(context.Background(), forms[0], env)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	l, ok := got.(List)
	if !ok || len(l.Items) != 2 {
		t.Errorf("variadic rest = %v, want list of 2", got)
	}
}

func TestEval_MacroExpand(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	// Use quasiquote — `list` is stdlib, not available in core
	evalStr(t, env, "(defmacro my-and [a b] `(if ~a ~b false))")

	e := NewEvaluator()
	forms, _ := Read("(my-and true false)")
	expanded, err := e.MacroExpand(context.Background(), forms[0], env)
	if err != nil {
		t.Fatalf("MacroExpand error: %v", err)
	}
	// MacroExpand returns the unevaluated expansion: (if true false false)
	list, ok := expanded.(List)
	if !ok {
		t.Fatalf("expanded = %T, want List", expanded)
	}
	if !list.Items[0].Equals(Symbol{V: "if"}) {
		t.Errorf("expanded head = %v, want if", list.Items[0])
	}
}

func TestEval_MaxDepth(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	eng := NewEvaluator()
	eng.MaxDepth = 5

	// Mutual recursion: f calls g calls f indefinitely.
	var gFn GoFunc
	fFn := GoFunc{Name: "f", Fn: func(ctx context.Context, eval Evaluator, args []Value, e *Env) (Value, error) {
		return eval.Apply(ctx, gFn, args, e)
	}}
	gFn = GoFunc{Name: "g", Fn: func(ctx context.Context, eval Evaluator, args []Value, e *Env) (Value, error) {
		return eval.Apply(ctx, fFn, args, e)
	}}
	env.Set("f", fFn)

	forms, _ := Read("(f)")
	_, err := eng.Eval(context.Background(), forms[0], env)
	if err == nil {
		t.Error("expected max depth error")
	}
	if !strings.Contains(err.Error(), "max call depth") {
		t.Errorf("error should mention max call depth, got: %v", err)
	}
}

func TestEval_ContextCancellation(t *testing.T) {
	t.Parallel()
	env := newTestEnv()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	e := NewEvaluator()
	forms, _ := Read("42")
	_, err := e.Eval(ctx, forms[0], env)
	if err == nil {
		t.Error("expected context cancellation error")
	}
}

func TestEval_NotAFunction(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	err := evalStrErr(env, "(42 1 2)")
	if err == nil {
		t.Error("calling a non-function should error")
	}
}

func TestEval_DefErrors(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	if err := evalStrErr(env, "(def)"); err == nil {
		t.Error("def with no args should error")
	}
	if err := evalStrErr(env, "(def x y z)"); err == nil {
		t.Error("def with 3 args should error")
	}
	if err := evalStrErr(env, "(def 42 1)"); err == nil {
		t.Error("def with non-symbol name should error")
	}
}

func TestEval_DefnErrors(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	if err := evalStrErr(env, "(defn)"); err == nil {
		t.Error("defn with no args should error")
	}
	if err := evalStrErr(env, "(defn 42 [] 1)"); err == nil {
		t.Error("defn with non-symbol name should error")
	}
	if err := evalStrErr(env, "(defn f 42 1)"); err == nil {
		t.Error("defn with non-vector params should error")
	}
}

func TestEval_DefmacroErrors(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	if err := evalStrErr(env, "(defmacro)"); err == nil {
		t.Error("defmacro with no args should error")
	}
	if err := evalStrErr(env, "(defmacro 42 [] 1)"); err == nil {
		t.Error("defmacro with non-symbol name should error")
	}
	if err := evalStrErr(env, "(defmacro m 42 1)"); err == nil {
		t.Error("defmacro with non-vector params should error")
	}
}

func TestEval_FnErrors(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	if err := evalStrErr(env, "(fn)"); err == nil {
		t.Error("fn with no args should error")
	}
	if err := evalStrErr(env, "(fn 42 1)"); err == nil {
		t.Error("fn with non-vector params should error")
	}
}

func TestEval_IfErrors(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	if err := evalStrErr(env, "(if)"); err == nil {
		t.Error("if with no args should error")
	}
	if err := evalStrErr(env, "(if true 1 2 3)"); err == nil {
		t.Error("if with 4 args should error")
	}
}

func TestEval_QuoteErrors(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	if err := evalStrErr(env, "(quote)"); err == nil {
		t.Error("quote with no args should error")
	}
	if err := evalStrErr(env, "(quote a b)"); err == nil {
		t.Error("quote with 2 args should error")
	}
}

func TestEval_LetErrors(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	if err := evalStrErr(env, "(let)"); err == nil {
		t.Error("let with no args should error")
	}
	if err := evalStrErr(env, "(let 42 1)"); err == nil {
		t.Error("let with non-vector bindings should error")
	}
	if err := evalStrErr(env, "(let [x] 1)"); err == nil {
		t.Error("let with odd binding count should error")
	}
}

func TestEval_LetStarErrors(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	if err := evalStrErr(env, "(let*)"); err == nil {
		t.Error("let* with no args should error")
	}
	if err := evalStrErr(env, "(let* 42 1)"); err == nil {
		t.Error("let* with non-vector bindings should error")
	}
}

func TestEval_WhenErrors(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	if err := evalStrErr(env, "(when)"); err == nil {
		t.Error("when with no args should error")
	}
}

func TestEval_UnlessErrors(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	if err := evalStrErr(env, "(unless)"); err == nil {
		t.Error("unless with no args should error")
	}
}

func TestEval_ThrowErrors(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	if err := evalStrErr(env, "(throw)"); err == nil {
		t.Error("throw with no args should error")
	}
	err := evalStrErr(env, "(throw 42)")
	if err == nil {
		t.Fatal("throw should propagate error")
	}
	if err.Error() != "42" {
		t.Errorf("throw 42 error = %q, want \"42\"", err.Error())
	}
}

func TestEval_SetErrors(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	if err := evalStrErr(env, "(set! x 1 2)"); err == nil {
		t.Error("set! with wrong arity should error")
	}
	if err := evalStrErr(env, "(set! 42 1)"); err == nil {
		t.Error("set! with non-symbol should error")
	}
}

func TestEval_CondErrors(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	if err := evalStrErr(env, "(cond (1))"); err == nil {
		t.Error("cond with 1-element clause should error")
	}
	if err := evalStrErr(env, "(cond 42)"); err == nil {
		t.Error("cond with non-list clause should error")
	}
}

func TestEval_TryErrors(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	if err := evalStrErr(env, "(try)"); err == nil {
		t.Error("try with no args should error")
	}
	if err := evalStrErr(env, "(try 1 2)"); err == nil {
		t.Error("try with non-catch last clause should error")
	}
}

func TestEval_Quasiquote_Vector(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	env.Set("x", Int{V: 5})

	got := evalStr(t, env, "`[1 ~x 3]")
	vec, ok := got.(Vector)
	if !ok || len(vec.Items) != 3 {
		t.Errorf("quasiquote vector = %v, want [1 5 3]", got)
		return
	}
	if !vec.Items[1].Equals(Int{V: 5}) {
		t.Errorf("vec[1] = %v, want 5", vec.Items[1])
	}
}

func TestEval_Quasiquote_Atom(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	got := evalStr(t, env, "`42")
	if !got.Equals(Int{V: 42}) {
		t.Errorf("quasiquote atom = %v, want 42", got)
	}
}

func TestEval_QuasiquoteSplice_Vector(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	env.Set("xs", Vector{Items: []Value{Int{V: 1}, Int{V: 2}}})

	got := evalStr(t, env, "`(0 ~@xs 3)")
	list, ok := got.(List)
	if !ok || len(list.Items) != 4 {
		t.Errorf("quasiquote splice vector = %v, want 4 items", got)
	}
}

func TestEval_MacroExpand_NonMacro(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	env.Set("x", Int{V: 10})
	e := NewEvaluator()

	form, _ := ReadOne("x")
	expanded, err := e.MacroExpand(context.Background(), form, env)
	if err != nil {
		t.Fatalf("MacroExpand error: %v", err)
	}
	if !expanded.Equals(Symbol{V: "x"}) {
		t.Errorf("MacroExpand(symbol) = %v, want x", expanded)
	}
}

func TestEval_QuasiquoteErrors(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	e := NewEvaluator()

	// Directly eval a (quasiquote) list with wrong arity.
	form, _ := ReadOne("(quasiquote)")
	_, err := e.Eval(context.Background(), form, env)
	if err == nil {
		t.Error("quasiquote with no args should error")
	}

	// Malformed unquote inside quasiquote: (quasiquote (unquote)) — unquote with 0 args.
	// The reader never produces this, so we build the form directly.
	malformed := List{Items: []Value{
		Symbol{V: "quasiquote"},
		List{Items: []Value{Symbol{V: "unquote"}}},
	}}
	_, err2 := e.Eval(context.Background(), malformed, env)
	if err2 == nil {
		t.Error("malformed unquote inside quasiquote should error")
	}
}

func TestEval_LetErrors_BindingName(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	if err := evalStrErr(env, "(let [42 1] 42)"); err == nil {
		t.Error("let binding name must be a symbol")
	}
	if err := evalStrErr(env, "(let* [42 1] 42)"); err == nil {
		t.Error("let* binding name must be a symbol")
	}
}

func TestEval_Keyword_WrongArity(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	env.Set("m", func() *HashMap {
		m, _ := NewHashMap().Assoc(Keyword{V: "a"}, Int{V: 1})
		return m
	}())
	if err := evalStrErr(env, "(:a m m)"); err == nil {
		t.Error("keyword lookup with 2 args should error")
	}
}

func TestEval_Keyword_NonHashMap(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	env.Set("s", String{V: "hello"})
	got := evalStr(t, env, "(:a s)")
	if !got.Equals(Nil{}) {
		t.Errorf("keyword on non-HashMap = %v, want nil", got)
	}
}

func TestEval_MacroExpand_UndefinedHead(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	e := NewEvaluator()
	form, _ := ReadOne("(undefined-sym 1 2)")
	// MacroExpand returns form unchanged when head eval errors
	expanded, err := e.MacroExpand(context.Background(), form, env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !expanded.Equals(form) {
		t.Errorf("expected form unchanged, got %v", expanded)
	}
}

func TestEval_MacroExpand_DepthExceeded(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	evalStr(t, env, "(defmacro once [] 42)")
	e := NewEvaluator()
	e.maxMacroDepth = 0 // 0 means any expansion exceeds the limit
	form, _ := ReadOne("(once)")
	_, err := e.MacroExpand(context.Background(), form, env)
	if err == nil {
		t.Error("expected macro depth exceeded error")
	}
}

func TestEval_SetEvaluator(t *testing.T) {
	t.Parallel()
	env := NewEnv(nil)
	e := NewEvaluator()
	env.SetEvaluator(e)
	if env.Evaluator() != e {
		t.Error("SetEvaluator should update the evaluator")
	}
}

func TestTailCall_ValueInterface(t *testing.T) {
	t.Parallel()
	tc := tailCall{}
	if tc.Type().V != "tail-call" {
		t.Errorf("tailCall Type() = %q, want tail-call", tc.Type().V)
	}
	if tc.String() != "#<tail-call>" {
		t.Errorf("tailCall String() = %q, want #<tail-call>", tc.String())
	}
	if tc.Equals(tailCall{}) {
		t.Error("tailCall.Equals should always return false")
	}

	rv := recurVal{}
	if rv.Type().V != "recur" {
		t.Errorf("recurVal Type() = %q, want recur", rv.Type().V)
	}
	if rv.String() != "#<recur>" {
		t.Errorf("recurVal String() = %q, want #<recur>", rv.String())
	}
	if rv.Equals(recurVal{}) {
		t.Error("recurVal.Equals should always return false")
	}
}

func TestEval_And_True(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	got := evalStr(t, env, "(and true true true)")
	if !got.Equals(Bool{V: true}) {
		t.Errorf("(and true true true) = %v, want true", got)
	}
}

func TestEval_And_False(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	got := evalStr(t, env, "(and true false true)")
	if !got.Equals(Bool{V: false}) {
		t.Errorf("(and true false true) = %v, want false", got)
	}
}

func TestEval_And_Empty(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	got := evalStr(t, env, "(and)")
	if !got.Equals(Bool{V: true}) {
		t.Errorf("(and) = %v, want true", got)
	}
}

func TestEval_And_ShortCircuit(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	got := evalStr(t, env, `(and false (throw "should not evaluate"))`)
	if !got.Equals(Bool{V: false}) {
		t.Errorf("(and false ...) should short-circuit, got %v", got)
	}
}

func TestEval_Or_True(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	got := evalStr(t, env, "(or false true false)")
	if !got.Equals(Bool{V: true}) {
		t.Errorf("(or false true false) = %v, want true", got)
	}
}

func TestEval_Or_False(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	got := evalStr(t, env, "(or false false false)")
	if !got.Equals(Bool{V: false}) {
		t.Errorf("(or false false false) = %v, want false", got)
	}
}

func TestEval_Or_Empty(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	got := evalStr(t, env, "(or)")
	if !got.Equals(Nil{}) {
		t.Errorf("(or) = %v, want nil", got)
	}
}

func TestEval_Or_ShortCircuit(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	got := evalStr(t, env, `(or true (throw "should not evaluate"))`)
	if !got.Equals(Bool{V: true}) {
		t.Errorf("(or true ...) should short-circuit, got %v", got)
	}
}

func TestEval_Not_True(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	got := evalStr(t, env, "(not true)")
	if !got.Equals(Bool{V: false}) {
		t.Errorf("(not true) = %v, want false", got)
	}
}

func TestEval_Not_False(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	got := evalStr(t, env, "(not false)")
	if !got.Equals(Bool{V: true}) {
		t.Errorf("(not false) = %v, want true", got)
	}
}

func TestEval_Not_Nil(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	got := evalStr(t, env, "(not nil)")
	if !got.Equals(Bool{V: true}) {
		t.Errorf("(not nil) = %v, want true", got)
	}
}

func TestEval_Not_Truthy(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	got := evalStr(t, env, "(not 42)")
	if !got.Equals(Bool{V: false}) {
		t.Errorf("(not 42) = %v, want false (42 is truthy)", got)
	}
}

func TestEval_Not_ArityError(t *testing.T) {
	t.Parallel()
	env := newTestEnv()
	if err := evalStrErr(env, "(not)"); err == nil {
		t.Error("(not) with no args should error")
	}
	if err := evalStrErr(env, "(not true false)"); err == nil {
		t.Error("(not true false) with 2 args should error")
	}
}
