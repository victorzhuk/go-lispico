package core

import "testing"

func counterTry(t *testing.T, what string, write func() error) {
	t.Helper()
	defer func() {
		if p := recover(); p != nil {
			t.Fatalf("%s panicked instead of forwarding the write to the root", what)
		}
	}()
	if err := write(); err != nil {
		t.Fatalf("%s: %v", what, err)
	}
}

func counterEntry(t *testing.T, reg *Registration, name string, fn bool) *registrationEntry {
	t.Helper()
	ent, ok := reg.entries[registrationKey{name: name, fn: fn}]
	if !ok || ent == nil {
		t.Fatalf("no journal entry for %q (function namespace %v); want one recorded by the view write", name, fn)
	}
	return ent
}

func counterBegin(t *testing.T, root *Env) *Registration {
	t.Helper()
	reg, err := root.BeginRegistration()
	if err != nil {
		t.Fatalf("BeginRegistration on an idle root: %v", err)
	}
	return reg
}

func counterSeed(t *testing.T, what string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
}

type counterReleaseMeter struct {
	releasedBytes int64
	releasedSlots int64
}

func (m *counterReleaseMeter) LeaseEval(reductions, allocBytes int64) (int64, int64, error) {
	return reductions, allocBytes, nil
}

func (m *counterReleaseMeter) ReturnEval(_, _ int64) {}

func (m *counterReleaseMeter) ChargeRetained(_, _ int64) error { return nil }

func (m *counterReleaseMeter) ReleaseRetained(bytes, slots int64) {
	m.releasedBytes += bytes
	m.releasedSlots += slots
}

func counterWantUnchanged(t *testing.T, root *Env, gen uint64, epoch int, what string) {
	t.Helper()
	if got := root.NameGen(); got != gen {
		t.Errorf("%s: root NameGen = %d; want %d unchanged", what, got, gen)
	}
	if got := root.MacroEpoch(); got != epoch {
		t.Errorf("%s: root MacroEpoch = %d; want %d unchanged", what, got, epoch)
	}
}

func counterWantBumped(t *testing.T, root *Env, gen uint64, epoch int, what string) {
	t.Helper()
	if got := root.NameGen(); got != gen+1 {
		t.Errorf("%s: root NameGen = %d; want %d, exactly one bump past the pre-abort %d", what, got, gen+1, gen)
	}
	if got := root.MacroEpoch(); got != epoch+1 {
		t.Errorf("%s: root MacroEpoch = %d; want %d, exactly one bump past the pre-abort %d", what, got, epoch+1, epoch)
	}
}

func TestRegistration_AbortInvalidatesCellCaches(t *testing.T) {
	t.Parallel()
	root := NewEnv(nil)
	counterSeed(t, "seed x", root.Set("x", Int{V: 1}))
	held, ok := root.Cell("x")
	if !ok {
		t.Fatalf("root.Cell(x) found nothing after seeding x")
	}

	reg := counterBegin(t, root)
	view := reg.Env()
	counterTry(t, "view.Set(x)", func() error { return view.Set("x", Int{V: 2}) })
	counterTry(t, "view.Set(n)", func() error { return view.Set("n", Int{V: 3}) })
	opVer := held.Version()
	gen := root.NameGen()
	reg.Abort()

	if cur, ok := root.Cell("x"); !ok || cur != held {
		t.Errorf("after abort root.Cell(x) = %p (found %v); want the held cell %p", cur, ok, held)
	}
	v, live, _, ver := root.ReadCellSnapshot(held)
	if !live || v != (Int{V: 1}) {
		t.Errorf("after abort the held cell for x reads %v (live %v); want the prior 1", v, live)
	}
	if ver <= opVer {
		t.Errorf("after abort the held cell version is %d; want it past the op write's %d", ver, opVer)
	}
	if got := root.NameGen(); got != gen+1 {
		t.Errorf("after a restoring abort root NameGen = %d; want %d, exactly one bump past the pre-abort %d so cached resolutions re-check", got, gen+1, gen)
	}
}

func TestRegistration_AbortBumpsMacroEpoch(t *testing.T) {
	t.Parallel()
	root := NewEnv(nil)
	root.SetEvaluator(NewEvaluator())
	reg := counterBegin(t, root)
	view := reg.Env()
	form, err := ReadOne("(defmacro m [a] a)")
	if err != nil {
		t.Fatalf("read defmacro: %v", err)
	}
	counterTry(t, "(defmacro m [a] a) through the view", func() error {
		_, err := view.Evaluator().Eval(t.Context(), form, view)
		return err
	})
	epoch := root.MacroEpoch()
	reg.Abort()

	if root.HasLive("m") {
		t.Errorf("after abort the op-defined macro m is still live on the root; want it removed")
	}
	if got := root.MacroEpoch(); got != epoch+1 {
		t.Errorf("after an abort that removed macro m root MacroEpoch = %d; want %d, exactly one bump past the pre-abort %d", got, epoch+1, epoch)
	}
}

