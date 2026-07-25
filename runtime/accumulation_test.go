package runtime

import (
	"context"
	"testing"

	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
)

// accumulationSrc conses a growing accumulator across 100k loop/recur
// iterations. List.Cons copies the whole result on every call today, so the
// bytes charged via ValueDeepBytes grow quadratically and blow the default
// 64 MiB allocation ledger long before the loop completes.
const accumulationSrc = `(loop ((i 0) (acc (quote ()))) (if (< i 100000) (recur (+ i 1) (cons i acc)) (count acc)))`

// TestAccumulation100k is the proposal's exact repro under default resource
// limits. It is expected to fail today in both execution modes — slice C
// (persistent list/vector backings) makes it pass.
func TestAccumulation100k(t *testing.T) {
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

			result, err := e.Eval(context.Background(), "test", accumulationSrc)
			if err != nil {
				t.Fatalf("Eval: %v", err)
			}
			want := core.Int{V: 100000}
			if !result.Equals(want) {
				t.Fatalf("result = %v, want %v", result, want)
			}
		})
	}
}
