package core

import (
	"fmt"
	"sync"
	"testing"
)

func abortTry(t *testing.T, what string, write func() error) {
	t.Helper()
	defer func() {
		if p := recover(); p != nil {
			t.Fatalf("%s panicked (%T) instead of forwarding the write to the root", what, p)
		}
	}()
	if err := write(); err != nil {
		t.Fatalf("%s: %v", what, err)
	}
}

func abortNS(fn bool) string {
	if fn {
		return "function"
	}
	return "value"
}

func abortMapCell(root *Env, name string, fn bool) *Cell {
	if fn {
		return root.funcs[name]
	}
	return root.vars[name]
}

func abortBegin(t *testing.T, root *Env) *Registration {
	t.Helper()
	reg, err := root.BeginRegistration()
	if err != nil {
		t.Fatalf("BeginRegistration on an idle root: %v", err)
	}
	return reg
}

func abortSeed(t *testing.T, what string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
}

func abortWantValue(t *testing.T, root *Env, name string, want Value, what string) {
	t.Helper()
	got, ok := root.Get(name)
	if !ok || got != want {
		t.Errorf("%s: root %s = %v (bound %v); want %v", what, name, got, ok, want)
	}
}

func abortWantAbsent(t *testing.T, root *Env, name, what string) {
	t.Helper()
	if got, ok := root.Get(name); ok {
		t.Errorf("%s: root %s = %v; want it unbound", what, name, got)
	}
}

func TestRegistration_AbortRestoresOverwrittenBindings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		seed  func(root *Env) error
		write func(view *Env) error
		fns   []bool
	}{
		{
			name:  "Set",
			seed:  func(r *Env) error { return r.Set("x", Int{V: 1}) },
			write: func(v *Env) error { return v.Set("x", Int{V: 2}) },
			fns:   []bool{false},
		},
		{
			name:  "SetFunc",
			seed:  func(r *Env) error { return r.SetFunc("x", Int{V: 1}) },
			write: func(v *Env) error { return v.SetFunc("x", Int{V: 2}) },
			fns:   []bool{true},
		},
		{
			name:  "SetBoth",
			seed:  func(r *Env) error { return r.SetBoth("x", Int{V: 1}) },
			write: func(v *Env) error { return v.SetBoth("x", Int{V: 2}) },
			fns:   []bool{false, true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := NewEnv(nil)
			abortSeed(t, "seed x", tt.seed(root))
			cells := make(map[bool]*Cell, len(tt.fns))
			for _, fn := range tt.fns {
				cells[fn] = abortMapCell(root, "x", fn)
			}

			reg := abortBegin(t, root)
			abortTry(t, "view."+tt.name+"(x)", func() error { return tt.write(reg.Env()) })
			opVer := make(map[bool]uint64, len(tt.fns))
			for _, fn := range tt.fns {
				opVer[fn] = cells[fn].Version()
			}
			reg.Abort()

			for _, fn := range tt.fns {
				ns := abortNS(fn)
				cur := abortMapCell(root, "x", fn)
				if cur != cells[fn] {
					t.Errorf("after abort the root %s cell for x is %p; want the original cell %p kept in place", ns, cur, cells[fn])
					continue
				}
				if cur.v != (Int{V: 1}) {
					t.Errorf("after abort the root %s cell for x holds %v; want the prior value 1", ns, cur.v)
				}
				if v := cur.Version(); v <= opVer[fn] {
					t.Errorf("after abort the root %s cell version for x is %d; want it past the op write's version %d", ns, v, opVer[fn])
				}
			}
		})
	}
}

