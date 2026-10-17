package goldset

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/victorzhuk/go-lispico/cl"
	"github.com/victorzhuk/go-lispico/core"
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

// BenchmarkGoldsetCall isolates the Engine.Call boundary the release gate
// runs against: setup Eval happens once, untimed, so the timed loop measures
// only argument marshalling, function-cell lookup, and frame setup per call.
// Exactly one Call per iteration is deliberate — an absolute Call target is
// adjudicated against this cell's ns/op.
func BenchmarkGoldsetCall(b *testing.B) {
	mode := benchMode(b)
	cell, err := CallFixture()
	if err != nil {
		b.Fatal(err)
	}

	b.Run(cell.Name, func(b *testing.B) {
		eng, err := NewEngine(mode)
		if err != nil {
			b.Fatal(err)
		}
		defer func() { _ = eng.Close() }()
		ctx := context.Background()

		if _, err := eng.Eval(ctx, cell.Name, cell.Source); err != nil {
			b.Fatal(err)
		}

		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			if _, err := eng.Call(ctx, cell.Fn, cell.Args...); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkGoldsetParse isolates the reader's share of BenchmarkGoldset: the
// same fixture sources, parsed alone through the shared, mode-invariant
// reader (docs/profiling-baseline.md).
func BenchmarkGoldsetParse(b *testing.B) {
	fixtures, err := Fixtures()
	if err != nil {
		b.Fatal(err)
	}

	for _, fx := range fixtures {
		b.Run(fx.Name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := core.Read(fx.Source); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkGoldsetCL measures the representative Clojure-adapter fixtures
// through a CL engine: one public Eval per timed iteration, including
// reading and adapter work. Loading, engine construction, warm-up, golden
// comparison, and cleanup stay outside timing.
func BenchmarkGoldsetCL(b *testing.B) {
	mode := benchMode(b)
	clFixtures, err := fixtures(filepath.Join("testdata", "cl"))
	if err != nil {
		b.Fatal(err)
	}

	for _, fx := range clFixtures {
		b.Run(fx.Name, func(b *testing.B) {
			eng, err := newEngine(mode, cl.Dialect())
			if err != nil {
				b.Fatal(err)
			}
			defer func() { _ = eng.Close() }()
			ctx := context.Background()

			warm, err := eng.Eval(ctx, fx.Name, fx.Source)
			if err != nil {
				b.Fatal(err)
			}
			if warm.String() != fx.Want {
				b.Fatalf("warm-up got %s, want %s", warm.String(), fx.Want)
			}

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

// BenchmarkGoldsetCLParse isolates the CL reader's share of
// BenchmarkGoldsetCL: the same sources read through the CL dialect, not the
// identity reader, one Read per timed iteration.
func BenchmarkGoldsetCLParse(b *testing.B) {
	dialect := cl.Dialect()
	clFixtures, err := fixtures(filepath.Join("testdata", "cl"))
	if err != nil {
		b.Fatal(err)
	}

	for _, fx := range clFixtures {
		b.Run(fx.Name, func(b *testing.B) {
			warm, err := dialect.Read(fx.Source)
			if err != nil {
				b.Fatal(err)
			}
			if len(warm) == 0 {
				b.Fatal("warm-up parsed no forms")
			}

			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := dialect.Read(fx.Source); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkGoldsetConcurrent measures two closures captured from distinct
// environments of one shared Clojure engine, invoked by exactly two fixed
// goroutines (not RunParallel): one Call per iteration split across the
// workers, no channel dispatch or shared counter in the timed loop.
// Start-release and completion-wait overhead stays inside measured wall time.
func BenchmarkGoldsetConcurrent(b *testing.B) {
	mode := benchMode(b)
	concFixtures, err := fixtures(filepath.Join("testdata", "concurrent"))
	if err != nil {
		b.Fatal(err)
	}
	if len(concFixtures) != 1 || concFixtures[0].Name != "distinct-closures" {
		b.Fatalf("want exactly distinct-closures, got %v", concFixtures)
	}
	fx := concFixtures[0]

	b.Run(fx.Name, func(b *testing.B) {
		eng, err := NewEngine(mode)
		if err != nil {
			b.Fatal(err)
		}
		defer func() { _ = eng.Close() }()
		ctx := context.Background()

		setup, err := eng.Eval(ctx, fx.Name, fx.Source)
		if err != nil {
			b.Fatal(err)
		}
		if setup.String() != fx.Want {
			b.Fatalf("setup got %s, want %s", setup.String(), fx.Want)
		}

		type worker struct {
			fn        string
			want      core.Int
			calls     int
			callsDone int
			err       error
		}
		workers := []*worker{
			{fn: "rule-a", want: core.Int{V: 101}, calls: b.N/2 + b.N%2},
			{fn: "rule-b", want: core.Int{V: 102}, calls: b.N / 2},
		}
		args := []core.Value{core.Int{V: 100}}
		for _, w := range workers {
			got, err := eng.Call(ctx, w.fn, args...)
			if err != nil {
				b.Fatal(err)
			}
			gotInt, ok := got.(core.Int)
			if !ok || gotInt != w.want {
				b.Fatalf("%s: got %#v, want %#v", w.fn, got, w.want)
			}
		}
		var ready, done sync.WaitGroup
		start := make(chan struct{})
		ready.Add(len(workers))
		done.Add(len(workers))
		for _, w := range workers {
			go func(w *worker) {
				defer done.Done()
				ready.Done()
				<-start
				for range w.calls {
					got, err := eng.Call(ctx, w.fn, args...)
					if err != nil {
						w.err = err
						return
					}
					gotInt, ok := got.(core.Int)
					if !ok || gotInt != w.want {
						w.err = fmt.Errorf("%s: got %#v, want %#v", w.fn, got, w.want)
						return
					}
					w.callsDone++
				}
			}(w)
		}
		ready.Wait()
		b.ReportAllocs()
		b.ResetTimer()
		close(start)
		done.Wait()
		b.StopTimer()
		for _, w := range workers {
			if w.err != nil {
				b.Fatal(w.err)
			}
			if w.callsDone != w.calls {
				b.Fatalf("%s: %d calls done, want %d", w.fn, w.callsDone, w.calls)
			}
		}
	})
}
