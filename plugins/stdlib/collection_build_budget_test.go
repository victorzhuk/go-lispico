package stdlib

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/internal/inventory"
)

// cbUnitCount sits above the 128-unit batch interval of core's builtin work
// budget, so a construction or traversal loop that steps once per element
// reaches a sync point mid-build instead of finishing inside the local batch.
const cbUnitCount = 200

// cbWideLen is large enough that a payload charge is unmistakable next to any
// container-sized quantity.
const cbWideLen = 4096

func cbInts(n int) []core.Value {
	vs := make([]core.Value, n)
	for i := range vs {
		vs[i] = core.Int{V: int64(n - i)}
	}
	return vs
}

func cbMap(t *testing.T, n int) *core.HashMap {
	t.Helper()
	m := core.NewHashMap()
	for i := range n {
		require.NoError(t, m.Set(core.Int{V: int64(i)}, core.Int{V: int64(i)}))
	}
	return m
}

// cbBuilders is every collection construction or traversal builtin in the
// seam whose cost grows with its input. concat gets Vector arguments on
// purpose: a trailing List takes the shared-tail path, which already charges
// per Cons, so only the flatten path is exercised here.
//
// The sort arms of the three interruption tests, and range's cancellation arm,
// are green before the migration: StableSort already owns and charges the sort
// kernel, and range already checks its caller's ctx per iteration. They stay
// here as non-regression guards. sort's ownership change is asserted through
// the inventory instead, in TestSort_ResultBranchIsFreshContainer and
// TestSort_ToSliceCopyIsBudgeted.
func cbBuilders(t *testing.T) []struct {
	name string
	args []core.Value
} {
	t.Helper()
	items := cbInts(cbUnitCount)
	pairs := make([]core.Value, 0, 2*cbUnitCount)
	for i := range cbUnitCount {
		pairs = append(pairs, core.Int{V: int64(i)}, core.Int{V: int64(i)})
	}
	return []struct {
		name string
		args []core.Value
	}{
		{"list", items},
		{"vector", items},
		{"hash-map", pairs},
		{"concat", []core.Value{core.NewVector(items), core.NewVector(items)}},
		{"reverse", []core.Value{core.NewList(items)}},
		{"range", []core.Value{core.Int{V: cbUnitCount}}},
		{"merge", []core.Value{cbMap(t, cbUnitCount), cbMap(t, cbUnitCount)}},
		{"sort", []core.Value{core.NewList(items)}},
	}
}

// cbApplyCharge dispatches fn through the apply site, which is where the
// unmarked-result fallback charge lives, and reports both the ledger total and
// whether the budget held.
func cbApplyCharge(t *testing.T, env *core.Env, fn core.Value, budgetBytes int, args ...core.Value) (int64, error) {
	t.Helper()
	ctx := core.WithEvalResourceLimits(context.Background(), 1<<20, budgetBytes)
	_, err := core.NewEvaluator().Apply(ctx, fn, args, env)
	return core.EvalMeterFrom(ctx).Snapshot().AllocationBytes, err
}

// cbStrings builds n distinct strings whose ordering is decided by a 4-byte
// prefix, so widening the payload changes the bytes moved but not the number
// of comparisons a sort performs.
func cbStrings(n, width int) core.Value {
	vs := make([]core.Value, n)
	for i := range vs {
		vs[i] = core.String{V: fmt.Sprintf("%04d", n-i) + strings.Repeat("x", width)}
	}
	return core.NewList(vs)
}

// TestCollectionBuild_TerminalUnderLowReductions: every collection builtin
// whose cost grows with its input must charge that build, so a long call under
// a ceiling below one batch ends terminally instead of running to completion
// unmetered.
func TestCollectionBuild_TerminalUnderLowReductions(t *testing.T) {
	env := setupEnv(t)
	for _, b := range cbBuilders(t) {
		t.Run(b.name, func(t *testing.T) {
			fn := collectionGoFunc(t, env, b.name)
			ctx := core.WithEvalResourceLimits(t.Context(), 100, 1<<30)
			_, err := fn.Fn(ctx, nil, b.args, env)
			requireResourceLimit(t, err)
			require.Truef(t, core.IsTerminalEvalError(err),
				"%s over a %d-unit input under a 100-reduction ceiling must fail terminally, got %v", b.name, cbUnitCount, err)
		})
	}
}