func TestRegistration_AbortRemovesAddedNames(t *testing.T) {
	t.Parallel()
	root := NewEnv(nil)
	reg := abortBegin(t, root)
	view := reg.Env()
	abortTry(t, "view.Set(x)", func() error { return view.Set("x", Int{V: 1}) })
	abortTry(t, "view.SetFunc(f)", func() error { return view.SetFunc("f", Int{V: 2}) })
	abortTry(t, "view.SetBoth(b)", func() error { return view.SetBoth("b", Int{V: 3}) })
	added := map[registrationKey]*Cell{
		{name: "x"}:           root.vars["x"],
		{name: "f", fn: true}: root.funcs["f"],
		{name: "b"}:           root.vars["b"],
		{name: "b", fn: true}: root.funcs["b"],
	}
	reg.Abort()

	for k, cell := range added {
		ns := abortNS(k.fn)
		if k.fn && root.HasLiveFunc(k.name) || !k.fn && root.HasLive(k.name) {
			t.Errorf("after abort the op-added %s binding %s is still live; want it absent", ns, k.name)
		}
		if cur := abortMapCell(root, k.name, k.fn); cur != nil {
			t.Errorf("after abort the root %s map holds %p for the op-added %s; want the op's cell %p deleted from the map so a later binding charges as fresh", ns, cur, k.name, cell)
		}
		if cell.v != nil || cell.canonical {
			t.Errorf("after abort the op's %s cell for %s holds %v (canonical %v); want it dropped", ns, k.name, cell.v, cell.canonical)
		}
	}
	abortWantAbsent(t, root, "x", "op-added x after abort")
	if got, ok := root.GetFunc("f"); ok {
		t.Errorf("op-added function f after abort: root f = %v; want it unbound", got)
	}
}

func TestRegistration_AbortRestoresCanonicalStatus(t *testing.T) {
	t.Parallel()
	root := NewEnv(nil)
	abortSeed(t, "seed canonical c", root.SetCanonical("c", Int{V: 1}))
	abortSeed(t, "seed plain p", root.Set("p", Int{V: 1}))
	abortSeed(t, "seed canonical function f", root.SetFuncCanonical("f", Int{V: 1}))

	reg := abortBegin(t, root)
	view := reg.Env()
	abortTry(t, "view.Set(c)", func() error { return view.Set("c", Int{V: 2}) })
	abortTry(t, "view.SetCanonical(p)", func() error { return view.SetCanonical("p", Int{V: 2}) })
	abortTry(t, "view.SetFunc(f)", func() error { return view.SetFunc("f", Int{V: 2}) })
	reg.Abort()

	if got, ok, canon := root.GetCanonical("c"); !ok || !canon || got != (Int{V: 1}) {
		t.Errorf("after abort root c = %v, bound %v, canonical %v; want the prior canonical 1", got, ok, canon)
	}
	if got, ok, canon := root.GetCanonical("p"); !ok || canon || got != (Int{V: 1}) {
		t.Errorf("after abort root p = %v, bound %v, canonical %v; want the prior non-canonical 1", got, ok, canon)
	}
	if got, ok, canon := root.GetFuncCanonical("f"); !ok || !canon || got != (Int{V: 1}) {
		t.Errorf("after abort root function f = %v, bound %v, canonical %v; want the prior canonical 1", got, ok, canon)
	}
}

func TestRegistration_AbortRestoresDeletedBinding(t *testing.T) {
	t.Parallel()
	root := NewEnv(nil)
	abortSeed(t, "seed canonical x", root.SetCanonical("x", Int{V: 1}))
	abortSeed(t, "seed function x", root.SetFunc("x", Int{V: 10}))
	varCell, funcCell := root.vars["x"], root.funcs["x"]

	reg := abortBegin(t, root)
	abortTry(t, "view.Delete(x)", func() error { reg.Env().Delete("x"); return nil })
	if root.HasLive("x") || root.HasLiveFunc("x") {
		t.Fatalf("view.Delete(x) left x live on the root; want both cells tombstoned before abort")
	}
	reg.Abort()

	if root.vars["x"] != varCell || root.funcs["x"] != funcCell {
		t.Errorf("after abort the root cells for x are %p/%p; want the original cells %p/%p", root.vars["x"], root.funcs["x"], varCell, funcCell)
	}
	if got, ok, canon := root.GetCanonical("x"); !ok || !canon || got != (Int{V: 1}) {
		t.Errorf("after abort root x = %v, bound %v, canonical %v; want the deleted canonical 1 restored", got, ok, canon)
	}
	if got, ok := root.GetFunc("x"); !ok || got != (Int{V: 10}) {
		t.Errorf("after abort root function x = %v, bound %v; want the deleted 10 restored", got, ok)
	}
}

