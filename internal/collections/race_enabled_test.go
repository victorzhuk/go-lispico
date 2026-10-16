//go:build race

package collections

// raceEnabled reports whether the race detector is active. The detector's
// shadow memory inflates measured allocation bytes, so absolute alloc bounds
// are skipped under -race; see the same exclusion in
// internal/goldset/alloc_test.go.
const raceEnabled = true
