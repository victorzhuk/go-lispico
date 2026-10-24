package vm_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/core/compiler"
	"github.com/victorzhuk/go-lispico/core/vm"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
)

// deepEqList builds a list tree with roughly width+depth compared nodes so a
// structural = crosses at least one BuiltinWorkBudget synchronization even at
// small reduction limits.
func deepEqList(t *testing.T, depth, width int) core.Value {
	t.Helper()
	items := make([]core.Value, width)
	for i := range items {
		items[i] = core.List{}
	}
	v := core.NewList(items)
	for i := 1; i < depth; i++ {
		v = core.NewList([]core.Value{v})
	}
	return v
}

func eqChunk(t *testing.T, src string) *vm.Chunk {
	t.Helper()
	forms, err := core.Read(src)
	require.NoError(t, err)
	comp := compiler.NewCompilerWithDialect("<test-eq>", core.Dialect{})
	for _, form := range forms {
		require.NoError(t, comp.Compile(form), "compile %s", src)
	}
	comp.Chunk().Emit(vm.OpReturn, 0)
	comp.Chunk().EnsureSites()
	comp.MarkCaptures()
	return comp.Chunk()
}

func eqTestEnv(t *testing.T, v core.Value) *core.Env {
	t.Helper()
	env := core.NewEnv(nil)
	p := stdlib.New()
	require.NoError(t, p.Init(env))
	if v != nil {
		require.NoError(t, env.RegisterValue("d", v, true))
	}
	return env
}

// TestVM_BoundedEq_ReductionsRefuseDeepPreboundEqual pins that a structural =
// over prebound deep equal collections refuses under a low reduction limit
// instead of answering true through an uncharged full-width traversal.
func TestVM_BoundedEq_ReductionsRefuseDeepPreboundEqual(t *testing.T) {
	t.Parallel()

	d := deepEqList(t, 16, 4096)
	env := eqTestEnv(t, d)
	v := vm.New(env)
	v.SetResourceLimits(64, 0)

	_, err := v.Run(context.Background(), eqChunk(t, "(= d d)"))
	require.Error(t, err, "deep prebound equal comparison must refuse, not complete uncharged")
}

// TestVM_BoundedEq_MatchesTreeWalkerUnderLowLimit pins VM and tree-walker
// equivalence on the refusal: both evaluators must reject the same program at
// the same limit, and both must complete with true when the limit is ample.
func TestVM_BoundedEq_MatchesTreeWalkerUnderLowLimit(t *testing.T) {
	t.Parallel()

	d := deepEqList(t, 16, 4096)

	treeEval := core.NewEvaluator()
	treeEnv := eqTestEnv(t, d)
	forms, err := core.Read("(= d d)")
	require.NoError(t, err)

	_, treeErr := treeEval.Eval(core.WithEvalResourceLimits(context.Background(), 64, 1<<30), forms[0], treeEnv)
	require.Error(t, treeErr, "tree-walker must refuse under the low limit")

	vmV := vm.New(eqTestEnv(t, d))
	vmV.SetResourceLimits(64, 0)
	_, vmErr := vmV.Run(context.Background(), eqChunk(t, "(= d d)"))
	require.Error(t, vmErr, "VM must refuse under the low limit too")

	// Ample budget: both complete and agree.
	treeOK, treeErr := treeEval.Eval(core.WithEvalResourceLimits(context.Background(), int(core.DefaultMaxReductions), 1<<30), forms[0], treeEnv)
	require.NoError(t, treeErr)
	require.True(t, treeOK.Equals(core.BoxBool(true)))

	v2 := vm.New(eqTestEnv(t, d))
	v2.SetResourceLimits(int(core.DefaultMaxReductions), 0)
	vmOK, err := v2.Run(context.Background(), eqChunk(t, "(= d d)"))
	require.NoError(t, err)
	require.True(t, vmOK.Equals(core.BoxBool(true)))
}

// TestVM_BoundedEq_CancellationAndDeadline pins that structural = observes a
// cancelled context and an expired deadline through the budget settlement
// instead of finishing the walk.
func TestVM_BoundedEq_CancellationAndDeadline(t *testing.T) {
	t.Parallel()

	d := deepEqList(t, 16, 4096)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	v := vm.New(eqTestEnv(t, d))
	_, err := v.Run(cancelled, eqChunk(t, "(= d d)"))
	require.ErrorIs(t, err, context.Canceled)

	deadlineCtx, deadlineCancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer deadlineCancel()
	v2 := vm.New(eqTestEnv(t, d))
	_, err = v2.Run(deadlineCtx, eqChunk(t, "(= d d)"))
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

// TestVM_BoundedEq_StructuralResultsAndScalarFastPath pins that the bounded
// path answers correctly (equal true, unequal false) and that scalar-only
// operands keep the direct Equals route.
func TestVM_BoundedEq_StructuralResultsAndScalarFastPath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		src  string
		bind core.Value
		want core.Value
	}{
		{"deep equal", "(= d d)", deepEqList(t, 8, 256), core.BoxBool(true)},
		{"deep unequal", "(= d '(1))", deepEqList(t, 8, 256), core.BoxBool(false)},
		{"scalars", "(= 1 1)", nil, core.BoxBool(true)},
		{"scalar mismatch", "(= 1 2)", nil, core.BoxBool(false)},
		{"strings", "(= \"a\" \"b\")", nil, core.BoxBool(false)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := vm.New(eqTestEnv(t, tc.bind))
			got, err := v.Run(context.Background(), eqChunk(t, tc.src))
			require.NoError(t, err)
			require.True(t, got.Equals(tc.want), "got %v, want %v", got, tc.want)
		})
	}
}
