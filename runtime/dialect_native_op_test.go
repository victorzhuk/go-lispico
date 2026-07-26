package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/cl"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/json"
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

			tw, err := New(nil, WithTreeWalker(), WithDialect(cl.Dialect()))
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
// "add" is defined with (def ... (fn ...)), not defun: def binds the value
// cell, defun the function cell. Engine.Call resolves the function cell
// first and falls back to the value cell (mirroring resolveHead's
// head-position order, core/eval.go), so either binding reaches "add"; its
// body's "+" still resolves through the function cell exactly as any other
// Lisp-2 call head does, exercising the same native-op path this fix
// targets.
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

func TestOperatorRedefinitionSurvivesPluginUse(t *testing.T) {
	t.Parallel()

	tw, err := New(nil, WithTreeWalker(), WithDialect(cl.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = tw.Close() })
	require.NoError(t, tw.Use(stdlib.New()))

	vmEng, err := New(nil, WithBytecode(), WithDialect(cl.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = vmEng.Close() })
	require.NoError(t, vmEng.Use(stdlib.New()))

	ctx := context.Background()

	_, err = tw.Eval(ctx, "redef", "(defun + (a b) 999)")
	require.NoError(t, err)
	_, err = vmEng.Eval(ctx, "redef", "(defun + (a b) 999)")
	require.NoError(t, err)

	twGot, err := tw.Eval(ctx, "call-before", "(+ 1 2)")
	require.NoError(t, err)
	assert.True(t, twGot.Equals(core.Int{V: 999}))

	vmGot, err := vmEng.Eval(ctx, "call-before", "(+ 1 2)")
	require.NoError(t, err)
	assert.True(t, vmGot.Equals(core.Int{V: 999}))

	require.NoError(t, tw.Use(json.New()))
	require.NoError(t, vmEng.Use(json.New()))

	twGot, err = tw.Eval(ctx, "call-after", "(+ 1 2)")
	require.NoError(t, err)
	assert.True(t, twGot.Equals(core.Int{V: 999}), "tree-walker call should use redefined +, got %v", twGot)

	vmGot, err = vmEng.Eval(ctx, "call-after", "(+ 1 2)")
	require.NoError(t, err)
	assert.True(t, vmGot.Equals(core.Int{V: 999}), "VM call should use redefined +, got %v", vmGot)
}

func TestNonOperatorBuiltinRedefinitionSurvivesPluginUse(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		redef string
		call  string
		want  core.Value
	}{
		{"map", "(defun map (fn lst) 12345)", "(map nil nil)", core.Int{V: 12345}},
		{"first", "(defun first (lst) 12345)", "(first nil)", core.Int{V: 12345}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tw, err := New(nil, WithTreeWalker(), WithDialect(cl.Dialect()))
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
			_, err = vmEng.Eval(ctx, "redef", tc.redef)
			require.NoError(t, err)

			twGot, err := tw.Eval(ctx, "call-before", tc.call)
			require.NoError(t, err)
			assert.True(t, twGot.Equals(tc.want))

			vmGot, err := vmEng.Eval(ctx, "call-before", tc.call)
			require.NoError(t, err)
			assert.True(t, vmGot.Equals(tc.want))

			require.NoError(t, tw.Use(json.New()))
			require.NoError(t, vmEng.Use(json.New()))

			twGot, err = tw.Eval(ctx, "call-after", tc.call)
			require.NoError(t, err)
			assert.True(t, twGot.Equals(tc.want), "tree-walker builtin redefinition must survive plugin load, got %v", twGot)

			vmGot, err = vmEng.Eval(ctx, "call-after", tc.call)
			require.NoError(t, err)
			assert.True(t, vmGot.Equals(tc.want), "VM builtin redefinition must survive plugin load, got %v", vmGot)
		})
	}
}

func TestPluginGoFuncsCallableAndCanonicalFastPath(t *testing.T) {
	if raceEnabled {
		t.Skip("alloc counts are unreliable under the race detector")
	}

	tw, err := New(nil, WithTreeWalker(), WithDialect(cl.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = tw.Close() })
	require.NoError(t, tw.Use(stdlib.New()))

	vmEng, err := New(nil, WithBytecode(), WithDialect(cl.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = vmEng.Close() })
	require.NoError(t, vmEng.Use(stdlib.New()))

	require.NoError(t, tw.Use(json.New()))
	require.NoError(t, vmEng.Use(json.New()))

	ctx := context.Background()

	got, err := tw.Eval(ctx, "plugin", `(json/encode "x")`)
	require.NoError(t, err)
	assert.Equal(t, core.String{V: "\"x\""}, got)

	got, err = vmEng.Eval(ctx, "plugin", `(json/encode "x")`)
	require.NoError(t, err)
	assert.Equal(t, core.String{V: "\"x\""}, got)

	_, err = vmEng.Eval(ctx, "setup", "(defun add (a b) (+ a b))")
	require.NoError(t, err)
	got, err = vmEng.Eval(ctx, "add-works", "(add 1 2)")
	require.NoError(t, err)
	assert.True(t, got.Equals(core.Int{V: 3}), "add body uses native canonical +, got %v", got)

	got, err = vmEng.Call(ctx, "+", core.Int{V: 1}, core.Int{V: 2})
	require.NoError(t, err)
	assert.True(t, got.Equals(core.Int{V: 3}), "canonical + call got %v", got)

	allocs := testing.AllocsPerRun(1000, func() {
		_, _ = vmEng.Call(ctx, "+", core.Int{V: 1}, core.Int{V: 2})
	})
	assert.LessOrEqual(t, allocs, float64(2), "CL canonical operator Call alloc ceiling, got %v", allocs)
}

func TestReloadPluginRestoresCanonical(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithBytecode(), WithDialect(cl.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	require.NoError(t, eng.Use(stdlib.New()))

	ctx := context.Background()
	_, err = eng.Eval(ctx, "redef", "(defun + (a b) 999)")
	require.NoError(t, err)

	got, err := eng.Eval(ctx, "before", "(+ 1 2)")
	require.NoError(t, err)
	assert.True(t, got.Equals(core.Int{V: 999}), "before reload, redefined + should win, got %v", got)

	require.NoError(t, eng.ReloadPlugin(stdlib.New()))

	got, err = eng.Eval(ctx, "after", "(+ 1 2)")
	require.NoError(t, err)
	assert.True(t, got.Equals(core.Int{V: 3}), "after reload, canonical + should be restored, got %v", got)
}
