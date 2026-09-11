package core

import (
	"context"
	"math"
	"strconv"
	"sync"
	"testing"
)

func TestHashMap_KeyIdentity(t *testing.T) {
	t.Parallel()

	t.Run("int and float are distinct keys", func(t *testing.T) {
		t.Parallel()
		m := NewHashMap()
		m, _, err := m.Assoc(Int{V: 1}, String{V: "int"})
		if err != nil {
			t.Fatal(err)
		}
		m, _, err = m.Assoc(Float{V: 1.0}, String{V: "float"})
		if err != nil {
			t.Fatal(err)
		}
		if m.Len() != 2 {
			t.Fatalf("Len() = %d, want 2 (Int{1} and Float{1.0} must not collide)", m.Len())
		}
		if v, ok := m.Get(Int{V: 1}); !ok || !v.Equals(String{V: "int"}) {
			t.Errorf("Get(Int{1}) = %v, %v", v, ok)
		}
		if v, ok := m.Get(Float{V: 1.0}); !ok || !v.Equals(String{V: "float"}) {
			t.Errorf("Get(Float{1.0}) = %v, %v", v, ok)
		}
	})

	t.Run("NaN key retrievable by another NaN", func(t *testing.T) {
		t.Parallel()
		m := NewHashMap()
		m, _, err := m.Assoc(Float{V: math.NaN()}, String{V: "nan"})
		if err != nil {
			t.Fatal(err)
		}
		v, ok := m.Get(Float{V: math.NaN()})
		if !ok || !v.Equals(String{V: "nan"}) {
			t.Errorf("Get(other NaN) = %v, %v; NaN keys must collapse to one bit pattern", v, ok)
		}
		if m.Len() != 1 {
			t.Fatalf("Len() = %d, want 1", m.Len())
		}
	})

	t.Run("positive and negative zero share one key", func(t *testing.T) {
		t.Parallel()
		m := NewHashMap()
		m, _, err := m.Assoc(Float{V: 0}, String{V: "zero"})
		if err != nil {
			t.Fatal(err)
		}
		v, ok := m.Get(Float{V: math.Copysign(0, -1)})
		if !ok || !v.Equals(String{V: "zero"}) {
			t.Errorf("Get(-0.0) = %v, %v; -0.0 must normalize to the +0.0 key", v, ok)
		}
		if m.Len() != 1 {
			t.Fatalf("Len() = %d, want 1", m.Len())
		}
	})
}

func TestHashMap_Get_AllocsPerRun(t *testing.T) {
	m := NewHashMap()
	for i := range 6 {
		var err error
		m, _, err = m.Assoc(Int{V: int64(i)}, Int{V: int64(i * 2)})
		if err != nil {
			t.Fatal(err)
		}
	}
	key := Int{V: 3}
	allocs := testing.AllocsPerRun(1000, func() {
		m.Get(key)
	})
	if allocs != 0 {
		t.Errorf("Get allocs = %v, want 0", allocs)
	}
}

func TestHashMap_PromotionBoundary(t *testing.T) {
	t.Parallel()

	m := NewHashMap()
	for i := range hashMapSmallLimit {
		var err error
		m, _, err = m.Assoc(Int{V: int64(i)}, Int{V: int64(i * 10)})
		if err != nil {
			t.Fatalf("Assoc(%d) error: %v", i, err)
		}
	}
	if m.large != nil {
		t.Fatal("map at the limit should still be in small form")
	}

	m9, _, err := m.Assoc(Int{V: hashMapSmallLimit}, Int{V: hashMapSmallLimit * 10})
	if err != nil {
		t.Fatalf("Assoc(9th key) error: %v", err)
	}
	if m9.large == nil {
		t.Fatal("the 9th distinct key should promote to map form")
	}
	if m9.Len() != hashMapSmallLimit+1 {
		t.Fatalf("Len() = %d, want %d", m9.Len(), hashMapSmallLimit+1)
	}
	if m.large != nil || m.Len() != hashMapSmallLimit {
		t.Fatal("Assoc must not mutate the receiver while promoting")
	}

	shrunk, _, err := m9.Dissoc(Int{V: hashMapSmallLimit})
	if err != nil {
		t.Fatalf("Dissoc error: %v", err)
	}
	if shrunk.large == nil {
		t.Fatal("dropping below the limit must not demote back to small form")
	}
	if shrunk.Len() != hashMapSmallLimit {
		t.Fatalf("Len() = %d, want %d", shrunk.Len(), hashMapSmallLimit)
	}
	if !shrunk.Equals(m) {
		t.Error("shrunk promoted map should equal the original small map with the same pairs")
	}

	for i := range hashMapSmallLimit {
		v, ok := shrunk.Get(Int{V: int64(i)})
		if !ok || !v.Equals(Int{V: int64(i * 10)}) {
			t.Errorf("Get(%d) = %v, %v; want %d, true", i, v, ok, i*10)
		}
	}

	var order []int64
	shrunk.Each(func(k, v Value) {
		order = append(order, k.(Int).V)
	})
	for i := 1; i < len(order); i++ {
		if order[i-1] >= order[i] {
			t.Fatalf("Each order not sorted by hashKey: %v", order)
		}
	}
}

