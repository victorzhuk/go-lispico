package core

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
)

func TestEnv_SetGet(t *testing.T) {
	t.Parallel()
	env := NewEnv(nil)
	env.Set("x", Int{V: 42})

	v, ok := env.Get("x")
	if !ok {
		t.Fatal("expected to find x")
	}
	if !v.Equals(Int{V: 42}) {
		t.Errorf("Get x = %v, want 42", v)
	}
}

func TestEnv_ParentLookup(t *testing.T) {
	t.Parallel()
	parent := NewEnv(nil)
	parent.Set("outer", String{V: "hello"})

	child := parent.Child()
	child.Set("inner", Int{V: 1})

	v, ok := child.Get("outer")
	if !ok {
		t.Fatal("child should find parent binding")
	}
	if !v.Equals(String{V: "hello"}) {
		t.Errorf("outer = %v, want \"hello\"", v)
	}

	_, ok = parent.Get("inner")
	if ok {
		t.Error("parent should not see child bindings")
	}
}

func TestEnv_Shadowing(t *testing.T) {
	t.Parallel()
	parent := NewEnv(nil)
	parent.Set("x", Int{V: 1})

	child := parent.Child()
	child.Set("x", Int{V: 99})

	v, _ := child.Get("x")
	if !v.Equals(Int{V: 99}) {
		t.Errorf("child x = %v, want 99 (shadow)", v)
	}

	v, _ = parent.Get("x")
	if !v.Equals(Int{V: 1}) {
		t.Errorf("parent x = %v, want 1 (unchanged)", v)
	}
}

func TestEnv_Find(t *testing.T) {
	t.Parallel()
	parent := NewEnv(nil)
	parent.Set("y", Bool{V: true})
	child := parent.Child()

	scope, ok := child.Find("y")
	if !ok {
		t.Fatal("Find should walk to parent")
	}
	if scope != parent {
		t.Error("Find should return the scope that owns the binding")
	}

	_, ok = child.Find("unknown")
	if ok {
		t.Error("Find should return false for unknown names")
	}
}

func TestEnv_MissingSym(t *testing.T) {
	t.Parallel()
	env := NewEnv(nil)
	_, ok := env.Get("missing")
	if ok {
		t.Error("Get of missing symbol should return false")
	}
}

func TestEnv_ChildVariadic_Fixed(t *testing.T) {
	t.Parallel()
	env := NewEnv(nil)
	params := []Symbol{{V: "a"}, {V: "b"}}
	args := []Value{Int{V: 1}, Int{V: 2}}

	child, err := env.ChildVariadic(params, args, Symbol{})
	if err != nil {
		t.Fatalf("ChildVariadic error: %v", err)
	}

	a, _ := child.Get("a")
	b, _ := child.Get("b")
	if !a.Equals(Int{V: 1}) || !b.Equals(Int{V: 2}) {
		t.Errorf("a=%v b=%v, want 1 2", a, b)
	}
}

func TestEnv_ChildVariadic_Variadic(t *testing.T) {
	t.Parallel()
	env := NewEnv(nil)
	params := []Symbol{{V: "a"}}
	args := []Value{Int{V: 1}, Int{V: 2}, Int{V: 3}}
	variadic := Symbol{V: "rest"}

	child, err := env.ChildVariadic(params, args, variadic)
	if err != nil {
		t.Fatalf("ChildVariadic error: %v", err)
	}

	a, _ := child.Get("a")
	rest, _ := child.Get("rest")
	if !a.Equals(Int{V: 1}) {
		t.Errorf("a = %v, want 1", a)
	}
	restList, ok := rest.(List)
	if !ok {
		t.Fatalf("rest should be List, got %T", rest)
	}
	if len(restList.Items) != 2 {
		t.Errorf("rest len = %d, want 2", len(restList.Items))
	}
}

func TestEnv_ChildVariadic_ArityError(t *testing.T) {
	t.Parallel()
	env := NewEnv(nil)
	params := []Symbol{{V: "a"}, {V: "b"}}
	args := []Value{Int{V: 1}}

	_, err := env.ChildVariadic(params, args, Symbol{})
	if err == nil {
		t.Error("expected arity error for wrong arg count")
	}
}

