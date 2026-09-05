// Command perfgate evaluates a benchstat comparison against ADR 0008's
// consumer performance-gate thresholds (docs/adr/0008-consumer-performance-gate.md).
// It shells out to golang.org/x/perf/cmd/benchstat for the statistics and
// applies the internal/perfgate package's per-tier verdict rules.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/victorzhuk/go-lispico/internal/perfgate"
)

// Process exit codes. Exit 2 is reserved for the needs-rerun signal
// release.yml acts on by rerunning at doubled benchtime, so every failure that
// a rerun cannot resolve — an unusable tier config, benchstat declining to
// pair the two runs — exits 3 instead of costing that pointless rerun.
const (
	exitPass          = 0
	exitFail          = 1
	exitNeedsRerun    = 2
	exitNotComparable = 3
)

func main() {
	os.Exit(run(os.Stdout, os.Stderr, os.Args[1:]))
}

func run(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("perfgate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	oldPath := fs.String("old", "", "benchstat-format input: reference run (Evaluator mode, or a stored VM baseline in non-regression mode)")
	newPath := fs.String("candidate", "", "benchstat-format input: candidate run (VM mode)")
	tiersPath := fs.String("tiers", "internal/perfgate/tiers.json", "tier config path")
	mode := fs.String("mode", "first-authorization", "first-authorization or non-regression (design.md 'Decision rules')")
	rerun := fs.Bool("rerun", false, "this invocation is the post-rerun attempt: resolve any still-inconclusive cell instead of leaving it inconclusive")
	// Default true means the concurrent tier's race gate is effectively
	// always-pass at this CLI layer unless the caller explicitly passes
	// -race-clean=false. release.yml never passes this flag today — it
	// relies entirely on the earlier race-suite step having already halted
	// the job on any race failure, so this is not yet an active check from
	// CI, just a manual override.
	raceClean := fs.Bool("race-clean", true, "whether the separate untimed -race run was clean (only checked for the concurrent tier)")
	outPath := fs.String("out", "", "verdict output path; empty writes to stdout")

	if err := fs.Parse(args); err != nil {
		return exitNotComparable
	}
	if *oldPath == "" || *newPath == "" {
		fmt.Fprintln(stderr, "perfgate: -old and -candidate are required")
		return exitNotComparable
	}

	m, err := parseMode(*mode)
	if err != nil {
		fmt.Fprintln(stderr, "perfgate:", err)
		return exitNotComparable
	}

	exitCode, err := evaluate(stdout, *oldPath, *newPath, *tiersPath, *outPath, m, *rerun, *raceClean)
	if err != nil {
		fmt.Fprintln(stderr, "perfgate:", err)
	}
	return exitCode
}

func parseMode(s string) (perfgate.Mode, error) {
	switch s {
	case "first-authorization":
		return perfgate.ModeFirstAuthorization, nil
	case "non-regression":
		return perfgate.ModeNonRegression, nil
	default:
		return perfgate.ModeUnknown, fmt.Errorf("unknown -mode %q, want first-authorization or non-regression", s)
	}
}

// evaluate runs benchstat, evaluates every cell, writes the verdict report,
// and returns the process exit code: 0 all pass, 1 any fail, 2 any cell is
// still inconclusive and needs a rerun, 3 the gate could not be configured or
// the two runs could not be paired. release.yml's "Rerun paired benchmark at
// doubled benchtime" / "Resolve inconclusive cells after rerun" steps
// distinguish exit 2 from exit 1: they rerun once at doubled benchtime and
// re-invoke this command with -rerun to resolve it. Exit 3 stays out of that
// path — no rerun turns an unusable config or an unpaired comparison into
// evidence.
func evaluate(stdout io.Writer, oldPath, newPath, tiersPath, outPath string, mode perfgate.Mode, rerun, raceClean bool) (int, error) {
	tiersFile, err := os.Open(tiersPath)
	if err != nil {
		return exitNotComparable, fmt.Errorf("open tier config: %w", err)
	}
	defer func() { _ = tiersFile.Close() }()
	tiers, err := perfgate.LoadTierConfig(tiersFile)
	if err != nil {
		return exitNotComparable, err
	}

	csvOut, err := runBenchstat(oldPath, newPath)
	if err != nil {
		return exitNotComparable, err
	}
	cells, err := perfgate.ParseBenchstatCSV(csvOut)
	if err != nil {
		if errors.Is(err, perfgate.ErrUnpairedComparison) {
			return exitNotComparable, withBenchstatDiagnostics(err)
		}
		return exitNotComparable, err
	}

	names := make([]string, 0, len(cells))
	for name := range cells {
		names = append(names, name)
	}
	sort.Strings(names)

	var report bytes.Buffer
	needsRerun := false
	anyFail := false
	for _, name := range names {
		cell := cells[name]
		cell.RaceClean = raceClean
		ct, ok := tiers[perfgate.TrimProcsSuffix(name)]
		if !ok {
			fmt.Fprintf(&report, "%s: FAIL no committed tier for this cell\n", name)
			anyFail = true
			continue
		}
		cell.BytesAllowanceBOp = ct.BytesAllowanceBOp

		res := perfgate.Evaluate(cell, ct.Tier, mode)
		verdict := res.Verdict
		if verdict == perfgate.VerdictInconclusive {
			if rerun {
				verdict = perfgate.Resolve(ct.Tier, mode)
			} else {
				needsRerun = true
			}
		}
		if verdict == perfgate.VerdictFail {
			anyFail = true
		}

		if res.Reason != "" {
			fmt.Fprintf(&report, "%s: %s (%s)\n", name, verdict, res.Reason)
		} else {
			fmt.Fprintf(&report, "%s: %s\n", name, verdict)
		}
	}

	if outPath == "" {
		if _, err := stdout.Write(report.Bytes()); err != nil {
			return exitNotComparable, err
		}
	} else if err := os.WriteFile(outPath, report.Bytes(), 0o644); err != nil {
		return exitNotComparable, fmt.Errorf("write verdict report: %w", err)
	}

	switch {
	case anyFail:
		return exitFail, nil
	case needsRerun:
		return exitNeedsRerun, nil
	default:
		return exitPass, nil
	}
}

// runBenchstat is a variable so tests can substitute committed benchstat
// output; production keeps the argv at exactly `-format csv <old> <new>`,
// which is what makes a refusal to pair observable instead of papered over.
var runBenchstat = func(oldPath, newPath string) ([]byte, error) {
	cmd := exec.Command("go", "run", "golang.org/x/perf/cmd/benchstat@v0.0.0-20260709024250-82a0b07e230d", "-format", "csv", oldPath, newPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("benchstat: %w: %s", err, stderr.String())
		}
		return nil, fmt.Errorf("run benchstat: %w", err)
	}
	benchstatStderr = stderr.String()
	return stdout.Bytes(), nil
}

// benchstatStderr holds what benchstat wrote to stderr while exiting 0. A run
// it declined to pair is a successful run by the exit code, so this is the
// only place its own words survive.
var benchstatStderr string

// withBenchstatDiagnostics appends benchstat's stderr to a pairing refusal for
// the operator's benefit. It is never the reason: on a declined pairing
// benchstat reports only that it cannot compute a geomean, which is about the
// summary row and says nothing about the two runs being incomparable.
func withBenchstatDiagnostics(err error) error {
	diag := strings.TrimSpace(benchstatStderr)
	if diag == "" {
		return err
	}
	return fmt.Errorf("%w; benchstat exited 0 and reported: %s", err, diag)
}
