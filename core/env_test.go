package core

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func requireRetainedLimitError(t *testing.T, err error) {
	t.Helper()
	var lerr *LispicoError
	if !errors.As(err, &lerr) || lerr.Code != CodeResourceLimit {
		t.Fatalf("error = %v, want %s", err, CodeResourceLimit)
	}
	if lerr.Message != "retained state capacity limit exceeded" {
		t.Fatalf("message = %q, want retained state capacity limit exceeded", lerr.Message)
	}
}

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

func TestEnv_RetainedSlotCeilingFailsClosed(t *testing.T) {
	t.Parallel()
	env := NewEnvWithRetainedLimits(nil, 0, 1)
	if err := env.Set("a", Int{V: 1}); err != nil {
		t.Fatalf("set a: %v", err)
	}
	wantBytes := retainedBindingBytes("a", Int{V: 1})
	gotBytes, gotSlots := env.RetainedUsage()
	if gotBytes != wantBytes || gotSlots != 1 {
		t.Fatalf("usage after a = (%d, %d), want (%d, 1)", gotBytes, gotSlots, wantBytes)
	}

	err := env.Set("b", Int{V: 2})
	requireRetainedLimitError(t, err)
	if _, ok := env.Get("b"); ok {
		t.Fatal("b should not be bound after failed retained slot charge")
	}
	if v, ok := env.Get("a"); !ok || !v.Equals(Int{V: 1}) {
		t.Fatalf("a = %v, %v; want 1, true", v, ok)
	}
	gotBytes, gotSlots = env.RetainedUsage()
	if gotBytes != wantBytes || gotSlots != 1 {
		t.Fatalf("usage after failed b = (%d, %d), want (%d, 1)", gotBytes, gotSlots, wantBytes)
	}
}

func TestEnv_SetBothCanonicalSlotCeilingFailsClosed(t *testing.T) {
	t.Parallel()
	env := NewEnvWithRetainedLimits(nil, 0, 1)
	err := env.SetBothCanonical("a", Int{V: 1})
	requireRetainedLimitError(t, err)
	if _, ok := env.Get("a"); ok {
		t.Fatal("a value should not be bound after failed dual-cell bind")
	}
	if _, ok := env.GetFunc("a"); ok {
		t.Fatal("a function should not be bound after failed dual-cell bind")
	}
	gotBytes, gotSlots := env.RetainedUsage()
	if gotBytes != 0 || gotSlots != 0 {
		t.Fatalf("usage after failed SetBothCanonical = (%d, %d), want (0, 0)", gotBytes, gotSlots)
	}
}

func TestEnv_RetainedByteCeilingFailsClosed(t *testing.T) {
	t.Parallel()
	first := String{V: "x"}
	limit := retainedBindingBytes("a", first)
	env := NewEnvWithRetainedLimits(nil, limit, 0)
	if err := env.Set("a", first); err != nil {
		t.Fatalf("set a: %v", err)
	}

	err := env.Set("b", String{V: "overflow"})
	requireRetainedLimitError(t, err)
	if _, ok := env.Get("b"); ok {
		t.Fatal("b should not be bound after failed retained byte charge")
	}
	gotBytes, gotSlots := env.RetainedUsage()
	if gotBytes != limit || gotSlots != 1 {
		t.Fatalf("usage after failed b = (%d, %d), want (%d, 1)", gotBytes, gotSlots, limit)
	}
}

func TestEnv_RetainedRebindDoesNotCharge(t *testing.T) {
	t.Parallel()
	first := Int{V: 1}
	env := NewEnvWithRetainedLimits(nil, retainedBindingBytes("x", first), 1)
	if err := env.Set("x", first); err != nil {
		t.Fatalf("set x: %v", err)
	}
	wantBytes, wantSlots := env.RetainedUsage()

	if err := env.Set("x", String{V: "much larger rebind"}); err != nil {
		t.Fatalf("rebind x: %v", err)
	}
	gotBytes, gotSlots := env.RetainedUsage()
	if gotBytes != wantBytes || gotSlots != wantSlots {
		t.Fatalf("usage after rebind = (%d, %d), want (%d, %d)", gotBytes, gotSlots, wantBytes, wantSlots)
	}
	if v, ok := env.Get("x"); !ok || !v.Equals(String{V: "much larger rebind"}) {
		t.Fatalf("x = %v, %v; want larger string, true", v, ok)
	}
}

