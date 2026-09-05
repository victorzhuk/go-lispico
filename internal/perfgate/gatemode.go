package perfgate

import "errors"

// BaselineLookup is what a release run observed while looking for the stored
// bench-vm.txt baseline: whether enumerating releases succeeded, which release
// tags were inspected, which tag the asset was downloaded from, and whether an
// asset that was listed then failed to download.
type BaselineLookup struct {
	EnumerationOK  bool
	Tags           []string
	DownloadedTag  string
	DownloadFailed bool
}

// BaselineOutcome is the four-way taxonomy of a baseline lookup. Absence is
// decided from the enumerated asset lists, never from a download exit code:
// only a known absence may select first-authorization thresholds.
type BaselineOutcome int

const (
	// The taxonomy starts at 1 so that no outcome is the zero value: an
	// uninitialized BaselineOutcome must not read as a baseline having been
	// found, the same fail-closed rule ModeUnknown and VerdictUnknown follow.
	BaselineFound BaselineOutcome = iota + 1
	BaselineAbsent
	BaselineEnumerationFailed
	BaselineDownloadFailed
)

var (
	// ErrBaselineEnumerationFailed reports that listing releases, or listing
	// one release's assets, failed — the repository's baseline state is
	// unknown, so no threshold branch may be selected.
	ErrBaselineEnumerationFailed = errors.New("perfgate: enumerating releases failed")

	// ErrBaselineDownloadFailed reports that a baseline asset was seen in a
	// release's asset list and the download then failed.
	ErrBaselineDownloadFailed = errors.New("perfgate: downloading the stored baseline failed")
)

// ResolveGateMode classifies a baseline lookup into a gate mode. A lookup that
// failed for any reason other than the baseline not existing yields
// ModeUnknown and an error, never a threshold branch.
func ResolveGateMode(lookup BaselineLookup) (Mode, BaselineOutcome, error) {
	switch {
	case !lookup.EnumerationOK:
		return ModeUnknown, BaselineEnumerationFailed, ErrBaselineEnumerationFailed
	case lookup.DownloadFailed:
		return ModeUnknown, BaselineDownloadFailed, ErrBaselineDownloadFailed
	case lookup.DownloadedTag != "":
		return ModeNonRegression, BaselineFound, nil
	default:
		return ModeFirstAuthorization, BaselineAbsent, nil
	}
}
