package stdlib

import (
	"context"
	"testing"

	"github.com/victorzhuk/go-lispico/core"
)

// TestAssocMonotonic_ChargesPerCallHonestly loops the real assoc builtin
// with a new key each call into the same growing map, asserting each
// call's charge exactly matches HashMapShallowBytes(newLen) +
// ValueDeepBytes(newValue) — never zero, never re-walking untouched
// existing entries' values.
//
// Unlike cons/conj, this total is NOT expected to grow linearly: HashMap
// has no persistent representation past hashMapSmallLimit — Assoc's
// map-form branch does a real full copy every call (core/types.go) — so
// the charge honestly reflects that real O(n)-per-call cost. Making this
// linear would require giving HashMap a persistent representation, which
// is out of scope here.
func TestAssocMonotonic_ChargesPerCallHonestly(t *testing.T) {
	const n = 500
	env := setupEnv(t)
	gfn := collectionGoFunc(t, env, "assoc")
	ctx := core.WithEvalResourceLimits(context.Background(), 1_000_000, 64<<20)

	m := core.NewHashMap()
	var total int64
	for i := range n {
		key := core.Int{V: int64(i)}
		val := core.Int{V: int64(i)}
		before := core.EvalMeterFrom(ctx).Snapshot().AllocationBytes
		result, err := gfn.Fn(ctx, nil, []core.Value{m, key, val}, env)
		if err != nil {
			t.Fatalf("iteration %d: assoc: %v", i, err)
		}
		next, ok := result.(*core.HashMap)
		if !ok {
			t.Fatalf("iteration %d: assoc returned %T, want *core.HashMap", i, result)
		}
		m = next
		after := core.EvalMeterFrom(ctx).Snapshot().AllocationBytes
		delta := after - before

		want := core.HashMapShallowBytes(m.Len()) + core.ValueDeepBytes(val)
		if delta != want {
			t.Fatalf("iteration %d: assoc charge = %d, want %d", i, delta, want)
		}
		if delta <= 0 {
			t.Fatalf("iteration %d: assoc charge = %d, want > 0", i, delta)
		}
		total += delta
	}

	// Honest lower bound: total must be at least the sum of
	// HashMapShallowBytes at each step — the real per-call map copy, which
	// is itself already superlinear (O(n^2)). That is the fact this test
	// exists to pin, not hide.
	var wantMinTotal int64
	for i := 1; i <= n; i++ {
		wantMinTotal += core.HashMapShallowBytes(i)
	}
	if total < wantMinTotal {
		t.Fatalf("total assoc charge over %d calls = %d, want >= %d", n, total, wantMinTotal)
	}
}
