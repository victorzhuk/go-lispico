package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
