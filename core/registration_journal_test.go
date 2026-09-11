package core

import "testing"

func journalTry(t *testing.T, what string, write func() error) error {
	t.Helper()
	defer func() {
		if p := recover(); p != nil {
			t.Fatalf("%s panicked (%T) instead of forwarding the write to the root", what, p)
		}
	}()
	return write()
}

func journalWrite(t *testing.T, what string, write func() error) {
	t.Helper()
	if err := journalTry(t, what, write); err != nil {
		t.Fatalf("%s: %v", what, err)
	}
}

func journalNS(fn bool) string {
	if fn {
		return "function"
	}
	return "value"
}

func journalEntry(t *testing.T, reg *Registration, name string, fn bool) *registrationEntry {
	t.Helper()
	ent, ok := reg.entries[registrationKey{name: name, fn: fn}]
	if !ok {
		t.Fatalf("no journal entry for %q in the %s namespace; want one recorded by the view write", name, journalNS(fn))
	}
	if ent == nil {
		t.Fatalf("journal entry for %q in the %s namespace is nil; want a recorded before-image", name, journalNS(fn))
	}
	return ent
}

func journalNoEntry(t *testing.T, reg *Registration, name, what string) {
	t.Helper()
	for _, fn := range []bool{false, true} {
		if _, ok := reg.entries[registrationKey{name: name, fn: fn}]; ok {
			t.Errorf("%s left a journal entry for %q in the %s namespace; want none", what, name, journalNS(fn))
		}
	}
}

func journalMapCell(root *Env, name string, fn bool) *Cell {
	if fn {
		return root.funcs[name]
	}
	return root.vars[name]
}

func beginJournal(t *testing.T, root *Env) *Registration {
	t.Helper()
	reg, err := root.BeginRegistration()
	if err != nil {
		t.Fatalf("BeginRegistration on an idle root: %v", err)
	}
	return reg
}

func TestRegistration_ViewHoldsNoBindings(t *testing.T) {
	t.Parallel()
	one := Value(Int{V: 1})
	tests := []struct {
		name      string
		seed      func(root *Env) error
		write     func(view *Env) error
		fns       []bool
		wantV     Value
		wantCanon bool
	}{
		{name: "Set", write: func(v *Env) error { return v.Set("x", one) }, fns: []bool{false}, wantV: one},
		{name: "SetCanonical", write: func(v *Env) error { return v.SetCanonical("x", one) }, fns: []bool{false}, wantV: one, wantCanon: true},
		{name: "SetFunc", write: func(v *Env) error { return v.SetFunc("x", one) }, fns: []bool{true}, wantV: one},
		{name: "SetFuncCanonical", write: func(v *Env) error { return v.SetFuncCanonical("x", one) }, fns: []bool{true}, wantV: one, wantCanon: true},
		{name: "SetBoth", write: func(v *Env) error { return v.SetBoth("x", one) }, fns: []bool{false, true}, wantV: one},
		{name: "SetBothCanonical", write: func(v *Env) error { return v.SetBothCanonical("x", one) }, fns: []bool{false, true}, wantV: one, wantCanon: true},
		{
			name:  "ReplaceCell",
			seed:  func(r *Env) error { return r.Set("x", Int{V: 0}) },
			write: func(v *Env) error { return v.ReplaceCell("x", one) },
			fns:   []bool{false},
			wantV: one,
		},
		{
			name:  "Delete",
			seed:  func(r *Env) error { return r.Set("x", Int{V: 0}) },
			write: func(v *Env) error { v.Delete("x"); return nil },
			fns:   []bool{false},
		},
		{name: "RegisterValue canonical", write: func(v *Env) error { return v.RegisterValue("x", one, true) }, fns: []bool{false}, wantV: one, wantCanon: true},
		{name: "RegisterValue", write: func(v *Env) error { return v.RegisterValue("x", one, false) }, fns: []bool{false}, wantV: one},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := NewEnv(nil)
			if tt.seed != nil {
				if err := tt.seed(root); err != nil {
					t.Fatalf("seed: %v", err)
				}
			}
			reg := beginJournal(t, root)
			view := reg.Env()
			journalWrite(t, "view."+tt.name, func() error { return tt.write(view) })

			if view.vars != nil || view.funcs != nil {
				t.Errorf("view holds binding maps after view.%s (vars %d, funcs %d); want both nil with the write landed on the root",
					tt.name, len(view.vars), len(view.funcs))
			}
			for _, fn := range tt.fns {
				cell := journalMapCell(root, "x", fn)
				if cell == nil {
					t.Errorf("root has no %s cell for x after view.%s; want the write landed on the root", journalNS(fn), tt.name)
					continue
				}
				if cell.v != tt.wantV || cell.canonical != tt.wantCanon {
					t.Errorf("root %s cell for x after view.%s = %v (canonical %v); want %v (canonical %v)",
						journalNS(fn), tt.name, cell.v, cell.canonical, tt.wantV, tt.wantCanon)
				}
			}
		})
	}
}

