package stdlib

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/internal/inventory"
)

// lbPayload builds a string whose shallow size is the charge a borrowed branch
// must not apply. Two runs differing only in width differ only by that charge.
func lbPayload(width int) core.String {
	return core.String{V: strings.Repeat("x", width)}
}

// lbSharedList builds a List past core's flat threshold, so it is stored as a
// shared-tail chain and At walks one node per index. A flat list makes the same
// access O(1) and would prove nothing. cbUnitCount also clears the 128-unit
// batch interval, so the walk reaches a sync point instead of finishing inside
// one local batch.
func lbSharedList(n int) core.List {
	items := make([]core.Value, n)
	for i := range items {
		items[i] = core.Int{V: int64(i)}
	}
	return core.NewList(items)
}

// lbRequireResultBranch fails unless the inventory records a branch of class
// for fn. Rows are matched on Fn and Class and never on BranchLabel: the label
// is the coder's to choose, the classification is the contract.
func lbRequireResultBranch(t *testing.T, fn, class string) {
	t.Helper()
	for _, got := range inventory.ResultBranches {
		if !slices.Contains(strings.Fields(got.Fn), fn) || got.Class != class {
			continue
		}
		require.Equalf(t, "plugins/stdlib/collections.go", got.File, "%s/%s: file", fn, got.BranchLabel)
		if class == "fresh-container" {
			require.NotEmptyf(t, got.ChargeExpr, "%s/%s: a fresh container must state the quantity it charges", fn, got.BranchLabel)
		}
		return
	}
	t.Fatalf("inventory.ResultBranches has no %s row for %s: that branch is still owned by the apply-site fallback", class, fn)
}

// TestBorrowed_FirstLastNthRestAreZeroByte: every branch here hands back a
// value its subject already owned, so the ledger must not grow with the
// quantity the branch never allocated, and the wide run must still clear a
// budget tighter than that quantity. The tight budget is what separates a
// genuinely borrowed branch from one that is merely cheap.
//
// first, last and nth borrow an element, so their discriminating axis is the
// element's payload. rest borrows a tail, whose shallow size grows with the
// subject's length and never with its elements, so its axis is length instead.
func TestBorrowed_FirstLastNthRestAreZeroByte(t *testing.T) {
	env := setupEnv(t)

	subject := func(p core.Value) core.List {
		return core.NewList([]core.Value{p, p, p})
	}

	arms := []struct {
		name    string
		builtin string
		args    func(p core.Value) []core.Value
	}{
		{"first", "first", func(p core.Value) []core.Value { return []core.Value{subject(p)} }},
		{"last", "last", func(p core.Value) []core.Value { return []core.Value{subject(p)} }},
		{"nth hit", "nth", func(p core.Value) []core.Value { return []core.Value{subject(p), core.Int{V: 0}} }},
		{"nth default", "nth", func(p core.Value) []core.Value { return []core.Value{subject(p), core.Int{V: 99}, p} }},
	}

	payloadShallow := core.StringShallowBytes(cbWideLen)
	for _, arm := range arms {
		t.Run(arm.name, func(t *testing.T) {
			fn := collectionGoFunc(t, env, arm.builtin)

			tiny, err := cbApplyCharge(t, env, fn, 1<<30, arm.args(lbPayload(1))...)
			require.NoError(t, err)
			wide, err := cbApplyCharge(t, env, fn, 1<<30, arm.args(lbPayload(cbWideLen))...)
			require.NoError(t, err)

			require.Equalf(t, tiny, wide,
				"%s over a %d-byte payload charged %d bytes against %d for a 1-byte one: a borrowed result must add zero bytes to the ledger",
				arm.builtin, cbWideLen, wide, tiny)

			tight := int(tiny + payloadShallow/2)
			_, err = cbApplyCharge(t, env, fn, tight, arm.args(lbPayload(cbWideLen))...)
			require.NoErrorf(t, err,
				"%s: a borrowed result must not trip a %d-byte budget, tighter than the %d-byte shallow size of the value it hands back",
				arm.builtin, tight, payloadShallow)
		})
	}

	t.Run("rest length axis", func(t *testing.T) {
		fn := collectionGoFunc(t, env, "rest")
		ints := func(n int) []core.Value {
			vs := make([]core.Value, n)
			for i := range vs {
				vs[i] = core.Int{V: int64(i)}
			}
			return vs
		}

		// Both subjects are built before the measured window opens, so the only
		// length-dependent quantity inside it is the tail rest hands back.
		short, err := cbApplyCharge(t, env, fn, 1<<30, core.NewList(ints(2)))
		require.NoError(t, err)
		long, err := cbApplyCharge(t, env, fn, 1<<30, core.NewList(ints(cbUnitCount)))
		require.NoError(t, err)

		require.Equalf(t, short, long,
			"rest over a %d-element list charged %d bytes against %d over a 2-element one: a borrowed tail must add zero bytes to the ledger",
			cbUnitCount, long, short)

		tailShallow := core.ListShallowBytes(cbUnitCount - 1)
		tight := int(short + (tailShallow-core.ListShallowBytes(1))/2)
		_, err = cbApplyCharge(t, env, fn, tight, core.NewList(ints(cbUnitCount)))
		require.NoErrorf(t, err,
			"rest: a borrowed tail must not trip a %d-byte budget, tighter than the %d-byte shallow size of the tail it hands back",
			tight, tailShallow)
	})

	// The ledger probe above goes green the moment the branch stops charging;
	// the row is what keeps that disposition recorded rather than incidental.
	t.Run("rest list tail is borrowed", func(t *testing.T) {
		lbRequireResultBranch(t, "rest", "borrowed")
	})
}

