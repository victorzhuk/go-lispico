// Command perfgate evaluates a benchstat comparison against ADR 0008's
// consumer performance-gate thresholds (docs/adr/0008-consumer-performance-gate.md).
// It shells out to golang.org/x/perf/cmd/benchstat for the statistics and
// applies the internal/perfgate package's per-tier verdict rules. Its
// resolve-mode subcommand classifies a recorded baseline lookup into the gate
// mode those rules are applied in.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
	evaluatorPath := fs.String("evaluator", "", "current Evaluator samples for cells absent from -old in non-regression mode")
	tiersPath := fs.String("tiers", "internal/perfgate/tiers.json", "tier config path")
	mode := fs.String("mode", "first-authorization", "first-authorization or non-regression (design.md 'Decision rules')")
	rerun := fs.Bool("rerun", false, "this invocation is the post-rerun attempt: resolve any still-inconclusive cell instead of leaving it inconclusive")
	raceClean := fs.Bool("race-clean", false, "whether the separate untimed -race run was clean (only checked for the concurrent tier)")
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

	exitCode, err := evaluate(stdout, stderr, *oldPath, *newPath, *evaluatorPath, *tiersPath, *outPath, m, *rerun, *raceClean)
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
func evaluate(stdout, stderr io.Writer, oldPath, newPath, evaluatorPath, tiersPath, outPath string, mode perfgate.Mode, rerun, raceClean bool) (int, error) {
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
	// The candidate drives the cell loop, so a run that skipped configured
	// cells would let them exit unevaluated. An empty run is evidence the
	// gate cannot use at all — a rerun regenerates the same corpus, so this
	// is not a cell a doubled benchtime could produce. A partial run is
	// refused outright by the preflight below, never carried into verdict
	// evaluation.
	if len(newMetrics) == 0 {
		missing := perfgate.MissingCandidateCells(tiers, newMetrics)
		return exitNotComparable, fmt.Errorf("candidate run has no benchmark samples; all %d configured cells are unmeasured: %s",
			len(missing), strings.Join(missing, ", "))
	}
	// An empty sample set for one unit reads as a zero median and a silent
	// improvement against a real baseline, so the candidate evidence itself
	// is preflighted before any statistics run, before the missing-baseline
	// branch, and before any cell verdict.
	if incomplete := perfgate.IncompleteCandidateMetrics(tiers, newMetrics); len(incomplete) > 0 {
		for _, line := range incomplete {
			fmt.Fprintln(stderr, line)
		}
		return exitNotComparable, nil
	}
	if !newID.Known() {
		return exitNotComparable, errors.New("candidate run has incomplete runner identity (requires goos, goarch, cpu)")
	}
	// The allocation count is an exact count that survives a change of
	// runner, so the figures come from the raw per-sample data and are
	// derived once, before any statistics run: a present baseline cell
	// without allocation evidence must refuse the comparison here rather
	// than read as a zero median and a silent improvement.
	crossRunner := !sameRunner(oldID, newID)
	cells := cellsWithoutLatency(newMetrics)
	names := make([]string, 0, len(cells))
	for name := range cells {
		names = append(names, name)
	}
	sort.Strings(names)

	// Reference selection. First authorization stays run-wide on -old and
	// never opens -evaluator. In non-regression mode a configured cell whose
	// exact raw name the stored baseline lacks is a first authorization
	// against the current Evaluator instead: the run mode never turns a new
	// cell's absence into a verdict on no evidence. A stored baseline that
	// is present always wins, however incomplete its evidence.
	primarySelected := map[string]bool{}
	supplementalSelected := map[string]bool{}
	for _, name := range names {
		if _, ok := tiers[perfgate.TrimProcsSuffix(name)]; !ok {
			continue
		}
		if mode != perfgate.ModeNonRegression {
			continue
		}
		if _, ok := oldMetrics[name]; ok {
			primarySelected[name] = true
			continue
		}
		supplementalSelected[name] = true
	}
	supplementalNames := make([]string, 0, len(supplementalSelected))
	for name := range supplementalSelected {
		supplementalNames = append(supplementalNames, name)
	}
	sort.Strings(supplementalNames)

	// The supplemental file opens only when a configured cell needs it, and
	// is validated before any comparison, statistics, or report write: a
	// selected reference without timing or allocation evidence refuses the
	// gate rather than reading as a silent improvement. Rows no selected
	// cell names are not metric-validated.
	var evaluatorID perfgate.RunnerIdentity
	var evaluatorMetrics map[string]perfgate.SampleMetrics
	if len(supplementalNames) > 0 {
		if evaluatorPath == "" {
			return exitNotComparable, fmt.Errorf("cell %q: missing current Evaluator reference (requires -evaluator)", supplementalNames[0])
		}
		evaluatorID, err = readRunnerIdentity(evaluatorPath)
		if err != nil {
			return exitNotComparable, err
		}
		evaluatorMetrics, err = readBenchmarkMetrics(evaluatorPath)
		if err != nil {
			return exitNotComparable, err
		}
		for _, name := range supplementalNames {
			ref, ok := evaluatorMetrics[name]
			if !ok {
				return exitNotComparable, fmt.Errorf("cell %q: current Evaluator reference has no benchmark samples", name)
			}
			if ref.LatencyN == 0 {
				return exitNotComparable, fmt.Errorf("cell %q: current Evaluator reference is missing sec/op", name)
			}
			if _, _, err := perfgate.CompareSamples(ref, newMetrics[name]); err != nil {
				return exitNotComparable, fmt.Errorf("compare samples for cell %q against current Evaluator: %w", name, err)
			}
		}
	}

	for _, name := range names {
		if _, ok := tiers[perfgate.TrimProcsSuffix(name)]; !ok {
			continue
		}
		baseline, ok := oldMetrics[name]
		if supplementalSelected[name] {
			baseline, ok = evaluatorMetrics[name], true
		}
		if !ok {
			continue
		}
		bytesRes, allocsRes, err := perfgate.CompareSamples(baseline, newMetrics[name])
		if err != nil {
			return exitNotComparable, fmt.Errorf("compare samples for cell %q: %w", name, err)
		}
		cell := cells[name]
		cell.Bytes = bytesRes
		cell.Allocs = allocsRes
		cells[name] = cell
	}

	// Each reference group runs benchstat on its own pair of physical files,
	// restricted to its own names; a partitioned inventory never licenses
	// the original-path fast path, which pairedBenchstat guards by comparing
	// lengths against the complete inventories.
	var primarySelectedRef map[string]bool
	if mode == perfgate.ModeNonRegression {
		primarySelectedRef = primarySelected
	}
	if !crossRunner && (primarySelectedRef == nil || len(primarySelected) > 0) {
		parsed, err := pairedBenchstat(oldPath, newPath, oldMetrics, newMetrics, primarySelectedRef)
		if err != nil {
			if errors.Is(err, perfgate.ErrUnpairedComparison) {
				return exitNotComparable, withBenchstatDiagnostics(err)
			}
			return exitNotComparable, err
		}
		for _, name := range names {
			if primarySelectedRef != nil && !primarySelectedRef[name] {
				continue
			}
			cell, ok := parsed[name]
			if !ok {
				continue
			}
			cached := cells[name]
			cached.Latency = cell.Latency
			cells[name] = cached
		}
	}

	// The supplemental pair's comparability is decided by the Evaluator
	// identity against the candidate's, independently of whatever machine
	// the stored baseline came from.
	supplementalCrossRunner := false
	if len(supplementalNames) > 0 {
		supplementalCrossRunner = !sameRunner(evaluatorID, newID)
		if !supplementalCrossRunner {
			parsed, err := pairedBenchstat(evaluatorPath, newPath, evaluatorMetrics, newMetrics, supplementalSelected)
			if err != nil {
				if errors.Is(err, perfgate.ErrUnpairedComparison) {
					return exitNotComparable, withBenchstatDiagnostics(err)
				}
				return exitNotComparable, err
			}
			for _, name := range supplementalNames {
				cell, ok := parsed[name]
				if !ok {
					continue
				}
				cached := cells[name]
				cached.Latency = cell.Latency
				cells[name] = cached
			}
		}
	}

	var bytesNotComparable string
	if crossRunner {
		bytesNotComparable = fmt.Sprintf("baseline ran on %s, candidate on %s", oldID, newID)
	}

	var report bytes.Buffer
	needsRerun := false
	anyFail := false
	comparisons := 0
	for _, name := range names {
		cell := cells[name]
		cell.RaceClean = raceClean
		ct, ok := tiers[perfgate.TrimProcsSuffix(name)]
		if !ok {
			fmt.Fprintf(&report, "%s: FAIL no committed tier for this cell\n", name)
			anyFail = true
			continue
		}
		cellMode := mode
		supplemental := supplementalSelected[name]
		if supplemental {
			cellMode = perfgate.ResolveCellMode(mode, false)
			if supplementalCrossRunner {
				cell.BytesNotComparable = fmt.Sprintf("evaluator ran on %s, candidate on %s", evaluatorID, newID)
			}
		} else {
			cell.BytesNotComparable = bytesNotComparable
		}
		cell.BytesAllowanceBOp = ct.BytesAllowanceBOp

		if !supplemental {
			if _, hasBaseline := oldMetrics[name]; !hasBaseline {
				// Absence of a baseline figure is not a zero: judged against
				// one, every newly added cell would fail its first release.
				// A rerun regenerates only the candidate run, never the
				// stored baseline, so the figure can never appear and the
				// cell collapses now.
				if perfgate.Resolve(ct.Tier, mode) == perfgate.VerdictFail {
					anyFail = true
				}
				fmt.Fprintf(&report, "%s: %s (no baseline figure for this cell)\n", name, perfgate.VerdictInconclusive)
				continue
			}
		}
		comparisons++

		res := perfgate.Evaluate(cell, ct.Tier, cellMode)
		verdict := res.Verdict
		if verdict == perfgate.VerdictInconclusive {
			switch {
			case supplemental && supplementalCrossRunner:
				// The Evaluator's machine is as fixed as the stored
				// baseline's, so an incomparable supplemental pair collapses
				// the same way: no rerun can move it.
				res.Reason = fmt.Sprintf("latency and bytes not comparable: evaluator ran on %s, candidate on %s", evaluatorID, newID)
				if perfgate.Resolve(ct.Tier, cellMode) == perfgate.VerdictFail {
					anyFail = true
				}
			case !supplemental && crossRunner:
				// A rerun cannot change which machine the stored baseline came
				// from, so this collapses now instead of spending the gate's
				// most expensive step to reach the same place. The report line
				// stays inconclusive: the latency and bytes evidence is
				// genuinely absent, only the rerun is futile. A cell the
				// allocation count decided is a FAIL, never reaching this
				// branch, so it keeps the reason that decided it.
				res.Reason = fmt.Sprintf("latency and bytes not comparable: baseline ran on %s, candidate on %s", oldID, newID)
				if perfgate.Resolve(ct.Tier, cellMode) == perfgate.VerdictFail {
					anyFail = true
				}
			case rerun:
				verdict = perfgate.Resolve(ct.Tier, cellMode)
			default:
				needsRerun = true
			}
		}
		if verdict == perfgate.VerdictFail {
			anyFail = true
		}

		reason := res.Reason
		if supplemental {
			// The report names the reference that authorized the cell, so a
			// first authorization reads as one whatever the run's mode was.
			if reason != "" {
				reason = "mode=first-authorization reference=evaluator, " + reason
			} else {
				reason = "mode=first-authorization reference=evaluator"
			}
		}
		if reason != "" {
			fmt.Fprintf(&report, "%s: %s (%s)\n", name, verdict, reason)
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

	// A pass requires that at least one cell was actually compared against a
	// baseline figure. A run whose every cell was inconclusive for want of a
	// baseline evaluated nothing, so an aggregate pass there would authorize
	// a release on no evidence at all.
	if comparisons == 0 {
		fmt.Fprintln(stderr, "no comparison was performed")
		return exitNotComparable, nil
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
	for name, m := range metrics {
		// Latency, Old, New, and DeltaPct stay zero so every per-tier
		// evaluator still reads the cell as not significant. Only the
		// sample counts are carried so a missing metric is observable.
		cell := perfgate.CellComparison{Name: name}
		cell.Latency.N = m.LatencyN
		cell.Bytes.N = len(m.BytesPerOp)
		cell.Allocs.N = len(m.AllocsPerOp)
		cells[name] = cell
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

// pairedBenchstat runs benchstat only on the cells both inventories measured,
// so one-sided rows never reach the statistics: a benchstat CSV row for a
// cell the other run lacks is an unpaired comparison, not partial evidence.
// A non-nil selected restricts the comparison to the names it maps to true.
func pairedBenchstat(oldPath, newPath string, oldMetrics, newMetrics map[string]perfgate.SampleMetrics, selected map[string]bool) (map[string]perfgate.CellComparison, error) {
	var commonNames []string
	for name := range newMetrics {
		if _, ok := oldMetrics[name]; !ok {
			continue
		}
		if selected != nil && !selected[name] {
			continue
		}
		commonNames = append(commonNames, name)
	}
	sort.Strings(commonNames)
	if len(commonNames) == 0 {
		return map[string]perfgate.CellComparison{}, nil
	}
	if len(commonNames) == len(oldMetrics) && len(commonNames) == len(newMetrics) {
		parsed, err := runAndParse(oldPath, newPath)
		if err != nil {
			return nil, err
		}
		return parsed, requirePairedLatency(parsed, commonNames)
	}
	dir, err := os.MkdirTemp("", "perfgate-pair")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	oldFiltered := filepath.Join(dir, "old.txt")
	newFiltered := filepath.Join(dir, "candidate.txt")
	common := make(map[string]bool, len(commonNames))
	for _, name := range commonNames {
		common[name] = true
	}
	if err := filterBenchmarkFile(oldPath, oldFiltered, common); err != nil {
		return nil, err
	}
	if err := filterBenchmarkFile(newPath, newFiltered, common); err != nil {
		return nil, err
	}
	parsed, err := runAndParse(oldFiltered, newFiltered)
	if err != nil {
		return nil, err
	}
	return parsed, requirePairedLatency(parsed, commonNames)
}

func runAndParse(oldPath, newPath string) (map[string]perfgate.CellComparison, error) {
	csvOut, err := runBenchstat(oldPath, newPath)
	if err != nil {
		return nil, err
	}
	return perfgate.ParseBenchstatCSV(csvOut)
}

// requirePairedLatency refuses a cell benchstat declined to pair: without a
// sec/op row the cell would read as not significant and pass on no evidence.
func requirePairedLatency(parsed map[string]perfgate.CellComparison, commonNames []string) error {
	for _, name := range commonNames {
		cell, ok := parsed[name]
		if !ok || cell.Latency.N <= 0 {
			return fmt.Errorf("cell %q: missing paired sec/op comparison", name)
		}
	}
	return nil
}

// filterBenchmarkFile copies srcPath to dstPath keeping only the benchmark
// result lines whose name is in common. Every other byte — metadata, blank
// lines, CRLF endings — is preserved so benchstat sees the same units and
// configuration it would from the original file.
func filterBenchmarkFile(srcPath, dstPath string, common map[string]bool) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	dst, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	reader := bufio.NewReader(src)
	for {
		line, readErr := reader.ReadString('\n')
		keep := true
		fields := strings.Fields(line)
		if len(fields) >= 4 && strings.HasPrefix(fields[0], "Benchmark") {
			keep = common[strings.TrimPrefix(fields[0], "Benchmark")]
		}
		if keep {
			if _, err := dst.WriteString(line); err != nil {
				_ = dst.Close()
				return err
			}
		}
		if readErr != nil {
			_ = dst.Close()
			if !errors.Is(readErr, io.EOF) {
				return readErr
			}
			return nil
		}
	}
}
