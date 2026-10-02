package core

// Callable — a callable value that is not a GoFunc, Lambda, or Keyword.
type Callable interface {
	Value
	LispCallable()
}

// IsCallable reports whether the evaluator can call v: true for GoFunc,
// Lambda, Keyword, any value implementing Callable, and the nil interface
// is always false.
func IsCallable(v Value) bool {
	switch v.(type) {
	case GoFunc, Lambda, Keyword, Callable:
		return true
	default:
		return false
	}
}
