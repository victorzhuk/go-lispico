package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/victorzhuk/go-lispico/cl"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
)

// Benchmarks using bracket literals are pinned to Clojure; the default flips to Common Lisp in shard-C.

func BenchmarkEngine_Creation(b *testing.B) {
	for i := 0; i < b.N; i++ {
		eng, err := New(nil)
		if err != nil {
			b.Fatal(err)
		}
		eng.Close()
	}
}

func BenchmarkEngine_StartupStdlibBytecode(b *testing.B) {
	// The lazy arm's warm engine populates the shared template registry, so the
	// first timed iteration is not charged with template construction. The eager
	// arm disables that registry, so its warm engine buys only general process
	// warm-up.
	b.Run("lazy", func(b *testing.B) {
		warm, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
		if err != nil {
			b.Fatal(err)
		}
		if err := warm.Use(stdlib.New()); err != nil {
			b.Fatal(err)
		}
		warm.Close()
		b.ResetTimer()
		benchmarkEngineStartupStdlibBytecode(b)
	})
	// eager reproduces the pre-lazy startup: stdlib fully executed into the
	// root env at Use time.
	b.Run("eager", func(b *testing.B) {
		restoreLazy := SetStdlibLazyDisabledForTesting(true)
		defer restoreLazy()
		warm, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
		if err != nil {
			b.Fatal(err)
		}
		if err := warm.Use(stdlib.New()); err != nil {
			b.Fatal(err)
		}
		warm.Close()
		b.ResetTimer()
		benchmarkEngineStartupStdlibBytecode(b)
	})
}

// BenchmarkEngine_UseStdlibBytecode isolates construction+Use+Close (no
// eval): the per-request embedder floor under lazy vs eager stdlib load.
func BenchmarkEngine_UseStdlibBytecode(b *testing.B) {
	run := func(b *testing.B) {
		for b.Loop() {
			eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
			if err != nil {
				b.Fatal(err)
			}
			if err := eng.Use(stdlib.New()); err != nil {
				b.Fatal(err)
			}
			eng.Close()
		}
	}
	b.Run("lazy", func(b *testing.B) {
		warm, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
		if err != nil {
			b.Fatal(err)
		}
		if err := warm.Use(stdlib.New()); err != nil {
			b.Fatal(err)
		}
		warm.Close()
		b.ResetTimer()
		run(b)
	})
	b.Run("eager", func(b *testing.B) {
		restoreLazy := SetStdlibLazyDisabledForTesting(true)
		defer restoreLazy()
		warm, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
		if err != nil {
			b.Fatal(err)
		}
		if err := warm.Use(stdlib.New()); err != nil {
			b.Fatal(err)
		}
		warm.Close()
		b.ResetTimer()
		run(b)
	})
}

