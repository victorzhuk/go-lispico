package stdlib

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/internal/inventory"
)

// numArgCount sits above the 128-unit batch interval of core's builtin work
// budget, so an argument loop that steps once per argument reaches a sync
// point mid-walk instead of finishing entirely inside the local batch.
const numArgCount = 200

// numTraversalOps is every numeric builtin whose argument walk grows with the
// call: each must reach a sync point. Descending inputs keep the ordering
// chains from short-circuiting before the walk is long enough to sync.
var numTraversalOps = []struct {
	name       string
	descending bool
}{
	{"+", false},
	{"-", false},
	{"*", false},
	{"/", false},
	{"max", false},
	{"min", false},
	{"<", false},
	{"<=", false},
	{">", true},
	{">=", true},
}

// numArgs builds numArgCount distinct non-zero Ints. Distinct values keep the
// ordering chains monotonic and non-zero ones keep / out of its domain error,
// so every op walks its whole argument list.
func numArgs(descending bool) []core.Value {
	args := make([]core.Value, numArgCount)
	for i := range args {
		if descending {
			args[i] = core.Int{V: int64(numArgCount - i)}
		} else {
			args[i] = core.Int{V: int64(i + 1)}
		}
	}
	return args
}

// numDeepList builds a List deep enough that comparing two of them costs more
// than the batch interval, so a bounded comparison syncs mid-walk.
func numDeepList() core.Value {
	vs := make([]core.Value, numArgCount)
	for i := range vs {
		vs[i] = core.Int{V: int64(i)}
	}
	return core.NewList(vs)
}

// TestNumeric_ArgTraversalTerminalUnderLowReductions: every numeric builtin
// whose cost grows with its argument count must charge that walk, so a long
// call under a ceiling below one batch ends terminally instead of running to
// completion unmetered.
func TestNumeric_ArgTraversalTerminalUnderLowReductions(t *testing.T) {
	env := setupEnv(t)
	for _, op := range numTraversalOps {
		t.Run(op.name, func(t *testing.T) {
			fn := collectionGoFunc(t, env, op.name)
			ctx := core.WithEvalResourceLimits(t.Context(), 100, 1<<30)
			_, err := fn.Fn(ctx, nil, numArgs(op.descending), env)
			requireResourceLimit(t, err)
			require.Truef(t, core.IsTerminalEvalError(err),
				"%s over %d arguments under a 100-reduction ceiling must fail terminally, got %v", op.name, numArgCount, err)
		})
	}
}

// TestNumeric_ArgTraversalTerminalUnderExpiredDeadline: the engine-owned
// deadline is observed at the budget's sync point, so a long numeric call
// surfaces context.DeadlineExceeded even though its own parent context is
// still live.
func TestNumeric_ArgTraversalTerminalUnderExpiredDeadline(t *testing.T) {
	env := setupEnv(t)
	for _, op := range numTraversalOps {
		t.Run(op.name, func(t *testing.T) {
			fn := collectionGoFunc(t, env, op.name)
			ctx := core.WithEvalResourceLimits(t.Context(), 1_000_000, 1<<30)
			ctx = core.WithEvalDeadline(ctx, time.Now().Add(-time.Millisecond))
			_, err := fn.Fn(ctx, nil, numArgs(op.descending), env)
			require.ErrorIsf(t, err, context.DeadlineExceeded,
				"%s over %d arguments past the engine deadline must surface DeadlineExceeded, got %v", op.name, numArgCount, err)
		})
	}
}

// TestNumeric_ArgTraversalTerminalUnderCancellation: caller cancellation is
// observed at the same sync point, so a long numeric call cannot outlive the
// context that started it.
func TestNumeric_ArgTraversalTerminalUnderCancellation(t *testing.T) {
	env := setupEnv(t)
	for _, op := range numTraversalOps {
		t.Run(op.name, func(t *testing.T) {
			fn := collectionGoFunc(t, env, op.name)
			parent, cancel := context.WithCancel(context.Background())
			ctx := core.WithEvalResourceLimits(parent, 1_000_000, 1<<30)
			cancel()
			_, err := fn.Fn(ctx, nil, numArgs(op.descending), env)
			require.ErrorIsf(t, err, context.Canceled,
				"%s over %d arguments under a cancelled caller must surface Canceled, got %v", op.name, numArgCount, err)
		})
	}
}