func TestHashMap_Equals_RepresentationBlind(t *testing.T) {
	t.Parallel()

	keys := []Value{
		Keyword{V: "a"}, Keyword{V: "b"}, Keyword{V: "c"}, Keyword{V: "d"}, Keyword{V: "e"},
		Keyword{V: "f"}, Keyword{V: "g"}, Keyword{V: "h"}, Keyword{V: "i"},
	}

	small := NewHashMap()
	for i, k := range keys[:5] {
		var err error
		small, _, err = small.Assoc(k, Int{V: int64(i)})
		if err != nil {
			t.Fatal(err)
		}
	}
	if small.large != nil {
		t.Fatal("expected small form")
	}

	promoted := NewHashMap()
	for i, k := range keys {
		var err error
		promoted, _, err = promoted.Assoc(k, Int{V: int64(i)})
		if err != nil {
			t.Fatal(err)
		}
	}
	if promoted.large == nil {
		t.Fatal("expected promoted form after 9 keys")
	}
	for _, k := range keys[5:] {
		var err error
		promoted, _, err = promoted.Dissoc(k)
		if err != nil {
			t.Fatal(err)
		}
	}
	if promoted.large == nil {
		t.Fatal("dropping below the limit must not demote (hysteresis)")
	}
	if promoted.Len() != 5 {
		t.Fatalf("Len() = %d, want 5", promoted.Len())
	}

	if !small.Equals(promoted) {
		t.Error("small.Equals(promoted) should be true for the same pairs")
	}
	if !promoted.Equals(small) {
		t.Error("promoted.Equals(small) should be true for the same pairs")
	}
}

func TestHashMap_Immutability(t *testing.T) {
	t.Parallel()

	t.Run("small form", func(t *testing.T) {
		t.Parallel()
		m := NewHashMap()
		m, _, err := m.Assoc(Keyword{V: "a"}, Int{V: 1})
		if err != nil {
			t.Fatal(err)
		}
		before := m.Len()

		if _, _, err := m.Assoc(Keyword{V: "b"}, Int{V: 2}); err != nil {
			t.Fatal(err)
		}
		if m.Len() != before {
			t.Error("Assoc mutated the receiver (small form)")
		}

		if _, _, err := m.Dissoc(Keyword{V: "a"}); err != nil {
			t.Fatal(err)
		}
		if m.Len() != before {
			t.Error("Dissoc mutated the receiver (small form)")
		}
		if _, ok := m.Get(Keyword{V: "a"}); !ok {
			t.Error("Dissoc mutated the receiver's underlying data (small form)")
		}
	})

	t.Run("promoted form", func(t *testing.T) {
		t.Parallel()
		m := NewHashMap()
		for i := range hashMapSmallLimit + 1 {
			var err error
			m, _, err = m.Assoc(Int{V: int64(i)}, Int{V: int64(i)})
			if err != nil {
				t.Fatal(err)
			}
		}
		if m.large == nil {
			t.Fatal("expected promoted form")
		}
		before := m.Len()

		if _, _, err := m.Assoc(Int{V: 100}, Int{V: 100}); err != nil {
			t.Fatal(err)
		}
		if m.Len() != before {
			t.Error("Assoc mutated the receiver (promoted form)")
		}

		if _, _, err := m.Dissoc(Int{V: 0}); err != nil {
			t.Fatal(err)
		}
		if m.Len() != before {
			t.Error("Dissoc mutated the receiver (promoted form)")
		}
		if _, ok := m.Get(Int{V: 0}); !ok {
			t.Error("Dissoc mutated the receiver's underlying data (promoted form)")
		}
	})
}

func TestHashMap_Each_AllocsPerRun(t *testing.T) {
	m := NewHashMap()
	for i := range 6 {
		var err error
		m, _, err = m.Assoc(Int{V: int64(i)}, Int{V: int64(i)})
		if err != nil {
			t.Fatal(err)
		}
	}
	allocs := testing.AllocsPerRun(1000, func() {
		m.Each(func(k, v Value) {})
	})
	if allocs != 0 {
		t.Errorf("Each allocs = %v, want 0", allocs)
	}
}

func TestHashMap_Assoc_AllocsPerRun(t *testing.T) {
	m := NewHashMap()
	m, _, err := m.Assoc(Keyword{V: "x"}, Int{V: 1})
	if err != nil {
		t.Fatal(err)
	}
	var key Value = Keyword{V: "y"}
	var val Value = Int{V: 2}
	allocs := testing.AllocsPerRun(1000, func() {
		_, _, _ = m.Assoc(key, val)
	})
	if allocs > 2 {
		t.Errorf("Assoc allocs = %v, want <= 2", allocs)
	}
}

// TestHashMap_TrieMatchesOracle runs a deterministic pseudo-random mix of
// Assoc, Dissoc and Get well past the small-map limit against a plain Go map
// as the oracle. The trie form is the thing under test, so the oracle has to
// be something other than the trie.
func TestHashMap_TrieMatchesOracle(t *testing.T) {
	t.Parallel()

	m := NewHashMap()
	oracle := map[int64]int64{}
	// Fixed multiplier and increment: a repeatable sequence, so a failure
	// reproduces exactly rather than only on an unlucky run.
	seed := int64(1)
	next := func(mod int64) int64 {
		seed = (seed*6364136223846793005 + 1442695040888963407) >> 16
		if seed < 0 {
			seed = -seed
		}
		return seed % mod
	}

	for i := range 20000 {
		k := next(3000)
		switch {
		case i%4 == 3 && len(oracle) > 0:
			var err error
			m, _, err = m.Dissoc(Int{V: k})
			if err != nil {
				t.Fatalf("Dissoc(%d): %v", k, err)
			}
			delete(oracle, k)
		default:
			v := next(1 << 20)
			var err error
			m, _, err = m.Assoc(Int{V: k}, Int{V: v})
			if err != nil {
				t.Fatalf("Assoc(%d): %v", k, err)
			}
			oracle[k] = v
		}

		if m.Len() != len(oracle) {
			t.Fatalf("step %d: Len() = %d, want %d", i, m.Len(), len(oracle))
		}
	}

	for k, want := range oracle {
		got, ok := m.Get(Int{V: k})
		if !ok {
			t.Fatalf("Get(%d) missing, want %d", k, want)
		}
		if !got.Equals(Int{V: want}) {
			t.Fatalf("Get(%d) = %v, want %d", k, got, want)
		}
	}
	for k := range int64(3000) {
		if _, present := oracle[k]; present {
			continue
		}
		if _, ok := m.Get(Int{V: k}); ok {
			t.Fatalf("Get(%d) present, want absent", k)
		}
	}

	// Sorted iteration must still cover exactly the oracle's keys, in order.
	entries := m.sortedEntries()
	if len(entries) != len(oracle) {
		t.Fatalf("sortedEntries() length = %d, want %d", len(entries), len(oracle))
	}
	for i := 1; i < len(entries); i++ {
		if !entries[i-1].hk.less(entries[i].hk) {
			t.Fatalf("sortedEntries() not ordered at %d", i)
		}
	}
}

