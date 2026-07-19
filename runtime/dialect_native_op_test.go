package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/cl"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
)

// TestDialectNativeOp_CL_FunctionCellParity guards the CL (Lisp-2) native-op
// divergence: a native-op head under CL resolves through the function cell,
// not the value cell, so a defun rebind must be observed by the VM's native
// path exactly like the tree-walker observes it. Before compileNativeOp
// became dialect-aware, the VM always froze off the value cell, so a defun
// rebind of "+"/"-"/"<" was invisible to it and it kept executing the
// original operator semantics while the tree-walker ran the redefinition.
func TestDialectNativeOp_CL_FunctionCellParity(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		redef string
		call  string
		want  core.Value
	}{
		{"plus redefined to subtract", "(defun + (a b) (- a b))", "(+ 5 3)", core.Int{V: 2}},
		{"minus redefined to add", "(defun - (a b) (+ a b))", "(- 5 3)", core.Int{V: 8}},
		{"lt redefined to constant", "(defun < (a b) true)", "(< 5 3)", core.Bool{V: true}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tw, err := New(nil, WithDialect(cl.Dialect()))
			require.NoError(t, err)
			t.Cleanup(func() { _ = tw.Close() })
			require.NoError(t, tw.Use(stdlib.New()))

			vmEng, err := New(nil, WithBytecode(), WithDialect(cl.Dialect()))
			require.NoError(t, err)
			t.Cleanup(func() { _ = vmEng.Close() })
			require.NoError(t, vmEng.Use(stdlib.New()))

			ctx := context.Background()

			_, err = tw.Eval(ctx, "redef", tc.redef)
			require.NoError(t, err)
			twGot, err := tw.Eval(ctx, "call", tc.call)
			require.NoError(t, err)

			_, err = vmEng.Eval(ctx, "redef", tc.redef)
			require.NoError(t, err)
			vmGot, err := vmEng.Eval(ctx, "call", tc.call)
			require.NoError(t, err)

			assert.True(t, vmGot.Equals(twGot), "VM %v (%T) != tree-walker %v (%T)", vmGot, vmGot, twGot, twGot)
			assert.True(t, vmGot.Equals(tc.want), "got %v, want %v", vmGot, tc.want)
		})
	}
}

// TestDialectNativeOp_CL_CanonicalUnaffected verifies a plain, non-redefined
// canonical "+" still works under CL bytecode after the function-cell fix.
func TestDialectNativeOp_CL_CanonicalUnaffected(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithBytecode(), WithDialect(cl.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	require.NoError(t, eng.Use(stdlib.New()))

	got, err := eng.Eval(context.Background(), "plain", "(+ 1 2)")
	require.NoError(t, err)
	assert.True(t, got.Equals(core.Int{V: 3}), "canonical CL + must still work under VM, got %v", got)
}

// TestDialectNativeOp_CL_NoGoFuncDispatch mirrors
// TestBytecodeRuntime_NativeOpNoGoFuncDispatch for CL: a canonical "+" under
// Lisp-2 must hit the same native fast path as under Clojure, not fall back
// to GoFunc dispatch through the function cell.
//
// "add" is defined with (def ... (fn ...)), not defun: defun binds only the
// function cell under Lisp-2, and Engine.Call resolves a callee name through
// the value cell only — a defun'd function is unreachable from Call at all
// (a separate, pre-existing gap, not this fix's concern). def binds the value
// cell, so Call can find "add"; its body's "+" still resolves through the
// function cell exactly as any other Lisp-2 call head does, exercising the
// same native-op path this fix targets.
func TestDialectNativeOp_CL_NoGoFuncDispatch(t *testing.T) {
	if raceEnabled {
		t.Skip("alloc counts are unreliable under the race detector")
	}

	eng, err := New(nil, WithBytecode(), WithDialect(cl.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	require.NoError(t, eng.Use(stdlib.New()))

	ctx := context.Background()
	_, err = eng.Eval(ctx, "setup", "(def add (fn (a b) (+ a b)))")
	require.NoError(t, err)

	got, err := eng.Call(ctx, "add", core.Int{V: 1}, core.Int{V: 2})
	require.NoError(t, err)
	require.True(t, got.Equals(core.Int{V: 3}), "add(1,2) = 3, got %v", got)

	allocs := testing.AllocsPerRun(1000, func() {
		_, _ = eng.Call(ctx, "add", core.Int{V: 1}, core.Int{V: 2})
	})
	assert.LessOrEqual(t, allocs, float64(2), "CL native op dispatch alloc ceiling, got %v", allocs)
}
