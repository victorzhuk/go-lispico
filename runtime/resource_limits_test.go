package runtime

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
)

func newLimitsEngine(t testing.TB, bytecode bool, limits ResourceLimits) Engine {
	t.Helper()
	e := newMeteringStdlibEngine(t, bytecode, limits)
	bindBuiltin(t, e, "+")
	return e
}

func newMeteringStdlibEngine(t testing.TB, bytecode bool, limits ResourceLimits) Engine {
	t.Helper()
	opts := []EngineOption{WithResourceLimits(limits), WithDialect(clojure.Dialect())}
	if bytecode {
		opts = append(opts, WithBytecode())
	} else {
		opts = append(opts, WithTreeWalker())
	}
	e, err := New(nil, opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })
	require.NoError(t, e.Use(stdlib.New()))
	return e
}

func evalLimits(t *testing.T, bytecode bool, limits ResourceLimits, src string) (core.Value, error) {
	t.Helper()
	e := newLimitsEngine(t, bytecode, limits)
	return e.Eval(context.Background(), "test", src)
}

func isResourceLimit(t *testing.T, err error) bool {
	t.Helper()
	var lerr *core.LispicoError
	return errors.As(err, &lerr) && lerr.Code == core.CodeResourceLimit
}

func resourceLimitErrorCode(t *testing.T, err error) string {
	t.Helper()
	require.Error(t, err)
	var lerr *core.LispicoError
	require.ErrorAs(t, err, &lerr)
	return lerr.Code
}

func meteringFieldsAvailable() bool {
	typ := reflect.TypeOf(ResourceLimits{})
	_, hasReductions := typ.FieldByName("MaxReductions")
	_, hasAllocations := typ.FieldByName("MaxAllocationBytes")
	return hasReductions && hasAllocations
}

func skipUntilMeteringFields(t *testing.T) {
	t.Helper()
	if !meteringFieldsAvailable() {
		t.Skip("red pending: ResourceLimits.MaxReductions and ResourceLimits.MaxAllocationBytes are not implemented")
	}
}

func requireMeteringField(t *testing.T, limits *ResourceLimits, name string, value int) {
	t.Helper()
	field := reflect.ValueOf(limits).Elem().FieldByName(name)
	if !field.IsValid() {
		t.Fatalf("ResourceLimits.%s missing; red until evaluation metering is implemented", name)
	}
	if field.Kind() != reflect.Int {
		t.Fatalf("ResourceLimits.%s kind = %s, want int", name, field.Kind())
	}
	field.SetInt(int64(value))
}

func meteringLimits(t *testing.T, maxReductions, maxAllocationBytes int) ResourceLimits {
	t.Helper()
	limits := ResourceLimits{
		MaxReaderDepth:     1 << 20,
		MaxStructuralDepth: 1 << 20,
		MaxCollectionLen:   1 << 30,
		MaxCacheEntries:    4096,
	}
	requireMeteringField(t, &limits, "MaxReductions", maxReductions)
	requireMeteringField(t, &limits, "MaxAllocationBytes", maxAllocationBytes)
	return limits
}

func retainedLimitBytes(name string, val core.Value) int64 {
	return core.MeterEnvMapEntryBytes + core.MeterEnvCellBytes + core.StringShallowBytes(len(name)) + core.ValueShallowBytes(val)
}

func evalModeName(bytecode bool) string {
	if bytecode {
		return "bytecode"
	}
	return "tree"
}

func vectorLiteral(item string, n int) string {
	var b strings.Builder
	b.Grow(n*(len(item)+1) + 2)
	b.WriteByte('[')
	for i := range n {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(item)
	}
	b.WriteByte(']')
	return b.String()
}

func allocationLoopSource(iterations, width int) string {
	return "(loop [i 0 last []] (if (= i " + strconv.Itoa(iterations) + ") last (recur (+ i 1) " + vectorLiteral("i", width) + ")))"
}

func reductionLoopSource(iterations int) string {
	return "(loop [i 0] (if (= i " + strconv.Itoa(iterations) + ") i (recur (+ i 1))))"
}

func macroAmplifiedSource(depth int) string {
	return "(defmacro amp [n] (if (= n 0) 1 `(amp ~(- n 1))))\n(amp " + strconv.Itoa(depth) + ")"
}

func deepCompilerEmitSource(width int) string {
	return vectorLiteral("(+ 1 2)", width)
}

