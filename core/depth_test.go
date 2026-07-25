package core

import (
	"strings"
	"testing"
)

func nestedList(depth int) Value {
	var v Value = Int{V: 1}
	for range depth {
		v = NewList([]Value{v})
	}
	return v
}

func TestValueWalksBoundOverDeepValues(t *testing.T) {
	v := nestedList(DefaultMaxStructuralDepth + 200)

	got := v.String()
	if !strings.Contains(got, "...") {
		t.Fatalf("String() = %q, want truncation marker", got)
	}
	if v.Equals(nestedList(DefaultMaxStructuralDepth + 200)) {
		t.Fatal("Equals() = true, want bounded false")
	}
	if bytes := ValueDeepBytes(v); bytes <= 0 {
		t.Fatalf("ValueDeepBytes() = %d, want capped positive count", bytes)
	}
	if nodes := ValueNodeCount(v); nodes <= 0 || nodes > DefaultMaxStructuralDepth+1 {
		t.Fatalf("ValueNodeCount() = %d, want capped positive count", nodes)
	}
}

// TestSharedListDepthIsNotChainLength guards against conflating a shared
// list's chain length with structural depth: a long flat list of scalars
// nests one level deep regardless of how many elements its chain walks
// through, so it must stay under a depth limit far smaller than its length.
func TestSharedListDepthIsNotChainLength(t *testing.T) {
	items := make([]Value, 5000)
	for i := range items {
		items[i] = Int{V: int64(i)}
	}
	list := NewList(items)
	if ValueDepthExceeds(list, 10) {
		t.Fatal("ValueDepthExceeds() = true, want false: chain length must not count as structural depth")
	}
}

func TestValueWalksPreserveShallowValues(t *testing.T) {
	items := make([]Value, 100)
	parts := make([]string, 100)
	for i := range items {
		items[i] = Int{V: int64(i)}
		parts[i] = items[i].String()
	}
	list := NewList(items)
	want := "(" + strings.Join(parts, " ") + ")"
	if got := list.String(); got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
	if !list.Equals(NewList(append([]Value(nil), items...))) {
		t.Fatal("Equals() = false, want true")
	}
	if !(NewVector(items)).Equals(NewVector(append([]Value(nil), items...))) {
		t.Fatal("Vector.Equals() = false, want true")
	}
	m := NewHashMap()
	m2 := NewHashMap()
	for i := range 16 {
		if err := m.Set(Int{V: int64(i)}, Int{V: int64(i + 1)}); err != nil {
			t.Fatalf("Set: %v", err)
		}
		if err := m2.Set(Int{V: int64(i)}, Int{V: int64(i + 1)}); err != nil {
			t.Fatalf("Set: %v", err)
		}
	}
	if !m.Equals(m2) {
		t.Fatal("HashMap.Equals() = false, want true")
	}
}
