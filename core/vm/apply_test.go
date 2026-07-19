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

func TestVM_ApplyPooled_Parity(t *testing.T) {
	t.Parallel()

	env := core.NewEnv(nil)

	fixed := &Chunk{
		Name:       "<fn>",
		Arity:      2,
		LocalNames: []string{"a", "b"},
		Code: []Instruction{
			Encode(OpGetLocal, 1),
			Encode(OpReturn, 0),
		},
	}
	variadic := &Chunk{
		Name:       "<fn>",
		Arity:      1,
		Variadic:   true,
		LocalNames: []string{"a", "rest"},
		Code: []Instruction{
			Encode(OpGetLocal, 1),
			Encode(OpReturn, 0),
		},
	}
	m := core.NewHashMap()
	require.NoError(t, m.Set(core.Keyword{V: "name"}, core.String{V: "Alice"}))
	goFn := core.GoFunc{
		Name: "id",
		Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
			return args[0], nil
		},
	}

	tests := []struct {
		name string
		fn   core.Value
		args []core.Value
		want core.Value
	}{
		{"fixed arity closure", NewClosure(fixed, env), []core.Value{core.Int{V: 1}, core.Int{V: 2}}, core.Int{V: 2}},
		{"variadic closure", NewClosure(variadic, env), []core.Value{core.Int{V: 1}, core.Int{V: 2}, core.Int{V: 3}}, core.List{Items: []core.Value{core.Int{V: 2}, core.Int{V: 3}}}},
		{"keyword", core.Keyword{V: "name"}, []core.Value{m}, core.String{V: "Alice"}},
		{"gofunc", goFn, []core.Value{core.Int{V: 5}}, core.Int{V: 5}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm := New(env)
			result, err := vm.ApplyPooled(context.Background(), tt.fn, tt.args, env)
			require.NoError(t, err)
			require.True(t, tt.want.Equals(result), "got %v, want %v", result, tt.want)
		})
	}
}

// TestVM_ApplyPooled_ArityErrorNoPartialState proves an arity mismatch is
// caught before apply pushes the closure's frame — a wrong argc must leave
// the receiver's stack and frames exactly as they were.
func TestVM_ApplyPooled_ArityErrorNoPartialState(t *testing.T) {
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

	beforeStack := vm.stackSize()
	beforeFrames := vm.frameCount()

	_, err := vm.ApplyPooled(context.Background(), closure, []core.Value{core.Int{V: 1}}, env)
	require.Error(t, err)
	var le *core.LispicoError
	require.ErrorAs(t, err, &le)
	require.Equal(t, "ArityError", le.Code)

	require.Equal(t, beforeStack, vm.stackSize(), "arity error must not leave a partial push")
	require.Equal(t, beforeFrames, vm.frameCount(), "arity error must not push a frame")
}

// TestVM_ApplyPooled_NoWrapperChunkAllocs proves ApplyPooled no longer
// allocates a per-call wrapper Chunk (struct + Constants slice + Code slice)
// to run a closure. That wrapper cost ~4 allocs on top of the call itself;
// bounding well under that catches a regression back to it.
func TestVM_ApplyPooled_NoWrapperChunkAllocs(t *testing.T) {
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

	vm.Reset()
	allocs := testing.AllocsPerRun(100, func() {
		vm.Reset()
		result, err := vm.ApplyPooled(ctx, closure, args, env)
		if err != nil {
			panic(err)
		}
		testSink = result
	})

	t.Logf("ApplyPooled AllocsPerRun: %.1f", allocs)
	if allocs > 3 {
		t.Fatalf("ApplyPooled allocates %.1f/run, want <= 3 (wrapper Chunk removed)", allocs)
	}
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