// TestFresh_KeysValsVectorRestChargeContainerOnly: each of these branches
// allocates a List over elements it borrowed from its subject, so the ledger
// must carry the container it allocated and nothing else — the same total for a
// 1-byte payload as for a 4096-byte one.
//
// That total is invariant by design: the callee's explicit charge equals the
// apply-site fallback it replaces, so the ledger arms are green before the
// migration and must stay green after it. The ownership change no meter can see
// is pinned by the result-branch rows instead.
func TestFresh_KeysValsVectorRestChargeContainerOnly(t *testing.T) {
	env := setupEnv(t)
	const n = 4

	elem := func(i, width int) core.String {
		return core.String{V: fmt.Sprintf("%02d", i) + strings.Repeat("x", width)}
	}
	payloadMap := func(width int) *core.HashMap {
		m := core.NewHashMap()
		for i := range n {
			require.NoError(t, m.Set(elem(i, width), elem(i, width)))
		}
		return m
	}
	payloadVector := func(width int) core.Vector {
		items := make([]core.Value, n)
		for i := range items {
			items[i] = elem(i, width)
		}
		return core.NewVector(items)
	}

	arms := []struct {
		name    string
		builtin string
		want    int64
		args    func(width int) []core.Value
	}{
		{"keys", "keys", core.ListShallowBytes(n), func(w int) []core.Value { return []core.Value{payloadMap(w)} }},
		{"vals", "vals", core.ListShallowBytes(n), func(w int) []core.Value { return []core.Value{payloadMap(w)} }},
		{"vector rest", "rest", core.ListShallowBytes(n - 1), func(w int) []core.Value { return []core.Value{payloadVector(w)} }},
	}

	for _, arm := range arms {
		t.Run(arm.name, func(t *testing.T) {
			fn := collectionGoFunc(t, env, arm.builtin)

			tiny, err := cbApplyCharge(t, env, fn, 1<<30, arm.args(1)...)
			require.NoError(t, err)
			wide, err := cbApplyCharge(t, env, fn, 1<<30, arm.args(cbWideLen)...)
			require.NoError(t, err)

			require.Equalf(t, arm.want, tiny,
				"%s charged %d bytes over %d elements, want the %d-byte container it allocated", arm.builtin, tiny, n, arm.want)
			require.Equalf(t, tiny, wide,
				"%s charged %d bytes over %d-byte elements against %d over 1-byte ones: a fresh container over borrowed elements must not bill the payload",
				arm.builtin, wide, cbWideLen, tiny)
		})
	}

	t.Run("fresh container rows", func(t *testing.T) {
		for _, name := range []string{"keys", "vals", "rest"} {
			t.Run(name, func(t *testing.T) {
				lbRequireResultBranch(t, name, "fresh-container")
			})
		}
	})
}

