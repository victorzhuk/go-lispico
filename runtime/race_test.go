//go:build race

package runtime

// raceEnabled is true when the test binary is built with -race. The race
// detector's own shadow-memory bookkeeping perturbs testing.AllocsPerRun
// counts non-deterministically, so alloc-count assertions skip under it.
const raceEnabled = true
