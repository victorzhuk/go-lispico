package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/victorzhuk/go-lispico/internal/perfgate"
)

const (
	// The two raw files record the same runner identity, so the pair stays
	// one benchstat would be willing to compare: the unpaired shape must come
	// from the injected benchstat output, never from the inputs themselves.
	pinnedProfileDir = "../../internal/perfgate/testdata/profile-30637802780"
	committedTiers   = "../../internal/perfgate/tiers.json"
	unpairedFixture  = "../../internal/perfgate/testdata/unpaired-single-group.csv"
)

func TestRun_UnpairedComparisonExitsThree(t *testing.T) {
	csvData, err := os.ReadFile(unpairedFixture)
	require.NoError(t, err, "single-group fixture is missing")
	swapBenchstat(t, func(_, _ string) ([]byte, error) { return csvData, nil })

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"-old", filepath.Join(pinnedProfileDir, "bench-evaluator.txt"),
		"-candidate", filepath.Join(pinnedProfileDir, "bench-vm.txt"),
		"-tiers", committedTiers,
	})

	assert.Equal(t, 3, code, "unpaired comparison must exit 3; stderr: %s", stderr.String())
	assert.Contains(t, stderr.String(), "single-group",
		"stderr does not give the pairing refusal as the reason: %s", stderr.String())
}

func TestRun_ConfigErrorExitsThree(t *testing.T) {
	swapBenchstat(t, func(_, _ string) ([]byte, error) {
		assert.Fail(t, "benchstat ran despite an unusable tier config")
		return nil, errors.New("benchstat must not run")
	})

	tiersPath := filepath.Join(t.TempDir(), "tiers.json")
	require.NoError(t, os.WriteFile(tiersPath, []byte(`{"cells":{"Goldset/counter-closure":"not-a-tier"}}`), 0o644))

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"-old", filepath.Join(pinnedProfileDir, "bench-evaluator.txt"),
		"-candidate", filepath.Join(pinnedProfileDir, "bench-vm.txt"),
		"-tiers", tiersPath,
	})

	assert.Equal(t, 3, code, "configuration error must exit 3, not the needs-rerun 2; stderr: %s", stderr.String())
}

// TestRun_EmptyCandidateExitsThree pins the gate's floor: a candidate run
// with no benchmark samples at all is evidence of nothing, and evaluating an
// empty cell set reaches the pass branch without ever comparing a figure.
// The pair is cross-runner so the swapped benchstat stays unreached: the
// inventory comparison must reject the candidate before any statistics run.
func TestRun_EmptyCandidateExitsThree(t *testing.T) {
	emptyCandidate := keepingCells(t, filepath.Join(crossRunnerBaselineDir, "bench-vm.txt"), "BenchmarkNone/")

	code, _, stderr := runGate(t,
		"-old", filepath.Join(pinnedProfileDir, "bench-vm.txt"),
		"-candidate", emptyCandidate,
		"-mode", "non-regression")

	assert.Equal(t, 3, code, "an empty candidate must never authorize a release; stderr: %s", stderr)
	assert.Contains(t, stderr, "Goldset/route-decision",
		"the diagnostic must name the configured cells the candidate never measured: %s", stderr)
}

// TestRun_CandidateMissingOneCellFailsGate covers a candidate that measured
// all but one configured cell. The evidence preflight rejects the run before
// any statistics run or cell verdict: an unmeasured cell is not a new
// baseline cell, tiers.json already commits a tier for it, and a rerun
// regenerates the same corpus, so the gate exits 3 outright.
func TestRun_CandidateMissingOneCellFailsGate(t *testing.T) {
	partial := droppingCells(t, filepath.Join(pinnedProfileDir, "bench-vm.txt"), "BenchmarkGoldset/route-decision")

	code, stdout, stderr := runGate(t,
		"-old", filepath.Join(crossRunnerBaselineDir, "bench-vm.txt"),
		"-candidate", partial,
		"-mode", "non-regression")

	assert.Equal(t, 3, code, "a cell the tier config demands that the candidate never measured must exit 3 before any verdict; stderr: %s", stderr)
	assert.Contains(t, stderr, "Goldset/route-decision (missing candidate samples)",
		"stderr must name the unmeasured cell as missing candidate evidence: %s", stderr)
	assert.Equal(t, "", stdout, "the preflight must return before any cell verdict is written")
}

func swapBenchstat(t *testing.T, fn func(oldPath, newPath string) ([]byte, error)) {
	t.Helper()

	prev := runBenchstat
	runBenchstat = fn
	t.Cleanup(func() { runBenchstat = prev })
}

// TestSameRunner_BytesStillEnforced is the case the narrowing leaves alone.
// Both runs came off the pinned profile's own runner, so their B/op figures
// are comparable and the non-increasing bytes bound still decides every cell
// that moved on it. Roles are reversed against the profile's own direction --
// the Evaluator arm is the candidate -- so the bytes axis has a real
// regression to fail on. The failing set is a stated partition, not the whole
// corpus: guard-nil's bytes fall by one and the thirteen reader cells are
// bit-identical, so fourteen cells stay clean.
func TestSameRunner_BytesStillEnforced(t *testing.T) {
	csvData, err := os.ReadFile(filepath.Join(pinnedProfileDir, "benchstat.csv"))
	require.NoError(t, err)
	swapBenchstat(t, func(_, _ string) ([]byte, error) { return csvData, nil })

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"-old", filepath.Join(pinnedProfileDir, "bench-vm.txt"),
		"-candidate", filepath.Join(pinnedProfileDir, "bench-evaluator.txt"),
		"-tiers", committedTiers,
		"-mode", "non-regression",
	})

	names := candidateCells(t)
	wantFail := append(withoutCell(cellsWithPrefix(t, names, "Goldset/", 13), "Goldset/guard-nil-2"), callBoundaryCell)
	sort.Strings(wantFail)

	lines := reportLines(t, stdout.String())
	var gotFail []string
	for _, name := range names {
		line, ok := lines[name]
		require.True(t, ok, "cell %q missing from the report:\n%s", name, stdout.String())
		if line.verdict != "FAIL" {
			continue
		}
		gotFail = append(gotFail, name)
		assert.Contains(t, line.reason, "bytes increased", "%s must fail on the axis it was decided on", name)
	}

	assert.Equal(t, wantFail, gotFail, "the bytes bound decides exactly the cells whose B/op rose")
	assert.Equal(t, 1, code, "a bytes regression between two runs of one runner fails the gate; stderr: %s", stderr.String())
}

const (
	// crossRunnerBaselineDir is a real stored VM baseline from a different
	// runner than pinnedProfileDir's -- the post-authorization shape the gate
	// meets on every later release.
	crossRunnerBaselineDir = "../../internal/perfgate/testdata/profile-30614184386"
	// callBoundaryCell exists in pinnedProfileDir and not in
	// crossRunnerBaselineDir, so it has no baseline figure to be judged against.
	callBoundaryCell = "GoldsetCall/call-boundary-2"
)

// TestCrossRunner_AllocsEnforcedBytesInconclusive pairs two committed VM runs
// from different runners. Neither latency nor allocated bytes is evidence
// across a hardware change, so both axes are undecided and the report names
// both runners. An allocation count is exact on any machine and stays
// enforced, which the swapped-roles subtests prove by failing on that axis and
// leaving the bytes axis undecided on the cells where nothing else moved.
func TestCrossRunner_AllocsEnforcedBytesInconclusive(t *testing.T) {
	amdRun := filepath.Join(pinnedProfileDir, "bench-vm.txt")
	intelRun := filepath.Join(crossRunnerBaselineDir, "bench-vm.txt")
	amd := identityOf(t, amdRun)
	intel := identityOf(t, intelRun)
	require.NotEqual(t, amd, intel, "the two committed profiles no longer disagree on the runner")
	// The committed Intel corpus predates GoldsetCall/call-boundary, so the
	// reversed-role runs drop that one cell from the config instead of
	// borrowing its rows from another runner's capture.
	tiers26 := tiersWithoutCallBoundary(t)

	t.Run("latency inconclusive naming both runners", func(t *testing.T) {
		// The candidate's own capture pair supplies the supplemental
		// reference for the call-boundary cell the Intel corpus predates:
		// same runner as the candidate, so that one cell now earns a
		// first-authorization verdict from real statistics while every
		// baseline-backed cell stays undecided across the hardware change.
		evaluatorRun := filepath.Join(pinnedProfileDir, "bench-evaluator.txt")
		swapBenchstat(t, func(_, _ string) ([]byte, error) {
			return []byte(pairedCSV(identityOf(t, amdRun),
				"GoldsetCall/call-boundary-2,9.747499999999999e-07,1%,1.8850000000000003e-07,1%,-80.66%,p=0.000 n=10")), nil
		})
		var outBuf, errBuf bytes.Buffer
		code := run(&outBuf, &errBuf, []string{
			"-tiers", committedTiers,
			"-old", intelRun, "-evaluator", evaluatorRun, "-candidate", amdRun,
			"-mode", "non-regression",
		})
		stdout, stderr := outBuf.String(), errBuf.String()

		out := stdout + stderr
		assert.Contains(t, out, intel, "the cross-runner report must name the baseline runner")
		assert.Contains(t, out, amd, "the cross-runner report must name the candidate runner")

		names := candidateCells(t)
		want := uniformVerdicts(names, "INCONCLUSIVE")
		want[callBoundaryCell] = "PASS"
		assert.Equal(t, want, verdictsFor(t, stdout, names),
			"a latency conclusion across differing runners is not licensed; only the same-capture Evaluator-backed cell is decided")
		assert.Equal(t, 0, code, "a rerun cannot change which machine the stored baseline came from, so a runner mismatch raises no needs-rerun signal and the release stays authorized on the comparable bytes and allocs axes; stderr: %s", stderr)
	})

	// Roles swapped for both subtests below: the candidate is the Intel run,
	// which reads higher B/op on every cell and one more allocation on each
	// Goldset/ cell. The two axes part company there -- the reader cells move
	// on bytes alone.
	t.Run("bytes inconclusive across differing runners", func(t *testing.T) {
		_, stdout, stderr := runGateWithTiers(t, tiers26, "-old", amdRun, "-candidate", intelRun, "-mode", "non-regression")

		parseCells := cellsWithPrefix(t, candidateCells(t), "GoldsetParse/", 13)
		assert.Equal(t, uniformVerdicts(parseCells, "INCONCLUSIVE"), verdictsFor(t, stdout, parseCells),
			"a B/op difference between two machines is an artifact of the machine, not evidence about the change; stderr: %s", stderr)

		wantReason := fmt.Sprintf("latency and bytes not comparable: baseline ran on %s, candidate on %s", amd, intel)
		lines := reportLines(t, stdout)
		var unexplained []string
		for _, name := range parseCells {
			if lines[name].reason != wantReason {
				unexplained = append(unexplained, name+": "+lines[name].reason)
			}
		}
		assert.Len(t, lines, 26, "dropping the cell from the config must drop its report line too")
		assert.NotContains(t, stdout, "GoldsetCall/call-boundary", "a cell the config omits has no verdict to report")
	})

	t.Run("allocation counts still enforced across differing runners", func(t *testing.T) {
		code, stdout, stderr := runGateWithTiers(t, tiers26, "-old", amdRun, "-candidate", intelRun, "-mode", "non-regression")

		goldsetCells := cellsWithPrefix(t, candidateCells(t), "Goldset/", 13)
		assert.Equal(t, uniformVerdicts(goldsetCells, "FAIL"), verdictsFor(t, stdout, goldsetCells),
			"an allocation count is exact on any runner, so it still decides the cell")

		lines := reportLines(t, stdout)
		var unexplained []string
		for _, name := range goldsetCells {
			if !strings.Contains(lines[name].reason, "allocs increased") {
				unexplained = append(unexplained, name+": "+lines[name].reason)
			}
		}
		assert.Empty(t, unexplained, "a decided allocs failure keeps its own reason instead of the runner-mismatch sentence")
		assert.Equal(t, 1, code, "an allocation regression must fail the gate even when latency and bytes are inconclusive; stderr: %s", stderr)
		assert.Len(t, lines, 26, "dropping the cell from the config must drop its report line too")
		assert.NotContains(t, stdout, "GoldsetCall/call-boundary", "a cell the config omits has no verdict to report")
	})
}

