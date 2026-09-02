package stdlib

import (
	"sort"
	"strings"
	"testing"

	"github.com/victorzhuk/go-lispico/internal/inventory"
)

// invTrustedHostCallees are the only callees a trusted-host phase may lean on.
// Each is a value method the host already bounds, so the phase inherits that
// bound instead of needing a budget of its own.
var invTrustedHostCallees = []string{".Equals", ".String", ".Type", "core.EqualsBounded"}

// invKnownNames is the closed set of names a row may name: every registered
// builtin plus every CL adapter id.
func invKnownNames() map[string]bool {
	known := make(map[string]bool)
	for _, names := range inventory.RegisteredNames {
		for _, name := range names {
			known[name] = true
		}
	}
	for _, id := range inventory.CLAdapterIDs {
		known[id] = true
	}
	return known
}

// invMigratedNames lists, sorted, the names whose family has been migrated. A
// name in an unmigrated family is owed no row yet, so the coverage assertions
// go red exactly when a family flips without its rows landing.
func invMigratedNames() []string {
	var out []string
	for name := range invKnownNames() {
		if inventory.FamilyMigrated[inventory.NameFamily[name]] {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// invRowNames splits a row's Fn field. A site inside a shared factory serves
// several builtins and records them space-separated; a support-family row names
// no Lisp function at all and splits to nothing.
func invRowNames(fn string) []string {
	return strings.Fields(fn)
}

func invStringSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, v := range values {
		set[v] = true
	}
	return set
}

func invNamesTrustedCallee(proof string) bool {
	for _, callee := range invTrustedHostCallees {
		if strings.Contains(proof, callee) {
			return true
		}
	}
	return false
}

func invRowKey(parts ...string) string {
	return strings.Join(parts, "\x00")
}

func TestWorkInventory_CoversEveryRegisteredName(t *testing.T) {
	known := invKnownNames()
	covered := make(map[string]bool)
	seen := make(map[string]bool)

	for _, row := range inventory.WorkPhases {
		key := invRowKey(row.Fn, row.File, row.Func, row.PhaseLabel)
		if seen[key] {
			t.Errorf("%s:%s:%s: duplicate WorkPhases row for %q", row.File, row.Func, row.PhaseLabel, row.Fn)
		}
		seen[key] = true

		for _, name := range invRowNames(row.Fn) {
			if !known[name] {
				t.Errorf("%s:%s:%s: %q is neither a registered name nor an adapter id", row.File, row.Func, row.PhaseLabel, name)
			}
			covered[name] = true
		}
	}

	for _, name := range invMigratedNames() {
		if !covered[name] {
			t.Errorf("%q: family %q is migrated but no WorkPhases row names it", name, inventory.NameFamily[name])
		}
	}
}

func TestResultInventory_CoversEveryRegisteredName(t *testing.T) {
	known := invKnownNames()
	classes := invStringSet(inventory.ResultClasses)
	dispositions := invStringSet(inventory.Dispositions)
	covered := make(map[string]bool)
	seen := make(map[string]bool)

	for _, row := range inventory.ResultBranches {
		key := invRowKey(row.Fn, row.File, row.Func, row.BranchLabel)
		if seen[key] {
			t.Errorf("%s:%s:%s: duplicate ResultBranches row for %q", row.File, row.Func, row.BranchLabel, row.Fn)
		}
		seen[key] = true

		for _, name := range invRowNames(row.Fn) {
			if !known[name] {
				t.Errorf("%s:%s:%s: %q is neither a registered name nor an adapter id", row.File, row.Func, row.BranchLabel, name)
			}
			covered[name] = true
		}
		if !classes[row.Class] {
			t.Errorf("%s:%s:%s: class %q is not a declared result class", row.File, row.Func, row.BranchLabel, row.Class)
		}
	}

	for _, row := range inventory.WorkPhases {
		if !dispositions[row.Disposition] {
			t.Errorf("%s:%s:%s: disposition %q is not a declared disposition", row.File, row.Func, row.PhaseLabel, row.Disposition)
		}
	}

	for _, name := range invMigratedNames() {
		if !covered[name] {
			t.Errorf("%q: family %q is migrated but no ResultBranches row names it", name, inventory.NameFamily[name])
		}
	}
}

// TestWorkInventory_BoundedExceptionsCarryProofAndMaxWork keeps the escape
// hatch expensive: a phase that opts out of the budget has to state the bound
// it relies on and where that bound comes from.
func TestWorkInventory_BoundedExceptionsCarryProofAndMaxWork(t *testing.T) {
	for _, row := range inventory.WorkPhases {
		if row.Disposition != "bounded-exception" {
			continue
		}
		if row.Proof == "" {
			t.Errorf("%s:%s:%s: bounded-exception carries no Proof", row.File, row.Func, row.PhaseLabel)
		}
		if row.MaxWork == 0 {
			t.Errorf("%s:%s:%s: bounded-exception carries no MaxWork", row.File, row.Func, row.PhaseLabel)
		}
	}
}

func TestWorkInventory_TrustedHostRowsNameAllowedCallee(t *testing.T) {
	for _, row := range inventory.WorkPhases {
		if row.Disposition != "trusted-host" {
			continue
		}
		if !invNamesTrustedCallee(row.Proof) {
			t.Errorf("%s:%s:%s: trusted-host Proof names no callee in %s",
				row.File, row.Func, row.PhaseLabel, strings.Join(invTrustedHostCallees, ", "))
		}
	}
}

// TestWorkInventory_EveryRowHasNonEmptyFamilies guards the gate itself: a row
// with no family is reconciled under no seam, so an empty set would silently
// exempt the row from every family flip.
func TestWorkInventory_EveryRowHasNonEmptyFamilies(t *testing.T) {
	families := invStringSet(inventory.Families)

	for _, row := range inventory.WorkPhases {
		if len(row.Families) == 0 {
			t.Errorf("%s:%s:%s: WorkPhases row names no family", row.File, row.Func, row.PhaseLabel)
		}
		for _, family := range row.Families {
			if !families[family] {
				t.Errorf("%s:%s:%s: %q is not a declared family", row.File, row.Func, row.PhaseLabel, family)
			}
		}
	}

	for _, row := range inventory.ResultBranches {
		if len(row.Families) == 0 {
			t.Errorf("%s:%s:%s: ResultBranches row names no family", row.File, row.Func, row.BranchLabel)
		}
		for _, family := range row.Families {
			if !families[family] {
				t.Errorf("%s:%s:%s: %q is not a declared family", row.File, row.Func, row.BranchLabel, family)
			}
		}
	}
}
