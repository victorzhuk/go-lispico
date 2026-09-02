// Engine-level goldens for stdlib failure classification. Every row carries the
// Code both execution paths must agree on, never an error string: typed
// construction prefixes "<Code>: " onto Error(), so a string golden would be
// rewritten by the migration instead of pinned by it.
//
// The dialect is the Lisp-1 identity dialect, so each name resolves to the
// stdlib registration under test rather than to a CL adapter carrying its own
// arity and argument order.
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

// stdlibErrorGolden is one hand-derived classification row: src fails under
// both execution paths, and both classify it under code.
type stdlibErrorGolden struct {
	name string
	src  string
	code string
}

var stdlibErrorGoldens = []stdlibErrorGolden{
	{name: "exact-arity/count-none", src: "(count)", code: "ArityError"},
	{name: "exact-arity/map-one", src: "(map (fn [x] x))", code: "ArityError"},
	{name: "ranged-arity/nth-too-few", src: "(nth (list 1 2))", code: "ArityError"},
	{name: "ranged-arity/nth-too-many", src: "(nth (list 1 2) 0 :d 9)", code: "ArityError"},
	{name: "ranged-arity/range-none", src: "(range)", code: "ArityError"},
	{name: "ranged-arity/range-too-many", src: "(range 1 2 3 4)", code: "ArityError"},
	{name: "variadic-min/conj-one", src: "(conj (list 1))", code: "ArityError"},
	{name: "variadic-min/apply-one", src: "(apply (fn [x] x))", code: "ArityError"},
	{name: "type/count-scalar", src: "(count 5)", code: "TypeError"},
	{name: "type/nth-index", src: `(nth (list 1 2) "x")`, code: "TypeError"},
	{name: "type/range-non-integer", src: `(range "3")`, code: "TypeError"},
	{name: "out-of-bounds/nth", src: "(nth (list 1 2) 5)", code: "EvalError"},
	{name: "zero-divisor/mod", src: "(mod 1 0)", code: "EvalError"},
	{name: "zero-divisor/quot", src: "(quot 1 0)", code: "EvalError"},
	{name: "malformed-parse/string-to-int", src: `(string->int "abc")`, code: "EvalError"},
	{name: "incomparable/sort", src: `(sort (list 1 "a"))`, code: "EvalError"},
}

// evalErrorBothPaths runs src under the VM and the tree-walker, requires each
// to fail with a *core.LispicoError, and returns the recovered errors keyed by
// path name. bind, when non-nil, installs host bindings before evaluation.
func evalErrorBothPaths(t *testing.T, label, src string, bind func(*testing.T, Engine)) map[string]*core.LispicoError {
	t.Helper()
	out := make(map[string]*core.LispicoError, len(goldenEvaluatorModes))
	for _, em := range goldenEvaluatorModes {
		eng := loadStdlibEngine(t, clojure.Dialect(), true, em.opts...)
		if bind != nil {
			bind(t, eng)
		}
		_, err := eng.Eval(context.Background(), "stdlib-error-golden", src)
		require.Error(t, err, "%s/%s: %s must fail", em.name, label, src)
		var le *core.LispicoError
		require.ErrorAs(t, err, &le,
			"%s/%s: %s must fail with a typed *core.LispicoError, got %T: %v", em.name, label, src, err, err)
		assert.NotEmpty(t, le.Message, "%s/%s: %s must carry a diagnostic message", em.name, label, src)
		out[em.name] = le
	}
	return out
}

// assertPathsAgree pins that the two execution paths classify a failure
// identically and describe it identically.
func assertPathsAgree(t *testing.T, label, src string, got map[string]*core.LispicoError) {
	t.Helper()
	treeErr, vmErr := got["tree-walker"], got["vm"]
	require.NotNil(t, treeErr, "%s: %s produced no tree-walker error", label, src)
	require.NotNil(t, vmErr, "%s: %s produced no VM error", label, src)
	assert.Equal(t, treeErr.Code, vmErr.Code,
		"%s: %s must classify identically under both paths", label, src)
	assert.Equal(t, treeErr.Message, vmErr.Message,
		"%s: %s must carry the same diagnostic under both paths", label, src)
}

// TestStdlibErrors_ClassificationGoldens pins that every stdlib failure class
// reaches an embedder as a *core.LispicoError under the Code its row names,
// through the public Engine API and under both execution paths.
func TestStdlibErrors_ClassificationGoldens(t *testing.T) {
	for _, g := range stdlibErrorGoldens {
		t.Run(g.name, func(t *testing.T) {
			got := evalErrorBothPaths(t, g.name, g.src, nil)
			for _, em := range goldenEvaluatorModes {
				assert.Equal(t, g.code, got[em.name].Code,
					"%s/%s: %s must classify under %s", em.name, g.name, g.src, g.code)
			}
			assertPathsAgree(t, g.name, g.src, got)
		})
	}
}

