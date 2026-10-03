package core

// Callable marks a value the evaluator can already call that is not a
// GoFunc, Lambda, or Keyword (the marker the VM closure carries). It is a
// classification marker only: implementing it does not add a new
// invocable kind to the evaluator's or the VM's apply dispatch.
type Callable interface {
	Value
	LispCallable()
}

// IsCallable reports whether the evaluator classifies v as callable: true
// for GoFunc, Lambda, Keyword, any value implementing the Callable marker,
// and the VM closure kind; the nil interface is always false. It does not
// add invocation dispatch for new kinds.
func IsCallable(v Value) bool {
	switch v.(type) {
	case GoFunc, Lambda, Keyword, Callable:
		return true
	default:
		return false
	}
}