// TestCrossRunner_CleanRunExitsZero is the release the narrowing exists to let
// through: a cross-runner pair whose allocation counts all held, with only the
// two machine-dependent axes left undecided. Nothing there is a rerun's to
// resolve, so every cell collapses and the gate authorizes the release. The
// candidate must still carry every configured cell: the committed Intel
// profile's identity, stamped onto the complete AMD run, gives a known
// different runner without dropping any cell from the inventory.
func TestCrossRunner_CleanRunExitsZero(t *testing.T) {
	intelFile := filepath.Join(crossRunnerBaselineDir, "bench-vm.txt")
	intelCPU, err := func() (string, error) {
		f, err := os.Open(intelFile)
		if err != nil {
			return "", err
		}
		defer func() { _ = f.Close() }()
		id, err := perfgate.ReadRunnerIdentity(f)
		if err != nil {
			return "", err
		}
		return id.CPU, nil
	}()
	require.NoError(t, err)
	cleanCandidate := withCPULines(t, filepath.Join(pinnedProfileDir, "bench-vm.txt"), intelCPU)
	require.NotEqual(t, intelCPU, identityOf(t, filepath.Join(pinnedProfileDir, "bench-vm.txt")))

	code, stdout, stderr := runGate(t, "-old", filepath.Join(pinnedProfileDir, "bench-vm.txt"), "-candidate", cleanCandidate, "-mode", "non-regression")

	parseCells := cellsWithPrefix(t, candidateCells(t), "GoldsetParse/", 13)
	assert.Equal(t, uniformVerdicts(parseCells, "INCONCLUSIVE"), verdictsFor(t, stdout, parseCells),
		"the reader cells hold their allocation counts, so only the undecided axes are left")
	assert.Equal(t, 0, code, "a cross-runner run whose allocation counts all held authorizes the release; stderr: %s", stderr)
}

// withCPULines copies a committed raw bench file into the test's temp dir with
// its cpu: lines replaced by the given cpu value, keeping every benchmark line
// verbatim. The committed corpora are never edited.
func withCPULines(t *testing.T, path, cpu string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var kept []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "cpu:") {
			kept = append(kept, "cpu: "+cpu)
			continue
		}
		kept = append(kept, line)
	}

	out := filepath.Join(t.TempDir(), "bench-recpu.txt")
	require.NoError(t, os.WriteFile(out, []byte(strings.Join(kept, "\n")), 0o644))
	return out
}

// tiersWithoutCallBoundary copies the committed tier config with only the
// GoldsetCall/call-boundary entry dropped from both maps, so a candidate
// corpus that predates the cell can still be judged. The committed config is
// never edited.
func tiersWithoutCallBoundary(t *testing.T) string {
	t.Helper()

	data, err := os.ReadFile(committedTiers)
	require.NoError(t, err)

	var cfg struct {
		Cells             map[string]string  `json:"cells"`
		BytesAllowanceBOp map[string]float64 `json:"bytesAllowanceBOp"`
	}
	require.NoError(t, json.Unmarshal(data, &cfg))
	delete(cfg.Cells, "GoldsetCall/call-boundary")
	delete(cfg.BytesAllowanceBOp, "GoldsetCall/call-boundary")
	require.Len(t, cfg.Cells, 26, "committed tiers.json gained cells beyond the known 27")
	require.Len(t, cfg.BytesAllowanceBOp, 26, "committed tiers.json gained allowances beyond the known 27")

	out, err := json.Marshal(cfg)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "tiers-26.json")
	require.NoError(t, os.WriteFile(path, out, 0o644))
	return path
}

// TestCrossRunner_UnknownIdentityIsNotAMatch feeds a baseline whose preamble
// carries no cpu: line. An unknown identity is not evidence that the two runs
// shared a runner, so it must not license a latency conclusion either -- and
// the side that is unknown has to be named as such.
func TestCrossRunner_UnknownIdentityIsNotAMatch(t *testing.T) {
	candidate := filepath.Join(pinnedProfileDir, "bench-vm.txt")
	baseline := withoutCPULines(t, candidate)

	code, stdout, stderr := runGate(t, "-old", baseline, "-candidate", candidate, "-mode", "non-regression")

	out := stdout + stderr
	assert.Contains(t, out, identityOf(t, baseline), "the report must name the unknown side as unknown")
	assert.Contains(t, out, identityOf(t, candidate), "the report must name the known side")

	names := candidateCells(t)
	assert.Equal(t, uniformVerdicts(names, "INCONCLUSIVE"), verdictsFor(t, stdout, names),
		"an unknown identity must not be read as a match")
	assert.Equal(t, 0, code, "an unknown identity is a runner mismatch no rerun can resolve, so it raises no needs-rerun signal and the release stays authorized on the comparable bytes and allocs axes; stderr: %s", stderr)
}

// TestCrossRunner_MissingBaselineCellIsInconclusive covers a cell the candidate
// measures and the baseline never did. Under the supplemental-reference
// contract that cell is judged against the current Evaluator from the
// candidate's own capture pair, so it earns a first-authorization verdict
// instead of reading the absent baseline figure as evidence.
func TestCrossRunner_MissingBaselineCellIsInconclusive(t *testing.T) {
	candidate := filepath.Join(pinnedProfileDir, "bench-vm.txt")
	baseline := filepath.Join(crossRunnerBaselineDir, "bench-vm.txt")
	evaluator := filepath.Join(pinnedProfileDir, "bench-evaluator.txt")

	swapBenchstat(t, func(_, _ string) ([]byte, error) {
		return []byte(pairedCSV(identityOf(t, candidate),
			"GoldsetCall/call-boundary-2,9.747499999999999e-07,1%,1.8850000000000003e-07,1%,-80.66%,p=0.000 n=10")), nil
	})
	var outBuf, errBuf bytes.Buffer
	run(&outBuf, &errBuf, []string{
		"-tiers", committedTiers,
		"-old", baseline, "-evaluator", evaluator, "-candidate", candidate,
		"-mode", "non-regression",
	})

	lines := reportLines(t, outBuf.String())
	line, ok := lines[callBoundaryCell]
	require.True(t, ok, "cell %q missing from the report:\n%s", callBoundaryCell, outBuf.String())
	assert.Equal(t, "PASS", line.verdict, "the same-capture Evaluator authorizes the cell the stored baseline never measured")
	assert.Equal(t, "mode=first-authorization reference=evaluator", line.reason,
		"the supplemental verdict must name its first-authorization mode and evaluator reference")
}

type reportLine struct {
	verdict string
	reason  string
}

// reportLines parses cmd/perfgate's report format ("name: VERDICT" or
// "name: VERDICT (reason)"), keyed by the full cell name, suffix included.
func reportLines(t *testing.T, report string) map[string]reportLine {
	t.Helper()

	lines := make(map[string]reportLine)
	for _, raw := range strings.Split(strings.TrimSpace(report), "\n") {
		name, rest, ok := strings.Cut(raw, ": ")
		if !ok {
			continue
		}
		line := reportLine{verdict: rest}
		if i := strings.Index(rest, " ("); i >= 0 {
			line.verdict = rest[:i]
			line.reason = strings.TrimSuffix(rest[i+2:], ")")
		}
		lines[name] = line
	}
	return lines
}