func TestRegistration_ViewWriteRecordsBeforeImage(t *testing.T) {
	t.Parallel()
	valX := registrationKey{name: "x"}
	fnX := registrationKey{name: "x", fn: true}
	two := Value(Int{V: 2})
	liveX := func(r *Env) error { return r.Set("x", Int{V: 1}) }
	tests := []struct {
		name  string
		seed  func(root *Env) error
		write func(view *Env) error
		keys  []registrationKey
	}{
		{name: "Set over a live name", seed: liveX, write: func(v *Env) error { return v.Set("x", two) }, keys: []registrationKey{valX}},
		{name: "Set of a new name", write: func(v *Env) error { return v.Set("x", two) }, keys: []registrationKey{valX}},
		{
			name: "Set over a tombstone",
			seed: func(r *Env) error {
				if err := r.Set("x", Int{V: 1}); err != nil {
					return err
				}
				r.Delete("x")
				return nil
			},
			write: func(v *Env) error { return v.Set("x", two) },
			keys:  []registrationKey{valX},
		},
		{name: "SetCanonical over a live name", seed: liveX, write: func(v *Env) error { return v.SetCanonical("x", two) }, keys: []registrationKey{valX}},
		{
			name:  "SetFunc over a live name",
			seed:  func(r *Env) error { return r.SetFunc("x", Int{V: 1}) },
			write: func(v *Env) error { return v.SetFunc("x", two) },
			keys:  []registrationKey{fnX},
		},
		{
			name:  "SetFuncCanonical over a live name",
			seed:  func(r *Env) error { return r.SetFunc("x", Int{V: 1}) },
			write: func(v *Env) error { return v.SetFuncCanonical("x", two) },
			keys:  []registrationKey{fnX},
		},
		{name: "SetBoth", seed: liveX, write: func(v *Env) error { return v.SetBoth("x", two) }, keys: []registrationKey{valX, fnX}},
		{name: "ReplaceCell over a live name", seed: liveX, write: func(v *Env) error { return v.ReplaceCell("x", two) }, keys: []registrationKey{valX}},
		{name: "Delete of a live name", seed: liveX, write: func(v *Env) error { v.Delete("x"); return nil }, keys: []registrationKey{valX}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := NewEnv(nil)
			if tt.seed != nil {
				if err := tt.seed(root); err != nil {
					t.Fatalf("seed: %v", err)
				}
			}
			type image struct {
				cell      *Cell
				v         Value
				canonical bool
			}
			before := make(map[registrationKey]image, len(tt.keys))
			for _, k := range tt.keys {
				img := image{cell: journalMapCell(root, k.name, k.fn)}
				if img.cell != nil {
					img.v, img.canonical = img.cell.v, img.cell.canonical
				}
				before[k] = img
			}

			reg := beginJournal(t, root)
			view := reg.Env()
			journalWrite(t, "view "+tt.name, func() error { return tt.write(view) })

			for _, k := range tt.keys {
				ns := journalNS(k.fn)
				ent := journalEntry(t, reg, k.name, k.fn)
				img := before[k]
				if ent.prior != img.cell {
					t.Errorf("%s entry prior = %p; want the pre-op map cell %p", ns, ent.prior, img.cell)
				}
				if ent.v != img.v || ent.canonical != img.canonical {
					t.Errorf("%s entry before-image = %v (canonical %v); want %v (canonical %v)", ns, ent.v, ent.canonical, img.v, img.canonical)
				}
				cur := journalMapCell(root, k.name, k.fn)
				if ent.last != cur {
					t.Errorf("%s entry last = %p; want the current map cell %p", ns, ent.last, cur)
				}
				if ent.last != nil && ent.lastVer != ent.last.Version() {
					t.Errorf("%s entry lastVer = %d; want last.Version() %d", ns, ent.lastVer, ent.last.Version())
				}
			}
		})
	}

	t.Run("no entry for a raw root write", func(t *testing.T) {
		t.Parallel()
		root := NewEnv(nil)
		if err := root.Set("x", Int{V: 1}); err != nil {
			t.Fatalf("seed x: %v", err)
		}
		reg := beginJournal(t, root)
		view := reg.Env()
		if err := root.Set("x", two); err != nil {
			t.Fatalf("raw root.Set(x): %v", err)
		}
		journalWrite(t, "view.Set(y)", func() error { return view.Set("y", two) })
		journalEntry(t, reg, "y", false)
		journalNoEntry(t, reg, "x", "a raw root.Set(x)")
	})

	t.Run("no entry for a view Delete of an absent name", func(t *testing.T) {
		t.Parallel()
		root := NewEnv(nil)
		reg := beginJournal(t, root)
		view := reg.Env()
		journalWrite(t, "view.Delete(ghost)", func() error { view.Delete("ghost"); return nil })
		journalWrite(t, "view.Set(y)", func() error { return view.Set("y", two) })
		journalEntry(t, reg, "y", false)
		journalNoEntry(t, reg, "ghost", "view.Delete of an absent name")
	})

	t.Run("no entry for a view write refused by capacity", func(t *testing.T) {
		t.Parallel()
		root := NewEnvWithRetainedLimits(nil, 0, 1)
		reg := beginJournal(t, root)
		view := reg.Env()
		journalWrite(t, "view.Set(y)", func() error { return view.Set("y", two) })
		err := journalTry(t, "view.Set(z)", func() error { return view.Set("z", two) })
		requireRetainedLimitError(t, err)
		journalEntry(t, reg, "y", false)
		journalNoEntry(t, reg, "z", "a view.Set refused by capacity")
	})

	t.Run("no entry for a finished view under another registration", func(t *testing.T) {
		t.Parallel()
		root := NewEnv(nil)
		reg1 := beginJournal(t, root)
		view1 := reg1.Env()
		reg1.Complete()
		reg2 := beginJournal(t, root)
		journalWrite(t, "finished view1.Set(x)", func() error { return view1.Set("x", two) })
		journalWrite(t, "reg2 view.Set(y)", func() error { return reg2.Env().Set("y", two) })
		journalEntry(t, reg2, "y", false)
		journalNoEntry(t, reg2, "x", "a write through the finished reg1 view")
	})
}

