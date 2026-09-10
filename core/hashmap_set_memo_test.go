package core

import "testing"

// setMemoisedMap returns a builder-form map carrying a published converted
// trie, reached the only legal way: n Sets, then exactly one Assoc.
func setMemoisedMap(t *testing.T, n int) *HashMap {
	t.Helper()
	m := setBuiltMap(t, n)
	if _, _, err := m.Assoc(Int{V: -1}, Int{V: -1}); err != nil {
		t.Fatal(err)
	}
	if m.large.memo.Load() == nil {
		t.Fatalf("the seeding Assoc published no converted trie; there is no memo for Set to clear")
	}
	assertBuilderForm(t, m, n)
	return m
}

// TestHashMap_SetClearsMemo walks every state a Set can land in. Only one of
// them has a converted trie to invalidate; the rest must stay untouched, and a
// Set rejected before it reaches storage must leave the memo standing.
func TestHashMap_SetClearsMemo(t *testing.T) {
	const n = 100
	newKey, newVal := Int{V: 1000}, Int{V: 2000}

	t.Run("builder form with a memo", func(t *testing.T) {
		m := setMemoisedMap(t, n)
		if err := m.Set(newKey, newVal); err != nil {
			t.Fatal(err)
		}
		if memo := m.large.memo.Load(); memo != nil {
			t.Errorf("Set left the pre-Set converted trie published; the next Assoc builds its result from a trie missing key %v", newKey)
		}
		assertBuilderForm(t, m, n+1)
	})

	t.Run("builder form with no memo", func(t *testing.T) {
		m := setBuiltMap(t, n)
		if err := m.Set(newKey, newVal); err != nil {
			t.Fatal(err)
		}
		if memo := m.large.memo.Load(); memo != nil {
			t.Errorf("Set published a converted trie on a map that had none; only Assoc and Dissoc convert")
		}
		assertBuilderForm(t, m, n+1)
	})

	t.Run("trie form", func(t *testing.T) {
		m := assocBuiltMap(t, n)
		if err := m.Set(newKey, newVal); err != nil {
			t.Fatal(err)
		}
		if memo := m.large.memo.Load(); memo != nil {
			t.Errorf("a trie-form map carries a converted trie; it never converts, so there is nothing to memoise")
		}
		if form := mapForm(m); form != "trie" {
			t.Errorf("mapForm = %s, want trie: Set on a trie-form map updates it in place", form)
		}
		if m.Len() != n+1 {
			t.Errorf("Len() = %d, want %d", m.Len(), n+1)
		}
	})

	t.Run("promotion out of small form", func(t *testing.T) {
		m := NewHashMap()
		for i := range int64(hashMapSmallLimit + 1) {
			if err := m.Set(Int{V: i}, Int{V: i}); err != nil {
				t.Fatal(err)
			}
		}
		if m.large.memo.Load() != nil {
			t.Errorf("the promoting Set installed a large form that already carries a converted trie")
		}
		assertBuilderForm(t, m, hashMapSmallLimit+1)
	})

	t.Run("rejected key keeps the memo", func(t *testing.T) {
		m := setMemoisedMap(t, n)
		memo := m.large.memo.Load()
		if err := m.Set(NewHashMap(), newVal); err == nil {
			t.Fatal("Set accepted an unhashable key, want an error")
		}
		if again := m.large.memo.Load(); again != memo {
			t.Errorf("a rejected Set dropped the converted trie; it never reached storage, so nothing it holds went stale")
		}
		assertBuilderForm(t, m, n)
	})

	t.Run("rejected key with no memo", func(t *testing.T) {
		m := setBuiltMap(t, n)
		if err := m.Set(NewHashMap(), newVal); err == nil {
			t.Fatal("Set accepted an unhashable key, want an error")
		}
		if m.large.memo.Load() != nil {
			t.Errorf("a rejected Set published a converted trie")
		}
		assertBuilderForm(t, m, n)
	})

	t.Run("allocs", func(t *testing.T) {
		memoised := setMemoisedMap(t, n)
		plain := setBuiltMap(t, n)
		var key Value = Int{V: 0}
		var val Value = Int{V: 0}

		memoisedAllocs := testing.AllocsPerRun(100, func() {
			_ = memoised.Set(key, val)
		})
		plainAllocs := testing.AllocsPerRun(100, func() {
			_ = plain.Set(key, val)
		})
		if memoisedAllocs != plainAllocs {
			t.Errorf("Set allocs = %v on a memoised map, %v on a plain builder map: invalidating the converted trie is one atomic store, not an allocation", memoisedAllocs, plainAllocs)
		}
		if memoisedAllocs != 0 {
			t.Errorf("Set allocs = %v, want 0", memoisedAllocs)
		}
	})
}

// TestHashMap_SetThenAssocMatchesConversionFreePath is the observable defect a
// stale converted trie produces: an Assoc taken after a Set builds its result
// from the pre-Set trie and silently loses the key the Set added. The control
// reaches the same contents without ever memoising a conversion.
func TestHashMap_SetThenAssocMatchesConversionFreePath(t *testing.T) {
	const n = 100
	newKey, newVal := Int{V: 1000}, Int{V: 2000}
	probe := Int{V: -2}

	m := setMemoisedMap(t, n)
	if err := m.Set(newKey, newVal); err != nil {
		t.Fatal(err)
	}
	got, _, err := m.Assoc(probe, probe)
	if err != nil {
		t.Fatal(err)
	}

	control := NewHashMap()
	for i := range int64(n) {
		if err := control.Set(Int{V: i}, Int{V: i}); err != nil {
			t.Fatal(err)
		}
	}
	if err := control.Set(newKey, newVal); err != nil {
		t.Fatal(err)
	}
	assertBuilderForm(t, control, n+1)
	if control.large.memo.Load() != nil {
		t.Fatal("the control map memoised a conversion before its only Assoc")
	}
	want, _, err := control.Assoc(probe, probe)
	if err != nil {
		t.Fatal(err)
	}

	if v, ok := got.Get(newKey); !ok || !v.Equals(newVal) {
		t.Errorf("Get(%v) = %v, %v on the Set-then-Assoc result; want %v, true: the Assoc built from a converted trie that predates the Set, so the Set is lost", newKey, v, ok, newVal)
	}
	if got.Len() != want.Len() {
		t.Errorf("Len() = %d, conversion-free path = %d", got.Len(), want.Len())
	}
	if !got.Equals(want) || !want.Equals(got) {
		t.Errorf("Set-then-Assoc result is not Equals the conversion-free path")
	}

	pairs := make([][2]Value, 0, n+2)
	for i := range int64(n) {
		pairs = append(pairs, [2]Value{Int{V: i}, Int{V: i}})
	}
	pairs = append(pairs, [2]Value{newKey, newVal}, [2]Value{probe, probe})
	assertMapParity(t, got, want, pairs, Int{V: -99})
}
