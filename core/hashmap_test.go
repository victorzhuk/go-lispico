package core

import (
	"math"
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
