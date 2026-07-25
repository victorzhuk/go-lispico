package stdlib

import (
	"context"
	"fmt"
	"runtime"
	"runtime/debug"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/core/compiler"
	"github.com/victorzhuk/go-lispico/core/vm"
)

func mergeGoFunc(tb testing.TB, env *core.Env) core.GoFunc {
	tb.Helper()
	fnVal, ok := env.Get("merge")
	if !ok {
		tb.Fatal("merge not registered")
	}
	gfn, ok := fnVal.(core.GoFunc)
	if !ok {
		tb.Fatalf("merge is not a GoFunc: %T", fnVal)
	}
	return gfn
}

// mergeKey uses a Keyword rather than an Int so toHashKey reuses the string
// field directly — Int keys route through strconv.FormatInt, which is
// alloc-free for small values (Go's smalls-table cache) but not for larger
// ones, skewing allocation counts across benchmark sizes for reasons
// unrelated to merge's own complexity.
func mergeKey(i int) core.Keyword {
	return core.Keyword{V: fmt.Sprintf("k%d", i)}
}

func buildMergeArgs(tb testing.TB, n int) []core.Value {
	tb.Helper()
	half := n / 2
	m1 := core.NewHashMap()
	for i := range half {
		if err := m1.Set(mergeKey(i), core.Int{V: int64(i)}); err != nil {
			tb.Fatalf("build m1: %v", err)
		}
	}
	m2 := core.NewHashMap()
	for i := half; i < n; i++ {
		if err := m2.Set(mergeKey(i), core.Int{V: int64(i)}); err != nil {
			tb.Fatalf("build m2: %v", err)
		}
	}
	return []core.Value{m1, m2}
}

func BenchmarkMerge(b *testing.B) {
	for _, n := range []int{10, 100, 1000, 10000} {
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			env := core.NewEnv(nil)
			if err := New().Init(env); err != nil {
				b.Fatalf("init stdlib: %v", err)
			}
			gfn := mergeGoFunc(b, env)
			args := buildMergeArgs(b, n)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := gfn.Fn(context.Background(), nil, args, env); err != nil {
					b.Fatalf("merge: %v", err)
				}
			}
		})
	}
}

func consGoFunc(tb testing.TB, env *core.Env) core.GoFunc {
	tb.Helper()
	fnVal, ok := env.Get("cons")
	if !ok {
		tb.Fatal("cons not registered")
	}
	gfn, ok := fnVal.(core.GoFunc)
	if !ok {
		tb.Fatalf("cons is not a GoFunc: %T", fnVal)
	}
	return gfn
}

// BenchmarkAccumulate_Cons builds an N-element list by calling the real
// cons builtin N times in a row — the quadratic-copy accumulation shape
// this change exists to fix, measured at the charged builtin call site.
func BenchmarkAccumulate_Cons(b *testing.B) {
	for _, n := range []int{100, 1000} {
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			env := core.NewEnv(nil)
			if err := New().Init(env); err != nil {
				b.Fatalf("init stdlib: %v", err)
			}
			gfn := consGoFunc(b, env)
			ctx := context.Background()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				var acc core.Value = core.List{}
				for i := range n {
					v, err := gfn.Fn(ctx, nil, []core.Value{core.Int{V: int64(i)}, acc}, env)
					if err != nil {
						b.Fatalf("cons: %v", err)
					}
					acc = v
				}
			}
		})
	}
}

func conjGoFunc(tb testing.TB, env *core.Env) core.GoFunc {
	tb.Helper()
	fnVal, ok := env.Get("conj")
	if !ok {
		tb.Fatal("conj not registered")
	}
	gfn, ok := fnVal.(core.GoFunc)
	if !ok {
		tb.Fatalf("conj is not a GoFunc: %T", fnVal)
	}
	return gfn
}

func concatGoFunc(tb testing.TB, env *core.Env) core.GoFunc {
	tb.Helper()
	fnVal, ok := env.Get("concat")
	if !ok {
		tb.Fatal("concat not registered")
	}
	gfn, ok := fnVal.(core.GoFunc)
	if !ok {
		tb.Fatalf("concat is not a GoFunc: %T", fnVal)
	}
	return gfn
}

func reverseGoFunc(tb testing.TB, env *core.Env) core.GoFunc {
	tb.Helper()
	fnVal, ok := env.Get("reverse")
	if !ok {
		tb.Fatal("reverse not registered")
	}
	gfn, ok := fnVal.(core.GoFunc)
	if !ok {
		tb.Fatalf("reverse is not a GoFunc: %T", fnVal)
	}
	return gfn
}

// BenchmarkAccumulate_Conj mirrors BenchmarkAccumulate_Cons through conj's
// List branch instead of cons's — before this change, conj built its result
// with an indexed At() loop over c, O(n) per call on a shared list, so
// accumulating n elements was O(n^2). n=10000 makes that regression visible;
// n=1000 does not.
func BenchmarkAccumulate_Conj(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			env := core.NewEnv(nil)
			if err := New().Init(env); err != nil {
				b.Fatalf("init stdlib: %v", err)
			}
			gfn := conjGoFunc(b, env)
			ctx := context.Background()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				var acc core.Value = core.List{}
				for i := range n {
					v, err := gfn.Fn(ctx, nil, []core.Value{acc, core.Int{V: int64(i)}}, env)
					if err != nil {
						b.Fatalf("conj: %v", err)
					}
					acc = v
				}
			}
		})
	}
}

