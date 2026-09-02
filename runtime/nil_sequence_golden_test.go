package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/cl"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
)

// nilSequenceCLCorpus pins nil as the empty sequence through the CL
// vocabulary and adapter names. CL disables bracket literals, so sources
// spell sequences with (list ...) and lambdas with (fn (x) ...).
var nilSequenceCLCorpus = []clAdapterGoldenCase{
	{name: "mapcar-nil", src: "(mapcar (fn (x) x) nil)", want: core.NewList(nil)},
	{name: "length-nil", src: "(length nil)", want: core.Int{V: 0}},
	{name: "car-nil", src: "(car nil)", want: core.Nil{}},
	{name: "cdr-nil", src: "(cdr nil)", want: core.NewList(nil)},
	{name: "append-nil", src: "(append nil (list 1) nil)", want: core.NewList([]core.Value{core.Int{V: 1}})},
	{name: "reverse-nil", src: "(reverse nil)", want: core.NewList(nil)},
	{name: "cons-onto-nil", src: "(cons 1 nil)", want: core.NewList([]core.Value{core.Int{V: 1}})},
	{name: "apply-trailing-nil", src: "(apply #'+ 1 2 nil)", want: core.Int{V: 3}},
	{name: "apply-only-nil", src: "(apply #'+ nil)", want: core.Int{V: 0}},
	{name: "nth-nil-subject", src: "(nth 0 nil)", want: core.Nil{}},
	{name: "sort-nil", src: "(sort nil (fn (a b) (< a b)))", want: core.NewList(nil)},
}

// nilSequenceClojureCorpus pins the same boundary through the canonical
// Lisp-1 names.
var nilSequenceClojureCorpus = []clAdapterGoldenCase{
	{name: "map-nil", src: "(map (fn [x] x) nil)", want: core.NewList(nil)},
	{name: "filter-nil", src: "(filter (fn [x] true) nil)", want: core.NewList(nil)},
	{name: "reduce-nil", src: "(reduce + nil)", want: core.Nil{}},
	{name: "reduce-init-nil", src: "(reduce + 7 nil)", want: core.Int{V: 7}},
	{name: "apply-trailing-nil", src: "(apply + 1 2 nil)", want: core.Int{V: 3}},
	{name: "count-nil", src: "(count nil)", want: core.Int{V: 0}},
	{name: "first-nil", src: "(first nil)", want: core.Nil{}},
	{name: "rest-nil", src: "(rest nil)", want: core.NewList(nil)},
	{name: "concat-nil", src: "(concat nil [1] nil)", want: core.NewList([]core.Value{core.Int{V: 1}})},
	{name: "nth-nil-default", src: "(nth nil 0 :missing)", want: core.Keyword{V: "missing"}},
	{name: "string-join-nil", src: `(string/join "," nil)`, want: core.String{V: ""}},
	{name: "cons-onto-nil", src: "(cons 1 nil)", want: core.NewList([]core.Value{core.Int{V: 1}})},
	{name: "conj-onto-nil", src: "(conj nil 1 2)", want: core.NewList([]core.Value{core.Int{V: 1}, core.Int{V: 2}})},
	{name: "reverse-nil", src: "(reverse nil)", want: core.NewList(nil)},
	{name: "empty-nil", src: "(empty? nil)", want: core.Bool{V: true}},
	{name: "empty-scalar", src: "(empty? 1)", want: core.Bool{V: false}},
}

// TestNilSequence_Goldens_EvaluatorAndVM runs both nil corpora under both
// evaluators: the CL adapter and vocabulary names and the canonical Clojure
// names must produce the same hand-derived values on nil input.
func TestNilSequence_Goldens_EvaluatorAndVM(t *testing.T) {
	for _, mode := range goldenEvaluatorModes {
		t.Run(mode.name, func(t *testing.T) {
			ctx := context.Background()
			clEng := newGoldenEngine(t, cl.Dialect(), true, mode.opts...)
			for _, tc := range nilSequenceCLCorpus {
				t.Run("cl/"+tc.name, func(t *testing.T) {
					got, err := clEng.Eval(ctx, "nil-sequence-cl", tc.src)
					require.NoError(t, err, "%s: %s must accept nil as the empty sequence: %v", mode.name, tc.src, err)
					assert.True(t, tc.want.Equals(got), "%s: %s => %v, want %v", mode.name, tc.src, got, tc.want)
				})
			}

			clojEng := newGoldenEngine(t, clojure.Dialect(), true, mode.opts...)
			for _, tc := range nilSequenceClojureCorpus {
				t.Run("clojure/"+tc.name, func(t *testing.T) {
					got, err := clojEng.Eval(ctx, "nil-sequence-clojure", tc.src)
					require.NoError(t, err, "%s: %s must accept nil as the empty sequence: %v", mode.name, tc.src, err)
					assert.True(t, tc.want.Equals(got), "%s: %s => %v, want %v", mode.name, tc.src, got, tc.want)
				})
			}
		})
	}
}

// TestNilSequence_ErrorGoldens_BothPaths pins the failures that stay
// failures once nil is a sequence: an unguarded index into nil is still out
// of bounds, and scalars are still rejected — identically on both paths.
func TestNilSequence_ErrorGoldens_BothPaths(t *testing.T) {
	for _, g := range []stdlibErrorGolden{
		{name: "out-of-bounds/nth-nil", src: "(nth nil 0)", code: "EvalError"},
		{name: "type/reverse-scalar", src: "(reverse 5)", code: "TypeError"},
		{name: "type/string-join-nil-separator", src: "(string/join nil nil)", code: "TypeError"},
		{name: "type/map-scalar", src: "(map (fn [x] x) 5)", code: "TypeError"},
	} {
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
