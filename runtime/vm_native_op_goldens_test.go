// Engine-level goldens for the operators the VM implements natively. The
// compiler maps + - * / < > <= >= = to fast-path opcodes that never dispatch to
// the stdlib Builtin, so classification on the default engine is decided by
// core/vm/vm.go rather than by the typed Builtin. Every row therefore asserts
// the two execution paths against EACH OTHER as well as against the Code its
// contract demands: a row that only checked each path against a literal would
// pass while the paths disagreed, which is exactly the gap
// stdlib_error_goldens_test.go leaves open by classifying zero divisors through
// mod and quot, neither of which has a native opcode.
package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
)

// vmNativeOpGolden is one hand-derived classification row: src fails under both
// execution paths, and both classify it under code.
type vmNativeOpGolden struct {
	name string
	src  string
	code string
}

// vmNativeOpGoldens covers every operator core/compiler nativeOp maps to an
// opcode, paired with each failure class the VM's native helper constructs.
var vmNativeOpGoldens = []vmNativeOpGolden{
	{name: "arity/sub-none", src: "(-)", code: "ArityError"},
	{name: "arity/div-one", src: "(/ 1)", code: "ArityError"},
	{name: "arity/eq-none", src: "(=)", code: "ArityError"},
	{name: "arity/lt-none", src: "(<)", code: "ArityError"},
	{name: "arity/gt-none", src: "(>)", code: "ArityError"},
	{name: "arity/le-none", src: "(<=)", code: "ArityError"},
	{name: "arity/ge-none", src: "(>=)", code: "ArityError"},

	{name: "type/add-operand", src: `(+ 1 "x")`, code: "TypeError"},
	{name: "type/sub-first", src: `(- "x")`, code: "TypeError"},
	{name: "type/sub-rest", src: `(- 1 "x")`, code: "TypeError"},
	{name: "type/mul-operand", src: `(* 1 "x")`, code: "TypeError"},
	{name: "type/div-first", src: `(/ "a" 2)`, code: "TypeError"},
	{name: "type/div-rest", src: `(/ 4 "b")`, code: "TypeError"},
	{name: "type/lt-first", src: `(< "x" 1)`, code: "TypeError"},
	{name: "type/lt-rest", src: `(< 1 "x")`, code: "TypeError"},
	{name: "type/gt-rest", src: `(> 1 "x")`, code: "TypeError"},
	{name: "type/le-rest", src: `(<= 1 "x")`, code: "TypeError"},
	{name: "type/ge-rest", src: `(>= 1 "x")`, code: "TypeError"},

	{name: "zero-divisor/div-int", src: "(/ 1 0)", code: "EvalError"},
	{name: "zero-divisor/div-float", src: "(/ 1 0.0)", code: "EvalError"},
}

// vmNativeOpControls carry the same failure class on operators the compiler
// does not map to an opcode, so both paths route through the typed stdlib
// Builtin. They discriminate a real native-path defect from a broken harness.
var vmNativeOpControls = []vmNativeOpGolden{
	{name: "zero-divisor/mod", src: "(mod 1 0)", code: "EvalError"},
	{name: "zero-divisor/quot", src: "(quot 1 0)", code: "EvalError"},
}

// classifyBothPaths runs src under the VM and the tree-walker and reports the
// Code each path classified the failure under. A path whose error is not a
// *core.LispicoError reports the empty string: the untyped shape an embedder
// cannot switch on.
func classifyBothPaths(t *testing.T, label, src string) map[string]string {
	t.Helper()
	out := make(map[string]string, len(goldenEvaluatorModes))
	for _, em := range goldenEvaluatorModes {
		eng := loadStdlibEngine(t, clojure.Dialect(), true, em.opts...)
		val, err := eng.Eval(context.Background(), "vm-native-op-golden", src)
		require.Error(t, err, "%s/%s: %s must fail, got %v", em.name, label, src, val)

		var le *core.LispicoError
		if !errors.As(err, &le) {
			assert.Fail(t, "untyped failure from a native operator",
				"%s/%s: %s must fail with a typed *core.LispicoError, got %T: %v", em.name, label, src, err, err)
			out[em.name] = ""
			continue
		}
		assert.NotEmpty(t, le.Message, "%s/%s: %s must carry a diagnostic message", em.name, label, src)
		out[em.name] = le.Code
	}
	return out
}

// assertNativeOpPathsAgree pins the two paths against each other. The native
// fast path bypasses the stdlib Builtin entirely, so the disagreement it can
// introduce is invisible to a per-path check against a literal.
func assertNativeOpPathsAgree(t *testing.T, label, src string, got map[string]string) {
	t.Helper()
	assert.Equal(t, got["tree-walker"], got["vm"],
		"%s: %s must classify identically under both paths (tree-walker %q, vm %q)",
		label, src, got["tree-walker"], got["vm"])
}

func assertNativeOpGoldens(t *testing.T, rows []vmNativeOpGolden) {
	t.Helper()
	for _, g := range rows {
		t.Run(g.name, func(t *testing.T) {
			got := classifyBothPaths(t, g.name, g.src)
			for _, em := range goldenEvaluatorModes {
				assert.Equal(t, g.code, got[em.name],
					"%s/%s: %s must classify under %s", em.name, g.name, g.src, g.code)
			}
			assertNativeOpPathsAgree(t, g.name, g.src, got)
		})
	}
}

// TestVMNativeOps_ClassificationGoldens pins that a failure raised inside the
// VM's native arithmetic and comparison opcodes reaches an embedder as a
// *core.LispicoError under the same Code the tree-walker assigns it.
func TestVMNativeOps_ClassificationGoldens(t *testing.T) {
	assertNativeOpGoldens(t, vmNativeOpGoldens)
}

// TestVMNativeOps_NonNativeControls pins that operators without a native
// opcode already agree, so a failure in the goldens above localises to the
// native path and not to the shared engine harness.
func TestVMNativeOps_NonNativeControls(t *testing.T) {
	assertNativeOpGoldens(t, vmNativeOpControls)
}
