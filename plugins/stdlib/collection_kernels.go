package stdlib

import (
	"context"
	"fmt"

	"github.com/victorzhuk/go-lispico/core"
)

type AccessOutcome int

const (
	AccessHit AccessOutcome = iota
	AccessOutOfRange
	AccessUnsupported
)

func IndexedAccess(ctx context.Context, subject core.Value, idx int64) (core.Value, AccessOutcome, error) {
	switch c := subject.(type) {
	case core.List:
		if idx < 0 || idx >= int64(c.Len()) {
			return nil, AccessOutOfRange, nil
		}
		b := core.NewBuiltinWorkBudget(ctx)
		cur := c
		for i := int64(0); i <= idx; i++ {
			if err := b.Step(); err != nil {
				return nil, AccessHit, err
			}
			if i < idx {
				cur = cur.Rest()
			}
		}
		if err := b.Flush(); err != nil {
			return nil, AccessHit, err
		}
		return cur.At(0), AccessHit, nil
	case core.Vector:
		if idx < 0 || idx >= int64(c.Len()) {
			return nil, AccessOutOfRange, nil
		}
		b := core.NewBuiltinWorkBudget(ctx)
		if err := b.Step(); err != nil {
			return nil, AccessHit, err
		}
		if err := b.Flush(); err != nil {
			return nil, AccessHit, err
		}
		return c.At(int(idx)), AccessHit, nil
	default:
		return nil, AccessUnsupported, nil
	}
}

func MapSequences(ctx context.Context, eval core.Evaluator, env *core.Env, fn core.Value, seqs []core.Value) (core.Value, error) {
	slices := make([][]core.Value, len(seqs))
	n := -1
	for i, s := range seqs {
		switch c := s.(type) {
		case core.List:
			slices[i] = c.ToSlice()
		case core.Vector:
			slices[i] = c.ToSlice()
		case core.Nil:
			slices[i] = nil
		default:
			return nil, fmt.Errorf("map: unsupported sequence type %T", s)
		}
		if n < 0 || len(slices[i]) < n {
			n = len(slices[i])
		}
	}
	if n < 0 {
		n = 0
	}

	b := core.NewBuiltinWorkBudget(ctx)
	results := make([]core.Value, 0, n)
	for i := 0; i < n; i++ {
		if err := b.Step(); err != nil {
			return nil, err
		}
		args := make([]core.Value, len(slices))
		for j := range slices {
			args[j] = slices[j][i]
		}
		r, err := eval.Apply(ctx, fn, args, env)
		if err != nil {
			if ferr := b.Flush(); ferr != nil {
				return nil, ferr
			}
			return nil, err
		}
		results = append(results, r)
	}
	if err := b.Flush(); err != nil {
		return nil, err
	}
	return core.NewList(results), nil
}
