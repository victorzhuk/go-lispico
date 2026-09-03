package stdlib

import (
	"sort"
	"testing"

	"github.com/victorzhuk/go-lispico/internal/inventory"
)

// TestAllFamiliesMigrated closes the migration gate. Every reconciler in this
// package consults FamilyMigrated before it demands rows, so a family left
// false disarms its own coverage checks instead of failing them, and a flag
// naming no declared family gates nothing at all. Both are silent everywhere
// else: the family goldens in package runtime only reach families that register
// a Lisp name, which leaves support unwatched.
func TestAllFamiliesMigrated(t *testing.T) {
	declared := invStringSet(inventory.Families)

	for _, family := range inventory.Families {
		migrated, ok := inventory.FamilyMigrated[family]
		switch {
		case !ok:
			t.Errorf("family %q is declared but carries no FamilyMigrated flag", family)
		case !migrated:
			t.Errorf("family %q is declared but not migrated", family)
		}
	}

	var undeclared []string
	for family := range inventory.FamilyMigrated {
		if !declared[family] {
			undeclared = append(undeclared, family)
		}
	}
	sort.Strings(undeclared)
	for _, family := range undeclared {
		t.Errorf("FamilyMigrated flags %q, which inventory.Families does not declare", family)
	}
}