// TestStdlibErrors_CallbackCodePassthrough pins that a typed error raised by a
// host callback reaches the caller with its own Code: the higher-order builtin
// that invoked it neither reclassifies nor flattens it.
func TestStdlibErrors_CallbackCodePassthrough(t *testing.T) {
	bind := func(t *testing.T, eng Engine) {
		t.Helper()
		require.NoError(t, eng.Bind("callback-probe", core.GoFunc{
			Name: "callback-probe",
			Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
				return nil, core.NewConcurrentUseError("callback-probe")
			},
		}))
	}

	for _, tc := range []struct{ name, src string }{
		{name: "map", src: "(map callback-probe (list 1 2))"},
		{name: "filter", src: "(filter callback-probe (list 1 2))"},
		{name: "reduce", src: "(reduce callback-probe (list 1 2))"},
		{name: "apply", src: "(apply callback-probe (list 1 2))"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := evalErrorBothPaths(t, tc.name, tc.src, bind)
			for _, em := range goldenEvaluatorModes {
				assert.Equal(t, core.CodeConcurrentUse, got[em.name].Code,
					"%s/%s: the callback's typed error must reach the caller under its own Code", em.name, tc.name)
			}
			assertPathsAgree(t, tc.name, tc.src, got)
		})
	}
}

// TestStdlibErrors_TerminalNotCatchable pins the recovery boundary: a
// non-terminal stdlib failure is catchable from Lisp, while a cancelled context
// and a crossed reduction ceiling unwind past try/catch untouched.
func TestStdlibErrors_TerminalNotCatchable(t *testing.T) {
	for _, em := range goldenEvaluatorModes {
		t.Run(em.name+"/non-terminal-is-caught", func(t *testing.T) {
			eng := loadStdlibEngine(t, clojure.Dialect(), true, em.opts...)
			got, err := eng.Eval(context.Background(), "terminal-control", "(try (count) (catch e :caught))")
			require.NoError(t, err, "%s: a non-terminal stdlib failure must be catchable", em.name)
			assert.True(t, core.Keyword{V: "caught"}.Equals(got),
				"%s: the catch clause must produce its handler value, got %v", em.name, got)
		})

		t.Run(em.name+"/cancelled-context-unwinds", func(t *testing.T) {
			eng := loadStdlibEngine(t, clojure.Dialect(), true, em.opts...)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			require.NoError(t, eng.Bind("cancel-probe", core.GoFunc{
				Name: "cancel-probe",
				Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
					cancel()
					return nil, ctx.Err()
				},
			}))

			got, err := eng.Eval(ctx, "terminal-cancel", "(try (cancel-probe) (catch e :caught))")
			require.Error(t, err, "%s: a cancelled context must unwind through try, got %v", em.name, got)
			assert.True(t, core.IsTerminalEvalError(err),
				"%s: cancellation must stay terminal, got %v", em.name, err)
			assert.True(t, errors.Is(err, context.Canceled),
				"%s: cancellation must stay matchable as context.Canceled, got %v", em.name, err)
		})

		t.Run(em.name+"/resource-limit-unwinds", func(t *testing.T) {
			opts := append(append([]EngineOption{}, em.opts...),
				WithResourceLimits(ResourceLimits{MaxReductions: 300, MaxCollectionLen: 1 << 30, MaxCacheEntries: 1 << 12}))
			eng := loadStdlibEngine(t, clojure.Dialect(), true, opts...)

			// Measured over these 599 elements: reaching the bound subject
			// through try costs at most 12 reductions and completing this
			// sort needs 7713 under the tree-walker and 7725 under the VM, so
			// under a 300 ceiling the Terminal can only come from sort's own
			// budget.
			bindPrebuiltSubject(t, eng, "budget-subject", 599)

			got, err := eng.Eval(context.Background(), "terminal-budget", "(try (sort budget-subject) (catch e :caught))")
			require.Error(t, err, "%s: a 599-element sort under a 300-reduction ceiling must unwind through try, got %v", em.name, got)
			var le *core.LispicoError
			require.ErrorAs(t, err, &le, "%s: the terminal error must be a typed *core.LispicoError, got %v", em.name, err)
			assert.Equal(t, core.CodeResourceLimit, le.Code,
				"%s: the reduction ceiling must classify under %s, got %s", em.name, core.CodeResourceLimit, le.Code)
			assert.True(t, core.IsTerminalEvalError(err),
				"%s: a resource-limit failure must stay terminal, got %v", em.name, err)
		})
	}
}