// TestCollectionBuild_ExpiredDeadline: the engine-owned deadline is observed at
// the budget's sync point, so a long build surfaces context.DeadlineExceeded
// even though its own parent context is still live.
func TestCollectionBuild_ExpiredDeadline(t *testing.T) {
	env := setupEnv(t)
	for _, b := range cbBuilders(t) {
		t.Run(b.name, func(t *testing.T) {
			fn := collectionGoFunc(t, env, b.name)
			ctx := core.WithEvalResourceLimits(t.Context(), 1_000_000, 1<<30)
			ctx = core.WithEvalDeadline(ctx, time.Now().Add(-time.Millisecond))
			_, err := fn.Fn(ctx, nil, b.args, env)
			require.ErrorIsf(t, err, context.DeadlineExceeded,
				"%s over a %d-unit input past the engine deadline must surface DeadlineExceeded, got %v", b.name, cbUnitCount, err)
		})
	}
}

// TestCollectionBuild_Cancellation: caller cancellation is observed at the same
// sync point, so a long build cannot outlive the context that started it.
func TestCollectionBuild_Cancellation(t *testing.T) {
	env := setupEnv(t)
	for _, b := range cbBuilders(t) {
		t.Run(b.name, func(t *testing.T) {
			fn := collectionGoFunc(t, env, b.name)
			parent, cancel := context.WithCancel(context.Background())
			ctx := core.WithEvalResourceLimits(parent, 1_000_000, 1<<30)
			cancel()
			_, err := fn.Fn(ctx, nil, b.args, env)
			require.ErrorIsf(t, err, context.Canceled,
				"%s over a %d-unit input under a cancelled caller must surface Canceled, got %v", b.name, cbUnitCount, err)
		})
	}
}

// TestRange_TerminalOnExpiredDeadlineMidLoop: range's own ctx.Err() check sees
// only the caller's context, so an engine deadline set on a live parent goes
// unobserved. The generation loop must reach the budget's sync point instead.
func TestRange_TerminalOnExpiredDeadlineMidLoop(t *testing.T) {
	env := setupEnv(t)
	fn := collectionGoFunc(t, env, "range")
	ctx := core.WithEvalResourceLimits(t.Context(), 1_000_000, 1<<30)
	ctx = core.WithEvalDeadline(ctx, time.Now().Add(-time.Millisecond))
	_, err := fn.Fn(ctx, nil, []core.Value{core.Int{V: cbUnitCount}}, env)
	require.ErrorIsf(t, err, context.DeadlineExceeded,
		"(range %d) past an engine deadline on a live parent must surface DeadlineExceeded, got %v", cbUnitCount, err)
}

// TestSort_ResultChargesContainerOnly: sort returns a fresh List over elements
// it borrowed from its subject, so its result charge is the container and
// nothing else. Widening every element 4096-fold must not move the ledger.
//
// The total is invariant by design: the callee's explicit charge equals the
// apply-site fallback it replaces, so this is green before the migration and
// must stay green after it. The ownership change it cannot see is asserted in
// TestSort_ResultBranchIsFreshContainer.
func TestSort_ResultChargesContainerOnly(t *testing.T) {
	env := setupEnv(t)
	fn := collectionGoFunc(t, env, "sort")
	const n = 8

	tiny, err := cbApplyCharge(t, env, fn, 1<<30, cbStrings(n, 1))
	require.NoError(t, err)
	wide, err := cbApplyCharge(t, env, fn, 1<<30, cbStrings(n, cbWideLen))
	require.NoError(t, err)

	require.Equalf(t, tiny, wide,
		"sorting %d %d-byte strings charged %d bytes against %d for 1-byte ones: a fresh container over borrowed elements must not bill the payload",
		n, cbWideLen, wide, tiny)
	require.GreaterOrEqualf(t, tiny, core.ListShallowBytes(n),
		"sort charged %d bytes, want at least the %d-byte container it allocated", tiny, core.ListShallowBytes(n))
}