// candidateCells is the cell set of pinnedProfileDir's runs, taken from the
// committed benchstat.csv so the expectation does not come from the reader
// under test.
func candidateCells(t *testing.T) []string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(pinnedProfileDir, "benchstat.csv"))
	require.NoError(t, err)
	cells, err := perfgate.ParseBenchstatCSV(data)
	require.NoError(t, err)
	require.Len(t, cells, 27)

	names := make([]string, 0, len(cells))
	for name := range cells {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// verdictsFor reads one verdict per named cell, "" for a cell the report never
// mentioned, so a whole report is compared in one assertion.
func verdictsFor(t *testing.T, report string, names []string) map[string]string {
	t.Helper()

	lines := reportLines(t, report)
	got := make(map[string]string, len(names))
	for _, name := range names {
		got[name] = lines[name].verdict
	}
	return got
}

func uniformVerdicts(names []string, verdict string) map[string]string {
	want := make(map[string]string, len(names))
	for _, name := range names {
		want[name] = verdict
	}
	return want
}

func withoutCell(names []string, drop string) []string {
	kept := make([]string, 0, len(names))
	for _, name := range names {
		if name != drop {
			kept = append(kept, name)
		}
	}
	return kept
}

func identityOf(t *testing.T, path string) string {
	t.Helper()

	f, err := os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	id, err := perfgate.ReadRunnerIdentity(f)
	require.NoError(t, err)
	return id.String()
}

// withoutCPULines copies a committed raw bench file into the test's temp dir
// with its cpu: lines dropped. The committed corpora are never edited.
func withoutCPULines(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var kept []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "cpu:") {
			continue
		}
		kept = append(kept, line)
	}

	out := filepath.Join(t.TempDir(), "bench-no-cpu.txt")
	require.NoError(t, os.WriteFile(out, []byte(strings.Join(kept, "\n")), 0o644))
	return out
}

// keepingCells copies a committed raw bench file into the test's temp dir with
// only the benchmark result lines under prefix kept. Every other line is
// copied verbatim, the goos:/goarch:/pkg:/cpu: preamble each of the ten
// appended runs repeats included: a filtered file that lost its preamble reads
// as an unknown identity and stops being the cross-runner case. The committed
// corpora are never edited.
func keepingCells(t *testing.T, path, prefix string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var kept []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "Benchmark") && !strings.HasPrefix(line, prefix) {
			continue
		}
		kept = append(kept, line)
	}

	out := filepath.Join(t.TempDir(), "bench-filtered.txt")
	require.NoError(t, os.WriteFile(out, []byte(strings.Join(kept, "\n")), 0o644))
	return out
}

// droppingCells copies a committed raw bench file into the test's temp dir
// with the benchmark result lines under prefix removed, preamble kept: the
// filtered file keeps a known runner identity. The committed corpora are
// never edited.
func droppingCells(t *testing.T, path, prefix string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var kept []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "Benchmark") && strings.HasPrefix(line, prefix) {
			continue
		}
		kept = append(kept, line)
	}

	out := filepath.Join(t.TempDir(), "bench-dropped.txt")
	require.NoError(t, os.WriteFile(out, []byte(strings.Join(kept, "\n")), 0o644))
	return out
}

// cellsWithPrefix is the cells of one benchmark group, read out of the
// committed cell set rather than retyped, with the group's size pinned so a
// corpus change cannot silently shrink what an assertion covers.
func cellsWithPrefix(t *testing.T, names []string, prefix string, want int) []string {
	t.Helper()

	kept := make([]string, 0, want)
	for _, name := range names {
		if strings.HasPrefix(name, prefix) {
			kept = append(kept, name)
		}
	}
	require.Len(t, kept, want, "the committed cell set no longer holds %d %s cells", want, prefix)
	return kept
}

// runGate invokes the gate with a benchstat that fails the test if it is
// reached: the identity comparison reads the two raw files itself, and a
// benchstat run on a mismatched pair aborts before the cell loop, silently
// foreclosing the bytes and allocs verdicts.
func runGate(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()

	swapBenchstat(t, func(_, _ string) ([]byte, error) {
		assert.Fail(t, "benchstat ran for a pair whose runner identities were never compared")
		return nil, errors.New("benchstat must not run")
	})

	var outBuf, errBuf bytes.Buffer
	code = run(&outBuf, &errBuf, append([]string{"-tiers", committedTiers}, args...))
	return code, outBuf.String(), errBuf.String()
}

// TestRun_ResolveModeSubcommand pins the gate mode to the stored baselines
// rather than the triggering event: a dispatched run reads the same lookup a
// release run does and reaches the same mode. A lookup that could not enumerate
// releases is not evidence of absence, so it selects no threshold branch at all
// and exits 3 instead of silently falling back to first-authorization.
func TestRun_ResolveModeSubcommand(t *testing.T) {
	swapBenchstat(t, func(_, _ string) ([]byte, error) {
		assert.Fail(t, "benchstat ran for resolve-mode, which reads no benchmark output")
		return nil, errors.New("benchstat must not run")
	})
	dir := t.TempDir()

	found := writeLookup(t, dir, "found.json", perfgate.BaselineLookup{
		EnumerationOK: true,
		Tags:          []string{"v0.12.0"},
		DownloadedTag: "v0.12.0",
	})
	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"resolve-mode", "-lookup", found})
	assert.Equal(t, 0, code, "a downloaded baseline is a clean resolution; stderr: %s", stderr.String())
	assert.Contains(t, stdout.String(), "mode=non-regression",
		"a stored baseline selects non-regression; stdout: %q", stdout.String())

	absent := writeLookup(t, dir, "absent.json", perfgate.BaselineLookup{
		EnumerationOK: true,
		Tags:          []string{"v0.11.0", "v0.12.0"},
	})
	stdout.Reset()
	stderr.Reset()
	code = run(&stdout, &stderr, []string{"resolve-mode", "-lookup", absent})
	assert.Equal(t, 0, code, "a known absence is a clean resolution; stderr: %s", stderr.String())
	assert.Contains(t, stdout.String(), "mode=first-authorization",
		"no release carries the asset, so the mode is first-authorization; stdout: %q", stdout.String())

	// The zero lookup: EnumerationOK is false by zero value, so an unpopulated
	// lookup fails closed the way a failed `gh release list` must.
	enumFailed := writeLookup(t, dir, "enumeration-failed.json", perfgate.BaselineLookup{})
	stdout.Reset()
	stderr.Reset()
	code = run(&stdout, &stderr, []string{"resolve-mode", "-lookup", enumFailed})
	assert.Equal(t, 3, code, "a failed enumeration must not resolve a mode; stderr: %s", stderr.String())
	assert.NotContains(t, stdout.String(), "mode=",
		"a failed enumeration must name no mode; stdout: %q", stdout.String())
	assert.Contains(t, stderr.String(), "enumerating releases",
		"stderr does not give the enumeration failure as the reason: %s", stderr.String())
}

// writeBenchFile writes a synthetic raw bench file with a known runner
// identity: the given cpu value in the preamble the identity reader needs,
// plus the given benchmark result lines. The reader under test parses the
// result, never a hand-assigned map.
func writeBenchFile(t *testing.T, cpu, body string) string {
	t.Helper()

	content := "goos: linux\ngoarch: amd64\npkg: github.com/victorzhuk/go-lispico/internal/goldset\ncpu: " + cpu + "\n" + body
	out := filepath.Join(t.TempDir(), "bench.txt")
	require.NoError(t, os.WriteFile(out, []byte(content), 0o644))
	return out
}

// writeSingleCellTiers commits a one-cell tier config so synthetic fixtures
// only have to carry complete evidence for the cell under test.
func writeSingleCellTiers(t *testing.T) string {
	t.Helper()

	out := filepath.Join(t.TempDir(), "tiers.json")
	require.NoError(t, os.WriteFile(out, []byte(`{"cells":{"Goldset/route-decision":"data-dominated"},"bytesAllowanceBOp":{"Goldset/route-decision":0}}`), 0o644))
	return out
}

// runGateWithTiers is runGate with an explicit tier config path.
func runGateWithTiers(t *testing.T, tiersPath string, args ...string) (code int, stdout, stderr string) {
	t.Helper()

	swapBenchstat(t, func(_, _ string) ([]byte, error) {
		assert.Fail(t, "benchstat ran for a run the preflight must have rejected")
		return nil, errors.New("benchstat must not run")
	})

	var outBuf, errBuf bytes.Buffer
	code = run(&outBuf, &errBuf, append([]string{"-tiers", tiersPath}, args...))
	return code, outBuf.String(), errBuf.String()
}

// TestRun_CandidateMissingRequiredMetricExitsThree pins the evidence
// preflight over the incomplete-candidate shapes: a cell that timed but
// skipped a metric would otherwise read as an empty median and a silent
// improvement. The diagnostic must name the exact missing units, in order,
// and the run must end before any verdict or statistics.
func TestRun_CandidateMissingRequiredMetricExitsThree(t *testing.T) {
	tiersPath := writeSingleCellTiers(t)
	baseline := writeBenchFile(t, "AMD EPYC 7763 64-Core Processor",
		"BenchmarkGoldset/route-decision-2  100  5000 ns/op  48 B/op  3 allocs/op\n")

	cases := []struct {
		name string
		body string
		want string
	}{
		{"time only", "BenchmarkGoldset/route-decision-2  100  5000 ns/op\n",
			"Goldset/route-decision (missing candidate samples): FAIL missing B/op, allocs/op"},
		{"bytes missing", "BenchmarkGoldset/route-decision-2  100  5000 ns/op  3 allocs/op\n",
			"Goldset/route-decision (missing candidate samples): FAIL missing B/op"},
		{"allocs missing", "BenchmarkGoldset/route-decision-2  100  5000 ns/op  48 B/op\n",
			"Goldset/route-decision (missing candidate samples): FAIL missing allocs/op"},
		{"both allocations missing", "BenchmarkGoldset/route-decision-2  100  5000 ns/op\n",
			"Goldset/route-decision (missing candidate samples): FAIL missing B/op, allocs/op"},
	}

	for _, runner := range []struct {
		name string
		cpu  string
	}{
		{"same runner", "AMD EPYC 7763 64-Core Processor"},
		{"cross runner", "Intel(R) Xeon(R) Platinum 8260"},
	} {
		t.Run(runner.name, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					candidate := writeBenchFile(t, runner.cpu, tc.body)

					code, stdout, stderr := runGateWithTiers(t, tiersPath,
						"-old", baseline, "-candidate", candidate, "-mode", "non-regression")

					assert.Equal(t, 3, code, "incomplete candidate evidence must exit 3; stderr: %s", stderr)
					assert.Contains(t, stderr, tc.want, "stderr must name the exact missing units: %s", stderr)
					assert.Equal(t, "", stdout, "no cell verdict may precede the preflight rejection")
				})
			}
		})
	}
}

