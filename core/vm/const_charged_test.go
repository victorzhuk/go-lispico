package vm_test

import (
	"errors"
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