// setBuiltMap builds a map past hashMapSmallLimit through Set alone, leaving it
// in builder form: staging map populated, trie root still nil. Assoc/Dissoc on
// such a map runs the conversion, which is what these tests measure.
func setBuiltMap(t *testing.T, n int) *HashMap {
	t.Helper()
	if n <= hashMapSmallLimit {
		t.Fatalf("n = %d does not exceed hashMapSmallLimit (%d), builder form unreachable", n, hashMapSmallLimit)
	}
	m := NewHashMap()
	for i := range int64(n) {
		if err := m.Set(Int{V: i}, Int{V: i}); err != nil {
			t.Fatal(err)
		}
	}
	assertBuilderForm(t, m, n)
	return m
}

func assertBuilderForm(t *testing.T, m *HashMap, n int) {
	t.Helper()
	if m.large == nil {
		t.Fatalf("map is small form, want builder form")
	}
	if m.large.root != nil {
		t.Fatalf("map is trie form, want builder form")
	}
	if len(m.large.m) != n {
		t.Fatalf("builder holds %d entries, want %d", len(m.large.m), n)
	}
}

// retainedTrieBytes sums the finished trie's nodes — the floor a conversion
// charge must exceed, since every insert but the last discards its path copy.
func retainedTrieBytes(n *hamtNode) int64 {
	if n == nil {
		return 0
	}
	total := hamtNodeBytes(n)
	for _, c := range n.children {
		total += retainedTrieBytes(c)
	}
	return total
}

// TestHashMap_ConversionBufferUnit isolates the conversion's entry-buffer term
// from the path copies it is charged alongside: replaying production's own
// insertion loop reproduces the path-copy sum alone, so the difference is the
// buffer and nothing else. Storage in this role is priced with the shared
// collection header, so a divergence fails here instead of passing quietly.
func TestHashMap_ConversionBufferUnit(t *testing.T) {
	t.Parallel()

	for _, size := range []struct {
		name string
		n    int
	}{
		{"n=9", 9},
		{"n=100", 100},
		{"n=1000", 1000},
	} {
		t.Run(size.name, func(t *testing.T) {
			t.Parallel()

			m := setBuiltMap(t, size.n)
			assertBuilderForm(t, m, size.n)

			_, charge := m.trieFromBuildMap()

			root := &hamtNode{}
			var pathCopies int64
			for _, e := range m.sortedEntries() {
				next, b, _ := root.assoc(e, hashOfKey(e.hk), 0)
				root, pathCopies = next, pathCopies+b
			}

			want := MeterCollectionHeaderBytes + int64(size.n)*MeterHashMapEntryBytes
			if got := charge - pathCopies; got != want {
				t.Fatalf("conversion buffer term = %d, want %d: the entry buffer is priced with MeterCollectionHeaderBytes plus MeterHashMapEntryBytes per pair", got, want)
			}
		})
	}
}

