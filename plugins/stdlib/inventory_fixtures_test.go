package stdlib

import (
	"path/filepath"
	"strings"
	"testing"
)

// invFixtureRoot resolves one fixture case directory. The reconcilers take a
// scan root, so each case is a self-contained miniature source tree.
func invFixtureRoot(t *testing.T, caseName string) string {
	t.Helper()
	return filepath.Join(moduleRoot(t), "plugins", "stdlib", "testdata", "inventory", caseName)
}

// invFixtureFindings runs both reconcilers over one case and combines the
// results: a work-shaped defect must not be silently satisfied by a
// result-shaped finding, or the reverse.
func invFixtureFindings(t *testing.T, caseName string) []string {
	t.Helper()
	root := invFixtureRoot(t, caseName)
	migrated := fixtureMigrated()
	out := append([]string(nil), reconcileWork(root, fixturePhases(caseName), migrated)...)
	return append(out, reconcileResult(root, fixtureResults(caseName), migrated)...)
}

// invFindingCode is the leading token of a finding. Fixtures assert on the code
// alone so rewording a finding's detail text cannot rot them.
func invFindingCode(finding string) string {
	code, _, _ := strings.Cut(finding, " ")
	return code
}

func invAssertSingleFinding(t *testing.T, caseName, wantCode string) {
	t.Helper()
	findings := invFixtureFindings(t, caseName)
	if len(findings) != 1 {
		t.Fatalf("case %s: want exactly 1 finding with code %s, got %d: %q",
			caseName, wantCode, len(findings), findings)
	}
	if got := invFindingCode(findings[0]); got != wantCode {
		t.Fatalf("case %s: want code %s, got %s in %q", caseName, wantCode, got, findings)
	}
}

func TestInventoryFixtures_CatchMissingRegistration(t *testing.T) {
	invAssertSingleFinding(t, "missing_registration", "MISSING_REGISTRATION")
}

func TestInventoryFixtures_CatchHelperOnlyLoop(t *testing.T) {
	invAssertSingleFinding(t, "helper_only_loop", "HELPER_ONLY_LOOP")
}

func TestInventoryFixtures_CatchOpaqueCall(t *testing.T) {
	invAssertSingleFinding(t, "opaque_call", "OPAQUE_CALL")
}

func TestInventoryFixtures_CatchUnflushedReturn(t *testing.T) {
	invAssertSingleFinding(t, "unflushed_return", "UNFLUSHED_RETURN")
}

func TestInventoryFixtures_CatchDuplicateCallbackCharge(t *testing.T) {
	invAssertSingleFinding(t, "duplicate_callback_charge", "DUPLICATE_CALLBACK_CHARGE")
}

func TestInventoryFixtures_CatchUnclassifiedResultBranch(t *testing.T) {
	invAssertSingleFinding(t, "unclassified_result_branch", "UNCLASSIFIED_RESULT_BRANCH")
}

func TestInventoryFixtures_CatchPrePostOnlyDisposition(t *testing.T) {
	invAssertSingleFinding(t, "prepost_only", "PREPOST_ONLY_DISPOSITION")
}

// The negative case of the set. The seven above prove each finding fires; a
// reconciler that flagged every source would pass all of them, so this one
// pins the other side — compliant source under the same scan yields nothing.
func TestInventoryFixtures_CompliantFixturePasses(t *testing.T) {
	if findings := invFixtureFindings(t, "compliant"); len(findings) != 0 {
		t.Fatalf("case compliant: want no findings, got %d: %q", len(findings), findings)
	}
}
