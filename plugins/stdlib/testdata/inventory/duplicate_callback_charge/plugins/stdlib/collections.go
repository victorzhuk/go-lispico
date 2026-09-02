package stdlib

import (
	"context"

	"github.com/victorzhuk/go-lispico/core"
)

// mapEach applies the callback once per element. The callback's own work is
// charged by a callback-owned row, and a second row charges it again.
func mapEach(ctx context.Context, eval core.Evaluator, fn core.Value, items []core.Value, env *core.Env) ([]core.Value, error) {
	out := make([]core.Value, 0, len(items))
	for _, item := range items {
		v, err := eval.Apply(ctx, fn, []core.Value{item}, env)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