// TestLast_TerminalOnSharedListUnderLowReductions: last reaches its element
// through List.At, which walks one node per index on a shared-tail list. That
// walk grows with the subject and is the builtin's own work, so a long list
// under a ceiling below one batch must end terminally instead of running to
// completion unmetered.
func TestLast_TerminalOnSharedListUnderLowReductions(t *testing.T) {
	env := setupEnv(t)
	fn := collectionGoFunc(t, env, "last")
	ctx := core.WithEvalResourceLimits(t.Context(), 100, 1<<30)
	_, err := fn.Fn(ctx, nil, []core.Value{lbSharedList(cbUnitCount)}, env)
	requireResourceLimit(t, err)
	require.Truef(t, core.IsTerminalEvalError(err),
		"last over a %d-element shared list under a 100-reduction ceiling must fail terminally, got %v", cbUnitCount, err)
}

// TestLast_ExpiredDeadline: the engine-owned deadline is observed at the
// budget's sync point, so the traversal surfaces context.DeadlineExceeded even
// though its own parent context is still live.
func TestLast_ExpiredDeadline(t *testing.T) {
	env := setupEnv(t)
	fn := collectionGoFunc(t, env, "last")
	ctx := core.WithEvalResourceLimits(t.Context(), 1_000_000, 1<<30)
	ctx = core.WithEvalDeadline(ctx, time.Now().Add(-time.Millisecond))
	_, err := fn.Fn(ctx, nil, []core.Value{lbSharedList(cbUnitCount)}, env)
	require.ErrorIsf(t, err, context.DeadlineExceeded,
		"last over a %d-element shared list past the engine deadline must surface DeadlineExceeded, got %v", cbUnitCount, err)
}

// TestLast_Cancellation: caller cancellation is observed at the same sync
// point, so the traversal cannot outlive the context that started it.
func TestLast_Cancellation(t *testing.T) {
	env := setupEnv(t)
	fn := collectionGoFunc(t, env, "last")
	parent, cancel := context.WithCancel(context.Background())
	ctx := core.WithEvalResourceLimits(parent, 1_000_000, 1<<30)
	cancel()
	_, err := fn.Fn(ctx, nil, []core.Value{lbSharedList(cbUnitCount)}, env)
	require.ErrorIsf(t, err, context.Canceled,
		"last over a %d-element shared list under a cancelled caller must surface Canceled, got %v", cbUnitCount, err)
}

// TestCount_StringScanBounded: count reads an O(1) Len on List, Vector, HashMap
// and Nil, but a String subject is converted whole to []rune — an opaque scan
// growing with the subject that the builtin cannot preempt. It must be recorded
// as a bounded exception carrying proof and a maximum, and must still report
// runes rather than bytes.
func TestCount_StringScanBounded(t *testing.T) {
	t.Run("bounded exception row", func(t *testing.T) {
		for _, got := range inventory.WorkPhases {
			if !slices.Contains(strings.Fields(got.Fn), "count") || got.Disposition != "bounded-exception" {
				continue
			}
			require.Equalf(t, "plugins/stdlib/collections.go", got.File, "count/%s: file", got.PhaseLabel)
			require.NotEmptyf(t, got.Proof, "count/%s: a bounded exception must state the proof of its bound", got.PhaseLabel)
			require.NotZerof(t, got.MaxWork, "count/%s: a bounded exception must state its maximum", got.PhaseLabel)
			return
		}
		t.Fatal("inventory.WorkPhases has no bounded-exception row for count: the []rune scan of a String subject is unaccounted for")
	})

	t.Run("exact rune count", func(t *testing.T) {
		env := setupEnv(t)
		fn := collectionGoFunc(t, env, "count")
		const runes = 4096
		ctx := core.WithEvalResourceLimits(t.Context(), 1<<20, 1<<30)
		got, err := fn.Fn(ctx, nil, []core.Value{core.String{V: strings.Repeat("é", runes)}}, env)
		require.NoError(t, err)
		require.Equalf(t, core.Int{V: runes}, got,
			"count over %d two-byte runes must report runes, not bytes", runes)
	})
}

