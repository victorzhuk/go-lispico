package core

import (
	"sync"
	"testing"
)

// TestHashMap_InvalidKeyPublishesNoMemo pins the key-domain refusal. toHashKey
// runs before any use of large storage, so the update never reaches the
// conversion: no trie is built, none is published, and nothing is charged.
func TestHashMap_InvalidKeyPublishesNoMemo(t *testing.T) {
	const n = 100
	bad := NewList([]Value{Int{V: 1}})

	t.Run("Assoc", func(t *testing.T) {
		m := setBuiltMap(t, n)

		got, bytes, err := m.Assoc(bad, Int{V: 1})
		if err == nil {
			t.Fatal("Assoc accepted an unhashable key, want an error")
		}
		if got != nil {
			t.Errorf("Assoc returned map %v alongside its error, want nil", got)
		}
		if bytes != 0 {
			t.Errorf("Assoc charged %d bytes, want 0: the key is rejected before the conversion runs", bytes)
		}
		if m.large.memo.Load() != nil {
			t.Errorf("a rejected Assoc published a converted trie; the refusal precedes the conversion, so there is nothing to publish")
		}
		assertBuilderForm(t, m, n)
	})

	t.Run("Dissoc", func(t *testing.T) {
		m := setBuiltMap(t, n)

		got, bytes, err := m.Dissoc(bad)
		if err == nil {
			t.Fatal("Dissoc accepted an unhashable key, want an error")
		}
		if got != nil {
			t.Errorf("Dissoc returned map %v alongside its error, want nil", got)
		}
		if bytes != 0 {
			t.Errorf("Dissoc charged %d bytes, want 0: the key is rejected before the conversion runs", bytes)
		}
		if m.large.memo.Load() != nil {
			t.Errorf("a rejected Dissoc published a converted trie; the refusal precedes the conversion, so there is nothing to publish")
		}
		assertBuilderForm(t, m, n)
	})
}

// TestHashMap_ConcurrentUpdatesPublishOneMemo fans out updates off one
// builder-form receiver. Publication is a single atomic store of a locally
// built root, so no partially built trie is ever observable: every result must
// carry the receiver's contents plus its own key, the receiver itself must stay
// in builder form, and the published trie must survive later updates unchanged.
func TestHashMap_ConcurrentUpdatesPublishOneMemo(t *testing.T) {
	const (
		n         = 1000
		workers   = 8
		perWorker = 32
	)
	m := setBuiltMap(t, n)

	results := make([][]*HashMap, workers)
	var wg sync.WaitGroup
	for w := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got := make([]*HashMap, perWorker)
			for j := range perWorker {
				key := Int{V: int64(n + w*perWorker + j)}
				next, _, err := m.Assoc(key, key)
				if err != nil {
					t.Errorf("worker %d: Assoc(%v) = %v", w, key, err)
					return
				}
				got[j] = next
			}
			results[w] = got
		}()
	}
	wg.Wait()

	for w := range workers {
		if results[w] == nil {
			t.Fatalf("worker %d produced no results", w)
		}
		for j, got := range results[w] {
			key := Int{V: int64(n + w*perWorker + j)}
			if got == nil {
				t.Fatalf("worker %d: Assoc(%v) produced no map", w, key)
			}
			assertMapFormInvariants(t, "concurrent result", got)
			if got.Len() != n+1 {
				t.Fatalf("worker %d: result Len() = %d, want %d: the update must carry the receiver's entries plus its own key", w, got.Len(), n+1)
			}
			if v, ok := got.Get(key); !ok || !v.Equals(key) {
				t.Fatalf("worker %d: Get(%v) = %v, %v; want %v, true", w, key, v, ok, key)
			}
			for _, existing := range []Int{{V: 0}, {V: n / 2}, {V: n - 1}} {
				if v, ok := got.Get(existing); !ok || !v.Equals(existing) {
					t.Fatalf("worker %d: result lost receiver key %v: Get = %v, %v", w, existing, v, ok)
				}
			}
		}
	}

	if form := mapForm(m); form != "builder" {
		t.Errorf("receiver mapForm = %s, want builder: the conversion leaves the receiver's own storage untouched", form)
	}
	if m.Len() != n {
		t.Errorf("receiver Len() = %d, want %d: the updates are copy-on-write", m.Len(), n)
	}
	assertBuilderForm(t, m, n)

	memo := m.large.memo.Load()
	if memo == nil {
		t.Fatal("the fan-out published no converted trie; the next update would convert again")
	}
	if _, _, err := m.Assoc(Int{V: -1}, Int{V: -1}); err != nil {
		t.Fatal(err)
	}
	if again := m.large.memo.Load(); again != memo {
		t.Errorf("a later update replaced the published trie; the conversion is published once and then reused")
	}
}
