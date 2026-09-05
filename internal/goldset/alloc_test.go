package goldset

import (
	"context"
	"testing"
)

// vmAllocCeilings pins the allocations one Eval of each fixture makes under the
// bytecode VM. The release consumer gate compares these counts against the
// previous release's stored baseline with a zero allowance (ADR 0008), so a
// single extra allocation fails a release weeks after it lands. Pinning them
// here moves that signal into the ordinary test run.
//
// Counts are exact in Go's benchmark output, unlike B/op, which is a rounded
// total/iterations average and drifts by a few bytes between runs. Move a
// number only with a reason, in either direction: a fixture that allocates less
// should tighten its pin rather than leave headroom.
var vmAllocCeilings = map[string]int{
	"counter-closure": 56,
	"guard-nil":       30,
	"kw-lookup":       31,
	"loop-sum":        87,
	"merge-config":    58,
	"pipeline":        71,
	"queue-promote":   174,
	"registry-fold":   69,
	"route-decision":  48,
	"rule-load":       164,
	"safe-parse":      71,
	"text-render":     42,
	"twice-macro":     43,
}

// TestGoldsetVMAllocations pins the per-fixture allocation count the release
// gate measures. It exists because a lost make([]core.Value, 0, n) in the map
// kernel turned one sized allocation into a growing append, and nothing failed
// until the gate compared two releases.
func TestGoldsetVMAllocations(t *testing.T) {
	fixtures, err := Fixtures()
	if err != nil {
		t.Fatal(err)
	}
	if len(fixtures) != len(vmAllocCeilings) {
		t.Fatalf("goldset carries %d fixtures but %d are pinned; pin the new one rather than leaving it unmeasured",
			len(fixtures), len(vmAllocCeilings))
	}

	for _, fx := range fixtures {
		t.Run(fx.Name, func(t *testing.T) {
			want, ok := vmAllocCeilings[fx.Name]
			if !ok {
				t.Fatalf("no allocation count pinned for fixture %q", fx.Name)
			}

			eng, err := NewEngine(ModeVM)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = eng.Close() })
			ctx := context.Background()

			// One untimed Eval first: the bytecode chunk cache and the deferred
			// stdlib names both allocate on first touch, and the gate measures
			// the steady state, not the warm-up.
			if _, err := eng.Eval(ctx, fx.Name, fx.Source); err != nil {
				t.Fatal(err)
			}

			var evalErr error
			got := int(testing.AllocsPerRun(50, func() {
				if _, err := eng.Eval(ctx, fx.Name, fx.Source); err != nil {
					evalErr = err
				}
			}))
			if evalErr != nil {
				t.Fatal(evalErr)
			}

			if got != want {
				t.Errorf("%s allocates %d times per Eval under the VM, pinned at %d", fx.Name, got, want)
			}
		})
	}
}
