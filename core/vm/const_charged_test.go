package vm_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/core/compiler"
	"github.com/victorzhuk/go-lispico/core/vm"
	lispicoruntime "github.com/victorzhuk/go-lispico/runtime"
)

const constChargedLiteralSrc = `{:model :large :tools [:read :grep] :routes [{:name :read :score [1 2 3]} {:name :grep :score [4 5 6]}]}`

func TestConstCharged_LedgerParity(t *testing.T) {
	t.Parallel()

	literal := readOneConstChargedForm(t, constChargedLiteralSrc)
	skeleton := literalSkeletonBytes(literal)
	require.Greater(t, skeleton, int64(1))
	limits := constChargedLimits(int(skeleton - 1))

	treeErr := runConstChargedProgram(t, false, limits, constChargedLiteralSrc)
	vmErr := runConstChargedProgram(t, true, limits, constChargedLiteralSrc)

	treeLerr := requireConstChargedResourceLimit(t, treeErr)
	vmLerr := requireConstChargedResourceLimit(t, vmErr)
	assert.Equal(t, treeLerr.Code, vmLerr.Code)
	assert.Equal(t, treeLerr.Message, vmLerr.Message)
}

func TestConstCharged_SkeletonChargeGuard(t *testing.T) {
	t.Parallel()

	literal := readOneConstChargedForm(t, constChargedLiteralSrc)
	skeleton := literalSkeletonBytes(literal)
	deep := core.ValueDeepBytes(literal)
	require.Greater(t, deep, skeleton+1)

	limits := constChargedLimits(int((skeleton + deep) / 2))
	for _, tt := range []struct {
		name     string
		bytecode bool
	}{
		{name: "tree-walker"},
		{name: "vm", bytecode: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := evalConstChargedProgram(t, tt.bytecode, limits, constChargedLiteralSrc)
			require.NoError(t, err)
			assert.True(t, literal.Equals(got), "got %v, want %v", got, literal)
		})
	}
}

func TestConstCharged_DepthParityNotCatchable(t *testing.T) {
	t.Parallel()

	limits := constChargedLimits(1 << 20)
	limits.MaxStructuralDepth = 1
	body := `(try {:model :large :tools [:read :grep]} (catch e 'caught))`

	treeErr := runConstChargedProgram(t, false, limits, body)
	vmErr := runConstChargedProgram(t, true, limits, body)

	treeLerr := requireConstChargedResourceLimit(t, treeErr)
	vmLerr := requireConstChargedResourceLimit(t, vmErr)
	assert.Equal(t, treeLerr.Code, vmLerr.Code)
	assert.Equal(t, treeLerr.Message, vmLerr.Message)
}

func TestConstCharged_SharingReturnsSameHashMap(t *testing.T) {
	t.Parallel()

	eng := newConstChargedEngine(t, true, constChargedLimits(1<<20))

	first, err := eng.Eval(t.Context(), "folded-sharing", `{:model :large :tools [:read :grep]}`)
	require.NoError(t, err)
	second, err := eng.Eval(t.Context(), "folded-sharing", `{:model :large :tools [:read :grep]}`)
	require.NoError(t, err)

	assert.True(t, first.Equals(second), "first %v, second %v", first, second)
	firstMap, ok := first.(*core.HashMap)
	require.True(t, ok, "first result type %T", first)
	secondMap, ok := second.(*core.HashMap)
	require.True(t, ok, "second result type %T", second)
	// Sharing is by design: immutable values, no in-language identity primitive;
	// quasiquoted HashMaps already share this behavior.
	assert.Same(t, firstMap, secondMap)
}

func TestBatchedAlloc_CrossvalMaxAllocationBytes(t *testing.T) {
	t.Parallel()

	limit := int(core.MeterScalarBytes * 10)
	src := batchedAllocLoopSource(128)

	_, treeErr := evalBatchedAllocTree(t, src, limit)
	_, vmErr := evalBatchedAllocVM(t, src, limit)

	treeLerr := requireConstChargedResourceLimit(t, treeErr)
	vmLerr := requireConstChargedResourceLimit(t, vmErr)
	assert.Equal(t, treeLerr.Code, vmLerr.Code)
	assert.Equal(t, treeLerr.Message, vmLerr.Message)
}

