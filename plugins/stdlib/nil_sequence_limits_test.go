package stdlib

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

type depthLimitEvaluator struct {
	limit int
}

func (e depthLimitEvaluator) Eval(context.Context, core.Value, *core.Env) (core.Value, error) {
	return core.Nil{}, errors.New("test evaluator only implements construction depth limit")
}

func (e depthLimitEvaluator) Apply(context.Context, core.Value, []core.Value, *core.Env) (core.Value, error) {
	return core.Nil{}, errors.New("test evaluator only implements construction depth limit")
}

func (e depthLimitEvaluator) ConstructionDepthLimit() int {
	return e.limit
}

func evalErrUnder(t *testing.T, ev core.Evaluator, env *core.Env, code string) error {
	t.Helper()
	forms, err := core.Read(code)
	if err != nil {
		return err
	}
	if len(forms) == 0 {
		t.Fatal("empty input")
	}
	_, err = ev.Eval(context.Background(), forms[0], env)
	return err
}

// TestCollections_LimitsFromActiveEvaluatorNotEnv: a child environment with
// no evaluator of its own must still enforce the limits carried by the
// evaluator that invoked the builtin; without one, the stdlib defaults apply.
func TestCollections_LimitsFromActiveEvaluatorNotEnv(t *testing.T) {
	root := setupEnv(t)
	child := root.Child()
	child.SetEvaluator(nil)
	require.Nil(t, child.Evaluator())

	list := func(vs ...core.Value) core.List { return core.NewList(vs) }
	one, two := core.Int{V: 1}, core.Int{V: 2}

	arms := []struct {
		name, builtin string
		eval          core.Evaluator
		args          []core.Value
		msg           string
	}{
		{"cons length", "cons", collectionLimitEvaluator{limit: 2}, []core.Value{core.Int{V: 0}, list(one, two)}, "cons length 3 exceeds collection limit 2"},
		{"conj length", "conj", collectionLimitEvaluator{limit: 2}, []core.Value{list(one, two), core.Int{V: 0}}, "conj length 3 exceeds collection limit 2"},
		{"range length", "range", collectionLimitEvaluator{limit: 3}, []core.Value{core.Int{V: 5}}, "range length 5 exceeds collection limit 3"},
		{"list depth", "list", depthLimitEvaluator{limit: 1}, []core.Value{list(one)}, "structural depth limit 1 exceeded"},
		{"hash-map depth", "hash-map", depthLimitEvaluator{limit: 1}, []core.Value{core.Keyword{V: "a"}, list(one)}, "structural depth limit 1 exceeded"},
		{"cons nested depth", "cons", depthLimitEvaluator{limit: 2}, []core.Value{list(list(one)), list(one)}, "structural depth limit 2 exceeded"},
	}
	for _, arm := range arms {
		fn := collectionGoFunc(t, root, arm.builtin)
		t.Run(arm.name+" from eval", func(t *testing.T) {
			_, err := fn.Fn(t.Context(), arm.eval, arm.args, child)
			requireResourceLimit(t, err)
			require.ErrorContains(t, err, arm.msg)
		})
		t.Run(arm.name+" default without eval", func(t *testing.T) {
			_, err := fn.Fn(t.Context(), nil, arm.args, child)
			require.NoError(t, err)
		})
	}
}
