package core

import (
	"slices"
	"testing"
)

// TestDialect_Forms_FullBaseKernelNames pins scenario 1: the identity dialect
// enumerates exactly the kernel's special-form names, sorted.
func TestDialect_Forms_FullBaseKernelNames(t *testing.T) {
	got := Dialect{}.Forms()
	want := make([]string, 0, len(kernel))
	for name := range kernel {
		want = append(want, name)
	}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("Dialect{}.Forms() = %v, want the kernel name set %v", got, want)
	}
	if !slices.IsSorted(got) {
		t.Fatalf("Dialect{}.Forms() = %v, want a sorted slice", got)
	}
}

// TestDialect_Forms_EmptyBaseListsOnlySpecified pins scenario 3: an empty-base
// dialect exposes only the forms its spec maps.
func TestDialect_Forms_EmptyBaseListsOnlySpecified(t *testing.T) {
	d := mustDialect(t, spec{
		Base:  emptyBase,
		Forms: map[string]string{"if": "if", "let": "let"},
	})
	got := d.Forms()
	if !slices.Equal(got, []string{"if", "let"}) {
		t.Fatalf("d.Forms() = %v, want [if let]", got)
	}
}
