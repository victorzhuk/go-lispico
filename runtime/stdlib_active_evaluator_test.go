package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
)

// TestLookup_ActiveEvaluatorPolicyNestedLambda pins the same policy at engine
// level for the lookup seam: a lambda body evaluated in a child scope with no
// evaluator of its own must still trip the engine's MaxCollectionLen, under
// both execution paths.
//
// The behaviour is already correct, so this is a regression pin — green before
// the migration and green after it. Its job is to keep the limit sourced from
// the active evaluator once these builtins take ownership of their results.
func TestLookup_ActiveEvaluatorPolicyNestedLambda(t *testing.T) {
	cases := []struct {
		name, src string
	}{
		{"assoc", `((fn [x] (assoc {:a 1 :b 2 :c 3 :d 4 :e 5 :f 6 :g 7 :h 8} :i x)) 0)`},
		{"conj", `((fn [x] (conj (list 1 2 3 4 5 6 7 8) x)) 0)`},
	}

	for _, mode := range goldenEvaluatorModes {
		for _, tc := range cases {
			t.Run(mode.name+"/"+tc.name, func(t *testing.T) {
				opts := append(append([]EngineOption{}, mode.opts...),
					WithResourceLimits(ResourceLimits{MaxCollectionLen: 8}))
				eng := newGoldenEngine(t, clojure.Dialect(), true, opts...)

				orphan := eng.RootEnv().Child()
				orphan.SetEvaluator(nil)
				form, err := core.ReadOne(tc.src)
				require.NoError(t, err)

				_, err = eng.RootEnv().Evaluator().Eval(context.Background(), form, orphan)
				require.Errorf(t, err, "%s/%s: a 9-element result under MaxCollectionLen 8 must fail inside the nested lambda", mode.name, tc.name)
				var le *core.LispicoError
				require.ErrorAsf(t, err, &le, "%s/%s: the limit failure must be a typed *core.LispicoError, got %v", mode.name, tc.name, err)
				assert.Equalf(t, core.CodeResourceLimit, le.Code,
					"%s/%s: the nested lambda must keep the engine's collection limit, got %s: %s", mode.name, tc.name, le.Code, le.Message)
			})
		}
	}
}
