package vm

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

// hostileEmbedderCtx is a legal (if unusual) context.Context embedder: a
// by-value struct with an `any` field that can hold a non-comparable
// dynamic value (a slice, here). reflect.Type.Comparable() reports true for
// this type regardless — an interface-kind field is always "comparable" in
// the static sense reflect checks — but == on two such values panics at
// runtime once that field holds a slice, map, or func on both sides.
type hostileEmbedderCtx struct {
	context.Context
	extra any
}

// TestVM_ReentrantRearm_HostileEmbedderCtxNeverPanics proves the reuse
// decision in RearmReentrantEvalState cannot panic even when the outer
// ctx's dynamic type is one reflect.Comparable() misclassifies as safe:
// comparableKind must reject it before core.RearmReentrantEvalState ever
// reaches ==. Two dispatches with the identical outer ctx value are
// required — the first only builds the wrapper, the second (after a Reset,
// which does not clear reentryCtx) drives reentrantCtx into the rearm
// branch, the one path that evaluates w.Context != ctx.
func TestVM_ReentrantRearm_HostileEmbedderCtxNeverPanics(t *testing.T) {
	env := core.NewEnv(nil)
	v := New(env, WithEvaluator(core.NewEvaluator()))

	ctx := hostileEmbedderCtx{Context: context.Background(), extra: []int{1, 2, 3}}
	fn := core.GoFunc{
		Name: "noop",
		Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
			return args[0], nil
		},
	}
	args := []core.Value{core.Int{V: 42}}

	v.Reset()
	v.SetTimeout(time.Second)
	result, err := v.ApplyPooled(ctx, fn, args, env)
	require.NoError(t, err)
	require.Equal(t, core.Int{V: 42}, result)

	v.Reset()
	v.SetTimeout(time.Second)
	result, err = v.ApplyPooled(ctx, fn, args, env)
	require.NoError(t, err)
	require.Equal(t, core.Int{V: 42}, result)
}
