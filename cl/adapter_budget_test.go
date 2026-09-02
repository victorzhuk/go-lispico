// Resource-ownership contracts for the CL collection adapters. Every adapter
// charges the scalable work it does itself exactly once and classifies every
// successful return exactly once, on top of the shared kernels in
// internal/collections which already own the traversal, mapping and sort
// budgets — so a charge the kernel makes must never be repeated here.
//
// Two harnesses appear below and they observe different quantities. clbApply
// dispatches through core's apply site, where the unmarked-result fallback
// (ValueShallowBytes) lives, so its ledger answers "was this return classified
// at all". clbDirect calls GoFunc.Fn with no apply site above it, so its
// ledger is the adapter's own charge and nothing else. A borrowed return is
// only visible to the first; an adapter-owned container charge only to the
// second.
//
// Every callback used here classifies its own result as borrowed, so callback
// dispatches contribute reductions but no bytes and a byte assertion measures
// the adapter alone.
package cl_test

import (
	"context"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/cl"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/internal/collections"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
)

// clbUnitCount sits above the 128-unit batch interval of core's builtin work
// budget, so a per-element loop reaches a sync point mid-run instead of
// finishing inside one local batch.
const clbUnitCount = 200

// clbWideLen is large enough that a payload charge is unmistakable next to any
// container-sized quantity.
const clbWideLen = 4096

func clbAdapter(t *testing.T, name string) core.GoFunc {
	t.Helper()
	entry, ok := cl.Dialect().Vocab()[name]
	require.Truef(t, ok, "CL dialect must carry a %q vocab entry", name)
	fn, ok := entry.Adapter.(core.GoFunc)
	require.Truef(t, ok, "CL %q adapter must be a core.GoFunc, got %T", name, entry.Adapter)
	return fn
}

func clbStdlibEnv(t *testing.T) *core.Env {
	t.Helper()
	env := core.NewEnv(nil)
	require.NoError(t, stdlib.New().Init(env))
	return env
}

func clbBuiltin(t *testing.T, env *core.Env, name string) core.GoFunc {
	t.Helper()
	v, ok := env.Get(name)
	require.Truef(t, ok, "stdlib must register %q", name)
	fn, ok := v.(core.GoFunc)
	require.Truef(t, ok, "stdlib %q must be a core.GoFunc, got %T", name, v)
	return fn
}

// clbApply dispatches fn through the apply site, which is where the
// unmarked-result fallback charge lives.
func clbApply(t *testing.T, fn core.GoFunc, args ...core.Value) (core.EvalMeterSnapshot, error) {
	t.Helper()
	ctx := core.WithEvalResourceLimits(context.Background(), 1<<30, 1<<30)
	_, err := core.NewEvaluator().Apply(ctx, fn, args, core.NewEnv(nil))
	return core.EvalMeterFrom(ctx).Snapshot(), err
}

// clbDirect calls fn.Fn with no apply site above it, so the ledger it reports
// carries the adapter's own charges only.
func clbDirect(t *testing.T, fn core.GoFunc, args ...core.Value) (core.EvalMeterSnapshot, error) {
	t.Helper()
	ctx := core.WithEvalResourceLimits(context.Background(), 1<<30, 1<<30)
	_, err := fn.Fn(ctx, core.NewEvaluator(), args, core.NewEnv(nil))
	return core.EvalMeterFrom(ctx).Snapshot(), err
}

// clbDispatchCost is what core's apply site charges for one GoFunc dispatch
// that does no work of its own. A callback-driven adapter pays it once per
// callback, so a comparison against a path that compares in Go has to add it
// back — calibrated here rather than assumed, since the number belongs to the
// apply site and not to any adapter contract.
func clbDispatchCost(t *testing.T) int64 {
	t.Helper()
	snap, err := clbApply(t, core.GoFunc{
		Name: "clb-noop",
		Fn: func(ctx context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			if err := core.ChargeGoFuncResultBytes(ctx, 0); err != nil {
				return nil, err
			}
			return core.Nil{}, nil
		},
	})
	require.NoError(t, err)
	return snap.Reductions
}

