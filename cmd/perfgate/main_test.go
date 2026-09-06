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

	t.Run("latency inconclusive naming both runners", func(t *testing.T) {
		code, stdout, stderr := runGate(t, "-old", intelRun, "-candidate", amdRun, "-mode", "non-regression")

		out := stdout + stderr
		assert.Contains(t, out, intel, "the cross-runner report must name the baseline runner")
		assert.Contains(t, out, amd, "the cross-runner report must name the candidate runner")

		names := candidateCells(t)
		assert.Equal(t, uniformVerdicts(names, "INCONCLUSIVE"), verdictsFor(t, stdout, names),
			"a latency conclusion across differing runners is not licensed")
		assert.Equal(t, 0, code, "a rerun cannot change which machine the stored baseline came from, so a runner mismatch raises no needs-rerun signal and the release stays authorized on the comparable bytes and allocs axes; stderr: %s", stderr)
	})

	// Roles swapped for both subtests below: the candidate is the Intel run,
	// which reads higher B/op on every cell and one more allocation on each
	// Goldset/ cell. The two axes part company there -- the reader cells move
	// on bytes alone.
	t.Run("bytes inconclusive across differing runners", func(t *testing.T) {
		_, stdout, stderr := runGate(t, "-old", amdRun, "-candidate", intelRun, "-mode", "non-regression")

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
		assert.Empty(t, unexplained, "an undecided cell must name the bytes axis alongside latency, and both runners")
	})

	t.Run("allocation counts still enforced across differing runners", func(t *testing.T) {
		code, stdout, stderr := runGate(t, "-old", amdRun, "-candidate", intelRun, "-mode", "non-regression")

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
	})
}

// TestCrossRunner_CleanRunExitsZero is the release the narrowing exists to let
// through: a cross-runner pair whose allocation counts all held, with only the
// two machine-dependent axes left undecided. Nothing there is a rerun's to
// resolve, so every cell collapses and the gate authorizes the release.
func TestCrossRunner_CleanRunExitsZero(t *testing.T) {
	amdRun := filepath.Join(pinnedProfileDir, "bench-vm.txt")
	intelParseRun := keepingCells(t, filepath.Join(crossRunnerBaselineDir, "bench-vm.txt"), "BenchmarkGoldsetParse/")

	code, stdout, stderr := runGate(t, "-old", amdRun, "-candidate", intelParseRun, "-mode", "non-regression")

	parseCells := cellsWithPrefix(t, candidateCells(t), "GoldsetParse/", 13)
	assert.Equal(t, uniformVerdicts(parseCells, "INCONCLUSIVE"), verdictsFor(t, stdout, parseCells),
		"the reader cells hold their allocation counts, so only the undecided axes are left")
	assert.Equal(t, 0, code, "a cross-runner run whose allocation counts all held authorizes the release; stderr: %s", stderr)
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
// measures and the baseline never did. Absence of a baseline figure is not a
// zero: judged against one, every newly added cell would fail its first
// release.
func TestCrossRunner_MissingBaselineCellIsInconclusive(t *testing.T) {
	candidate := filepath.Join(pinnedProfileDir, "bench-vm.txt")
	baseline := filepath.Join(crossRunnerBaselineDir, "bench-vm.txt")

	_, stdout, _ := runGate(t, "-old", baseline, "-candidate", candidate, "-mode", "non-regression")

	lines := reportLines(t, stdout)
	line, ok := lines[callBoundaryCell]
	require.True(t, ok, "cell %q missing from the report:\n%s", callBoundaryCell, stdout)
	assert.Equal(t, "INCONCLUSIVE", line.verdict, "a cell with no baseline figure is neither passed nor failed")
	assert.Contains(t, line.reason, "baseline", "the report must say the baseline figure is what is missing")
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
