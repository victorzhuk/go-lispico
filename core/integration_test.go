package core

import (
	"context"
	"sync"
	"testing"
)

// newCoreEnv returns an env with minimal built-in functions for integration tests.
func newCoreEnv() *Env {
	env := NewEnv(nil)

	env.Set("+", GoFunc{Name: "+", Fn: func(_ context.Context, _ Evaluator, args []Value, _ *Env) (Value, error) {
		var sum int64
		for _, a := range args {
			sum += a.(Int).V
		}
		return Int{V: sum}, nil
	}})
	env.Set("-", GoFunc{Name: "-", Fn: func(_ context.Context, _ Evaluator, args []Value, _ *Env) (Value, error) {
		if len(args) == 0 {
			return Int{V: 0}, nil
		}
		result := args[0].(Int).V
		for _, a := range args[1:] {
			result -= a.(Int).V
		}
		return Int{V: result}, nil
	}})
	env.Set("*", GoFunc{Name: "*", Fn: func(_ context.Context, _ Evaluator, args []Value, _ *Env) (Value, error) {
		result := int64(1)
		for _, a := range args {
			result *= a.(Int).V
		}
		return Int{V: result}, nil
	}})
	env.Set("=", GoFunc{Name: "=", Fn: func(_ context.Context, _ Evaluator, args []Value, _ *Env) (Value, error) {
		if len(args) < 2 {
			return Bool{V: true}, nil
		}
		for i := 1; i < len(args); i++ {
			if !args[i].Equals(args[0]) {
				return Bool{V: false}, nil
			}
		}
		return Bool{V: true}, nil
	}})
	env.Set("<", GoFunc{Name: "<", Fn: func(_ context.Context, _ Evaluator, args []Value, _ *Env) (Value, error) {
		return Bool{V: args[0].(Int).V < args[1].(Int).V}, nil
	}})
	env.Set(">", GoFunc{Name: ">", Fn: func(_ context.Context, _ Evaluator, args []Value, _ *Env) (Value, error) {
		return Bool{V: args[0].(Int).V > args[1].(Int).V}, nil
	}})
	env.Set("str", GoFunc{Name: "str", Fn: func(_ context.Context, _ Evaluator, args []Value, _ *Env) (Value, error) {
		var s string
		for _, a := range args {
			if sv, ok := a.(String); ok {
				s += sv.V
			} else {
				s += a.String()
			}
		}
		return String{V: s}, nil
	}})

	return env
}

func evalAll(t testing.TB, env *Env, src string) Value {
	t.Helper()
	forms, err := Read(src)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	e := NewEvaluator()
	var result Value = Nil{}
	for _, form := range forms {
		result, err = e.Eval(context.Background(), form, env)
		if err != nil {
			t.Fatalf("Eval error: %v", err)
		}
	}
	return result
}

func TestIntegration_Fibonacci(t *testing.T) {
	t.Parallel()
	env := newCoreEnv()

	src := `
(defn fib [n]
  (if (< n 2)
    n
    (+ (fib (- n 1)) (fib (- n 2)))))
(fib 10)`

	got := evalAll(t, env, src)
	if !got.Equals(Int{V: 55}) {
		t.Errorf("fib(10) = %v, want 55", got)
	}
}

func TestIntegration_Factorial_Loop(t *testing.T) {
	t.Parallel()
	env := newCoreEnv()

	src := `
(defn factorial [n]
  (loop [i n acc 1]
    (if (= i 0)
      acc
      (recur (- i 1) (* acc i)))))
(factorial 10)`

	got := evalAll(t, env, src)
	if !got.Equals(Int{V: 3628800}) {
		t.Errorf("factorial(10) = %v, want 3628800", got)
	}
}

func TestIntegration_MacroComposition(t *testing.T) {
	t.Parallel()
	env := newCoreEnv()

	src := `
(defmacro my-when [cond & body]
  ` + "`" + `(if ~cond (do ~@body) nil))

(def result nil)
(my-when true
  (def result 42))
result`

	got := evalAll(t, env, src)
	if !got.Equals(Int{V: 42}) {
		t.Errorf("macro my-when result = %v, want 42", got)
	}
}

func TestIntegration_Closure(t *testing.T) {
	t.Parallel()
	env := newCoreEnv()

	src := `
(defn make-adder [n]
  (fn [x] (+ x n)))

(def add5 (make-adder 5))
(add5 10)`

	got := evalAll(t, env, src)
	if !got.Equals(Int{V: 15}) {
		t.Errorf("make-adder closure = %v, want 15", got)
	}
}

func TestIntegration_LetStar_Sequential(t *testing.T) {
	t.Parallel()
	env := newCoreEnv()

	src := `(let* [x 10 y (* x 2) z (+ y 5)] z)`
	got := evalAll(t, env, src)
	if !got.Equals(Int{V: 25}) {
		t.Errorf("let* = %v, want 25", got)
	}
}

