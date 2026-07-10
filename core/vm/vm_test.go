package vm

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

func TestVM_New(t *testing.T) {
	env := core.NewEnv(nil)
	vm := New(env)

	if vm.stackSize() != 0 {
		t.Errorf("expected empty stack, got %d", vm.stackSize())
	}
	if vm.frameCount() != 0 {
		t.Errorf("expected no frames, got %d", vm.frameCount())
	}
}

func TestVM_StackOperations(t *testing.T) {
	vm := New(core.NewEnv(nil))

	vm.push(core.Int{V: 1})
	vm.push(core.Int{V: 2})
	vm.push(core.Int{V: 3})

	if vm.stackSize() != 3 {
		t.Errorf("expected stack size 3, got %d", vm.stackSize())
	}

	peeked, err := vm.peek()
	if err != nil || !peeked.Equals(core.Int{V: 3}) {
		t.Errorf("expected peek to return 3, got %v (err %v)", peeked, err)
	}

	v, err := vm.pop()
	if err != nil || !v.Equals(core.Int{V: 3}) {
		t.Errorf("expected pop to return 3, got %v (err %v)", v, err)
	}
	if vm.stackSize() != 2 {
		t.Errorf("expected stack size 2 after pop, got %d", vm.stackSize())
	}
}

func TestVM_PopUnderflow(t *testing.T) {
	vm := New(core.NewEnv(nil))

	_, err := vm.pop()
	if err == nil {
		t.Fatal("expected error popping empty stack")
	}
	if _, ok := err.(*core.LispicoError); !ok {
		t.Errorf("expected *core.LispicoError, got %T", err)
	}
}