func TestRegistration_AbortWithoutOwnedEntriesKeepsCounters(t *testing.T) {
	t.Parallel()
	root := NewEnv(nil)
	counterSeed(t, "seed x", root.Set("x", Int{V: 1}))
	counterSeed(t, "seed y", root.Set("y", Int{V: 1}))

	empty := counterBegin(t, root)
	gen, epoch := root.NameGen(), root.MacroEpoch()
	empty.Abort()
	counterWantUnchanged(t, root, gen, epoch, "abort of an op with no writes")

	foreign := counterBegin(t, root)
	counterTry(t, "view.Set(x)", func() error { return foreign.Env().Set("x", Int{V: 2}) })
	counterSeed(t, "raw root.Set(x) after the op write", root.Set("x", Int{V: 3}))
	gen, epoch = root.NameGen(), root.MacroEpoch()
	foreign.Abort()
	counterWantUnchanged(t, root, gen, epoch, "abort whose only entry the host took over")
	if got, ok := root.Get("x"); !ok || got != (Int{V: 3}) {
		t.Errorf("after abort root x = %v (bound %v); want the host's 3", got, ok)
	}

	owned := counterBegin(t, root)
	counterTry(t, "view.Set(y)", func() error { return owned.Env().Set("y", Int{V: 2}) })
	gen, epoch = root.NameGen(), root.MacroEpoch()
	owned.Abort()
	counterWantBumped(t, root, gen, epoch, "abort restoring the op-owned y")
	if got, ok := root.Get("y"); !ok || got != (Int{V: 1}) {
		t.Errorf("after abort root y = %v (bound %v); want the prior 1", got, ok)
	}
}

func TestRegistration_RebuildDuringOperationKeepsOwnedTombstone(t *testing.T) {
	t.Parallel()
	meter := &counterReleaseMeter{}
	root := NewEnv(nil)
	root.SetRetainedMeter(meter)
	counterSeed(t, "seed x", root.Set("x", Int{V: 1}))
	counterSeed(t, "seed y", root.Set("y", Int{V: 1}))
	counterSeed(t, "seed z", root.Set("z", Int{V: 1}))
	xc, yc, zc := root.vars["x"], root.vars["y"], root.vars["z"]

	reg := counterBegin(t, root)
	view := reg.Env()
	counterTry(t, "view.Delete(x)", func() error { view.Delete("x"); return nil })
	counterTry(t, "view.Set(n)", func() error { return view.Set("n", Int{V: 4}) })
	counterTry(t, "view.Delete(n)", func() error { view.Delete("n"); return nil })
	root.Delete("y")
	nc := root.vars["n"]
	for name, cell := range map[string]*Cell{"x": xc, "n": nc} {
		if ent := counterEntry(t, reg, name, false); ent.last != cell {
			t.Fatalf("journal entry for %s records last cell %p; want the tombstoned map cell %p", name, ent.last, cell)
		}
	}
	wantBytes := zc.retainedBytes + xc.retainedBytes + nc.retainedBytes
	root.Rebuild()

	for name, cell := range map[string]*Cell{"x": xc, "n": nc} {
		if cur := root.vars[name]; cur != cell {
			t.Errorf("Rebuild during the op left root cell %p for %s; want the op's tombstone %p pinned in the map", cur, name, cell)
		}
		if cell.rebuilt {
			t.Errorf("Rebuild during the op marked the pinned tombstone for %s rebuilt; want it untouched", name)
		}
	}
	if cur, ok := root.vars["y"]; ok {
		t.Errorf("Rebuild during the op kept the unjournaled tombstone for y (%p, own cell %p); want it dropped", cur, yc)
	}
	if meter.releasedSlots != 1 || meter.releasedBytes != yc.retainedBytes {
		t.Errorf("Rebuild during the op released %d slots / %d bytes; want only y's 1 slot / %d bytes, pinned tombstones kept charged", meter.releasedSlots, meter.releasedBytes, yc.retainedBytes)
	}
	if bytes, slots := root.RetainedUsage(); slots != 3 || bytes != wantBytes {
		t.Errorf("after Rebuild during the op root retained usage = %d bytes / %d slots; want %d / 3, live z plus the two pinned tombstones", bytes, slots, wantBytes)
	}

	reg.Abort()
	if cur := root.vars["x"]; cur != xc {
		t.Errorf("after abort the root cell for x is %p; want the pinned cell %p restored in place", cur, xc)
	}
	if got, ok := root.Get("x"); !ok || got != (Int{V: 1}) {
		t.Errorf("after abort root x = %v (bound %v); want the deleted 1 restored", got, ok)
	}

	root.Rebuild()
	if _, ok := root.vars["n"]; ok {
		t.Errorf("Rebuild after the op ended kept the aborted addition n; want its tombstone dropped")
	}
	if cur := root.vars["x"]; cur != xc {
		t.Errorf("Rebuild after the op ended left root cell %p for x; want the live restored cell %p", cur, xc)
	}
}
