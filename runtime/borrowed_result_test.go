package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

// borrowedResultTestShared: a large string whose shallow size is the
// charge a correct implementation must NOT apply to a wholly borrowed
// GoFunc result (ChargeGoFuncResultBytes(ctx, 0)).
const borrowedLen = 4096

func borrowedPayload() core.String {
	return core.String{V: strings.Repeat("x", borrowedLen)}
}

func borrowedShallowBytes() int64 {
	return core.StringShallowBytes(borrowedLen)
}

// borrowedUsage evaluates "(f)" on a fresh engine in the given mode, where
// f is bound per variant, and returns the alloc ledger total. Identical
// engine configuration across variants makes the difference between two
// variants attributable solely to the GoFunc's charging behavior.
func borrowedUsage(t *testing.T, bytecode bool, fn core.GoFunc) int64 {
	t.Helper()
	eng := newMeteringStdlibEngine(t, bytecode, meteringLimits(t, 1_000_000, 16<<20))
	require.NoError(t, eng.Bind("f", fn))
	ctx := core.WithEvalResourceLimits(t.Context(), 1_000_000, 16<<20)
	_, err := eng.Eval(ctx, "borrowed-usage", "(f)")
	require.NoError(t, err)
	return core.EvalMeterFrom(ctx).Snapshot().AllocationBytes
}

func borrowedGoFunc(charge bool) core.GoFunc {
	payload := borrowedPayload()
	return core.GoFunc{
		Fn: func(ctx context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			if charge {
				if err := core.ChargeGoFuncResultBytes(ctx, 0); err != nil {
					return nil, err
				}
			}
			return payload, nil
		},
	}
}

func TestChargeGoFuncResultBytes_ZeroByteBorrowed_Evaluator(t *testing.T) {
	skipUntilMeteringFields(t)

	const bytecode = false
	borrowed := borrowedUsage(t, bytecode, borrowedGoFunc(true))
	unmarked := borrowedUsage(t, bytecode, borrowedGoFunc(false))

	// Unmarked fallback: apply site charges ValueShallowBytes(result) once.
	assert.GreaterOrEqual(t, unmarked, borrowed+borrowedShallowBytes(),
		"unmarked GoFunc result must be charged the borrowed value's shallow bytes at the apply site (borrowed=%d unmarked=%d)", borrowed, unmarked)

	// Borrowed disposition: zero-byte charge, borrowed value's shallow size
	// never enters the ledger.
	assert.Less(t, borrowed, unmarked, "charge(ctx,0) must remove the apply-site fallback charge")

	// Tight budget between the two totals: borrowed run passes, unmarked trips.
	tight := int(borrowed + borrowedShallowBytes()/2)

	t.Run("borrowed_under_tight_budget", func(t *testing.T) {
		eng := newMeteringStdlibEngine(t, bytecode, meteringLimits(t, 1_000_000, tight))
		require.NoError(t, eng.Bind("f", borrowedGoFunc(true)))
		ctx := core.WithEvalResourceLimits(t.Context(), 1_000_000, tight)
		_, err := eng.Eval(ctx, "borrowed-tight", "(f)")
		require.NoError(t, err, "wholly borrowed result must not trip a budget tighter than its shallow size")
	})

	t.Run("fresh_unmarked_trips_tight_budget", func(t *testing.T) {
		eng := newMeteringStdlibEngine(t, bytecode, meteringLimits(t, 1_000_000, tight))
		require.NoError(t, eng.Bind("f", borrowedGoFunc(false)))
		ctx := core.WithEvalResourceLimits(t.Context(), 1_000_000, tight)
		_, err := eng.Eval(ctx, "unmarked-tight", "(f)")
		assert.True(t, isResourceLimit(t, err), "unmarked result's fallback shallow charge must trip the tight budget, got %v", err)
	})
}

func TestChargeGoFuncResultBytes_ZeroByteBorrowed_VM(t *testing.T) {
	skipUntilMeteringFields(t)

	const bytecode = true
	borrowed := borrowedUsage(t, bytecode, borrowedGoFunc(true))
	unmarked := borrowedUsage(t, bytecode, borrowedGoFunc(false))

	assert.GreaterOrEqual(t, unmarked, borrowed+borrowedShallowBytes(),
		"VM apply site must charge the unmarked result's shallow bytes once (borrowed=%d unmarked=%d)", borrowed, unmarked)

	tight := int(borrowed + borrowedShallowBytes()/2)
	eng := newMeteringStdlibEngine(t, bytecode, meteringLimits(t, 1_000_000, tight))
	require.NoError(t, eng.Bind("f", borrowedGoFunc(true)))
	ctx := core.WithEvalResourceLimits(t.Context(), 1_000_000, tight)
	_, err := eng.Eval(ctx, "vm-borrowed-tight", "(f)")
	require.NoError(t, err, "VM: wholly borrowed result must not trip a budget tighter than its shallow size")
}

