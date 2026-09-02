package stdlib

import "github.com/victorzhuk/go-lispico/internal/inventory"

// These accessors carry the fixture data the sealed fixture assertions read.
// Each case records every phase and every branch its source holds except the
// one defect the case exists to catch, so each yields exactly one finding.

// fixtureFile is the scoped path every case reuses. A path outside
// invScopeFiles is never scanned, and one outside invFileFamilies resolves to
// no family, which would add a second finding of its own.
const fixtureFile = "plugins/stdlib/collections.go"

func fixturePhases(caseName string) []inventory.WorkPhase {
	switch caseName {
	// The walk in countAll is deliberately left unrecorded.
	case "missing_registration":
		return nil

	case "helper_only_loop":
		return []inventory.WorkPhase{
			{
				Families:    []string{"collection"},
				Fn:          "count",
				File:        fixtureFile,
				Func:        "countAll",
				PhaseLabel:  "element walk",
				Disposition: "budgeted",
			},
			{
				Families:    []string{"collection"},
				Fn:          "count",
				File:        fixtureFile,
				Func:        "countElements",
				PhaseLabel:  "element walk",
				Disposition: "budgeted",
			},
		}

	case "opaque_call":
		return []inventory.WorkPhase{
			{
				Families:    []string{"string"},
				Fn:          "string/replace",
				File:        fixtureFile,
				Func:        "replaceAll",
				PhaseLabel:  "replacement",
				Disposition: "none-bounded-dispatch",
			},
		}

	case "unflushed_return":
		return []inventory.WorkPhase{
			{
				Families:    []string{"collection"},
				Fn:          "count",
				File:        fixtureFile,
				Func:        "countAll",
				PhaseLabel:  "budget lifetime",
				Disposition: "budgeted",
			},
			{
				Families:    []string{"collection"},
				Fn:          "count",
				File:        fixtureFile,
				Func:        "countAll",
				PhaseLabel:  "element walk",
				Disposition: "budgeted",
			},
		}

	case "duplicate_callback_charge":
		return []inventory.WorkPhase{
			{
				Families:    []string{"higher-order"},
				Fn:          "map",
				File:        fixtureFile,
				Func:        "mapEach",
				PhaseLabel:  "callback apply",
				Disposition: "callback-owned",
			},
			{
				Families:    []string{"higher-order"},
				Fn:          "map",
				File:        fixtureFile,
				Func:        "mapEach",
				PhaseLabel:  "callback result",
				Disposition: "callback-owned",
			},
		}

	case "unclassified_result_branch":
		return []inventory.WorkPhase{
			{
				Families:    []string{"collection"},
				Fn:          "vector",
				File:        fixtureFile,
				Func:        "collectAll",
				PhaseLabel:  "element walk",
				Disposition: "budgeted",
			},
		}

	case "prepost_only":
		return []inventory.WorkPhase{
			{
				Families:    []string{"collection"},
				Fn:          "count",
				File:        fixtureFile,
				Func:        "countAll",
				PhaseLabel:  "element walk",
				Disposition: "budgeted",
			},
		}

	case "compliant":
		return []inventory.WorkPhase{
			{
				Families:    []string{"collection"},
				Fn:          "last",
				File:        fixtureFile,
				Func:        "lastOf",
				PhaseLabel:  "budget lifetime",
				Disposition: "budgeted",
			},
			{
				Families:    []string{"collection"},
				Fn:          "last",
				File:        fixtureFile,
				Func:        "lastOf",
				PhaseLabel:  "element walk",
				Disposition: "budgeted",
			},
		}
	}
	return nil
}