// clbNaturalLess is the CL sort predicate for every row below. It decides
// order through the same comparator stdlib sort uses internally, so an
// identical input yields an identical comparison sequence on both sides and
// the two reduction totals differ only by work one of them owns.
func clbNaturalLess(calls *int) core.GoFunc {
	return core.GoFunc{
		Name: "clb-less",
		Fn: func(ctx context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
			*calls++
			if err := core.ChargeGoFuncResultBytes(ctx, 0); err != nil {
				return nil, err
			}
			cmp, err := collections.NaturalCmp(args[0], args[1])
			if err != nil {
				return nil, err
			}
			return core.Bool{V: cmp < 0}, nil
		},
	}
}

// clbIdentity returns its argument unchanged, so a :key projection built on it
// leaves the sort order — and therefore the comparison count — untouched.
func clbIdentity(calls *int) core.GoFunc {
	return core.GoFunc{
		Name: "clb-identity",
		Fn: func(ctx context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
			*calls++
			if err := core.ChargeGoFuncResultBytes(ctx, 0); err != nil {
				return nil, err
			}
			if len(args) == 0 {
				return core.Nil{}, nil
			}
			return args[0], nil
		},
	}
}

func clbIntValues(n int) []core.Value {
	vs := make([]core.Value, n)
	for i := range vs {
		vs[i] = core.Int{V: int64(n - i)}
	}
	return vs
}

func clbInts(n int) core.List { return core.NewList(clbIntValues(n)) }

// clbStrings builds n distinct strings whose order is decided by a 4-byte
// prefix, so widening the payload changes the bytes moved without changing the
// number of comparisons the sort performs.
func clbStrings(n, width int) core.List {
	vs := make([]core.Value, n)
	for i := range vs {
		vs[i] = core.String{V: clbPrefix(n-i) + strings.Repeat("x", width)}
	}
	return core.NewList(vs)
}

func clbPrefix(v int) string {
	digits := []byte{'0', '0', '0', '0'}
	for i := 3; i >= 0 && v > 0; i-- {
		digits[i] = byte('0' + v%10)
		v /= 10
	}
	return string(digits)
}

// TestCL_NthResultIsZeroByte: CL nth never allocates its result. A hit returns
// a member stored in the subject list and every out-of-range shape returns the
// shared core.Nil{} singleton, so both branches must classify the return as
// borrowed and neither may reach the apply-site fallback.
//
// The wide/narrow pair is the discriminator: an unclassified hit is charged
// ValueShallowBytes of whatever it happened to return, so widening the member
// 4096-fold moves the ledger.
func TestCL_NthResultIsZeroByte(t *testing.T) {
	nth := clbAdapter(t, "nth")
	wideList := listOf(core.String{V: strings.Repeat("x", clbWideLen)}, core.Int{V: 1})
	narrowList := listOf(core.String{V: "x"}, core.Int{V: 1})

	cases := []struct {
		name string
		args []core.Value
	}{
		{"hit/wide-member", []core.Value{core.Int{V: 0}, wideList}},
		{"hit/narrow-member", []core.Value{core.Int{V: 0}, narrowList}},
		{"out-of-range/past-end", []core.Value{core.Int{V: 9}, wideList}},
		{"out-of-range/nil-subject", []core.Value{core.Int{V: 0}, core.Nil{}}},
	}

	bytes := make(map[string]int64, len(cases))
	for _, tc := range cases {
		snap, err := clbApply(t, nth, tc.args...)
		require.NoErrorf(t, err, "%s must succeed", tc.name)
		bytes[tc.name] = snap.AllocationBytes
	}
	t.Logf("CL nth result bytes: %v", bytes)

	for _, tc := range cases {
		assert.Zerof(t, bytes[tc.name], "CL nth %s must charge zero result bytes: the hit borrows a stored member and the out-of-range branch returns the shared core.Nil{} singleton, so neither may reach the apply-site fallback (got %d bytes)", tc.name, bytes[tc.name])
	}
	assert.Equal(t, bytes["hit/narrow-member"], bytes["hit/wide-member"],
		"CL nth must charge the same for a 1-byte and a %d-byte member; a ledger that tracks the member's size means the hit branch was never classified as borrowed", clbWideLen)
}