func TestEnv_ChildVariadic_VariadicEmpty(t *testing.T) {
	t.Parallel()
	env := NewEnv(nil)
	params := []Symbol{{V: "a"}}
	args := []Value{Int{V: 1}}
	variadic := Symbol{V: "rest"}

	child, err := env.ChildVariadic(params, args, variadic)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rest, _ := child.Get("rest")
	restList, ok := rest.(List)
	if !ok {
		t.Fatalf("rest should be List")
	}
	if len(restList.Items) != 0 {
		t.Errorf("empty variadic should bind empty list, got %d items", len(restList.Items))
	}
}

func TestEnv_ConcurrentReads(t *testing.T) {
	t.Parallel()
	env := NewEnv(nil)
	env.Set("shared", Int{V: 99})

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, ok := env.Get("shared")
			if !ok || !v.Equals(Int{V: 99}) {
				t.Errorf("concurrent read failed: %v %v", v, ok)
			}
		}()
	}
	wg.Wait()
}

func TestEnv_SetEvaluator(t *testing.T) {
	t.Parallel()
	env := NewEnv(nil)
	if env.Evaluator() != nil {
		t.Error("new env should have nil evaluator")
	}
	child := env.Child()
	if child.Evaluator() != nil {
		t.Error("child should inherit nil evaluator")
	}
}

func TestEnv_Cell_WriteThrough(t *testing.T) {
	t.Parallel()
	env := NewEnv(nil)
	env.Set("x", Int{V: 1})

	cell, ok := env.Cell("x")
	if !ok {
		t.Fatal("expected to resolve cell for x")
	}
	v, live, _ := env.ReadCell(cell)
	if !live || !v.Equals(Int{V: 1}) {
		t.Fatalf("ReadCell = %v, %v; want 1, true", v, live)
	}

	env.Set("x", Int{V: 2})
	v, live, _ = env.ReadCell(cell)
	if !live || !v.Equals(Int{V: 2}) {
		t.Fatalf("ReadCell after rebind = %v, %v; want 2, true", v, live)
	}
}

func TestEnv_Cell_TombstonedDelete(t *testing.T) {
	t.Parallel()
	env := NewEnv(nil)
	env.Set("x", Int{V: 1})
	cell, ok := env.Cell("x")
	if !ok {
		t.Fatal("expected to resolve cell for x")
	}

	env.Delete("x")

	if _, live, _ := env.ReadCell(cell); live {
		t.Error("held cell should report not-live after Delete")
	}
	if _, ok := env.Get("x"); ok {
		t.Error("Get should not find a deleted name")
	}
	if _, ok := env.Cell("x"); ok {
		t.Error("Cell should not resolve a deleted name")
	}

	// A different name bound to Nil{} is a legitimate live binding, distinct
	// from a tombstoned one.
	env.Set("y", Nil{})
	v, ok := env.Get("y")
	if !ok || !v.Equals(Nil{}) {
		t.Errorf("Get(y) = %v, %v; want Nil{}, true", v, ok)
	}
}

func TestEnv_CanonicalClearedOnRebind(t *testing.T) {
	t.Parallel()
	env := NewEnv(nil)
	env.SetCanonical("+", Int{V: 1})

	_, _, isCanon := env.GetCanonical("+")
	if !isCanon {
		t.Fatal("expected + to be canonical after SetCanonical")
	}

	env.Set("+", Int{V: 1})
	_, _, isCanon = env.GetCanonical("+")
	if isCanon {
		t.Error("Set should clear the canonical marker even with an equal value")
	}
}

func TestEnv_ConcurrentSetGet(t *testing.T) {
	t.Parallel()
	env := NewEnv(nil)
	env.Set("shared", Int{V: 0})

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(2)
		go func(n int) {
			defer wg.Done()
			env.Set("shared", Int{V: int64(n)})
		}(i)
		go func() {
			defer wg.Done()
			if v, ok := env.Get("shared"); ok {
				if _, isInt := v.(Int); !isInt {
					t.Errorf("concurrent Get returned non-Int %T", v)
				}
			}
		}()
	}
	wg.Wait()
}

