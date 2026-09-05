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

// Dispositions enumerates the valid values of WorkPhase.Disposition. Each says
// how the phase's cost is accounted for: "budgeted" charges the caller's budget
// as it runs; "bounded-exception" opts out of the budget against a ceiling it
// states in MaxWork and backs with Proof; "trusted-host" runs inside host code
// the runtime cannot preempt, named in Proof; "callback-owned" leaves the cost
// to the callback the phase dispatches, one row per callback; "load-time" runs
// while the plugin loads, before any budget exists; "none-bounded-dispatch"
// does no work that grows with the input.
var Dispositions = []string{
	"budgeted",
	"bounded-exception",
	"trusted-host",
	"callback-owned",
	"load-time",
	"none-bounded-dispatch",
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
