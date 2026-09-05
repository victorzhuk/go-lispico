package main

import (
	"bytes"
	"errors"
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

const (
	// crossRunnerBaselineDir is a real stored VM baseline from a different
	// runner than pinnedProfileDir's -- the post-authorization shape the gate
	// meets on every later release.
	crossRunnerBaselineDir = "../../internal/perfgate/testdata/profile-30614184386"
	// callBoundaryCell exists in pinnedProfileDir and not in
	// crossRunnerBaselineDir, so it has no baseline figure to be judged against.
	callBoundaryCell = "GoldsetCall/call-boundary-2"
)

// TestCrossRunner_LatencyInconclusiveBytesEnforced pairs two committed VM runs
// from different runners. Latency across a hardware change is not evidence, so
// every cell's latency verdict is inconclusive and names both runners; the
// exact byte and allocation counts survive the change and stay enforced, which
// the swapped-roles subtest proves by failing on them.
func TestCrossRunner_LatencyInconclusiveBytesEnforced(t *testing.T) {
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
		assert.Equal(t, 2, code, "every cell inconclusive is the needs-rerun signal; stderr: %s", stderr)
	})

	t.Run("bytes still enforced across differing runners", func(t *testing.T) {
		// Roles swapped: the older run allocates more on every cell, so a gate
		// that skipped the bytes axis when the identities differ would report
		// the same all-inconclusive result as the subtest above.
		code, stdout, stderr := runGate(t, "-old", amdRun, "-candidate", intelRun, "-mode", "non-regression")

		names := withoutCell(candidateCells(t), callBoundaryCell)
		assert.Equal(t, uniformVerdicts(names, "FAIL"), verdictsFor(t, stdout, names),
			"a bytes regression stays a failure when the two runners differ")

		lines := reportLines(t, stdout)
		var unexplained []string
		for _, name := range names {
			if !strings.Contains(lines[name].reason, "bytes increased") {
				unexplained = append(unexplained, name+": "+lines[name].reason)
			}
		}
		assert.Empty(t, unexplained, "every cross-runner failure must name the bytes axis it was decided on")
		assert.Equal(t, 1, code, "a bytes regression must fail the gate even when latency is inconclusive; stderr: %s", stderr)
	})
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
	assert.Equal(t, 2, code, "an unknown identity leaves every latency cell needing a rerun; stderr: %s", stderr)
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