// TestCL_AdaptersNoSecondWorkCharge: the CL adapters delegate their scalable
// work to shared kernels that already budget it, so an adapter consumes the
// same reductions as the stdlib path over the same kernel for an identical
// element count. A second charge in cl/cl.go shows up here as a delta.
//
// sort is the exception in shape only. CL sort dispatches a caller-supplied
// predicate where stdlib sort compares in Go, so its total carries one
// reduction per predicate dispatch on top; the ToSlice copy stdlib already
// bills is the remaining difference and must be zero.
func TestCL_AdaptersNoSecondWorkCharge(t *testing.T) {
	env := clbStdlibEnv(t)
	items := clbInts(clbUnitCount)

	t.Run("nth", func(t *testing.T) {
		clSnap, err := clbApply(t, clbAdapter(t, "nth"), core.Int{V: clbUnitCount - 1}, items)
		require.NoError(t, err)
		stdSnap, err := clbApply(t, clbBuiltin(t, env, "nth"), items, core.Int{V: clbUnitCount - 1})
		require.NoError(t, err)
		t.Logf("nth reductions: cl=%d stdlib=%d", clSnap.Reductions, stdSnap.Reductions)
		assert.Equal(t, stdSnap.Reductions, clSnap.Reductions,
			"CL nth must consume the reductions collections.IndexedAccess already charges for a %d-element traversal and no more", clbUnitCount)
	})

	t.Run("mapcar", func(t *testing.T) {
		var clCalls, stdCalls int
		clSnap, err := clbApply(t, clbAdapter(t, "mapcar"), clbIdentity(&clCalls), items)
		require.NoError(t, err)
		stdSnap, err := clbApply(t, clbBuiltin(t, env, "map"), clbIdentity(&stdCalls), items)
		require.NoError(t, err)
		t.Logf("mapcar reductions: cl=%d stdlib=%d (calls cl=%d stdlib=%d)", clSnap.Reductions, stdSnap.Reductions, clCalls, stdCalls)
		require.Equal(t, stdCalls, clCalls, "CL mapcar and stdlib map must dispatch the callback the same number of times")
		assert.Equal(t, stdSnap.Reductions, clSnap.Reductions,
			"CL mapcar must consume the reductions collections.MapSequences already charges for %d elements and no more", clbUnitCount)
	})

	t.Run("sort", func(t *testing.T) {
		var clCalls int
		clSnap, err := clbApply(t, clbAdapter(t, "sort"), items, clbNaturalLess(&clCalls))
		require.NoError(t, err)
		stdSnap, err := clbApply(t, clbBuiltin(t, env, "sort"), items)
		require.NoError(t, err)
		dispatch := clbDispatchCost(t)
		t.Logf("sort reductions: cl=%d stdlib=%d predicate-calls=%d dispatch-cost=%d", clSnap.Reductions, stdSnap.Reductions, clCalls, dispatch)
		assert.Equal(t, stdSnap.Reductions+int64(clCalls)*dispatch, clSnap.Reductions,
			"CL sort must bill its own ToSlice copy of %d elements exactly as stdlib sort bills its copy; the only admissible extra is the %d predicate dispatches", clbUnitCount, clCalls)
	})
}

