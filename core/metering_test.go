package core

import (
	"fmt"
	"testing"
)

// TestVectorLedgerBytesIndependentOfLayout pins that the allocation ledger's
// accounted size for a Vector comes from the ADR 0011 fixed table, not from
// the Vector's internal Go representation. A flat Vector and a trie-form
// Vector holding the same elements must charge identically — the ledger
// must stay blind to any future flat/trie layout change.
func TestVectorLedgerBytesIndependentOfLayout(t *testing.T) {
	if MeterCollectionHeaderBytes != 24 {
		t.Fatalf("MeterCollectionHeaderBytes = %d, want 24", MeterCollectionHeaderBytes)
	}
	if MeterValueSlotBytes != 16 {
		t.Fatalf("MeterValueSlotBytes = %d, want 16", MeterValueSlotBytes)
	}
	if MeterScalarBytes != 16 {
		t.Fatalf("MeterScalarBytes = %d, want 16", MeterScalarBytes)
	}

	for _, n := range []int{100, 1000, 10000} {
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			items := make([]Value, n)
			for i := range items {
				items[i] = Int{V: int64(i)}
			}
			flatVec := NewVector(items)

			var trieVec Vector
			for _, item := range items {
				trieVec, _ = trieVec.Conj(item)
			}
			if trieVec.root == nil {
				t.Fatalf("trieVec stayed flat at n=%d, want a promoted trie", n)
			}

			if flatVec.Len() != n || trieVec.Len() != n {
				t.Fatalf("Len() = flat:%d trie:%d, want both %d", flatVec.Len(), trieVec.Len(), n)
			}
			if !flatVec.Equals(trieVec) {
				t.Fatalf("flatVec and trieVec hold different content at n=%d", n)
			}

			wantShallow := MeterCollectionHeaderBytes + int64(n)*MeterValueSlotBytes
			wantDeep := wantShallow + int64(n)*MeterScalarBytes

			flatShallow, trieShallow := ValueShallowBytes(flatVec), ValueShallowBytes(trieVec)
			if flatShallow != trieShallow {
				t.Errorf("ValueShallowBytes differ by layout: flat=%d trie=%d", flatShallow, trieShallow)
			}
			if flatShallow != wantShallow {
				t.Errorf("ValueShallowBytes = %d, want %d", flatShallow, wantShallow)
			}

			flatDeep, trieDeep := ValueDeepBytes(flatVec), ValueDeepBytes(trieVec)
			if flatDeep != trieDeep {
				t.Errorf("ValueDeepBytes differ by layout: flat=%d trie=%d", flatDeep, trieDeep)
			}
			if flatDeep != wantDeep {
				t.Errorf("ValueDeepBytes = %d, want %d", flatDeep, wantDeep)
			}
		})
	}
}
