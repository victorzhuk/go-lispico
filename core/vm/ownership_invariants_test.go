package vm

import (
	"context"
	"sync"
	"testing"

	"github.com/victorzhuk/go-lispico/core"
)

// Boxes escape the owning VM's single-threaded run loop through closures
// handed to host goroutines, so two goroutines may touch one box.
func TestCellBoxConcurrentLoadStore(t *testing.T) {
	box := &cellBox{v: core.Int{V: 0}}
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 1; i <= 500; i++ {
				box.store(core.Int{V: int64(i)})
			}
		}()
	}
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 500 {
				if _, ok := box.load().(core.Int); !ok {
					t.Error("load observed a non-Int value")
					return
				}
			}
		}()
	}
	wg.Wait()
}

// Consumer path: host goroutines apply two closures sharing one captured
// cell, writing and reading it through OpSetCap/OpGetCap on separate VMs.
func TestApplyConcurrentSharedCapturedCell(t *testing.T) {
	env := core.NewEnv(nil)
	setter := &Chunk{
		Name:       "<set>",
		Arity:      0,
		Constants:  []core.Value{core.Int{V: 1}},
		LocalNames: nil,
		Code: []Instruction{
			Encode(OpConst, 0),
			Encode(OpSetCap, 0),
			Encode(OpReturn, 0),
		},
	}
	getter := &Chunk{
		Name:       "<get>",
		Arity:      0,
		LocalNames: nil,
		Code: []Instruction{
			Encode(OpGetCap, 0),
			Encode(OpReturn, 0),
		},
	}
	box := &cellBox{v: core.Int{V: 0}}
	setFn := NewClosure(setter, []*cellBox{box}, env)
	getFn := NewClosure(getter, []*cellBox{box}, env)

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			v := New(env)
			for range 500 {
				if _, err := v.Apply(context.Background(), setFn, nil, env); err != nil {
					t.Errorf("apply setter: %v", err)
					return
				}
			}
		}()
		go func() {
			defer wg.Done()
			v := New(env)
			for range 500 {
				got, err := v.Apply(context.Background(), getFn, nil, env)
				if err != nil {
					t.Errorf("apply getter: %v", err)
					return
				}
				if _, ok := got.(core.Int); !ok {
					t.Errorf("getter returned %v, want Int", got)
					return
				}
			}
		}()
	}
	wg.Wait()
}

// The run/apply loop truncates the slices on a clean exit but leaves stale
// slots in the retained backing arrays, keeping the last call's operand and
// frame object graphs reachable.
func TestResetIncrementalClearsRetainedBacking(t *testing.T) {
	v := New(nil)
	for i := range 8 {
		v.push(core.Int{V: int64(i)})
	}
	v.frames = append(v.frames, Frame{chunk: &Chunk{Name: "t"}})
	v.pushFreeze(0, OpAdd, core.Int{V: 1})

	v.stack = v.stack[:0]
	v.frames = v.frames[:0]
	v.freezeStack = v.freezeStack[:0]
	v.handlers = v.handlers[:0]
	v.depth = 0

	if err := v.ResetIncremental(); err != nil {
		t.Fatal(err)
	}
	for i, s := range v.stack[:cap(v.stack)] {
		if s != nil {
			t.Fatalf("stack backing slot %d retains %v", i, s)
		}
	}
	for i, f := range v.frames[:cap(v.frames)] {
		if f.chunk != nil || f.env != nil || f.ip != 0 || f.base != 0 || f.isClosure || f.caps != nil {
			t.Fatalf("frames backing slot %d retains %+v", i, f)
		}
	}
	for i, r := range v.freezeStack[:cap(v.freezeStack)] {
		if r != (freezeRec{}) {
			t.Fatalf("freezeStack backing slot %d retains %+v", i, r)
		}
	}
}
