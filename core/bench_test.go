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

// buildFlatListSource and buildFlatVectorSource return a single list/vector
// literal of n integers — a wide-collection shape the small literal
// benchmarks below don't cover.
func buildFlatListSource(n int) string {
	var items strings.Builder
	items.WriteByte('(')
	for i := range n {
		if i > 0 {
			items.WriteByte(' ')
		}
		fmt.Fprintf(&items, "%d", i)
	}
	items.WriteByte(')')
	return items.String()
}

func buildFlatVectorSource(n int) string {
	var items strings.Builder
	items.WriteByte('[')
	for i := range n {
		if i > 0 {
			items.WriteByte(' ')
		}
		fmt.Fprintf(&items, "%d", i)
	}
	items.WriteByte(']')
	return items.String()
}

// buildEscapeHeavySource returns a list of n string literals, each carrying
// multiple escape sequences — the shape where a naive Tokenize pre-count
// pass would decode every string twice instead of just scanning past it.
func buildEscapeHeavySource(n int) string {
	var items strings.Builder
	items.WriteByte('(')
	for i := range n {
		if i > 0 {
			items.WriteByte(' ')
		}
		items.WriteString(`"line one\nline two\ttab"`)
	}
	items.WriteByte(')')
	return items.String()
}

// buildCommentDominatedSource returns n comment lines followed by one real
// form — nearly every scanned byte is comment, not token, so both Tokenize
// passes redo the same comment-skipping work for it.
func buildCommentDominatedSource(n int) string {
	var b strings.Builder
	for range n {
		b.WriteString("; a representative comment line, long enough to matter\n")
	}
	b.WriteString("(+ 1 2)")
	return b.String()
}

// buildLargeStringLiteralSource returns a single n-byte escape-free string
// literal — readString's zero-copy fast path engages on both Tokenize
// passes, so the count pass still walks all n bytes for no allocation gain.
func buildLargeStringLiteralSource(n int) string {
	return `"` + strings.Repeat("x", n) + `"`
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

// representativeSource is the shared program behind BenchmarkRead_Representative,
// BenchmarkTokenize_Representative, TestReaderStats_Bench, and
// TestTokenize_CountMatchesLen's Read_Representative case — one definition so
// editing it can't silently desync the stats pin from what it pins.
const representativeSource = `
(defn fib [n]
  (if (< n 2)
    n
    (+ (fib (- n 1)) (fib (- n 2)))))
(def data [1 2 3 4 5 6 7 8 9 10])
(def items (list 1 2 3 4 5))
(let [x 1 y 2] (+ x y))
`

// BenchmarkRead_Representative reads a small multi-form program mixing
// defn, let, and list/vector literals — a realistic reader workload rather
// than one isolated expression.
func BenchmarkRead_Representative(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		Read(representativeSource)
	}
}

// BenchmarkRead_WithFunctionRef and BenchmarkRead_WithReaderVector cover the
// non-default reader flags — every BenchmarkRead_* above runs under
// defaultReaderFlags only.
func BenchmarkRead_WithFunctionRef(b *testing.B) {
	b.ReportAllocs()
	src := "#'my-fn"
	d := FullDialect().WithFunctionRef()
	b.ResetTimer()
	for range b.N {
		d.Read(src)
	}
}

func BenchmarkRead_WithReaderVector(b *testing.B) {
	b.ReportAllocs()
	src := "#(1 2 3)"
	d := FullDialect().WithReaderVector()
	b.ResetTimer()
	for range b.N {
		d.Read(src)
	}
}

// BenchmarkRead_BracketLiteralsRejected measures the WithoutBracketLiterals
// branch itself: with the flag off, a bracket character has no fallback
// meaning — it is always a hard parse error — so an error return is the only
// way this branch is ever reached.
func BenchmarkRead_BracketLiteralsRejected(b *testing.B) {
	b.ReportAllocs()
	src := "(f [1 2])"
	d := FullDialect().WithoutBracketLiterals()
	b.ResetTimer()
	for range b.N {
		d.Read(src)
	}
}

// BenchmarkRead_DeepNesting covers parser recursion depth, well under
// defaultReaderDepth so it measures the parse cost, not the depth-limit error.
func BenchmarkRead_DeepNesting(b *testing.B) {
	b.ReportAllocs()
	src := strings.Repeat("(", 200) + "1" + strings.Repeat(")", 200)
	b.ResetTimer()
	for range b.N {
		Read(src)
	}
}

// BenchmarkRead_LargeFlatList and BenchmarkRead_LargeFlatVector cover a wide
// collection literal, in contrast to BenchmarkRead_Small{List,Vector}Literal's
// 3-element case.
func BenchmarkRead_LargeFlatList(b *testing.B) {
	b.ReportAllocs()
	src := buildFlatListSource(1000)
	b.ResetTimer()
	for range b.N {
		Read(src)
	}
}

