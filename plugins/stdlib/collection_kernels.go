package stdlib

import (
	"context"

	"github.com/victorzhuk/go-lispico/core"
)

type AccessOutcome int

const (
	AccessHit AccessOutcome = iota
	AccessOutOfRange
	AccessUnsupported
)

func IndexedAccess(ctx context.Context, subject core.Value, idx int64) (core.Value, AccessOutcome, error) {
	panic("not implemented")
}

func MapSequences(ctx context.Context, eval core.Evaluator, env *core.Env, fn core.Value, seqs []core.Value) (core.Value, error) {
	panic("not implemented")
}
