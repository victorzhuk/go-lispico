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

// TestRuntimeScopedDefinitionParity pins the vm-scoped-definition-fallback
// contract at the public Engine seam: the three exact scoped-definition
// repros and the empty-scope case from the bytecode-vm spec delta must
// produce the tree-walker-pinned values and binding effects under both
// shipped dialects and both evaluator modes, cold and repeated.
func TestRuntimeScopedDefinitionParity(t *testing.T) {
	t.Parallel()

	type scopedDefCase struct {
		name    string
		dialect core.Dialect
		src     string
		want    core.Value
		errCode string // non-empty: expect a typed LispicoError with this code
	}

	cases := []scopedDefCase{
		// Spec delta: "Function-local definition leaves the outer value intact".
		{
			name:    "clojure/function-local def leaves outer value intact",
			dialect: clojure.Dialect(),
			src:     `(def x 10) ((fn [] (def x 1))) x`,
			want:    core.Int{V: 10},
		},
		// Spec delta: "Definition updates the current local binding".
		{
			name:    "clojure/def updates current local binding",
			dialect: clojure.Dialect(),
			src:     `(let [x 1] (def x 2) x)`,
			want:    core.Int{V: 2},
		},
		// Spec delta: "New local definition does not escape".
		{
			name:    "clojure/new local def does not escape",
			dialect: clojure.Dialect(),
			src:     `(let [x 1] (def y x)) y`,
			errCode: "UndefinedError",
		},
		// Spec delta: "Empty scope still isolates definitions".
		{
			name:    "clojure/empty scope still isolates definitions",
			dialect: clojure.Dialect(),
			src:     `(def x 10) (let [] (def x 1)) x`,
			want:    core.Int{V: 10},
		},
		// CL list-pair shapes of the same four delta scenarios.
		{
			name:    "cl/function-local def leaves outer value intact",
			dialect: cl.Dialect(),
			src:     `(def x 10) ((fn () (def x 1))) x`,
			want:    core.Int{V: 10},
		},
		{
			name:    "cl/def updates current local binding",
			dialect: cl.Dialect(),
			src:     `(let ((x 1)) (def x 2) x)`,
			want:    core.Int{V: 2},
		},
		{
			name:    "cl/new local def does not escape",
			dialect: cl.Dialect(),
			src:     `(let ((x 1)) (def y x)) y`,
			errCode: "UndefinedError",
		},
		{
			name:    "cl/empty scope still isolates definitions",
			dialect: cl.Dialect(),
			src:     `(def x 10) (let () (def x 1)) x`,
			want:    core.Int{V: 10},
		},
	}

	for _, mode := range goldenEvaluatorModes {
		for _, tc := range cases {
			t.Run(mode.name+"/"+tc.name, func(t *testing.T) {
				t.Parallel()

				eng := newGoldenEngine(t, tc.dialect, true, mode.opts...)
				ctx := context.Background()

				// Cold Eval: first evaluation compiles (or refuses and falls
				// back) and must match the tree-walker-pinned result.
				cold, err := eng.Eval(ctx, "cold", tc.src)
				if tc.errCode != "" {
					require.Error(t, err, "TestRuntimeScopedDefinitionParity/%s/%s: cold Eval must report the undefined final symbol, got %v", mode.name, tc.name, cold)
					var le *core.LispicoError
					require.ErrorAs(t, err, &le, "TestRuntimeScopedDefinitionParity/%s/%s: cold Eval error must be typed", mode.name, tc.name)
					assert.Equal(t, tc.errCode, le.Code, "TestRuntimeScopedDefinitionParity/%s/%s: cold Eval error code", mode.name, tc.name)
				} else {
					require.NoErrorf(t, err, "TestRuntimeScopedDefinitionParity/%s/%s: cold Eval must succeed", mode.name, tc.name)
					assert.Truef(t, tc.want.Equals(cold), "TestRuntimeScopedDefinitionParity/%s/%s: cold Eval = %v, want %v", mode.name, tc.name, cold, tc.want)
				}

				// Repeated Eval: the same source re-evaluated (fallback forms
				// are not chunk-cached) must stay pinned.
				warm, err := eng.Eval(ctx, "warm", tc.src)
				if tc.errCode != "" {
					require.Error(t, err, "TestRuntimeScopedDefinitionParity/%s/%s: repeated Eval must report the undefined final symbol, got %v", mode.name, tc.name, warm)
					var le *core.LispicoError
					require.ErrorAs(t, err, &le, "TestRuntimeScopedDefinitionParity/%s/%s: repeated Eval error must be typed", mode.name, tc.name)
					assert.Equal(t, tc.errCode, le.Code, "TestRuntimeScopedDefinitionParity/%s/%s: repeated Eval error code", mode.name, tc.name)
				} else {
					require.NoErrorf(t, err, "TestRuntimeScopedDefinitionParity/%s/%s: repeated Eval must succeed", mode.name, tc.name)
					assert.Truef(t, tc.want.Equals(warm), "TestRuntimeScopedDefinitionParity/%s/%s: repeated Eval = %v, want %v", mode.name, tc.name, warm, tc.want)
				}
			})
		}
	}
}