func TestIntegration_HashMap_Operations(t *testing.T) {
	t.Parallel()
	env := newCoreEnv()

	src := `
(def m {:a 1 :b 2})
(:a m)`

	got := evalAll(t, env, src)
	if !got.Equals(Int{V: 1}) {
		t.Errorf("HashMap :a = %v, want 1", got)
	}
}

func TestIntegration_TryCatch_ErrorPropagation(t *testing.T) {
	t.Parallel()
	env := newCoreEnv()

	src := `
(defn safe-divide [a b]
  (try
    (if (= b 0)
      (throw "division by zero")
      a)
    (catch err (str "caught: " err))))

(safe-divide 10 0)`

	got := evalAll(t, env, src)
	s, ok := got.(String)
	if !ok || s.V != "caught: division by zero" {
		t.Errorf("safe-divide = %v, want \"caught: division by zero\"", got)
	}
}

func TestIntegration_Quasiquote_Nested(t *testing.T) {
	t.Parallel()
	env := newCoreEnv()

	src := `
(def xs '(1 2 3))
` + "`" + `(a ~@xs b)`

	got := evalAll(t, env, src)
	list, ok := got.(List)
	if !ok || len(list.Items) != 5 {
		t.Errorf("quasiquote splice = %v, want 5-element list", got)
	}
}

func TestIntegration_RoundTrip_ParsePrint(t *testing.T) {
	t.Parallel()

	inputs := []string{
		"42",
		`"hello world"`,
		":keyword",
		"(+ 1 2)",
		"[1 2 3]",
		"nil",
		"true",
		"false",
	}

	for _, src := range inputs {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			v, err := ReadOne(src)
			if err != nil {
				t.Fatalf("ReadOne(%q): %v", src, err)
			}

			// Parse the String() representation back and verify equality
			v2, err := ReadOne(v.String())
			if err != nil {
				t.Fatalf("ReadOne(String()): %v for %q (printed as %q)", err, src, v.String())
			}
			if !v.Equals(v2) {
				t.Errorf("round-trip: %q → %q → not equal", src, v.String())
			}
		})
	}
}

func TestIntegration_Determinism(t *testing.T) {
	t.Parallel()
	src := `(let* [x 10 y 20] (+ x y))`
	env := newCoreEnv()
	e := NewEvaluator()
	forms, _ := Read(src)

	results := make([]Value, 10)
	for i := range results {
		r, err := e.Eval(context.Background(), forms[0], env)
		if err != nil {
			t.Fatalf("eval %d: %v", i, err)
		}
		results[i] = r
	}
	for i := 1; i < len(results); i++ {
		if !results[i].Equals(results[0]) {
			t.Errorf("non-deterministic: run 0=%v run %d=%v", results[0], i, results[i])
		}
	}
}

func TestIntegration_Immutability(t *testing.T) {
	t.Parallel()
	env := newCoreEnv()

	// Assoc should not mutate original HashMap
	m := NewHashMap()
	m2, err := m.Assoc(Keyword{V: "x"}, Int{V: 1})
	if err != nil {
		t.Fatal(err)
	}
	if m.Len() != 0 {
		t.Error("original map should be unchanged after Assoc")
	}
	if m2.Len() != 1 {
		t.Error("new map should have 1 entry")
	}
	_ = env
}

func TestIntegration_ConcurrentEval(t *testing.T) {
	t.Parallel()
	env := newCoreEnv()
	evalAll(t, env, "(defn add [x y] (+ x y))")

	e := NewEvaluator()
	forms, _ := Read("(add 3 4)")

	var wg sync.WaitGroup
	errs := make(chan error, 20)

	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := e.Eval(context.Background(), forms[0], env)
			if err != nil {
				errs <- err
				return
			}
			if !result.Equals(Int{V: 7}) {
				errs <- nil // signal wrong result
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Errorf("concurrent eval error: %v", err)
		} else {
			t.Error("concurrent eval returned wrong result")
		}
	}
}

func TestIntegration_Plugin_Registration(t *testing.T) {
	t.Parallel()
	env := newCoreEnv()
	reg := NewRegistry()

	plug := &stubPlugin{
		name: "math",
		meta: PluginMeta{Version: "1.0.0"},
	}
	if err := reg.Register(plug); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := plug.Init(env); err != nil {
		t.Fatalf("Init: %v", err)
	}

	ns := reg.Namespaces()
	if len(ns) != 1 || ns[0] != "math" {
		t.Errorf("namespaces = %v, want [math]", ns)
	}
}

func TestIntegration_Cond_Full(t *testing.T) {
	t.Parallel()
	env := newCoreEnv()

	src := `
(defn classify [n]
  (cond
    ((< n 0) "negative")
    ((= n 0) "zero")
    (:else   "positive")))
(str (classify -1) " " (classify 0) " " (classify 1))`

	got := evalAll(t, env, src)
	if !got.Equals(String{V: "negative zero positive"}) {
		t.Errorf("classify = %v, want \"negative zero positive\"", got)
	}
}
