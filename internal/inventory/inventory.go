package inventory

// WorkPhase records one bounded stretch of work inside a builtin: the phase
// label, how its cost is accounted for, and the evidence backing that claim.
type WorkPhase struct {
	Families    []string
	Fn          string
	File        string
	Func        string
	PhaseLabel  string
	Disposition string
	Proof       string
	MaxWork     int64
}

// ResultBranch records one value a builtin can return, classified by what the
// caller ends up owning: a shared singleton, a borrowed structure, or fresh
// allocation whose size the charge expression describes.
type ResultBranch struct {
	Families    []string
	Fn          string
	File        string
	Func        string
	BranchLabel string
	Class       string
	ChargeExpr  string
}

// Families enumerates the valid values of the Families field. A row naming
// anything outside this set is a typo, not a new family.
var Families = []string{
	"numeric",
	"types",
	"collection",
	"higher-order",
	"string",
	"cl-adapter",
	"support",
}

// Dispositions enumerates the valid values of WorkPhase.Disposition. Every
// other value asserts something — a ceiling, an inherited bound, or that there
// is no work that scales — and none of them can say a phase is unbounded,
// known and owned by a named change; forcing such a phase into
// bounded-exception is what produced the false ceiling "unbounded-tracked"
// replaces.
var Dispositions = []string{
	"budgeted",
	"bounded-exception",
	"trusted-host",
	"callback-owned",
	"load-time",
	"none-bounded-dispatch",
	"unbounded-tracked",
}

// ResultClasses enumerates the valid values of ResultBranch.Class.
var ResultClasses = []string{
	"scalar-singleton",
	"borrowed",
	"fresh-container",
	"fresh-deep",
	"incremental-persistent",
	"mixed-string",
}