// TestHashMap_ResultChargesDeep: hash-map allocates entries holding its
// arguments, so the apply-site shallow fallback under-bills it by the whole
// payload. The result must carry a deep charge, which also makes it fail
// closed earlier under a tight MaxAllocationBytes.
func TestHashMap_ResultChargesDeep(t *testing.T) {
	env := setupEnv(t)
	fn := collectionGoFunc(t, env, "hash-map")
	key := core.Keyword{V: "a"}
	payloadDelta := core.StringShallowBytes(cbWideLen) - core.StringShallowBytes(1)

	tiny, err := cbApplyCharge(t, env, fn, 1<<30, key, core.String{V: "x"})
	require.NoError(t, err)
	wide, err := cbApplyCharge(t, env, fn, 1<<30, key, core.String{V: strings.Repeat("x", cbWideLen)})
	require.NoError(t, err)

	require.GreaterOrEqualf(t, wide, tiny+payloadDelta,
		"(hash-map :a <%d-byte string>) charged %d bytes against %d for a 1-byte value: the result must be charged deeply, not at the %d-byte shallow fallback",
		cbWideLen, wide, tiny, core.HashMapShallowBytes(1))

	t.Run("fails closed under a tight budget", func(t *testing.T) {
		tight := int(tiny + payloadDelta/2)
		_, err := cbApplyCharge(t, env, fn, tight, key, core.String{V: strings.Repeat("x", cbWideLen)})
		requireResourceLimit(t, err)
	})
}

