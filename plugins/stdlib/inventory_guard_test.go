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

// TestWorkInventory_NoUndisposedException ties a ceiling to the one disposition
// that gives it meaning. The source reconciler only asks a bounded-exception row
// for a non-zero MaxWork, so two shapes pass everywhere else: a ceiling on a row
// whose disposition enforces nothing by it, and a negative ceiling on a row that
// claims to be bounded by it.
func TestWorkInventory_NoUndisposedException(t *testing.T) {
	for _, row := range inventory.WorkPhases {
		exception := row.Disposition == "bounded-exception"
		switch {
		case exception && row.MaxWork <= 0:
			t.Errorf("%s %s:%s %q is a bounded-exception phase with MaxWork %d; a bounded exception needs a positive ceiling",
				row.Fn, row.File, row.Func, row.PhaseLabel, row.MaxWork)
		case !exception && row.MaxWork > 0:
			t.Errorf("%s %s:%s %q carries MaxWork %d under disposition %q; only a bounded-exception phase declares a ceiling",
				row.Fn, row.File, row.Func, row.PhaseLabel, row.MaxWork, row.Disposition)
		}
	}
}