// TestRun_MissingMetricPrecedesMissingBaseline pins the preflight's place in
// the ordering: a time-only candidate measurement of a cell the baseline
// never recorded is rejected as incomplete evidence, not reported through
// the missing-baseline branch.
func TestRun_MissingMetricPrecedesMissingBaseline(t *testing.T) {
	tiersPath := writeSingleCellTiers(t)
	baseline := writeBenchFile(t, "Intel(R) Xeon(R) Platinum 8260",
		"BenchmarkGoldset/other-cell-2  100  5000 ns/op  48 B/op  3 allocs/op\n")
	candidate := writeBenchFile(t, "AMD EPYC 7763 64-Core Processor",
		"BenchmarkGoldset/route-decision-2  100  5000 ns/op\n")

	code, stdout, stderr := runGateWithTiers(t, tiersPath,
		"-old", baseline, "-candidate", candidate, "-mode", "non-regression")

	assert.Equal(t, 3, code, "a time-only candidate measurement is incomplete evidence; stderr: %s", stderr)
	assert.Contains(t, stderr, "Goldset/route-decision (missing candidate samples): FAIL missing B/op, allocs/op",
		"the missing allocation evidence must be what the gate reports: %s", stderr)
	assert.NotContains(t, stdout+stderr, "INCONCLUSIVE (no baseline figure",
		"the missing-metric rejection must come before the missing-baseline branch")
}

// TestRun_BaselineMissingAllocsCrossRunnerExitsThree pins the baseline side of
// the evidence floor: a present baseline cell that timed but never recorded an
// allocation figure is not comparable evidence, so the cross-runner gate must
// refuse before any verdict instead of reading the absent samples as zero.
func TestRun_BaselineMissingAllocsCrossRunnerExitsThree(t *testing.T) {
	tiersPath := writeSingleCellTiers(t)
	baseline := writeBenchFile(t, "fixture-runner-a",
		"BenchmarkGoldset/route-decision-2  100  10000 ns/op\n")
	candidate := writeBenchFile(t, "fixture-runner-b",
		"BenchmarkGoldset/route-decision-2  100  9000 ns/op  16 B/op  1 allocs/op\n")

	code, stdout, stderr := runGateWithTiers(t, tiersPath,
		"-old", baseline, "-candidate", candidate, "-mode", "non-regression")

	assert.Equal(t, 3, code, "a baseline cell with no allocation samples must refuse the comparison; stderr: %s", stderr)
	assert.Empty(t, stdout, "a refused comparison writes no report; stdout: %s", stdout)
}

// TestRun_MeasuredZeroMetricsAccepted pins the boundary the preflight exists
// to respect: a literal 0 B/op and 0 allocs/op are measurements, not missing
// evidence, so a genuine zero-cost cell passes the completeness gate.
func TestRun_MeasuredZeroMetricsAccepted(t *testing.T) {
	tiersPath := writeSingleCellTiers(t)
	baselineBody := "BenchmarkGoldset/route-decision-2  100  6000 ns/op  0 B/op  0 allocs/op\n"
	candidateBody := "BenchmarkGoldset/route-decision-2  100  5000 ns/op  0 B/op  0 allocs/op\n"

	t.Run("same runner", func(t *testing.T) {
		baseline := writeBenchFile(t, "AMD EPYC 7763 64-Core Processor", baselineBody)
		candidate := writeBenchFile(t, "AMD EPYC 7763 64-Core Processor", candidateBody)
		csvData := "goos: linux\ngoarch: amd64\npkg: x\ncpu: AMD EPYC 7763 64-Core Processor\n" +
			",old.txt,,,new.txt,,\n,sec/op,CI,sec/op,CI,vs base,P\n" +
			"Goldset/route-decision-2,6e-06,1%,5e-06,1%,-16.67%,p=0.000 n=10\n"
		swapBenchstat(t, func(_, _ string) ([]byte, error) { return []byte(csvData), nil })

		var stdout, stderr bytes.Buffer
		code := run(&stdout, &stderr, []string{
			"-old", baseline, "-candidate", candidate, "-tiers", tiersPath, "-mode", "non-regression",
		})

		assert.Equal(t, 0, code, "measured zeros are complete evidence and an improving same-runner pair must pass; stderr: %s", stderr.String())
		assert.NotContains(t, stderr.String(), "missing candidate samples",
			"no completeness diagnostic may fire on a measured zero")
		assert.Equal(t, []string{"Goldset/route-decision-2: PASS"}, reportBodyLines(t, stdout.String()),
			"the report must contain exactly the passing cell line; stdout: %s", stdout.String())
	})

	t.Run("cross runner control", func(t *testing.T) {
		baseline := writeBenchFile(t, "AMD EPYC 7763 64-Core Processor", baselineBody)
		candidate := withCPULines(t, baseline, "Intel(R) Xeon(R) Platinum 8260")

		code, stdout, stderr := runGateWithTiers(t, tiersPath,
			"-old", baseline, "-candidate", candidate, "-mode", "non-regression")

		assert.Equal(t, 0, code, "unchanged measured allocations authorize the release on the comparable axes; stderr: %s", stderr)
		assert.Contains(t, stdout, "INCONCLUSIVE", "the machine-dependent axes stay undecided on a cross-runner pair")
		assert.NotContains(t, stderr, "missing candidate samples", "measured zeros are never a completeness failure")
	})
}

// TestRun_DisjointInventoryRefusesZeroComparisons pins the zero-comparison
// refusal in run-wide first authorization, the mode whose reference names come
// only from -old: a candidate that measured every configured cell while the
// baseline measured none of them produces inconclusive-only evidence and must
// not pass the aggregate gate.
func TestRun_DisjointInventoryRefusesZeroComparisons(t *testing.T) {
	tiersPath := writeSingleCellTiers(t)

	for _, tc := range []struct {
		name         string
		baselineCPU  string
		candidateCPU string
	}{
		{"same runner", "fixture-runner-a", "fixture-runner-a"},
		{"cross runner", "fixture-runner-a", "fixture-runner-b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			benchstatRan := false
			swapBenchstat(t, func(_, _ string) ([]byte, error) {
				benchstatRan = true
				return nil, errors.New("benchstat must not run on a disjoint inventory")
			})
			baseline := writeBenchFile(t, tc.baselineCPU,
				repeatLine("BenchmarkGoldset/other-cell-2  100  5000 ns/op  48 B/op  3 allocs/op"))
			candidate := writeBenchFile(t, tc.candidateCPU,
				repeatLine("BenchmarkGoldset/route-decision-2  100  5000 ns/op  48 B/op  3 allocs/op"))

			var stdout, stderr bytes.Buffer
			code := run(&stdout, &stderr, []string{
				"-old", baseline, "-candidate", candidate, "-tiers", tiersPath, "-mode", "first-authorization",
			})

			assert.Equal(t, 3, code, "a run that compared no cell must not authorize a release; stderr: %s", stderr.String())
			assert.Contains(t, stderr.String(), "no comparison was performed",
				"the refusal must name the absence of any comparison: %s", stderr.String())
			assert.False(t, benchstatRan, "no statistics may run on a disjoint inventory")
			assert.Contains(t, stdout.String(), "INCONCLUSIVE (no baseline figure",
				"the existing per-cell report is still printed before the refusal")
		})
	}
}

// repeatLine gives the raw reader ten samples of one result line, the count a
// median needs so a synthetic fixture is evidence, not a single observation.
func repeatLine(line string) string {
	return strings.Repeat(line+"\n", 10)
}

// writeTiers commits a multi-cell tier config with a zero byte allowance for
// every cell, marshaled through the same wire shape as the committed config.
func writeTiers(t *testing.T, cells map[string]string) string {
	t.Helper()

	allowances := make(map[string]int, len(cells))
	for name := range cells {
		allowances[name] = 0
	}
	data, err := json.Marshal(map[string]any{"cells": cells, "bytesAllowanceBOp": allowances})
	require.NoError(t, err)
	out := filepath.Join(t.TempDir(), "tiers.json")
	require.NoError(t, os.WriteFile(out, data, 0o644))
	return out
}

// pairedCSV renders a swapped benchstat output carrying only the given result
// rows, each a full seven-column sec/op comparison.
func pairedCSV(cpu string, rows ...string) string {
	return "goos: linux\ngoarch: amd64\npkg: x\ncpu: " + cpu + "\n" +
		",old.txt,,,new.txt,,\n,sec/op,CI,sec/op,CI,vs base,P\n" +
		strings.Join(rows, "\n") + "\n"
}

