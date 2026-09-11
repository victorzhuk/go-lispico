package core

import (
	"errors"
	"slices"
	"testing"
)

func requireRegistrationRefused(t *testing.T, err error, what string) {
	t.Helper()
	var lerr *LispicoError
	if !errors.As(err, &lerr) || lerr.Code != CodeRegistrationActive {
		t.Fatalf("%s: error = %v, want a *LispicoError with Code %s", what, err, CodeRegistrationActive)
	}
}

func beginViewRegistration(t *testing.T, root *Env) *Registration {
	t.Helper()
	reg, err := root.BeginRegistration()
	if err != nil {
		t.Fatalf("BeginRegistration on an idle root: %v", err)
	}
	return reg
}

func TestRegistration_ViewForwardsReadsAndKeepsRootIdentity(t *testing.T) {
	t.Parallel()
	root := NewEnv(nil)
	eval := depthLimitEvaluator{limit: 7}
	root.SetEvaluator(eval)
	for name, val := range map[string]Value{"x": Int{V: 1}, "y": String{V: "why"}} {
		if err := root.Set(name, val); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	if err := root.SetCanonical("c", Int{V: 2}); err != nil {
		t.Fatalf("seed c: %v", err)
	}
	if err := root.SetFunc("f", Int{V: 3}); err != nil {
		t.Fatalf("seed f: %v", err)
	}
	root.BumpMacroEpoch()

	reg := beginViewRegistration(t, root)
	view := reg.Env()
	if view == root {
		t.Fatalf("Registration.Env() returned the root itself; want a distinct view forwarding to the root")
	}

	if got, ok := view.Get("x"); !ok || got != (Int{V: 1}) {
		t.Errorf("view.Get(x) = %v, %v; want 1, true as on the root", got, ok)
	}
	if got, ok, canon := view.GetCanonical("c"); !ok || !canon || got != (Int{V: 2}) {
		t.Errorf("view.GetCanonical(c) = %v, %v, %v; want 2, true, true as on the root", got, ok, canon)
	}
	if got, ok := view.GetFunc("f"); !ok || got != (Int{V: 3}) {
		t.Errorf("view.GetFunc(f) = %v, %v; want 3, true as on the root", got, ok)
	}
	rootCell, _ := root.Cell("x")
	if cell, ok := view.Cell("x"); !ok || cell != rootCell {
		t.Errorf("view.Cell(x) = %p, %v; want the root's cell %p", cell, ok, rootCell)
	}
	wantNames := root.LocalNames()
	slices.Sort(wantNames)
	gotNames := view.LocalNames()
	slices.Sort(gotNames)
	if !slices.Equal(gotNames, wantNames) {
		t.Errorf("view.LocalNames() = %v; want the root's %v", gotNames, wantNames)
	}
	if !view.HasLive("x") {
		t.Errorf("view.HasLive(x) = false; want true as on the root")
	}
	if got, want := view.NameGen(), root.NameGen(); got != want {
		t.Errorf("view.NameGen() = %d; want the root's %d", got, want)
	}
	if got, want := view.MacroEpoch(), root.MacroEpoch(); got != want {
		t.Errorf("view.MacroEpoch() = %d; want the root's %d", got, want)
	}
	if got := view.Evaluator(); got != Evaluator(eval) {
		t.Errorf("view.Evaluator() = %v; want the root's evaluator %v", got, eval)
	}
	gotBytes, gotSlots := view.RetainedUsage()
	wantBytes, wantSlots := root.RetainedUsage()
	if gotBytes != wantBytes || gotSlots != wantSlots {
		t.Errorf("view.RetainedUsage() = %d, %d; want the root's %d, %d", gotBytes, gotSlots, wantBytes, wantSlots)
	}

	if err := root.Set("x", Int{V: 10}); err != nil {
		t.Fatalf("raw root rebind of x: %v", err)
	}
	if got, ok := view.Get("x"); !ok || got != (Int{V: 10}) {
		t.Errorf("after a raw root rebind, view.Get(x) = %v, %v; want 10, true", got, ok)
	}

	reg.Complete()
	if err := root.Set("x", Int{V: 11}); err != nil {
		t.Fatalf("raw root rebind of x after completion: %v", err)
	}
	if got, ok := view.Get("x"); !ok || got != (Int{V: 11}) {
		t.Errorf("after completion and a raw root rebind, view.Get(x) = %v, %v; want 11, true", got, ok)
	}
	if got, want := view.NameGen(), root.NameGen(); got != want {
		t.Errorf("after completion view.NameGen() = %d; want the root's %d", got, want)
	}
}

func TestRegistration_NestedBeginRefused(t *testing.T) {
	t.Parallel()
	root := NewEnv(nil)
	reg1 := beginViewRegistration(t, root)

	_, err := root.BeginRegistration()
	requireRegistrationRefused(t, err, "second root.BeginRegistration while reg1 is active")
	_, err = reg1.Env().BeginRegistration()
	requireRegistrationRefused(t, err, "reg1.Env().BeginRegistration while reg1 is active")

	reg1.Complete()
	reg2, err := root.BeginRegistration()
	if err != nil || reg2 == nil {
		t.Fatalf("root.BeginRegistration after reg1.Complete() = %v, %v; want a new registration", reg2, err)
	}
	reg2.Complete()
}

func TestRegistration_CompleteAndAbortAreIdempotent(t *testing.T) {
	t.Parallel()
	root := NewEnv(nil)
	reg1 := beginViewRegistration(t, root)
	reg1.Complete()
	reg1.Abort()
	reg1.Complete()

	reg2, err := root.BeginRegistration()
	if err != nil || reg2 == nil {
		t.Fatalf("root.BeginRegistration after reg1 finished = %v, %v; want a new registration", reg2, err)
	}
	reg1.Abort()
	reg1.Complete()
	_, err = root.BeginRegistration()
	requireRegistrationRefused(t, err, "root.BeginRegistration after stale reg1.Abort/Complete while reg2 is active (reg2 must stay active)")

	reg2.Abort()
	reg3, err := root.BeginRegistration()
	if err != nil || reg3 == nil {
		t.Fatalf("root.BeginRegistration after reg2.Abort() = %v, %v; want a new registration", reg3, err)
	}
	reg3.Complete()
}

func TestRegistration_ViewGetZeroAllocs(t *testing.T) {
	root := NewEnv(nil)
	if err := root.Set("x", Int{V: 1}); err != nil {
		t.Fatalf("seed x: %v", err)
	}
	reg := beginViewRegistration(t, root)
	defer reg.Complete()
	view := reg.Env()
	if view == root {
		t.Fatalf("Registration.Env() returned the root itself; want a distinct view so the alloc count measures the forwarding path")
	}
	allocs := testing.AllocsPerRun(100, func() {
		if _, ok := view.Get("x"); !ok {
			panic("view.Get(x) missed")
		}
	})
	if allocs != 0 {
		t.Fatalf("view.Get(x) allocs = %v; want 0", allocs)
	}
}