// TestLookup_ActiveEvaluatorPolicyDirectFn extends the arm table of
// TestCollections_LimitsFromActiveEvaluatorNotEnv to the lookup seam's own
// builtins: limits come from the evaluator that invoked the builtin, never from
// the scope it happens to run in, and without one the stdlib defaults apply.
//
// The behaviour is already correct, so this is a regression pin — green before
// the migration and green after it. It exists so a later ownership change
// cannot quietly reroute a limit through the environment.
func TestLookup_ActiveEvaluatorPolicyDirectFn(t *testing.T) {
	root := setupEnv(t)
	child := root.Child()
	child.SetEvaluator(nil)
	require.Nil(t, child.Evaluator())

	list := func(vs ...core.Value) core.List { return core.NewList(vs) }
	one, two, three := core.Int{V: 1}, core.Int{V: 2}, core.Int{V: 3}
	ka, kb, kc := core.Keyword{V: "a"}, core.Keyword{V: "b"}, core.Keyword{V: "c"}
	nested := list(list(one))

	arms := []struct {
		name, builtin string
		eval          core.Evaluator
		args          []core.Value
		msg           string
	}{
		{"assoc length", "assoc", collectionLimitEvaluator{limit: 2}, []core.Value{core.NewHashMap(), ka, one, kb, two, kc, three}, "assoc length 3 exceeds collection limit 2"},
		{"assoc nested depth", "assoc", depthLimitEvaluator{limit: 2}, []core.Value{core.NewHashMap(), ka, nested}, "structural depth limit 2 exceeded"},
		{"conj vector length", "conj", collectionLimitEvaluator{limit: 2}, []core.Value{core.NewVector([]core.Value{one, two}), three}, "conj length 3 exceeds collection limit 2"},
		{"conj vector nested depth", "conj", depthLimitEvaluator{limit: 2}, []core.Value{core.NewVector([]core.Value{one}), nested}, "structural depth limit 2 exceeded"},
		{"hash-map length", "hash-map", collectionLimitEvaluator{limit: 2}, []core.Value{ka, one, kb, two, kc, three}, "hash-map length 3 exceeds collection limit 2"},
		{"hash-map nested depth", "hash-map", depthLimitEvaluator{limit: 1}, []core.Value{ka, list(one)}, "structural depth limit 1 exceeded"},
		{"list length", "list", collectionLimitEvaluator{limit: 2}, []core.Value{one, two, three}, "list length 3 exceeds collection limit 2"},
		{"list nested depth", "list", depthLimitEvaluator{limit: 1}, []core.Value{list(one)}, "structural depth limit 1 exceeded"},
	}
	for _, arm := range arms {
		fn := collectionGoFunc(t, root, arm.builtin)
		t.Run(arm.name+" from eval", func(t *testing.T) {
			_, err := fn.Fn(t.Context(), arm.eval, arm.args, child)
			requireResourceLimit(t, err)
			require.ErrorContains(t, err, arm.msg)
		})
		t.Run(arm.name+" default without eval", func(t *testing.T) {
			_, err := fn.Fn(t.Context(), nil, arm.args, child)
			require.NoError(t, err)
		})
	}
}
