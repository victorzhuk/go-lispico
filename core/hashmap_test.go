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
		m, err := m.Assoc(Int{V: 1}, String{V: "int"})
		if err != nil {
			t.Fatal(err)
		}
		m, err = m.Assoc(Float{V: 1.0}, String{V: "float"})
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
		m, err := m.Assoc(Float{V: math.NaN()}, String{V: "nan"})
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
		m, err := m.Assoc(Float{V: 0}, String{V: "zero"})
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
		m, err = m.Assoc(Int{V: int64(i)}, Int{V: int64(i * 2)})
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
		m, err = m.Assoc(Int{V: int64(i)}, Int{V: int64(i * 10)})
		if err != nil {
			t.Fatalf("Assoc(%d) error: %v", i, err)
		}
	}
	if m.m != nil {
		t.Fatal("map at the limit should still be in small form")
	}

	m9, err := m.Assoc(Int{V: hashMapSmallLimit}, Int{V: hashMapSmallLimit * 10})
	if err != nil {
		t.Fatalf("Assoc(9th key) error: %v", err)
	}
	if m9.m == nil {
		t.Fatal("the 9th distinct key should promote to map form")
	}
	if m9.Len() != hashMapSmallLimit+1 {
		t.Fatalf("Len() = %d, want %d", m9.Len(), hashMapSmallLimit+1)
	}
	if m.m != nil || m.Len() != hashMapSmallLimit {
		t.Fatal("Assoc must not mutate the receiver while promoting")
	}

	shrunk, err := m9.Dissoc(Int{V: hashMapSmallLimit})
	if err != nil {
		t.Fatalf("Dissoc error: %v", err)
	}
	if shrunk.m == nil {
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
		small, err = small.Assoc(k, Int{V: int64(i)})
		if err != nil {
			t.Fatal(err)
		}
	}
	if small.m != nil {
		t.Fatal("expected small form")
	}

	promoted := NewHashMap()
	for i, k := range keys {
		var err error
		promoted, err = promoted.Assoc(k, Int{V: int64(i)})
		if err != nil {
			t.Fatal(err)
		}
	}
	if promoted.m == nil {
		t.Fatal("expected promoted form after 9 keys")
	}
	for _, k := range keys[5:] {
		var err error
		promoted, err = promoted.Dissoc(k)
		if err != nil {
			t.Fatal(err)
		}
	}
	if promoted.m == nil {
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
		m, err := m.Assoc(Keyword{V: "a"}, Int{V: 1})
		if err != nil {
			t.Fatal(err)
		}
		before := m.Len()

		if _, err := m.Assoc(Keyword{V: "b"}, Int{V: 2}); err != nil {
			t.Fatal(err)
		}
		if m.Len() != before {
			t.Error("Assoc mutated the receiver (small form)")
		}

		if _, err := m.Dissoc(Keyword{V: "a"}); err != nil {
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
			m, err = m.Assoc(Int{V: int64(i)}, Int{V: int64(i)})
			if err != nil {
				t.Fatal(err)
			}
		}
		if m.m == nil {
			t.Fatal("expected promoted form")
		}
		before := m.Len()

		if _, err := m.Assoc(Int{V: 100}, Int{V: 100}); err != nil {
			t.Fatal(err)
		}
		if m.Len() != before {
			t.Error("Assoc mutated the receiver (promoted form)")
		}

		if _, err := m.Dissoc(Int{V: 0}); err != nil {
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
		m, err = m.Assoc(Int{V: int64(i)}, Int{V: int64(i)})
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
	m, err := m.Assoc(Keyword{V: "x"}, Int{V: 1})
	if err != nil {
		t.Fatal(err)
	}
	var key Value = Keyword{V: "y"}
	var val Value = Int{V: 2}
	allocs := testing.AllocsPerRun(1000, func() {
		_, _ = m.Assoc(key, val)
	})
	if allocs > 2 {
		t.Errorf("Assoc allocs = %v, want <= 2", allocs)
	}
}
