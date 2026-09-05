package perfgate

import (
	"errors"
	"io"
)

// ErrInconsistentPreamble reports a raw bench file whose repeated preambles
// disagree. The release workflow appends one sample run per iteration, so two
// different CPUs in one file mean the samples did not all run on one machine.
var ErrInconsistentPreamble = errors.New("perfgate: inconsistent benchmark preamble")

// RunnerIdentity is the machine a raw bench-*.txt was produced on. pkg is
// excluded on purpose: it names the benchmarked package, not the runner, so a
// package rename would otherwise read as a hardware change.
type RunnerIdentity struct {
	GOOS   string
	GOARCH string
	CPU    string
}

// Known reports whether the file carried a complete identity.
func (RunnerIdentity) Known() bool { panic("not implemented") }

// String renders goos/goarch/cpu, with "unknown" in the position of any field
// the file did not carry.
func (RunnerIdentity) String() string { panic("not implemented") }

// ReadRunnerIdentity reads the goos/goarch/cpu preamble out of raw
// `go test -bench` output. benchstat is not a usable source: it drops the cpu:
// line under -ignore and reports only one group's preamble otherwise.
func ReadRunnerIdentity(r io.Reader) (RunnerIdentity, error) { panic("not implemented") }
