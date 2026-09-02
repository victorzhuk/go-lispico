package inventory

// ResultBranches holds every recorded result branch. Each migration seam
// appends its own rows here; the file carries data only.
//
// Every row below belongs to the support family, which registers nothing under
// a Lisp name, so each leaves Fn empty.
var ResultBranches = []ResultBranch{
	// Each of these returns a *core.LispicoError on the error path, which the
	// apply site never charges: an error unwinds the call instead of becoming a
	// result the caller owns and the ledger has to size. None of the six result
	// classes describes an error value, so the rows take the one that charges
	// nothing and name no ChargeExpr.
	{
		Families:    []string{"support"},
		File:        "plugins/stdlib/errors.go",
		Func:        "arityErrorf",
		BranchLabel: "error return",
		Class:       "scalar-singleton",
	},
	{
		Families:    []string{"support"},
		File:        "plugins/stdlib/errors.go",
		Func:        "typeErrorf",
		BranchLabel: "error return",
		Class:       "scalar-singleton",
	},
	{
		Families:    []string{"support"},
		File:        "plugins/stdlib/errors.go",
		Func:        "domainErrorf",
		BranchLabel: "error return",
		Class:       "scalar-singleton",
	},
	{
		Families:    []string{"support"},
		File:        "plugins/stdlib/errors.go",
		Func:        "wrapCause",
		BranchLabel: "error return",
		Class:       "scalar-singleton",
	},
	{
		Families:    []string{"support"},
		File:        "internal/collections/errors.go",
		Func:        "typeErrorf",
		BranchLabel: "error return",
		Class:       "scalar-singleton",
	},
	{
		Families:    []string{"support"},
		File:        "internal/collections/errors.go",
		Func:        "domainErrorf",
		BranchLabel: "error return",
		Class:       "scalar-singleton",
	},
}