func fixtureResults(caseName string) []inventory.ResultBranch {
	switch caseName {
	case "missing_registration":
		return []inventory.ResultBranch{
			{
				Families:    []string{"collection"},
				Fn:          "count",
				File:        fixtureFile,
				Func:        "countAll",
				BranchLabel: "error return",
				Class:       "scalar-singleton",
			},
			{
				Families:    []string{"collection"},
				Fn:          "count",
				File:        fixtureFile,
				Func:        "countAll",
				BranchLabel: "count return",
				Class:       "scalar-singleton",
			},
		}

	case "helper_only_loop":
		return []inventory.ResultBranch{
			{
				Families:    []string{"collection"},
				Fn:          "count",
				File:        fixtureFile,
				Func:        "countAll",
				BranchLabel: "delegated return",
				Class:       "borrowed",
			},
			{
				Families:    []string{"collection"},
				Fn:          "count",
				File:        fixtureFile,
				Func:        "countElements",
				BranchLabel: "error return",
				Class:       "scalar-singleton",
			},
			{
				Families:    []string{"collection"},
				Fn:          "count",
				File:        fixtureFile,
				Func:        "countElements",
				BranchLabel: "count return",
				Class:       "scalar-singleton",
			},
		}

	case "opaque_call":
		return []inventory.ResultBranch{
			{
				Families:    []string{"string"},
				Fn:          "string/replace",
				File:        fixtureFile,
				Func:        "replaceAll",
				BranchLabel: "replaced return",
				Class:       "mixed-string",
				ChargeExpr:  "len(s)",
			},
		}

	case "unflushed_return":
		return []inventory.ResultBranch{
			{
				Families:    []string{"collection"},
				Fn:          "count",
				File:        fixtureFile,
				Func:        "countAll",
				BranchLabel: "error return",
				Class:       "scalar-singleton",
			},
			{
				Families:    []string{"collection"},
				Fn:          "count",
				File:        fixtureFile,
				Func:        "countAll",
				BranchLabel: "count return",
				Class:       "scalar-singleton",
			},
		}

	case "duplicate_callback_charge":
		return []inventory.ResultBranch{
			{
				Families:    []string{"higher-order"},
				Fn:          "map",
				File:        fixtureFile,
				Func:        "mapEach",
				BranchLabel: "error return",
				Class:       "scalar-singleton",
			},
			{
				Families:    []string{"higher-order"},
				Fn:          "map",
				File:        fixtureFile,
				Func:        "mapEach",
				BranchLabel: "mapped return",
				Class:       "fresh-container",
				ChargeExpr:  "len(out)",
			},
		}

	case "unclassified_result_branch":
		return []inventory.ResultBranch{
			{
				Families:    []string{"collection"},
				Fn:          "vector",
				File:        fixtureFile,
				Func:        "collectAll",
				BranchLabel: "error return",
				Class:       "scalar-singleton",
			},
			// fresh-container hands the caller fresh allocation, and this row
			// deliberately names no ChargeExpr for it.
			{
				Families:    []string{"collection"},
				Fn:          "vector",
				File:        fixtureFile,
				Func:        "collectAll",
				BranchLabel: "collected return",
				Class:       "fresh-container",
			},
		}

	case "prepost_only":
		return []inventory.ResultBranch{
			{
				Families:    []string{"collection"},
				Fn:          "count",
				File:        fixtureFile,
				Func:        "countAll",
				BranchLabel: "error return",
				Class:       "scalar-singleton",
			},
			{
				Families:    []string{"collection"},
				Fn:          "count",
				File:        fixtureFile,
				Func:        "countAll",
				BranchLabel: "count return",
				Class:       "scalar-singleton",
			},
		}

	case "compliant":
		return []inventory.ResultBranch{
			{
				Families:    []string{"collection"},
				Fn:          "last",
				File:        fixtureFile,
				Func:        "lastOf",
				BranchLabel: "error return",
				Class:       "scalar-singleton",
			},
			{
				Families:    []string{"collection"},
				Fn:          "last",
				File:        fixtureFile,
				Func:        "lastOf",
				BranchLabel: "element return",
				Class:       "borrowed",
			},
		}
	}
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