// TestNumeric_ShortCallsKeepExactValuesAndErrors is the characterisation
// test for the numeric family: every value and every typed error a short call
// produces today must survive the budget migration byte for byte. It passes
// before the migration and must still pass after it; that is its whole
// purpose.
func TestNumeric_ShortCallsKeepExactValuesAndErrors(t *testing.T) {
	env := setupEnv(t)

	t.Run("values", func(t *testing.T) {
		for _, tt := range []struct {
			input string
			want  core.Value
		}{
			{"(+)", core.Int{V: 0}},
			{"(+ 5)", core.Int{V: 5}},
			{"(+ 1 2)", core.Int{V: 3}},
			{"(+ 1 2 3 4)", core.Int{V: 10}},
			{"(+ 1 2.5)", core.Float{V: 3.5}},
			{"(+ 1.5 2.5)", core.Float{V: 4.0}},
			{"(- 5)", core.Int{V: -5}},
			{"(- 10 3)", core.Int{V: 7}},
			{"(- 10 1 2 3)", core.Int{V: 4}},
			{"(- 3.5)", core.Float{V: -3.5}},
			{"(- 10 2.5)", core.Float{V: 7.5}},
			{"(*)", core.Int{V: 1}},
			{"(* 5)", core.Int{V: 5}},
			{"(* 3 4)", core.Int{V: 12}},
			{"(* 2 3 4)", core.Int{V: 24}},
			{"(* 3 2.5)", core.Float{V: 7.5}},
			{"(/ 10 2)", core.Float{V: 5.0}},
			{"(/ 100 2 5)", core.Float{V: 10.0}},
			{"(/ 5 2)", core.Float{V: 2.5}},
			{"(mod 10 3)", core.Int{V: 1}},
			{"(mod 9 3)", core.Int{V: 0}},
			{"(quot 10 3)", core.Int{V: 3}},
			{"(quot 12 4)", core.Int{V: 3}},
			{"(pow 2 3)", core.Float{V: 8.0}},
			{"(sqrt 16)", core.Float{V: 4.0}},
			{"(abs 5)", core.Float{V: 5.0}},
			{"(abs -5)", core.Float{V: 5.0}},
			{"(floor 3.7)", core.Float{V: 3.0}},
			{"(ceil 3.2)", core.Float{V: 4.0}},
			{"(zero? 0)", core.Bool{V: true}},
			{"(zero? 1)", core.Bool{V: false}},
			{"(pos? 5)", core.Bool{V: true}},
			{"(pos? -5)", core.Bool{V: false}},
			{"(neg? -5)", core.Bool{V: true}},
			{"(neg? 5)", core.Bool{V: false}},
			{"(max 1 5 3)", core.Int{V: 5}},
			{"(min 1 5 3)", core.Int{V: 1}},
			{"(max 1 5.5 3)", core.Float{V: 5.5}},
			{"(min 1.5 5 3)", core.Float{V: 1.5}},
			{"(= 1)", core.Bool{V: true}},
			{"(= 1 1)", core.Bool{V: true}},
			{"(= 1 2)", core.Bool{V: false}},
			{"(= 2 2 2 2)", core.Bool{V: true}},
			{"(= 2 2 3)", core.Bool{V: false}},
			{"(= 1 1.0)", core.Bool{V: false}},
			{"(= 1.5 1.5)", core.Bool{V: true}},
			{`(= "a" "a")`, core.Bool{V: true}},
			{`(= "a" "b")`, core.Bool{V: false}},
			{"(= :cheap :cheap)", core.Bool{V: true}},
			{"(= :cheap :smart)", core.Bool{V: false}},
			{`(= 1 "1")`, core.Bool{V: false}},
			{"(= nil nil)", core.Bool{V: true}},
			{"(= [1 2] [1 2])", core.Bool{V: true}},
			{"(= [1 2] [1 3])", core.Bool{V: false}},
			{"(= {:a 1} {:a 1})", core.Bool{V: true}},
			{"(< 1 2)", core.Bool{V: true}},
			{"(< 2 1)", core.Bool{V: false}},
			{"(< 1 1)", core.Bool{V: false}},
			{"(< 1 2 3)", core.Bool{V: true}},
			{"(< 1 3 2)", core.Bool{V: false}},
			{"(< 5)", core.Bool{V: true}},
			{"(< 1 1.5)", core.Bool{V: true}},
			{"(< 2.5 2)", core.Bool{V: false}},
			{"(< 9007199254740992 9007199254740993)", core.Bool{V: true}},
			{"(> 2 1)", core.Bool{V: true}},
			{"(> 1 2)", core.Bool{V: false}},
			{"(> 3 2 1)", core.Bool{V: true}},
			{"(> 3 1 2)", core.Bool{V: false}},
			{"(<= 1 1)", core.Bool{V: true}},
			{"(<= 1 1 2)", core.Bool{V: true}},
			{"(<= 2 1)", core.Bool{V: false}},
			{"(>= 1 1)", core.Bool{V: true}},
			{"(>= 3 3 2)", core.Bool{V: true}},
			{"(>= 1 2)", core.Bool{V: false}},
			{"(< 1.1 1.2 1.3)", core.Bool{V: true}},
		} {
			t.Run(tt.input, func(t *testing.T) {
				got := eval(t, env, tt.input)
				require.Truef(t, got.Equals(tt.want), "%s = %v, want %v", tt.input, got, tt.want)
			})
		}
	})

	t.Run("typedErrors", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			builtin  string
			args     []core.Value
			wantCode string
			wantMsg  string
		}{
			{"mod exact one", "mod", []core.Value{core.Int{V: 1}}, "ArityError", "mod:"},
			{"quot exact three", "quot", []core.Value{core.Int{V: 1}, core.Int{V: 2}, core.Int{V: 3}}, "ArityError", "quot:"},
			{"divide variadic min", "/", []core.Value{core.Int{V: 1}}, "ArityError", "/:"},
			{"minus variadic min", "-", nil, "ArityError", "-:"},
			{"max variadic min", "max", nil, "ArityError", "max:"},
			{"min variadic min", "min", nil, "ArityError", "min:"},
			{"equal variadic min", "=", nil, "ArityError", "=:"},
			{"less variadic min", "<", nil, "ArityError", "<:"},
			{"plus non-number", "+", []core.Value{core.Int{V: 1}, core.String{V: "a"}}, "TypeError", "expected number"},
			{"minus non-number", "-", []core.Value{core.String{V: "a"}}, "TypeError", "expected number"},
			{"multiply non-number", "*", []core.Value{core.Keyword{V: "a"}}, "TypeError", "expected number"},
			{"mod float operand", "mod", []core.Value{core.Float{V: 1.5}, core.Int{V: 2}}, "TypeError", "mod:"},
			{"quot string operand", "quot", []core.Value{core.String{V: "1"}, core.Int{V: 2}}, "TypeError", "quot:"},
			{"divide non-number dividend", "/", []core.Value{core.String{V: "1"}, core.Int{V: 2}}, "TypeError", "/:"},
			{"divide non-number divisor", "/", []core.Value{core.Int{V: 1}, core.String{V: "2"}}, "TypeError", "/:"},
			{"max non-number", "max", []core.Value{core.Int{V: 1}, core.String{V: "a"}}, "TypeError", "expected number"},
			{"min non-number", "min", []core.Value{core.String{V: "a"}}, "TypeError", "expected number"},
			{"less string arg", "<", []core.Value{core.Int{V: 1}, core.String{V: "a"}}, "TypeError", "expected number"},
			{"less single non-number", "<", []core.Value{core.String{V: "a"}}, "TypeError", "expected number"},
			{"ge keyword arg", ">=", []core.Value{core.Keyword{V: "a"}, core.Int{V: 1}}, "TypeError", "expected number"},
			{"divide int zero", "/", []core.Value{core.Int{V: 1}, core.Int{V: 0}}, "EvalError", "/:"},
			{"divide float zero", "/", []core.Value{core.Int{V: 1}, core.Float{V: 0}}, "EvalError", "/:"},
			{"mod zero divisor", "mod", []core.Value{core.Int{V: 1}, core.Int{V: 0}}, "EvalError", "mod:"},
			{"quot zero divisor", "quot", []core.Value{core.Int{V: 1}, core.Int{V: 0}}, "EvalError", "quot:"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				err := builtinErr(t, env, tt.builtin, tt.args...)
				var le *core.LispicoError
				require.ErrorAs(t, err, &le)
				require.Equalf(t, tt.wantCode, le.Code, "%s: code drift, got %v", tt.builtin, err)
				require.Containsf(t, le.Message, tt.wantMsg, "%s: message drift", tt.builtin)
			})
		}
	})
}