// TestHashMap_ConversionChargeIsReproducible pins the contract that converting
// one builder-form map charges one number: the ledger may not depend on the
// staging map's iteration order. Conversion is a per-value cost, so only the
// first update against a retained receiver pays it; later updates charge the
// path they copied, and equal contents convert for equal bytes.
func TestHashMap_ConversionChargeIsReproducible(t *testing.T) {
	t.Parallel()

	const repeats = 8
	sizes := []struct {
		name string
		n    int
	}{
		{"n=9", 9},
		{"n=100", 100},
		{"n=1000", 1000},
	}

	requireConvertThenSteady := func(t *testing.T, charges []int64) {
		t.Helper()
		if charges[1] >= charges[0] {
			t.Fatalf("charge after the conversion = %d, want < %d: only the first update converts, every later one charges the path it copied", charges[1], charges[0])
		}
		for i := 2; i < len(charges); i++ {
			if charges[i] != charges[1] {
				t.Fatalf("post-conversion charge on repeat %d = %d, want %d: updates against a converted receiver must charge one number", i, charges[i], charges[1])
			}
		}
	}

	for _, size := range sizes {
		t.Run("assoc/"+size.name, func(t *testing.T) {
			t.Parallel()
			m := setBuiltMap(t, size.n)
			charges := make([]int64, repeats)
			for i := range charges {
				_, bytes, err := m.Assoc(Int{V: -1}, Int{V: -1})
				if err != nil {
					t.Fatal(err)
				}
				charges[i] = bytes
			}
			assertBuilderForm(t, m, size.n)
			t.Logf("assoc %s charges: %v", size.name, charges)
			requireConvertThenSteady(t, charges)
		})

		t.Run("dissoc/"+size.name, func(t *testing.T) {
			t.Parallel()
			m := setBuiltMap(t, size.n)
			charges := make([]int64, repeats)
			for i := range charges {
				_, bytes, err := m.Dissoc(Int{V: 0})
				if err != nil {
					t.Fatal(err)
				}
				charges[i] = bytes
			}
			assertBuilderForm(t, m, size.n)
			t.Logf("dissoc %s charges: %v", size.name, charges)
			requireConvertThenSteady(t, charges)
		})
	}

	t.Run("assoc/colliding keys", func(t *testing.T) {
		t.Parallel()
		a, b := findCollidingKeys(t)
		m := NewHashMap()
		for i := range int64(hashMapSmallLimit + 1) {
			if err := m.Set(Int{V: -(i + 2)}, Int{V: i}); err != nil {
				t.Fatal(err)
			}
		}
		for _, k := range []int64{a, b} {
			if err := m.Set(Int{V: k}, Int{V: k}); err != nil {
				t.Fatal(err)
			}
		}
		n := hashMapSmallLimit + 3
		assertBuilderForm(t, m, n)
		charges := make([]int64, repeats)
		for i := range charges {
			_, bytes, err := m.Assoc(Int{V: -1}, Int{V: -1})
			if err != nil {
				t.Fatal(err)
			}
			charges[i] = bytes
		}
		assertBuilderForm(t, m, n)
		t.Logf("assoc colliding (keys %d, %d) charges: %v", a, b, charges)
		requireConvertThenSteady(t, charges)
	})

	t.Run("assoc/cross-receiver", func(t *testing.T) {
		t.Parallel()
		const n = 100
		first, second := setBuiltMap(t, n), setBuiltMap(t, n)
		_, firstCharge, err := first.Assoc(Int{V: -1}, Int{V: -1})
		if err != nil {
			t.Fatal(err)
		}
		_, secondCharge, err := second.Assoc(Int{V: -1}, Int{V: -1})
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("cross-receiver conversion charges: %d, %d", firstCharge, secondCharge)
		if firstCharge != secondCharge {
			t.Fatalf("conversion charges %d and %d differ: the charge follows the contents, not which value converted", firstCharge, secondCharge)
		}
	})

	for _, size := range sizes[1:] {
		t.Run("honesty/"+size.name, func(t *testing.T) {
			t.Parallel()
			m := setBuiltMap(t, size.n)
			root, charge := m.trieFromBuildMap()
			floor := retainedTrieBytes(root) + HashMapShallowBytes(size.n)
			t.Logf("honesty %s: charge=%d floor=%d", size.name, charge, floor)
			if charge <= floor {
				t.Fatalf("conversion charge = %d, want > %d (retained trie plus entry buffer): the discarded path copies must stay billed", charge, floor)
			}
		})
	}
}

// findCollidingKeys searches for two distinct Int keys whose hashes agree in
// every bit, forcing the trie to bottom out into a collision node. The hash is
// fixed-seed, so the pair this finds is the same on every run.
func findCollidingKeys(t *testing.T) (int64, int64) {
	t.Helper()
	seen := map[uint32]int64{}
	for i := range int64(4_000_000) {
		hk, err := toHashKey(Int{V: i})
		if err != nil {
			t.Fatal(err)
		}
		h := hashOfKey(hk)
		if prev, ok := seen[h]; ok {
			return prev, i
		}
		seen[h] = i
	}
	t.Fatal("no colliding key pair found")
	return 0, 0
}

// TestHashMap_HashCollisions pins the collision-node path. A 32-bit hash
// consumed 5 bits at a time runs out after seven levels, so colliding keys are
// reachable by construction rather than by bad luck, and a trie that kept
// descending past that point would not terminate.
func TestHashMap_HashCollisions(t *testing.T) {
	t.Parallel()

	a, b := findCollidingKeys(t)
	hkA, _ := toHashKey(Int{V: a})
	hkB, _ := toHashKey(Int{V: b})
	if hashOfKey(hkA) != hashOfKey(hkB) {
		t.Fatalf("keys %d and %d do not collide", a, b)
	}

	// Push past the small form so the trie, not the sorted slice, holds them.
	m := NewHashMap()
	for i := range int64(hashMapSmallLimit + 1) {
		var err error
		m, _, err = m.Assoc(Int{V: -(i + 1)}, Int{V: i})
		if err != nil {
			t.Fatal(err)
		}
	}
	if m.large == nil || m.large.root == nil {
		t.Fatal("expected trie form")
	}

	var err error
	m, _, err = m.Assoc(Int{V: a}, Int{V: 100})
	if err != nil {
		t.Fatal(err)
	}
	m, _, err = m.Assoc(Int{V: b}, Int{V: 200})
	if err != nil {
		t.Fatal(err)
	}

	if m.Len() != hashMapSmallLimit+3 {
		t.Fatalf("Len() = %d, want %d — colliding keys must count separately",
			m.Len(), hashMapSmallLimit+3)
	}
	for k, want := range map[int64]int64{a: 100, b: 200} {
		got, ok := m.Get(Int{V: k})
		if !ok || !got.Equals(Int{V: want}) {
			t.Fatalf("Get(%d) = %v, %v; want %d", k, got, ok, want)
		}
	}

	// Removing one collided key must leave the other reachable.
	shrunk, _, err := m.Dissoc(Int{V: a})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := shrunk.Get(Int{V: a}); ok {
		t.Fatalf("Get(%d) still present after Dissoc", a)
	}
	got, ok := shrunk.Get(Int{V: b})
	if !ok || !got.Equals(Int{V: 200}) {
		t.Fatalf("Dissoc of a collided sibling lost key %d: %v, %v", b, got, ok)
	}
	if shrunk.Len() != hashMapSmallLimit+2 {
		t.Fatalf("Len() = %d, want %d", shrunk.Len(), hashMapSmallLimit+2)
	}
	if _, stillThere := m.Get(Int{V: a}); !stillThere {
		t.Fatal("Dissoc mutated the receiver")
	}
}