func TestVM_OpNil(t *testing.T) {
	vm := New(core.NewEnv(nil))
	chunk := &Chunk{
		Name: "test",
		Code: []Instruction{
			Encode(OpNil, 0),
			Encode(OpReturn, 0),
		},
	}

	result, err := vm.Run(context.Background(), chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := result.(core.Nil); !ok {
		t.Errorf("expected Nil, got %T", result)
	}
}

func TestVM_OpTrue(t *testing.T) {
	vm := New(core.NewEnv(nil))
	chunk := &Chunk{
		Name: "test",
		Code: []Instruction{
			Encode(OpTrue, 0),
			Encode(OpReturn, 0),
		},
	}

	result, err := vm.Run(context.Background(), chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	b, ok := result.(core.Bool)
	if !ok || !b.V {
		t.Errorf("expected true, got %v", result)
	}
}

func TestVM_OpFalse(t *testing.T) {
	vm := New(core.NewEnv(nil))
	chunk := &Chunk{
		Name: "test",
		Code: []Instruction{
			Encode(OpFalse, 0),
			Encode(OpReturn, 0),
		},
	}

	result, err := vm.Run(context.Background(), chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	b, ok := result.(core.Bool)
	if !ok || b.V {
		t.Errorf("expected false, got %v", result)
	}
}

func TestVM_OpConst(t *testing.T) {
	vm := New(core.NewEnv(nil))
	chunk := &Chunk{
		Name: "test",
		Constants: []core.Value{
			core.Int{V: 42},
		},
		Code: []Instruction{
			Encode(OpConst, 0),
			Encode(OpReturn, 0),
		},
	}

	result, err := vm.Run(context.Background(), chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Equals(core.Int{V: 42}) {
		t.Errorf("expected 42, got %v", result)
	}
}

func TestVM_OpGetGlobal(t *testing.T) {
	env := core.NewEnv(nil)
	env.Set("x", core.Int{V: 100})

	vm := New(env)
	chunk := &Chunk{
		Name: "test",
		Constants: []core.Value{
			core.Symbol{V: "x"},
		},
		Code: []Instruction{
			Encode(OpGetGlobal, 0),
			Encode(OpReturn, 0),
		},
	}

	result, err := vm.Run(context.Background(), chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Equals(core.Int{V: 100}) {
		t.Errorf("expected 100, got %v", result)
	}
}

func TestVM_OpGetGlobal_Undefined(t *testing.T) {
	vm := New(core.NewEnv(nil))
	chunk := &Chunk{
		Name: "test",
		Constants: []core.Value{
			core.Symbol{V: "undefined"},
		},
		Code: []Instruction{
			Encode(OpGetGlobal, 0),
			Encode(OpReturn, 0),
		},
	}

	_, err := vm.Run(context.Background(), chunk)
	if err == nil {
		t.Fatal("expected error for undefined symbol")
	}
}

func TestVM_OpSetGlobal(t *testing.T) {
	env := core.NewEnv(nil)
	vm := New(env)
	chunk := &Chunk{
		Name: "test",
		Constants: []core.Value{
			core.Symbol{V: "x"},
			core.Int{V: 42},
		},
		Code: []Instruction{
			Encode(OpConst, 1),
			Encode(OpSetGlobal, 0),
			Encode(OpReturn, 0),
		},
	}

	_, err := vm.Run(context.Background(), chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	val, ok := env.Get("x")
	if !ok {
		t.Fatal("expected x to be defined")
	}
	if !val.Equals(core.Int{V: 42}) {
		t.Errorf("expected x=42, got %v", val)
	}
}

func TestVM_OpGetLocal(t *testing.T) {
	vm := New(core.NewEnv(nil))
	chunk := &Chunk{
		Name:   "test",
		Locals: 2,
		Code: []Instruction{
			Encode(OpNil, 0),
			Encode(OpTrue, 0),
			Encode(OpGetLocal, 1),
			Encode(OpReturn, 0),
		},
	}

	result, err := vm.Run(context.Background(), chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Equals(core.Bool{V: true}) {
		t.Errorf("expected true, got %v", result)
	}
}

func TestVM_OpSetLocal(t *testing.T) {
	vm := New(core.NewEnv(nil))
	chunk := &Chunk{
		Name:   "test",
		Locals: 1,
		Code: []Instruction{
			Encode(OpConst, 0),
			Encode(OpSetLocal, 0),
			Encode(OpGetLocal, 0),
			Encode(OpReturn, 0),
		},
		Constants: []core.Value{
			core.Int{V: 42},
		},
	}

	result, err := vm.Run(context.Background(), chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Equals(core.Int{V: 42}) {
		t.Errorf("expected 42, got %v", result)
	}
}

func TestVM_OpPop(t *testing.T) {
	vm := New(core.NewEnv(nil))
	chunk := &Chunk{
		Name: "test",
		Code: []Instruction{
			Encode(OpTrue, 0),
			Encode(OpPop, 0),
			Encode(OpNil, 0),
			Encode(OpReturn, 0),
		},
	}

	result, err := vm.Run(context.Background(), chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := result.(core.Nil); !ok {
		t.Errorf("expected Nil, got %v", result)
	}
}

func TestVM_OpJump(t *testing.T) {
	vm := New(core.NewEnv(nil))
	chunk := &Chunk{
		Name: "test",
		Code: []Instruction{
			Encode(OpTrue, 0),
			Encode(OpJump, 1),
			Encode(OpFalse, 0),
			Encode(OpReturn, 0),
		},
	}

	result, err := vm.Run(context.Background(), chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Equals(core.Bool{V: true}) {
		t.Errorf("expected true (skipped false), got %v", result)
	}
}

func TestVM_OpJumpIfFalse(t *testing.T) {
	tests := []struct {
		name     string
		cond     core.Value
		expected core.Value
	}{
		{"jumps on false", core.Bool{V: false}, core.Int{V: 1}},
		{"no jump on true", core.Bool{V: true}, core.Int{V: 2}},
		{"jumps on nil", core.Nil{}, core.Int{V: 1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm := New(core.NewEnv(nil))
			chunk := &Chunk{
				Name: "test",
				Constants: []core.Value{
					tt.cond,
					core.Int{V: 1},
					core.Int{V: 2},
				},
				Code: []Instruction{
					Encode(OpConst, 0),
					Encode(OpJumpIfFalse, 2),
					Encode(OpConst, 2),
					Encode(OpJump, 1),
					Encode(OpConst, 1),
					Encode(OpReturn, 0),
				},
			}

			result, err := vm.Run(context.Background(), chunk)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !result.Equals(tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestVM_OpMakeList(t *testing.T) {
	vm := New(core.NewEnv(nil))
	chunk := &Chunk{
		Name: "test",
		Constants: []core.Value{
			core.Int{V: 1},
			core.Int{V: 2},
			core.Int{V: 3},
		},
		Code: []Instruction{
			Encode(OpConst, 0),
			Encode(OpConst, 1),
			Encode(OpConst, 2),
			Encode(OpMakeList, 3),
			Encode(OpReturn, 0),
		},
	}

	result, err := vm.Run(context.Background(), chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	list, ok := result.(core.List)
	if !ok {
		t.Fatalf("expected List, got %T", result)
	}
	if len(list.Items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(list.Items))
	}
	if !list.Items[0].Equals(core.Int{V: 1}) || !list.Items[1].Equals(core.Int{V: 2}) || !list.Items[2].Equals(core.Int{V: 3}) {
		t.Errorf("unexpected list items: %v", list)
	}
}

func TestVM_OpMakeVector(t *testing.T) {
	vm := New(core.NewEnv(nil))
	chunk := &Chunk{
		Name: "test",
		Constants: []core.Value{
			core.Int{V: 1},
			core.Int{V: 2},
		},
		Code: []Instruction{
			Encode(OpConst, 0),
			Encode(OpConst, 1),
			Encode(OpMakeVector, 2),
			Encode(OpReturn, 0),
		},
	}

	result, err := vm.Run(context.Background(), chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	vec, ok := result.(core.Vector)
	if !ok {
		t.Fatalf("expected Vector, got %T", result)
	}
	if len(vec.Items) != 2 {
		t.Errorf("expected 2 items, got %d", len(vec.Items))
	}
}

func TestVM_OpMakeMap(t *testing.T) {
	vm := New(core.NewEnv(nil))
	chunk := &Chunk{
		Name: "test",
		Constants: []core.Value{
			core.Keyword{V: "a"},
			core.Int{V: 1},
			core.Keyword{V: "b"},
			core.Int{V: 2},
		},
		Code: []Instruction{
			Encode(OpConst, 0),
			Encode(OpConst, 1),
			Encode(OpConst, 2),
			Encode(OpConst, 3),
			Encode(OpMakeMap, 2),
			Encode(OpReturn, 0),
		},
	}

	result, err := vm.Run(context.Background(), chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	hm, ok := result.(*core.HashMap)
	if !ok {
		t.Fatalf("expected HashMap, got %T", result)
	}

	val, ok := hm.Get(core.Keyword{V: "a"})
	if !ok || !val.Equals(core.Int{V: 1}) {
		t.Errorf("expected :a -> 1, got %v", val)
	}

	val, ok = hm.Get(core.Keyword{V: "b"})
	if !ok || !val.Equals(core.Int{V: 2}) {
		t.Errorf("expected :b -> 2, got %v", val)
	}
}

func TestVM_OpClosure(t *testing.T) {
	vm := New(core.NewEnv(nil))
	subChunk := &Chunk{
		Name:  "<fn>",
		Arity: 1,
		Code: []Instruction{
			Encode(OpGetLocal, 0),
			Encode(OpReturn, 0),
		},
	}
	chunk := &Chunk{
		Name:      "test",
		SubChunks: []*Chunk{subChunk},
		Code: []Instruction{
			Encode(OpClosure, 0),
			Encode(OpReturn, 0),
		},
	}

	result, err := vm.Run(context.Background(), chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	closure, ok := result.(*Closure)
	if !ok {
		t.Fatalf("expected Closure, got %T", result)
	}
	if closure.Chunk != subChunk {
		t.Error("closure chunk mismatch")
	}
}

func TestVM_CallGoFunc(t *testing.T) {
	env := core.NewEnv(nil)
	env.Set("add", core.GoFunc{
		Name: "add",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			a := args[0].(core.Int).V
			b := args[1].(core.Int).V
			return core.Int{V: a + b}, nil
		},
	})

	vm := New(env)
	chunk := &Chunk{
		Name: "test",
		Constants: []core.Value{
			core.Symbol{V: "add"},
			core.Int{V: 2},
			core.Int{V: 3},
		},
		Code: []Instruction{
			Encode(OpGetGlobal, 0),
			Encode(OpConst, 1),
			Encode(OpConst, 2),
			Encode(OpCall, 2),
			Encode(OpReturn, 0),
		},
	}

	result, err := vm.Run(context.Background(), chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Equals(core.Int{V: 5}) {
		t.Errorf("expected 5, got %v", result)
	}
}

func TestVM_CallClosure(t *testing.T) {
	vm := New(core.NewEnv(nil))
	fnChunk := &Chunk{
		Name:  "double",
		Arity: 1,
		Code: []Instruction{
			Encode(OpGetLocal, 0),
			Encode(OpReturn, 0),
		},
	}
	chunk := &Chunk{
		Name:      "test",
		SubChunks: []*Chunk{fnChunk},
		Constants: []core.Value{
			core.Int{V: 42},
		},
		Code: []Instruction{
			Encode(OpClosure, 0),
			Encode(OpConst, 0),
			Encode(OpCall, 1),
			Encode(OpReturn, 0),
		},
	}

	result, err := vm.Run(context.Background(), chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Equals(core.Int{V: 42}) {
		t.Errorf("expected 42, got %v", result)
	}
}

func TestVM_TailCall(t *testing.T) {
	vm := New(core.NewEnv(nil))
	loopChunk := &Chunk{
		Name:   "loop",
		Arity:  1,
		Locals: 1,
		Code: []Instruction{
			Encode(OpGetLocal, 0),
			Encode(OpJumpIfFalse, 2),
			Encode(OpConst, 0),
			Encode(OpReturn, 0),
			Encode(OpConst, 1),
			Encode(OpReturn, 0),
		},
		Constants: []core.Value{
			core.Int{V: 0},
			core.Int{V: 999},
		},
	}
	chunk := &Chunk{
		Name:      "test",
		SubChunks: []*Chunk{loopChunk},
		Constants: []core.Value{
			core.Bool{V: false},
		},
		Code: []Instruction{
			Encode(OpClosure, 0),
			Encode(OpConst, 0),
			Encode(OpTailCall, 1),
		},
	}

	result, err := vm.Run(context.Background(), chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Equals(core.Int{V: 999}) {
		t.Errorf("expected 999 (from tail call), got %v", result)
	}
}

func TestVM_CallNonCallable(t *testing.T) {
	vm := New(core.NewEnv(nil))
	chunk := &Chunk{
		Name: "test",
		Constants: []core.Value{
			core.Int{V: 42},
		},
		Code: []Instruction{
			Encode(OpConst, 0),
			Encode(OpCall, 0),
			Encode(OpReturn, 0),
		},
	}

	_, err := vm.Run(context.Background(), chunk)
	if err == nil {
		t.Fatal("expected error for calling non-callable")
	}
}

func TestVM_ContextCancellation(t *testing.T) {
	vm := New(core.NewEnv(nil))
	chunk := &Chunk{
		Name: "test",
		Code: []Instruction{
			Encode(OpNil, 0),
			Encode(OpReturn, 0),
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := vm.Run(ctx, chunk)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestVM_Reset(t *testing.T) {
	vm := New(core.NewEnv(nil))
	chunk := &Chunk{
		Name: "test",
		Code: []Instruction{
			Encode(OpTrue, 0),
			Encode(OpReturn, 0),
		},
	}

	_, _ = vm.Run(context.Background(), chunk)

	if vm.stackSize() != 0 {
		t.Errorf("expected empty stack after run, got %d", vm.stackSize())
	}

	vm.push(core.Int{V: 1})
	vm.push(core.Int{V: 2})
	vm.reset()

	if vm.stackSize() != 0 {
		t.Errorf("expected empty stack after reset, got %d", vm.stackSize())
	}
	if vm.frameCount() != 0 {
		t.Errorf("expected no frames after reset, got %d", vm.frameCount())
	}
}

func TestClosure_Type(t *testing.T) {
	c := NewClosure(&Chunk{Name: "test"}, nil)

	if c.Type().V != "closure" {
		t.Errorf("expected type 'closure', got %s", c.Type().V)
	}
}

func TestClosure_String(t *testing.T) {
	c := NewClosure(&Chunk{Name: "my-fn"}, nil)

	expected := "#<closure my-fn>"
	if c.String() != expected {
		t.Errorf("expected %q, got %q", expected, c.String())
	}
}

func TestClosure_Equals(t *testing.T) {
	chunk := &Chunk{Name: "test"}
	env := core.NewEnv(nil)

	c1 := NewClosure(chunk, env)
	c2 := NewClosure(chunk, env)
	c3 := NewClosure(&Chunk{Name: "other"}, env)

	if !c1.Equals(c1) {
		t.Error("closure should equal itself")
	}
	if c1.Equals(c2) {
		t.Error("different closures should not be equal")
	}
	if c1.Equals(c3) {
		t.Error("closures with different chunks should not be equal")
	}
	if c1.Equals(core.Int{V: 42}) {
		t.Error("closure should not equal non-closure")
	}
}

func TestVM_MultipleReturns(t *testing.T) {
	vm := New(core.NewEnv(nil))

	innerChunk := &Chunk{
		Name:  "inner",
		Arity: 0,
		Code: []Instruction{
			Encode(OpConst, 0),
			Encode(OpReturn, 0),
		},
		Constants: []core.Value{core.Int{V: 100}},
	}

	outerChunk := &Chunk{
		Name:      "outer",
		SubChunks: []*Chunk{innerChunk},
		Code: []Instruction{
			Encode(OpClosure, 0),
			Encode(OpCall, 0),
			Encode(OpReturn, 0),
		},
	}

	result, err := vm.Run(context.Background(), outerChunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Equals(core.Int{V: 100}) {
		t.Errorf("expected 100, got %v", result)
	}
}

func TestVM_EmptyList(t *testing.T) {
	vm := New(core.NewEnv(nil))
	chunk := &Chunk{
		Name: "test",
		Code: []Instruction{
			Encode(OpMakeList, 0),
			Encode(OpReturn, 0),
		},
	}

	result, err := vm.Run(context.Background(), chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	list, ok := result.(core.List)
	if !ok {
		t.Fatalf("expected List, got %T", result)
	}
	if len(list.Items) != 0 {
		t.Errorf("expected empty list, got %d items", len(list.Items))
	}
}

func TestVM_EmptyVector(t *testing.T) {
	vm := New(core.NewEnv(nil))
	chunk := &Chunk{
		Name: "test",
		Code: []Instruction{
			Encode(OpMakeVector, 0),
			Encode(OpReturn, 0),
		},
	}

	result, err := vm.Run(context.Background(), chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	vec, ok := result.(core.Vector)
	if !ok {
		t.Fatalf("expected Vector, got %T", result)
	}
	if len(vec.Items) != 0 {
		t.Errorf("expected empty vector, got %d items", len(vec.Items))
	}
}

func TestVM_EmptyMap(t *testing.T) {
	vm := New(core.NewEnv(nil))
	chunk := &Chunk{
		Name: "test",
		Code: []Instruction{
			Encode(OpMakeMap, 0),
			Encode(OpReturn, 0),
		},
	}

	result, err := vm.Run(context.Background(), chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	hm, ok := result.(*core.HashMap)
	if !ok {
		t.Fatalf("expected HashMap, got %T", result)
	}
	if hm.Len() != 0 {
		t.Errorf("expected empty map, got %d entries", hm.Len())
	}
}

func TestVM_MalformedChunk_OutOfRangeLocalSlot(t *testing.T) {
	vm := New(core.NewEnv(nil))
	chunk := &Chunk{
		Name: "test",
		Code: []Instruction{
			Encode(OpGetLocal, 5),
			Encode(OpReturn, 0),
		},
	}

	_, err := vm.Run(context.Background(), chunk)
	if err == nil {
		t.Fatal("expected error for out-of-range local slot, got nil")
	}
	if _, ok := err.(*core.LispicoError); !ok {
		t.Errorf("expected *core.LispicoError, got %T", err)
	}
}

func TestVM_MalformedChunk_OutOfRangeSetLocalSlot(t *testing.T) {
	vm := New(core.NewEnv(nil))
	chunk := &Chunk{
		Name: "test",
		Code: []Instruction{
			Encode(OpTrue, 0),
			Encode(OpSetLocal, 9),
			Encode(OpReturn, 0),
		},
	}

	_, err := vm.Run(context.Background(), chunk)
	if err == nil {
		t.Fatal("expected error for out-of-range local slot, got nil")
	}
	if _, ok := err.(*core.LispicoError); !ok {
		t.Errorf("expected *core.LispicoError, got %T", err)
	}
}

func TestVM_MalformedChunk_MakeListUnderflow(t *testing.T) {
	vm := New(core.NewEnv(nil))
	chunk := &Chunk{
		Name: "test",
		Code: []Instruction{
			Encode(OpMakeList, 5),
			Encode(OpReturn, 0),
		},
	}

	_, err := vm.Run(context.Background(), chunk)
	if err == nil {
		t.Fatal("expected error for make-list exceeding stack, got nil")
	}
	if _, ok := err.(*core.LispicoError); !ok {
		t.Errorf("expected *core.LispicoError, got %T", err)
	}
}

func TestVM_MalformedChunk_MakeVectorUnderflow(t *testing.T) {
	vm := New(core.NewEnv(nil))
	chunk := &Chunk{
		Name: "test",
		Code: []Instruction{
			Encode(OpMakeVector, 3),
			Encode(OpReturn, 0),
		},
	}

	_, err := vm.Run(context.Background(), chunk)
	if err == nil {
		t.Fatal("expected error for make-vector exceeding stack, got nil")
	}
	if _, ok := err.(*core.LispicoError); !ok {
		t.Errorf("expected *core.LispicoError, got %T", err)
	}
}

func TestVM_MalformedChunk_MakeMapUnderflow(t *testing.T) {
	vm := New(core.NewEnv(nil))
	chunk := &Chunk{
		Name: "test",
		Code: []Instruction{
			Encode(OpMakeMap, 2),
			Encode(OpReturn, 0),
		},
	}

	_, err := vm.Run(context.Background(), chunk)
	if err == nil {
		t.Fatal("expected error for make-map exceeding stack, got nil")
	}
	if _, ok := err.(*core.LispicoError); !ok {
		t.Errorf("expected *core.LispicoError, got %T", err)
	}
}

func TestVM_EmptyReturn_Underflow(t *testing.T) {
	vm := New(core.NewEnv(nil))
	chunk := &Chunk{
		Name: "test",
		Code: []Instruction{
			Encode(OpReturn, 0),
		},
	}

	_, err := vm.Run(context.Background(), chunk)
	if err == nil {
		t.Fatal("expected error for bare OpReturn on empty stack, got nil")
	}
	if _, ok := err.(*core.LispicoError); !ok {
		t.Errorf("expected *core.LispicoError, got %T", err)
	}
}

func TestVM_MalformedChunk_CallUnderflow(t *testing.T) {
	vm := New(core.NewEnv(nil))
	chunk := &Chunk{
		Name: "test",
		Code: []Instruction{
			Encode(OpCall, 0),
			Encode(OpReturn, 0),
		},
	}

	_, err := vm.Run(context.Background(), chunk)
	if err == nil {
		t.Fatal("expected error for OpCall on empty stack, got nil")
	}
	if _, ok := err.(*core.LispicoError); !ok {
		t.Errorf("expected *core.LispicoError, got %T", err)
	}
}

func TestVM_MalformedChunk_TailCallUnderflow(t *testing.T) {
	vm := New(core.NewEnv(nil))
	chunk := &Chunk{
		Name: "test",
		Code: []Instruction{
			Encode(OpTailCall, 2),
			Encode(OpReturn, 0),
		},
	}

	_, err := vm.Run(context.Background(), chunk)
	if err == nil {
		t.Fatal("expected error for OpTailCall exceeding stack, got nil")
	}
	if _, ok := err.(*core.LispicoError); !ok {
		t.Errorf("expected *core.LispicoError, got %T", err)
	}
}

func TestVM_MalformedChunk_InstructionPointerOutOfRange(t *testing.T) {
	vm := New(core.NewEnv(nil))
	chunk := &Chunk{
		Name: "test",
		Code: []Instruction{
			Encode(OpJump, 100),
		},
	}

	_, err := vm.Run(context.Background(), chunk)
	if err == nil {
		t.Fatal("expected error for instruction pointer running off the chunk, got nil")
	}
	if _, ok := err.(*core.LispicoError); !ok {
		t.Errorf("expected *core.LispicoError, got %T", err)
	}
}

func TestVM_NativeOpAdd(t *testing.T) {
	t.Parallel()
	env := core.NewEnv(nil)
	env.SetCanonical("+", core.GoFunc{Name: "+", Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
		var s int64
		for _, a := range args {
			s += a.(core.Int).V
		}
		return core.Int{V: s}, nil
	}})
	vm := New(env)
	chunk := &Chunk{Name: "test", Code: []Instruction{
		Encode(OpGetGlobal, 0), // +
		Encode(OpConst, 1), Encode(OpConst, 2),
		Encode(OpAdd, 2),
		Encode(OpReturn, 0),
	}}
	chunk.Constants = []core.Value{core.Symbol{V: "+"}, core.Int{V: 3}, core.Int{V: 4}}

	result, err := vm.Run(context.Background(), chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Equals(core.Int{V: 7}) {
		t.Errorf("expected 7, got %v", result)
	}
}

func TestVM_NativeOpSub(t *testing.T) {
	t.Parallel()
	env := core.NewEnv(nil)
	env.SetCanonical("-", core.GoFunc{Name: "-", Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
		r := args[0].(core.Int).V
		for _, a := range args[1:] {
			r -= a.(core.Int).V
		}
		return core.Int{V: r}, nil
	}})
	vm := New(env)
	chunk := &Chunk{Name: "test", Code: []Instruction{
		Encode(OpGetGlobal, 0),
		Encode(OpConst, 1), Encode(OpConst, 2),
		Encode(OpSub, 2),
		Encode(OpReturn, 0),
	}}
	chunk.Constants = []core.Value{core.Symbol{V: "-"}, core.Int{V: 10}, core.Int{V: 3}}

	result, err := vm.Run(context.Background(), chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Equals(core.Int{V: 7}) {
		t.Errorf("expected 7, got %v", result)
	}
}

func TestVM_NativeOpMul(t *testing.T) {
	t.Parallel()
	env := core.NewEnv(nil)
	env.SetCanonical("*", core.GoFunc{Name: "*", Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
		r := int64(1)
		for _, a := range args {
			r *= a.(core.Int).V
		}
		return core.Int{V: r}, nil
	}})
	vm := New(env)
	chunk := &Chunk{Name: "test", Code: []Instruction{
		Encode(OpGetGlobal, 0),
		Encode(OpConst, 1), Encode(OpConst, 2),
		Encode(OpMul, 2),
		Encode(OpReturn, 0),
	}}
	chunk.Constants = []core.Value{core.Symbol{V: "*"}, core.Int{V: 6}, core.Int{V: 7}}

	result, err := vm.Run(context.Background(), chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Equals(core.Int{V: 42}) {
		t.Errorf("expected 42, got %v", result)
	}
}

func TestVM_NativeOpDiv(t *testing.T) {
	t.Parallel()
	fn := func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
		if len(args) < 2 {
			return nil, fmt.Errorf("/: requires at least 2 arguments")
		}
		d := float64(args[0].(core.Int).V)
		for _, a := range args[1:] {
			div := float64(a.(core.Int).V)
			if div == 0 {
				return nil, fmt.Errorf("/: division by zero")
			}
			d /= div
		}
		return core.Float{V: d}, nil
	}
	env := core.NewEnv(nil)
	env.SetCanonical("/", core.GoFunc{Name: "/", Fn: fn})
	v := New(env)
	chunk := &Chunk{Name: "test", Code: []Instruction{
		Encode(OpGetGlobal, 0),
		Encode(OpConst, 1), Encode(OpConst, 2),
		Encode(OpDiv, 2),
		Encode(OpReturn, 0),
	}}
	chunk.Constants = []core.Value{core.Symbol{V: "/"}, core.Int{V: 10}, core.Int{V: 2}}
	result, err := v.Run(context.Background(), chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Equals(core.Float{V: 5}) {
		t.Errorf("expected 5.0, got %v", result)
	}

	chunk2 := &Chunk{Name: "test", Code: []Instruction{
		Encode(OpGetGlobal, 0),
		Encode(OpConst, 1), Encode(OpConst, 2),
		Encode(OpDiv, 2),
		Encode(OpReturn, 0),
	}}
	chunk2.Constants = []core.Value{core.Symbol{V: "/"}, core.Int{V: 10}, core.Int{V: 0}}
	_, err = v.Run(context.Background(), chunk2)
	if err == nil {
		t.Fatal("expected division by zero error")
	}
}

func TestVM_NativeOpLt(t *testing.T) {
	t.Parallel()
	env := core.NewEnv(nil)
	env.SetCanonical("<", core.GoFunc{Name: "<", Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
		return core.Bool{V: args[0].(core.Int).V < args[1].(core.Int).V}, nil
	}})
	vm := New(env)
	chunk := &Chunk{Name: "test", Code: []Instruction{
		Encode(OpGetGlobal, 0),
		Encode(OpConst, 1), Encode(OpConst, 2),
		Encode(OpLt, 2),
		Encode(OpReturn, 0),
	}}
	chunk.Constants = []core.Value{core.Symbol{V: "<"}, core.Int{V: 1}, core.Int{V: 2}}
	result, err := vm.Run(context.Background(), chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Equals(core.Bool{V: true}) {
		t.Errorf("expected true, got %v", result)
	}
}

func TestVM_NativeOpGt(t *testing.T) {
	t.Parallel()
	env := core.NewEnv(nil)
	env.SetCanonical(">", core.GoFunc{Name: ">", Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
		return core.Bool{V: args[0].(core.Int).V > args[1].(core.Int).V}, nil
	}})
	vm := New(env)
	chunk := &Chunk{Name: "test", Code: []Instruction{
		Encode(OpGetGlobal, 0),
		Encode(OpConst, 1), Encode(OpConst, 2),
		Encode(OpGt, 2),
		Encode(OpReturn, 0),
	}}
	chunk.Constants = []core.Value{core.Symbol{V: ">"}, core.Int{V: 3}, core.Int{V: 2}}
	result, err := vm.Run(context.Background(), chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Equals(core.Bool{V: true}) {
		t.Errorf("expected true, got %v", result)
	}
}

func TestVM_NativeOpLe(t *testing.T) {
	t.Parallel()
	env := core.NewEnv(nil)
	env.SetCanonical("<=", core.GoFunc{Name: "<=", Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
		return core.Bool{V: args[0].(core.Int).V <= args[1].(core.Int).V}, nil
	}})
	vm := New(env)
	chunk := &Chunk{Name: "test", Code: []Instruction{
		Encode(OpGetGlobal, 0),
		Encode(OpConst, 1), Encode(OpConst, 2),
		Encode(OpLe, 2),
		Encode(OpReturn, 0),
	}}
	chunk.Constants = []core.Value{core.Symbol{V: "<="}, core.Int{V: 2}, core.Int{V: 2}}
	result, err := vm.Run(context.Background(), chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Equals(core.Bool{V: true}) {
		t.Errorf("expected true, got %v", result)
	}
}

func TestVM_NativeOpGe(t *testing.T) {
	t.Parallel()
	env := core.NewEnv(nil)
	env.SetCanonical(">=", core.GoFunc{Name: ">=", Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
		return core.Bool{V: args[0].(core.Int).V >= args[1].(core.Int).V}, nil
	}})
	vm := New(env)
	chunk := &Chunk{Name: "test", Code: []Instruction{
		Encode(OpGetGlobal, 0),
		Encode(OpConst, 1), Encode(OpConst, 2),
		Encode(OpGe, 2),
		Encode(OpReturn, 0),
	}}
	chunk.Constants = []core.Value{core.Symbol{V: ">="}, core.Int{V: 2}, core.Int{V: 2}}
	result, err := vm.Run(context.Background(), chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Equals(core.Bool{V: true}) {
		t.Errorf("expected true, got %v", result)
	}
}

func TestVM_NativeOpEq(t *testing.T) {
	t.Parallel()
	env := core.NewEnv(nil)
	env.SetCanonical("=", core.GoFunc{Name: "=", Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
		return core.Bool{V: args[0].Equals(args[1])}, nil
	}})
	vm := New(env)
	chunk := &Chunk{Name: "test", Code: []Instruction{
		Encode(OpGetGlobal, 0),
		Encode(OpConst, 1), Encode(OpConst, 2),
		Encode(OpEq, 2),
		Encode(OpReturn, 0),
	}}
	chunk.Constants = []core.Value{core.Symbol{V: "="}, core.Int{V: 5}, core.Int{V: 5}}
	result, err := vm.Run(context.Background(), chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Equals(core.Bool{V: true}) {
		t.Errorf("expected true, got %v", result)
	}
}

func TestVM_NativeOp_EqString(t *testing.T) {
	t.Parallel()
	env := core.NewEnv(nil)
	env.SetCanonical("=", core.GoFunc{Name: "=", Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
		return core.Bool{V: args[0].Equals(args[1])}, nil
	}})
	vm := New(env)
	chunk := &Chunk{Name: "test", Code: []Instruction{
		Encode(OpGetGlobal, 0),
		Encode(OpConst, 1), Encode(OpConst, 2),
		Encode(OpEq, 2),
		Encode(OpReturn, 0),
	}}
	chunk.Constants = []core.Value{core.Symbol{V: "="}, core.String{V: "hello"}, core.String{V: "hello"}}
	result, err := vm.Run(context.Background(), chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Equals(core.Bool{V: true}) {
		t.Errorf("expected true, got %v", result)
	}
}

// TestVM_NativeOp_ReboundFallback proves env.Set (non-canonical) over a
// canonical binding routes to the custom function, not the native fast path.
func TestVM_NativeOp_ReboundFallback(t *testing.T) {
	t.Parallel()
	env := core.NewEnv(nil)
	env.SetCanonical("+", core.GoFunc{Name: "+", Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
		var s int64
		for _, a := range args {
			s += a.(core.Int).V
		}
		return core.Int{V: s}, nil
	}})
	// Rebind via Set (clears canonical marker) — should fall back.
	env.Set("+", core.GoFunc{Name: "+", Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
		return core.Int{V: 999}, nil
	}})
	vm := New(env)
	chunk := &Chunk{Name: "test", Code: []Instruction{
		Encode(OpGetGlobal, 0),
		Encode(OpConst, 1), Encode(OpConst, 2),
		Encode(OpAdd, 2),
		Encode(OpReturn, 0),
	}}
	chunk.Constants = []core.Value{core.Symbol{V: "+"}, core.Int{V: 1}, core.Int{V: 2}}
	result, err := vm.Run(context.Background(), chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Equals(core.Int{V: 999}) {
		t.Errorf("expected 999 from rebound, got %v", result)
	}
}

// TestVM_NativeOp_ChildRebind proves shadowing + in a child env (which has nil
// canonical map) causes non-canonical fallback, not native fast path.
func TestVM_NativeOp_ChildRebind(t *testing.T) {
	t.Parallel()
	globals := core.NewEnv(nil)
	globals.SetCanonical("+", core.GoFunc{Name: "+", Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
		var s int64
		for _, a := range args {
			s += a.(core.Int).V
		}
		return core.Int{V: s}, nil
	}})

	// Test that a child-env override still triggers fallback.
	child := core.NewEnv(globals)
	child.Set("+", core.GoFunc{Name: "+", Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
		return core.Int{V: 888}, nil
	}})

	vm := New(child)
	chunk := &Chunk{Name: "test", Code: []Instruction{
		Encode(OpGetGlobal, 0),
		Encode(OpConst, 1), Encode(OpConst, 2),
		Encode(OpAdd, 2),
		Encode(OpReturn, 0),
	}}
	chunk.Constants = []core.Value{core.Symbol{V: "+"}, core.Int{V: 1}, core.Int{V: 2}}
	result, err := vm.Run(context.Background(), chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Equals(core.Int{V: 888}) {
		t.Errorf("expected 888 from child rebind, got %v", result)
	}
}

// TestVM_NativeOp_FallbackEnv verifies that when a non-canonical binding
// triggers fallback inside a closure, the env passed to GoFunc.Fn is the
// active frame's env (a child of globals), not vm.globals. secret lives
// only in the closure frame; vm.globals lacks it, so a correct env yields
// 42 while the old globals bug would yield -1.
func TestVM_NativeOp_FallbackEnv(t *testing.T) {
	t.Parallel()

	root := core.NewEnv(nil)
	root.SetCanonical("+", core.GoFunc{
		Name: "+",
		Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
			var s int64
			for _, a := range args {
				if n, ok := a.(core.Int); ok {
					s += n.V
				}
			}
			return core.Int{V: s}, nil
		},
	})

	child := root.Child()
	child.Set("+", core.GoFunc{
		Name: "+",
		Fn: func(_ context.Context, _ core.Evaluator, _ []core.Value, env *core.Env) (core.Value, error) {
			if v, ok := env.Get("secret"); ok {
				return v, nil
			}
			return core.Int{V: -1}, nil
		},
	})
	child.Set("secret", core.Int{V: 42})

	subChunk := &Chunk{Name: "inner"}
	subChunk.Emit(OpGetGlobal, subChunk.AddConstant(core.Symbol{V: "+"}))
	subChunk.Emit(OpConst, subChunk.AddConstant(core.Int{V: 1}))
	subChunk.Emit(OpConst, subChunk.AddConstant(core.Int{V: 2}))
	subChunk.Emit(OpAdd, 2)
	subChunk.Emit(OpReturn, 0)

	mainChunk := &Chunk{Name: "main"}
	mainChunk.Emit(OpConst, mainChunk.AddConstant(NewClosure(subChunk, child)))
	mainChunk.Emit(OpCall, 0)
	mainChunk.Emit(OpReturn, 0)

	vm := New(root)
	result, err := vm.Run(context.Background(), mainChunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Equals(core.Int{V: 42}) {
		t.Errorf("expected 42 (secret from frame env via fallback), got %v", result)
	}
}

// TestVM_NativeOp_SetClearsCanonical verifies that Set over a canonical
// binding removes the canonical marker — even when the value is structurally
// identical.
func TestVM_NativeOp_SetClearsCanonical(t *testing.T) {
	t.Parallel()
	env := core.NewEnv(nil)
	origFn := func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
		var s int64
		for _, a := range args {
			s += a.(core.Int).V
		}
		return core.Int{V: s}, nil
	}
	env.SetCanonical("+", core.GoFunc{Name: "+", Fn: origFn})
	// Set same function struct via Set — clears canonical despite same value.
	env.Set("+", core.GoFunc{Name: "+", Fn: origFn})
	vm := New(env)
	chunk := &Chunk{Name: "test", Code: []Instruction{
		Encode(OpGetGlobal, 0),
		Encode(OpConst, 1), Encode(OpConst, 2),
		Encode(OpAdd, 2),
		Encode(OpReturn, 0),
	}}
	chunk.Constants = []core.Value{core.Symbol{V: "+"}, core.Int{V: 1}, core.Int{V: 2}}
	// dispatchNativeOp checks GetCanonical which returns false after Set.
	// Then vm.call resolves the GoFunc and calls it. Since we lost native
	// fast path, the fallback goes through vm.call/apply which pushes a
	// frame and calls the GoFunc directly — the same origFn runs, producing
	// 3 via the normal call path instead of native fast path.
	result, err := vm.Run(context.Background(), chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Equals(core.Int{V: 3}) {
		t.Errorf("expected 3 (fallback call produces same result), got %v", result)
	}
}

// TestVM_NativeOp_LookupTimeCapture proves canonical status is captured at
// OpGetGlobal time, not re-resolved after args. A custom + (non-canonical)
// is bound, then an arg GoFunc restores canonical as a side effect. The
// dispatch must use the pre-resolved non-canonical status → fallback →
// custom fn returns 999, not native 3.
func TestVM_NativeOp_LookupTimeCapture(t *testing.T) {
	t.Parallel()

	addFn := func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
		var s int64
		for _, a := range args {
			if n, ok := a.(core.Int); ok {
				s += n.V
			}
		}
		return core.Int{V: s}, nil
	}

	root := core.NewEnv(nil)
	root.SetCanonical("+", core.GoFunc{Name: "+", Fn: addFn})

	customPlus := core.GoFunc{
		Name: "+",
		Fn: func(_ context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			return core.Int{V: 999}, nil
		},
	}
	root.Set("+", customPlus)

	restoreFn := core.GoFunc{
		Name: "restore",
		Fn: func(_ context.Context, _ core.Evaluator, _ []core.Value, env *core.Env) (core.Value, error) {
			env.SetCanonical("+", core.GoFunc{Name: "+", Fn: addFn})
			return core.Int{V: 1}, nil
		},
	}
	root.Set("restore", restoreFn)

	// (+ (restore) 2): OpGetGlobal + resolves customPlus (non-canonical),
	// then (restore) side-effect restores canonical, then OpAdd dispatches.
	// Lookup-time capture → no canonicalSlot → fallback → customPlus → 999.
	chunk := &Chunk{Name: "test"}
	chunk.Emit(OpGetGlobal, chunk.AddConstant(core.Symbol{V: "+"}))
	chunk.Emit(OpGetGlobal, chunk.AddConstant(core.Symbol{V: "restore"}))
	chunk.Emit(OpCall, 0)
	chunk.Emit(OpConst, chunk.AddConstant(core.Int{V: 2}))
	chunk.Emit(OpAdd, 2)
	chunk.Emit(OpReturn, 0)

	vm := New(root)
	result, err := vm.Run(context.Background(), chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Equals(core.Int{V: 999}) {
		t.Errorf("expected 999 (custom + via fallback despite mid-eval canonical restore), got %v", result)
	}
}

// TestVM_NativeOp_OpcodeMismatch proves a canonical slot for OpAdd cannot
// be stolen by OpSub. A hand-built chunk resolves canonical + but dispatches
// OpSub at the same fnIdx; the opcode guard must reject and fall back.
func TestVM_NativeOp_OpcodeMismatch(t *testing.T) {
	t.Parallel()

	env := core.NewEnv(nil)
	env.SetCanonical("+", core.GoFunc{Name: "+", Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
		var s int64
		for _, a := range args {
			if n, ok := a.(core.Int); ok {
				s += n.V
			}
		}
		return core.Int{V: s}, nil
	}})

	// OpGetGlobal "+" sets canonicalSlots[slot] = OpAdd.
	// Then OpSub at the same fnIdx: expectedOp=OpAdd ≠ OpSub → fallback.
	chunk := &Chunk{Name: "test", Code: []Instruction{
		Encode(OpGetGlobal, 0),
		Encode(OpConst, 1), Encode(OpConst, 2),
		Encode(OpSub, 2),
		Encode(OpReturn, 0),
	}}
	chunk.Constants = []core.Value{core.Symbol{V: "+"}, core.Int{V: 10}, core.Int{V: 3}}

	vm := New(env)
	result, err := vm.Run(context.Background(), chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Fallback calls + GoFunc (3), not subtraction (7).
	if !result.Equals(core.Int{V: 13}) {
		t.Errorf("expected 13 (addition via fallback, not subtraction), got %v", result)
	}
}

// TestVM_NativeOp_FastPathSkipsGoFunc proves the canonical fast path
// executes native arithmetic without invoking GoFunc.Fn. A counter on the
// GoFunc must stay zero after VM evaluation of a canonical +.
func TestVM_NativeOp_FastPathSkipsGoFunc(t *testing.T) {
	t.Parallel()

	var callCount int
	env := core.NewEnv(nil)
	env.SetCanonical("+", core.GoFunc{
		Name: "+",
		Fn: func(_ context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			callCount++
			return core.Int{V: 0}, nil
		},
	})

	chunk := &Chunk{Name: "test", Code: []Instruction{
		Encode(OpGetGlobal, 0),
		Encode(OpConst, 1), Encode(OpConst, 2),
		Encode(OpAdd, 2),
		Encode(OpReturn, 0),
	}}
	chunk.Constants = []core.Value{core.Symbol{V: "+"}, core.Int{V: 1}, core.Int{V: 2}}

	vm := New(env)
	result, err := vm.Run(context.Background(), chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Equals(core.Int{V: 3}) {
		t.Errorf("expected 3 from native fast path, got %v", result)
	}
	if callCount != 0 {
		t.Errorf("GoFunc.Fn invoked %d time(s); canonical fast path must skip it", callCount)
	}
}

// TestVM_NativeOp_StaleSlotClearedByNonNativeLookup seeds a stale
// canonicalAt entry, then performs a non-native OpGetGlobal at the same
// slot index. The stale entry must be cleared so the subsequent OpAdd falls
// back instead of fast-pathing on a value that is not the operator.
func TestVM_NativeOp_StaleSlotClearedByNonNativeLookup(t *testing.T) {
	t.Parallel()

	env := core.NewEnv(nil)
	env.SetCanonical("+", core.GoFunc{Name: "+", Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
		var s int64
		for _, a := range args {
			if n, ok := a.(core.Int); ok {
				s += n.V
			}
		}
		return core.Int{V: s}, nil
	}})
	env.Set("x", core.Int{V: 10})

	vm := New(env)

	chunk := &Chunk{Name: "test", Code: []Instruction{
		Encode(OpGetGlobal, 0),
		Encode(OpConst, 1), Encode(OpConst, 2),
		Encode(OpAdd, 2),
		Encode(OpReturn, 0),
	}}
	chunk.Constants = []core.Value{core.Symbol{V: "x"}, core.Int{V: 1}, core.Int{V: 2}}

	// Seed stale canonicalAt[0] = OpAdd as if a prior throw left it behind.
	vm.canonicalAt = []Opcode{OpAdd}

	// Run: OpGetGlobal "x" (non-native) must clear slot 0.
	// Then OpAdd at fnIdx=0: no canonicalSlot → fallback → vm.call(Int{10}) → error.
	_, err := vm.Run(context.Background(), chunk)
	if err == nil {
		t.Fatal("expected error calling non-function x=10 via fallback; stale canonicalSlots entry may have survived")
	}
}

func TestVM_UncapturedLocalsNoEnv(t *testing.T) {
	t.Parallel()
	vm := New(core.NewEnv(nil))
	chunk := &Chunk{
		Name:       "test",
		Locals:     1,
		LocalNames: []string{"x"},
		Captured:   nil,
		FullEnv:    false,
		Code: []Instruction{
			Encode(OpConst, 0),
			Encode(OpSetLocal, 0),
			Encode(OpGetLocal, 0),
			Encode(OpReturn, 0),
		},
		Constants: []core.Value{core.Int{V: 42}},
	}

	result, err := vm.Run(context.Background(), chunk)
	require.NoError(t, err)
	assert.True(t, result.Equals(core.Int{V: 42}))
}

func TestVM_CapturedLocalsUseEnv(t *testing.T) {
	t.Parallel()
	vm := New(core.NewEnv(nil))
	chunk := &Chunk{
		Name:       "test",
		Locals:     1,
		LocalNames: []string{"x"},
		Captured:   []bool{true},
		FullEnv:    false,
		Code: []Instruction{
			Encode(OpConst, 0),
			Encode(OpSetLocal, 0),
			Encode(OpGetLocal, 0),
			Encode(OpReturn, 0),
		},
		Constants: []core.Value{core.Int{V: 42}},
	}

	result, err := vm.Run(context.Background(), chunk)
	require.NoError(t, err)
	assert.True(t, result.Equals(core.Int{V: 42}))
}

func TestVM_FullEnvUsesEnv(t *testing.T) {
	t.Parallel()
	vm := New(core.NewEnv(nil))
	chunk := &Chunk{
		Name:       "test",
		Locals:     1,
		LocalNames: []string{"x"},
		Captured:   nil,
		FullEnv:    true,
		Code: []Instruction{
			Encode(OpConst, 0),
			Encode(OpSetLocal, 0),
			Encode(OpGetLocal, 0),
			Encode(OpReturn, 0),
		},
		Constants: []core.Value{core.Int{V: 42}},
	}

	result, err := vm.Run(context.Background(), chunk)
	require.NoError(t, err)
	assert.True(t, result.Equals(core.Int{V: 42}))
}