func TestRegistration_AbortRestoresReplacedCell(t *testing.T) {
	t.Parallel()
	root := NewEnv(nil)
	abortSeed(t, "seed x", root.Set("x", Int{V: 1}))
	prior := root.vars["x"]

	reg := abortBegin(t, root)
	abortTry(t, "view.ReplaceCell(x)", func() error { return reg.Env().ReplaceCell("x", Int{V: 2}) })
	opCell := root.vars["x"]
	if opCell == prior {
		t.Fatalf("view.ReplaceCell(x) kept the prior cell in the root map; want a fresh cell before abort")
	}
	opVer := opCell.Version()
	reg.Abort()

	if cur := root.vars["x"]; cur != prior {
		t.Errorf("after abort the root cell for x is %p; want the prior cell %p reinstalled", cur, prior)
	}
	abortWantValue(t, root, "x", Int{V: 1}, "replaced x after abort")
	if v, live, _, ver := root.ReadCellSnapshot(opCell); live || ver <= opVer {
		t.Errorf("after abort the op's replacement cell for x holds %v (live %v, version %d); want a tombstone past version %d", v, live, ver, opVer)
	}
}

func TestRegistration_HostEqualValueRebindSurvivesAbort(t *testing.T) {
	t.Parallel()
	root := NewEnv(nil)
	abortSeed(t, "seed x", root.Set("x", Int{V: 1}))
	abortSeed(t, "seed control y", root.Set("y", Int{V: 1}))
	reg := abortBegin(t, root)
	view := reg.Env()
	op := Value(Int{V: 5})
	abortTry(t, "view.Set(x)", func() error { return view.Set("x", op) })
	abortTry(t, "view.Set(y)", func() error { return view.Set("y", op) })
	abortSeed(t, "raw root.Set(x) with the op's value", root.Set("x", op))
	hostVer := root.vars["x"].Version()
	reg.Abort()

	abortWantValue(t, root, "x", op, "host equal-value rebind of x after abort")
	if v := root.vars["x"].Version(); v != hostVer {
		t.Errorf("after abort the version of x is %d; want the host write's version %d untouched", v, hostVer)
	}
	abortWantValue(t, root, "y", Int{V: 1}, "op-only control y after abort")
}

func TestRegistration_HostRebindAfterOperationSurvivesAbort(t *testing.T) {
	t.Parallel()
	root := NewEnv(nil)
	abortSeed(t, "seed x", root.Set("x", Int{V: 1}))
	abortSeed(t, "seed function f", root.SetFunc("f", Int{V: 1}))
	abortSeed(t, "seed control y", root.Set("y", Int{V: 1}))
	reg := abortBegin(t, root)
	view := reg.Env()
	abortTry(t, "view.Set(x)", func() error { return view.Set("x", Int{V: 2}) })
	abortTry(t, "view.SetFunc(f)", func() error { return view.SetFunc("f", Int{V: 2}) })
	abortTry(t, "view.Set(n)", func() error { return view.Set("n", Int{V: 2}) })
	abortTry(t, "view.Set(y)", func() error { return view.Set("y", Int{V: 2}) })
	abortSeed(t, "raw root.SetCanonical(x)", root.SetCanonical("x", Int{V: 3}))
	abortSeed(t, "raw root.SetFunc(f)", root.SetFunc("f", Int{V: 3}))
	abortSeed(t, "raw root.Set(n)", root.Set("n", Int{V: 3}))
	reg.Abort()

	if got, ok, canon := root.GetCanonical("x"); !ok || !canon || got != (Int{V: 3}) {
		t.Errorf("after abort root x = %v, bound %v, canonical %v; want the host's canonical 3", got, ok, canon)
	}
	if got, ok := root.GetFunc("f"); !ok || got != (Int{V: 3}) {
		t.Errorf("after abort root function f = %v, bound %v; want the host's 3", got, ok)
	}
	abortWantValue(t, root, "n", Int{V: 3}, "host rebind of the op-added n after abort")
	abortWantValue(t, root, "y", Int{V: 1}, "op-only control y after abort")
}