func strConcatLoopSource(iterations, chunkLen int) string {
	return `(loop [i 0 s ""] (if (= i ` + strconv.Itoa(iterations) + `) s (recur (+ i 1) (str s "` + strings.Repeat("x", chunkLen) + `"))))`
}

func flatReaderLiteral(width int) string {
	return vectorLiteral("1", width)
}

func trySource(body string) string {
	return "(try " + body + " (catch e :caught))"
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

func TestLimits_RetainedDefaultsNormalize(t *testing.T) {
	limits := resolveLimits(ResourceLimits{MaxRetainedBytesPerEnv: -1, MaxRetainedSlotsPerEnv: -1, MaxCacheBytes: -1, MaxCacheNodes: -1})
	assert.Equal(t, 32*1024*1024, limits.MaxRetainedBytesPerEnv)
	assert.Equal(t, 100_000, limits.MaxRetainedSlotsPerEnv)
	assert.Equal(t, 64*1024*1024, limits.MaxCacheBytes)
	assert.Equal(t, 1_000_000, limits.MaxCacheNodes)
}

func TestLimits_RetainedSlotCeilingFailsClosed(t *testing.T) {
	e, err := New(nil, WithDialect(clojure.Dialect()), WithResourceLimits(ResourceLimits{MaxRetainedSlotsPerEnv: 1}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })

	require.NoError(t, e.Bind("a", core.Int{V: 1}))
	err = e.Bind("b", core.Int{V: 2})
	assert.True(t, isResourceLimit(t, err), "expected ResourceLimitError, got %v", err)
	if _, ok := e.RootEnv().Get("b"); ok {
		t.Fatal("b should not be bound after failed retained slot charge")
	}
	if v, ok := e.RootEnv().Get("a"); !ok || !v.Equals(core.Int{V: 1}) {
		t.Fatalf("a = %v, %v; want 1, true", v, ok)
	}
}

func TestLimits_Lisp2BindRollbackOnFuncCellLimit(t *testing.T) {
	e, err := New(nil, WithDialect(core.FullDialect().Lisp2()), WithResourceLimits(ResourceLimits{MaxRetainedSlotsPerEnv: 1}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })

	err = e.Bind("a", core.Int{V: 1})
	assert.True(t, isResourceLimit(t, err), "expected ResourceLimitError, got %v", err)
	if _, ok := e.RootEnv().Get("a"); ok {
		t.Fatal("a value cell should be unbound after dual-cell bind failure")
	}
	if _, ok := e.RootEnv().GetFunc("a"); ok {
		t.Fatal("a function cell should be unbound after dual-cell bind failure")
	}
	bytes, slots := e.RootEnv().RetainedUsage()
	assert.Equal(t, int64(0), bytes)
	assert.Equal(t, int64(0), slots)
}

func TestLimits_Lisp2LoadScopeBindingRollbackOnFuncCellLimit(t *testing.T) {
	e, err := New(nil, WithDialect(core.FullDialect().Lisp2()), WithResourceLimits(ResourceLimits{MaxRetainedSlotsPerEnv: 1}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })

	_, scope, err := e.LoadScope(t.Context(), "x", map[string]core.Value{"x": core.Int{V: 1}})
	assert.True(t, isResourceLimit(t, err), "expected ResourceLimitError, got %v", err)
	require.NotNil(t, scope)
	if _, ok := scope.Get("x"); ok {
		t.Fatal("x value cell should be unbound after function-cell bind failure")
	}
	if _, ok := scope.GetFunc("x"); ok {
		t.Fatal("x function cell should be unbound after function-cell bind failure")
	}
	bytes, slots := scope.RetainedUsage()
	assert.Equal(t, int64(0), bytes)
	assert.Equal(t, int64(0), slots)
}

func TestLimits_Lisp2LazyMaterializeRollbackOnFuncCellLimit(t *testing.T) {
	e, err := New(nil, WithBytecode(), WithDialect(core.FullDialect().Lisp2()), WithResourceLimits(ResourceLimits{MaxRetainedSlotsPerEnv: 1}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })
	require.NoError(t, e.Use(stdlib.New()))

	_, err = e.Eval(t.Context(), "first-touch", "(+ 1 2)")
	require.Error(t, err)
	if _, ok := e.RootEnv().Get("+"); ok {
		t.Fatal("+ value cell should be unbound after lazy function-cell bind failure")
	}
	if _, ok := e.RootEnv().GetFunc("+"); ok {
		t.Fatal("+ function cell should be unbound after lazy function-cell bind failure")
	}
}

func TestLimits_RetainedByteCeilingFailsClosed(t *testing.T) {
	e, err := New(nil, WithDialect(clojure.Dialect()), WithResourceLimits(ResourceLimits{MaxRetainedBytesPerEnv: 1}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })

	err = e.Bind("a", core.Int{V: 1})
	assert.True(t, isResourceLimit(t, err), "expected ResourceLimitError, got %v", err)
	if _, ok := e.RootEnv().Get("a"); ok {
		t.Fatal("a should not be bound after failed retained byte charge")
	}
	bytes, slots := e.RootEnv().RetainedUsage()
	assert.Equal(t, int64(0), bytes)
	assert.Equal(t, int64(0), slots)
}

func TestLimits_RetainedRebindAndDeleteDoNotCharge(t *testing.T) {
	initial := core.Int{V: 1}
	e, err := New(nil, WithDialect(clojure.Dialect()), WithResourceLimits(ResourceLimits{MaxRetainedSlotsPerEnv: 1}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })

	require.NoError(t, e.Bind("x", initial))
	wantBytes, wantSlots := e.RootEnv().RetainedUsage()
	require.NoError(t, e.Bind("x", core.String{V: "larger"}))
	gotBytes, gotSlots := e.RootEnv().RetainedUsage()
	assert.Equal(t, wantBytes, gotBytes)
	assert.Equal(t, wantSlots, gotSlots)

	e.RootEnv().Delete("x")
	gotBytes, gotSlots = e.RootEnv().RetainedUsage()
	assert.Equal(t, wantBytes, gotBytes)
	assert.Equal(t, wantSlots, gotSlots)
	assert.True(t, isResourceLimit(t, e.Bind("y", core.Int{V: 2})))
}

func TestLimits_RetainedUsageExact(t *testing.T) {
	val := core.String{V: "value"}
	fn := core.GoFunc{Name: "fn"}
	e, err := New(nil, WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })

	require.NoError(t, e.Bind("value", val))
	require.NoError(t, e.Bind("fn", fn))

	wantBytes := retainedLimitBytes("value", val) + retainedLimitBytes("fn", fn)
	gotBytes, gotSlots := e.RootEnv().RetainedUsage()
	assert.Equal(t, wantBytes, gotBytes)
	assert.Equal(t, int64(2), gotSlots)
}

