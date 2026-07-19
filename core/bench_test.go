package core

import (
	"context"
	"fmt"
	"sort"
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

// BenchmarkHashMap_ScanVsMap freezes hashMapSmallLimit: it compares a linear
// scan over sorted entries against a native Go map lookup at the small-form
// boundary sizes, independent of HashMap's own auto-promotion at 9 keys.
func BenchmarkHashMap_ScanVsMap(b *testing.B) {
	for _, n := range []int{4, 8, 16} {
		entries := make([]entry, n)
		mapForm := make(map[hashKey]entry, n)
		for i := range n {
			k := Keyword{V: fmt.Sprintf("key%02d", i)}
			hk, err := toHashKey(k)
			if err != nil {
				b.Fatal(err)
			}
			e := entry{hk: hk, k: k, v: Int{V: int64(i)}}
			entries[i] = e
			mapForm[hk] = e
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].hk.less(entries[j].hk) })
		target := entries[n-1].hk
		scanForm := &HashMap{entries: entries}

		b.Run(fmt.Sprintf("scan/n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				scanForm.find(target)
			}
		})
		b.Run(fmt.Sprintf("map/n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				_ = mapForm[target]
			}
		})
	}
}