func TestRegistration_HostAddSurvivesAbort(t *testing.T) {
	t.Parallel()
	root := NewEnv(nil)
	abortSeed(t, "seed x", root.Set("x", Int{V: 1}))
	reg := abortBegin(t, root)
	view := reg.Env()
	abortTry(t, "view.Set(x)", func() error { return view.Set("x", Int{V: 2}) })
	abortSeed(t, "raw root.Set(h)", root.Set("h", Int{V: 7}))
	abortSeed(t, "raw root.SetFunc(x)", root.SetFunc("x", Int{V: 8}))
	reg.Abort()

	abortWantValue(t, root, "h", Int{V: 7}, "host-added h after abort")
	if got, ok := root.GetFunc("x"); !ok || got != (Int{V: 8}) {
		t.Errorf("after abort root function x = %v, bound %v; want the host-added 8", got, ok)
	}
	abortWantValue(t, root, "x", Int{V: 1}, "op-only value x after abort")
}

func TestRegistration_HostDeleteAfterOperationSurvivesAbort(t *testing.T) {
	t.Parallel()
	root := NewEnv(nil)
	abortSeed(t, "seed x", root.Set("x", Int{V: 1}))
	abortSeed(t, "seed control y", root.Set("y", Int{V: 1}))
	reg := abortBegin(t, root)
	view := reg.Env()
	abortTry(t, "view.Set(x)", func() error { return view.Set("x", Int{V: 2}) })
	abortTry(t, "view.Set(n)", func() error { return view.Set("n", Int{V: 2}) })
	abortTry(t, "view.Set(y)", func() error { return view.Set("y", Int{V: 2}) })
	root.Delete("x")
	root.Delete("n")
	reg.Abort()

	abortWantAbsent(t, root, "x", "host-deleted x after abort")
	abortWantAbsent(t, root, "n", "host-deleted op-added n after abort")
	abortWantValue(t, root, "y", Int{V: 1}, "op-only control y after abort")
}

func TestRegistration_HostDeleteRecreateSurvivesAbort(t *testing.T) {
	t.Parallel()
	root := NewEnv(nil)
	abortSeed(t, "seed x", root.Set("x", Int{V: 1}))
	abortSeed(t, "seed control y", root.Set("y", Int{V: 1}))
	reg := abortBegin(t, root)
	view := reg.Env()
	abortTry(t, "view.Set(x)", func() error { return view.Set("x", Int{V: 2}) })
	abortTry(t, "view.Set(y)", func() error { return view.Set("y", Int{V: 2}) })
	root.Delete("x")
	abortSeed(t, "raw root.Set(x) after delete", root.Set("x", Int{V: 3}))
	hostVer := root.vars["x"].Version()
	reg.Abort()

	abortWantValue(t, root, "x", Int{V: 3}, "host-recreated x after abort")
	if v := root.vars["x"].Version(); v != hostVer {
		t.Errorf("after abort the version of x is %d; want the host recreate's version %d untouched", v, hostVer)
	}
	abortWantValue(t, root, "y", Int{V: 1}, "op-only control y after abort")
}

