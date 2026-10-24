package core

import (
	"context"
	"sync"
	"testing"
)

type stubEvaluator struct{}

func (stubEvaluator) Eval(_ context.Context, _ Value, _ *Env) (Value, error) { return nil, nil }
func (stubEvaluator) Apply(_ context.Context, _ Value, _ []Value, _ *Env) (Value, error) {
	return nil, nil
}

// NewList keeps short slices flat without copying, so ChildVariadic must
// clone the rest slice before it escapes into the binding.
func TestChildVariadicRestSliceImmutableAfterEscape(t *testing.T) {
	parent := NewEnv(nil)
	args := []Value{Int{V: 1}, Int{V: 2}, Int{V: 3}}
	child, err := parent.ChildVariadic([]Symbol{{V: "a"}}, args, Symbol{V: "rest"})
	if err != nil {
		t.Fatal(err)
	}
	args[1] = Int{V: 99}
	got, ok := child.Get("rest")
	if !ok {
		t.Fatal("rest not bound")
	}
	want := NewList([]Value{Int{V: 2}, Int{V: 3}})
	if !got.Equals(want) {
		t.Fatalf("rest = %v, want %v", got, want)
	}
}

// A warm ReplaceCell installs a fresh cell identity whose version restarts
// at 1; without a NameGen bump a VM site cache keyed on the retired cell's
// unchanged version keeps serving the stale value.
func TestReplaceCellBumpsNameGenOnLiveAndCold(t *testing.T) {
	warm := NewEnv(nil)
	if err := warm.Set("x", Int{V: 1}); err != nil {
		t.Fatal(err)
	}
	old, ok := warm.Cell("x")
	if !ok {
		t.Fatal("x not bound")
	}
	before := warm.NameGen()
	if err := warm.ReplaceCell("x", Int{V: 2}); err != nil {
		t.Fatal(err)
	}
	if warm.NameGen() == before {
		t.Fatal("NameGen not bumped on live cell replacement")
	}
	if v, ok := warm.Get("x"); !ok || !v.Equals(Int{V: 2}) {
		t.Fatalf("Get(x) after ReplaceCell = %v, %v; want 2, true", v, ok)
	}
	if cur, ok := warm.Cell("x"); !ok || cur == old {
		t.Fatal("ReplaceCell must install a fresh cell identity")
	}

	cold := NewEnv(nil)
	coldBefore := cold.NameGen()
	if err := cold.ReplaceCell("y", Int{V: 7}); err != nil {
		t.Fatal(err)
	}
	if cold.NameGen() == coldBefore {
		t.Fatal("NameGen not bumped on cold ReplaceCell")
	}
	if v, ok := cold.Get("y"); !ok || !v.Equals(Int{V: 7}) {
		t.Fatalf("Get(y) after cold ReplaceCell = %v, %v", v, ok)
	}
}

// NewEnv reads the parent's evaluator while another goroutine republishes
// it; the race detector fails this when the read is unsynchronized.
func TestNewEnvConcurrentWithParentEvaluatorSet(t *testing.T) {
	parent := NewEnv(nil)
	parent.SetEvaluator(stubEvaluator{})
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			parent.SetEvaluator(stubEvaluator{})
		}()
		go func() {
			defer wg.Done()
			_ = NewEnv(parent).Evaluator()
		}()
	}
	wg.Wait()
}
