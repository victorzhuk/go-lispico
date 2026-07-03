package vm

import (
	"context"
	"testing"

	"github.com/victorzhuk/go-lispico/core"
)

func TestVM_ApplyClosure(t *testing.T) {
	t.Parallel()

	env := core.NewEnv(nil)
	vm := New(env, nil)

	chunk := &Chunk{
		Name:       "<fn>",
		Arity:      2,
		LocalNames: []string{"a", "b"},
		Code: []Instruction{
			Encode(OpGetLocal, 0),
			Encode(OpReturn, 0),
		},
	}
	closure := NewClosure(chunk, env)

	result, err := vm.Apply(context.Background(), closure, []core.Value{core.Int{V: 2}, core.Int{V: 3}}, env)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !result.Equals(core.Int{V: 2}) {
		t.Fatalf("expected 2, got %v", result)
	}
}
