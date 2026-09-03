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

// invTrackedChanges are the changes that own an unbounded phase. A row that
// defers its bound has to name one, so "unbounded" is a tracked defect with an
// owner rather than a shrug.
var invTrackedChanges = []string{
	"core-value-walk-sharing-bound",
	"format-mismatched-verb-bound",
}

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

// invNamesToken matches whole tokens, not substrings: a proof that merely
// mentions a name in prose, or names a lookalike such as .EqualsFold, is not
// evidence that the phase leans on the thing named.
func invNamesToken(proof string, allowed []string) bool {
	for _, token := range strings.Fields(proof) {
		token = strings.TrimLeft(token, "([{\"'`")
		token = strings.TrimRight(token, ".,;:)]}\"'`")
		for _, want := range allowed {
			if token == want {
				return true
			}
		}
	}
	return false
}

func invNamesTrustedCallee(proof string) bool { return invNamesToken(proof, invTrustedHostCallees) }

func invNamesTrackedChange(proof string) bool { return invNamesToken(proof, invTrackedChanges) }

// invSupportOnly reports whether a row belongs solely to the support family.
// Nothing in support is registered under a Lisp name, so such a row names none.
func invSupportOnly(families []string) bool {
	return len(families) == 1 && families[0] == "support"
}

// invCheckRowNames enforces the Fn contract: a support-only row carries an
// empty Fn, and every other row names at least one known builtin or adapter id.
// Whatever it names is folded into covered.
func invCheckRowNames(t *testing.T, families []string, fn, site string, known, covered map[string]bool) {
	t.Helper()

	if invSupportOnly(families) {
		if fn != "" {
			t.Errorf("%s: support-only row names %q; support registers nothing, so Fn must be empty", site, fn)
		}
		return
	}

	names := invRowNames(fn)
	if len(names) == 0 {
		t.Errorf("%s: row names no builtin; only a support-only row may leave Fn empty", site)
		return
	}
	for _, name := range names {
		if !known[name] {
			t.Errorf("%s: %q is neither a registered name nor an adapter id", site, name)
		}
		covered[name] = true
	}
}

func invRowKey(parts ...string) string {
	return strings.Join(parts, "\x00")
}

func TestWorkInventory_CoversEveryRegisteredName(t *testing.T) {
	known := invKnownNames()
	covered := make(map[string]bool)
	seen := make(map[string]bool)

	for _, row := range inventory.WorkPhases {
		site := row.File + ":" + row.Func + ":" + row.PhaseLabel
		key := invRowKey(row.Fn, row.File, row.Func, row.PhaseLabel)
		if seen[key] {
			t.Errorf("%s: duplicate WorkPhases row for %q", site, row.Fn)
		}
		seen[key] = true

		invCheckRowNames(t, row.Families, row.Fn, site, known, covered)
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
		site := row.File + ":" + row.Func + ":" + row.BranchLabel
		key := invRowKey(row.Fn, row.File, row.Func, row.BranchLabel)
		if seen[key] {
			t.Errorf("%s: duplicate ResultBranches row for %q", site, row.Fn)
		}
		seen[key] = true

		invCheckRowNames(t, row.Families, row.Fn, site, known, covered)
		if !classes[row.Class] {
			t.Errorf("%s: class %q is not a declared result class", site, row.Class)
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
// it relies on and where that bound comes from, and a phase that has no bound
// at all has to name the change that will remove it.
func TestWorkInventory_BoundedExceptionsCarryProofAndMaxWork(t *testing.T) {
	for _, row := range inventory.WorkPhases {
		switch row.Disposition {
		case "bounded-exception":
			if row.Proof == "" {
				t.Errorf("%s:%s:%s: bounded-exception carries no Proof", row.File, row.Func, row.PhaseLabel)
			}
			if row.MaxWork == 0 {
				t.Errorf("%s:%s:%s: bounded-exception carries no MaxWork", row.File, row.Func, row.PhaseLabel)
			}
		case "unbounded-tracked":
			if row.Proof == "" {
				t.Errorf("%s:%s:%s: unbounded-tracked carries no Proof", row.File, row.Func, row.PhaseLabel)
			}
			if !invNamesTrackedChange(row.Proof) {
				t.Errorf("%s:%s:%s: unbounded-tracked Proof names no change in %s",
					row.File, row.Func, row.PhaseLabel, strings.Join(invTrackedChanges, ", "))
			}
			if row.MaxWork != 0 {
				t.Errorf("%s:%s:%s: unbounded-tracked must not state a MaxWork; there is no ceiling to state",
					row.File, row.Func, row.PhaseLabel)
			}
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