func TestChargeGoFuncResultBytes_ZeroByteBorrowed_Reentry(t *testing.T) {
	skipUntilMeteringFields(t)

	const innerLen = 512
	innerShallow := core.StringShallowBytes(innerLen)
	inner := core.String{V: strings.Repeat("y", innerLen)}

	// outer re-enters evaluation (ev.Apply on the inner GoFunc) and then
	// returns the borrowed payload. callInner=false gives the baseline.
	outer := func(callInner bool, charge bool) core.GoFunc {
		payload := borrowedPayload()
		return core.GoFunc{
			Name: "f",
			Fn: func(ctx context.Context, ev core.Evaluator, _ []core.Value, env *core.Env) (core.Value, error) {
				if callInner {
					innerFn := core.GoFunc{
						Name: "inner-f",
						Fn: func(ctx context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
							if err := core.ChargeGoFuncResultBytes(ctx, innerShallow); err != nil {
								return nil, err
							}
							return inner, nil
						},
					}
					if _, err := ev.Apply(ctx, innerFn, nil, env); err != nil {
						return nil, err
					}
				}
				if charge {
					if err := core.ChargeGoFuncResultBytes(ctx, 0); err != nil {
						return nil, err
					}
				}
				return payload, nil
			},
		}
	}

	usage := func(t *testing.T, bytecode bool, callInner, charge bool) int64 {
		t.Helper()
		return borrowedUsage(t, bytecode, outer(callInner, charge))
	}

	for _, bytecode := range []bool{false, true} {
		t.Run(evalModeName(bytecode), func(t *testing.T) {
			base := usage(t, bytecode, false, true)
			withInner := usage(t, bytecode, true, true)

			// The inner charge is still billed exactly once; the outer
			// zero-byte disposition is untouched by it.
			assert.GreaterOrEqual(t, withInner, base+innerShallow,
				"inner re-entry charge must be counted (base=%d withInner=%d)", base, withInner)

			// An inner charge must NOT be mistaken for the outer callee
			// having charged its own result: the outer unmarked return
			// still falls back to the full shallow charge.
			unmarkedOuter := usage(t, bytecode, true, false)
			assert.GreaterOrEqual(t, unmarkedOuter, withInner+borrowedShallowBytes(),
				"inner charge must not suppress the outer dispatch's fallback (withInner=%d unmarkedOuter=%d)", withInner, unmarkedOuter)

			// Tight budget: borrowed outer with inner charge still passes.
			tight := int(withInner + borrowedShallowBytes()/2)
			eng := newMeteringStdlibEngine(t, bytecode, meteringLimits(t, 1_000_000, tight))
			require.NoError(t, eng.Bind("f", outer(true, true)))
			ctx := core.WithEvalResourceLimits(t.Context(), 1_000_000, tight)
			_, err := eng.Eval(ctx, "reentry-borrowed-tight", "(f)")
			require.NoError(t, err, "re-entry: wholly borrowed outer result must not trip a budget tighter than its shallow size")
		})
	}
}

func TestChargeGoFuncResultBytes_ZeroByteBorrowed_DirectApply(t *testing.T) {
	skipUntilMeteringFields(t)

	usage := func(charge bool) int64 {
		ev := core.NewEvaluator()
		env := core.NewEnv(nil)
		ctx := core.WithEvalResourceLimits(context.Background(), 1_000_000, 16<<20)
		payload := borrowedPayload()
		fn := core.GoFunc{
			Name: "f",
			Fn: func(ctx context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
				if charge {
					if err := core.ChargeGoFuncResultBytes(ctx, 0); err != nil {
						return nil, err
					}
				}
				return payload, nil
			},
		}
		_, err := ev.Apply(ctx, fn, nil, env)
		require.NoError(t, err)
		return core.EvalMeterFrom(ctx).Snapshot().AllocationBytes
	}

	borrowed := usage(true)
	unmarked := usage(false)

	assert.GreaterOrEqual(t, unmarked, borrowed+borrowedShallowBytes(),
		"direct core.Evaluator.Apply must charge the unmarked result's shallow bytes once (borrowed=%d unmarked=%d)", borrowed, unmarked)
}