// TestHamtNode_CollisionAssoc pins the two insert arms inside a collision
// node. Reaching them through Assoc needs three keys sharing a full 32-bit
// hash: a birthday scan finds a colliding pair cheaply, but a third key
// matching one specific hash costs on the order of 2^32 trials, so the node is
// built directly and assoc is driven at the level that owns the arm. The arm
// scans hashKeys and never reads h, so the hash argument is immaterial.
func TestHamtNode_CollisionAssoc(t *testing.T) {
	t.Parallel()

	mk := func(v int64) entry {
		t.Helper()
		hk, err := toHashKey(Int{V: v})
		if err != nil {
			t.Fatal(err)
		}
		return entry{hk: hk, k: Int{V: v}, v: Int{V: v * 10}}
	}
	a, b, c := mk(1), mk(2), mk(3)
	node := &hamtNode{entries: []entry{a, b}}
	if !node.isCollision() {
		t.Fatal("fixture is not a collision node")
	}

	t.Run("rebind-replaces-in-place", func(t *testing.T) {
		out, bytes, added := node.assoc(entry{hk: a.hk, k: a.k, v: Int{V: 99}}, 0, 0)
		if added {
			t.Fatal("added = true, want false — rebinding a key the node holds is not an insert")
		}
		if len(out.entries) != 2 {
			t.Fatalf("len(entries) = %d, want 2 — a rebind must not grow the node", len(out.entries))
		}
		if got, ok := out.get(a.hk, 0, 0); !ok || !got.Equals(Int{V: 99}) {
			t.Fatalf("get(rebound) = %v, %v; want 99", got, ok)
		}
		if got, ok := out.get(b.hk, 0, 0); !ok || !got.Equals(b.v) {
			t.Fatalf("get(sibling) = %v, %v; want %v untouched", got, ok, b.v)
		}
		if want := hamtNodeBytes(out); bytes != want {
			t.Fatalf("charged %d bytes, want %d — the charge is the node it returns", bytes, want)
		}
		if got, ok := node.get(a.hk, 0, 0); !ok || !got.Equals(a.v) {
			t.Fatal("assoc mutated the receiver")
		}
	})

	t.Run("third-key-appends", func(t *testing.T) {
		out, bytes, added := node.assoc(c, 0, 0)
		if !added {
			t.Fatal("added = false, want true — a key the node does not hold is an insert")
		}
		if !out.isCollision() {
			t.Fatal("the node left collision form, want a longer scanned list")
		}
		if len(out.entries) != 3 {
			t.Fatalf("len(entries) = %d, want 3", len(out.entries))
		}
		for _, e := range []entry{a, b, c} {
			if got, ok := out.get(e.hk, 0, 0); !ok || !got.Equals(e.v) {
				t.Fatalf("get(%v) = %v, %v; want %v — every collided key stays reachable", e.k, got, ok, e.v)
			}
		}
		if want := hamtNodeBytes(out); bytes != want {
			t.Fatalf("charged %d bytes, want %d — the charge is the node it returns", bytes, want)
		}
		if len(node.entries) != 2 {
			t.Fatalf("receiver grew to %d entries, want 2", len(node.entries))
		}
	})
}

// TestHashMap_LargeFormPrintsIndependentOfBuildOrder pins the determinism the
// fixed-seed hash exists to protect: two large maps with the same pairs, built
// in different orders, are indistinguishable.
func TestHashMap_LargeFormPrintsIndependentOfBuildOrder(t *testing.T) {
	t.Parallel()

	const n = 200
	forward := NewHashMap()
	backward := NewHashMap()
	for i := range int64(n) {
		var err error
		forward, _, err = forward.Assoc(Int{V: i}, Int{V: i * 2})
		if err != nil {
			t.Fatal(err)
		}
		backward, _, err = backward.Assoc(Int{V: n - 1 - i}, Int{V: (n - 1 - i) * 2})
		if err != nil {
			t.Fatal(err)
		}
	}

	if forward.String() != backward.String() {
		t.Fatal("maps with equal pairs print differently depending on insertion order")
	}
	if !forward.Equals(backward) {
		t.Fatal("maps with equal pairs are not Equals")
	}
	first := forward.String()
	for range 20 {
		if got := forward.String(); got != first {
			t.Fatalf("String() nondeterministic across calls: %s != %s", got, first)
		}
	}
}

// TestHashMap_ConversionChargeIgnoresBuildOrder is the same determinism one
// step further out: equal contents converted from differently ordered builders
// must charge the same bytes.
func TestHashMap_ConversionChargeIgnoresBuildOrder(t *testing.T) {
	t.Parallel()

	sizes := []struct {
		name string
		n    int
	}{
		{"n=9", 9},
		{"n=100", 100},
		{"n=1000", 1000},
	}

	for _, size := range sizes {
		t.Run(size.name, func(t *testing.T) {
			t.Parallel()
			ascending := NewHashMap()
			descending := NewHashMap()
			for i := range int64(size.n) {
				if err := ascending.Set(Int{V: i}, Int{V: i * 2}); err != nil {
					t.Fatal(err)
				}
				j := int64(size.n) - 1 - i
				if err := descending.Set(Int{V: j}, Int{V: j * 2}); err != nil {
					t.Fatal(err)
				}
			}
			assertBuilderForm(t, ascending, size.n)
			assertBuilderForm(t, descending, size.n)

			_, up, err := ascending.Assoc(Int{V: -1}, Int{V: -1})
			if err != nil {
				t.Fatal(err)
			}
			_, down, err := descending.Assoc(Int{V: -1}, Int{V: -1})
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("build order %s charges: ascending=%d descending=%d", size.name, up, down)
			if up != down {
				t.Fatalf("ascending build charges %d, descending charges %d: equal contents must charge equally", up, down)
			}
		})
	}
}

