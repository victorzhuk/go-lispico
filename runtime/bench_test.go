package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
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
	eng, err := New(nil, WithDialect(clojure.Dialect()))
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