func TestLimits_DirectEvaluatorEvalFlushesResidualReductions(t *testing.T) {
	t.Parallel()

	ctx := core.WithEvalResourceLimits(t.Context(), 1, 1<<20)
	ev := core.NewEvaluator()
	_, err := ev.Eval(ctx, core.Vector{Items: []core.Value{core.Int{V: 1}, core.Int{V: 2}}}, core.NewEnv(nil))

	assert.True(t, isResourceLimit(t, err), "expected residual reductions to flush, got %v", err)
}

func TestLimits_EngineResourceLimitsOverrideCallerEvalState(t *testing.T) {
	skipUntilMeteringFields(t)

	limits := meteringLimits(t, 64, 1<<30)
	src := reductionLoopSource(512)

	for _, bytecode := range []bool{false, true} {
		t.Run(evalModeName(bytecode), func(t *testing.T) {
			t.Run("Eval", func(t *testing.T) {
				ctx := core.EnsureEvalState(t.Context())
				_, err := evalLimits(t, bytecode, limits, src)
				require.True(t, isResourceLimit(t, err), "control must exceed engine limit")

				eng := newLimitsEngine(t, bytecode, limits)
				_, err = eng.Eval(ctx, "prepopulated", src)
				assert.True(t, isResourceLimit(t, err), "engine must override caller evalState limits, got %v", err)
			})

			t.Run("EvalWithBindings", func(t *testing.T) {
				ctx := core.DetachEvalState(t.Context())
				eng := newLimitsEngine(t, bytecode, limits)
				_, err := eng.EvalWithBindings(ctx, src, map[string]core.Value{})
				assert.True(t, isResourceLimit(t, err), "engine must override detached evalState limits, got %v", err)
			})

			t.Run("Call", func(t *testing.T) {
				ctx := core.EnsureEvalState(t.Context())
				eng := newLimitsEngine(t, bytecode, limits)
				_, err := eng.Eval(t.Context(), "define-spin", "(defn spin [] "+src+")")
				require.NoError(t, err)

				_, err = eng.Call(ctx, "spin")
				assert.True(t, isResourceLimit(t, err), "engine call must override caller evalState limits, got %v", err)
			})
		})
	}
}