// TestEquals_DeepComparisonIsInterruptible: = walks whole structures, so its
// comparison must charge per compared node and end terminally under a low
// ceiling rather than running an unbounded traversal to completion.
func TestEquals_DeepComparisonIsInterruptible(t *testing.T) {
	env := setupEnv(t)
	fn := collectionGoFunc(t, env, "=")
	ctx := core.WithEvalResourceLimits(t.Context(), 100, 1<<30)
	_, err := fn.Fn(ctx, nil, []core.Value{numDeepList(), numDeepList()}, env)
	requireResourceLimit(t, err)
	require.Truef(t, core.IsTerminalEvalError(err),
		"= over two equal %d-element Lists under a 100-reduction ceiling must fail terminally, got %v", numArgCount, err)
}

// TestEquals_HostValueIsTrustedBoundary: a host Value's own Equals is work the
// runtime cannot preempt, so the inventory must record that boundary as a
// trusted-host disposition rather than leave = looking fully budgeted.
func TestEquals_HostValueIsTrustedBoundary(t *testing.T) {
	const (
		wantFn    = "="
		wantLabel = "host-equals-boundary"
	)
	for _, got := range inventory.WorkPhases {
		if got.Fn != wantFn || got.PhaseLabel != wantLabel {
			continue
		}
		require.Equalf(t, []string{"numeric"}, got.Families, "%s/%s: families", wantFn, wantLabel)
		require.Equalf(t, "plugins/stdlib/comparison.go", got.File, "%s/%s: file", wantFn, wantLabel)
		require.Equalf(t, "equalsAll", got.Func, "%s/%s: func", wantFn, wantLabel)
		require.Equalf(t, "trusted-host", got.Disposition, "%s/%s: disposition", wantFn, wantLabel)
		return
	}
	t.Fatalf("inventory.WorkPhases has no row Fn %q PhaseLabel %q: the host Equals boundary reached by = is unrecorded", wantFn, wantLabel)
}

