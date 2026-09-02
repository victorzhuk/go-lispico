package inventory

// WorkPhases holds every recorded work phase. Each migration seam appends its
// own rows here; the file carries data only, so appends never touch assertions.
//
// Every row below belongs to the support family, which registers nothing under
// a Lisp name, so each leaves Fn empty.
var WorkPhases = []WorkPhase{
	// Both loops walk a repo-owned fixed list once while the plugin loads,
	// outside any builtin dispatch: there is no budget yet to charge them
	// against, and neither list grows with program input.
	{
		Families:    []string{"support"},
		File:        "plugins/stdlib/plugin.go",
		Func:        "Init",
		PhaseLabel:  "registrar walk",
		Disposition: "load-time",
	},
	{
		Families:    []string{"support"},
		File:        "plugins/stdlib/bootstrap.go",
		Func:        "loadBootstrap",
		PhaseLabel:  "bootstrap entry walk",
		Disposition: "load-time",
	},

	// The error builders each reach fmt.Sprintf, whose cost the caller cannot
	// see. MaxWork below counts bytes of formatted message: every format
	// literal is under 128 bytes and carries at most two verbs, each rendering
	// either a registered builtin name or the name of one of the 13 concrete
	// core.Value types, so 256 covers any combination with margin.
	{
		Families:    []string{"support"},
		File:        "plugins/stdlib/errors.go",
		Func:        "arityErrorf",
		PhaseLabel:  "message format",
		Disposition: "bounded-exception",
		Proof: "Its call sites in plugins/stdlib (arithmetic.go, comparison.go, " +
			"strings.go) pass four %s verbs between them and nothing else; each " +
			"renders a registered builtin name. No argument is a core.Value, so " +
			"no container is traversed and every message renders in constant time.",
		MaxWork: 256,
	},
	{
		Families:    []string{"support"},
		File:        "plugins/stdlib/errors.go",
		Func:        "typeErrorf",
		PhaseLabel:  "message format",
		Disposition: "bounded-exception",
		Proof: "Its call sites in plugins/stdlib pass 31 %T and 5 %s verbs. %T " +
			"renders the name of one of the 13 concrete core.Value types and %s " +
			"a registered builtin name; neither reads a value's contents, so no " +
			"container is traversed and every message renders in constant time.",
		MaxWork: 256,
	},
	{
		Families:    []string{"support"},
		File:        "plugins/stdlib/errors.go",
		Func:        "domainErrorf",
		PhaseLabel:  "message format",
		Disposition: "bounded-exception",
		Proof: "The bound does not hold universally. Seven of the nine call sites " +
			"format no verb at all and render in constant time; MaxWork is the " +
			"ceiling for those seven only. The two exceptions are both assert " +
			"branches in plugins/stdlib/control.go. The first passes core.String.V " +
			"to a %s: the message is linear in a user-supplied string, and no cap " +
			"on core.String length exists in core or plugins/stdlib, so it can " +
			"exceed MaxWork. The second passes an arbitrary core.Value to a %v: " +
			"core.boundedString caps structural depth at " +
			"core.DefaultMaxStructuralDepth but never caps breadth, so a wide " +
			"List, Vector or HashMap renders element by element and the cost " +
			"scales with the container. assert belongs to the higher-order family; " +
			"that seam owns both fixes.",
		MaxWork: 256,
	},
	{
		Families:    []string{"support"},
		File:        "plugins/stdlib/errors.go",
		Func:        "wrapCause",
		PhaseLabel:  "message format",
		Disposition: "bounded-exception",
		Proof: "The format is \"%s: %v\": the %s is an operation-name literal " +
			"supplied at the call site, and the %v renders a Go error, never a " +
			"core.Value, so no container can be traversed. A cause that already " +
			"is a *core.LispicoError takes the identity return above and never " +
			"reaches fmt.Sprintf. The unhashable-key error core's HashMap " +
			"mutators return carries one %T and renders in constant time. The " +
			"bound does not hold for the two strconv call sites in " +
			"plugins/stdlib/strings.go: strconv.NumError quotes the whole parsed " +
			"argument, so the message grows with that string. MaxWork is the " +
			"ceiling for every other cause. string->int and string->float belong " +
			"to the string family; that seam owns the fix.",
		MaxWork: 256,
	},
	{
		Families:    []string{"support"},
		File:        "internal/collections/errors.go",
		Func:        "typeErrorf",
		PhaseLabel:  "message format",
		Disposition: "bounded-exception",
		Proof: "Its call sites in internal/collections (kernels.go, order.go) pass " +
			"two %T and one %s between them; %T renders the name of one of the 13 " +
			"concrete core.Value types and %s a registered builtin name, so no " +
			"container is traversed and every message renders in constant time.",
		MaxWork: 256,
	},
	{
		Families:    []string{"support"},
		File:        "internal/collections/errors.go",
		Func:        "domainErrorf",
		PhaseLabel:  "message format",
		Disposition: "bounded-exception",
		Proof: "Both call sites, in internal/collections/order.go, pass two %T " +
			"verbs and nothing else. %T renders the name of one of the 13 " +
			"concrete core.Value types without reading its contents, so no " +
			"container is traversed and every message renders in constant time.",
		MaxWork: 256,
	},

	// Numeric family. The four variadic arithmetic kernels, the min/max
	// factory and the ordering factory walk an argument list whose length is
	// the caller's to choose, so each charges one Step per argument. Every
	// other numeric builtin takes a fixed operand count and runs in constant
	// time, with no stretch of work to budget.
	{
		Families:    []string{"numeric"},
		Fn:          "+",
		File:        "plugins/stdlib/arithmetic.go",
		Func:        "addNumbers",
		PhaseLabel:  "argument budget",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"numeric"},
		Fn:          "+",
		File:        "plugins/stdlib/arithmetic.go",
		Func:        "addNumbers",
		PhaseLabel:  "argument walk",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"numeric"},
		Fn:          "-",
		File:        "plugins/stdlib/arithmetic.go",
		Func:        "subtractNumbers",
		PhaseLabel:  "argument budget",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"numeric"},
		Fn:          "-",
		File:        "plugins/stdlib/arithmetic.go",
		Func:        "subtractNumbers",
		PhaseLabel:  "argument walk",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"numeric"},
		Fn:          "*",
		File:        "plugins/stdlib/arithmetic.go",
		Func:        "multiplyNumbers",
		PhaseLabel:  "argument budget",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"numeric"},
		Fn:          "*",
		File:        "plugins/stdlib/arithmetic.go",
		Func:        "multiplyNumbers",
		PhaseLabel:  "argument walk",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"numeric"},
		Fn:          "/",
		File:        "plugins/stdlib/arithmetic.go",
		Func:        "divideNumbers",
		PhaseLabel:  "argument budget",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"numeric"},
		Fn:          "/",
		File:        "plugins/stdlib/arithmetic.go",
		Func:        "divideNumbers",
		PhaseLabel:  "argument walk",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"numeric"},
		Fn:          "mod pow quot",
		File:        "plugins/stdlib/arithmetic.go",
		Func:        "registerArithmetic",
		PhaseLabel:  "fixed-arity dispatch",
		Disposition: "none-bounded-dispatch",
	},
	{
		Families:    []string{"numeric"},
		Fn:          "neg? pos? zero?",
		File:        "plugins/stdlib/arithmetic.go",
		Func:        "registerArithmetic",
		PhaseLabel:  "sign predicate dispatch",
		Disposition: "none-bounded-dispatch",
	},
	{
		Families:    []string{"numeric"},
		Fn:          "abs ceil floor sqrt",
		File:        "plugins/stdlib/arithmetic.go",
		Func:        "unaryMathFunc",
		PhaseLabel:  "unary dispatch",
		Disposition: "none-bounded-dispatch",
	},
	{
		Families:    []string{"numeric"},
		Fn:          "max min",
		File:        "plugins/stdlib/arithmetic.go",
		Func:        "minMaxFunc",
		PhaseLabel:  "argument budget",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"numeric"},
		Fn:          "max min",
		File:        "plugins/stdlib/arithmetic.go",
		Func:        "minMaxFunc",
		PhaseLabel:  "argument walk",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"numeric"},
		Fn:          "=",
		File:        "plugins/stdlib/comparison.go",
		Func:        "registerComparison",
		PhaseLabel:  "host-equals-boundary",
		Disposition: "trusted-host",
		Proof: "= compares through core.EqualsBounded, which charges one Step " +
			"per compared node and stops at the host boundary: a Value outside " +
			"the four core container types is compared by its own Equals, work " +
			"the runtime cannot preempt and must not bill to the caller's budget.",
	},
	{
		Families:    []string{"numeric"},
		Fn:          "=",
		File:        "plugins/stdlib/comparison.go",
		Func:        "equalsAll",
		PhaseLabel:  "argument budget",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"numeric"},
		Fn:          "=",
		File:        "plugins/stdlib/comparison.go",
		Func:        "equalsAll",
		PhaseLabel:  "argument walk",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"numeric"},
		Fn:          "< <= > >=",
		File:        "plugins/stdlib/comparison.go",
		Func:        "orderingFunc",
		PhaseLabel:  "argument budget",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"numeric"},
		Fn:          "< <= > >=",
		File:        "plugins/stdlib/comparison.go",
		Func:        "orderingFunc",
		PhaseLabel:  "chain walk",
		Disposition: "budgeted",
	},

	// Types family. Every predicate and conversion reads one argument's tag
	// and returns; none walks a structure, so none holds a budget.
	{
		Families:    []string{"types"},
		Fn:          "bool? float? fn? int? keyword? list? macro? map? nil? string? symbol? type vector?",
		File:        "plugins/stdlib/types.go",
		Func:        "registerTypes",
		PhaseLabel:  "type predicate dispatch",
		Disposition: "none-bounded-dispatch",
	},
	{
		Families:    []string{"types"},
		Fn:          "float->int int->float",
		File:        "plugins/stdlib/types.go",
		Func:        "registerTypes",
		PhaseLabel:  "numeric conversion dispatch",
		Disposition: "none-bounded-dispatch",
	},
	{
		Families:    []string{"types"},
		Fn:          "keyword->str str->keyword",
		File:        "plugins/stdlib/types.go",
		Func:        "registerTypes",
		PhaseLabel:  "retyped view dispatch",
		Disposition: "none-bounded-dispatch",
	},
}
