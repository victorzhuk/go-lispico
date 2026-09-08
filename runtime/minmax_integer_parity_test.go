package runtime

import (
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/victorzhuk/go-lispico/core"
)

// minMaxParityValueCase drives one min/max invocation through both public
// entry points: Call with core.Value arguments and Eval with the equivalent
// source text.
type minMaxParityValueCase struct {
	name string
	fn   string
	args []core.Value
	src  string
	want core.Value
}

var minMaxParityValueCases = []minMaxParityValueCase{
	{
		name: "max singleton int64 max",
		fn:   "max",
		args: []core.Value{core.Int{V: math.MaxInt64}},
		src:  "(max 9223372036854775807)",
		want: core.Int{V: math.MaxInt64},
	},
	{
		name: "min singleton int64 min",
		fn:   "min",
		args: []core.Value{core.Int{V: math.MinInt64}},
		src:  "(min -9223372036854775808)",
		want: core.Int{V: math.MinInt64},
	},
	{
		name: "max adjacent above 2^53 ascending",
		fn:   "max",
		args: []core.Value{core.Int{V: 9007199254740992}, core.Int{V: 9007199254740993}},
		src:  "(max 9007199254740992 9007199254740993)",
		want: core.Int{V: 9007199254740993},
	},
	{
		name: "max adjacent above 2^53 descending",
		fn:   "max",
		args: []core.Value{core.Int{V: 9007199254740993}, core.Int{V: 9007199254740992}},
		src:  "(max 9007199254740993 9007199254740992)",
		want: core.Int{V: 9007199254740993},
	},
	{
		name: "min adjacent above 2^53 ascending",
		fn:   "min",
		args: []core.Value{core.Int{V: 9007199254740992}, core.Int{V: 9007199254740993}},
		src:  "(min 9007199254740992 9007199254740993)",
		want: core.Int{V: 9007199254740992},
	},
	{
		name: "min adjacent above 2^53 descending",
		fn:   "min",
		args: []core.Value{core.Int{V: 9007199254740993}, core.Int{V: 9007199254740992}},
		src:  "(min 9007199254740993 9007199254740992)",
		want: core.Int{V: 9007199254740992},
	},
	{
		name: "min adjacent below negative 2^53",
		fn:   "min",
		args: []core.Value{core.Int{V: -9007199254740992}, core.Int{V: -9007199254740993}},
		src:  "(min -9007199254740992 -9007199254740993)",
		want: core.Int{V: -9007199254740993},
	},
	{
		name: "max adjacent below negative 2^53",
		fn:   "max",
		args: []core.Value{core.Int{V: -9007199254740993}, core.Int{V: -9007199254740992}},
		src:  "(max -9007199254740993 -9007199254740992)",
		want: core.Int{V: -9007199254740992},
	},
	{
		name: "max mixed sign endpoints",
		fn:   "max",
		args: []core.Value{core.Int{V: math.MinInt64}, core.Int{V: math.MaxInt64}},
		src:  "(max -9223372036854775808 9223372036854775807)",
		want: core.Int{V: math.MaxInt64},
	},
	{
		name: "min mixed sign endpoints",
		fn:   "min",
		args: []core.Value{core.Int{V: math.MinInt64}, core.Int{V: math.MaxInt64}},
		src:  "(min -9223372036854775808 9223372036854775807)",
		want: core.Int{V: math.MinInt64},
	},
	{
		name: "max float operand promotes",
		fn:   "max",
		args: []core.Value{core.Int{V: 2}, core.Float{V: 1.5}},
		src:  "(max 2 1.5)",
		want: core.Float{V: 2},
	},
	{
		name: "min float operand promotes",
		fn:   "min",
		args: []core.Value{core.Int{V: 1}, core.Float{V: 1.5}},
		src:  "(min 1 1.5)",
		want: core.Float{V: 1},
	},
}

// minMaxParityRefusalCase pins the typed refusals both entry points must
// surface unchanged.
type minMaxParityRefusalCase struct {
	name    string
	fn      string
	args    []core.Value
	src     string
	code    string
	message string
}