func TestEnv_RetainedDeleteKeepsBacking(t *testing.T) {
	t.Parallel()
	env := NewEnvWithRetainedLimits(nil, 0, 1)
	if err := env.Set("x", Int{V: 1}); err != nil {
		t.Fatalf("set x: %v", err)
	}
	wantBytes, wantSlots := env.RetainedUsage()

	env.Delete("x")
	if _, ok := env.Get("x"); ok {
		t.Fatal("x should be deleted")
	}
	gotBytes, gotSlots := env.RetainedUsage()
	if gotBytes != wantBytes || gotSlots != wantSlots {
		t.Fatalf("usage after delete = (%d, %d), want (%d, %d)", gotBytes, gotSlots, wantBytes, wantSlots)
	}
	if err := env.Set("x", Int{V: 2}); err != nil {
		t.Fatalf("revive x: %v", err)
	}
	gotBytes, gotSlots = env.RetainedUsage()
	if gotBytes != wantBytes || gotSlots != wantSlots {
		t.Fatalf("usage after revive = (%d, %d), want (%d, %d)", gotBytes, gotSlots, wantBytes, wantSlots)
	}
}

func TestEnv_RebuildReleasesDeadCapacity(t *testing.T) {
	t.Parallel()
	env := NewEnvWithRetainedLimits(nil, 0, 4)
	live := String{V: "live"}
	liveFn := GoFunc{Name: "live-fn"}
	requireNoErr := func(label string, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
	}

	requireNoErr("set live", env.Set("live", live))
	requireNoErr("set live func", env.SetFunc("live-fn", liveFn))
	requireNoErr("set dead", env.Set("dead", String{V: "dead"}))
	requireNoErr("set dead func", env.SetFunc("dead-fn", GoFunc{Name: "dead-fn"}))
	env.Delete("dead")
	env.Delete("dead-fn")
	requireRetainedLimitError(t, env.Set("fresh", Int{V: 1}))

	env.Rebuild()

	wantBytes := retainedBindingBytes("live", live) + retainedBindingBytes("live-fn", liveFn)
	gotBytes, gotSlots := env.RetainedUsage()
	if gotBytes != wantBytes || gotSlots != 2 {
		t.Fatalf("usage after Rebuild = (%d, %d), want (%d, 2)", gotBytes, gotSlots, wantBytes)
	}
	if _, ok := env.Get("dead"); ok {
		t.Fatal("dead value binding survived Rebuild")
	}
	if _, ok := env.GetFunc("dead-fn"); ok {
		t.Fatal("dead function binding survived Rebuild")
	}
	requireNoErr("set fresh after Rebuild", env.Set("fresh", Int{V: 1}))
}

func TestEnv_RebuildPreservesLiveCellIdentity(t *testing.T) {
	t.Parallel()
	env := NewEnv(nil)
	requireNoErr := func(label string, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
	}
	requireNoErr("set x", env.Set("x", Int{V: 1}))
	requireNoErr("set dead", env.Set("dead", Int{V: 2}))
	cell, ok := env.Cell("x")
	if !ok {
		t.Fatal("expected cell for x")
	}
	env.Delete("dead")

	env.Rebuild()

	gotCell, ok := env.Cell("x")
	if !ok {
		t.Fatal("expected cell for x after Rebuild")
	}
	if gotCell != cell {
		t.Fatal("Rebuild changed live cell identity")
	}
	if v, live, _ := env.ReadCell(cell); !live || !v.Equals(Int{V: 1}) {
		t.Fatalf("held cell after Rebuild = %v, live=%v; want 1, true", v, live)
	}
}

func TestEnv_RebuildBumpsNameGenAndDropsDeadCells(t *testing.T) {
	t.Parallel()
	env := NewEnv(nil)
	requireNoErr := func(label string, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
	}
	requireNoErr("set live", env.Set("live", Int{V: 1}))
	requireNoErr("set dropped", env.Set("dropped", Int{V: 2}))
	liveCell, ok := env.CellLocal("live")
	if !ok {
		t.Fatal("expected live cell")
	}
	droppedCell, ok := env.CellLocal("dropped")
	if !ok {
		t.Fatal("expected dropped cell before Delete")
	}
	gen := env.NameGen()
	env.Delete("dropped")

	env.Rebuild()

	if env.NameGen() <= gen {
		t.Fatalf("NameGen after Rebuild = %d, want > %d", env.NameGen(), gen)
	}
	if _, ok := env.CellLocal("dropped"); ok {
		t.Fatal("dropped cell still resolves locally after Rebuild")
	}
	if _, live, _ := env.ReadCell(droppedCell); live {
		t.Fatal("held dropped cell became live after Rebuild")
	}
	gotLiveCell, ok := env.CellLocal("live")
	if !ok {
		t.Fatal("live cell missing after Rebuild")
	}
	if gotLiveCell != liveCell {
		t.Fatal("live cell identity changed after Rebuild")
	}
	if v, ok := env.Get("live"); !ok || !v.Equals(Int{V: 1}) {
		t.Fatalf("live = %v, %v; want 1, true", v, ok)
	}
}