func TestRegistration_HostReplaceCellSurvivesAbort(t *testing.T) {
	t.Parallel()
	root := NewEnv(nil)
	abortSeed(t, "seed x", root.Set("x", Int{V: 1}))
	abortSeed(t, "seed control y", root.Set("y", Int{V: 1}))
	reg := abortBegin(t, root)
	view := reg.Env()
	abortTry(t, "view.Set(x)", func() error { return view.Set("x", Int{V: 2}) })
	abortTry(t, "view.Set(y)", func() error { return view.Set("y", Int{V: 2}) })
	abortSeed(t, "raw root.ReplaceCell(x)", root.ReplaceCell("x", Int{V: 3}))
	host := root.vars["x"]
	reg.Abort()

	if cur := root.vars["x"]; cur != host {
		t.Errorf("after abort the root cell for x is %p; want the host's replacement cell %p left in place", cur, host)
	}
	abortWantValue(t, root, "x", Int{V: 3}, "host-replaced x after abort")
	abortWantValue(t, root, "y", Int{V: 1}, "op-only control y after abort")
}

func TestRegistration_OperationHostOperationAbortKeepsHostState(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		host  func(root *Env) error
		check func(t *testing.T, root *Env)
	}{
		{
			name: "host rebind",
			host: func(r *Env) error { return r.Set("x", Int{V: 3}) },
			check: func(t *testing.T, r *Env) {
				if got, ok, canon := r.GetCanonical("x"); !ok || canon || got != (Int{V: 3}) {
					t.Errorf("after abort root x = %v, bound %v, canonical %v; want the host's non-canonical 3", got, ok, canon)
				}
			},
		},
		{
			name: "host delete",
			host: func(r *Env) error { r.Delete("x"); return nil },
			check: func(t *testing.T, r *Env) {
				abortWantAbsent(t, r, "x", "x deleted by the host between op writes, after abort")
			},
		},
		{
			name: "host replace cell",
			host: func(r *Env) error { return r.ReplaceCell("x", Int{V: 3}) },
			check: func(t *testing.T, r *Env) {
				abortWantValue(t, r, "x", Int{V: 3}, "x replaced by the host between op writes, after abort")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := NewEnv(nil)
			abortSeed(t, "seed canonical x", root.SetCanonical("x", Int{V: 1}))
			abortSeed(t, "seed control y", root.Set("y", Int{V: 1}))
			reg := abortBegin(t, root)
			view := reg.Env()
			abortTry(t, "first view.Set(x)", func() error { return view.Set("x", Int{V: 2}) })
			abortSeed(t, "raw root write of x", tt.host(root))
			hostCell := root.vars["x"]
			abortTry(t, "second view.Set(x)", func() error { return view.Set("x", Int{V: 4}) })
			abortTry(t, "view.Set(y)", func() error { return view.Set("y", Int{V: 2}) })
			reg.Abort()

			tt.check(t, root)
			if cur := root.vars["x"]; cur != hostCell {
				t.Errorf("after abort the root cell for x is %p; want the cell %p the host left before the second op write", cur, hostCell)
			}
			abortWantValue(t, root, "y", Int{V: 1}, "op-only control y after abort")
		})
	}
}

func TestRegistration_CompletedViewForwardsUnattributed(t *testing.T) {
	t.Parallel()
	root := NewEnv(nil)
	abortSeed(t, "seed x", root.Set("x", Int{V: 1}))
	abortSeed(t, "seed control y", root.Set("y", Int{V: 1}))
	reg1 := abortBegin(t, root)
	view1 := reg1.Env()
	abortTry(t, "view1.Set(x)", func() error { return view1.Set("x", Int{V: 2}) })
	reg1.Complete()
	abortTry(t, "completed view1.Set(idle)", func() error { return view1.Set("idle", Int{V: 5}) })

	reg2 := abortBegin(t, root)
	abortTry(t, "completed view1.Set(during)", func() error { return view1.Set("during", Int{V: 6}) })
	abortTry(t, "reg2 view.Set(y)", func() error { return reg2.Env().Set("y", Int{V: 2}) })
	reg2.Abort()

	abortWantValue(t, root, "x", Int{V: 2}, "x written by the completed reg1, after reg2 abort")
	abortWantValue(t, root, "idle", Int{V: 5}, "completed-view write made while idle, after reg2 abort")
	abortWantValue(t, root, "during", Int{V: 6}, "completed-view write made during reg2, after reg2 abort")
	abortWantValue(t, root, "y", Int{V: 1}, "reg2's op-only control y after reg2 abort")
}