// TestCollectionBuild_ValuesAndErrorsUnchanged restates the goldens the sealed
// suites already pin for these eight builtins. It is green before the
// migration and must stay green after it: every value and every typed error
// survives the budget work byte for byte. That is its whole purpose.
func TestCollectionBuild_ValuesAndErrorsUnchanged(t *testing.T) {
	env := setupEnv(t)

	t.Run("constructor shapes", func(t *testing.T) {
		list, ok := eval(t, env, `(list 1 2 3)`).(core.List)
		require.True(t, ok, "(list 1 2 3) must return a core.List")
		require.Equal(t, 3, list.Len())

		vec, ok := eval(t, env, `(vector 1 2 3)`).(core.Vector)
		require.True(t, ok, "(vector 1 2 3) must return a core.Vector")
		require.Equal(t, 3, vec.Len())

		m, ok := eval(t, env, `(hash-map :a 1 :b 2)`).(*core.HashMap)
		require.True(t, ok, "(hash-map :a 1 :b 2) must return a *core.HashMap")
		require.Equal(t, 2, m.Len())

		sorted, ok := eval(t, env, `(sort [3 1 2])`).(core.List)
		require.True(t, ok, "sort must return a core.List even for a Vector subject")
		require.Equal(t, 3, sorted.Len())
	})

	t.Run("values", func(t *testing.T) {
		for _, tt := range []struct{ name, input, want string }{
			{"list", "(list 1 2 3)", "(1 2 3)"},
			{"list empty", "(list)", "()"},
			{"concat vectors", "(concat [1 2] [3 4])", "(1 2 3 4)"},
			{"concat empty list", "(concat '())", "()"},
			{"concat nil", "(concat nil)", "()"},
			{"concat nil holes", "(concat nil '(1) nil)", "(1)"},
			{"reverse", "(reverse (list 1 2 3))", "(3 2 1)"},
			{"reverse empty list", "(reverse '())", "()"},
			{"reverse nil", "(reverse nil)", "()"},
			{"merge two maps", "(merge {:a 1} {:b 2})", "{:a 1 :b 2}"},
			{"merge later wins", "(merge {:a 1} {:a 2})", "{:a 2}"},
			{"merge single map", "(merge {:a 1})", "{:a 1}"},
			{"merge no args", "(merge)", "{}"},
			{"merge nil skipped", "(merge {:a 1} nil {:b 2})", "{:a 1 :b 2}"},
			{"merge three maps chain", "(merge {:a 1} {:b 2} {:a 3})", "{:a 3 :b 2}"},
			{"merge all-nil arguments", "(merge nil nil)", "{}"},
			{"merge sorted regardless of insertion order", "(merge {:c 3} {:a 1} {:b 2})", "{:a 1 :b 2 :c 3}"},
			{"sort ints", "(sort [3 1 2])", "(1 2 3)"},
			{"sort list input", "(sort (list 3 1 2))", "(1 2 3)"},
			{"sort already sorted", "(sort (list 1 2 3))", "(1 2 3)"},
			{"sort mixed numbers", "(sort [2.5 1 3])", "(1 2.5 3)"},
			{"sort strings", `(sort ["b" "a" "c"])`, `("a" "b" "c")`},
			{"sort keywords", "(sort [:c :a :b])", "(:a :b :c)"},
			{"sort empty vector", "(sort [])", "()"},
			{"sort empty list", "(sort '())", "()"},
			{"sort nil", "(sort nil)", "()"},
			{"range end only", "(range 3)", "(0 1 2)"},
			{"range start end", "(range 2 5)", "(2 3 4)"},
			{"range with step", "(range 0 10 3)", "(0 3 6 9)"},
			{"range negative step", "(range 3 0 -1)", "(3 2 1)"},
			{"range empty", "(range 0)", "()"},
			{"range unreachable", "(range 5 2)", "()"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				got := eval(t, env, tt.input)
				require.Equalf(t, tt.want, got.String(), "%s value drift", tt.input)
			})
		}
	})

	t.Run("typedErrors", func(t *testing.T) {
		for _, tt := range []struct{ name, input, code, msg string }{
			{"hash-map odd arity", "(hash-map :a)", "ArityError", "hash-map: requires even number of arguments"},
			{"concat scalar", "(concat 5)", "TypeError", "concat: expected collection, got core.Int"},
			{"reverse scalar", "(reverse 5)", "TypeError", "reverse: expected collection, got core.Int"},
			{"reverse arity", "(reverse)", "ArityError", "reverse: requires 1 argument"},
			{"merge non-map", "(merge {:a 1} 5)", "TypeError", "merge: expected map, got core.Int"},
			{"sort zero args", "(sort)", "ArityError", "sort: requires 1 argument"},
			{"sort two args", "(sort [1] [2])", "ArityError", "sort: requires 1 argument"},
			{"sort int subject", "(sort 5)", "TypeError", "sort: expected collection, got core.Int"},
			{"sort keyword subject", "(sort :k)", "TypeError", "sort: expected collection, got core.Keyword"},
			{"sort mixed kinds", `(sort [1 "a"])`, "EvalError", "sort: cannot compare core.String with core.Int"},
			{"range zero step", "(range 0 5 0)", "EvalError", "range: step must not be zero"},
			{"range non-int", "(range 1.5)", "TypeError", "range: requires integer arguments, got core.Float"},
			{"range arity", "(range)", "ArityError", "range: requires 1 to 3 arguments"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				wantTypedError(t, evalErr(t, env, tt.input), tt.code, tt.msg)
			})
		}
	})

	// The collection-length and structural-depth ceilings are asserted
	// verbatim elsewhere; the budget migration must not reword them.
	t.Run("limitMessages", func(t *testing.T) {
		err := evalErr(t, env, "(range 0 99999999)")
		requireResourceLimit(t, err)
		require.ErrorContains(t, err, "range length 99999999 exceeds collection limit")

		ev := core.NewEvaluator()
		ev.MaxCollectionLen = 3
		err = evalErrUnder(t, ev, setupEnv(t), "(concat [1 2] [3 4])")
		requireResourceLimit(t, err)
		require.ErrorContains(t, err, "concat length 4 exceeds collection limit 3")

		fn := collectionGoFunc(t, env, "hash-map")
		_, err = fn.Fn(t.Context(), depthLimitEvaluator{limit: 1},
			[]core.Value{core.Keyword{V: "a"}, core.NewList([]core.Value{core.Int{V: 1}})}, env)
		requireResourceLimit(t, err)
		require.ErrorContains(t, err, "structural depth limit 1 exceeded")
	})
}

// sort's ledger total and its interruption behaviour are unchanged by design,
// so no meter assertion can tell the intended end state from the current one.
// What moves is ownership: the callee declares its own result, suppressing the
// apply-site fallback, and the pre-kernel copy gets its own budgeted phase.
// The inventory is where that is checkable.

