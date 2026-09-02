package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
)

// TestNilSequence_NestedLambdaKeepsEngineLimits pins that collection limits
// come from the active evaluator, not from the scope a builtin happens to run
// in: a lambda body evaluated in a child scope with no evaluator of its own
// must still trip the engine's MaxCollectionLen under both execution paths.
func TestNilSequence_NestedLambdaKeepsEngineLimits(t *testing.T) {
	const src = `((fn [x] (cons x (list 1 2 3 4 5 6 7 8))) 0)`
	for _, mode := range goldenEvaluatorModes {
		t.Run(mode.name, func(t *testing.T) {
			opts := append(append([]EngineOption{}, mode.opts...),
				WithResourceLimits(ResourceLimits{MaxCollectionLen: 8}))
			eng := newGoldenEngine(t, clojure.Dialect(), true, opts...)

			orphan := eng.RootEnv().Child()
			orphan.SetEvaluator(nil)
			form, err := core.ReadOne(src)
			require.NoError(t, err)

			_, err = eng.RootEnv().Evaluator().Eval(context.Background(), form, orphan)
			require.Error(t, err, "%s: a 9-element cons under MaxCollectionLen 8 must fail inside the nested lambda", mode.name)
			var le *core.LispicoError
			require.ErrorAs(t, err, &le, "%s: the limit failure must be a typed *core.LispicoError, got %v", mode.name, err)
			assert.Equal(t, core.CodeResourceLimit, le.Code,
				"%s: the nested lambda must keep the engine's collection limit, got %s: %s", mode.name, le.Code, le.Message)
		})
	}
}