// fanOutSizes straddle hashMapSmallLimit and the sizes the conversion charge
// was measured at.
var fanOutSizes = []int{9, 100, 1000}

// fanOutBaselineCharge is the charge one Assoc against a retained Set-built
// receiver paid before the conversion was retained: the first update still
// pays it, only the ones after it may not.
var fanOutBaselineCharge = map[int]int64{9: 4304, 100: 101264, 1000: 1145784}

const (
	fanOutMaxChargeAfterFirst int64 = 4096
	fanOutMaxAllocsAfterFirst       = 16.0
	fanOutUpdates                   = 8
	fanOutCeilingUpdates            = 64
	fanOutCeilingTotal        int64 = 2 << 20
	fanOutFirstLedgerFloor    int64 = 500000
	// fanOutLaterLedgerCeiling bounds the ledger delta over the
	// fanOutUpdates-1 = 7 updates that follow the converting one on an n=1000
	// receiver. Seven path copies of a 1000-entry trie intrinsically cost about
	// 10.2 KB: a trie-form n=1000 map with no conversion in play charges 1448
	// bytes for one update, so seven charge 10136 — the measured delta here is
	// 10216. The ceiling keeps the separation the shape is meant to show, 1145936
	// bytes for the first update against 10216 for the rest, and leaves headroom
	// over the measured value rather than tracking it.
	fanOutLaterLedgerCeiling int64 = 16384
)

func withinOneTenth(got, want int64) bool {
	delta := got - want
	if delta < 0 {
		delta = -delta
	}
	return delta*10 <= want
}

// TestHashMap_FanOutAssocStaysBounded drives k updates against one retained
// builder-form receiver — the fan-out shape, where every call starts from the
// same map. Repeated updates on a bulk-built map do not re-pay its conversion:
// the first update bears it, the rest are bounded by the trie's depth.
func TestHashMap_FanOutAssocStaysBounded(t *testing.T) {
	second := map[int]int64{}
	for _, n := range fanOutSizes {
		t.Run("builder/n="+strconv.Itoa(n), func(t *testing.T) {
			m := setBuiltMap(t, n)
			probe := Int{V: -1}

			_, first, err := m.Assoc(probe, probe)
			if err != nil {
				t.Fatal(err)
			}
			if want := fanOutBaselineCharge[n]; !withinOneTenth(first, want) {
				t.Errorf("first Assoc charge = %d, want within 10%% of %d: the first toucher still pays the conversion", first, want)
			}

			for i := range fanOutUpdates {
				key := Int{V: int64(-2 - i)}
				_, charge, err := m.Assoc(key, key)
				if err != nil {
					t.Fatal(err)
				}
				if i == 0 {
					second[n] = charge
				}
				if charge > fanOutMaxChargeAfterFirst {
					t.Errorf("update %d charges %d bytes, want <= %d: a repeated update re-pays the conversion", i+2, charge, fanOutMaxChargeAfterFirst)
				}
			}

			allocs := testing.AllocsPerRun(100, func() {
				_, _, _ = m.Assoc(probe, probe)
			})
			if allocs > fanOutMaxAllocsAfterFirst {
				t.Errorf("Assoc allocs after the conversion = %v, want <= %v", allocs, fanOutMaxAllocsAfterFirst)
			}
			assertBuilderForm(t, m, n)
		})
	}

	// The trie arm is the control: it never converts, so it already meets the
	// bounds the builder arm has to reach.
	for _, n := range fanOutSizes {
		t.Run("trie/n="+strconv.Itoa(n), func(t *testing.T) {
			m := assocBuiltMap(t, n)
			probe := Int{V: -1}
			for i := range fanOutUpdates {
				key := Int{V: int64(-2 - i)}
				_, charge, err := m.Assoc(key, key)
				if err != nil {
					t.Fatal(err)
				}
				if charge > fanOutMaxChargeAfterFirst {
					t.Errorf("update %d charges %d bytes, want <= %d", i+1, charge, fanOutMaxChargeAfterFirst)
				}
			}
			allocs := testing.AllocsPerRun(100, func() {
				_, _, _ = m.Assoc(probe, probe)
			})
			if allocs > fanOutMaxAllocsAfterFirst {
				t.Errorf("Assoc allocs = %v, want <= %v", allocs, fanOutMaxAllocsAfterFirst)
			}
		})
	}

	if len(second) != len(fanOutSizes) {
		return
	}
	lo, hi := second[fanOutSizes[0]], second[fanOutSizes[0]]
	for _, n := range fanOutSizes {
		if second[n] < lo {
			lo = second[n]
		}
		if second[n] > hi {
			hi = second[n]
		}
	}
	if lo <= 0 || hi > 4*lo {
		t.Errorf("second-update charge spans %d..%d across n = %v, want a spread of at most 4x: the charge still scales with map size", lo, hi, fanOutSizes)
	}

}