func TestEnv_Get_ZeroAllocs(t *testing.T) {
	env := NewEnv(nil)
	env.Set("x", Int{V: 42})

	allocs := testing.AllocsPerRun(100, func() {
		if _, ok := env.Get("x"); !ok {
			t.Fatal("expected to find x")
		}
	})
	if allocs != 0 {
		t.Errorf("Env.Get allocated %.1f times per run, want 0", allocs)
	}
}

func TestEnv_RebindAcrossConcreteTypes(t *testing.T) {
	t.Parallel()
	env := NewEnv(nil)
	env.Set("x", Int{V: 1})
	env.Set("x", String{V: "hi"})
	env.Set("x", GoFunc{Name: "x", Fn: func(context.Context, Evaluator, []Value, *Env) (Value, error) {
		return Nil{}, nil
	}})

	v, ok := env.Get("x")
	if !ok {
		t.Fatal("expected to find x")
	}
	if _, isFn := v.(GoFunc); !isFn {
		t.Errorf("expected final binding to be GoFunc, got %T", v)
	}
}

func TestEnv_NameGen(t *testing.T) {
	t.Parallel()
	env := NewEnv(nil)
	env.Set("x", Int{V: 1})
	g0 := env.NameGen()

	env.Set("x", Int{V: 2})
	if env.NameGen() != g0 {
		t.Error("rebinding a live name must not bump NameGen")
	}

	env.Set("y", Int{V: 1})
	if env.NameGen() <= g0 {
		t.Error("binding a new name must bump NameGen")
	}
	g1 := env.NameGen()

	env.Delete("y")
	env.Set("y", Int{V: 2})
	if env.NameGen() <= g1 {
		t.Error("reviving a tombstoned name must bump NameGen")
	}
}

func TestEnv_CellVersion(t *testing.T) {
	t.Parallel()
	env := NewEnv(nil)
	env.Set("x", Int{V: 1})
	cell, ok := env.Cell("x")
	if !ok {
		t.Fatal("expected cell for x")
	}
	assertVersion := func(want uint64) {
		t.Helper()
		if got := cell.Version(); got != want {
			t.Fatalf("cell version = %d, want %d", got, want)
		}
	}

	assertVersion(1)
	env.Get("x")
	env.GetCanonical("x")
	env.ReadCell(cell)
	_, _, _, snapVer := env.ReadCellSnapshot(cell)
	if snapVer != 1 {
		t.Fatalf("snapshot version = %d, want 1", snapVer)
	}
	env.Cell("x")
	env.CellLocal("x")
	env.Find("x")
	env.VarNames()
	assertVersion(1)

	env.Set("x", Int{V: 2})
	assertVersion(2)
	env.SetCanonical("x", Int{V: 2})
	assertVersion(3)
	env.Set("x", Int{V: 2})
	assertVersion(4)
	env.Delete("x")
	assertVersion(5)
	env.Delete("x")
	assertVersion(5)
	env.Set("x", Int{V: 3})
	assertVersion(6)
}

