// Engine-level goldens for what assert reports about its own arguments. Each
// expectation is derived from the contract, never captured from a run: a
// failing assertion is domainErrorf("assertion failed: %.200s", rendered) with
// Code EvalError, and rendered is core.ValueStringContext of the message
// argument the apply site already produced.
//
// stdlibErrorGoldens cannot host these rows: it pins Code alone and runs under
// the Lisp-1 identity dialect only, while the contract here is the exact
// message under both execution modes and both dialects.
package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

// assertMessageGolden is one hand-derived reporting row. An empty code means
// src must succeed and yield core.Nil{}; otherwise src must fail with a
// *core.LispicoError carrying exactly code and msg.
type assertMessageGolden struct {
	name string
	src  string
	code string
	msg  string
}

var assertMessageGoldens = []assertMessageGolden{
	{
		name: "quoted-symbol-message",
		src:  "(assert false 'x)",
		code: "EvalError",
		msg:  "assertion failed: x",
	},
	{
		name: "constructed-list-message",
		src:  "(assert false (list 1 2))",
		code: "EvalError",
		msg:  "assertion failed: (1 2)",
	},
	{
		name: "quoted-list-message",
		src:  "(assert false '(1 2))",
		code: "EvalError",
		msg:  "assertion failed: (1 2)",
	},
	{
		name: "quoted-symbol-condition",
		src:  "(assert 'x)",
	},
	{
		name: "constructed-list-condition",
		src:  "(assert (list 1 2))",
	},
	// Control: an unquoted unbound name fails at the apply site, before assert
	// is entered, and must keep failing there rather than being reported as an
	// assertion failure.
	{
		name: "unbound-symbol-message",
		src:  "(assert false x)",
		code: "UndefinedError",
		msg:  "undefined: x",
	},
	// Control: the core.String fast path skips the render and is untouched.
	{
		name: "string-message",
		src:  `(assert false "boom")`,
		code: "EvalError",
		msg:  "assertion failed: boom",
	},
}

// assertGoldenDialects is the dialect axis: the Lisp-1 identity dialect, where
// assert is the stdlib registration itself, and the CL dialect, which reaches
// the same registration through the Lisp-2 GoFunc bridge.
var assertGoldenDialects = []string{"", "cl"}

// assertOutcome is what one (mode, dialect) combination reported for a source:
// the failure's Code and Message, or an empty code and the rendered result when
// the source succeeded.
type assertOutcome struct {
	code    string
	message string
}

// runAssertGolden evaluates src in a freshly loaded stdlib engine under the
// given evaluator mode and dialect.
func runAssertGolden(t *testing.T, mode int, dia, src string) assertOutcome {
	t.Helper()
	em := goldenEvaluatorModes[mode]
	eng := loadStdlibEngine(t, familyDialect(dia), true, em.opts...)
	got, err := eng.Eval(context.Background(), "assert-goldens", src)
	if err == nil {
		require.NotNil(t, got, "%s/%s: %s returned no value", em.name, dia, src)
		return assertOutcome{message: got.String()}
	}
	var le *core.LispicoError
	require.ErrorAs(t, err, &le,
		"%s/%s: %s must fail with a typed *core.LispicoError, got %T: %v", em.name, dia, src, err, err)
	return assertOutcome{code: le.Code, message: le.Message}
}

// TestAssertMessage_GoldensAcrossModesAndDialects pins what assert reports for
// each argument shape, in every combination of execution mode and dialect.
func TestAssertMessage_GoldensAcrossModesAndDialects(t *testing.T) {
	for _, g := range assertMessageGoldens {
		t.Run(g.name, func(t *testing.T) {
			for mode, em := range goldenEvaluatorModes {
				for _, dia := range assertGoldenDialects {
					got := runAssertGolden(t, mode, dia, g.src)
					if g.code == "" {
						assert.Equal(t, core.Nil{}.String(), got.message,
							"%s/%s: %s must succeed with nil, got %q (code %q)",
							em.name, dia, g.src, got.message, got.code)
						assert.Empty(t, got.code,
							"%s/%s: %s must not fail", em.name, dia, g.src)
						continue
					}
					assert.Equal(t, g.code, got.code,
						"%s/%s: %s must classify under %s", em.name, dia, g.src, g.code)
					assert.Equal(t, g.msg, got.message,
						"%s/%s: %s must report %q", em.name, dia, g.src, g.msg)
				}
			}
		})
	}
}

// TestAssertMessage_ModesAndDialectsAgree pins that assert reports one source
// identically whichever evaluator runs it and whichever dialect names it: the
// Code and the Message are byte-identical across all four combinations.
func TestAssertMessage_ModesAndDialectsAgree(t *testing.T) {
	for _, g := range assertMessageGoldens {
		t.Run(g.name, func(t *testing.T) {
			var first assertOutcome
			firstLabel := ""
			for mode, em := range goldenEvaluatorModes {
				for _, dia := range assertGoldenDialects {
					label := em.name + "/" + dia
					got := runAssertGolden(t, mode, dia, g.src)
					if firstLabel == "" {
						first, firstLabel = got, label
						continue
					}
					assert.Equal(t, first.code, got.code,
						"%s: %s must classify as %s does (%q)", label, g.src, firstLabel, first.code)
					assert.Equal(t, first.message, got.message,
						"%s: %s must report as %s does (%q)", label, g.src, firstLabel, first.message)
				}
			}
		})
	}
}