// BenchmarkConcat_LastArgShared concats a 2-element prefix onto an n-element
// shared list passed as the last argument — the case this change shares
// instead of copying. n is the shared base list's size; before this change,
// flattening it with an indexed At() loop was O(n) per call on its own,
// on top of walking it again to build the flat result.
func BenchmarkConcat_LastArgShared(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		items := make([]core.Value, n)
		for i := range items {
			items[i] = core.Int{V: int64(i)}
		}
		base := core.NewList(items)
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			env := core.NewEnv(nil)
			if err := New().Init(env); err != nil {
				b.Fatalf("init stdlib: %v", err)
			}
			gfn := concatGoFunc(b, env)
			ctx := context.Background()
			prefix := core.NewList([]core.Value{core.Int{V: -1}, core.Int{V: -2}})
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := gfn.Fn(ctx, nil, []core.Value{prefix, base}, env); err != nil {
					b.Fatalf("concat: %v", err)
				}
			}
		})
	}
}

// BenchmarkReverse_Large reverses an n-element shared list — before this
// change, an indexed At() loop made this O(n^2) on the shared representation.
func BenchmarkReverse_Large(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		items := make([]core.Value, n)
		for i := range items {
			items[i] = core.Int{V: int64(i)}
		}
		base := core.NewList(items)
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			env := core.NewEnv(nil)
			if err := New().Init(env); err != nil {
				b.Fatalf("init stdlib: %v", err)
			}
			gfn := reverseGoFunc(b, env)
			ctx := context.Background()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := gfn.Fn(ctx, nil, []core.Value{base}, env); err != nil {
					b.Fatalf("reverse: %v", err)
				}
			}
		})
	}
}

func BenchmarkBootstrapPhases(b *testing.B) {
	ctx := context.Background()
	entries := stdlibBootstrapEntries()

	b.Run("read", func(b *testing.B) {
		source := entries[len(entries)-1].source
		for b.Loop() {
			if _, err := core.Read(source); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("macro-expand", func(b *testing.B) {
		source := entries[0].source
		forms, err := core.Read(source)
		if err != nil {
			b.Fatal(err)
		}
		env := core.NewEnv(nil)
		macro := core.NewEvaluator()
		for b.Loop() {
			if _, err := macro.MacroExpand(ctx, forms[0], env); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("compile-validate", func(b *testing.B) {
		source := entries[len(entries)-1].source
		forms, err := core.Read(source)
		if err != nil {
			b.Fatal(err)
		}
		form := forms[0]
		for b.Loop() {
			comp := compiler.NewCompiler("<bench>")
			if err := comp.Compile(form); err != nil {
				b.Fatal(err)
			}
			comp.Chunk().Emit(vm.OpReturn, 0)
			comp.MarkCaptures()
			if err := comp.Chunk().Validate(); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("env-populate", func(b *testing.B) {
		for b.Loop() {
			env := core.NewEnv(nil)
			env.SetEvaluator(core.NewEvaluator())
			registerBuiltinsForBench(New(), env)
		}
	})

	b.Run("binding-mirror", func(b *testing.B) {
		for b.Loop() {
			env := core.NewEnv(nil)
			env.SetEvaluator(core.NewEvaluator())
			registerBuiltinsForBench(New(), env)
			before := snapshotBootstrapBindings(env)
			for _, name := range []string{"->", "->>", "as->", "if-let", "when-let", "get-in"} {
				env.Set(name, core.Int{V: 1})
			}
			New().mirrorBootstrapBindings(env, before)
		}
	})
}

func registerBuiltinsForBench(p *Plugin, env *core.Env) {
	p.registerArithmetic(env)
	p.registerComparison(env)
	p.registerStrings(env)
	p.registerCollections(env)
	p.registerHigherOrder(env)
	p.registerControl(env)
	p.registerTypes(env)
}

func snapshotBootstrapBindings(env *core.Env) map[string]struct{} {
	before := make(map[string]struct{})
	for _, name := range env.VarNames() {
		before[name] = struct{}{}
	}
	for _, name := range env.FuncNames() {
		before[name] = struct{}{}
	}
	return before
}

func TestMerge_LinearGrowth(t *testing.T) {
	env := setupEnv(t)
	gfn := mergeGoFunc(t, env)

	allocsFor := func(n int) float64 {
		args := buildMergeArgs(t, n)
		return testing.AllocsPerRun(5, func() {
			_, err := gfn.Fn(context.Background(), nil, args, env)
			require.NoError(t, err)
		})
	}

	// bytesFor is the primary signal: allocation count is nearly blind to
	// the O(n^2) byte-copying pathology this test guards against (an old
	// entry-by-entry bulk-builder rebuild copies the whole map on every
	// insert, so bytes blow up long before the alloc count does).
	bytesFor := func(n int) float64 {
		args := buildMergeArgs(t, n)
		const iterations = 20
		old := debug.SetGCPercent(-1)
		defer debug.SetGCPercent(old)
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		for range iterations {
			_, err := gfn.Fn(context.Background(), nil, args, env)
			require.NoError(t, err)
		}
		runtime.ReadMemStats(&after)
		return float64(after.TotalAlloc-before.TotalAlloc) / iterations
	}

	allocsSmall, allocsLarge := allocsFor(100), allocsFor(1000)
	if ratio := allocsLarge / allocsSmall; ratio >= 20 {
		t.Errorf("merge allocation count grows superlinearly: allocs(100)=%.0f allocs(1000)=%.0f ratio=%.1f", allocsSmall, allocsLarge, ratio)
	}

	bytesSmall, bytesLarge := bytesFor(100), bytesFor(1000)
	if ratio := bytesLarge / bytesSmall; ratio >= 40 {
		t.Errorf("merge bytes allocated grow superlinearly: bytes(100)=%.0f bytes(1000)=%.0f ratio=%.1f", bytesSmall, bytesLarge, ratio)
	}
}