// assocBuiltMap builds a map past hashMapSmallLimit through Assoc alone,
// leaving it in trie form: the arm that never converts.
func assocBuiltMap(t *testing.T, n int) *HashMap {
	t.Helper()
	if n <= hashMapSmallLimit {
		t.Fatalf("n = %d does not exceed hashMapSmallLimit (%d), trie form unreachable", n, hashMapSmallLimit)
	}
	m := NewHashMap()
	for i := range int64(n) {
		var err error
		m, _, err = m.Assoc(Int{V: i}, Int{V: i})
		if err != nil {
			t.Fatal(err)
		}
	}
	if m.large == nil || m.large.root == nil {
		t.Fatalf("map is %s form, want trie", mapForm(m))
	}
	return m
}

// TestHashMap_FanOutDissocStaysBounded is the Assoc case at Dissoc's identical
// conversion call site.
func TestHashMap_FanOutDissocStaysBounded(t *testing.T) {
	for _, n := range fanOutSizes {
		t.Run("builder/n="+strconv.Itoa(n), func(t *testing.T) {
			m := setBuiltMap(t, n)
			victim := Int{V: 0}

			_, first, err := m.Dissoc(victim)
			if err != nil {
				t.Fatal(err)
			}
			if first <= 0 {
				t.Errorf("first Dissoc charge = %d, want > 0: the conversion is still paid once", first)
			}

			for i := range fanOutUpdates {
				_, charge, err := m.Dissoc(Int{V: int64(i)})
				if err != nil {
					t.Fatal(err)
				}
				if charge > fanOutMaxChargeAfterFirst {
					t.Errorf("update %d charges %d bytes, want <= %d: a repeated update re-pays the conversion", i+2, charge, fanOutMaxChargeAfterFirst)
				}
			}

			allocs := testing.AllocsPerRun(100, func() {
				_, _, _ = m.Dissoc(victim)
			})
			if allocs > fanOutMaxAllocsAfterFirst {
				t.Errorf("Dissoc allocs after the conversion = %v, want <= %v", allocs, fanOutMaxAllocsAfterFirst)
			}
			assertBuilderForm(t, m, n)
		})
	}

	for _, n := range fanOutSizes {
		t.Run("trie/n="+strconv.Itoa(n), func(t *testing.T) {
			m := assocBuiltMap(t, n)
			victim := Int{V: 0}
			for i := range fanOutUpdates {
				_, charge, err := m.Dissoc(Int{V: int64(i)})
				if err != nil {
					t.Fatal(err)
				}
				if charge > fanOutMaxChargeAfterFirst {
					t.Errorf("update %d charges %d bytes, want <= %d", i+1, charge, fanOutMaxChargeAfterFirst)
				}
			}
			allocs := testing.AllocsPerRun(100, func() {
				_, _, _ = m.Dissoc(victim)
			})
			if allocs > fanOutMaxAllocsAfterFirst {
				t.Errorf("Dissoc allocs = %v, want <= %v", allocs, fanOutMaxAllocsAfterFirst)
			}
		})
	}
}

// TestHashMap_FanOutFromMapLiteralStaysBounded reaches builder form through
// the reader instead of Set: a map literal past the small limit is promoted
// into the same staging map, so it converts on its first update too.
func TestHashMap_FanOutFromMapLiteralStaysBounded(t *testing.T) {
	t.Parallel()

	for _, n := range fanOutSizes {
		t.Run("n="+strconv.Itoa(n), func(t *testing.T) {
			forms, err := Read(pairSource(n))
			if err != nil {
				t.Fatalf("reading a %d-pair map literal: %v", n, err)
			}
			if len(forms) != 1 {
				t.Fatalf("read %d forms, want 1", len(forms))
			}
			m, ok := forms[0].(*HashMap)
			if !ok {
				t.Fatalf("map literal read as %T, want *HashMap", forms[0])
			}
			assertBuilderForm(t, m, n)

			probe := Keyword{V: "probe"}
			if _, _, err := m.Assoc(probe, Int{V: 1}); err != nil {
				t.Fatal(err)
			}
			for i := range fanOutUpdates {
				key := Keyword{V: "probe" + strconv.Itoa(i)}
				_, charge, err := m.Assoc(key, Int{V: 1})
				if err != nil {
					t.Fatal(err)
				}
				if charge > fanOutMaxChargeAfterFirst {
					t.Errorf("update %d charges %d bytes, want <= %d: a promoted literal re-pays its conversion", i+2, charge, fanOutMaxChargeAfterFirst)
				}
			}
			assertBuilderForm(t, m, n)
		})
	}
}

// TestHashMap_FanOutUnderDefaultAllocationCeiling runs the fan-out through the
// evaluation ledger at its default ceiling. Paying the conversion per update
// exhausts the ceiling long before the loop ends.
func TestHashMap_FanOutUnderDefaultAllocationCeiling(t *testing.T) {
	t.Parallel()

	m := setBuiltMap(t, 1000)
	ctx := WithEvalResourceLimits(context.Background(), int(DefaultMaxReductions), int(DefaultMaxAllocationBytes))

	var total int64
	for i := range fanOutCeilingUpdates {
		key := Int{V: int64(-1 - i)}
		_, charge, err := m.Assoc(key, key)
		if err != nil {
			t.Fatal(err)
		}
		total += charge
		if err := ChargeEvalAllocBytes(ctx, charge); err != nil {
			t.Fatalf("update %d of %d exhausted the default allocation ceiling after %d bytes: %v", i+1, fanOutCeilingUpdates, total, err)
		}
	}
	if total > fanOutCeilingTotal {
		t.Errorf("%d updates against one receiver charge %d bytes, want <= %d", fanOutCeilingUpdates, total, fanOutCeilingTotal)
	}
}