// TestRun_OneSidedCellDoesNotForceExitThree pins the pairing boundary: a
// candidate-only configured cell is authorized by the current Evaluator as a
// first-authorization verdict and must neither turn an otherwise paired PASS
// into a refusal nor vanish from the report.
func TestRun_OneSidedCellDoesNotForceExitThree(t *testing.T) {
	tiersPath := writeTiers(t, map[string]string{
		"Goldset/new":  "data-dominated",
		"Goldset/pass": "data-dominated",
	})
	baseline := writeBenchFile(t, "fixture-runner-a",
		repeatLine("BenchmarkGoldset/pass-2  100  10000 ns/op  16 B/op  1 allocs/op"))
	evaluator := writeBenchFile(t, "fixture-runner-a",
		repeatLine("BenchmarkGoldset/new-2  100  10000 ns/op  16 B/op  1 allocs/op"))
	candidate := writeBenchFile(t, "fixture-runner-a",
		repeatLine("BenchmarkGoldset/new-2  100  9000 ns/op  16 B/op  1 allocs/op")+
			repeatLine("BenchmarkGoldset/pass-2  100  9000 ns/op  16 B/op  1 allocs/op"))
	swapBenchstat(t, func(_, _ string) ([]byte, error) {
		return []byte(pairedCSV("fixture-runner-a",
			"Goldset/pass-2,1e-05,1%,9e-06,1%,-10.00%,p=0.000 n=10",
			"Goldset/new-2,1e-05,1%,9.7e-06,1%,-3.00%,p=0.000 n=10")), nil
	})

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"-old", baseline, "-evaluator", evaluator, "-candidate", candidate, "-tiers", tiersPath, "-mode", "non-regression",
	})

	assert.Equal(t, 0, code, "a paired PASS plus one Evaluator-authorized cell must authorize the release; stderr: %s", stderr.String())
	assert.Empty(t, stderr.String(), "no diagnostic may fire on complete paired evidence: %s", stderr.String())
	want := "Goldset/new-2: PASS (mode=first-authorization reference=evaluator)\n" +
		"Goldset/pass-2: PASS\n"
	assert.Equal(t, want, stdout.String(),
		"the report must carry the candidate-only cell as a first-authorization PASS and the paired cell as PASS, sorted")
}

// TestRun_OneSidedCellsKeepPairedVerdicts pins that pairing filters without
// losing candidate inventory: paired cells keep their statistical verdicts,
// the candidate-only configured cell earns its Evaluator-backed
// first-authorization verdict, and an unconfigured baseline-only cell never
// reaches the report.
func TestRun_OneSidedCellsKeepPairedVerdicts(t *testing.T) {
	tiersPath := writeTiers(t, map[string]string{
		"Goldset/fail": "data-dominated",
		"Goldset/new":  "data-dominated",
		"Goldset/pass": "data-dominated",
	})
	baseline := writeBenchFile(t, "fixture-runner-a",
		repeatLine("BenchmarkGoldset/fail-2  100  10000 ns/op  16 B/op  1 allocs/op")+
			repeatLine("BenchmarkGoldset/historical-2  100  10000 ns/op")+
			repeatLine("BenchmarkGoldset/pass-2  100  10000 ns/op  16 B/op  1 allocs/op"))
	evaluator := writeBenchFile(t, "fixture-runner-a",
		repeatLine("BenchmarkGoldset/new-2  100  10000 ns/op  16 B/op  1 allocs/op"))
	candidate := writeBenchFile(t, "fixture-runner-a",
		repeatLine("BenchmarkGoldset/fail-2  100  12000 ns/op  16 B/op  1 allocs/op")+
			repeatLine("BenchmarkGoldset/new-2  100  9000 ns/op  16 B/op  1 allocs/op")+
			repeatLine("BenchmarkGoldset/pass-2  100  9000 ns/op  16 B/op  1 allocs/op"))
	swapBenchstat(t, func(_, _ string) ([]byte, error) {
		return []byte(pairedCSV("fixture-runner-a",
			"Goldset/fail-2,1e-05,1%,1.2e-05,1%,+20.00%,p=0.000 n=10",
			"Goldset/pass-2,1e-05,1%,9e-06,1%,-10.00%,p=0.000 n=10",
			"Goldset/new-2,1e-05,1%,9.7e-06,1%,-3.00%,p=0.000 n=10")), nil
	})

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"-old", baseline, "-evaluator", evaluator, "-candidate", candidate, "-tiers", tiersPath, "-mode", "non-regression",
	})

	assert.Equal(t, 1, code, "the paired latency regression must fail the gate; stderr: %s", stderr.String())
	lines := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
	if assert.Len(t, lines, 3, "the report must carry exactly the three candidate cells: %q", stdout.String()) {
		assert.True(t, strings.HasPrefix(lines[0], "Goldset/fail-2: FAIL "),
			"the first line must be the fail verdict: %q", lines[0])
		assert.Equal(t, "Goldset/new-2: PASS (mode=first-authorization reference=evaluator)", lines[1],
			"the candidate-only configured cell must carry its Evaluator-backed first-authorization verdict")
		assert.Equal(t, "Goldset/pass-2: PASS", lines[2], "the paired improved cell must pass")
	}
	assert.NotContains(t, stdout.String(), "historical",
		"a baseline-only unconfigured cell must never reach the report")
}

// TestRun_CandidateOnlyUntieredCellStillFails pins that pairing does not
// dilute the tier commitment: a candidate cell with no committed tier fails
// even when every paired cell passes.
func TestRun_CandidateOnlyUntieredCellStillFails(t *testing.T) {
	tiersPath := writeTiers(t, map[string]string{"Goldset/pass": "data-dominated"})
	baseline := writeBenchFile(t, "fixture-runner-a",
		repeatLine("BenchmarkGoldset/pass-2  100  10000 ns/op  16 B/op  1 allocs/op"))
	candidate := writeBenchFile(t, "fixture-runner-a",
		repeatLine("BenchmarkGoldset/extra-2  100  9000 ns/op  16 B/op  1 allocs/op")+
			repeatLine("BenchmarkGoldset/pass-2  100  9000 ns/op  16 B/op  1 allocs/op"))
	swapBenchstat(t, func(_, _ string) ([]byte, error) {
		return []byte(pairedCSV("fixture-runner-a",
			"Goldset/pass-2,1e-05,1%,9e-06,1%,-10.00%,p=0.000 n=10")), nil
	})

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"-old", baseline, "-candidate", candidate, "-tiers", tiersPath, "-mode", "non-regression",
	})

	assert.Equal(t, 1, code, "an undeclared candidate cell must fail the gate; stderr: %s", stderr.String())
	assert.Contains(t, stdout.String(), "Goldset/extra-2: FAIL no committed tier for this cell",
		"the undeclared cell must be reported as a tier failure: %s", stdout.String())
}

// TestRun_MissingPairedLatencyExitsThree pins the paired-evidence floor: a
// common cell whose benchstat row carries a nonpositive sample count is not
// timing evidence, and the gate refuses before any report is written.
func TestRun_MissingPairedLatencyExitsThree(t *testing.T) {
	tiersPath := writeTiers(t, map[string]string{"Goldset/pass": "data-dominated"})
	baseline := writeBenchFile(t, "fixture-runner-a",
		repeatLine("BenchmarkGoldset/pass-2  100  10000 ns/op  16 B/op  1 allocs/op"))
	candidate := writeBenchFile(t, "fixture-runner-a",
		repeatLine("BenchmarkGoldset/pass-2  100  9000 ns/op  16 B/op  1 allocs/op"))
	swapBenchstat(t, func(_, _ string) ([]byte, error) {
		return []byte(pairedCSV("fixture-runner-a",
			"Goldset/pass-2,1e-05,1%,9e-06,1%,-10.00%,p=0.000 n=0")), nil
	})

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"-old", baseline, "-candidate", candidate, "-tiers", tiersPath, "-mode", "non-regression",
	})

	assert.Equal(t, 3, code, "a paired cell with a zero-count timing row must refuse the run; stderr: %s", stderr.String())
	assert.Empty(t, stdout.String(), "a refused pairing writes no report; stdout: %s", stdout.String())
	assert.Contains(t, stderr.String(), `cell "Goldset/pass-2": missing paired sec/op comparison`,
		"the refusal must name the cell and the missing timing evidence: %s", stderr.String())
}

// TestRun_CandidatePreflightPrecedesBaselineValidation pins the preflight
// order: an incomplete candidate is reported as missing candidate samples even
// when the baseline is equally incomplete, never through the baseline-side
// allocation validation.
func TestRun_CandidatePreflightPrecedesBaselineValidation(t *testing.T) {
	tiersPath := writeSingleCellTiers(t)
	baseline := writeBenchFile(t, "fixture-runner-a",
		repeatLine("BenchmarkGoldset/route-decision-2  100  10000 ns/op"))
	candidate := writeBenchFile(t, "fixture-runner-a",
		repeatLine("BenchmarkGoldset/route-decision-2  100  9000 ns/op"))

	code, _, stderr := runGateWithTiers(t, tiersPath,
		"-old", baseline, "-candidate", candidate, "-mode", "non-regression")

	assert.Equal(t, 3, code, "time-only candidate evidence must exit 3; stderr: %s", stderr)
	assert.Contains(t, stderr, "Goldset/route-decision (missing candidate samples)",
		"the candidate preflight diagnostic must fire first: %s", stderr)
	assert.NotContains(t, stderr, "compare samples for cell",
		"the baseline-side validation must not fire before the candidate preflight: %s", stderr)
}

