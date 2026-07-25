package core

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// buildLetSource returns `(let [v0 0 v1 1 ... v(n-1) (n-1)] (+ v0 v1 ...))` —
// a let binding n symbols, straddling the persistent vector's future
// promotion boundary at n=32.
func buildLetSource(n int) string {
	var pairs, sum strings.Builder
	for i := range n {
		if i > 0 {
			sum.WriteByte(' ')
		}
		fmt.Fprintf(&pairs, "v%d %d ", i, i)
		fmt.Fprintf(&sum, "v%d", i)
	}
	return fmt.Sprintf("(let [%s] (+ %s))", strings.TrimSpace(pairs.String()), sum.String())
}

// buildFnDefSource and buildFnCallSource split a fn definition with n params
// from its call site, so the def cost stays out of the timed loop.
func buildFnDefSource(n int) string {
	var params, sum strings.Builder
	for i := range n {
		if i > 0 {
			sum.WriteByte(' ')
		}
		fmt.Fprintf(&params, "p%d ", i)
		fmt.Fprintf(&sum, "p%d", i)
	}
	return fmt.Sprintf("(defn addN [%s] (+ %s))", strings.TrimSpace(params.String()), sum.String())
}

func buildFnCallSource(n int) string {
	var args strings.Builder
	for i := range n {
		if i > 0 {
			args.WriteByte(' ')
		}
		fmt.Fprintf(&args, "%d", i)
	}
	return fmt.Sprintf("(addN %s)", args.String())
}

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

// BenchmarkEval_LetSmall, _LetWide16, and _LetWide40 cover the binding-carrier
// regression risk: `let` binds through a Vector, and a small vector (the
// overwhelmingly common shape) must not regress against wide ones straddling
// the future promotion threshold at 32.
func BenchmarkEval_LetSmall(b *testing.B) {
	b.ReportAllocs()
	env := newCoreEnv()
	e := NewEvaluator()
	forms, _ := Read(buildLetSource(4))
	b.ResetTimer()
	for range b.N {
		e.Eval(context.Background(), forms[0], env)
	}
}

func BenchmarkEval_LetWide16(b *testing.B) {
	b.ReportAllocs()
	env := newCoreEnv()
	e := NewEvaluator()
	forms, _ := Read(buildLetSource(16))
	b.ResetTimer()
	for range b.N {
		e.Eval(context.Background(), forms[0], env)
	}
}

func BenchmarkEval_LetWide40(b *testing.B) {
	b.ReportAllocs()
	env := newCoreEnv()
	e := NewEvaluator()
	forms, _ := Read(buildLetSource(40))
	b.ResetTimer()
	for range b.N {
		e.Eval(context.Background(), forms[0], env)
	}
}

// BenchmarkEval_FnCallSmall, _FnCallWide16, and _FnCallWide40 cover the same
// carrier risk for fn parameter lists.
func BenchmarkEval_FnCallSmall(b *testing.B) {
	b.ReportAllocs()
	env := newCoreEnv()
	evalAll(b, env, buildFnDefSource(4))
	e := NewEvaluator()
	forms, _ := Read(buildFnCallSource(4))
	b.ResetTimer()
	for range b.N {
		e.Eval(context.Background(), forms[0], env)
	}
}

func BenchmarkEval_FnCallWide16(b *testing.B) {
	b.ReportAllocs()
	env := newCoreEnv()
	evalAll(b, env, buildFnDefSource(16))
	e := NewEvaluator()
	forms, _ := Read(buildFnCallSource(16))
	b.ResetTimer()
	for range b.N {
		e.Eval(context.Background(), forms[0], env)
	}
}

func BenchmarkEval_FnCallWide40(b *testing.B) {
	b.ReportAllocs()
	env := newCoreEnv()
	evalAll(b, env, buildFnDefSource(40))
	e := NewEvaluator()
	forms, _ := Read(buildFnCallSource(40))
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

// BenchmarkRead_SmallListLiteral and BenchmarkRead_SmallVectorLiteral cover
// the hot path: small literal collections as they come off the reader,
// before any interpreter work touches them.
func BenchmarkRead_SmallListLiteral(b *testing.B) {
	b.ReportAllocs()
	src := "(1 2 3)"
	b.ResetTimer()
	for range b.N {
		Read(src)
	}
}

func BenchmarkRead_SmallVectorLiteral(b *testing.B) {
	b.ReportAllocs()
	src := "[1 2 3]"
	b.ResetTimer()
	for range b.N {
		Read(src)
	}
}

// BenchmarkRead_Representative reads a small multi-form program mixing
// defn, let, and list/vector literals — a realistic reader workload rather
// than one isolated expression.
func BenchmarkRead_Representative(b *testing.B) {
	b.ReportAllocs()
	src := `
(defn fib [n]
  (if (< n 2)
    n
    (+ (fib (- n 1)) (fib (- n 2)))))
(def data [1 2 3 4 5 6 7 8 9 10])
(def items (list 1 2 3 4 5))
(let [x 1 y 2] (+ x y))
`
	b.ResetTimer()
	for range b.N {
		Read(src)
	}
}

// BenchmarkVectorAt covers index reads at sizes straddling the persistent
// vector's future node-width promotion boundary.
func BenchmarkVectorAt(b *testing.B) {
	for _, n := range []int{32, 1000, 100000} {
		items := make([]Value, n)
		for i := range items {
			items[i] = Int{V: int64(i)}
		}
		vec := NewVector(items)
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := range b.N {
				_ = vec.At(i % n)
			}
		})
	}
}

// BenchmarkListCons_Accumulate builds an N-element list one Cons at a time —
// the quadratic-copy shape this change exists to fix.
func BenchmarkListCons_Accumulate(b *testing.B) {
	for _, n := range []int{100, 1000} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				lst := List{}
				for j := range n {
					lst, _ = lst.Cons(Int{V: int64(j)})
				}
			}
		})
	}
}

// BenchmarkVectorConj_AccumulateFromLargeFlat conj's onto a 100000-element
// vector one element at a time. NewVector always returns a flat backing, so
// the first Conj call promotes it to a trie; every call after that is a
// normal trie Conj. If the promotion re-ran on every call instead of once,
// this would be O(n) per Conj instead of amortized O(1) after the first.
func BenchmarkVectorConj_AccumulateFromLargeFlat(b *testing.B) {
	items := make([]Value, 100000)
	for i := range items {
		items[i] = Int{V: int64(i)}
	}
	base := NewVector(items)
	b.ReportAllocs()
	for range b.N {
		v := base
		for j := range 1000 {
			v, _ = v.Conj(Int{V: int64(j)})
		}
		_ = v
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
