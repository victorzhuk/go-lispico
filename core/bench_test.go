package core

import (
	"context"
	"testing"
)

func BenchmarkEval_SimpleArith(b *testing.B) {
	b.ReportAllocs()
	env := newCoreEnv()
	e := NewEvaluator()
	forms, _ := Read("(+ 1 2)")
	b.ResetTimer()
	for range b.N {
		e.Eval(context.Background(), forms[0], env)
	}
}

func BenchmarkEval_Fibonacci(b *testing.B) {
	b.ReportAllocs()
	env := newCoreEnv()
	evalAll(b, env, `(defn fib [n] (if (< n 2) n (+ (fib (- n 1)) (fib (- n 2)))))`)
	e := NewEvaluator()
	forms, _ := Read("(fib 10)")
	b.ResetTimer()
	for range b.N {
		e.Eval(context.Background(), forms[0], env)
	}
}

func BenchmarkEval_FactorialLoop(b *testing.B) {
	b.ReportAllocs()
	env := newCoreEnv()
	evalAll(b, env, `(defn factorial [n] (loop [i n acc 1] (if (= i 0) acc (recur (- i 1) (* acc i)))))`)
	e := NewEvaluator()
	forms, _ := Read("(factorial 20)")
	b.ResetTimer()
	for range b.N {
		e.Eval(context.Background(), forms[0], env)
	}
}

func BenchmarkEval_MacroExpand(b *testing.B) {
	b.ReportAllocs()
	env := newCoreEnv()
	evalAll(b, env, "(defmacro my-when [cond & body] `(if ~cond (do ~@body) nil))")
	e := NewEvaluator()
	forms, _ := Read("(my-when true 42)")
	b.ResetTimer()
	for range b.N {
		e.Eval(context.Background(), forms[0], env)
	}
}

func BenchmarkRead_Simple(b *testing.B) {
	b.ReportAllocs()
	src := "(+ 1 (* 2 3))"
	b.ResetTimer()
	for range b.N {
		Read(src)
	}
}

func BenchmarkHashMap_Assoc(b *testing.B) {
	b.ReportAllocs()
	m := NewHashMap()
	key := Keyword{V: "x"}
	val := Int{V: 42}
	b.ResetTimer()
	for range b.N {
		m.Assoc(key, val)
	}
}