func TestBatchedAlloc_MeterAccountingByteIdentical(t *testing.T) {
	t.Parallel()

	const scalarOps = 17
	meter := &recordingEvalMeter{}
	ctx := core.WithEvalResourceLimits(core.WithEvalMeter(t.Context(), meter), 1_000_000, 1_000_000)
	top, err := core.StartEval(ctx)
	require.NoError(t, err)

	env := newBatchedAllocEnv(t)
	chunk := batchedScalarOpsChunk(scalarOps)
	v := vm.New(env)
	v.SetEvalMeter(core.EvalMeterFrom(ctx))
	got, runErr := v.Run(ctx, chunk)
	finishErr := core.FinishEval(ctx, top)

	require.NoError(t, runErr)
	require.NoError(t, finishErr)
	require.True(t, core.Int{V: 3}.Equals(got), "got %v", got)
	assert.Equal(t, int64(scalarOps)*core.MeterScalarBytes, meter.leasedAlloc-meter.returnedAlloc)
}

type recordingEvalMeter struct {
	leasedRed     int64
	leasedAlloc   int64
	returnedRed   int64
	returnedAlloc int64
}

func (m *recordingEvalMeter) LeaseEval(reductions, allocBytes int64) (int64, int64, error) {
	m.leasedRed += reductions
	m.leasedAlloc += allocBytes
	return reductions, allocBytes, nil
}

func (m *recordingEvalMeter) ReturnEval(reductions, allocBytes int64) {
	m.returnedRed += reductions
	m.returnedAlloc += allocBytes
}

func (m *recordingEvalMeter) ChargeRetained(_, _ int64) error { return nil }

func (m *recordingEvalMeter) ReleaseRetained(_, _ int64) {}

func batchedAllocLoopSource(iterations int) string {
	return fmt.Sprintf(`(loop [i 0 acc 0] (if (< i %d) (recur (+ i 1) (+ acc 1)) acc))`, iterations)
}