func TestLimits_MeteringFieldsExist(t *testing.T) {
	typ := reflect.TypeOf(ResourceLimits{})
	for _, name := range []string{"MaxReductions", "MaxAllocationBytes"} {
		t.Run(name, func(t *testing.T) {
			field, ok := typ.FieldByName(name)
			if !ok {
				t.Fatalf("red: ResourceLimits.%s missing before evaluation metering implementation", name)
			}
			assert.Equal(t, reflect.Int, field.Type.Kind(), "ResourceLimits.%s type", name)
		})
	}
}

func TestLimits_MeteringAdversariesTripTightLimits(t *testing.T) {
	skipUntilMeteringFields(t)

	tests := []struct {
		name   string
		src    string
		limits ResourceLimits
		modes  []bool
	}{
		{
			name:   "tight allocation loop",
			src:    allocationLoopSource(256, 64),
			limits: meteringLimits(t, 1_000_000, 32<<10),
			modes:  []bool{false, true},
		},
		{
			name:   "macro-amplified recursion",
			src:    macroAmplifiedSource(256),
			limits: meteringLimits(t, 64, 1<<30),
			modes:  []bool{false, true},
		},
		{
			name:   "deep compiler emit",
			src:    deepCompilerEmitSource(512),
			limits: meteringLimits(t, 128, 1<<30),
			modes:  []bool{true},
		},
		{
			name:   "GoFunc string concatenation loop",
			src:    strConcatLoopSource(192, 128),
			limits: meteringLimits(t, 1_000_000, 32<<10),
			modes:  []bool{false, true},
		},
		{
			name:   "flat huge reader literal",
			src:    flatReaderLiteral(4096),
			limits: meteringLimits(t, 1_000_000, 4<<10),
			modes:  []bool{false, true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, bytecode := range tt.modes {
				t.Run(evalModeName(bytecode), func(t *testing.T) {
					_, err := evalLimits(t, bytecode, tt.limits, tt.src)
					assert.True(t, isResourceLimit(t, err), "expected ResourceLimitError, got %v", err)
				})
			}
		})
	}
}

func TestMetering_AssocChargesDeepBytes(t *testing.T) {
	skipUntilMeteringFields(t)

	const width = 1024
	payload := core.Vector{Items: make([]core.Value, width)}
	for i := range payload.Items {
		payload.Items[i] = core.Int{V: int64(i)}
	}

	src := "(assoc (assoc {} :a (payload)) :b (payload))"
	newEngine := func(t testing.TB, bytecode bool, limits ResourceLimits) Engine {
		t.Helper()
		eng := newMeteringStdlibEngine(t, bytecode, limits)
		require.NoError(t, eng.Bind("payload", core.GoFunc{
			Name: "payload",
			Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
				return payload, nil
			},
		}))
		return eng
	}

	for _, bytecode := range []bool{false, true} {
		t.Run(evalModeName(bytecode)+"/tight", func(t *testing.T) {
			eng := newEngine(t, bytecode, meteringLimits(t, 1_000_000, 64<<10))
			_, err := eng.Eval(t.Context(), "assoc-deep-tight", src)
			assert.True(t, isResourceLimit(t, err), "expected ResourceLimitError, got %v", err)
		})
	}

	want := core.NewHashMap()
	require.NoError(t, want.Set(core.Keyword{V: "a"}, payload))
	require.NoError(t, want.Set(core.Keyword{V: "b"}, payload))
	deep := core.ValueDeepBytes(want)

	for _, bytecode := range []bool{false, true} {
		t.Run(evalModeName(bytecode)+"/under-budget", func(t *testing.T) {
			const maxAlloc = 256 << 10
			ctx := core.WithEvalResourceLimits(t.Context(), 1_000_000, maxAlloc)
			eng := newEngine(t, bytecode, meteringLimits(t, 1_000_000, maxAlloc))
			got, err := eng.Eval(ctx, "assoc-deep-under-budget", src)
			require.NoError(t, err)
			assert.True(t, want.Equals(got), "got %v, want %v", got, want)

			used := core.EvalMeterFrom(ctx).Snapshot().AllocationBytes
			assert.GreaterOrEqual(t, used, deep, "assoc must charge deep result bytes")
			assert.Greater(t, used, core.HashMapShallowBytes(2), "assoc charge must not stop at shallow map entries")
		})
	}
}