// TestPairedBenchstat_PreservesInputsAndCleansTemporaryFiles pins the pairing
// boundary directly: mixed inventories force filtered copies, every retained
// byte survives the round trip, the originals stay untouched, and the
// temporary directory is removed on both the success and the failure path.
func TestPairedBenchstat_PreservesInputsAndCleansTemporaryFiles(t *testing.T) {
	preamble := "goos: linux\r\ngoarch: amd64\r\npkg: github.com/victorzhuk/go-lispico/internal/goldset\r\ncpu: fixture-runner-a\r\ngoos: linux\r\n"
	commonLine := "BenchmarkGoldset/common-2  100  5000 ns/op  48 B/op  3 allocs/op\r\n"
	oldBytes := []byte(preamble +
		"BenchmarkGoldset/old-only-2  100  5000 ns/op  48 B/op  3 allocs/op\r\n" +
		"BenchmarkGoldset/common-2  100  5000 ns/op  48 B/op  3 allocs/op")
	newBytes := []byte(preamble +
		"BenchmarkGoldset/common-2  100  5000 ns/op  48 B/op  3 allocs/op\r\n" +
		"BenchmarkGoldset/new-only-2  100  5000 ns/op  48 B/op  3 allocs/op")
	wantFilteredOld := []byte(preamble + "BenchmarkGoldset/common-2  100  5000 ns/op  48 B/op  3 allocs/op")
	wantFilteredNew := []byte(preamble + commonLine)

	oldPath := filepath.Join(t.TempDir(), "old.txt")
	newPath := filepath.Join(t.TempDir(), "candidate.txt")
	require.NoError(t, os.WriteFile(oldPath, oldBytes, 0o644))
	require.NoError(t, os.WriteFile(newPath, newBytes, 0o644))

	oldMetrics := map[string]perfgate.SampleMetrics{
		"Goldset/common-2":   {Name: "Goldset/common-2"},
		"Goldset/old-only-2": {Name: "Goldset/old-only-2"},
	}
	newMetrics := map[string]perfgate.SampleMetrics{
		"Goldset/common-2":   {Name: "Goldset/common-2"},
		"Goldset/new-only-2": {Name: "Goldset/new-only-2"},
	}
	pairedCSV := []byte("goos: linux\ngoarch: amd64\npkg: x\ncpu: fixture-runner-a\n" +
		",old.txt,,,new.txt,,\n,sec/op,CI,sec/op,CI,vs base,P\n" +
		"Goldset/common-2,5e-06,1%,5e-06,1%,+0.00%,p=0.500 n=10\n")

	t.Run("success", func(t *testing.T) {
		var gotOld, gotNew, filteredOld, filteredNew string
		swapBenchstat(t, func(oldArg, newArg string) ([]byte, error) {
			gotOld, gotNew = oldArg, newArg
			// The temporary files exist only while benchstat runs; read
			// them here, before pairedBenchstat removes the directory.
			oldData, err := os.ReadFile(oldArg)
			require.NoError(t, err)
			newData, err := os.ReadFile(newArg)
			require.NoError(t, err)
			filteredOld, filteredNew = string(oldData), string(newData)
			return pairedCSV, nil
		})

		parsed, err := pairedBenchstat(oldPath, newPath, oldMetrics, newMetrics, nil)
		require.NoError(t, err)
		assert.Contains(t, parsed, "Goldset/common-2", "the common cell must be paired")

		assert.NotEqual(t, oldPath, gotOld, "mixed inventories must filter through new paths")
		assert.NotEqual(t, newPath, gotNew, "mixed inventories must filter through new paths")
		assert.Equal(t, string(wantFilteredOld), filteredOld,
			"the old copy keeps metadata verbatim, the common result line, and the missing final newline")
		assert.Equal(t, string(wantFilteredNew), filteredNew,
			"the candidate copy keeps metadata verbatim and the common result line's CRLF")
		assert.NoDirExists(t, filepath.Dir(gotOld), "the temporary directory must be removed on success")

		afterOld, err := os.ReadFile(oldPath)
		require.NoError(t, err)
		afterNew, err := os.ReadFile(newPath)
		require.NoError(t, err)
		assert.Equal(t, string(oldBytes), string(afterOld), "the original old file is byte-identical")
		assert.Equal(t, string(newBytes), string(afterNew), "the original candidate file is byte-identical")
	})

	t.Run("benchstat failure", func(t *testing.T) {
		var gotOld string
		swapBenchstat(t, func(oldArg, _ string) ([]byte, error) {
			gotOld = oldArg
			return nil, errors.New("injected benchstat failure")
		})

		_, err := pairedBenchstat(oldPath, newPath, oldMetrics, newMetrics, nil)
		assert.Error(t, err, "a benchstat failure must propagate")
		assert.NoDirExists(t, filepath.Dir(gotOld), "the temporary directory must be removed on failure")
	})
}

// TestPairedBenchstat_EqualInventoriesUseOriginalPaths pins the fast path:
// when both arms measured exactly the same cells, benchstat reads the original
// files and no filtered copy is produced.
func TestPairedBenchstat_EqualInventoriesUseOriginalPaths(t *testing.T) {
	body := repeatLine("BenchmarkGoldset/common-2  100  5000 ns/op  48 B/op  3 allocs/op")
	oldPath := writeBenchFile(t, "fixture-runner-a", body)
	newPath := writeBenchFile(t, "fixture-runner-a", body)
	metrics := func() map[string]perfgate.SampleMetrics {
		return map[string]perfgate.SampleMetrics{"Goldset/common-2": {Name: "Goldset/common-2"}}
	}
	pairedCSV := []byte("goos: linux\ngoarch: amd64\npkg: x\ncpu: fixture-runner-a\n" +
		",old.txt,,,new.txt,,\n,sec/op,CI,sec/op,CI,vs base,P\n" +
		"Goldset/common-2,5e-06,1%,5e-06,1%,+0.00%,p=0.500 n=10\n")

	var gotOld, gotNew string
	swapBenchstat(t, func(oldArg, newArg string) ([]byte, error) {
		gotOld, gotNew = oldArg, newArg
		return pairedCSV, nil
	})

	_, err := pairedBenchstat(oldPath, newPath, metrics(), metrics(), nil)
	require.NoError(t, err)
	assert.Equal(t, oldPath, gotOld, "equal complete inventories use the original old path")
	assert.Equal(t, newPath, gotNew, "equal complete inventories use the original candidate path")
}

// TestRun_IncompleteBaselineMetricsExitsThree pins the baseline side of the
// allocation evidence floor through the real run entry: a present baseline
// cell whose allocation samples are absent must refuse before benchstat,
// verdicts, or any report write, on both runner paths.
func TestRun_IncompleteBaselineMetricsExitsThree(t *testing.T) {
	tiersPath := writeSingleCellTiers(t)
	candidateBody := repeatLine("BenchmarkGoldset/route-decision-2  100  9000 ns/op  16 B/op  1 allocs/op")

	cases := []struct {
		name  string
		body  string
		items string
	}{
		{"baseline missing B/op", "BenchmarkGoldset/route-decision-2  100  10000 ns/op  1 allocs/op\n", "baseline B/op"},
		{"baseline missing allocs/op", "BenchmarkGoldset/route-decision-2  100  10000 ns/op  16 B/op\n", "baseline allocs/op"},
		{"baseline missing both", "BenchmarkGoldset/route-decision-2  100  10000 ns/op\n", "baseline B/op, baseline allocs/op"},
		{"baseline measured zero bytes missing allocs", "BenchmarkGoldset/route-decision-2  100  10000 ns/op  0 B/op\n", "baseline allocs/op"},
	}

	for _, runner := range []struct {
		name string
		cpuA string
		cpuB string
	}{
		{"same runner", "fixture-runner-a", "fixture-runner-a"},
		{"cross runner", "fixture-runner-a", "fixture-runner-b"},
	} {
		t.Run(runner.name, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					baseline := writeBenchFile(t, runner.cpuA, tc.body)
					candidate := writeBenchFile(t, runner.cpuB, candidateBody)
					outPath := filepath.Join(t.TempDir(), "report.txt")
					require.NoError(t, os.WriteFile(outPath, []byte("SENTINEL"), 0o644))

					code, stdout, stderr := runGateWithTiers(t, tiersPath,
						"-old", baseline, "-candidate", candidate, "-mode", "non-regression", "-out", outPath)

					assert.Equal(t, 3, code, "absent baseline allocation samples must exit 3; stderr: %s", stderr)
					assert.Empty(t, stdout, "a refused comparison writes no report; stdout: %s", stdout)
					assert.Contains(t, stderr, fmt.Sprintf("compare samples for cell %q: missing allocation samples: %s",
						"Goldset/route-decision-2", tc.items),
						"stderr must name the cell and the exact ordered missing items: %s", stderr)
					outData, err := os.ReadFile(outPath)
					require.NoError(t, err)
					assert.Equal(t, "SENTINEL", string(outData), "-out must be neither created nor overwritten")
				})
			}
		})
	}
}

// TestRun_SuffixMismatchIsUnpaired pins exact-name pairing in run-wide first
// authorization, whose reference names come only from -old: a differing
// GOMAXPROCS suffix makes the cells distinct, so the candidate cell reads as
// missing-baseline evidence and zero comparisons refuse the run.
func TestRun_SuffixMismatchIsUnpaired(t *testing.T) {
	tiersPath := writeSingleCellTiers(t)
	baseline := writeBenchFile(t, "fixture-runner-a",
		repeatLine("BenchmarkGoldset/route-decision-4  100  10000 ns/op  16 B/op  1 allocs/op"))
	candidate := writeBenchFile(t, "fixture-runner-a",
		repeatLine("BenchmarkGoldset/route-decision-2  100  9000 ns/op  16 B/op  1 allocs/op"))

	code, stdout, stderr := runGateWithTiers(t, tiersPath,
		"-old", baseline, "-candidate", candidate, "-mode", "first-authorization")

	assert.Contains(t, stdout, "Goldset/route-decision-2: INCONCLUSIVE (no baseline figure for this cell)",
		"the differing suffix must not fabricate a baseline; stdout: %s", stdout)
	assert.Equal(t, 3, code, "zero comparisons must exit 3; stderr: %s", stderr)
	assert.Contains(t, stderr, "no comparison was performed",
		"the zero-comparison refusal must fire: %s", stderr)
}

// reportBodyLines returns the non-empty, non-header lines of a report.
func reportBodyLines(t *testing.T, report string) []string {
	t.Helper()

	var lines []string
	for _, line := range strings.Split(report, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, strings.TrimSpace(line))
		}
	}
	return lines
}

// writeLookup marshals the struct the subcommand unmarshals into, so the
// fixture's wire shape cannot drift from the type the classification reads.
func writeLookup(t *testing.T, dir, name string, lookup perfgate.BaselineLookup) string {
	t.Helper()

	data, err := json.Marshal(lookup)
	require.NoError(t, err)

	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, data, 0o644))
	return path
}

