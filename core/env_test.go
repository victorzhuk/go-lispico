package core

import (
	"sync"
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