// TestCL_SortResultChargesContainerOnly: both sort returns are fresh
// containers over elements borrowed from the subject, so each charges its
// shallow container size and nothing else.
//
// The adapter-owned rows use the direct harness on purpose: through the apply
// site the fallback charge and a correct adapter charge are the same number,
// so only a ledger taken with no apply site above it can tell whether the
// adapter classified its return at all.
func TestCL_SortResultChargesContainerOnly(t *testing.T) {
	sortFn := clbAdapter(t, "sort")

	t.Run("adapter-owned/list", func(t *testing.T) {
		var calls int
		snap, err := clbDirect(t, sortFn, clbInts(clbUnitCount), clbNaturalLess(&calls))
		require.NoError(t, err)
		t.Logf("direct list sort bytes: %d (predicate calls %d)", snap.AllocationBytes, calls)
		assert.Equal(t, core.ListShallowBytes(clbUnitCount), snap.AllocationBytes,
			"CL sort's List return must charge core.ListShallowBytes(%d) itself; an unclassified return leaves the adapter's own ledger at zero", clbUnitCount)
	})

	t.Run("adapter-owned/vector", func(t *testing.T) {
		var calls int
		snap, err := clbDirect(t, sortFn, core.NewVector(clbIntValues(clbUnitCount)), clbNaturalLess(&calls))
		require.NoError(t, err)
		t.Logf("direct vector sort bytes: %d (predicate calls %d)", snap.AllocationBytes, calls)
		assert.Equal(t, core.VectorShallowBytes(clbUnitCount), snap.AllocationBytes,
			"CL sort's Vector return must charge core.VectorShallowBytes(%d) itself; an unclassified return leaves the adapter's own ledger at zero", clbUnitCount)
	})

	t.Run("payload-invariant", func(t *testing.T) {
		var wideCalls, narrowCalls int
		wide, err := clbApply(t, sortFn, clbStrings(clbUnitCount, clbWideLen), clbNaturalLess(&wideCalls))
		require.NoError(t, err)
		narrow, err := clbApply(t, sortFn, clbStrings(clbUnitCount, 1), clbNaturalLess(&narrowCalls))
		require.NoError(t, err)
		t.Logf("sort payload bytes: wide=%d narrow=%d (calls wide=%d narrow=%d)", wide.AllocationBytes, narrow.AllocationBytes, wideCalls, narrowCalls)
		require.Equal(t, narrowCalls, wideCalls, "the two payload widths must produce the same comparison sequence")
		assert.Equal(t, narrow.AllocationBytes, wide.AllocationBytes,
			"sorting %d-byte strings and 1-byte strings must move the ledger identically: sort borrows every element and allocates only the container", clbWideLen)
	})
}

// TestCL_SortBudgetsCopyAndKeywordScan: the two loops CL sort runs outside the
// kernel — the ToSlice copy and the :key keyword scan — are the adapter's to
// bill, and the interruption rows prove the kernel's terminal outcomes survive
// the adapter unchanged.
func TestCL_SortBudgetsCopyAndKeywordScan(t *testing.T) {
	sortFn := clbAdapter(t, "sort")
	items := clbInts(clbUnitCount)

	t.Run("keyword-scan-charges-one-step-per-pair", func(t *testing.T) {
		var bareCalls, keyedCalls, keyCalls int
		bare, err := clbApply(t, sortFn, items, clbNaturalLess(&bareCalls))
		require.NoError(t, err)
		keyed, err := clbApply(t, sortFn, items, clbNaturalLess(&keyedCalls), core.Keyword{V: "key"}, clbIdentity(&keyCalls))
		require.NoError(t, err)
		t.Logf("sort reductions: bare=%d keyed=%d (compare bare=%d keyed=%d, key calls=%d)",
			bare.Reductions, keyed.Reductions, bareCalls, keyedCalls, keyCalls)

		require.Equal(t, bareCalls, keyedCalls, "an identity :key must not change the comparison sequence")
		require.Equal(t, clbUnitCount, keyCalls, "the key projection must run once per element")

		// collections.StableSort charges one Step per element for the key
		// projection and the adapter dispatches the key function once per
		// element; the single :key pair the scan walked is the only remaining
		// difference between the two runs.
		want := bare.Reductions + int64(clbUnitCount) + int64(keyCalls)*clbDispatchCost(t) + 1
		assert.Equal(t, want, keyed.Reductions,
			"CL sort's keyword scan must charge one Step per keyword pair, so the :key run must cost exactly one reduction more than the kernel's own key work")
	})

	t.Run("terminal-under-low-reductions", func(t *testing.T) {
		var calls int
		ctx := core.WithEvalResourceLimits(context.Background(), 100, 1<<30)
		_, err := sortFn.Fn(ctx, core.NewEvaluator(), []core.Value{items, clbNaturalLess(&calls)}, core.NewEnv(nil))
		require.Error(t, err, "a %d-element sort under a 100-reduction ceiling must not run to completion", clbUnitCount)
		var le *core.LispicoError
		require.ErrorAs(t, err, &le, "the rejection must be a typed *core.LispicoError, got %v", err)
		assert.Equal(t, core.CodeResourceLimit, le.Code, "a ceiling breach must be a resource-limit error")
		assert.Truef(t, core.IsTerminalEvalError(err),
			"a %d-element CL sort under a 100-reduction ceiling must fail terminally, got %v", clbUnitCount, err)
	})

	t.Run("expired-deadline", func(t *testing.T) {
		var calls int
		ctx := core.WithEvalResourceLimits(context.Background(), 1<<30, 1<<30)
		ctx = core.WithEvalDeadline(ctx, time.Now().Add(-time.Millisecond))
		_, err := sortFn.Fn(ctx, core.NewEvaluator(), []core.Value{items, clbNaturalLess(&calls)}, core.NewEnv(nil))
		require.ErrorIsf(t, err, context.DeadlineExceeded,
			"a %d-element CL sort past the engine deadline on a live parent must surface DeadlineExceeded, got %v", clbUnitCount, err)
	})

	t.Run("cancelled-caller", func(t *testing.T) {
		var calls int
		parent, cancel := context.WithCancel(context.Background())
		ctx := core.WithEvalResourceLimits(parent, 1<<30, 1<<30)
		cancel()
		_, err := sortFn.Fn(ctx, core.NewEvaluator(), []core.Value{items, clbNaturalLess(&calls)}, core.NewEnv(nil))
		require.ErrorIsf(t, err, context.Canceled,
			"a %d-element CL sort under a cancelled caller must surface Canceled, got %v", clbUnitCount, err)
	})
}