func TestMetering_FusedArithmeticChargesLedger(t *testing.T) {
	skipUntilMeteringFields(t)

	const iterations = 1024
	src := "(loop [i 0 x 1024] (if (= i " + strconv.Itoa(iterations) + ") x (recur (+ i 1) (+ x 1))))"

	t.Run("tight", func(t *testing.T) {
		eng := newMeteringStdlibEngine(t, true, meteringLimits(t, 1_000_000, 24<<10))
		_, err := eng.Eval(t.Context(), "fused-arith-tight", src)
		assert.True(t, isResourceLimit(t, err), "expected ResourceLimitError, got %v", err)
	})

	t.Run("under-budget", func(t *testing.T) {
		const maxAlloc = 128 << 10
		ctx := core.WithEvalResourceLimits(t.Context(), 1_000_000, maxAlloc)
		eng := newMeteringStdlibEngine(t, true, meteringLimits(t, 1_000_000, maxAlloc))
		got, err := eng.Eval(ctx, "fused-arith-under-budget", src)
		require.NoError(t, err)
		assert.True(t, core.Int{V: 1024 + iterations}.Equals(got), "got %v", got)

		used := core.EvalMeterFrom(ctx).Snapshot().AllocationBytes
		assert.GreaterOrEqual(t, used, int64(iterations)*core.MeterScalarBytes, "fused native ops must charge scalar results")
	})
}

func TestMetering_AssocAndFusedArith_BaselineParity(t *testing.T) {
	skipUntilMeteringFields(t)

	assocSrc := "(assoc {:a 1} :b [2 3])"
	var assocWant core.Value
	for _, bytecode := range []bool{false, true} {
		t.Run("assoc/"+evalModeName(bytecode), func(t *testing.T) {
			got, err := evalLimits(t, bytecode, meteringLimits(t, 1_000_000, 1<<20), assocSrc)
			require.NoError(t, err)
			if assocWant == nil {
				assocWant = got
			}
			assert.True(t, assocWant.Equals(got), "got %v, want %v", got, assocWant)
		})
	}

	fusedSrc := "(loop [i 0 x 1024] (if (= i 5) x (recur (+ i 1) (+ x 1))))"
	got, err := newMeteringStdlibEngine(t, true, meteringLimits(t, 1_000_000, 1<<20)).Eval(t.Context(), "fused-arith-baseline", fusedSrc)
	require.NoError(t, err)
	assert.True(t, core.Int{V: 1029}.Equals(got), "got %v", got)
}

func TestLimits_MeteringAdversariesErrorClassParity(t *testing.T) {
	skipUntilMeteringFields(t)

	tests := []struct {
		name   string
		src    string
		limits ResourceLimits
	}{
		{
			name:   "reduction loop",
			src:    reductionLoopSource(8_192),
			limits: meteringLimits(t, 256, 1<<30),
		},
		{
			name:   "allocation loop",
			src:    allocationLoopSource(2_048, 64),
			limits: meteringLimits(t, 1_000_000, 16<<10),
		},
		{
			name:   "macro recursion",
			src:    macroAmplifiedSource(256),
			limits: meteringLimits(t, 32, 1<<30),
		},
		{
			name:   "GoFunc string concatenation loop",
			src:    strConcatLoopSource(256, 128),
			limits: meteringLimits(t, 1_000_000, 16<<10),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			treeErr := runEvalLimits(t, false, tt.limits, tt.src)
			vmErr := runEvalLimits(t, true, tt.limits, tt.src)
			treeCode := resourceLimitErrorCode(t, treeErr)
			vmCode := resourceLimitErrorCode(t, vmErr)

			assert.Equal(t, core.CodeResourceLimit, treeCode, "tree-walker must return ResourceLimitError: %v", treeCode)
			assert.Equal(t, core.CodeResourceLimit, vmCode, "vm must return ResourceLimitError: %v", vmCode)
			assert.Equal(t, treeCode, vmCode, "tree/vm terminal error class mismatch")
			assert.True(t, core.IsTerminalEvalError(treeErr))
			assert.True(t, core.IsTerminalEvalError(vmErr))
		})
	}
}

func runEvalLimits(t *testing.T, bytecode bool, limits ResourceLimits, src string) error {
	t.Helper()
	_, err := evalLimits(t, bytecode, limits, src)
	return err
}

