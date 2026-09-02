package stdlib

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/runtime"
)

// hoAssertCap is the rune precision the two assert message sites carry. The
// emitted message is at most len("assertion failed: ") + hoAssertCap 4-byte
// runes, which is the only quantity either site bounds: the render behind the
// %v site still runs String() to completion before fmt applies precision.
const hoAssertCap = 200

// hoMaxAssertMessage is that ceiling in bytes.
const hoMaxAssertMessage = len("assertion failed: ") + hoAssertCap*4

// hoWideRune is four bytes wide, so a message capped by a rune precision
// reaches its byte ceiling instead of a quarter of it.
const hoWideRune = "\U0001D6FC"

// hoBorrowedCallback hands back payload and marks it borrowed, so each
// re-entry adds zero bytes to the ledger. That leaves the calling builtin's
// own result disposition as the only payload-sized quantity in the total.
func hoBorrowedCallback(payload core.Value) core.GoFunc {
	return core.GoFunc{
		Name: "cb",
		Fn: func(ctx context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			if err := core.ChargeGoFuncResultBytes(ctx, 0); err != nil {
				return nil, err
			}
			return payload, nil
		},
	}
}

// hoTruthyCallback is the cheapest callback that keeps every filter element
// and folds without allocating, so an interruption test measures the calling
// builtin's own accounting rather than the callback's.
func hoTruthyCallback() core.GoFunc {
	return core.GoFunc{
		Name: "cb",
		Fn: func(_ context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			return core.Bool{V: true}, nil
		},
	}
}

// hoInterruptionArms is every higher-order builtin whose cost grows with its
// input, over an input past the budget's 128-unit batch interval.
func hoInterruptionArms() []struct {
	name string
	args []core.Value
} {
	items := core.NewList(cbInts(cbUnitCount))
	cb := hoTruthyCallback()
	return []struct {
		name string
		args []core.Value
	}{
		{"map", []core.Value{cb, items}},
		{"filter", []core.Value{cb, items}},
		{"reduce", []core.Value{cb, items}},
		{"apply", []core.Value{cb, items}},
	}
}

// TestHigherOrder_CallbackResultNotChargedTwice: BeginGoFuncDispatch clears the
// callee-charged marker and EndGoFuncDispatch restores the saved value, so a
// callback's own disposition never survives its dispatch. A builtin that hands
// back what a callback returned therefore reaches its apply site unmarked, and
// the fallback charges the value's shallow size a second time. Both builtins
// here return a callback result verbatim, so the ledger must not move when the
// callback's payload grows 4096-fold.
func TestHigherOrder_CallbackResultNotChargedTwice(t *testing.T) {
	env := setupEnv(t)
	payloadShallow := core.StringShallowBytes(cbWideLen)

	arms := []struct {
		name    string
		builtin string
		args    func(cb core.Value) []core.Value
	}{
		{"reduce final accumulator", "reduce", func(cb core.Value) []core.Value {
			return []core.Value{cb, core.Int{V: 0}, core.NewList([]core.Value{core.Int{V: 1}})}
		}},
		{"apply callee result", "apply", func(cb core.Value) []core.Value {
			return []core.Value{cb, core.Int{V: 1}, core.NewList([]core.Value{core.Int{V: 2}})}
		}},
	}

	for _, arm := range arms {
		t.Run(arm.name, func(t *testing.T) {
			fn := collectionGoFunc(t, env, arm.builtin)

			tiny, err := cbApplyCharge(t, env, fn, 1<<30, arm.args(hoBorrowedCallback(lbPayload(1)))...)
			require.NoError(t, err)
			wide, err := cbApplyCharge(t, env, fn, 1<<30, arm.args(hoBorrowedCallback(lbPayload(cbWideLen)))...)
			require.NoError(t, err)

			require.Equalf(t, tiny, wide,
				"%s over a %d-byte callback result charged %d bytes against %d for a 1-byte one: a result the callback already accounted for must add zero bytes at the calling builtin's apply site",
				arm.builtin, cbWideLen, wide, tiny)

			tight := int(tiny + payloadShallow/2)
			_, err = cbApplyCharge(t, env, fn, tight, arm.args(hoBorrowedCallback(lbPayload(cbWideLen)))...)
			require.NoErrorf(t, err,
				"%s: a callback-accounted result must not trip a %d-byte budget, tighter than the %d-byte shallow size it must never re-charge",
				arm.builtin, tight, payloadShallow)
		})
	}
}

