package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/cl"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
)

// TestCLAdapters_EmptyBaseNoAdapters: an empty-base dialect with an explicit
// vocabulary allowlist exposes no CL collection adapter names — only the
// allowlisted names are callable, and stdlib names absent from the allowlist
// are undefined. The explicit Vocabulary is required: a nil vocabulary
// disables the allowlist strip entirely.

func TestCLAdapters_EmptyBaseNoAdapters(t *testing.T) {
	d := core.EmptyDialect().
		Add("if", "if").
		Add("quote", "quote").
		Vocabulary(map[string]string{"first": "first"})
	e, err := New(nil, WithDialect(d))
	require.NoError(t, err)
	defer e.Close()

	require.NoError(t, e.Use(stdlib.New()))

	ctx := context.Background()

	got, err := e.Eval(ctx, "first", "(first '(1 2 3))")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 1}.Equals(got), "first is in the allowlist and must be callable")

	for _, tc := range []struct {
		name string
		src  string
	}{
		{"nth", "(nth 0 nil)"},
		{"mapcar", "(mapcar 0 nil)"},
		{"sort", "(sort nil nil)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := e.Eval(ctx, tc.name, tc.src)
			require.Error(t, err, "%s must be undefined under an empty-base allowlist", tc.name)
			assert.Contains(t, err.Error(), "undefined", "expected an undefined-name error, got: %v", err)
		})
	}

	got, err = e.Eval(ctx, "control", "(if true 7 8)")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 7}.Equals(got), "allowlisted special forms remain callable")
}

// clAdapterGoldenCase is one hand-derived literal: the source evaluates to
// want under its dialect, independent of evaluator and stdlib-lazy mode.
type clAdapterGoldenCase struct {
	name string
	src  string
	want core.Value
}

// clAdapterCorpus covers every CL collection adapter call shape over the
// shared kernels: indexed access, multi-sequence mapping with shortest-list
// termination, and the full sort grammar including :key and result typing.
var clAdapterCorpus = []clAdapterGoldenCase{
	{name: "nth-hit", src: "(nth 1 (list 10 20 30))", want: core.Int{V: 20}},
	{name: "nth-beyond-end", src: "(nth 5 (list 1 2))", want: core.Nil{}},
	{name: "nth-nil-subject", src: "(nth 0 ())", want: core.Nil{}},
	{name: "mapcar-single", src: "(mapcar (fn (x) (* x 2)) (list 1 2 3))", want: core.NewList([]core.Value{core.Int{V: 2}, core.Int{V: 4}, core.Int{V: 6}})},
	{name: "mapcar-multi-sequence", src: "(mapcar (fn (a b) (+ a b)) (list 1 2 3) (list 10 20 30))", want: core.NewList([]core.Value{core.Int{V: 11}, core.Int{V: 22}, core.Int{V: 33}})},
	{name: "mapcar-shortest-terminates", src: "(mapcar (fn (a b) (+ a b)) (list 1 2 3) (list 10 20))", want: core.NewList([]core.Value{core.Int{V: 11}, core.Int{V: 22}})},
	{name: "mapcar-empty", src: "(mapcar (fn (x) (* x 2)) ())", want: core.NewList(nil)},
	{name: "sort-list", src: "(sort (list 3 1 2) (fn (a b) (< a b)))", want: core.NewList([]core.Value{core.Int{V: 1}, core.Int{V: 2}, core.Int{V: 3}})},
	{name: "sort-key", src: "(sort (list 1 2 3) (fn (a b) (< a b)) :key (fn (x) (- x)))", want: core.NewList([]core.Value{core.Int{V: 3}, core.Int{V: 2}, core.Int{V: 1}})},
	{name: "sort-vector", src: "(sort #(3 1 2) (fn (a b) (< a b)))", want: core.NewVector([]core.Value{core.Int{V: 1}, core.Int{V: 2}, core.Int{V: 3}})},
	{name: "sort-nil", src: "(sort () (fn (a b) (< a b)))", want: core.NewList(nil)},
}

// clojureCanonicalCorpus covers the canonical Clojure-style names on the
// same shared inputs, so parity with the CL adapter names is observable.
var clojureCanonicalCorpus = []clAdapterGoldenCase{
	{name: "nth", src: "(nth [10 20 30] 1)", want: core.Int{V: 20}},
	{name: "sort", src: "(sort [3 1 2])", want: core.NewList([]core.Value{core.Int{V: 1}, core.Int{V: 2}, core.Int{V: 3}})},
	{name: "map", src: "(map (fn [x] (* x 2)) [1 2 3])", want: core.NewList([]core.Value{core.Int{V: 2}, core.Int{V: 4}, core.Int{V: 6}})},
}

