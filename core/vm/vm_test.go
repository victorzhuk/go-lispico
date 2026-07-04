package vm

import (
	"context"
	"testing"

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