// writeBenchFileWithPreamble writes a synthetic raw bench file whose preamble
// lines are supplied verbatim, so identity-rejection cases can exercise a
// candidate run with missing or blank identity keys.
func writeBenchFileWithPreamble(t *testing.T, preamble, body string) string {
	t.Helper()

	out := filepath.Join(t.TempDir(), "bench.txt")
	require.NoError(t, os.WriteFile(out, []byte(preamble+body), 0o644))
	return out
}

// TestRun_CandidateIncompleteRunnerIdentityExitsThree pins the identity floor
// of the evidence preflight: a candidate run whose preamble is missing any
// required identity key (goos, goarch, cpu) — or carries a whitespace-only
// cpu value — is not comparable evidence and must refuse with exit 3 before
// any statistics, verdict, or report write, in both gate modes.
func TestRun_CandidateIncompleteRunnerIdentityExitsThree(t *testing.T) {
	tiersPath := writeSingleCellTiers(t)
	baseline := writeBenchFile(t, "AMD EPYC 7763 64-Core Processor",
		"BenchmarkGoldset/route-decision-2  100  6000 ns/op  48 B/op  3 allocs/op\n")
	completeBody := "BenchmarkGoldset/route-decision-2  100  5000 ns/op  48 B/op  3 allocs/op\n"
	const wantDiagnostic = "perfgate: candidate run has incomplete runner identity (requires goos, goarch, cpu)"

	cases := []struct {
		name     string
		preamble string
	}{
		{"cpu missing", "goos: linux\ngoarch: amd64\npkg: github.com/victorzhuk/go-lispico/internal/goldset\n"},
		{"goos missing", "goarch: amd64\npkg: github.com/victorzhuk/go-lispico/internal/goldset\ncpu: AMD EPYC 7763 64-Core Processor\n"},
		{"goarch missing", "goos: linux\npkg: github.com/victorzhuk/go-lispico/internal/goldset\ncpu: AMD EPYC 7763 64-Core Processor\n"},
		{"all identity keys missing", "pkg: github.com/victorzhuk/go-lispico/internal/goldset\n"},
		{"whitespace-only cpu", "goos: linux\ngoarch: amd64\npkg: github.com/victorzhuk/go-lispico/internal/goldset\ncpu:    \n"},
	}

	for _, mode := range []string{"first-authorization", "non-regression"} {
		t.Run(mode, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					candidate := writeBenchFileWithPreamble(t, tc.preamble, completeBody)

					code, stdout, stderr := runGateWithTiers(t, tiersPath,
						"-old", baseline, "-candidate", candidate, "-mode", mode)

					assert.Equal(t, 3, code, "an incomplete candidate runner identity must exit 3; stderr: %s", stderr)
					assert.Contains(t, stderr, wantDiagnostic,
						"stderr must carry the exact identity diagnostic: %s", stderr)
					assert.Equal(t, "", stdout, "no cell verdict may precede the identity rejection")
				})
			}

			t.Run("existing report untouched", func(t *testing.T) {
				candidate := writeBenchFileWithPreamble(t,
					"goos: linux\ngoarch: amd64\npkg: github.com/victorzhuk/go-lispico/internal/goldset\n", completeBody)
				outPath := filepath.Join(t.TempDir(), "report.txt")
				require.NoError(t, os.WriteFile(outPath, []byte("sentinel\n"), 0o644))

				code, stdout, stderr := runGateWithTiers(t, tiersPath,
					"-old", baseline, "-candidate", candidate, "-mode", mode, "-out", outPath)

				assert.Equal(t, 3, code, "the identity rejection must exit 3; stderr: %s", stderr)
				assert.Equal(t, "", stdout, "a rejected run writes no stdout report: %s", stdout)
				after, err := os.ReadFile(outPath)
				require.NoError(t, err)
				assert.Equal(t, []byte("sentinel\n"), after,
					"an existing report must remain byte-identical after the identity rejection")
			})

			t.Run("absent report stays absent", func(t *testing.T) {
				candidate := writeBenchFileWithPreamble(t,
					"goos: linux\ngoarch: amd64\npkg: github.com/victorzhuk/go-lispico/internal/goldset\n", completeBody)
				outPath := filepath.Join(t.TempDir(), "report.txt")

				code, _, stderr := runGateWithTiers(t, tiersPath,
					"-old", baseline, "-candidate", candidate, "-mode", mode, "-out", outPath)

				assert.Equal(t, 3, code, "the identity rejection must exit 3; stderr: %s", stderr)
				if _, err := os.Stat(outPath); err == nil {
					t.Fatal("a rejected run must not create the report file")
				}
			})
		})
	}
}

// TestRun_ConcurrentOmittedRaceCleanFails pins the -race-clean default: a
// configured concurrent cell with otherwise passing evidence and no explicit
// -race-clean attestation must fail, because the default attestation is
// false and only a real clean race run may authorize the concurrent tier.
func TestRun_ConcurrentOmittedRaceCleanFails(t *testing.T) {
	tiersJSON := `{"cells":{"Goldset/concurrent-cell":"concurrent"},"bytesAllowanceBOp":{"Goldset/concurrent-cell":0}}`
	tiersPath := filepath.Join(t.TempDir(), "tiers.json")
	require.NoError(t, os.WriteFile(tiersPath, []byte(tiersJSON), 0o644))

	baseline := writeBenchFile(t, "AMD EPYC 7763 64-Core Processor",
		"BenchmarkGoldset/concurrent-cell-2  100  6000 ns/op  64 B/op  2 allocs/op\n")
	candidate := writeBenchFile(t, "AMD EPYC 7763 64-Core Processor",
		"BenchmarkGoldset/concurrent-cell-2  100  6000 ns/op  64 B/op  2 allocs/op\n")
	csvData := "goos: linux\ngoarch: amd64\npkg: x\ncpu: AMD EPYC 7763 64-Core Processor\n" +
		",old.txt,,,new.txt,,\n,sec/op,CI,sec/op,CI,vs base,P\n" +
		"Goldset/concurrent-cell-2,6e-06,1%,6e-06,1%,+0.00%,p=0.000 n=10\n"
	swapBenchstat(t, func(_, _ string) ([]byte, error) { return []byte(csvData), nil })

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"-old", baseline, "-candidate", candidate, "-tiers", tiersPath, "-mode", "non-regression",
	})

	assert.Equal(t, 1, code,
		"omitting -race-clean must fail the concurrent cell; stdout: %s stderr: %s", stdout.String(), stderr.String())
	assert.Contains(t, stdout.String(), "FAIL",
		"the concurrent cell must report FAIL without a clean-race attestation; stdout: %s", stdout.String())
}

// writeEngineSensitiveTiers commits a zero-allowance engine-sensitive config
// for the named cells so synthetic fixtures only carry those cells.
func writeEngineSensitiveTiers(t *testing.T, cells ...string) string {
	t.Helper()

	tiers := map[string]any{}
	allowance := map[string]int{}
	for _, cell := range cells {
		tiers[cell] = "engine-sensitive"
		allowance[cell] = 0
	}
	data, err := json.Marshal(map[string]any{"cells": tiers, "bytesAllowanceBOp": allowance})
	require.NoError(t, err)
	out := filepath.Join(t.TempDir(), "tiers.json")
	require.NoError(t, os.WriteFile(out, data, 0o644))
	return out
}

// TestRun_SupplementalEvaluatorMixedCellsPass pins the mixed-reference
// contract: a stored baseline authorizes the existing cell, the current
// Evaluator authorizes the new cell, and both verdicts pass.
func TestRun_SupplementalEvaluatorMixedCellsPass(t *testing.T) {
	tiersPath := writeEngineSensitiveTiers(t, "Goldset/existing", "Goldset/new")
	oldFile := writeBenchFile(t, "fixture-runner-a",
		repeatLine("BenchmarkGoldset/existing-2  100  10000 ns/op  100 B/op  2 allocs/op"))
	evaluatorFile := writeBenchFile(t, "fixture-runner-a",
		repeatLine("BenchmarkGoldset/existing-2  100  20000 ns/op  200 B/op  4 allocs/op")+
			repeatLine("BenchmarkGoldset/new-2  100  10000 ns/op  100 B/op  2 allocs/op"))
	candidateFile := writeBenchFile(t, "fixture-runner-a",
		repeatLine("BenchmarkGoldset/existing-2  100  10400 ns/op  100 B/op  2 allocs/op")+
			repeatLine("BenchmarkGoldset/new-2  100  8000 ns/op  80 B/op  2 allocs/op"))
	outPath := filepath.Join(t.TempDir(), "report.txt")
	swapBenchstat(t, func(_, _ string) ([]byte, error) {
		return []byte(pairedCSV("fixture-runner-a",
			"Goldset/existing-2,1e-05,1%,1.04e-05,1%,+4.00%,p=0.000 n=10",
			"Goldset/new-2,1e-05,1%,8e-06,1%,-20.00%,p=0.000 n=10")), nil
	})

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"-old", oldFile, "-evaluator", evaluatorFile, "-candidate", candidateFile,
		"-tiers", tiersPath, "-mode", "non-regression", "-race-clean=true", "-out", outPath,
	})

	assert.Equal(t, 0, code, "both authorized cells pass; stderr: %s", stderr.String())
	report, err := os.ReadFile(outPath)
	require.NoError(t, err, "a passing gate must write the report")
	assert.Equal(t, []string{
		"Goldset/existing-2: PASS",
		"Goldset/new-2: PASS (mode=first-authorization reference=evaluator)",
	}, reportBodyLines(t, string(report)),
		"the report must carry exactly the sorted existing and supplemental verdicts; report: %s", string(report))
}