// TestHigherOrder_FreshContainerChargedOnce: both builtins allocate one List
// over elements they borrowed, so the whole call must cost exactly that
// container and nothing else. Widening every element 4096-fold must not move
// the total, and neither may a second charge on top of the first.
//
// The expected total is exact because nothing else on this path charges bytes:
// the callback marks its own result borrowed, and the subject is built before
// the measured window opens.
func TestHigherOrder_FreshContainerChargedOnce(t *testing.T) {
	env := setupEnv(t)
	const n = 16
	want := core.ListShallowBytes(n)

	subject := func(width int) (core.Value, core.Value) {
		p := lbPayload(width)
		items := make([]core.Value, n)
		for i := range items {
			items[i] = p
		}
		return core.NewList(items), p
	}

	for _, builtin := range []string{"map", "filter"} {
		t.Run(builtin, func(t *testing.T) {
			fn := collectionGoFunc(t, env, builtin)

			for _, width := range []int{1, cbWideLen} {
				coll, p := subject(width)
				got, err := cbApplyCharge(t, env, fn, 1<<30, hoBorrowedCallback(p), coll)
				require.NoError(t, err)
				require.Equalf(t, want, got,
					"%s over %d elements of %d bytes each charged %d bytes, want exactly %d: the result is one fresh container over borrowed elements, charged once and never scaled by element size",
					builtin, n, width, got, want)
			}
		})
	}
}

// TestHigherOrder_TerminalUnderLowReductions: every higher-order builtin whose
// cost grows with its input must charge that work, so a long call under a
// ceiling below one batch ends terminally instead of running to completion
// unmetered.
func TestHigherOrder_TerminalUnderLowReductions(t *testing.T) {
	env := setupEnv(t)
	for _, arm := range hoInterruptionArms() {
		t.Run(arm.name, func(t *testing.T) {
			fn := collectionGoFunc(t, env, arm.name)
			ctx := core.WithEvalResourceLimits(t.Context(), 100, 1<<30)
			_, err := fn.Fn(ctx, core.NewEvaluator(), arm.args, env)
			requireResourceLimit(t, err)
			require.Truef(t, core.IsTerminalEvalError(err),
				"%s over a %d-unit input under a 100-reduction ceiling must fail terminally, got %v", arm.name, cbUnitCount, err)
		})
	}
}

// TestHigherOrder_ExpiredDeadline: the engine-owned deadline is observed at the
// budget's sync point, so a long call surfaces context.DeadlineExceeded even
// though its own parent context is still live.
func TestHigherOrder_ExpiredDeadline(t *testing.T) {
	env := setupEnv(t)
	for _, arm := range hoInterruptionArms() {
		t.Run(arm.name, func(t *testing.T) {
			fn := collectionGoFunc(t, env, arm.name)
			ctx := core.WithEvalResourceLimits(t.Context(), 1_000_000, 1<<30)
			ctx = core.WithEvalDeadline(ctx, time.Now().Add(-time.Millisecond))
			_, err := fn.Fn(ctx, core.NewEvaluator(), arm.args, env)
			require.ErrorIsf(t, err, context.DeadlineExceeded,
				"%s over a %d-unit input past the engine deadline must surface DeadlineExceeded, got %v", arm.name, cbUnitCount, err)
		})
	}
}

// TestHigherOrder_Cancellation: caller cancellation is observed at the same
// sync point, so a long call cannot outlive the context that started it.
func TestHigherOrder_Cancellation(t *testing.T) {
	env := setupEnv(t)
	for _, arm := range hoInterruptionArms() {
		t.Run(arm.name, func(t *testing.T) {
			fn := collectionGoFunc(t, env, arm.name)
			parent, cancel := context.WithCancel(context.Background())
			ctx := core.WithEvalResourceLimits(parent, 1_000_000, 1<<30)
			cancel()
			_, err := fn.Fn(ctx, core.NewEvaluator(), arm.args, env)
			require.ErrorIsf(t, err, context.Canceled,
				"%s over a %d-unit input under a cancelled caller must surface Canceled, got %v", arm.name, cbUnitCount, err)
		})
	}
}