func TestEnv_FuncCellVersion(t *testing.T) {
	t.Parallel()
	parent := NewEnv(nil)
	parentFunc := GoFunc{Name: "parent"}
	parent.SetFunc("f", parentFunc)

	env := parent.Child()
	localFunc := GoFunc{Name: "local"}
	env.SetFunc("f", localFunc)

	env.mu.RLock()
	cell := env.funcs["f"]
	env.mu.RUnlock()
	if cell == nil {
		t.Fatal("expected function cell for f")
	}
	assertVersion := func(want uint64) {
		t.Helper()
		if got := cell.Version(); got != want {
			t.Fatalf("function cell version = %d, want %d", got, want)
		}
	}
	assertFuncName := func(got Value, want string) {
		t.Helper()
		fn, ok := got.(GoFunc)
		if !ok {
			t.Fatalf("function binding got %T, want GoFunc", got)
		}
		if fn.Name != want {
			t.Fatalf("function binding name = %q, want %q", fn.Name, want)
		}
	}

	assertVersion(1)
	if v, ok := env.GetFunc("f"); !ok {
		t.Fatal("GetFunc should find f")
	} else {
		assertFuncName(v, localFunc.Name)
	}
	if v, ok, canon := env.GetFuncCanonical("f"); !ok || canon {
		t.Fatalf("GetFuncCanonical found=%v canonical=%v, want true false", ok, canon)
	} else {
		assertFuncName(v, localFunc.Name)
	}
	if _, _, _, snapVer := env.ReadCellSnapshot(cell); snapVer != 1 {
		t.Fatalf("function snapshot version = %d, want 1", snapVer)
	}
	if names := env.FuncNames(); len(names) != 1 || names[0] != "f" {
		t.Fatalf("FuncNames = %v, want [f]", names)
	}
	assertVersion(1)

	canonFunc := GoFunc{Name: "canon"}
	env.SetFuncCanonical("f", canonFunc)
	assertVersion(2)
	if v, ok, canon := env.GetFuncCanonical("f"); !ok || !canon {
		t.Fatalf("canonical function found=%v canonical=%v, want true true", ok, canon)
	} else {
		assertFuncName(v, canonFunc.Name)
	}

	env.SetFunc("f", localFunc)
	assertVersion(3)
	if _, ok, canon := env.GetFuncCanonical("f"); !ok || canon {
		t.Fatalf("rebound function found=%v canonical=%v, want true false", ok, canon)
	}

	env.Delete("f")
	assertVersion(4)
	if _, live, canon := env.ReadCell(cell); live || canon {
		t.Fatalf("deleted function cell live=%v canonical=%v, want false false", live, canon)
	}
	if names := env.FuncNames(); len(names) != 0 {
		t.Fatalf("FuncNames after Delete = %v, want []", names)
	}
	if v, ok := env.GetFunc("f"); !ok {
		t.Fatal("GetFunc should find parent f after local Delete")
	} else {
		assertFuncName(v, parentFunc.Name)
	}

	env.Delete("f")
	assertVersion(4)
	env.SetFunc("f", localFunc)
	assertVersion(5)
}

func TestEnv_FuncCell_ValueCanonicalCoherent(t *testing.T) {
	t.Parallel()
	env := NewEnv(nil)
	nonCanon := GoFunc{Name: "custom"}
	canon := GoFunc{Name: "canon"}
	env.SetFunc("+", nonCanon)

	env.mu.RLock()
	cell := env.funcs["+"]
	env.mu.RUnlock()
	if cell == nil {
		t.Fatal("expected function cell for +")
	}

	var stop atomic.Bool
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; !stop.Load(); i++ {
			if i%2 == 0 {
				env.SetFunc("+", nonCanon)
			} else {
				env.SetFuncCanonical("+", canon)
			}
		}
	}()

	for range 500000 {
		v, live, isCanon := env.ReadCell(cell)
		if !live {
			continue
		}
		if (v.(GoFunc).Name == nonCanon.Name) == isCanon {
			stop.Store(true)
			wg.Wait()
			t.Fatalf("torn function read: value=%s canonical=%v", v.(GoFunc).Name, isCanon)
		}
	}
	stop.Store(true)
	wg.Wait()
}

// TestEnv_Cell_ValueCanonicalCoherent proves value and canonical flag are
// published as one unit: a concurrent reader never observes a rebind's new
// value paired with the previous binding's canonical flag. Run under -race.
func TestEnv_Cell_ValueCanonicalCoherent(t *testing.T) {
	t.Parallel()
	env := NewEnv(nil)
	nonCanon := GoFunc{Name: "custom"}
	canon := GoFunc{Name: "canon"}
	env.Set("+", nonCanon)

	cell, ok := env.Cell("+")
	if !ok {
		t.Fatal("expected cell for +")
	}

	var stop atomic.Bool
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; !stop.Load(); i++ {
			if i%2 == 0 {
				env.Set("+", nonCanon)
			} else {
				env.SetCanonical("+", canon)
			}
		}
	}()

	for range 500000 {
		v, live, isCanon := env.ReadCell(cell)
		if !live {
			continue
		}
		// custom must never be canonical; canon must always be canonical.
		if (v.(GoFunc).Name == "custom") == isCanon {
			stop.Store(true)
			wg.Wait()
			t.Fatalf("torn read: value=%s canonical=%v", v.(GoFunc).Name, isCanon)
		}
	}
	stop.Store(true)
	wg.Wait()
}
