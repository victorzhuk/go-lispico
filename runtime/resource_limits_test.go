package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
)

// evalLimits builds an Engine in the requested mode with the given resource
// limits, loads stdlib, and evaluates src. Returns the result and error so
// callers can assert agreement between the tree-walker and bytecode evaluators.
func evalLimits(t *testing.T, bytecode bool, limits ResourceLimits, src string) (core.Value, error) {
	t.Helper()
	opts := []EngineOption{WithResourceLimits(limits), WithDialect(clojure.Dialect())}
	if bytecode {
		opts = append(opts, WithBytecode())
	}
	e, err := New(nil, opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })
	require.NoError(t, e.Use(stdlib.New()))
	bindBuiltin(t, e, "+")
	return e.Eval(context.Background(), "test", src)
}

func isResourceLimit(t *testing.T, err error) bool {
	t.Helper()
	var lerr *core.LispicoError
	return errors.As(err, &lerr) && lerr.Code == core.CodeResourceLimit
}

// deepVector returns source for a vector nested n deep: [[[...1...]]].
func deepVector(n int) string {
	return strings.Repeat("[", n) + "1" + strings.Repeat("]", n)
}

// deepMap returns source for a map literal nested n deep: {:k {:k ... 1 ...}}.
func deepMap(n int) string {
	var b strings.Builder
	for range n {
		b.WriteString("{:k ")
	}
	b.WriteString("1")
	for range n {
		b.WriteString("}")
	}
	return b.String()
}

// Limits that allow parsing (reader 200) but bound structural depth low, so the
// reader does not reject first and the evaluator/compiler ceiling is exercised.
var lowStruct = ResourceLimits{MaxReaderDepth: 200, MaxStructuralDepth: 10, MaxCollectionLen: 1 << 30, MaxCacheEntries: 4096}

func TestLimits_LiveBranchFailsBoth(t *testing.T) {
	src := "(if true " + deepVector(50) + " 1)"
	for _, bc := range []bool{false, true} {
		_, err := evalLimits(t, bc, lowStruct, src)
		assert.True(t, isResourceLimit(t, err), "bytecode=%v: expected ResourceLimitError, got %v", bc, err)
	}
}

func TestLimits_DeadBranchSucceedsBoth(t *testing.T) {
	src := "(if false " + deepVector(50) + " 1)"
	for _, bc := range []bool{false, true} {
		v, err := evalLimits(t, bc, lowStruct, src)
		require.NoError(t, err, "bytecode=%v", bc)
		assert.True(t, core.Int{V: 1}.Equals(v), "bytecode=%v: dead branch must return 1", bc)
	}
}

func TestLimits_UncalledFnBodyNotEnforced(t *testing.T) {
	src := "(do (fn [] " + deepVector(50) + ") 1)"
	for _, bc := range []bool{false, true} {
		v, err := evalLimits(t, bc, lowStruct, src)
		require.NoError(t, err, "bytecode=%v: defining an uncalled fn with a deep body must succeed", bc)
		assert.True(t, core.Int{V: 1}.Equals(v), "bytecode=%v", bc)
	}
}

func TestLimits_CalledFnBodyEnforced(t *testing.T) {
	src := "((fn [] " + deepVector(50) + "))"
	for _, bc := range []bool{false, true} {
		_, err := evalLimits(t, bc, lowStruct, src)
		assert.True(t, isResourceLimit(t, err), "bytecode=%v: calling a fn with a deep body must reject", bc)
	}
}

func TestLimits_QuasiquoteMapFailsBoth(t *testing.T) {
	src := "(quasiquote " + deepMap(20) + ")"
	for _, bc := range []bool{false, true} {
		_, err := evalLimits(t, bc, lowStruct, src)
		assert.True(t, isResourceLimit(t, err), "bytecode=%v: quasiquoted nested map must reject", bc)
	}
}