func TestRegistration_HostEqualValueRebindIsUnowned(t *testing.T) {
	t.Parallel()
	root := NewEnv(nil)
	reg := beginJournal(t, root)
	view := reg.Env()
	v := Value(Int{V: 5})
	journalWrite(t, "view.Set(x)", func() error { return view.Set("x", v) })
	journalWrite(t, "view.Set(y)", func() error { return view.Set("y", v) })
	if err := root.Set("x", v); err != nil {
		t.Fatalf("raw root.Set(x) with the op's value: %v", err)
	}

	entX := journalEntry(t, reg, "x", false)
	cellX := root.vars["x"]
	if entX.last != cellX {
		t.Errorf("x entry last = %p; want the map cell %p", entX.last, cellX)
	}
	if cellX != nil && entX.lastVer == cellX.Version() {
		t.Errorf("x entry lastVer = %d equals the cell version after an equal-value raw root rebind; want a mismatch marking the host write unowned", entX.lastVer)
	}
	entY := journalEntry(t, reg, "y", false)
	if cellY := root.vars["y"]; cellY == nil || entY.lastVer != cellY.Version() {
		t.Errorf("y entry lastVer = %d; want the cell version of y, written only through the view", entY.lastVer)
	}
}

func TestRegistration_RegisterValueThroughViewIsAttributed(t *testing.T) {
	t.Parallel()
	root := NewEnv(nil)
	if err := root.Set("old", Int{V: 1}); err != nil {
		t.Fatalf("seed old: %v", err)
	}
	oldCell := root.vars["old"]
	reg := beginJournal(t, root)
	view := reg.Env()
	journalWrite(t, "view.RegisterValue(old, canonical)", func() error { return view.RegisterValue("old", Int{V: 2}, true) })
	journalWrite(t, "view.RegisterValue(fresh)", func() error { return view.RegisterValue("fresh", Int{V: 3}, false) })

	if got, ok, canon := root.GetCanonical("old"); !ok || !canon || got != (Int{V: 2}) {
		t.Errorf("root.GetCanonical(old) = %v, %v, %v; want 2, true, true", got, ok, canon)
	}
	if got, ok, canon := root.GetCanonical("fresh"); !ok || canon || got != (Int{V: 3}) {
		t.Errorf("root.GetCanonical(fresh) = %v, %v, %v; want 3, true, false", got, ok, canon)
	}

	entOld := journalEntry(t, reg, "old", false)
	if entOld.prior != oldCell || entOld.v != (Int{V: 1}) || entOld.canonical {
		t.Errorf("old entry before-image = %p, %v (canonical %v); want the existing cell %p, 1 (canonical false)",
			entOld.prior, entOld.v, entOld.canonical, oldCell)
	}
	entFresh := journalEntry(t, reg, "fresh", false)
	if entFresh.prior != nil || entFresh.v != nil || entFresh.canonical {
		t.Errorf("fresh entry before-image = %p, %v (canonical %v); want nil prior, nil value (canonical false)",
			entFresh.prior, entFresh.v, entFresh.canonical)
	}
}

