package goldset

import (
	"context"
	"os"
	"testing"
)

// benchMode selects the execution mode for this bench process via
// GOLDSET_MODE (eval|vm). The release job runs the suite once per mode per
// sample, interleaved, so benchstat pairs identical cell names across the
// two output files (ADR 0008's Paired release run).
func benchMode(b *testing.B) Mode {
	switch v := os.Getenv("GOLDSET_MODE"); v {
	case "", string(ModeEvaluator):
		return ModeEvaluator
	case string(ModeVM):
		return ModeVM
	default:
		b.Fatalf("unknown GOLDSET_MODE %q, want eval or vm", v)
		return ""
	}
}

func BenchmarkGoldset(b *testing.B) {
	mode := benchMode(b)
	fixtures, err := Fixtures()
	if err != nil {
		b.Fatal(err)
	}

	for _, fx := range fixtures {
		b.Run(fx.Name, func(b *testing.B) {
			eng, err := NewEngine(mode)
			if err != nil {
				b.Fatal(err)
			}
			defer func() { _ = eng.Close() }()
			ctx := context.Background()

			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := eng.Eval(ctx, fx.Name, fx.Source); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