// BenchmarkEngine_StartupFullSurfaceBytecode measures the lazy/eager
// convergence claim: a script that touches the full stdlib surface (all
// template entries materialize) must cost the same as eager startup, since
// deferred work is only shifted, not removed.
func BenchmarkEngine_StartupFullSurfaceBytecode(b *testing.B) {
	// One top-level form per entry: macros only expand at top level.
	fullSurface := []string{
		"(+ 1 2)", "(- 5 3)", "(* 2 4)", "(/ 8 2)", "(mod 7 3)", "(quot 7 2)", "(pow 2 3)", "(sqrt 4)", "(abs -1)", "(floor 1.5)", "(ceil 1.5)", "(zero? 0)", "(pos? 1)", "(neg? -1)", "(max 1 2)", "(min 1 2)",
		"(= 1 1)", "(< 1 2)", "(> 2 1)", "(<= 1 1)", "(>= 2 2)",
		"(str \"a\" \"b\")", "(first [1 2])", "(rest [1 2])", "(cons 1 [2])", "(list 1 2)", "(concat [1] [2])", "(count [1 2])", "(nth [1 2] 0)", "(reverse [1 2])", "(sort [2 1])",
		"(map (fn [x] (+ x 1)) [1 2])", "(filter (fn [x] (> x 1)) [1 2])", "(reduce + 0 [1 2])", "(apply + [1 2])", "(type 1)", "(get {:a 1} :a)",
		"(get-in {:a {:b 2}} [:a :b])",
		"(-> 1 (+ 2))", "(->> [1 2] (map (fn [x] (+ x 1))))", "(as-> 1 x (+ x 1))", "(if-let [x 1] x 0)", "(when-let [x 1] x)",
	}
	run := func(b *testing.B) {
		ctx := context.Background()
		for b.Loop() {
			eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
			if err != nil {
				b.Fatal(err)
			}
			if err := eng.Use(stdlib.New()); err != nil {
				b.Fatal(err)
			}
			for _, src := range fullSurface {
				if _, err := eng.Eval(ctx, "full-surface", src); err != nil {
					b.Fatal(err)
				}
			}
			eng.Close()
		}
	}
	b.Run("lazy", func(b *testing.B) {
		warm, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
		if err != nil {
			b.Fatal(err)
		}
		if err := warm.Use(stdlib.New()); err != nil {
			b.Fatal(err)
		}
		warm.Close()
		b.ResetTimer()
		run(b)
	})
	b.Run("eager", func(b *testing.B) {
		restoreLazy := SetStdlibLazyDisabledForTesting(true)
		defer restoreLazy()
		warm, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
		if err != nil {
			b.Fatal(err)
		}
		if err := warm.Use(stdlib.New()); err != nil {
			b.Fatal(err)
		}
		warm.Close()
		b.ResetTimer()
		run(b)
	})
}

func benchmarkEngineStartupStdlibBytecode(b *testing.B) {
	ctx := context.Background()
	for b.Loop() {
		eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
		if err != nil {
			b.Fatal(err)
		}
		if err := eng.Use(stdlib.New()); err != nil {
			b.Fatal(err)
		}
		if _, err := eng.Eval(ctx, "startup", "(get-in (hash-map :a 1) (vector :a))"); err != nil {
			b.Fatal(err)
		}
		eng.Close()
	}
}

func BenchmarkEngine_EvalSimple(b *testing.B) {
	eng, err := New(nil)
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()

	bindBuiltin(b, eng, "+")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = eng.Eval(context.Background(), "bench", "(+ 1 2)")
	}
}

func BenchmarkEngine_EvalComplex(b *testing.B) {
	eng, err := New(nil, WithDialect(clojure.Dialect()))
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()

	bindBuiltin(b, eng, "+")
	bindBuiltin(b, eng, "-")
	bindBuiltin(b, eng, "=")
	bindBuiltin(b, eng, "<")

	_, _ = eng.Eval(context.Background(), "setup", `
(defn fib [n]
  (if (< n 2)
    n
    (+ (fib (- n 1)) (fib (- n 2)))))
`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = eng.Eval(context.Background(), "bench", "(fib 10)")
	}
}

func BenchmarkEngine_LoadDir(b *testing.B) {
	dir := b.TempDir()

	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("file_%02d.lisp", i)
		content := ""
		for j := 0; j < 100; j++ {
			content += fmt.Sprintf("(def var-%d-%d %d)\n", i, j, j)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		eng, err := New(nil)
		if err != nil {
			b.Fatal(err)
		}
		if err := eng.LoadDir(dir); err != nil {
			b.Fatal(err)
		}
		eng.Close()
	}
}

func BenchmarkEngine_HotReload(b *testing.B) {
	eng, err := New(nil)
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()

	dir := b.TempDir()
	file := filepath.Join(dir, "reload.lisp")

	if err := os.WriteFile(file, []byte("(def x 1)"), 0o644); err != nil {
		b.Fatal(err)
	}

	impl := eng.(*engineImpl)
	w := newFileWatcher(impl, dir, 10*time.Millisecond)
	w.ctx = context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		content := fmt.Sprintf("(def x %d)", i)
		if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
			b.Fatal(err)
		}
		w.scan()
	}
}

