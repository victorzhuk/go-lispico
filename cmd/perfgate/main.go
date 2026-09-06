// Command perfgate evaluates a benchstat comparison against ADR 0008's
// consumer performance-gate thresholds (docs/adr/0008-consumer-performance-gate.md).
// It shells out to golang.org/x/perf/cmd/benchstat for the statistics and
// applies the internal/perfgate package's per-tier verdict rules. Its
// resolve-mode subcommand classifies a recorded baseline lookup into the gate
// mode those rules are applied in.
package main

import (
	"bytes"
	"encoding/json"
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
	if len(args) > 0 && args[0] == "resolve-mode" {
		return resolveMode(stdout, stderr, args[1:])
	}

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

// resolveMode classifies the baseline lookup release.yml recorded while
// searching the releases for a stored bench-vm.txt, so the gate mode comes from
// what the repository actually holds rather than from the triggering event: a
// dispatched run and a release run read the same lookup and reach the same
// mode. A lookup that could not be completed names no mode at all — exit 3,
// never a silent fall back to first-authorization thresholds.
func resolveMode(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("perfgate resolve-mode", flag.ContinueOnError)
	fs.SetOutput(stderr)
	lookupPath := fs.String("lookup", "", "JSON record of the baseline lookup: enumeration outcome, inspected tags, downloaded tag")

	if err := fs.Parse(args); err != nil {
		return exitNotComparable
	}
	if *lookupPath == "" {
		fmt.Fprintln(stderr, "perfgate: resolve-mode requires -lookup")
		return exitNotComparable
	}

	data, err := os.ReadFile(*lookupPath)
	if err != nil {
		fmt.Fprintln(stderr, "perfgate:", err)
		return exitNotComparable
	}
	var lookup perfgate.BaselineLookup
	if err := json.Unmarshal(data, &lookup); err != nil {
		fmt.Fprintf(stderr, "perfgate: parse %s: %v\n", *lookupPath, err)
		return exitNotComparable
	}

	mode, _, err := perfgate.ResolveGateMode(lookup)
	if err != nil {
		// The lookup sentinels carry the perfgate: prefix themselves.
		fmt.Fprintln(stderr, err)
		return exitNotComparable
	}

	var name string
	switch mode {
	case perfgate.ModeFirstAuthorization:
		name = "first-authorization"
	case perfgate.ModeNonRegression:
		name = "non-regression"
	default:
		fmt.Fprintln(stderr, "perfgate: the baseline lookup selected no gate mode")
		return exitNotComparable
	}

	fmt.Fprintf(stdout, "mode=%s\n", name)
	return exitPass
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
//
// Latency is only comparable within one runner, so the two raw files' runner
// identities are read first and benchstat runs only when they match. Allocated
// bytes move with the machine as well — a different allocator size class turns
// the same allocation into a different B/op — so that axis is left undecided
// on a cross-runner pair too. The allocation count is exact on any machine: it
// comes from the raw per-sample figures on both paths and stays enforced
// either way, which is what makes the two undecided axes safe.
//
// The needs-rerun signal is raised by cause, not by verdict: only an
// inconclusive cell a rerun could actually change raises it. A runner mismatch
// and a cell the stored baseline never measured are both beyond a rerun's
// reach, so they take Resolve's collapse straight away and the release is still
// authorized on the axes that were comparable.
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

	oldID, err := readRunnerIdentity(oldPath)
	if err != nil {
		return exitNotComparable, err
	}
	newID, err := readRunnerIdentity(newPath)
	if err != nil {
		return exitNotComparable, err
	}
	oldMetrics, err := readBenchmarkMetrics(oldPath)
	if err != nil {
		return exitNotComparable, err
	}
	newMetrics, err := readBenchmarkMetrics(newPath)
	if err != nil {
		return exitNotComparable, err
	}

	// The identity comparison comes before benchstat, not after it: benchstat
	// declines to pair two runs from different machines and that refusal
	// returns exit 3 before the cell loop, silently foreclosing the bytes and
	// allocs verdicts the differing runners do not invalidate.
	crossRunner := !sameRunner(oldID, newID)
	var cells map[string]perfgate.CellComparison
	if crossRunner {
		cells = cellsWithoutLatency(newMetrics)
	} else {
		csvOut, err := runBenchstat(oldPath, newPath)
		if err != nil {
			return exitNotComparable, err
		}
		cells, err = perfgate.ParseBenchstatCSV(csvOut)
		if err != nil {
			if errors.Is(err, perfgate.ErrUnpairedComparison) {
				return exitNotComparable, withBenchstatDiagnostics(err)
			}
			return exitNotComparable, err
		}
	}

	names := make([]string, 0, len(cells))
	for name := range cells {
		names = append(names, name)
	}
	sort.Strings(names)

	var bytesNotComparable string
	if crossRunner {
		bytesNotComparable = fmt.Sprintf("baseline ran on %s, candidate on %s", oldID, newID)
	}

	var report bytes.Buffer
	needsRerun := false
	anyFail := false
	for _, name := range names {
		cell := cells[name]
		cell.RaceClean = raceClean
		cell.BytesNotComparable = bytesNotComparable
		ct, ok := tiers[perfgate.TrimProcsSuffix(name)]
		if !ok {
			fmt.Fprintf(&report, "%s: FAIL no committed tier for this cell\n", name)
			anyFail = true
			continue
		}
		cell.BytesAllowanceBOp = ct.BytesAllowanceBOp

		baseline, hasBaseline := oldMetrics[name]
		if !hasBaseline {
			// Absence of a baseline figure is not a zero: judged against one,
			// every newly added cell would fail its first release. A rerun
			// regenerates only the candidate run, never the stored baseline, so
			// the figure can never appear and the cell collapses now.
			if perfgate.Resolve(ct.Tier, mode) == perfgate.VerdictFail {
				anyFail = true
			}
			fmt.Fprintf(&report, "%s: %s (no baseline figure for this cell)\n", name, perfgate.VerdictInconclusive)
			continue
		}
		// The allocation count is an exact count that survives a change of
		// runner, so both figures come from the raw per-sample data rather
		// than from benchstat, which is not consulted at all on a cross-runner
		// pair.
		cell.Bytes, cell.Allocs = perfgate.CompareSamples(baseline, newMetrics[name])

		res := perfgate.Evaluate(cell, ct.Tier, mode)
		verdict := res.Verdict
		if verdict == perfgate.VerdictInconclusive {
			switch {
			case crossRunner:
				// A rerun cannot change which machine the stored baseline came
				// from, so this collapses now instead of spending the gate's
				// most expensive step to reach the same place. The report line
				// stays inconclusive: the latency and bytes evidence is
				// genuinely absent, only the rerun is futile. A cell the
				// allocation count decided is a FAIL, never reaching this
				// branch, so it keeps the reason that decided it.
				res.Reason = fmt.Sprintf("latency and bytes not comparable: baseline ran on %s, candidate on %s", oldID, newID)
				if perfgate.Resolve(ct.Tier, mode) == perfgate.VerdictFail {
					anyFail = true
				}
			case rerun:
				verdict = perfgate.Resolve(ct.Tier, mode)
			default:
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

	// Exit precedence, worst first: 3 (already returned above — the gate could
	// not be configured, or evidence that should have paired did not) outranks
	// 1 (a cell failed) outranks 2 (a cell needs a rerun). A failure a rerun
	// cannot overturn must not be reported as one a rerun might.
	switch {
	case anyFail:
		return exitFail, nil
	case needsRerun:
		return exitNeedsRerun, nil
	default:
		return exitPass, nil
	}
}

// sameRunner licenses a latency conclusion. An unknown identity is not
// evidence that the two runs shared a machine, so it never matches — not even
// another unknown.
func sameRunner(a, b perfgate.RunnerIdentity) bool {
	return a.Known() && b.Known() && a == b
}

// cellsWithoutLatency is the cell set for a cross-runner comparison: the candidate
// run's cells with a zero Latency, which every per-tier evaluator reads as not
// statistically significant and reports inconclusive.
func cellsWithoutLatency(metrics map[string]perfgate.SampleMetrics) map[string]perfgate.CellComparison {
	cells := make(map[string]perfgate.CellComparison, len(metrics))
	for name := range metrics {
		cells[name] = perfgate.CellComparison{Name: name}
	}
	return cells
}

func readRunnerIdentity(path string) (perfgate.RunnerIdentity, error) {
	f, err := os.Open(path)
	if err != nil {
		return perfgate.RunnerIdentity{}, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	id, err := perfgate.ReadRunnerIdentity(f)
	if err != nil {
		return perfgate.RunnerIdentity{}, fmt.Errorf("%s: %w", path, err)
	}
	return id, nil
}

func readBenchmarkMetrics(path string) (map[string]perfgate.SampleMetrics, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	metrics, err := perfgate.ReadBenchmarkMetrics(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return metrics, nil
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
