package runtime

import (
	"context"
	"testing"

	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
)

// accumulationSrc conses a growing accumulator across 100k loop/recur
// iterations. With a shared-tail List each call allocates one node, so the
// charged bytes grow linearly and the loop stays under the default 64 MiB
// allocation ledger.
const accumulationSrc = `(loop ((i 0) (acc (quote ()))) (if (< i 100000) (recur (+ i 1) (cons i acc)) (count acc)))`

// mapAccumulationSrc is the same shape over a map. A chained assoc copies one
// root-to-leaf trie path per call rather than the whole map, which moved the
// ledger ceiling from ~1445 keys to somewhere between 40000 and 45000. 20000
// sits an order of magnitude past the old ceiling with margin below the new
// one — a persistent map cannot make this unbounded, since building n keys one
// at a time allocates O(n log n) however the structure is arranged.
const mapAccumulationSrc = `(loop ((i 0) (m {})) (if (< i 20000) (recur (+ i 1) (assoc m i i)) (count m)))`

// TestAccumulation100k is the proposal's exact repro under default resource
// limits, in both execution modes.
func TestAccumulation100k(t *testing.T) {
	runAccumulation(t, accumulationSrc, 100000)
}

// TestMapAccumulation20k is the same gate for maps: before the persistent
// large form it failed between 1440 and 1450 keys, in both modes, with a
// ResourceLimitError indistinguishable to the host from a genuine runaway.
func TestMapAccumulation20k(t *testing.T) {
	runAccumulation(t, mapAccumulationSrc, 20000)
}

func runAccumulation(t *testing.T, src string, want int64) {
	t.Helper()
	for _, tc := range []struct {
		name     string
		bytecode bool
	}{
		{"tree-walker", false},
		{"bytecode", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := []EngineOption{WithDialect(clojure.Dialect())}
			if tc.bytecode {
				opts = append(opts, WithBytecode())
			} else {
				opts = append(opts, WithTreeWalker())
			}
			e, err := New(nil, opts...)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			t.Cleanup(func() { _ = e.Close() })
			if err := e.Use(stdlib.New()); err != nil {
				t.Fatalf("Use(stdlib): %v", err)
			}

			result, err := e.Eval(context.Background(), "test", src)
			if err != nil {
				t.Fatalf("Eval: %v", err)
			}
			if wantVal := (core.Int{V: want}); !result.Equals(wantVal) {
				t.Fatalf("result = %v, want %v", result, wantVal)
			}
		})
	}
}