func TestLimits_MeteringAdversariesPassUnderDefaults(t *testing.T) {
	limits := ResourceLimits{MaxReaderDepth: 1 << 20, MaxStructuralDepth: 1 << 20, MaxCollectionLen: 1 << 30, MaxCacheEntries: 4096}
	tests := []struct {
		name  string
		src   string
		modes []bool
	}{
		{name: "tight allocation loop", src: allocationLoopSource(64, 16), modes: []bool{false, true}},
		{name: "macro-amplified recursion", src: macroAmplifiedSource(32), modes: []bool{false, true}},
		{name: "deep compiler emit", src: deepCompilerEmitSource(128), modes: []bool{true}},
		{name: "GoFunc string concatenation loop", src: strConcatLoopSource(32, 16), modes: []bool{false, true}},
		{name: "flat huge reader literal", src: flatReaderLiteral(512), modes: []bool{false, true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, bytecode := range tt.modes {
				t.Run(evalModeName(bytecode), func(t *testing.T) {
					got, err := evalLimits(t, bytecode, limits, tt.src)
					require.NoError(t, err)
					assert.NotNil(t, got)
				})
			}
		})
	}
}

func TestLimits_FlatHugeReaderLiteralChargedBeforeFirstEval(t *testing.T) {
	skipUntilMeteringFields(t)

	for _, bytecode := range []bool{false, true} {
		t.Run(evalModeName(bytecode), func(t *testing.T) {
			var calls atomic.Int64
			eng := newLimitsEngine(t, bytecode, meteringLimits(t, 1_000_000, 4<<10))
			require.NoError(t, eng.Bind("mark", core.GoFunc{
				Name: "mark",
				Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
					calls.Add(1)
					return core.Int{V: 1}, nil
				},
			}))

			_, err := eng.Eval(t.Context(), "reader", "(mark)\n"+flatReaderLiteral(4096))
			assert.True(t, isResourceLimit(t, err), "expected ResourceLimitError, got %v", err)
			assert.Equal(t, int64(0), calls.Load(), "reader allocation charge must run before the first form")
		})
	}
}

func TestLimits_MeteringTryCatchNotCatchable(t *testing.T) {
	skipUntilMeteringFields(t)

	tests := []struct {
		name   string
		src    string
		limits ResourceLimits
	}{
		{
			name:   "reduction ceiling",
			src:    trySource(reductionLoopSource(5000)),
			limits: meteringLimits(t, 512, 1<<30),
		},
		{
			name:   "allocation ceiling",
			src:    trySource(allocationLoopSource(512, 64)),
			limits: meteringLimits(t, 1_000_000, 32<<10),
		},
		{
			name:   "GoFunc result allocation ceiling",
			src:    trySource(strConcatLoopSource(192, 128)),
			limits: meteringLimits(t, 1_000_000, 32<<10),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, bytecode := range []bool{false, true} {
				t.Run(evalModeName(bytecode), func(t *testing.T) {
					got, err := evalLimits(t, bytecode, tt.limits, tt.src)
					assert.True(t, isResourceLimit(t, err), "got result=%v err=%v", got, err)
				})
			}
		})
	}
}

func TestLimits_MeteringCounterIsolationRace(t *testing.T) {
	if !raceEnabled {
		t.Skip("race-only: run go test -race")
	}
	skipUntilMeteringFields(t)

	tests := []struct {
		name   string
		src    string
		limits ResourceLimits
	}{
		{
			name:   "reduction",
			src:    reductionLoopSource(256),
			limits: meteringLimits(t, 5000, 1<<30),
		},
		{
			name:   "allocation",
			src:    allocationLoopSource(64, 32),
			limits: meteringLimits(t, 1_000_000, 128<<10),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, bytecode := range []bool{false, true} {
				t.Run(evalModeName(bytecode), func(t *testing.T) {
					runConcurrentMeteredEval(t, bytecode, tt.limits, tt.src)
				})
			}
		})
	}
}

func runConcurrentMeteredEval(t *testing.T, bytecode bool, limits ResourceLimits, src string) {
	t.Helper()
	eng := newLimitsEngine(t, bytecode, limits)
	ctx := t.Context()

	const workers = 8
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 3 {
				if _, err := eng.Eval(ctx, "race", src); err != nil {
					errs <- err
					return
				}
			}
			errs <- nil
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}
}