func TestRegistration_AbortedViewForwardsUnattributed(t *testing.T) {
	t.Parallel()
	root := NewEnv(nil)
	abortSeed(t, "seed x", root.Set("x", Int{V: 1}))
	abortSeed(t, "seed control y", root.Set("y", Int{V: 1}))
	reg1 := abortBegin(t, root)
	view1 := reg1.Env()
	abortTry(t, "view1.Set(x)", func() error { return view1.Set("x", Int{V: 2}) })
	reg1.Abort()
	abortWantValue(t, root, "x", Int{V: 1}, "x after reg1 abort")

	abortTry(t, "aborted view1.Set(x)", func() error { return view1.Set("x", Int{V: 7}) })
	reg2 := abortBegin(t, root)
	abortTry(t, "aborted view1.Set(during)", func() error { return view1.Set("during", Int{V: 9}) })
	abortTry(t, "reg2 view.Set(y)", func() error { return reg2.Env().Set("y", Int{V: 2}) })
	reg2.Abort()

	abortWantValue(t, root, "x", Int{V: 7}, "aborted-view write made while idle, after reg2 abort")
	abortWantValue(t, root, "during", Int{V: 9}, "aborted-view write made during reg2, after reg2 abort")
	abortWantValue(t, root, "y", Int{V: 1}, "reg2's op-only control y after reg2 abort")
}

func TestRegistration_ConcurrentHostWritesRace(t *testing.T) {
	t.Parallel()
	const (
		shared     = 8
		opOnly     = 4
		iterations = 200
	)
	prior := Value(Int{V: -1})
	root := NewEnv(nil)
	for i := range shared {
		abortSeed(t, "seed shared key", root.Set(fmt.Sprintf("s%d", i), prior))
	}
	for i := range opOnly {
		abortSeed(t, "seed op-only key", root.Set(fmt.Sprintf("c%d", i), prior))
	}
	reg := abortBegin(t, root)
	view := reg.Env()

	errs := make(chan error, 4*iterations)
	var wg sync.WaitGroup
	for g := range 2 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for i := range iterations {
				if err := view.Set(fmt.Sprintf("s%d", (i+g)%shared), String{V: "op"}); err != nil {
					errs <- err
				}
				if err := view.Set(fmt.Sprintf("c%d", (i+g)%opOnly), String{V: "op"}); err != nil {
					errs <- err
				}
			}
		}()
		go func() {
			defer wg.Done()
			for i := range iterations {
				if err := root.Set(fmt.Sprintf("s%d", (i+g)%shared), Int{V: int64(g*iterations + i)}); err != nil {
					errs <- err
				}
				if i%50 == 0 {
					root.Rebuild()
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent write: %v", err)
	}
	reg.Abort()

	for i := range shared {
		name := fmt.Sprintf("s%d", i)
		got, ok := root.Get(name)
		if !ok {
			t.Errorf("after abort shared key %s is unbound; want its prior or a host-written value", name)
			continue
		}
		if _, isOp := got.(String); isOp {
			t.Errorf("after abort shared key %s holds the op-only value %v; want its prior or a host-written value", name, got)
		}
	}
	for i := range opOnly {
		abortWantValue(t, root, fmt.Sprintf("c%d", i), prior, "op-only key after abort")
	}
}
