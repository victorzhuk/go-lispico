package collections

import (
	"fmt"

	"github.com/victorzhuk/go-lispico/core"
)

func ToFloat(name string, v core.Value) (float64, error) {
	switch n := v.(type) {
	case core.Int:
		return float64(n.V), nil
	case core.Float:
		return n.V, nil
	default:
		return 0, typeErrorf("%s: expected number, got %T", name, v)
	}
}

// NumCmp compares two numbers, returning -1, 0, or 1. An int-int pair is
// compared exactly; a mixed pair promotes to float like arithmetic does.
func NumCmp(name string, a, b core.Value) (int, error) {
	ai, aInt := a.(core.Int)
	bi, bInt := b.(core.Int)
	if aInt && bInt {
		switch {
		case ai.V < bi.V:
			return -1, nil
		case ai.V > bi.V:
			return 1, nil
		}
		return 0, nil
	}

	af, err := ToFloat(name, a)
	if err != nil {
		return 0, err
	}
	bf, err := ToFloat(name, b)
	if err != nil {
		return 0, err
	}
	switch {
	case af < bf:
		return -1, nil
	case af > bf:
		return 1, nil
	}
	return 0, nil
}

// NaturalCmp orders two values of the same kind: numbers by NumCmp, strings
// and keywords lexicographically. Mixed kinds (beyond int/float) are an error.
func NaturalCmp(a, b core.Value) (int, error) {
	if as, ok := a.(core.String); ok {
		bs, ok := b.(core.String)
		if !ok {
			return 0, fmt.Errorf("sort: cannot compare %T with %T", a, b)
		}
		switch {
		case as.V < bs.V:
			return -1, nil
		case as.V > bs.V:
			return 1, nil
		}
		return 0, nil
	}
	if ak, ok := a.(core.Keyword); ok {
		bk, ok := b.(core.Keyword)
		if !ok {
			return 0, fmt.Errorf("sort: cannot compare %T with %T", a, b)
		}
		switch {
		case ak.V < bk.V:
			return -1, nil
		case ak.V > bk.V:
			return 1, nil
		}
		return 0, nil
	}
	return NumCmp("sort", a, b)
}
