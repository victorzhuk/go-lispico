package runtime

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/json"
)

// newJSONParityEngine builds a fresh engine under the given evaluator mode
// with an explicit clojure dialect and the json plugin loaded. stdlib is
// deliberately absent: every fixture below is self-contained in
// json/encode and json/decode. Each call site gets its own engine so the
// two modes (and the subtests) never share dispatch state.
func newJSONParityEngine(t *testing.T, opts ...EngineOption) Engine {
	t.Helper()
	opts = append([]EngineOption{WithDialect(clojure.Dialect())}, opts...)
	eng, err := New(nil, opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	require.NoError(t, eng.Use(json.New()))
	return eng
}

// jsonParityValueCase is one JSON number payload driven through both public
// entry points — Call with the JSON text as a core.String and Eval with the
// equivalent source — under both evaluator modes.
type jsonParityValueCase struct {
	name string
	json string
	want core.Value
}

// jsonParityValueCases covers the int64 endpoints, the 2^53 pair, and the
// first integer past int64: everything that fits int64 stays an exact
// core.Int, the one just outside falls back to core.Float.
var jsonParityValueCases = []jsonParityValueCase{
	{name: "int64 max stays exact int", json: "9223372036854775807", want: core.Int{V: math.MaxInt64}},
	{name: "int64 min stays exact int", json: "-9223372036854775808", want: core.Int{V: math.MinInt64}},
	{name: "two to the 53rd stays exact int", json: "9007199254740992", want: core.Int{V: 9007199254740992}},
	{name: "two to the 53rd plus one stays exact int", json: "9007199254740993", want: core.Int{V: 9007199254740993}},
	{name: "int64 max plus one falls back to float", json: "9223372036854775808", want: core.Float{V: 9223372036854775808}},
}

// jsonParitySrc renders the Eval-arm source for a payload.
func jsonParitySrc(payload string) string {
	return fmt.Sprintf("(json/decode %q)", payload)
}

// assertJSONParityValue reports rather than aborts, so a single run shows
// every dispatch arm that disagrees instead of stopping at the first one.
func assertJSONParityValue(t *testing.T, want, got core.Value, label string) {
	t.Helper()
	if !assert.NotNilf(t, got, "%s: nil result, want %v", label, want) {
		return
	}
	assert.Equalf(t, want.Type().V, got.Type().V, "%s: want type %v, got type %v", label, want.Type().V, got.Type().V)
	assert.Truef(t, got.Equals(want), "%s: want %v, got %v", label, want, got)
}

// TestJSON_ExactIntegersAcrossDispatchModes pins json/decode numeric
// results at the Engine boundary across the tree-walker and the VM, through
// Call and Eval: every integer that fits int64 decodes to an exact core.Int
// (the int64 endpoints and the 2^53 pair included), the first integer past
// int64 falls back to core.Float, an overflowing exponent refuses on every
// path, and an encode/decode round trip preserves the exact integer — in
// both modes, at both dispatch boundaries.
func TestJSON_ExactIntegersAcrossDispatchModes(t *testing.T) {
	ctx := t.Context()

	for _, tc := range jsonParityValueCases {
		t.Run(tc.name, func(t *testing.T) {
			src := jsonParitySrc(tc.json)
			byMode := make(map[string]core.Value, len(goldenEvaluatorModes))

			for _, mode := range goldenEvaluatorModes {
				eng := newJSONParityEngine(t, mode.opts...)

				called, err := eng.Call(ctx, "json/decode", core.String{V: tc.json})
				if assert.NoErrorf(t, err, "%s/call: json/decode of %s must not fail", mode.name, tc.json) {
					assertJSONParityValue(t, tc.want, called, mode.name+"/call")
					byMode[mode.name] = called
				}

				evaled, err := eng.Eval(ctx, "json-parity", src)
				if assert.NoErrorf(t, err, "%s/eval: %s must not fail", mode.name, src) {
					assertJSONParityValue(t, tc.want, evaled, mode.name+"/eval")
				}
			}

			if byMode["tree-walker"] != nil && byMode["vm"] != nil {
				assertJSONParityValue(t, byMode["tree-walker"], byMode["vm"], "vm vs tree-walker")
			}
		})
	}

	t.Run("encode-decode round trip two to the 53rd plus one", func(t *testing.T) {
		want := core.Int{V: 9007199254740993}
		byMode := make(map[string]core.Value, len(goldenEvaluatorModes))

		for _, mode := range goldenEvaluatorModes {
			eng := newJSONParityEngine(t, mode.opts...)

			evaled, err := eng.Eval(ctx, "json-parity", `(json/decode (json/encode 9007199254740993))`)
			if assert.NoErrorf(t, err, "%s/eval: encode-decode round trip must not fail", mode.name) {
				assertJSONParityValue(t, want, evaled, mode.name+"/eval")
			}

			encoded, err := eng.Call(ctx, "json/encode", want)
			if assert.NoErrorf(t, err, "%s/call: json/encode of %v must not fail", mode.name, want) {
				decoded, err := eng.Call(ctx, "json/decode", encoded)
				if assert.NoErrorf(t, err, "%s/call: json/decode of the encoded %v must not fail", mode.name, encoded) {
					assertJSONParityValue(t, want, decoded, mode.name+"/call")
					byMode[mode.name] = decoded
				}
			}
		}

		if byMode["tree-walker"] != nil && byMode["vm"] != nil {
			assertJSONParityValue(t, byMode["tree-walker"], byMode["vm"], "vm vs tree-walker")
		}
	})

	// The spec promises both modes refuse "1e400" with an equivalent
	// conversion error, not identically worded messages: assert each arm
	// errors, and never compare error strings across modes.
	t.Run("exponent overflow refuses in every mode", func(t *testing.T) {
		for _, mode := range goldenEvaluatorModes {
			eng := newJSONParityEngine(t, mode.opts...)

			got, err := eng.Eval(ctx, "json-parity", `(json/decode "1e400")`)
			assert.Errorf(t, err, "%s/eval: json/decode of 1e400 must refuse with a conversion error", mode.name)
			assert.Nilf(t, got, "%s/eval: want nil value alongside the refusal, got %v", mode.name, got)

			got, err = eng.Call(ctx, "json/decode", core.String{V: "1e400"})
			assert.Errorf(t, err, "%s/call: json/decode of 1e400 must refuse with a conversion error", mode.name)
			assert.Nilf(t, got, "%s/call: want nil value alongside the refusal, got %v", mode.name, got)
		}
	})
}
