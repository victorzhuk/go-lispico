package stdlib

import "github.com/victorzhuk/go-lispico/internal/inventory"

// These accessors carry the fixture data the sealed fixture assertions read.
// The phase and result sets deliberately stay empty until the fixture cases
// land, so the assertions start from a known-red baseline.

func fixturePhases(caseName string) []inventory.WorkPhase {
	return nil
}

func fixtureResults(caseName string) []inventory.ResultBranch {
	return nil
}

// Fixtures scan a self-contained fixture root, so the family gate must never
// suppress a fixture finding the way inventory.FamilyMigrated would.
func fixtureMigrated() map[string]bool {
	migrated := make(map[string]bool, len(inventory.Families))
	for _, family := range inventory.Families {
		migrated[family] = true
	}
	return migrated
}
