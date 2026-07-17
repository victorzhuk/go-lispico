package vm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

func TestVM_ApplyClosure(t *testing.T) {
	t.Parallel()

	env := core.NewEnv(nil)
	vm := New(env)

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

func TestVM_ApplyPooled_Basic(t *testing.T) {
	t.Parallel()

	env := core.NewEnv(nil)
	vm := New(env)

	chunk := &Chunk{
		Name:       "<fn>",
		Arity:      1,
		LocalNames: []string{"x"},
		Code: []Instruction{
			Encode(OpGetLocal, 0),
			Encode(OpReturn, 0),
		},
	}
	closure := NewClosure(chunk, env)

	// First call on fresh VM
	result, err := vm.ApplyPooled(context.Background(), closure, []core.Value{core.Int{V: 42}}, env)
	require.NoError(t, err)
	require.True(t, core.Int{V: 42}.Equals(result))

	// Reset and call again — same instance, no fresh VM allocation
	vm.Reset()
	result, err = vm.ApplyPooled(context.Background(), closure, []core.Value{core.Int{V: 99}}, env)
	require.NoError(t, err)
	require.True(t, core.Int{V: 99}.Equals(result))
}

func TestVM_ApplyPooled_GoFunc(t *testing.T) {
	t.Parallel()

	env := core.NewEnv(nil)
	vm := New(env, WithEvaluator(core.NewEvaluator()))

	fn := core.GoFunc{
		Name: "id",
		Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
			return args[0], nil
		},
	}

	result, err := vm.ApplyPooled(context.Background(), fn, []core.Value{core.Int{V: 7}}, env)
	require.NoError(t, err)
	require.True(t, core.Int{V: 7}.Equals(result))
}

// TestVM_ApplyPreservesFreshIsolation proves VM.Apply still creates a fresh VM
// and does not alter the receiver's state.
func TestVM_ApplyPreservesFreshIsolation(t *testing.T) {
	t.Parallel()

	env := core.NewEnv(nil)
	vm := New(env)

	chunk := &Chunk{
		Name:       "<fn>",
		Arity:      1,
		LocalNames: []string{"x"},
		Code: []Instruction{
			Encode(OpGetLocal, 0),
			Encode(OpReturn, 0),
		},
	}
	closure := NewClosure(chunk, env)

	// VM.Apply must not affect receiver state
	beforeStack := vm.stackSize()
	beforeFrames := vm.frameCount()

	result, err := vm.Apply(context.Background(), closure, []core.Value{core.Int{V: 1}}, env)
	require.NoError(t, err)
	require.True(t, core.Int{V: 1}.Equals(result))

	// Receiver unchanged
	require.Equal(t, beforeStack, vm.stackSize(), "VM.Apply must not alter receiver stack")
	require.Equal(t, beforeFrames, vm.frameCount(), "VM.Apply must not alter receiver frames")
}

func TestVM_ApplyKeyword(t *testing.T) {
	t.Parallel()

	env := core.NewEnv(nil)
	v := New(env)

	m := core.NewHashMap()
	require.NoError(t, m.Set(core.Keyword{V: "name"}, core.String{V: "Alice"}))

	result, err := v.Apply(context.Background(), core.Keyword{V: "name"}, []core.Value{m}, env)
	require.NoError(t, err)
	require.True(t, core.String{V: "Alice"}.Equals(result))

	result, err = v.Apply(context.Background(), core.Keyword{V: "missing"}, []core.Value{m}, env)
	require.NoError(t, err)
	require.True(t, core.Nil{}.Equals(result))

	result, err = v.Apply(context.Background(), core.Keyword{V: "name"}, []core.Value{core.Int{V: 42}}, env)
	require.NoError(t, err)
	require.True(t, core.Nil{}.Equals(result))

	_, err = v.ApplyPooled(context.Background(), core.Keyword{V: "name"}, []core.Value{m, m}, env)
	require.Error(t, err)
	var le *core.LispicoError
	require.ErrorAs(t, err, &le)
	require.Equal(t, "EvalError", le.Code)
}

var testSink core.Value

// TestVM_ApplyPooled_AllocationRegression proves ApplyPooled with reset
// receiver allocates fewer bytes than a fresh Apply for the same closure.
// This directly detects any reintroduced vm.New in ApplyPooled.
func TestVM_ApplyPooled_AllocationRegression(t *testing.T) {
	env := core.NewEnv(nil)
	vm := New(env)

	chunk := &Chunk{
		Name:       "<fn>",
		Arity:      1,
		LocalNames: []string{"x"},
		Code: []Instruction{
			Encode(OpGetLocal, 0),
			Encode(OpReturn, 0),
		},
	}
	closure := NewClosure(chunk, env)
	args := []core.Value{core.Int{V: 42}}
	ctx := context.Background()

	// Fresh Apply — allocates a new VM per call.
	freshAllocs := testing.AllocsPerRun(100, func() {
		result, err := vm.Apply(ctx, closure, args, env)
		if err != nil {
			panic(err)
		}
		testSink = result
	})

	// Pooled Apply — reuses receiver, reset between calls.
	vm.Reset()
	pooledAllocs := testing.AllocsPerRun(100, func() {
		vm.Reset()
		result, err := vm.ApplyPooled(ctx, closure, args, env)
		if err != nil {
			panic(err)
		}
		testSink = result
	})

	t.Logf("fresh AllocsPerRun: %.1f, pooled AllocsPerRun: %.1f", freshAllocs, pooledAllocs)
	if pooledAllocs >= freshAllocs {
		t.Fatalf("ApplyPooled (%.1f allocs/run) must allocate fewer than fresh Apply (%.1f allocs/run)", pooledAllocs, freshAllocs)
	}
}
