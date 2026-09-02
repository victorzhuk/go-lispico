package core

// EqualsBounded compares a and b structurally, charging one unit of budget per
// compared node so a deep comparison stays preemptible. The budget's error is
// returned unchanged: settling it against a builtin's own error precedence is
// the caller's job.
func EqualsBounded(a, b Value, budget *BuiltinWorkBudget) (bool, error) {
	if err := budget.Step(); err != nil {
		return false, err
	}
	switch av := a.(type) {
	case nil:
		return b == nil, nil
	case List:
		bv, ok := b.(List)
		if !ok || av.Len() != bv.Len() {
			return false, nil
		}
		ac, bc := av.cursor(), bv.cursor()
		for {
			x, more := ac.next()
			if !more {
				return true, nil
			}
			y, _ := bc.next()
			eq, err := EqualsBounded(x, y, budget)
			if err != nil {
				return false, err
			}
			if !eq {
				return false, nil
			}
		}
	case Vector:
		bv, ok := b.(Vector)
		if !ok || av.Len() != bv.Len() {
			return false, nil
		}
		for i := 0; i < av.Len(); i++ {
			eq, err := EqualsBounded(av.At(i), bv.At(i), budget)
			if err != nil {
				return false, err
			}
			if !eq {
				return false, nil
			}
		}
		return true, nil
	case *HashMap:
		bv, ok := b.(*HashMap)
		if !ok || av.Len() != bv.Len() {
			return false, nil
		}
		equal := true
		var walkErr error
		av.eachRaw(func(e entry) {
			if !equal || walkErr != nil {
				return
			}
			other, found := bv.getByHashKey(e.hk)
			if !found {
				equal = false
				return
			}
			eq, err := EqualsBounded(e.v, other, budget)
			if err != nil {
				walkErr = err
				return
			}
			equal = eq
		})
		if walkErr != nil {
			return false, walkErr
		}
		return equal, nil
	default:
		// An arbitrary host Value.Equals is a trusted-host boundary the runtime
		// cannot preempt, so it is not stepped: charging inside it would
		// attribute host work to our budget.
		return a.Equals(b), nil
	}
}