func BenchmarkRead_LargeFlatVector(b *testing.B) {
	b.ReportAllocs()
	src := buildFlatVectorSource(1000)
	b.ResetTimer()
	for range b.N {
		Read(src)
	}
}

// BenchmarkTokenize_Representative isolates Tokenize from Parse; every
// benchmark above measures Read, which runs both.
func BenchmarkTokenize_Representative(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		r := NewReader(representativeSource)
		r.Tokenize()
	}
}

// BenchmarkRead_EscapeHeavyStrings covers a source dense with escaped string
// literals — the Tokenize pre-count pass must scan past each escape without
// decoding it a second time.
func BenchmarkRead_EscapeHeavyStrings(b *testing.B) {
	b.ReportAllocs()
	src := buildEscapeHeavySource(40)
	b.ResetTimer()
	for range b.N {
		Read(src)
	}
}

// BenchmarkRead_CommentDominated and BenchmarkRead_LargeStringLiteral cover
// two shapes where the two-pass count/emit scan doubles CPU work for near-
// zero allocation benefit: a source that is nearly all comment, and one
// large escape-free string literal. Not a regression to fix — the trade-off
// the two-pass design makes, put where a benchmark can see it instead of
// staying invisible to the corpus like the escape-decode double-buffer did.
func BenchmarkRead_CommentDominated(b *testing.B) {
	b.ReportAllocs()
	src := buildCommentDominatedSource(200)
	b.ResetTimer()
	for range b.N {
		Read(src)
	}
}

func BenchmarkRead_LargeStringLiteral(b *testing.B) {
	b.ReportAllocs()
	src := buildLargeStringLiteralSource(100_000)
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

// BenchmarkHashMap_AssocChain threads a map through N immutable Assoc calls —
// the shape `(reduce assoc {} pairs)` compiles to. Per-op cost that rises with
// n means each call is copying the accumulated map rather than sharing it.
func BenchmarkHashMap_AssocChain(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				m := NewHashMap()
				for j := range n {
					m, _, _ = m.Assoc(Int{V: int64(j)}, Int{V: int64(j)})
				}
			}
		})
	}
}

// BenchmarkHashMap_SetBuild fills a fresh map through the mutable Set escape
// hatch — the path every large-map builder takes (hash-map, merge, map
// literals, OpMakeMap, json/decode). Giving HashMap a persistent large form
// puts this path at risk, so it is measured before as well as after.
func BenchmarkHashMap_SetBuild(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				m := NewHashMap()
				for j := range n {
					_ = m.Set(Int{V: int64(j)}, Int{V: int64(j)})
				}
			}
		})
	}
}

// BenchmarkHashMap_GetLarge reads from the trie form. It builds through Assoc
// rather than Set on purpose: Set builds into the Go staging map, so a
// Set-built map would measure the form this benchmark is not about.
// ScanVsMap pins the small-form boundary and says nothing about either.
func BenchmarkHashMap_GetLarge(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		m := NewHashMap()
		for j := range n {
			var err error
			m, _, err = m.Assoc(Int{V: int64(j)}, Int{V: int64(j)})
			if err != nil {
				b.Fatal(err)
			}
		}
		key := Int{V: int64(n - 1)}
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				m.Get(key)
			}
		})
	}
}

// BenchmarkListAt indexes a bulk-built list at sizes straddling
// listFlatThreshold. NewList promotes past the threshold, so At goes from a
// slice index to a chain walk; anything looping over At pays that per element.
func BenchmarkListAt(b *testing.B) {
	for _, n := range []int{32, 100, 1000} {
		items := make([]Value, n)
		for i := range items {
			items[i] = Int{V: int64(i)}
		}
		lst := NewList(items)
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := range b.N {
				_ = lst.At(i % n)
			}
		})
	}
}

// BenchmarkQuasiquoteWideList expands a quasiquoted list literal at sizes
// straddling listFlatThreshold. The expansion loop indexes the source list
// positionally, so a promoted backing makes it quadratic in the literal's
// length.
func BenchmarkQuasiquoteWideList(b *testing.B) {
	for _, n := range []int{16, 64, 256} {
		var sb strings.Builder
		sb.WriteString("`(")
		for i := range n {
			if i > 0 {
				sb.WriteByte(' ')
			}
			fmt.Fprintf(&sb, "%d", i)
		}
		sb.WriteByte(')')
		forms, err := Read(sb.String())
		if err != nil {
			b.Fatal(err)
		}
		env := newCoreEnv()
		e := NewEvaluator()
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				if _, err := e.Eval(context.Background(), forms[0], env); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
