package core

import (
	"slices"
	"testing"
)

var registrationNames = (*Registration).Names

func TestRegistration_NamesListsValueCellWrites(t *testing.T) {
	t.Parallel()
	root := NewEnv(nil)
	if err := root.Set("d", Int{V: 1}); err != nil {
		t.Fatalf("seed d: %v", err)
	}

	reg := beginViewRegistration(t, root)
	if got := registrationNames(reg); got != nil {
		t.Errorf("Names before any write = %v; want nil", got)
	}
	view := reg.Env()
	for _, w := range []struct {
		what string
		err  error
	}{
		{"view.Set(a)", view.Set("a", Int{V: 2})},
		{"view.SetCanonical(b)", view.SetCanonical("b", Int{V: 3})},
		{"view.Set(x)", view.Set("x", Int{V: 4})},
		{"view.SetFunc(f)", view.SetFunc("f", GoFunc{Name: "f"})},
		{"root.Set(r)", root.Set("r", Int{V: 5})},
	} {
		if w.err != nil {
			t.Fatalf("%s: %v", w.what, w.err)
		}
	}
	view.Delete("d")
	view.Delete("x")

	got := slices.Clone(registrationNames(reg))
	slices.Sort(got)
	if want := []string{"a", "b", "d", "x"}; !slices.Equal(got, want) {
		t.Errorf("Names = %v; want %v (view value-cell writes, each once; SetFunc-only and root writes absent)", got, want)
	}

	reg.Complete()
	if got := registrationNames(reg); got != nil {
		t.Errorf("Names after Complete = %v; want nil", got)
	}

	aborted := beginViewRegistration(t, root)
	if err := aborted.Env().Set("y", Int{V: 6}); err != nil {
		t.Fatalf("view.Set(y): %v", err)
	}
	aborted.Abort()
	if got := registrationNames(aborted); got != nil {
		t.Errorf("Names after Abort = %v; want nil", got)
	}
}
