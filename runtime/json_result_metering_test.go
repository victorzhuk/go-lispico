package runtime

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

// jsonResultCase is one JSON payload whose decoded value a single
// json/decode dispatch must charge at exactly its deep byte size — no more
// (the dispatch fallback must not re-charge the root the callee already
// accounted for), no less (one byte below the deep budget fails closed).
type jsonResultCase struct {
	name string
	json string
	want core.Value
}

// jsonResultMeteringCases builds the expected values independently of any
// invocation under test: budgets in the tests below are derived from these
// via core.ValueDeepBytes/core.ValueShallowBytes, never from a decoded
// result.
func jsonResultMeteringCases(t *testing.T) []jsonResultCase {
	t.Helper()
	nested := core.NewHashMap()
	require.NoError(t, nested.Set(core.Keyword{V: "k"}, core.NewVector([]core.Value{core.Int{V: 1}, core.Int{V: 2}})))
	return []jsonResultCase{
		{name: "scalar", json: "42", want: core.Int{V: 42}},
		{name: "string", json: `"hi"`, want: core.String{V: "hi"}},
		{name: "empty-vector", json: "[]", want: core.NewVector(nil)},
		{name: "empty-object", json: "{}", want: core.NewHashMap()},
		{name: "nested", json: `{"k":[1,2]}`, want: nested},
	}
}

// jsonMeteredCtx seeds a caller-owned cumulative ledger on the context, the
// same fixture shape apply_pool_test uses: every charge of every later Call
// on the returned ctx accumulates against budget.
func jsonMeteredCtx(t *testing.T, budget int64) context.Context {
	t.Helper()
	ctx, _, _ := core.AdoptEvalStateWithMeter(context.Background(), time.Time{}, 0,
		core.EvalMeterSnapshot{MaxReductions: 1 << 20, MaxAllocationBytes: budget})
	return ctx
}

// assertJSONRefused asserts a terminal resource-limit refusal: a
// *core.LispicoError classified under core.CodeResourceLimit and no value
// published alongside it. Error wording is deliberately not asserted — the
// two modes may phrase refusals differently.
func assertJSONRefused(t *testing.T, label string, got core.Value, err error) {
	t.Helper()
	if assert.Errorf(t, err, "%s: want a refusal, got value %v and no error", label, got) {
		var lerr *core.LispicoError
		if assert.Truef(t, errors.As(err, &lerr), "%s: refusal must be a *core.LispicoError, got %T: %v", label, err, err) {
			assert.Equalf(t, core.CodeResourceLimit, lerr.Code, "%s: refusal must classify under %s, got %s", label, core.CodeResourceLimit, lerr.Code)
		}
	}
	assert.Nilf(t, got, "%s: want nil value alongside the refusal, got %v", label, got)
}

// TestJSON_DecodeChargesExactDeepBytesAcrossDispatchModes pins that one
// json/decode dispatch charges its result exactly once, at the full deep
// byte size of the decoded value: under MaxAllocationBytes == deep the call
// succeeds in both evaluator modes, for scalars, strings, empty containers,
// and nested structures. A dispatch fallback that re-charges the root on top
// of the callee's own deep charge refuses this exact budget.
func TestJSON_DecodeChargesExactDeepBytesAcrossDispatchModes(t *testing.T) {
	for _, tc := range jsonResultMeteringCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			deep := core.ValueDeepBytes(tc.want)
			for _, mode := range goldenEvaluatorModes {
				t.Run(mode.name, func(t *testing.T) {
					opts := append(append([]EngineOption{}, mode.opts...),
						WithResourceLimits(ResourceLimits{MaxAllocationBytes: int(deep)}))
					eng := newJSONParityEngine(t, opts...)

					got, err := eng.Call(t.Context(), "json/decode", core.String{V: tc.json})
					if assert.NoErrorf(t, err,
						"%s: json/decode of %s under an exact deep budget of %d bytes must succeed — one dispatch must charge the result once, not twice",
						mode.name, tc.json, deep) {
						assertJSONParityValue(t, tc.want, got, mode.name+"/exact-deep-budget")
					}
				})
			}
		})
	}
}

// TestJSON_DecodeOneByteBelowBudgetFailsClosed pins the terminal side of the
// same budget: MaxAllocationBytes == deep-1 refuses json/decode in both
// modes with a *core.LispicoError under core.CodeResourceLimit and no value.
// This holds before and after the duplicate root charge is removed.
func TestJSON_DecodeOneByteBelowBudgetFailsClosed(t *testing.T) {
	for _, tc := range jsonResultMeteringCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			deep := core.ValueDeepBytes(tc.want)
			for _, mode := range goldenEvaluatorModes {
				t.Run(mode.name, func(t *testing.T) {
					opts := append(append([]EngineOption{}, mode.opts...),
						WithResourceLimits(ResourceLimits{MaxAllocationBytes: int(deep - 1)}))
					eng := newJSONParityEngine(t, opts...)

					got, err := eng.Call(t.Context(), "json/decode", core.String{V: tc.json})
					assertJSONRefused(t, fmt.Sprintf(
						"%s/deep-minus-one: json/decode of %s under a %d-byte budget (deep is %d)",
						mode.name, tc.json, deep-1, deep), got, err)
				})
			}
		})
	}
}