// TestHigherOrder_CallbackCountUnchanged: budgeting the input copy and the
// per-element step must not add, drop or reorder a single callback dispatch.
// The counts are exact, because a bound would not notice one extra call.
func TestHigherOrder_CallbackCountUnchanged(t *testing.T) {
	env := setupEnv(t)
	ev := core.NewEvaluator()
	items := core.NewList(cbInts(5))

	calls := 0
	cb := core.GoFunc{
		Name: "cb",
		Fn: func(_ context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			calls++
			return core.Bool{V: true}, nil
		},
	}

	cases := []struct {
		name    string
		builtin string
		args    []core.Value
		want    int
	}{
		{"map", "map", []core.Value{cb, items}, 5},
		{"filter", "filter", []core.Value{cb, items}, 5},
		{"reduce seeded", "reduce", []core.Value{cb, core.Int{V: 0}, items}, 5},
		{"reduce unseeded", "reduce", []core.Value{cb, items}, 4},
		{"apply", "apply", []core.Value{cb, items}, 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls = 0
			fn := collectionGoFunc(t, env, tc.builtin)
			ctx := core.WithEvalResourceLimits(context.Background(), 1<<20, 1<<30)
			_, err := fn.Fn(ctx, ev, tc.args, env)
			require.NoError(t, err)
			require.Equalf(t, tc.want, calls,
				"%s over 5 elements dispatched its callback %d times, want exactly %d", tc.builtin, calls, tc.want)
		})
	}

	t.Run("reduce empty unseeded", func(t *testing.T) {
		calls = 0
		fn := collectionGoFunc(t, env, "reduce")
		ctx := core.WithEvalResourceLimits(context.Background(), 1<<20, 1<<30)
		got, err := fn.Fn(ctx, ev, []core.Value{cb, core.NewList(nil)}, env)
		require.NoError(t, err)
		require.Equal(t, 0, calls, "reduce over an empty collection with no init must not dispatch its callback")
		require.Equalf(t, core.Nil{}, got, "reduce over an empty collection with no init must return nil, got %v", got)
	})
}

// TestApply_DoesNotMutateCallerArguments: apply assembles its call arguments
// with append over a reslice of its own args, which keeps the original
// capacity and writes past the reslice's length. Under the VM args is a window
// into the value stack, so those writes land in slots the frame still owns.
// The assembly must copy at exact capacity instead, leaving every argument the
// caller evaluated — and the last-argument collection among them — as it was.
func TestApply_DoesNotMutateCallerArguments(t *testing.T) {
	a := core.Int{V: 1}
	tailItems := []core.Value{core.Int{V: 2}, core.Int{V: 3}}
	wantCallArgs := []core.Value{a, core.Int{V: 2}, core.Int{V: 3}}

	t.Run("stack window", func(t *testing.T) {
		env := setupEnv(t)
		fn := collectionGoFunc(t, env, "apply")

		var got []core.Value
		callee := core.GoFunc{
			Name: "f",
			Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
				got = slices.Clone(args)
				return core.Nil{}, nil
			},
		}

		// The shape the VM hands a builtin: len is the argument count, cap
		// runs on into stack slots the frame has not released.
		sentinel := core.Keyword{V: "stack-slot"}
		stack := []core.Value{callee, a, core.NewList(tailItems), sentinel, sentinel, sentinel}
		want := slices.Clone(stack)

		ctx := core.WithEvalResourceLimits(context.Background(), 1<<20, 1<<30)
		_, err := fn.Fn(ctx, core.NewEvaluator(), stack[:3], env)
		require.NoError(t, err)

		require.Equal(t, wantCallArgs, got, "apply must pass the leading arguments followed by the last collection's elements")
		require.Equal(t, want, stack,
			"apply overwrote its caller's argument window: assembling call arguments must copy at exact capacity, never append into slots the caller still owns")
	})

	modes := []struct {
		name string
		opt  runtime.EngineOption
	}{
		{"tree-walker", runtime.WithTreeWalker()},
		{"vm", runtime.WithBytecode()},
	}
	for _, mode := range modes {
		t.Run(mode.name, func(t *testing.T) {
			eng, err := runtime.New(nil, runtime.WithDialect(clojure.Dialect()), mode.opt)
			require.NoError(t, err)
			t.Cleanup(func() { _ = eng.Close() })
			require.NoError(t, eng.Use(New()))

			var got []core.Value
			require.NoError(t, eng.Bind("f", core.GoFunc{
				Name: "f",
				Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
					got = slices.Clone(args)
					return core.Nil{}, nil
				},
			}))
			require.NoError(t, eng.Bind("a", a))
			require.NoError(t, eng.Bind("coll", core.NewList(tailItems)))

			wantColl := core.NewList(slices.Clone(tailItems))

			_, err = eng.Eval(context.Background(), "apply-aliasing", "(apply f a coll)")
			require.NoError(t, err)
			require.Equal(t, wantCallArgs, got, "apply must pass the leading arguments followed by the last collection's elements")

			gotA, ok := eng.RootEnv().Get("a")
			require.True(t, ok)
			require.Truef(t, gotA.Equals(a), "apply changed the evaluated argument a: got %v, want %v", gotA, a)

			gotColl, ok := eng.RootEnv().Get("coll")
			require.True(t, ok)
			require.Truef(t, gotColl.Equals(wantColl), "apply changed its last-argument collection: got %v, want %v", gotColl, wantColl)
		})
	}
}

