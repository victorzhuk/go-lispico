package core

// Callable — a callable value that is not a GoFunc, Lambda, or Keyword.
type Callable interface {
	Value
	LispCallable()
}

// IsCallable reports whether the evaluator can call v. The classification
// of callable kinds lands with the chunk that implements the adapters.
func IsCallable(v Value) bool {
	return false
}