func BenchmarkEngine_Stats(b *testing.B) {
	eng, err := New(nil)
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()

	bindBuiltin(b, eng, "+")

	for i := 0; i < 1000; i++ {
		_, _ = eng.Eval(context.Background(), "setup", "(+ 1 2)")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = eng.Stats()
	}
}

// BenchmarkEngine_Call is not a gate cell and must not be cited as one: the
// release workflow benches ./internal/goldset/ only, a package this one is
// not part of.
func BenchmarkEngine_Call(b *testing.B) {
	eng, err := New(nil, WithDialect(clojure.Dialect()))
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()

	bindBuiltin(b, eng, "+")

	_, _ = eng.Eval(context.Background(), "setup", "(defn add [a b] (+ a b))")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = eng.Call(context.Background(), "add", core.Int{V: 1}, core.Int{V: 2})
	}
}

// BenchmarkEngine_CallBytecode carries the same gate-cell caveat as
// BenchmarkEngine_Call above.
func BenchmarkEngine_CallBytecode(b *testing.B) {
	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()

	bindBuiltin(b, eng, "+")

	_, _ = eng.Eval(context.Background(), "setup", "(defn add [a b] (+ a b))")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = eng.Call(context.Background(), "add", core.Int{V: 1}, core.Int{V: 2})
	}
}

// BenchmarkEngine_CallBytecodePlain measures Engine.Call for a GoFunc-free
// body ((defn pick [a b] a), compiled to OpGetLocal/OpReturn) — the clean
// boundary the lazy-observability fast path optimizes: no reentrantCtx
// evalState, no callback, no timing.
//
// Same gate-cell caveat as BenchmarkEngine_Call.
func BenchmarkEngine_CallBytecodePlain(b *testing.B) {
	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()

	_, _ = eng.Eval(context.Background(), "setup", "(defn pick [a b] a)")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = eng.Call(context.Background(), "pick", core.Int{V: 1}, core.Int{V: 2})
	}
}

// BenchmarkEngine_CallBytecodeCanonical measures Engine.Call for an arithmetic
// body ((defn add [a b] (+ a b))) with a canonical stdlib `+` — the path the
// native-op fast path actually optimizes. Unlike BenchmarkEngine_CallBytecode
// (which binds `+` via Engine.Bind, clearing the canonical flag and forcing a
// GoFunc fallback), this reflects the shipped runtime: canonical `+` compiles
// to OpAdd and executes via execNativeFastFused, no GoFunc call frame.
//
// Same gate-cell caveat as BenchmarkEngine_Call.
func BenchmarkEngine_CallBytecodeCanonical(b *testing.B) {
	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()
	if err := eng.Use(stdlib.New()); err != nil {
		b.Fatal(err)
	}

	_, _ = eng.Eval(context.Background(), "setup", "(defn add [a b] (+ a b))")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = eng.Call(context.Background(), "add", core.Int{V: 1}, core.Int{V: 2})
	}
}

// BenchmarkEngine_FibonacciCL measures recursive fib under the shipped
// default dialect (Common Lisp, Lisp-2) with real stdlib canonical
// arithmetic — every other fib/Call benchmark in this repo runs under
// clojure.Dialect() (Lisp-1), which runtime.New() does not default to.
//
// It is not a gate cell and must not be cited as one: the release workflow
// benches ./internal/goldset/ only, and that corpus is Clojure-dialect and
// non-recursive by decision (consumer-release-gate, "Gate corpus dialect and
// recursion coverage"). What this benchmark is for is per-change evidence on
// the Lisp-2 path — it is the recorded acceptance gate for the func-cell site
// cache (archive/2026-07-29-vm-func-site-cache, −30.43% p=0.000), which is
// why it stays.
func BenchmarkEngine_FibonacciCL(b *testing.B) {
	eng, err := New(nil, WithBytecode(), WithDialect(cl.Dialect()))
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()
	if err := eng.Use(stdlib.New()); err != nil {
		b.Fatal(err)
	}

	_, err = eng.Eval(context.Background(), "setup", `
(defun fib (n)
  (if (< n 2)
    n
    (+ (fib (- n 1)) (fib (- n 2)))))`)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = eng.Call(context.Background(), "fib", core.Int{V: 15})
	}
}

