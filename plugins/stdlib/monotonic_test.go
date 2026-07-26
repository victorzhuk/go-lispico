package stdlib

import (
	"context"
	"testing"

	"github.com/victorzhuk/go-lispico/core"
)

// TestAssocMonotonic_ChargesPerCallHonestly loops the real assoc builtin
// with a new key each call into the same growing map, asserting the charge
// is never zero and never grows with the accumulated map size.
//
// Past hashMapSmallLimit the map is a persistent trie, so a call copies the
// path down to the new key and shares the rest with its argument. The charge
// is therefore bounded by the trie's depth, not by how large the map has
// become — the property that keeps a chained assoc linear rather than
// quadratic, and keeps it under the ledger.
func TestAssocMonotonic_ChargesPerCallHonestly(t *testing.T) {
	const n = 500
	env := setupEnv(t)
	gfn := collectionGoFunc(t, env, "assoc")
	ctx := core.WithEvalResourceLimits(context.Background(), 1_000_000, 64<<20)

	m := core.NewHashMap()
	var total, maxDelta int64
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

		if delta <= 0 {
			t.Fatalf("iteration %d: assoc charge = %d, want > 0", i, delta)
		}
		if delta > maxDelta {
			maxDelta = delta
		}
		total += delta
	}

	// The per-call charge must stay bounded as the map grows. Charging the
	// whole map per call — what a non-persistent representation forces —
	// would put the last iteration alone near HashMapShallowBytes(n).
	if maxDelta >= core.HashMapShallowBytes(n) {
		t.Fatalf("largest single assoc charge = %d, want well under %d: the charge is scaling with map size",
			maxDelta, core.HashMapShallowBytes(n))
	}

	// And the total must stay linear-ish rather than quadratic. The sum of
	// HashMapShallowBytes at each step is what per-call whole-map charging
	// cost; anything near it means the quadratic charge is back.
	var quadraticTotal int64
	for i := 1; i <= n; i++ {
		quadraticTotal += core.HashMapShallowBytes(i)
	}
	if total >= quadraticTotal/4 {
		t.Fatalf("total assoc charge over %d calls = %d, want far below the quadratic %d",
			n, total, quadraticTotal)
	}
}