func TestChargeGoFuncResultBytes_MixedIncrementalChargesFreshOnly(t *testing.T) {
	skipUntilMeteringFields(t)

	borrowed := borrowedPayload()

	// base: wholly borrowed string, zero-byte disposition.
	baseFn := core.GoFunc{
		Name: "f",
		Fn: func(ctx context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			if err := core.ChargeGoFuncResultBytes(ctx, 0); err != nil {
				return nil, err
			}
			return borrowed, nil
		},
	}

	// mixed: fresh vector sharing the borrowed string plus one fresh scalar;
	// the callee charges only the fresh scalar's bytes.
	freshDelta := core.StringShallowBytes(1)
	mixedFn := core.GoFunc{
		Name: "f",
		Fn: func(ctx context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			result := core.NewVector([]core.Value{borrowed, core.String{V: "z"}})
			if err := core.ChargeGoFuncResultBytes(ctx, freshDelta); err != nil {
				return nil, err
			}
			return result, nil
		},
	}

	for _, bytecode := range []bool{false, true} {
		t.Run(evalModeName(bytecode), func(t *testing.T) {
			base := borrowedUsage(t, bytecode, baseFn)
			mixed := borrowedUsage(t, bytecode, mixedFn)

			// Fresh bytes charged exactly once, and no fallback re-charge of
			// the result's shallow size on top of the callee's delta.
			assert.GreaterOrEqual(t, mixed, base+freshDelta,
				"mixed result's fresh bytes must be charged (base=%d mixed=%d)", base, mixed)
			assert.Less(t, mixed, base+freshDelta+core.VectorShallowBytes(2),
				"apply-site fallback must be skipped for a callee-charged mixed result (base=%d mixed=%d)", base, mixed)
			assert.Less(t, mixed, base+borrowedShallowBytes(),
				"borrowed substructure must not be re-charged for a mixed result (base=%d mixed=%d)", base, mixed)
		})
	}
}

// getUsage evaluates src against an engine holding a map whose :a entry is
// payload and a "d" binding of the same payload, returning the alloc ledger
// total. Nothing on this path scales with the payload's size except the
// apply-site fallback charge, so two runs differing only in payload size
// differ only by that charge.
func getUsage(t *testing.T, bytecode bool, payload core.Value, budget int, src string) (int64, error) {
	t.Helper()
	m := core.NewHashMap()
	require.NoError(t, m.Set(core.Keyword{V: "a"}, payload))

	eng := newMeteringStdlibEngine(t, bytecode, meteringLimits(t, 1_000_000, budget))
	require.NoError(t, eng.Bind("m", m))
	require.NoError(t, eng.Bind("d", payload))
	ctx := core.WithEvalResourceLimits(t.Context(), 1_000_000, budget)
	_, err := eng.Eval(ctx, "borrowed-get", src)
	return core.EvalMeterFrom(ctx).Snapshot().AllocationBytes, err
}

func TestBorrowed_GetReturnsStoredCollection(t *testing.T) {
	skipUntilMeteringFields(t)

	const src = `(get m :a)`

	for _, bytecode := range []bool{false, true} {
		t.Run(evalModeName(bytecode), func(t *testing.T) {
			tiny, err := getUsage(t, bytecode, core.String{V: "x"}, 16<<20, src)
			require.NoError(t, err)
			large, err := getUsage(t, bytecode, borrowedPayload(), 16<<20, src)
			require.NoError(t, err)

			assert.Equal(t, tiny, large,
				"a stored value returned as-is must add zero result bytes to the ledger (tiny=%d large=%d)", tiny, large)

			tight := int(tiny + borrowedShallowBytes()/2)
			_, err = getUsage(t, bytecode, borrowedPayload(), tight, src)
			require.NoError(t, err, "borrowed stored value must not trip a budget tighter than its shallow size")
		})
	}
}

func TestBorrowed_GetDefaultIsBorrowed(t *testing.T) {
	skipUntilMeteringFields(t)

	const src = `(get m :missing d)`

	for _, bytecode := range []bool{false, true} {
		t.Run(evalModeName(bytecode), func(t *testing.T) {
			tiny, err := getUsage(t, bytecode, core.String{V: "x"}, 16<<20, src)
			require.NoError(t, err)
			large, err := getUsage(t, bytecode, borrowedPayload(), 16<<20, src)
			require.NoError(t, err)

			assert.Equal(t, tiny, large,
				"a caller-supplied default returned as-is must add zero result bytes to the ledger (tiny=%d large=%d)", tiny, large)

			tight := int(tiny + borrowedShallowBytes()/2)
			_, err = getUsage(t, bytecode, borrowedPayload(), tight, src)
			require.NoError(t, err, "borrowed default must not trip a budget tighter than its shallow size")
		})
	}
}