func BenchmarkEngine_FuncCall(b *testing.B) {
	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()
	if err := eng.Use(stdlib.New()); err != nil {
		b.Fatal(err)
	}

	_, _ = eng.Eval(context.Background(), "setup", "(defn add [a b] (+ a b))")
	add, err := eng.Func("add")
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for b.Loop() {
		_, _ = add.Call(context.Background(), core.Int{V: 1}, core.Int{V: 2})
	}
}

func BenchmarkEngine_FuncCallCallback(b *testing.B) {
	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()
	if err := eng.Use(stdlib.New()); err != nil {
		b.Fatal(err)
	}

	_, _ = eng.Eval(context.Background(), "setup", "(defn add [a b] (+ a b))")
	add, err := eng.Func("add")
	if err != nil {
		b.Fatal(err)
	}
	eng.OnPluginCall(func(PluginCallEvent) {})

	b.ResetTimer()
	for b.Loop() {
		_, _ = add.Call(context.Background(), core.Int{V: 1}, core.Int{V: 2})
	}
}

// BenchmarkEngine_PinnedFnCall mirrors BenchmarkEngine_FuncCall but on the
// single-owner PinnedFn path. The body is a GoFunc-free defn — the bench
// targets the steady-state allocation cost of the private VM (incremental
// reset, no pool Get/Put). A GoFunc-free body must report 0 allocs/op; any
// allocation here signals a regression in the private-VM reset strategy.
func BenchmarkEngine_PinnedFnCall(b *testing.B) {
	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()
	if err := eng.Use(stdlib.New()); err != nil {
		b.Fatal(err)
	}

	_, _ = eng.Eval(context.Background(), "setup", "(defn add [a b] (+ a b))")
	add, err := eng.Func("add")
	if err != nil {
		b.Fatal(err)
	}
	pinned := add.Pin()

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		_, _ = pinned.Call(context.Background(), core.Int{V: 1}, core.Int{V: 2})
	}
}

// BenchmarkEngine_PinnedFnCallCallback mirrors BenchmarkEngine_FuncCallCallback
// on the single-owner PinnedFn path with OnPluginCall registered. Measures
// the with-callback cost the same way as the Fn variant — a true like-for-like.
func BenchmarkEngine_PinnedFnCallCallback(b *testing.B) {
	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()
	if err := eng.Use(stdlib.New()); err != nil {
		b.Fatal(err)
	}

	_, _ = eng.Eval(context.Background(), "setup", "(defn add [a b] (+ a b))")
	add, err := eng.Func("add")
	if err != nil {
		b.Fatal(err)
	}
	pinned := add.Pin()
	eng.OnPluginCall(func(PluginCallEvent) {})

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		_, _ = pinned.Call(context.Background(), core.Int{V: 1}, core.Int{V: 2})
	}
}

// BenchmarkEngine_CallBytecodeCallback is BenchmarkEngine_CallBytecode with
// an OnPluginCall callback registered — measures the with-callback cost
// (timing + RLock + slice copy + dispatch) the fast path pays only when a
// caller actually asked for it.
//
// Same gate-cell caveat as BenchmarkEngine_Call.
func BenchmarkEngine_CallBytecodeCallback(b *testing.B) {
	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()

	bindBuiltin(b, eng, "+")

	_, _ = eng.Eval(context.Background(), "setup", "(defn add [a b] (+ a b))")
	eng.OnPluginCall(func(PluginCallEvent) {})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = eng.Call(context.Background(), "add", core.Int{V: 1}, core.Int{V: 2})
	}
}

func BenchmarkEngine_Bind(b *testing.B) {
	eng, err := New(nil)
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = eng.Bind(fmt.Sprintf("var-%d", i), core.Int{V: int64(i)})
	}
}

func BenchmarkEngine_ParallelEval(b *testing.B) {
	eng, err := New(nil)
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()

	bindBuiltin(b, eng, "+")

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = eng.Eval(context.Background(), "parallel", "(+ 1 2)")
		}
	})
}