func evalBatchedAllocTree(t *testing.T, src string, maxAllocationBytes int) (core.Value, error) {
	t.Helper()

	forms := readConstChargedForms(t, src)
	env := newBatchedAllocEnv(t)
	eval := core.NewEvaluator()
	ctx := core.WithEvalResourceLimits(t.Context(), 1_000_000, maxAllocationBytes)
	var result core.Value = core.Nil{}
	var err error
	for _, form := range forms {
		result, err = eval.Eval(ctx, form, env)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func evalBatchedAllocVM(t *testing.T, src string, maxAllocationBytes int) (core.Value, error) {
	t.Helper()

	forms := readConstChargedForms(t, src)
	chunks, err := compiler.CompileAll(forms)
	require.NoError(t, err)

	env := newBatchedAllocEnv(t)
	ctx := core.WithEvalResourceLimits(t.Context(), 1_000_000, maxAllocationBytes)
	v := vm.New(env)
	var result core.Value = core.Nil{}
	for _, chunk := range chunks {
		v.SetEvalMeter(core.EvalMeterFrom(ctx))
		result, err = v.Run(ctx, chunk)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func newBatchedAllocEnv(t *testing.T) *core.Env {
	t.Helper()

	env := core.NewEnv(nil)
	add := core.GoFunc{
		Name: "+",
		Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
			var sum int64
			for _, arg := range args {
				sum += arg.(core.Int).V
			}
			return core.Int{V: sum}, nil
		},
	}
	lt := core.GoFunc{
		Name: "<",
		Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
			return core.Bool{V: args[0].(core.Int).V < args[1].(core.Int).V}, nil
		},
	}
	require.NoError(t, env.SetCanonical("+", add))
	require.NoError(t, env.SetFuncCanonical("+", add))
	require.NoError(t, env.SetCanonical("<", lt))
	require.NoError(t, env.SetFuncCanonical("<", lt))
	return env
}

func batchedScalarOpsChunk(scalarOps int) *vm.Chunk {
	code := make([]vm.Instruction, 0, scalarOps*5+1)
	for i := range scalarOps {
		if i > 0 {
			code = append(code, vm.Encode(vm.OpPop, 0))
		}
		code = append(
			code,
			vm.Encode(vm.OpFreezeNative, 0),
			vm.Encode(vm.OpConst, 1),
			vm.Encode(vm.OpConst, 2),
			vm.Encode(vm.OpAdd, 2),
		)
	}
	code = append(code, vm.Encode(vm.OpReturn, 0))

	chunk := &vm.Chunk{
		Name:     "batched-scalar-ops",
		MaxStack: 3,
		Constants: []core.Value{
			core.Symbol{V: "+"},
			core.Int{V: 1},
			core.Int{V: 2},
		},
		Code: code,
	}
	chunk.EnsureSites()
	return chunk
}

func runConstChargedProgram(t *testing.T, bytecode bool, limits lispicoruntime.ResourceLimits, body string) error {
	t.Helper()
	_, err := evalConstChargedProgram(t, bytecode, limits, body)
	return err
}

func evalConstChargedProgram(t *testing.T, bytecode bool, limits lispicoruntime.ResourceLimits, body string) (core.Value, error) {
	t.Helper()

	eng := newConstChargedEngine(t, bytecode, limits)
	bindConstChargedFn(t, eng, bytecode, "literal", body)
	return eng.Call(t.Context(), "literal")
}

func newConstChargedEngine(t *testing.T, bytecode bool, limits lispicoruntime.ResourceLimits) lispicoruntime.Engine {
	t.Helper()

	opts := []lispicoruntime.EngineOption{
		lispicoruntime.WithDialect(clojure.Dialect()),
		lispicoruntime.WithResourceLimits(limits),
	}
	if bytecode {
		opts = append(opts, lispicoruntime.WithBytecode())
	} else {
		opts = append(opts, lispicoruntime.WithTreeWalker())
	}
	eng, err := lispicoruntime.New(nil, opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	return eng
}

func bindConstChargedFn(t *testing.T, eng lispicoruntime.Engine, bytecode bool, name, body string) {
	t.Helper()

	var fn core.Value
	if bytecode {
		fn = bytecodeConstChargedFn(t, body)
	} else {
		fn = core.Lambda{Body: readConstChargedForms(t, body), Env: core.NewEnv(nil)}
	}
	require.NoError(t, eng.Bind(name, fn))
}

func bytecodeConstChargedFn(t *testing.T, body string) core.Value {
	t.Helper()

	forms := readConstChargedForms(t, "(fn [] "+body+")")
	chunks, err := compiler.CompileAll(forms)
	require.NoError(t, err)
	got, err := vm.New(core.NewEnv(nil)).Run(t.Context(), chunks[0])
	require.NoError(t, err)
	return got
}

func readOneConstChargedForm(t *testing.T, src string) core.Value {
	t.Helper()

	forms := readConstChargedForms(t, src)
	require.Len(t, forms, 1)
	return forms[0]
}

func readConstChargedForms(t *testing.T, src string) []core.Value {
	t.Helper()

	forms, err := core.Read(src)
	require.NoError(t, err, "read source")
	return forms
}

func literalSkeletonBytes(v core.Value) int64 {
	switch val := v.(type) {
	case core.List:
		bytes := core.ListShallowBytes(val.Len())
		for _, item := range val.ToSlice() {
			bytes += literalSkeletonBytes(item)
		}
		return bytes
	case core.Vector:
		bytes := core.VectorShallowBytes(val.Len())
		for i := range val.Len() {
			bytes += literalSkeletonBytes(val.At(i))
		}
		return bytes
	case *core.HashMap:
		bytes := core.HashMapShallowBytes(val.Len())
		val.Each(func(k, v core.Value) {
			bytes += literalSkeletonBytes(k) + literalSkeletonBytes(v)
		})
		return bytes
	default:
		return 0
	}
}

func constChargedLimits(maxAllocationBytes int) lispicoruntime.ResourceLimits {
	return lispicoruntime.ResourceLimits{
		MaxReaderDepth:     200,
		MaxStructuralDepth: 1 << 20,
		MaxCollectionLen:   1 << 30,
		MaxCacheEntries:    4096,
		MaxReductions:      1_000_000,
		MaxAllocationBytes: maxAllocationBytes,
	}
}

func requireConstChargedResourceLimit(t *testing.T, err error) *core.LispicoError {
	t.Helper()

	require.Error(t, err)
	var lerr *core.LispicoError
	require.True(t, errors.As(err, &lerr), "expected *core.LispicoError, got %T: %v", err, err)
	require.Equal(t, core.CodeResourceLimit, lerr.Code)
	require.True(t, core.IsTerminalEvalError(err), "expected terminal error: %v", err)
	return lerr
}