// TestAssert_MessageBoundedAndUnchanged: both assert failure sites render a
// user-supplied operand, and neither caps what it emits today. The cap is a
// rune precision, so it must leave every in-bounds wording byte-identical and
// hold the emitted message under its byte ceiling for any operand.
//
// The %v site caps the emitted message only. The render behind it runs
// String() to completion before fmt applies precision, so nothing here claims
// that work is bounded.
func TestAssert_MessageBoundedAndUnchanged(t *testing.T) {
	env := setupEnv(t)
	fn := collectionGoFunc(t, env, "assert")
	ev := core.NewEvaluator()

	call := func(args ...core.Value) (core.Value, error) {
		ctx := core.WithEvalResourceLimits(context.Background(), 1<<20, 1<<30)
		return fn.Fn(ctx, ev, args, env)
	}

	// The wording under test is the error's own message; Error() prefixes the
	// code, which is not what either site formats.
	message := func(t *testing.T, err error) string {
		t.Helper()
		var lerr *core.LispicoError
		require.ErrorAs(t, err, &lerr)
		return lerr.Message
	}

	t.Run("success returns nil", func(t *testing.T) {
		got, err := call(core.Bool{V: true})
		require.NoError(t, err)
		require.Equalf(t, core.Nil{}, got, "a passing assert must return nil, got %v", got)
	})

	t.Run("no message wording unchanged", func(t *testing.T) {
		_, err := call(core.Bool{V: false})
		require.Equal(t, "assertion failed", message(t, err))
	})

	t.Run("string operand at the cap unchanged", func(t *testing.T) {
		s := strings.Repeat("x", hoAssertCap)
		_, err := call(core.Bool{V: false}, core.String{V: s})
		require.Equalf(t, "assertion failed: "+s, message(t, err),
			"an operand of %d runes is in bounds: its wording must stay byte-identical", hoAssertCap)
	})

	t.Run("string operand beyond the cap is bounded", func(t *testing.T) {
		s := strings.Repeat(hoWideRune, 1000)
		_, err := call(core.Bool{V: false}, core.String{V: s})
		got := message(t, err)
		require.Equalf(t, "assertion failed: "+string([]rune(s)[:hoAssertCap]), got,
			"a %d-rune string operand must be emitted truncated to its first %d runes", len([]rune(s)), hoAssertCap)
		require.LessOrEqualf(t, len(got), hoMaxAssertMessage,
			"a %d-byte string operand emitted a %d-byte message, ceiling is %d", len(s), len(got), hoMaxAssertMessage)
	})

	t.Run("non-string operand beyond the cap is bounded", func(t *testing.T) {
		k := core.Keyword{V: strings.Repeat(hoWideRune, 1000)}
		_, err := call(core.Bool{V: false}, k)
		got := message(t, err)
		require.Equalf(t, "assertion failed: "+string([]rune(k.String())[:hoAssertCap]), got,
			"a %d-rune non-string operand must be emitted truncated to its first %d runes", len([]rune(k.String())), hoAssertCap)
		require.LessOrEqualf(t, len(got), hoMaxAssertMessage,
			"a %d-byte keyword operand emitted a %d-byte message, ceiling is %d", len(k.V), len(got), hoMaxAssertMessage)
	})
}