func TestLimits_NestedCallsDoNotTripStructural(t *testing.T) {
	src := "1"
	for range 40 {
		src = "(+ " + src + ")"
	}
	lim := ResourceLimits{MaxReaderDepth: 200, MaxStructuralDepth: 5, MaxCollectionLen: 1 << 30, MaxCacheEntries: 4096}
	for _, bc := range []bool{false, true} {
		_, err := evalLimits(t, bc, lim, src)
		assert.NoError(t, err, "bytecode=%v: nested calls must not trip structural depth", bc)
	}
}

// TestLimits_SharedCounterAcrossCallback: an outer literal vector elevates the
// structural counter, and a lambda invoked via map shares it. With limit 6 the
// lambda's 6-deep body alone passes (6 == 6) but inside the outer vector (1+6)
// fails — proving the counter survives VM→GoFunc→eval.Apply in both evaluators.
func TestLimits_SharedCounterAcrossCallback(t *testing.T) {
	body6 := deepVector(6)
	alone := "(map (fn [x] " + body6 + ") (list 1))"    // body depth 6 == limit 6 → ok
	inside := "[(map (fn [x] " + body6 + ") (list 1))]" // outer 1 + body 6 = 7 > 6 → reject
	lim := ResourceLimits{MaxReaderDepth: 200, MaxStructuralDepth: 6, MaxCollectionLen: 1 << 30, MaxCacheEntries: 4096}
	for _, bc := range []bool{false, true} {
		_, err := evalLimits(t, bc, lim, alone)
		assert.NoError(t, err, "bytecode=%v: lambda body alone (6 == limit) must succeed", bc)
		_, err = evalLimits(t, bc, lim, inside)
		assert.True(t, isResourceLimit(t, err), "bytecode=%v: outer vector + lambda body (1+6 > 6) must reject — shared counter", bc)
	}
}

func TestLimits_TryCatchNotCatchable(t *testing.T) {
	src := "(try " + deepVector(50) + " (catch e 1))"
	for _, bc := range []bool{false, true} {
		_, err := evalLimits(t, bc, lowStruct, src)
		assert.True(t, isResourceLimit(t, err), "bytecode=%v: resource-limit breach must NOT be caught by try", bc)
	}
}

func TestLimits_NegativeNormalize(t *testing.T) {
	neg := ResourceLimits{MaxReaderDepth: -5, MaxStructuralDepth: -3, MaxCollectionLen: -2, MaxCacheEntries: -1}
	for _, bc := range []bool{false, true} {
		v, err := evalLimits(t, bc, neg, "(+ 1 2)")
		require.NoError(t, err, "bytecode=%v: negative limits must normalize and still run", bc)
		assert.True(t, core.Int{V: 3}.Equals(v), "bytecode=%v", bc)
	}
	_, err := evalLimits(t, false, neg, deepVector(5000))
	assert.True(t, isResourceLimit(t, err), "negative-normalized default must still reject extreme depth")
}

func TestLimits_RangeCapViaRegistration(t *testing.T) {
	lim := ResourceLimits{MaxReaderDepth: 200, MaxStructuralDepth: 1024, MaxCollectionLen: 100, MaxCacheEntries: 4096}
	for _, bc := range []bool{false, true} {
		_, err := evalLimits(t, bc, lim, "(range 0 999999)")
		assert.True(t, isResourceLimit(t, err), "bytecode=%v: oversized range must reject at MaxCollectionLen", bc)
		v, err := evalLimits(t, bc, lim, "(range 0 5)")
		require.NoError(t, err, "bytecode=%v", bc)
		assert.NotNil(t, v)
	}
}

func TestLimits_RangeCancelledContext(t *testing.T) {
	e, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })
	require.NoError(t, e.Use(stdlib.New()))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = e.Eval(ctx, "test", "(range 0 1000000000)")
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled), "expected context.Canceled, got %v", err)
}

func TestLimits_ReaderCeilingConfigured(t *testing.T) {
	lim := ResourceLimits{MaxReaderDepth: 20, MaxStructuralDepth: 1024, MaxCollectionLen: 1 << 30, MaxCacheEntries: 4096}
	for _, bc := range []bool{false, true} {
		_, err := evalLimits(t, bc, lim, deepVector(100))
		assert.True(t, isResourceLimit(t, err), "bytecode=%v: deep source must reject at configured MaxReaderDepth", bc)
	}
}