// TestRun_StoredBaselineWinsOverFasterEvaluator pins reference precedence: a
// present stored baseline decides the existing cell even when the Evaluator
// comparison would have passed.
func TestRun_StoredBaselineWinsOverFasterEvaluator(t *testing.T) {
	tiersPath := writeEngineSensitiveTiers(t, "Goldset/existing", "Goldset/new")
	oldFile := writeBenchFile(t, "fixture-runner-a",
		repeatLine("BenchmarkGoldset/existing-2  100  10000 ns/op  100 B/op  2 allocs/op"))
	evaluatorFile := writeBenchFile(t, "fixture-runner-a",
		repeatLine("BenchmarkGoldset/existing-2  100  20000 ns/op  200 B/op  4 allocs/op")+
			repeatLine("BenchmarkGoldset/new-2  100  10000 ns/op  100 B/op  2 allocs/op"))
	candidateFile := writeBenchFile(t, "fixture-runner-a",
		repeatLine("BenchmarkGoldset/existing-2  100  11000 ns/op  100 B/op  2 allocs/op")+
			repeatLine("BenchmarkGoldset/new-2  100  8000 ns/op  80 B/op  2 allocs/op"))
	outPath := filepath.Join(t.TempDir(), "report.txt")

	swapBenchstat(t, func(_, _ string) ([]byte, error) {
		return []byte(pairedCSV("fixture-runner-a",
			"Goldset/existing-2,1e-05,1%,1.1e-05,1%,+10.00%,p=0.000 n=10",
			"Goldset/new-2,1e-05,1%,8e-06,1%,-20.00%,p=0.000 n=10")), nil
	})
	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"-old", oldFile, "-evaluator", evaluatorFile, "-candidate", candidateFile,
		"-tiers", tiersPath, "-mode", "non-regression", "-race-clean=true", "-out", outPath,
	})

	assert.Equal(t, 1, code, "the stored baseline's regression must fail the gate; stderr: %s", stderr.String())
	report, err := os.ReadFile(outPath)
	require.NoError(t, err, "a failed gate still writes its report")
	assert.Contains(t, string(report), "Goldset/existing-2: FAIL (latency regressed 10.00%",
		"the existing cell must fail against the stored baseline, not the Evaluator; report: %s", string(report))
}

// TestRun_MissingEvaluatorForNewCellExitsThree pins the supplemental
// preflight: a new cell with no stored baseline and no -evaluator flag must
// refuse before any comparison, statistics, or report write.
func TestRun_MissingEvaluatorForNewCellExitsThree(t *testing.T) {
	tiersPath := writeEngineSensitiveTiers(t, "Goldset/existing", "Goldset/new")
	oldFile := writeBenchFile(t, "fixture-runner-a",
		repeatLine("BenchmarkGoldset/existing-2  100  10000 ns/op  100 B/op  2 allocs/op"))
	candidateFile := writeBenchFile(t, "fixture-runner-a",
		repeatLine("BenchmarkGoldset/existing-2  100  10400 ns/op  100 B/op  2 allocs/op")+
			repeatLine("BenchmarkGoldset/new-2  100  8000 ns/op  80 B/op  2 allocs/op"))
	outPath := filepath.Join(t.TempDir(), "report.txt")

	code, stdout, stderr := runGateWithTiers(t, tiersPath,
		"-old", oldFile, "-candidate", candidateFile,
		"-mode", "non-regression", "-race-clean=true", "-out", outPath)

	assert.Equal(t, 3, code, "a new cell without -evaluator must exit 3; stderr: %s", stderr)
	assert.Empty(t, stdout, "a refused preflight writes no report; stdout: %s", stdout)
	assert.Contains(t, stderr, `cell "Goldset/new-2": missing current Evaluator reference (requires -evaluator)`,
		"stderr must name the cell and the missing flag: %s", stderr)
	_, err := os.Stat(outPath)
	assert.True(t, os.IsNotExist(err), "no report may be written for a refused preflight")
}

// TestRun_UnusedInvalidEvaluatorPathIgnored pins that the supplemental path
// opens only when a configured cell actually needs it.
func TestRun_UnusedInvalidEvaluatorPathIgnored(t *testing.T) {
	tiersPath := writeEngineSensitiveTiers(t, "Goldset/existing")
	oldFile := writeBenchFile(t, "fixture-runner-a",
		repeatLine("BenchmarkGoldset/existing-2  100  10000 ns/op  100 B/op  2 allocs/op"))
	candidateFile := writeBenchFile(t, "fixture-runner-a",
		repeatLine("BenchmarkGoldset/existing-2  100  10400 ns/op  100 B/op  2 allocs/op"))
	outPath := filepath.Join(t.TempDir(), "report.txt")

	swapBenchstat(t, func(_, _ string) ([]byte, error) {
		return []byte(pairedCSV("fixture-runner-a",
			"Goldset/existing-2,1e-05,1%,1.04e-05,1%,+4.00%,p=0.000 n=10")), nil
	})
	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"-old", oldFile, "-evaluator", filepath.Join(t.TempDir(), "does-not-exist.txt"),
		"-candidate", candidateFile, "-tiers", tiersPath, "-mode", "non-regression",
		"-race-clean=true", "-out", outPath,
	})

	assert.Equal(t, 0, code, "an unused evaluator path must not be opened; stderr: %s", stderr.String())
	report, err := os.ReadFile(outPath)
	require.NoError(t, err, "a passing gate writes its report")
	assert.Equal(t, []string{"Goldset/existing-2: PASS"}, reportBodyLines(t, string(report)),
		"the report must carry only the baseline-backed verdict; report: %s", string(report))
}

// TestRun_AllNewCellsCountAsComparisons pins that a fully supplemental
// inventory performs real comparisons: no zero-comparison refusal, no
// historical leakage from the unconfigured stored baseline.
func TestRun_AllNewCellsCountAsComparisons(t *testing.T) {
	tiersPath := writeEngineSensitiveTiers(t, "Goldset/new-a", "Goldset/new-b")
	oldFile := writeBenchFile(t, "fixture-runner-b",
		repeatLine("BenchmarkGoldset/historical-2  100  9000 ns/op  16 B/op  1 allocs/op"))
	evaluatorFile := writeBenchFile(t, "fixture-runner-a",
		repeatLine("BenchmarkGoldset/new-a-2  100  10000 ns/op  100 B/op  2 allocs/op")+
			repeatLine("BenchmarkGoldset/new-b-2  100  20000 ns/op  200 B/op  4 allocs/op"))
	candidateFile := writeBenchFile(t, "fixture-runner-a",
		repeatLine("BenchmarkGoldset/new-a-2  100  8000 ns/op  80 B/op  2 allocs/op")+
			repeatLine("BenchmarkGoldset/new-b-2  100  16000 ns/op  160 B/op  4 allocs/op"))
	outPath := filepath.Join(t.TempDir(), "report.txt")

	swapBenchstat(t, func(_, _ string) ([]byte, error) {
		return []byte(pairedCSV("fixture-runner-a",
			"Goldset/new-a-2,1e-05,1%,8e-06,1%,-20.00%,p=0.000 n=10",
			"Goldset/new-b-2,2e-05,1%,1.6e-05,1%,-20.00%,p=0.000 n=10")), nil
	})
	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"-old", oldFile, "-evaluator", evaluatorFile, "-candidate", candidateFile,
		"-tiers", tiersPath, "-mode", "non-regression", "-race-clean=true", "-out", outPath,
	})

	assert.Equal(t, 0, code, "both supplemental cells pass; stderr: %s", stderr.String())
	assert.Empty(t, stderr, "a clean supplemental run writes no stderr: %s", stderr.String())
	report, err := os.ReadFile(outPath)
	require.NoError(t, err, "a passing gate writes its report")
	assert.Equal(t, []string{
		"Goldset/new-a-2: PASS (mode=first-authorization reference=evaluator)",
		"Goldset/new-b-2: PASS (mode=first-authorization reference=evaluator)",
	}, reportBodyLines(t, string(report)),
		"the report must carry exactly the two supplemental verdicts; report: %s", string(report))
}

// TestRun_CurrentEvaluatorMissingTimingExitsThree pins the supplemental
// metric floor: a selected Evaluator reference without sec/op refuses before
// comparisons or report writes.
func TestRun_CurrentEvaluatorMissingTimingExitsThree(t *testing.T) {
	tiersPath := writeEngineSensitiveTiers(t, "Goldset/existing", "Goldset/new")
	oldFile := writeBenchFile(t, "fixture-runner-a",
		repeatLine("BenchmarkGoldset/existing-2  100  10000 ns/op  100 B/op  2 allocs/op"))
	evaluatorFile := writeBenchFile(t, "fixture-runner-a",
		repeatLine("BenchmarkGoldset/existing-2  100  20000 ns/op  200 B/op  4 allocs/op")+
			repeatLine("BenchmarkGoldset/new-2  100  100 B/op  2 allocs/op"))
	candidateFile := writeBenchFile(t, "fixture-runner-a",
		repeatLine("BenchmarkGoldset/existing-2  100  10400 ns/op  100 B/op  2 allocs/op")+
			repeatLine("BenchmarkGoldset/new-2  100  8000 ns/op  80 B/op  2 allocs/op"))
	outPath := filepath.Join(t.TempDir(), "report.txt")

	code, stdout, stderr := runGateWithTiers(t, tiersPath,
		"-old", oldFile, "-evaluator", evaluatorFile, "-candidate", candidateFile,
		"-mode", "non-regression", "-race-clean=true", "-out", outPath)

	assert.Equal(t, 3, code, "a supplemental reference missing sec/op must exit 3; stderr: %s", stderr)
	assert.Empty(t, stdout, "a refused preflight writes no report; stdout: %s", stdout)
	assert.Contains(t, stderr, `cell "Goldset/new-2": current Evaluator reference is missing sec/op`,
		"stderr must name the cell and the missing timing: %s", stderr)
	_, err := os.Stat(outPath)
	assert.True(t, os.IsNotExist(err), "no report may be written for a refused preflight")
}