// goldenEvaluatorModes is the evaluator axis of the golden matrix.
var goldenEvaluatorModes = []struct {
	name string
	opts []EngineOption
}{
	{name: "tree-walker", opts: []EngineOption{WithTreeWalker()}},
	{name: "vm", opts: []EngineOption{WithBytecode()}},
}

// newGoldenEngine builds an engine under d with the given options, loading
// stdlib and forcing lazy publication so cell assertions observe the final
// state. The process-global lazy flag is restored before the caller asserts.
func newGoldenEngine(t *testing.T, d core.Dialect, eager bool, opts ...EngineOption) Engine {
	t.Helper()
	restore := SetStdlibLazyDisabledForTesting(eager)
	defer restore()
	opts = append([]EngineOption{WithDialect(d)}, opts...)
	eng, err := New(nil, opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	require.NoError(t, eng.Use(stdlib.New()))
	if !eager {
		eng.RootEnv().VarNames()
		eng.RootEnv().FuncNames()
	}
	return eng
}

// TestCLAdapters_EvaluatorAndVM_Goldens runs the full adapter and canonical
// corpora under both evaluators, pins the adapter bindings in both cells, and
// proves parity: CL mapcar and canonical map agree on the same input, and the
// CL and Clojure-style shapes agree over the shared kernel.
func TestCLAdapters_EvaluatorAndVM_Goldens(t *testing.T) {
	for _, mode := range goldenEvaluatorModes {
		t.Run(mode.name, func(t *testing.T) {
			clEng := newGoldenEngine(t, cl.Dialect(), true, mode.opts...)
			root := clEng.RootEnv()
			for _, name := range []string{"nth", "mapcar", "sort"} {
				_, ok := root.GetFunc(name)
				require.True(t, ok, "%s: %s must be bound in the function cell", mode.name, name)
			}
			_, ok := root.Get("mapcar")
			require.True(t, ok, "%s: mapcar must be bound in the value cell", mode.name)

			ctx := context.Background()
			for _, tc := range clAdapterCorpus {
				got, err := clEng.Eval(ctx, "cl-adapters", tc.src)
				require.NoError(t, err, "%s: %s: %v", mode.name, tc.src, err)
				assert.True(t, tc.want.Equals(got), "%s: %s => %v, want %v", mode.name, tc.src, got, tc.want)
			}

			mapcarGot, err := clEng.Eval(ctx, "cl-adapters", `(mapcar (fn (x) (* x 2)) (list 1 2 3))`)
			require.NoError(t, err)
			mapGot, err := clEng.Eval(ctx, "cl-adapters", `(map (fn (x) (* x 2)) (list 1 2 3))`)
			require.NoError(t, err)
			assert.True(t, mapGot.Equals(mapcarGot), "%s: mapcar and canonical map must agree on the shared input", mode.name)

			clojEng := newGoldenEngine(t, clojure.Dialect(), true, mode.opts...)
			for _, tc := range clojureCanonicalCorpus {
				got, err := clojEng.Eval(ctx, "clojure-canonical", tc.src)
				require.NoError(t, err, "%s: %s: %v", mode.name, tc.src, err)
				assert.True(t, tc.want.Equals(got), "%s: %s => %v, want %v", mode.name, tc.src, got, tc.want)
			}
			clojMapGot, err := clojEng.Eval(ctx, "clojure-canonical", `(map (fn [x] (* x 2)) [1 2 3])`)
			require.NoError(t, err)
			assert.True(t, clojMapGot.Equals(mapcarGot), "%s: CL mapcar and Clojure-style map must agree over the shared kernel", mode.name)
		})
	}
}

// TestCLAdapters_EagerLazy_Goldens runs the adapter corpus and binding
// assertions under every combination of stdlib-lazy mode and evaluator, so
// each visible name satisfies its own call shape regardless of materialization.
func TestCLAdapters_EagerLazy_Goldens(t *testing.T) {
	modes := []struct {
		name  string
		eager bool
		opts  []EngineOption
	}{
		{name: "lazy/tree-walker", eager: false, opts: []EngineOption{WithTreeWalker()}},
		{name: "eager/tree-walker", eager: true, opts: []EngineOption{WithTreeWalker()}},
		{name: "lazy/vm", eager: false, opts: []EngineOption{WithBytecode()}},
		{name: "eager/vm", eager: true, opts: []EngineOption{WithBytecode()}},
	}
	for _, mode := range modes {
		t.Run(mode.name, func(t *testing.T) {
			eng := newGoldenEngine(t, cl.Dialect(), mode.eager, mode.opts...)
			root := eng.RootEnv()
			for _, name := range []string{"nth", "mapcar", "sort"} {
				_, ok := root.GetFunc(name)
				require.True(t, ok, "%s: %s must be bound in the function cell", mode.name, name)
			}
			_, ok := root.Get("mapcar")
			require.True(t, ok, "%s: mapcar must be bound in the value cell", mode.name)
			for _, tc := range clAdapterCorpus {
				got, err := eng.Eval(context.Background(), "cl-adapters", tc.src)
				require.NoError(t, err, "%s: %s: %v", mode.name, tc.src, err)
				assert.True(t, tc.want.Equals(got), "%s: %s => %v, want %v", mode.name, tc.src, got, tc.want)
			}
		})
	}
}

// TestCLAdapters_LowReductionBudget pins the terminal budget behavior: a
// 599-element sort under a 300-reduction ceiling must surface a Terminal
// ResourceLimitError from the kernel's mandatory budget sync.
func TestCLAdapters_LowReductionBudget(t *testing.T) {
	eng := newGoldenEngine(t, cl.Dialect(), true,
		WithBytecode(),
		WithResourceLimits(ResourceLimits{MaxReductions: 300, MaxCollectionLen: 1 << 30, MaxCacheEntries: 1 << 12}),
	)
	// Measured over these 599 elements: reaching the bound subject costs 4
	// reductions and completing this sort needs 24855, so under a 300 ceiling
	// the Terminal can only come from sort's own budget.
	bindPrebuiltSubject(t, eng, "lowbudget-subject", 599)
	_, err := eng.Eval(context.Background(), "cl-budget", `(sort lowbudget-subject (fn (a b) (< a b)))`)
	require.Error(t, err, "a 599-element sort under a 300-reduction budget must Terminal")
	var le *core.LispicoError
	require.ErrorAs(t, err, &le, "error must be a typed *core.LispicoError, got %v", err)
	assert.Equal(t, core.CodeResourceLimit, le.Code,
		"the crossed reduction ceiling must classify under %s, got %s", core.CodeResourceLimit, le.Code)
	assert.True(t, core.IsTerminalEvalError(err), "low Reduction budget must be terminal, got %v", err)
}

// TestCLAdapters_LateVMDeadline pins the Terminal-wins precedence at the
// sort's mandatory Flush: a late engine deadline surfaces DeadlineExceeded
// over a pending non-Terminal predicate error, and a final Flush that crosses
// the reduction ceiling surfaces ResourceLimitError over it.
func TestCLAdapters_LateVMDeadline(t *testing.T) {
	t.Run("deadline-wins-over-pending-type-error", func(t *testing.T) {
		const timeout = 100 * time.Millisecond
		eng := newGoldenEngine(t, cl.Dialect(), true, WithBytecode(), WithTimeout(timeout))
		var predCalls int
		require.NoError(t, eng.Bind("cldeadline-pred", core.GoFunc{
			Name: "cldeadline-pred",
			Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
				predCalls++
				time.Sleep(300 * time.Millisecond)
				if predCalls == 2 {
					return nil, core.NewTypeError("int", core.Int{V: 1})
				}
				return core.Int{V: 1}, nil
			},
		}))
		_, err := eng.Eval(context.Background(), "cl-deadline", `(sort (list 3 1 2) #'cldeadline-pred)`)
		require.Error(t, err, "a predicate sleeping past the engine deadline must surface an error")
		assert.True(t, errors.Is(err, context.DeadlineExceeded),
			"late VM deadline must surface context.DeadlineExceeded, got %v", err)
		assert.GreaterOrEqual(t, predCalls, 2, "the predicate must run before the mandatory Flush observes the deadline")
	})

	t.Run("resource-limit-wins-over-pending-type-error", func(t *testing.T) {
		eng := newGoldenEngine(t, cl.Dialect(), true,
			WithBytecode(),
			WithResourceLimits(ResourceLimits{MaxReductions: 300, MaxCollectionLen: 1 << 30, MaxCacheEntries: 1 << 12}),
		)
		// Measured over these 380 elements: the run has accrued 268
		// reductions by the predicate's second call and needs 396 to reach
		// the end of sort's mandatory Flush, so the 300 ceiling provably
		// falls between the two.
		bindPrebuiltSubject(t, eng, "clbudget-subject", 380)
		var predCalls int
		require.NoError(t, eng.Bind("clbudget-pred", core.GoFunc{
			Name: "clbudget-pred",
			Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
				predCalls++
				if predCalls == 2 {
					return nil, core.NewTypeError("int", core.Int{V: 1})
				}
				return core.Int{V: 1}, nil
			},
		}))
		_, err := eng.Eval(context.Background(), "cl-budget-pending", `(sort clbudget-subject #'clbudget-pred)`)
		require.Error(t, err, "the sort Flush crossing the reduction ceiling must surface an error")
		var le *core.LispicoError
		require.ErrorAs(t, err, &le, "error must be a typed *core.LispicoError, got %v", err)
		assert.Equal(t, core.CodeResourceLimit, le.Code,
			"Terminal ResourceLimitError must win over the predicate's pending TypeError, got %v", err)
		assert.Equal(t, 2, predCalls, "the predicate must error on its second call before the budget crosses")
	})
}
