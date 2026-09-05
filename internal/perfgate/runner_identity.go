package perfgate

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
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
func (id RunnerIdentity) Known() bool {
	return id.GOOS != "" && id.GOARCH != "" && id.CPU != ""
}

// String renders goos/goarch/cpu, with "unknown" in the position of any field
// the file did not carry.
func (id RunnerIdentity) String() string {
	return orUnknown(id.GOOS) + "/" + orUnknown(id.GOARCH) + "/" + orUnknown(id.CPU)
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

// ReadRunnerIdentity reads the goos/goarch/cpu preamble out of raw
// `go test -bench` output. benchstat is not a usable source: it drops the cpu:
// line under -ignore and reports only one group's preamble otherwise.
func ReadRunnerIdentity(r io.Reader) (RunnerIdentity, error) {
	var id RunnerIdentity
	// A raw bench file repeats its preamble once per appended sample run; every
	// repetition is compared, since a mid-run machine swap only shows up in a
	// later one. Values are trimmed first: the AMD runner pads cpu: with
	// trailing spaces, and that padding is not a hardware difference.
	keys := []struct {
		prefix string
		dst    *string
	}{
		{"goos:", &id.GOOS},
		{"goarch:", &id.GOARCH},
		{"cpu:", &id.CPU},
	}

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		for _, k := range keys {
			if !strings.HasPrefix(line, k.prefix) {
				continue
			}
			value := strings.TrimSpace(strings.TrimPrefix(line, k.prefix))
			if *k.dst != "" && *k.dst != value {
				return RunnerIdentity{}, fmt.Errorf("%w: %s %q then %q",
					ErrInconsistentPreamble, strings.TrimSuffix(k.prefix, ":"), *k.dst, value)
			}
			*k.dst = value
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return RunnerIdentity{}, fmt.Errorf("perfgate: read runner identity: %w", err)
	}
	return id, nil
}
