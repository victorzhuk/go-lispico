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
// on one shared Engine so -mutexprofile can attribute wait time to
// bytecodeEvaluator.mu.
func BenchmarkEngine_EvalCachedConcurrent(b *testing.B) {
	b.Run("hits", benchmarkEvalCachedConcurrentHits)
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