// TestSort_ResultBranchIsFreshContainer: sort returns a List it allocated over
// elements borrowed from its subject, so the inventory must classify that
// branch as a fresh container stating its own charge expression, rather than
// leave the apply-site fallback owning the result.
func TestSort_ResultBranchIsFreshContainer(t *testing.T) {
	const (
		wantFn    = "sort"
		wantLabel = "sorted list"
	)
	for _, got := range inventory.ResultBranches {
		if got.Fn != wantFn || got.BranchLabel != wantLabel {
			continue
		}
		require.Equalf(t, "plugins/stdlib/collections.go", got.File, "%s/%s: file", wantFn, wantLabel)
		require.Equalf(t, "fresh-container", got.Class, "%s/%s: class", wantFn, wantLabel)
		require.NotEmptyf(t, got.ChargeExpr, "%s/%s: a fresh container must state the quantity it charges", wantFn, wantLabel)
		return
	}
	t.Fatalf("inventory.ResultBranches has no row Fn %q BranchLabel %q: sort's result is still owned by the apply-site fallback", wantFn, wantLabel)
}

// TestSort_ToSliceCopyIsBudgeted: the ToSlice copy of the subject happens
// before StableSort is entered and grows with the subject, so it is stdlib's
// own work and must be recorded as budgeted.
func TestSort_ToSliceCopyIsBudgeted(t *testing.T) {
	const (
		wantFn    = "sort"
		wantLabel = "subject copy"
	)
	for _, got := range inventory.WorkPhases {
		if got.Fn != wantFn || got.PhaseLabel != wantLabel {
			continue
		}
		require.Equalf(t, "plugins/stdlib/collections.go", got.File, "%s/%s: file", wantFn, wantLabel)
		require.Equalf(t, "budgeted", got.Disposition, "%s/%s: disposition", wantFn, wantLabel)
		return
	}
	t.Fatalf("inventory.WorkPhases has no row Fn %q PhaseLabel %q: the pre-kernel copy sort performs is unrecorded", wantFn, wantLabel)
}

// TestSort_KernelWorkNotDoubleCharged: internal/collections.StableSort already
// charges the comparison kernel. A budgeted stdlib phase naming that file would
// bill the same work a second time, which is the double charge this migration
// forbids. Green before and after; it exists to stay that way.
func TestSort_KernelWorkNotDoubleCharged(t *testing.T) {
	const kernelFile = "internal/collections/kernels.go"
	for _, got := range inventory.WorkPhases {
		if got.Fn != "sort" || got.Disposition != "budgeted" {
			continue
		}
		require.NotEqualf(t, kernelFile, got.File,
			"sort work phase %q is budgeted against %s: the kernel already charges that work, so a second row double-charges it", got.PhaseLabel, kernelFile)
	}
}

// The collection-length ceiling on hash-map's result is NEW as of this change:
// routing the result charge through chargeCollectionResult put hash-map under
// the same limit merge already enforced, closing an inconsistency where one
// map-returning builtin ignored the collection limit. The ordering is
// deliberate too — the length check runs before the structural-depth walk, so
// when both limits are breached the length message is the one that surfaces.
// Both are pinned here because nothing else runs these builtins under narrow
// limits.

func TestHashMap_RespectsCollectionLimit(t *testing.T) {
	ev := core.NewEvaluator()
	ev.MaxCollectionLen = 2
	err := evalErrUnder(t, ev, setupEnv(t), "(hash-map :a 1 :b 2 :c 3)")
	requireResourceLimit(t, err)
	require.ErrorContains(t, err, "hash-map length 3 exceeds collection limit 2")
}

func TestHashMap_LengthLimitPrecedesDepthLimit(t *testing.T) {
	ev := core.NewEvaluator()
	ev.MaxCollectionLen = 1
	ev.MaxStructuralDepth = 1
	err := evalErrUnder(t, ev, setupEnv(t), "(hash-map :a (list 1) :b (list 2))")
	requireResourceLimit(t, err)
	require.ErrorContains(t, err, "hash-map length 2 exceeds collection limit 1")
	require.NotContains(t, err.Error(), "structural depth limit",
		"the length check runs first, so the depth message must not be the one that surfaces")
}

// TestMerge_RespectsCollectionLimit is the consistency claim that justifies
// keeping hash-map's new ceiling: merge returns a *HashMap under the same
// limit and has always enforced it.
func TestMerge_RespectsCollectionLimit(t *testing.T) {
	ev := core.NewEvaluator()
	ev.MaxCollectionLen = 2
	err := evalErrUnder(t, ev, setupEnv(t), "(merge {:a 1 :b 2} {:c 3})")
	requireResourceLimit(t, err)
	require.ErrorContains(t, err, "merge length 3 exceeds collection limit 2")
}