// numApplyAllocCharge dispatches fn through the apply site, which is where the
// fallback shallow charge lives, and reports the allocation bytes the ledger
// ended up holding.
func numApplyAllocCharge(t *testing.T, env *core.Env, fn core.Value, args ...core.Value) int64 {
	t.Helper()
	ctx := core.WithEvalResourceLimits(context.Background(), 1<<20, 1<<30)
	if _, err := core.NewEvaluator().Apply(ctx, fn, args, env); err != nil {
		t.Fatalf("apply: %v", err)
	}
	return core.EvalMeterFrom(ctx).Snapshot().AllocationBytes
}

// TestTypes_ConversionViewsAreZeroByte: str->keyword and keyword->str retype
// their argument's existing bytes rather than allocating new ones, so their
// ledger cost must not grow with the payload.
func TestTypes_ConversionViewsAreZeroByte(t *testing.T) {
	env := setupEnv(t)
	big := strings.Repeat("x", 4096)

	for _, tt := range []struct {
		builtin     string
		small, wide core.Value
	}{
		{"str->keyword", core.String{V: "x"}, core.String{V: big}},
		{"keyword->str", core.Keyword{V: "x"}, core.Keyword{V: big}},
	} {
		t.Run(tt.builtin, func(t *testing.T) {
			fn := collectionGoFunc(t, env, tt.builtin)
			small := numApplyAllocCharge(t, env, fn, tt.small)
			wide := numApplyAllocCharge(t, env, fn, tt.wide)
			require.Equalf(t, small, wide,
				"%s charged %d bytes for a 4096-byte payload and %d for a 1-byte one: a retyped view must cost the same either way",
				tt.builtin, wide, small)
		})
	}
}

// TestNumeric_ResultsStayUnmarked: an arithmetic result is a fresh scalar the
// apply site already bills through its shallow fallback. The migration must
// not mark it as callee-charged, or the scalar goes unbilled.
func TestNumeric_ResultsStayUnmarked(t *testing.T) {
	env := setupEnv(t)
	unmarked := core.GoFunc{
		Name: "unmarked-control",
		Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
			return core.BoxInt(3), nil
		},
	}
	args := []core.Value{core.Int{V: 1}, core.Int{V: 2}}

	want := numApplyAllocCharge(t, env, unmarked, args...)
	require.Positivef(t, want, "control dispatch charged %d bytes: the apply-site shallow fallback is not firing at all", want)

	got := numApplyAllocCharge(t, env, collectionGoFunc(t, env, "+"), args...)
	require.Equalf(t, want, got,
		"(+ 1 2) charged %d bytes, want %d: its Int result must stay unmarked so the apply-site shallow fallback still bills it", got, want)
}