// TestJSON_DecodeLaterDispatchesKeepCharges proves the decode result charge
// does not leak past its own dispatch, on a caller-owned cumulative ledger:
// two decodes of "42" fit exactly 2*deep (the second is refused one byte
// short), and a later json/encode — which returns its result without
// self-charging — still bills its own shallow bytes on top of the decode
// (deep + shallow fits, one byte short refuses). All on fresh engines and
// fresh ledgers per cell, in both evaluator modes.
func TestJSON_DecodeLaterDispatchesKeepCharges(t *testing.T) {
	const payload = "42"
	wantDecoded := core.Int{V: 42}
	deep := core.ValueDeepBytes(wantDecoded)
	wantEncoded := core.String{V: payload}
	encodeShallow := core.ValueShallowBytes(wantEncoded)

	for _, mode := range goldenEvaluatorModes {
		t.Run(mode.name, func(t *testing.T) {
			t.Run("two decodes at twice deep", func(t *testing.T) {
				eng := newJSONParityEngine(t, mode.opts...)
				ctx := jsonMeteredCtx(t, 2*deep)

				first, err := eng.Call(ctx, "json/decode", core.String{V: payload})
				require.NoErrorf(t, err, "%s: first decode under a %d-byte ledger must succeed", mode.name, 2*deep)
				assertJSONParityValue(t, wantDecoded, first, mode.name+"/cumulative-first")

				second, err := eng.Call(ctx, "json/decode", core.String{V: payload})
				if assert.NoErrorf(t, err,
					"%s: second decode under a %d-byte ledger (%d per dispatch) must succeed — each dispatch must charge its own deep exactly once",
					mode.name, 2*deep, deep) {
					assertJSONParityValue(t, wantDecoded, second, mode.name+"/cumulative-second")
				}
			})

			t.Run("second decode refused one byte short", func(t *testing.T) {
				eng := newJSONParityEngine(t, mode.opts...)
				ctx := jsonMeteredCtx(t, 2*deep-1)

				_, err := eng.Call(ctx, "json/decode", core.String{V: payload})
				require.NoErrorf(t, err, "%s: first decode under a %d-byte ledger (%d needed) must succeed", mode.name, 2*deep-1, deep)

				got, err := eng.Call(ctx, "json/decode", core.String{V: payload})
				assertJSONRefused(t, fmt.Sprintf(
					"%s/cumulative-second: second decode under a %d-byte ledger after a first deep charge of %d",
					mode.name, 2*deep-1, deep), got, err)
			})

			t.Run("decode then encode at deep plus encode shallow", func(t *testing.T) {
				eng := newJSONParityEngine(t, mode.opts...)
				ctx := jsonMeteredCtx(t, deep+encodeShallow)

				decoded, err := eng.Call(ctx, "json/decode", core.String{V: payload})
				require.NoErrorf(t, err, "%s: decode under a %d-byte ledger must succeed", mode.name, deep+encodeShallow)

				encoded, err := eng.Call(ctx, "json/encode", decoded)
				if assert.NoErrorf(t, err,
					"%s: json/encode after decode under a %d-byte ledger (decode deep %d + encode shallow %d) must succeed — a later dispatch must still charge its own result",
					mode.name, deep+encodeShallow, deep, encodeShallow) {
					assertJSONParityValue(t, wantEncoded, encoded, mode.name+"/encode-after-decode")
				}
			})

			t.Run("encode refused one byte short", func(t *testing.T) {
				eng := newJSONParityEngine(t, mode.opts...)
				ctx := jsonMeteredCtx(t, deep+encodeShallow-1)

				decoded, err := eng.Call(ctx, "json/decode", core.String{V: payload})
				require.NoErrorf(t, err, "%s: decode under a %d-byte ledger (%d needed) must succeed", mode.name, deep+encodeShallow-1, deep)

				got, err := eng.Call(ctx, "json/encode", decoded)
				assertJSONRefused(t, fmt.Sprintf(
					"%s/encode-after-decode: json/encode under a %d-byte ledger (decode deep %d + encode shallow %d)",
					mode.name, deep+encodeShallow-1, deep, encodeShallow), got, err)
			})
		})
	}
}