func BenchmarkEngine_ParallelCallBytecode(b *testing.B) {
	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()

	bindBuiltin(b, eng, "+")

	_, _ = eng.Eval(context.Background(), "setup", "(defn add [a b] (+ a b))")

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			_, _ = eng.Call(context.Background(), "add", core.Int{V: int64(i)}, core.Int{V: 1})
			i++
		}
	})
}

func BenchmarkEngine_ParallelCall(b *testing.B) {
	eng, err := New(nil, WithDialect(clojure.Dialect()))
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()

	bindBuiltin(b, eng, "+")

	_, _ = eng.Eval(context.Background(), "setup", "(defn add [a b] (+ a b))")

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			_, _ = eng.Call(context.Background(), "add", core.Int{V: int64(i)}, core.Int{V: 1})
			i++
		}
	})
}

func BenchmarkEngine_ParallelStats(b *testing.B) {
	eng, err := New(nil)
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()

	bindBuiltin(b, eng, "+")

	for i := 0; i < 1000; i++ {
		_, _ = eng.Eval(context.Background(), "setup", "(+ 1 2)")
	}

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = eng.Stats()
		}
	})
}

func BenchmarkEngine_EvalWithBindings(b *testing.B) {
	eng, err := New(nil)
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()

	bindings := map[string]core.Value{
		"x": core.Int{V: 10},
		"y": core.Int{V: 20},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = eng.EvalWithBindings(context.Background(), "(+ x y)", bindings)
	}
}

func buildFileLoadSource() string {
	var lines []string
	for i := range 1000 {
		lines = append(lines, fmt.Sprintf("(def x%d %d)", i, i))
	}
	lines = append(lines, "(do x0 x999)")
	return "(do " + strings.Join(lines, " ") + ")"
}

// BenchmarkEngine_LoadFileTreeWalker measures repeated eval of a file-like
// source through the tree-walking evaluator (no bytecode, no chunk cache).
func BenchmarkEngine_LoadFileTreeWalker(b *testing.B) {
	eng, err := New(nil, WithTreeWalker(), WithDialect(clojure.Dialect()))
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()

	src := buildFileLoadSource()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = eng.Eval(context.Background(), "file", src)
	}
}

// BenchmarkEngine_LoadFileBytecode measures repeated eval of the same
// file-like source through the bytecode VM. After the first iteration the
// per-form chunks are cached and the VMs are reused from a sync.Pool.
func BenchmarkEngine_LoadFileBytecode(b *testing.B) {
	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()

	src := buildFileLoadSource()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = eng.Eval(context.Background(), "file", src)
	}
}

// benchmarkEngineAccumulate runs the accumulation-loop shape from
// TestAccumulation100k end to end through the public Eval API, at n small
// enough to stay under the default allocation ledger.
func benchmarkEngineAccumulate(b *testing.B, bytecode bool, n int) {
	b.Helper()
	opts := []EngineOption{WithDialect(clojure.Dialect())}
	if bytecode {
		opts = append(opts, WithBytecode())
	} else {
		opts = append(opts, WithTreeWalker())
	}
	eng, err := New(nil, opts...)
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()
	if err := eng.Use(stdlib.New()); err != nil {
		b.Fatal(err)
	}

	src := fmt.Sprintf(`(loop [i 0 acc '()] (if (< i %d) (recur (+ i 1) (cons i acc)) (count acc)))`, n)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := eng.Eval(context.Background(), "bench", src); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEngine_Accumulate100_Bytecode(b *testing.B) {
	benchmarkEngineAccumulate(b, true, 100)
}

func BenchmarkEngine_Accumulate100_TreeWalker(b *testing.B) {
	benchmarkEngineAccumulate(b, false, 100)
}

func BenchmarkEngine_Accumulate1000_Bytecode(b *testing.B) {
	benchmarkEngineAccumulate(b, true, 1000)
}

func BenchmarkEngine_Accumulate1000_TreeWalker(b *testing.B) {
	benchmarkEngineAccumulate(b, false, 1000)
}