var minMaxParityRefusalCases = []minMaxParityRefusalCase{
	{
		name:    "max without arguments",
		fn:      "max",
		src:     "(max)",
		code:    "ArityError",
		message: "max: requires at least 1 argument",
	},
	{
		name:    "min without arguments",
		fn:      "min",
		src:     "(min)",
		code:    "ArityError",
		message: "min: requires at least 1 argument",
	},
	{
		name:    "max with a string operand",
		fn:      "max",
		args:    []core.Value{core.Int{V: 1}, core.String{V: "a"}},
		src:     `(max 1 "a")`,
		code:    "TypeError",
		message: "max: expected number, got core.String",
	},
}

// assertMinMaxValue reports rather than aborts, so a single run shows every
// dispatch arm that disagrees instead of stopping at the first one.
func assertMinMaxValue(t *testing.T, want, got core.Value, label string) {
	t.Helper()
	if !assert.NotNilf(t, got, "%s: nil result, want %v", label, want) {
		return
	}
	assert.Equalf(t, want.Type().V, got.Type().V, "%s: want type %v, got type %v", label, want.Type().V, got.Type().V)
	assert.Truef(t, got.Equals(want), "%s: want %v, got %v", label, want, got)
}

func assertMinMaxRefusal(t *testing.T, err error, code, message, label string) {
	t.Helper()
	if !assert.Errorf(t, err, "%s: want refusal %s", label, code) {
		return
	}
	var le *core.LispicoError
	if !assert.Truef(t, errors.As(err, &le), "%s: want *core.LispicoError, got %T", label, err) {
		return
	}
	assert.Equalf(t, code, le.Code, "%s: want code %s, got %s", label, code, le.Code)
	assert.Equalf(t, message, le.Message, "%s: want message %q, got %q", label, message, le.Message)
}

// TestMinMax_ExactIntegersAcrossDispatchModes pins min and max results at the
// Engine boundary across the tree-walker and the VM, through Call and Eval:
// integer extrema stay exact, a float operand keeps float promotion, and the
// arity and type refusals stay typed and identically worded on every path.
func TestMinMax_ExactIntegersAcrossDispatchModes(t *testing.T) {
	ctx := t.Context()

	for _, tc := range minMaxParityValueCases {
		t.Run(tc.name, func(t *testing.T) {
			byMode := make(map[string]core.Value, len(goldenEvaluatorModes))

			for _, mode := range goldenEvaluatorModes {
				eng := newReentrantCallDepthEngine(t, mode.opts...)

				called, err := eng.Call(ctx, tc.fn, tc.args...)
				if assert.NoErrorf(t, err, "%s/call", mode.name) {
					assertMinMaxValue(t, tc.want, called, mode.name+"/call")
					byMode[mode.name] = called
				}

				evaled, err := eng.Eval(ctx, "minmax", tc.src)
				if assert.NoErrorf(t, err, "%s/eval", mode.name) {
					assertMinMaxValue(t, tc.want, evaled, mode.name+"/eval")
				}
			}

			if byMode["tree-walker"] != nil && byMode["vm"] != nil {
				assertMinMaxValue(t, byMode["tree-walker"], byMode["vm"], "vm vs tree-walker")
			}
		})
	}

	for _, tc := range minMaxParityRefusalCases {
		t.Run(tc.name, func(t *testing.T) {
			for _, mode := range goldenEvaluatorModes {
				eng := newReentrantCallDepthEngine(t, mode.opts...)

				got, err := eng.Call(ctx, tc.fn, tc.args...)
				assert.Nilf(t, got, "%s/call: want nil value, got %v", mode.name, got)
				assertMinMaxRefusal(t, err, tc.code, tc.message, mode.name+"/call")

				got, err = eng.Eval(ctx, "minmax", tc.src)
				assert.Nilf(t, got, "%s/eval: want nil value, got %v", mode.name, got)
				assertMinMaxRefusal(t, err, tc.code, tc.message, mode.name+"/eval")
			}
		})
	}
}