func TestRegistration_RawRebindDuringOperationZeroAllocs(t *testing.T) {
	root := NewEnv(nil)
	reg := beginJournal(t, root)
	view := reg.Env()
	journalWrite(t, "view.Set(x)", func() error { return view.Set("x", Int{V: 1}) })
	if n := len(reg.entries); n != 1 {
		t.Fatalf("after one view write to x the journal holds %d entries; want 1", n)
	}

	v := Value(Int{V: 9})
	var setErr error
	allocs := testing.AllocsPerRun(100, func() {
		if err := root.Set("x", v); err != nil {
			setErr = err
		}
	})
	if setErr != nil {
		t.Fatalf("raw root.Set(x): %v", setErr)
	}
	if allocs != 0 {
		t.Errorf("raw root.Set(x) during an active registration allocs = %v; want 0", allocs)
	}
	if n := len(reg.entries); n != 1 {
		t.Errorf("after raw root rebinds the journal holds %d entries; want it to stay at 1", n)
	}
}

func TestRegistration_JournalBoundedByDistinctNames(t *testing.T) {
	t.Parallel()
	root := NewEnv(nil)
	reg := beginJournal(t, root)
	view := reg.Env()
	for i := range 1000 {
		journalWrite(t, "view.Set(x)", func() error { return view.Set("x", Int{V: int64(i)}) })
	}
	journalWrite(t, "view.Set(y)", func() error { return view.Set("y", Int{V: -1}) })

	if n := len(reg.entries); n != 2 {
		t.Fatalf("after 1000 view rebinds of x and one of y the journal holds %d entries; want 2", n)
	}
	entX := journalEntry(t, reg, "x", false)
	if cellX := root.vars["x"]; cellX == nil || entX.lastVer != cellX.Version() {
		t.Errorf("x entry lastVer = %d; want the cell version after the last view rebind", entX.lastVer)
	}
}
