package runtime

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
)

// concurrentWorkingSet covers the rule-shaped source categories EvalCached
// serves: branching, closure state, keyword lookup, collection fold. Each
// source parses to one top-level form, so its cache key (sourceHash+formIndex)
// stays stable across repeated Eval calls.
var concurrentWorkingSet = []string{
	`(cond (> 1 2) :a (> 2 1) :b :else :c)`,
	`((fn [x] (let [f (fn [] x)] (f))) 5)`,
	`(get {:a 1 :b 2 :c 3} :b)`,
	`(reduce + 0 [1 2 3 4 5 6 7 8])`,
}

func newConcurrentEvalEngine(b *testing.B) Engine {
	b.Helper()
	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	if err != nil {
		b.Fatal(err)
	}
	if err := eng.Use(stdlib.New()); err != nil {
		b.Fatal(err)
	}
	return eng
}

// BenchmarkEngine_EvalCachedConcurrent drives concurrent EvalCached traffic
// on one shared Engine so -mutexprofile can attribute wait time to the chunk
// cache's stripe mutexes (cacheStripe.mu).
func BenchmarkEngine_EvalCachedConcurrent(b *testing.B) {
	b.Run("hits", benchmarkEvalCachedConcurrentHits)
	b.Run("hits-wide", benchmarkEvalCachedConcurrentHitsWide)
	b.Run("mixed", benchmarkEvalCachedConcurrentMixed)
}

// benchmarkEvalCachedConcurrentHits is the steady state: every evaluation
// hits the chunk cache, so the only lock section per call is the probe.
func benchmarkEvalCachedConcurrentHits(b *testing.B) {
	eng := newConcurrentEvalEngine(b)
	defer eng.Close()
	ctx := context.Background()

	for _, src := range concurrentWorkingSet {
		if _, err := eng.Eval(ctx, "warm", src); err != nil {
			b.Fatal(err)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()

	var goroutineSeed atomic.Int64
	b.RunParallel(func(pb *testing.PB) {
		i := int(goroutineSeed.Add(1))
		for pb.Next() {
			src := concurrentWorkingSet[i%len(concurrentWorkingSet)]
			if _, err := eng.Eval(ctx, "bench", src); err != nil {
				b.Error(err)
				return
			}
			i++
		}
	})
}

// benchmarkEvalCachedConcurrentMixed interleaves the warm working set with a
// bounded fraction of misses (roughly 1 in 8): a counter baked into a fixed-
// shape literal, so every miss pays admit (and eventually evict) rather than
// growing compile cost per iteration.
func benchmarkEvalCachedConcurrentMixed(b *testing.B) {
	eng := newConcurrentEvalEngine(b)
	defer eng.Close()
	ctx := context.Background()

	for _, src := range concurrentWorkingSet {
		if _, err := eng.Eval(ctx, "warm", src); err != nil {
			b.Fatal(err)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()

	var goroutineSeed, missCounter atomic.Int64
	b.RunParallel(func(pb *testing.PB) {
		i := int(goroutineSeed.Add(1))
		for pb.Next() {
			src := concurrentWorkingSet[i%len(concurrentWorkingSet)]
			if i%8 == 0 {
				src = fmt.Sprintf("(+ %d 1)", missCounter.Add(1))
			}
			if _, err := eng.Eval(ctx, "bench", src); err != nil {
				b.Error(err)
				return
			}
			i++
		}
	})
}

// concurrentWorkingSetWide widens concurrentWorkingSet's four rule shapes
// into 32 distinct sources by varying the literal each one closes over. With
// only 4 sources spread across 8 stripes, achievable contention reduction is
// capped near 4x by occupancy — at most 4 of 8 stripes ever hold an entry —
// regardless of how well the striping itself performs. This working set is
// wide enough that occupancy isn't the bottleneck, so a hits-arm comparison
// reads as the fix's own effect rather than as it underperforming.
var concurrentWorkingSetWide = buildConcurrentWorkingSetWide()

func buildConcurrentWorkingSetWide() []string {
	shapes := []func(n int) string{
		func(n int) string { return fmt.Sprintf(`(cond (> %d 2) :a (> 2 %d) :b :else :c)`, n, n) },
		func(n int) string { return fmt.Sprintf(`((fn [x] (let [f (fn [] x)] (f))) %d)`, n) },
		func(n int) string { return fmt.Sprintf(`(get {:a 1 :b 2 :c %d} :b)`, n) },
		func(n int) string { return fmt.Sprintf(`(reduce + %d [1 2 3 4 5 6 7 8])`, n) },
	}
	sources := make([]string, 0, len(shapes)*8)
	for _, shape := range shapes {
		for n := range 8 {
			sources = append(sources, shape(n))
		}
	}
	return sources
}

// runConcurrentHitsBench is the shared steady-state driver for the hits-wide
// and stripe-count-comparison arms: warm every source once, then hammer them
// from b.RunParallel with no misses.
func runConcurrentHitsBench(b *testing.B, eng Engine, sources []string) {
	b.Helper()
	ctx := context.Background()
	for _, src := range sources {
		if _, err := eng.Eval(ctx, "warm", src); err != nil {
			b.Fatal(err)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()

	var goroutineSeed atomic.Int64
	b.RunParallel(func(pb *testing.PB) {
		i := int(goroutineSeed.Add(1))
		for pb.Next() {
			src := sources[i%len(sources)]
			if _, err := eng.Eval(ctx, "bench", src); err != nil {
				b.Error(err)
				return
			}
			i++
		}
	})
}

// benchmarkEvalCachedConcurrentHitsWide is hits with the wide working set,
// so achievable contention reduction isn't capped by a source count narrower
// than the stripe count.
func benchmarkEvalCachedConcurrentHitsWide(b *testing.B) {
	eng := newConcurrentEvalEngine(b)
	defer eng.Close()
	runConcurrentHitsBench(b, eng, concurrentWorkingSetWide)
}

// newConcurrentEvalEngineWithStripes mirrors newConcurrentEvalEngine but lets
// a benchmark pin the chunk cache's stripe count via the test-only
// withCacheStripes option; stripes <= 0 leaves the engine's adaptive default.
func newConcurrentEvalEngineWithStripes(b *testing.B, stripes int) Engine {
	b.Helper()
	opts := []EngineOption{WithBytecode(), WithDialect(clojure.Dialect())}
	if stripes > 0 {
		opts = append(opts, withCacheStripes(stripes))
	}
	eng, err := New(nil, opts...)
	if err != nil {
		b.Fatal(err)
	}
	if err := eng.Use(stdlib.New()); err != nil {
		b.Fatal(err)
	}
	return eng
}

// BenchmarkEngine_EvalCachedConcurrentStripes compares a single stripe
// against the engine's adaptive default stripe count within one binary, so
// the stripe-count A/B never crosses a build.
func BenchmarkEngine_EvalCachedConcurrentStripes(b *testing.B) {
	b.Run("1", func(b *testing.B) {
		eng := newConcurrentEvalEngineWithStripes(b, 1)
		defer eng.Close()
		runConcurrentHitsBench(b, eng, concurrentWorkingSetWide)
	})
	b.Run("default", func(b *testing.B) {
		eng := newConcurrentEvalEngineWithStripes(b, 0)
		defer eng.Close()
		runConcurrentHitsBench(b, eng, concurrentWorkingSetWide)
	})
}
