// Package collections provides shared collection mechanics for stdlib
// registration and dialect adapters.
package collections

import (
	"context"
	"fmt"
	"sort"

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

// SortKeyFunc projects an input element to a sort key. StableSort invokes it
// at most once per element, in the original input order, when key is non-nil.
type SortKeyFunc func(core.Value) (core.Value, error)

// SortLessFunc reports whether a must sort before b. StableSort invokes it
// through a scheduling wrapper that charges one Step per call; callback
// execution itself never costs reductions.
type SortLessFunc func(a, b core.Value) (bool, error)

// StableSort returns items sorted stably by less under the optional key
// projection, never mutating the caller's slice (the kernel sorts an
// internal copy). The first callback error is latched and returned
// unchanged; a mandatory Flush on every return path makes a Terminal
// ResourceLimitError win over any pending non-Terminal callback error.
func StableSort(ctx context.Context, items []core.Value, key SortKeyFunc, less SortLessFunc) ([]core.Value, error) {
	b := core.NewBuiltinWorkBudget(ctx)
	pairs := make([]sortPair, len(items))
	for i, v := range items {
		pairs[i].val = v
		if err := b.Step(); err != nil {
			return finishSort(b, nil, err)
		}
	}

	for i := range pairs {
		if key == nil {
			pairs[i].key = pairs[i].val
			continue
		}
		k, err := key(pairs[i].val)
		if err != nil {
			return finishSort(b, nil, err)
		}
		pairs[i].key = k
		if err := b.Step(); err != nil {
			return finishSort(b, nil, err)
		}
	}

	var sortErr error
	sort.SliceStable(pairs, func(i, j int) bool {
		if sortErr != nil {
			return false
		}
		if err := b.Step(); err != nil {
			sortErr = err
			return false
		}
		ok, err := less(pairs[i].key, pairs[j].key)
		if err != nil {
			sortErr = err
			return false
		}
		if !ok {
			return false
		}
		ok, err = less(pairs[j].key, pairs[i].key)
		if err != nil {
			sortErr = err
			return false
		}
		return !ok
	})
	if sortErr != nil {
		return finishSort(b, nil, sortErr)
	}
	sorted := make([]core.Value, len(pairs))
	for i := range pairs {
		sorted[i] = pairs[i].val
	}
	return finishSort(b, sorted, nil)
}

type sortPair struct {
	key core.Value
	val core.Value
}

func finishSort(b *core.BuiltinWorkBudget, sorted []core.Value, err error) ([]core.Value, error) {
	if ferr := b.Flush(); ferr != nil && (err == nil || (core.IsTerminalEvalError(ferr) && !core.IsTerminalEvalError(err))) {
		return nil, ferr
	}
	if err != nil {
		return nil, err
	}
	return sorted, nil
}