// TestCL_UnknownKeywordMessageBounded: the unknown-keyword rejection renders
// the offending keyword under a precision, so the message a caller can force
// is bounded by the format's own limit instead of by the keyword's length.
// fmt precision counts runes, so the ceiling is the literal plus 200 runes at
// utf8.UTFMax bytes each.
func TestCL_UnknownKeywordMessageBounded(t *testing.T) {
	const prefix = "sort: unknown keyword "
	sortFn := clbAdapter(t, "sort")
	items := intList(3, 1, 2)

	t.Run("short-keyword-is-unchanged", func(t *testing.T) {
		var calls int
		_, err := clbDirect(t, sortFn, items, clbNaturalLess(&calls), core.Keyword{V: "bogus"}, core.Nil{})
		require.Error(t, err, "an unknown keyword must be rejected")
		var le *core.LispicoError
		require.ErrorAs(t, err, &le, "the rejection must be a typed *core.LispicoError, got %v", err)
		assert.Equal(t, "EvalError", le.Code, "an unknown keyword is a domain rejection")
		assert.Equal(t, prefix+":bogus", le.Message,
			"a keyword of 200 runes or fewer must render whole: bounding the message must not truncate ordinary diagnostics")
		assert.Zero(t, calls, "the grammar must be rejected before the predicate runs")
	})

	t.Run("long-keyword-is-bounded", func(t *testing.T) {
		var calls int
		long := strings.Repeat("×", 10000)
		_, err := clbDirect(t, sortFn, items, clbNaturalLess(&calls), core.Keyword{V: long}, core.Nil{})
		require.Error(t, err, "an unknown keyword must be rejected")
		var le *core.LispicoError
		require.ErrorAs(t, err, &le, "the rejection must be a typed *core.LispicoError, got %v", err)
		t.Logf("long-keyword message: %d bytes for a %d-rune keyword", len(le.Message), utf8.RuneCountInString(long))

		bound := len(prefix) + 200*utf8.UTFMax
		assert.LessOrEqualf(t, len(le.Message), bound,
			"a %d-rune keyword must not produce a message that grows with it: got %d bytes, ceiling %d", utf8.RuneCountInString(long), len(le.Message), bound)
		assert.True(t, strings.HasPrefix(le.Message, prefix), "the bounded message must keep its diagnostic prefix, got %q", le.Message)
		assert.Equal(t, "EvalError", le.Code, "bounding the message must not change its classification")
		assert.False(t, core.IsTerminalEvalError(err), "an unknown-keyword rejection must stay catchable")
	})
}
