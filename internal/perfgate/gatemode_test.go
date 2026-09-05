package perfgate

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resolveGateMode keeps a not-yet-implemented ResolveGateMode from aborting the
// test binary, so every case in the taxonomy reports its own failure.
func resolveGateMode(t *testing.T, lookup BaselineLookup) (mode Mode, outcome BaselineOutcome, err error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ResolveGateMode(%+v) panicked: %v", lookup, r)
		}
	}()
	return ResolveGateMode(lookup)
}

func TestResolveGateMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		lookup      BaselineLookup
		wantMode    Mode
		wantOutcome BaselineOutcome
		wantErr     error
	}{
		{
			name:        "a downloaded baseline selects non-regression",
			lookup:      BaselineLookup{EnumerationOK: true, Tags: []string{"v0.12.0"}, DownloadedTag: "v0.12.0"},
			wantMode:    ModeNonRegression,
			wantOutcome: BaselineFound,
		},
		{
			name:        "every enumerated release inspected without the asset selects first authorization",
			lookup:      BaselineLookup{EnumerationOK: true, Tags: []string{"v0.11.0", "v0.12.0"}},
			wantMode:    ModeFirstAuthorization,
			wantOutcome: BaselineAbsent,
		},
		{
			name:        "a repository with no releases holds no baseline",
			lookup:      BaselineLookup{EnumerationOK: true},
			wantMode:    ModeFirstAuthorization,
			wantOutcome: BaselineAbsent,
		},
		{
			name:        "enumeration failure is unknown, not absence",
			lookup:      BaselineLookup{EnumerationOK: false},
			wantMode:    ModeUnknown,
			wantOutcome: BaselineEnumerationFailed,
			wantErr:     ErrBaselineEnumerationFailed,
		},
		{
			name:        "a listed asset that fails to download is unknown, not absence",
			lookup:      BaselineLookup{EnumerationOK: true, Tags: []string{"v0.12.0"}, DownloadFailed: true},
			wantMode:    ModeUnknown,
			wantOutcome: BaselineDownloadFailed,
			wantErr:     ErrBaselineDownloadFailed,
		},
		{
			name:        "the zero lookup fails closed",
			lookup:      BaselineLookup{},
			wantMode:    ModeUnknown,
			wantOutcome: BaselineEnumerationFailed,
			wantErr:     ErrBaselineEnumerationFailed,
		},
		{
			name:        "a dispatch run against a repository holding a baseline agrees with a release run",
			lookup:      BaselineLookup{EnumerationOK: true, Tags: []string{"v0.13.0"}, DownloadedTag: "v0.13.0"},
			wantMode:    ModeNonRegression,
			wantOutcome: BaselineFound,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mode, outcome, err := resolveGateMode(t, tt.lookup)
			if tt.wantErr == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tt.wantErr)
			}
			assert.Equal(t, tt.wantMode, mode)
			assert.Equal(t, tt.wantOutcome, outcome)
		})
	}
}

// TestResolveGateMode_FailureNeverSelectsFirstAuthorization pins the rule the
// unchecked `gh release list` broke: a lookup that failed must not be read as
// the repository holding no baseline.
func TestResolveGateMode_FailureNeverSelectsFirstAuthorization(t *testing.T) {
	t.Parallel()

	failures := []struct {
		name   string
		lookup BaselineLookup
	}{
		{
			name:   "enumeration failed",
			lookup: BaselineLookup{EnumerationOK: false, Tags: []string{"v0.12.0"}},
		},
		{
			name:   "download failed",
			lookup: BaselineLookup{EnumerationOK: true, Tags: []string{"v0.12.0"}, DownloadFailed: true},
		},
	}
	for _, tt := range failures {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mode, outcome, err := resolveGateMode(t, tt.lookup)
			require.Error(t, err)
			assert.NotEqual(t, ModeFirstAuthorization, mode)
			assert.Equal(t, ModeUnknown, mode)
			assert.NotEqual(t, BaselineAbsent, outcome)
			assert.True(t,
				errors.Is(err, ErrBaselineEnumerationFailed) || errors.Is(err, ErrBaselineDownloadFailed),
				"a resolution failure must carry one of the baseline sentinels, got %v", err)
		})
	}
}