func TestEnv_LambdaCaptureDoesNotDoubleCount(t *testing.T) {
	t.Parallel()
	env := NewEnv(nil)
	if err := env.Set("captured", String{V: "value"}); err != nil {
		t.Fatalf("set captured: %v", err)
	}
	wantBytes, wantSlots := env.RetainedUsage()

	val, err := NewEvaluator().Eval(t.Context(), List{Items: []Value{
		Symbol{V: "fn"},
		Vector{},
		Symbol{V: "captured"},
	}}, env)
	if err != nil {
		t.Fatalf("eval fn: %v", err)
	}
	lambda, ok := val.(Lambda)
	if !ok {
		t.Fatalf("Eval(fn) got %T, want Lambda", val)
	}
	if lambda.Env != env {
		t.Fatal("lambda did not capture env identity")
	}
	gotBytes, gotSlots := env.RetainedUsage()
	if gotBytes != wantBytes || gotSlots != wantSlots {
		t.Fatalf("usage after Lambda capture = (%d, %d), want (%d, %d)", gotBytes, gotSlots, wantBytes, wantSlots)
	}
}

func TestEnv_RebuildConcurrentReadersWriters(t *testing.T) {
	t.Parallel()
	child := NewEnv(nil).Child()
	if err := child.Set("seed", Int{V: 0}); err != nil {
		t.Fatalf("set seed: %v", err)
	}
	names := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	start := make(chan struct{})
	var wg sync.WaitGroup

	for id := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := range 1000 {
				name := names[(id+i)%len(names)]
				if err := child.Set(name, Int{V: int64(i)}); err != nil {
					t.Errorf("set %s: %v", name, err)
					return
				}
				if i%3 == 0 {
					child.Delete(names[(id+i+1)%len(names)])
				}
			}
		}()
	}
	for id := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := range 1000 {
				name := names[(id+i)%len(names)]
				child.Get(name)
				child.CellLocal(name)
				child.VarNames()
				child.RetainedUsage()
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for range 1000 {
			child.Rebuild()
		}
	}()

	close(start)
	wg.Wait()
}

func TestEnv_RetainedUsageExact(t *testing.T) {
	t.Parallel()
	env := NewEnvWithRetainedLimits(nil, 0, 0)
	val := Vector{Items: []Value{Int{V: 1}, String{V: "x"}}}
	fn := GoFunc{Name: "f"}
	if err := env.Set("v", val); err != nil {
		t.Fatalf("set v: %v", err)
	}
	if err := env.SetFunc("f", fn); err != nil {
		t.Fatalf("set func: %v", err)
	}

	wantBytes := retainedBindingBytes("v", val) + retainedBindingBytes("f", fn)
	gotBytes, gotSlots := env.RetainedUsage()
	if gotBytes != wantBytes || gotSlots != 2 {
		t.Fatalf("usage = (%d, %d), want (%d, 2)", gotBytes, gotSlots, wantBytes)
	}
}

func TestEnv_BareNewEnvRetainedUsageUncapped(t *testing.T) {
	t.Parallel()
	env := NewEnv(nil)
	wantBytes := int64(0)
	for _, name := range []string{"a", "b", "c"} {
		val := String{V: name}
		if err := env.Set(name, val); err != nil {
			t.Fatalf("set %s: %v", name, err)
		}
		wantBytes += retainedBindingBytes(name, val)
	}
	gotBytes, gotSlots := env.RetainedUsage()
	if gotBytes != wantBytes || gotSlots != 3 {
		t.Fatalf("usage = (%d, %d), want (%d, 3)", gotBytes, gotSlots, wantBytes)
	}
}

func TestEnv_ChildInheritsRetainedLimits(t *testing.T) {
	t.Parallel()
	parent := NewEnvWithRetainedLimits(nil, 0, 1)
	child := parent.Child()
	if err := child.Set("a", Int{V: 1}); err != nil {
		t.Fatalf("set a: %v", err)
	}
	requireRetainedLimitError(t, child.Set("b", Int{V: 2}))
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