// TestHashMap_MemoHoldsConvertedTrie pins the retained conversion itself: one
// update publishes it, it matches the staging map's contents, and no later
// update replaces it.
func TestHashMap_MemoHoldsConvertedTrie(t *testing.T) {
	t.Parallel()

	for _, n := range fanOutSizes {
		t.Run("n="+strconv.Itoa(n), func(t *testing.T) {
			t.Parallel()
			m := setBuiltMap(t, n)
			if m.large.memo.Load() != nil {
				t.Fatalf("a Set-built map already carries a converted trie before any update")
			}

			if _, _, err := m.Assoc(Int{V: -1}, Int{V: -1}); err != nil {
				t.Fatal(err)
			}
			memo := m.large.memo.Load()
			if memo == nil {
				t.Fatalf("the first update published no converted trie: every later update re-pays the conversion")
			}
			if m.large.root != nil {
				t.Fatalf("the receiver was promoted to trie form; the update must leave the receiver's storage alone")
			}
			if m.large.m == nil {
				t.Fatalf("the receiver lost its staging map")
			}
			if form := mapForm(m); form != "builder" {
				t.Errorf("mapForm = %s, want builder", form)
			}

			held := 0
			memo.each(func(e entry) {
				held++
				if _, ok := m.large.m[e.hk]; !ok {
					t.Errorf("converted trie holds key %v the staging map does not", e.k)
				}
			})
			if held != len(m.large.m) {
				t.Errorf("converted trie holds %d entries, want %d", held, len(m.large.m))
			}

			if _, _, err := m.Assoc(Int{V: -2}, Int{V: -2}); err != nil {
				t.Fatal(err)
			}
			if again := m.large.memo.Load(); again != memo {
				t.Errorf("a later update replaced the converted trie; it must be published exactly once per value")
			}
		})
	}

	t.Run("concurrent", func(t *testing.T) {
		t.Parallel()
		const goroutines, updates = 8, 32
		const n = 1000
		m := setBuiltMap(t, n)

		var wg sync.WaitGroup
		for g := range goroutines {
			wg.Add(1)
			go func(g int) {
				defer wg.Done()
				for i := range updates {
					key := Int{V: int64(-1 - (g*updates + i))}
					if _, _, err := m.Assoc(key, key); err != nil {
						t.Errorf("goroutine %d update %d: %v", g, i, err)
						return
					}
				}
			}(g)
		}
		wg.Wait()

		memo := m.large.memo.Load()
		if memo == nil {
			t.Fatalf("%d concurrent updates published no converted trie", goroutines*updates)
		}
		held := 0
		memo.each(func(entry) { held++ })
		if held != n {
			t.Errorf("converted trie holds %d entries, want %d", held, n)
		}
		assertBuilderForm(t, m, n)
	})
}

// TestHashMap_MemoLeavesReadPathOnBuilderForm checks the retained conversion
// stays invisible: every read still answers from the staging map, so a
// memoised map is indistinguishable from a plain builder-form one.
func TestHashMap_MemoLeavesReadPathOnBuilderForm(t *testing.T) {
	const n = 100
	memoised := setBuiltMap(t, n)
	plain := setBuiltMap(t, n)

	if _, _, err := memoised.Assoc(Int{V: -1}, Int{V: -1}); err != nil {
		t.Fatal(err)
	}
	if memoised.large.memo.Load() == nil {
		t.Fatalf("the first update published no converted trie; the read path has nothing to stay off")
	}

	pairs := make([][2]Value, 0, n)
	for i := range int64(n) {
		pairs = append(pairs, [2]Value{Int{V: i}, Int{V: i}})
	}
	assertMapParity(t, memoised, plain, pairs, Int{V: -1})

	memoisedAllocs := testing.AllocsPerRun(100, func() {
		memoised.Each(func(k, v Value) {})
	})
	plainAllocs := testing.AllocsPerRun(100, func() {
		plain.Each(func(k, v Value) {})
	})
	if memoisedAllocs != plainAllocs {
		t.Errorf("Each allocs = %v on a memoised map, %v on a plain builder map: the read path must stay on the staging map", memoisedAllocs, plainAllocs)
	}
}

// TestHashMap_ConversionChargedOncePerValue pins first-toucher-pays at the
// ledger: two meters update one shared builder-form map and exactly one of
// them sees the conversion.
func TestHashMap_ConversionChargedOncePerValue(t *testing.T) {
	t.Parallel()

	const n = 1000
	m := setBuiltMap(t, n)

	firstCtx, firstMeter := allocCeilingContext(64 << 20)
	laterCtx, laterMeter := allocCeilingContext(64 << 20)

	charge := func(ctx context.Context, key int64) int64 {
		t.Helper()
		_, bytes, err := m.Assoc(Int{V: key}, Int{V: key})
		if err != nil {
			t.Fatal(err)
		}
		if err := ChargeEvalAllocBytes(ctx, bytes); err != nil {
			t.Fatalf("charging %d bytes for key %d: %v", bytes, key, err)
		}
		return bytes
	}

	conversion := charge(firstCtx, -1)
	total := conversion
	for i := 1; i < fanOutUpdates; i++ {
		total += charge(laterCtx, int64(-1-i))
	}

	if got := admittedBytes(firstMeter); got < fanOutFirstLedgerFloor {
		t.Errorf("first toucher admitted %d bytes, want >= %d: it must bear the conversion", got, fanOutFirstLedgerFloor)
	}
	if got := admittedBytes(laterMeter); got > fanOutLaterLedgerCeiling {
		t.Errorf("later updates admitted %d bytes, want <= %d: converting is a per-value cost, not a per-update one", got, fanOutLaterLedgerCeiling)
	}
	if want := conversion + fanOutUpdates*fanOutMaxChargeAfterFirst; total > want {
		t.Errorf("%d updates against one receiver charge %d bytes total, want <= %d", fanOutUpdates, total, want)
	}
}
