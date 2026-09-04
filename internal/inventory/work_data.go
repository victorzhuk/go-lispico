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
			"branches in plugins/stdlib/control.go, and they no longer read " +
			"alike. control.go:29 passes core.String.V to a %.200s: fmt " +
			"truncates a %s operand at its precision without reading past it, " +
			"so the work is flat in the operand rather than linear - measured " +
			"258 B/op alike for operands of 1e3, 1e5 and 1e7 bytes - and the " +
			"render is bounded, though by its own 818-byte message ceiling and " +
			"not by MaxWork. control.go:31 passes an arbitrary core.Value to a " +
			"%.200v: that precision caps only what is emitted, so the render " +
			"behind it still walks the whole operand and stays unbounded. Both " +
			"branches belong to the higher-order family and carry their own " +
			"rows there, where the value branch is the one recorded " +
			"unbounded-tracked.",
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
			"mutators return carries one %T and renders in constant time. " +
			"MaxWork is the ceiling for every cause but one: a strconv.NumError " +
			"quotes the whole parsed argument, so its message grows with that " +
			"string. string->int and string->float are the only call sites that " +
			"raise one, and the string-family row under the phase label " +
			"\"strconv message format\" states the ceiling they run under.",
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
		Func:        "equalsAll",
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

	// Collection family, part one: the eight construction and traversal
	// builtins. Every phase whose cost grows with the input charges the
	// caller's budget one unit per element copied or constructed.

	{
		Families:    []string{"collection"},
		Fn:          "list",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "argument budget",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection"},
		Fn:          "list",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "argument copy",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection"},
		Fn:          "vector",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "argument budget",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection"},
		Fn:          "vector",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "argument copy",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection"},
		Fn:          "concat",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "flatten budget",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection"},
		Fn:          "concat",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "flatten walk",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection"},
		Fn:          "concat",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "prefix walk",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection"},
		Fn:          "concat",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "shared-tail cons walk",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection"},
		Fn:          "concat",
		File:        "plugins/stdlib/collections.go",
		Func:        "appendCollectionElems",
		PhaseLabel:  "element copy",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection"},
		Fn:          "reverse",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "reverse budget",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection"},
		Fn:          "reverse",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "subject copy",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection"},
		Fn:          "reverse",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "reverse walk",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection"},
		Fn:          "hash-map",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "pair budget",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection"},
		Fn:          "hash-map",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "pair walk",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection"},
		Fn:          "merge",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "merge budget",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection"},
		Fn:          "merge",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "source entry walk",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection"},
		Fn:          "sort",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "sort budget",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection"},
		Fn:          "sort",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "subject copy",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection"},
		Fn:          "range",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "generation budget",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection"},
		Fn:          "range",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "bounds walk",
		Disposition: "none-bounded-dispatch",
	},
	{
		Families:    []string{"collection"},
		Fn:          "range",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "generation walk",
		Disposition: "budgeted",
	},

	// The two core walks the builtins cannot preempt. Neither is bounded:
	// both descend a value as a tree while the ledger charges a shared node
	// once, and this core shares structure by design, so a node reached by
	// two references is walked once per reference.
	// Owned by core-value-walk-sharing-bound.

	{
		Families:    []string{"collection"},
		Fn:          "hash-map list merge range vector",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "result deep sizing",
		Disposition: "unbounded-tracked",
		Proof: "core.ValueDeepBytes walks the finished result once, as a tree and " +
			"not as a graph, so a node reached by two references is visited " +
			"twice while the ledger charged it once. This core shares structure " +
			"by design: core.List.Cons on a shared-tail list returns " +
			"core.ListShallowBytes(1) whatever the list's length, and " +
			"plugins/stdlib/collections.go's cons charges exactly that, so " +
			"consing a list onto itself doubles the walk for a constant charge. " +
			"Measured: consing a ten-element list onto itself 26 times costs " +
			"1040 ledger bytes while core.ValueDeepBytes reports 24159191024 " +
			"and String renders 1476395007 characters. The allocation ledger " +
			"therefore does not bound this walk and no static ceiling replaces " +
			"it. Owned by core-value-walk-sharing-bound.",
	},
	{
		Families:    []string{"collection"},
		Fn:          "concat hash-map list merge range vector",
		File:        "plugins/stdlib/collections.go",
		Func:        "chargeCollectionResult",
		PhaseLabel:  "construction depth walk",
		Disposition: "unbounded-tracked",
		Proof: "core.CheckConstructionDepthWith caps the depth of the descent, " +
			"not the number of nodes it visits. constructionDepthExceeded " +
			"(core/depth.go:69) iterates every element at every level and " +
			"recurses into each collection-typed one, and returns early only " +
			"when the limit actually trips; a wide, shallow structure never " +
			"trips it and is walked whole, once per reference, while the " +
			"ledger charged each shared node once. Measured: a ten-element " +
			"list consed onto itself 26 times sits at nesting depth 27, far " +
			"under core.DefaultMaxStructuralDepth, and takes 1.677s for 1040 " +
			"ledger bytes, doubling per cons (3.3us, 132us, 6.8ms, 563ms, " +
			"1.68s). Owned by core-value-walk-sharing-bound.",
	},

	// Collection family, part two: the lookup, indexed-access and persistent
	// update builtins. Every traversal whose cost grows with the subject
	// charges the caller's budget one unit per node walked, element copied,
	// map entry read or key-value pair applied; the rest is fixed dispatch
	// over one argument, or an exception carrying its own bound.

	{
		Families:    []string{"collection"},
		Fn:          "first",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "indexed head dispatch",
		Disposition: "none-bounded-dispatch",
	},
	{
		Families:    []string{"collection"},
		Fn:          "rest",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "vector copy budget",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection"},
		Fn:          "rest",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "vector copy walk",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection"},
		Fn:          "last",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "traversal budget",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection"},
		Fn:          "last",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "shared-list traversal walk",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection"},
		Fn:          "nth",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "indexed access dispatch",
		Disposition: "none-bounded-dispatch",
	},
	{
		Families:    []string{"collection"},
		Fn:          "count",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "string rune scan",
		Disposition: "bounded-exception",
		Proof: "[]rune(c.V) converts the whole subject in one Go conversion, " +
			"with no point inside it where a Step could run. The bound " +
			"comes from the subject rather than from the conversion: a " +
			"core.String reaches this builtin only by already existing as a " +
			"charged value, and core.StringShallowBytes bills " +
			"core.MeterStringHeaderBytes (16) plus one byte per byte of V, " +
			"so a String that fits under core.DefaultMaxAllocationBytes " +
			"(67108864) carries at most 67108848 bytes. The conversion is " +
			"O(len(V)) and reads each of those bytes at most once, which is " +
			"where MaxWork comes from. A longer String cannot exist: " +
			"building it would have failed the allocation ledger first.",
		MaxWork: 67_108_848,
	},
	{
		Families:    []string{"collection"},
		Fn:          "cons",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "vector copy budget",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection"},
		Fn:          "cons",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "vector copy walk",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection"},
		Fn:          "conj",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "vector append budget",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection"},
		Fn:          "conj",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "vector append walk",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection"},
		Fn:          "conj",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "list cons walk",
		Disposition: "none-bounded-dispatch",
	},
	{
		Families:    []string{"collection"},
		Fn:          "conj",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "vector backing copy",
		Disposition: "bounded-exception",
		Proof: "core.Vector.Conj copies the source's flat backing before it " +
			"appends: below core's vectorFlatThreshold it rebuilds the " +
			"whole flat slice, and above it a still-flat source is promoted " +
			"once through buildVecTrieFromFlat. Neither copy can be " +
			"preempted from stdlib. Its element count is bounded by the " +
			"allocation ledger the source vector was charged against: every " +
			"element cost at least core.MeterScalarBytes (16) when the " +
			"vector was built, so a vector that fits under " +
			"core.DefaultMaxAllocationBytes carries at most " +
			"core.DefaultMaxAllocationBytes/16 elements. Every later Conj " +
			"on the promoted result shares the trie and copies only the " +
			"tail buffer, at most vecBranch elements.",
		MaxWork: 4_194_304,
	},
	{
		Families:    []string{"collection"},
		Fn:          "empty?",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "emptiness dispatch",
		Disposition: "none-bounded-dispatch",
	},
	{
		Families:    []string{"collection"},
		Fn:          "get",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "map lookup dispatch",
		Disposition: "none-bounded-dispatch",
	},
	{
		Families:    []string{"collection"},
		Fn:          "assoc",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "keyval budget",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection"},
		Fn:          "assoc",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "keyval walk",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection"},
		Fn:          "assoc",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "inserted element walk",
		Disposition: "none-bounded-dispatch",
	},
	{
		Families:    []string{"collection"},
		Fn:          "keys",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "entry budget",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection"},
		Fn:          "keys",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "entry walk",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection"},
		Fn:          "vals",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "entry budget",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection"},
		Fn:          "vals",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "entry walk",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection"},
		Fn:          "contains?",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "key hashing",
		Disposition: "bounded-exception",
		Proof: "core.HashMap.Get hashes the key and then either scans the " +
			"sorted-slice form, capped at core's hashMapSmallLimit of 8 " +
			"entries, or descends the HAMT at vecBits per level - at most 7 " +
			"levels for a 32-bit hash - before a collision node scans only " +
			"the keys sharing a full hash. Just the hashing grows with " +
			"input, and only with the key's own bytes: core.toHashKey " +
			"rejects every container type, so a key is one of the seven " +
			"scalar forms and core.hashOfKey reads at most len(hk.str) " +
			"bytes. That length is bounded by the allocation ledger the key " +
			"was charged against, one byte per byte of V, so a key that " +
			"fits under core.DefaultMaxAllocationBytes carries at most " +
			"67108848 bytes.",
		MaxWork: 67_108_848,
	},
	{
		Families:    []string{"collection"},
		Fn:          "dissoc",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "key budget",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection"},
		Fn:          "dissoc",
		File:        "plugins/stdlib/collections.go",
		Func:        "collectionBuiltins",
		PhaseLabel:  "key walk",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection"},
		Fn:          "get-in",
		File:        "plugins/stdlib/collections.go",
		Func:        "getInLookup",
		PhaseLabel:  "path budget",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection"},
		Fn:          "get-in",
		File:        "plugins/stdlib/collections.go",
		Func:        "getInLookup",
		PhaseLabel:  "path walk",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection"},
		Fn:          "get get-in",
		File:        "plugins/stdlib/collections.go",
		Func:        "lookupArityError",
		PhaseLabel:  "message format",
		Disposition: "bounded-exception",
		Proof: "Its two call sites, get and get-in in " +
			"plugins/stdlib/collections.go, pass one %s and one %d. The %s " +
			"renders a registered builtin name and the %d an argument " +
			"count; neither reads a core.Value's contents, so no container " +
			"is traversed and every message renders in constant time.",
		MaxWork: 256,
	},
	{
		Families:    []string{"collection"},
		Fn:          "get get-in",
		File:        "plugins/stdlib/collections.go",
		Func:        "lookupTypeError",
		PhaseLabel:  "message format",
		Disposition: "bounded-exception",
		Proof: "Its three call sites, get and get-in in " +
			"plugins/stdlib/collections.go, pass two %s and one %T. The %s " +
			"verbs render a registered builtin name and an expected-kind " +
			"literal, and %T renders the name of one of the 13 concrete " +
			"core.Value types without reading its contents, so no container " +
			"is traversed and every message renders in constant time.",
		MaxWork: 256,
	},
	{
		Families:    []string{"collection"},
		Fn:          "assoc conj cons dissoc",
		File:        "plugins/stdlib/collections.go",
		Func:        "chargeConsResult",
		PhaseLabel:  "nested element depth walk",
		Disposition: "unbounded-tracked",
		Proof: "The loop runs once per newly introduced element - the " +
			"arguments the caller wrote, already billed one Step each by " +
			"the builtin that called in - so the walk never revisits the " +
			"accumulated result. Each walk is itself unbounded: " +
			"core.CheckNestedElementDepthWith is checkDepthAt(v, 1, eval) " +
			"over constructionDepthExceeded (core/depth.go:38), the same " +
			"function the construction depth walk runs and differing only " +
			"in starting depth, which does not change the node count. Its " +
			"limit caps the depth of the descent, not the number of nodes " +
			"visited, and a newly introduced element may itself share " +
			"substructure, in which case a node reached by two references " +
			"is visited twice while the ledger charged it once. Measured: " +
			"a ten-element list consed onto itself 26 times sits at nesting " +
			"depth 27, far under core.DefaultMaxStructuralDepth, and takes " +
			"1.677s for 1040 ledger bytes. The fmt.Sprintf on the " +
			"length-limit path renders one %s and two %d and costs constant " +
			"time. Owned by core-value-walk-sharing-bound.",
	},

	// The registrar walks the table above once while the plugin loads,
	// outside any builtin dispatch: there is no budget yet to charge it
	// against, and the table does not grow with program input.
	{
		Families:    []string{"collection"},
		Fn:          "assoc concat conj cons contains? count dissoc empty? first get get-in hash-map keys last list merge nth range rest reverse sort vals vector",
		File:        "plugins/stdlib/collections.go",
		Func:        "registerCollections",
		PhaseLabel:  "registrar walk",
		Disposition: "load-time",
	},

	// Higher-order family. filter, reduce and apply each open a budget and
	// walk their subject twice: seqInput copies the whole sequence before the
	// loop is entered, so that copy is billed alongside the per-element work.
	// map opens none - collections.MapSequences owns both the walk and the
	// charge for what it builds.
	{
		Families:    []string{"higher-order"},
		Fn:          "map",
		File:        "plugins/stdlib/higher_order.go",
		Func:        "registerHigherOrder",
		PhaseLabel:  "kernel delegation",
		Disposition: "none-bounded-dispatch",
	},
	{
		Families:    []string{"higher-order"},
		Fn:          "filter",
		File:        "plugins/stdlib/higher_order.go",
		Func:        "registerHigherOrder",
		PhaseLabel:  "subject budget",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"higher-order"},
		Fn:          "filter",
		File:        "plugins/stdlib/higher_order.go",
		Func:        "registerHigherOrder",
		PhaseLabel:  "subject copy walk",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"higher-order"},
		Fn:          "filter",
		File:        "plugins/stdlib/higher_order.go",
		Func:        "registerHigherOrder",
		PhaseLabel:  "predicate walk",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"higher-order"},
		Fn:          "reduce",
		File:        "plugins/stdlib/higher_order.go",
		Func:        "registerHigherOrder",
		PhaseLabel:  "subject budget",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"higher-order"},
		Fn:          "reduce",
		File:        "plugins/stdlib/higher_order.go",
		Func:        "registerHigherOrder",
		PhaseLabel:  "subject copy walk",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"higher-order"},
		Fn:          "reduce",
		File:        "plugins/stdlib/higher_order.go",
		Func:        "registerHigherOrder",
		PhaseLabel:  "fold walk",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"higher-order"},
		Fn:          "apply",
		File:        "plugins/stdlib/higher_order.go",
		Func:        "registerHigherOrder",
		PhaseLabel:  "argument budget",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"higher-order"},
		Fn:          "apply",
		File:        "plugins/stdlib/higher_order.go",
		Func:        "registerHigherOrder",
		PhaseLabel:  "tail copy walk",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"higher-order"},
		Fn:          "apply",
		File:        "plugins/stdlib/higher_order.go",
		Func:        "registerHigherOrder",
		PhaseLabel:  "argument assembly walk",
		Disposition: "budgeted",
	},

	// assert's two failure messages read alike and are not. The %.200s branch
	// truncates its operand before anything reads it; the %.200v branch caps
	// only what is emitted, and the render behind that cap walks a value that
	// can share substructure.
	{
		Families:    []string{"higher-order"},
		Fn:          "assert",
		File:        "plugins/stdlib/control.go",
		Func:        "registerControl",
		PhaseLabel:  "string failure message format",
		Disposition: "bounded-exception",
		Proof: "The operand of \"assertion failed: %.200s\" is a core.String's " +
			"own bytes, and fmt truncates a %s operand at its precision " +
			"without reading past it, so the work is flat in the operand " +
			"rather than linear: measured 258 B/op alike for operands of " +
			"1e3, 1e5 and 1e7 bytes. MaxWork is the message ceiling, the " +
			"18-byte literal plus 200 runes at utf8.UTFMax.",
		MaxWork: 818,
	},
	{
		Families:    []string{"higher-order"},
		Fn:          "assert",
		File:        "plugins/stdlib/control.go",
		Func:        "registerControl",
		PhaseLabel:  "value failure message format",
		Disposition: "unbounded-tracked",
		Proof: "The precision in \"assertion failed: %.200v\" caps the emitted " +
			"message and nothing else: the render behind it walks the whole " +
			"operand before fmt truncates the result. That operand is an " +
			"arbitrary core.Value, so the walk descends a tree whose nodes " +
			"may be reached by more than one reference while the ledger " +
			"charged each of them once, and no ceiling can be stated for it. " +
			"Read the precision as a bound on output, never as one on work. " +
			"Owned by core-value-walk-sharing-bound.",
	},

	// CL adapter family. Each adapter renders its own error messages through
	// fmt.Sprintf, so each states the ceiling that render runs under.
	{
		Families:    []string{"cl-adapter"},
		Fn:          "cl/nth@1",
		File:        "cl/cl.go",
		Func:        "clNth",
		PhaseLabel:  "arity message format",
		Disposition: "bounded-exception",
		Proof: "The literal carries one %d over len(args), a Go int, and no " +
			"other verb; no operand is a core.Value, so nothing is " +
			"traversed and the message renders in constant time. MaxWork is " +
			"the message ceiling, the 44-byte literal plus the 20 bytes an " +
			"int renders to at most.",
		MaxWork: 64,
	},
	{
		Families:    []string{"cl-adapter"},
		Fn:          "cl/nth@1",
		File:        "cl/cl.go",
		Func:        "clNth",
		PhaseLabel:  "index message format",
		Disposition: "bounded-exception",
		Proof: "The literal carries one %d over core.Int's V, an int64, and " +
			"no other verb; no operand is a core.Value, so nothing is " +
			"traversed and the message renders in constant time. MaxWork is " +
			"the message ceiling, the 37-byte literal plus the 20 bytes an " +
			"int64 renders to at most.",
		MaxWork: 57,
	},
	{
		Families:    []string{"cl-adapter"},
		Fn:          "cl/mapcar@1",
		File:        "cl/cl.go",
		Func:        "clMapcar",
		PhaseLabel:  "sequence type check walk",
		Disposition: "bounded-exception",
		Proof: "The walk reads one type tag per sequence operand and never " +
			"looks inside one, so it holds no Step of its own. Its length " +
			"is the operand count, which the allocation ledger bounds: the " +
			"operands arrive as slots of a charged sequence - the call form " +
			"the reader billed, or the list apply spreads, billed at " +
			"core.MeterValueSlotBytes (16) a slot - so a call that fits " +
			"under core.DefaultMaxAllocationBytes carries at most " +
			"core.DefaultMaxAllocationBytes/16 operands, which is MaxWork. " +
			"The same shape in the collection family charges a Step per " +
			"argument; this one does not yet.",
		MaxWork: 4_194_304,
	},
	{
		Families:    []string{"cl-adapter"},
		Fn:          "cl/mapcar@1",
		File:        "cl/cl.go",
		Func:        "clMapcar",
		PhaseLabel:  "arity message format",
		Disposition: "bounded-exception",
		Proof: "The literal carries one %d over len(args), a Go int, and no " +
			"other verb; no operand is a core.Value, so nothing is " +
			"traversed and the message renders in constant time. MaxWork is " +
			"the message ceiling, the 59-byte literal plus the 20 bytes an " +
			"int renders to at most.",
		MaxWork: 79,
	},
	{
		Families:    []string{"cl-adapter"},
		Fn:          "cl/sort@1",
		File:        "cl/cl.go",
		Func:        "clSort",
		PhaseLabel:  "subject budget",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"cl-adapter"},
		Fn:          "cl/sort@1",
		File:        "cl/cl.go",
		Func:        "clSort",
		PhaseLabel:  "keyword pair walk",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"cl-adapter"},
		Fn:          "cl/sort@1",
		File:        "cl/cl.go",
		Func:        "clSort",
		PhaseLabel:  "subject copy walk",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"cl-adapter"},
		Fn:          "cl/sort@1",
		File:        "cl/cl.go",
		Func:        "clSort",
		PhaseLabel:  "arity message format",
		Disposition: "bounded-exception",
		Proof: "Both arity sites share one literal carrying a single %d over " +
			"len(args), a Go int; no operand is a core.Value, so nothing is " +
			"traversed and either message renders in constant time. MaxWork " +
			"is the message ceiling, the 94-byte literal plus the 20 bytes " +
			"an int renders to at most.",
		MaxWork: 114,
	},
	{
		Families:    []string{"cl-adapter"},
		Fn:          "cl/sort@1",
		File:        "cl/cl.go",
		Func:        "clSort",
		PhaseLabel:  "unknown keyword message format",
		Disposition: "bounded-exception",
		Proof: "The precision in \"sort: unknown keyword %.200v\" caps the " +
			"emitted message at 819 bytes: core.Keyword's String " +
			"(core/types.go:161) returns \":\" + V, so the first of the 200 " +
			"runes the precision keeps is a 1-byte colon rather than a rune " +
			"at utf8.UTFMax, and the ceiling is the 22-byte literal plus " +
			"that colon plus the remaining 199 runes. It caps nothing else: " +
			"that String builds \":\" + V in full before fmt truncates the " +
			"result, so the render is linear in V. Measured 1282, 106849 " +
			"and 10003359 B/op for V of 1e3, 1e5 and 1e7 bytes. It is " +
			"bounded all the same, because a Keyword is a scalar that " +
			"cannot share substructure: the ledger charges it " +
			"core.StringShallowBytes, core.MeterStringHeaderBytes (16) plus " +
			"one byte per byte of V, so a Keyword that fits under " +
			"core.DefaultMaxAllocationBytes carries at most 67108848 bytes " +
			"of V, exactly as the ledger bounds count's rune scan. MaxWork " +
			"is that ledger bound on the render, a different quantity from " +
			"the 819-byte ceiling on what is emitted. A longer Keyword " +
			"cannot exist: building it would have failed the ledger first.",
		MaxWork: 67_108_848,
	},

	// Shared collection kernels. Each holds the budget for the walk it owns,
	// so the adapters above bill only what they do before entering one.
	{
		Families:    []string{"collection", "cl-adapter"},
		Fn:          "nth cl/nth@1",
		File:        "internal/collections/kernels.go",
		Func:        "IndexedAccess",
		PhaseLabel:  "list cursor budget",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection", "cl-adapter"},
		Fn:          "nth cl/nth@1",
		File:        "internal/collections/kernels.go",
		Func:        "IndexedAccess",
		PhaseLabel:  "list cursor walk",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection", "cl-adapter"},
		Fn:          "nth cl/nth@1",
		File:        "internal/collections/kernels.go",
		Func:        "IndexedAccess",
		PhaseLabel:  "vector index budget",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"higher-order", "cl-adapter"},
		Fn:          "map cl/mapcar@1",
		File:        "internal/collections/kernels.go",
		Func:        "MapSequences",
		PhaseLabel:  "element budget",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"higher-order", "cl-adapter"},
		Fn:          "map cl/mapcar@1",
		File:        "internal/collections/kernels.go",
		Func:        "MapSequences",
		PhaseLabel:  "cursor setup walk",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"higher-order", "cl-adapter"},
		Fn:          "map cl/mapcar@1",
		File:        "internal/collections/kernels.go",
		Func:        "MapSequences",
		PhaseLabel:  "argument assembly walk",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"higher-order", "cl-adapter"},
		Fn:          "map cl/mapcar@1",
		File:        "internal/collections/kernels.go",
		Func:        "MapSequences",
		PhaseLabel:  "callback dispatch walk",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection", "cl-adapter"},
		Fn:          "sort cl/sort@1",
		File:        "internal/collections/kernels.go",
		Func:        "StableSort",
		PhaseLabel:  "pair budget",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection", "cl-adapter"},
		Fn:          "sort cl/sort@1",
		File:        "internal/collections/kernels.go",
		Func:        "StableSort",
		PhaseLabel:  "pair build walk",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection", "cl-adapter"},
		Fn:          "sort cl/sort@1",
		File:        "internal/collections/kernels.go",
		Func:        "StableSort",
		PhaseLabel:  "key projection walk",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection", "cl-adapter"},
		Fn:          "sort cl/sort@1",
		File:        "internal/collections/kernels.go",
		Func:        "StableSort",
		PhaseLabel:  "comparison scheduling",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"collection", "cl-adapter"},
		Fn:          "sort cl/sort@1",
		File:        "internal/collections/kernels.go",
		Func:        "StableSort",
		PhaseLabel:  "output copy walk",
		Disposition: "budgeted",
	},

	// String family. The four builtins whose cost grows with an argument list or
	// a collection open a budget and charge a Step per part. Everything else
	// hands its subject to a Go string primitive with no point inside it where a
	// Step could run: stepping per byte would put a reduction charge on every
	// string operation, so each records the ceiling the allocation ledger puts on
	// that subject instead. It is the same ceiling count's rune scan carries.
	// core.StringShallowBytes bills core.MeterStringHeaderBytes (16) plus one
	// byte per byte, so a core.String that fits under
	// core.DefaultMaxAllocationBytes (67108864) carries at most 67108848 bytes,
	// and a primitive reading one can scan no more than those.
	{
		Families:    []string{"string"},
		Fn:          "str",
		File:        "plugins/stdlib/strings.go",
		Func:        "registerStrings",
		PhaseLabel:  "argument budget",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"string"},
		Fn:          "str",
		File:        "plugins/stdlib/strings.go",
		Func:        "registerStrings",
		PhaseLabel:  "argument render walk",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"string"},
		Fn:          "string/join",
		File:        "plugins/stdlib/strings.go",
		Func:        "registerStrings",
		PhaseLabel:  "element budget",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"string"},
		Fn:          "string/join",
		File:        "plugins/stdlib/strings.go",
		Func:        "registerStrings",
		PhaseLabel:  "element render walk",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"string"},
		Fn:          "string/split",
		File:        "plugins/stdlib/strings.go",
		Func:        "registerStrings",
		PhaseLabel:  "part budget",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"string"},
		Fn:          "string/split",
		File:        "plugins/stdlib/strings.go",
		Func:        "registerStrings",
		PhaseLabel:  "part wrap walk",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"string"},
		Fn:          "string/lines",
		File:        "plugins/stdlib/strings.go",
		Func:        "registerStrings",
		PhaseLabel:  "line budget",
		Disposition: "budgeted",
	},
	{
		Families:    []string{"string"},
		Fn:          "string/lines",
		File:        "plugins/stdlib/strings.go",
		Func:        "registerStrings",
		PhaseLabel:  "line wrap walk",
		Disposition: "budgeted",
	},

	// format's own two phases. The argument loop is bounded by the operand
	// count; the render behind it is not, and carries its own row on toAny.
	{
		Families:    []string{"string"},
		Fn:          "format",
		File:        "plugins/stdlib/strings.go",
		Func:        "registerStrings",
		PhaseLabel:  "argument conversion walk",
		Disposition: "bounded-exception",
		Proof: "The loop reads one argument per iteration and holds no Step of " +
			"its own. Its length is the operand count, which the allocation " +
			"ledger bounds: the operands arrive as slots of a charged sequence " +
			"- the call form the reader billed, or the list apply spreads, " +
			"billed at core.MeterValueSlotBytes (16) a slot - so a call that " +
			"fits under core.DefaultMaxAllocationBytes carries at most " +
			"core.DefaultMaxAllocationBytes/16 operands, which is MaxWork. What " +
			"each iteration renders is toAny's work, not this loop's, and the " +
			"row on toAny states what bounds it.",
		MaxWork: 4_194_304,
	},
	{
		Families:    []string{"string"},
		Fn:          "format",
		File:        "plugins/stdlib/strings.go",
		Func:        "registerStrings",
		PhaseLabel:  "render assembly",
		Disposition: "unbounded-tracked",
		Proof: "fmt.Sprintf copies the already-materialized operands into one " +
			"output buffer, so its cost is the sum of what the loop above " +
			"produced. Every non-scalar operand in that sum came out of toAny's " +
			"v.String render, which descends a value as a tree while the ledger " +
			"charged each shared node once, so the sum inherits that " +
			"unboundedness whole. The pre-charge in front of Sprintf does not " +
			"bound it either: estimateFormatAllocBytes counts one byte per byte " +
			"while %q renders a byte that is not valid UTF-8 as four, measured " +
			"3.9997 on a 1 MiB escaped leaf, which is why the builtin charges " +
			"the shortfall afterwards rather than reading the estimate as a " +
			"ceiling. Owned by core-value-walk-sharing-bound. A third cause " +
			"sits outside that owner: when a verb and the operand's type " +
			"disagree, fmt renders the whole operand inside a mismatch " +
			"diagnostic such as %!d(string=...) while " +
			"estimateFormatValueBytes returns a constant that does not " +
			"depend on the operand, and an explicit argument index aims " +
			"every directive at that same operand - no sharing and no %q " +
			"escape in play. Owned by format-mismatched-verb-bound.",
	},

	// The Go string primitives. Each reads core.String operands the ledger has
	// already sized, so each states that ledger bound as its ceiling.
	{
		Families:    []string{"string"},
		Fn:          "string/split",
		File:        "plugins/stdlib/strings.go",
		Func:        "registerStrings",
		PhaseLabel:  "separator scan",
		Disposition: "bounded-exception",
		Proof: "strings.Split walks the subject once looking for the separator " +
			"and offers no point inside it where a Step could run. Both " +
			"operands are core.String values that reached the builtin only by " +
			"already sitting in the allocation ledger, so MaxWork is that " +
			"ledger bound. The parts it hands back alias the subject's backing " +
			"array, so the walk copies no contents; the per-part loop that " +
			"wraps them is budgeted above.",
		MaxWork: 67_108_848,
	},
	{
		Families:    []string{"string"},
		Fn:          "string/lines",
		File:        "plugins/stdlib/strings.go",
		Func:        "registerStrings",
		PhaseLabel:  "newline scan",
		Disposition: "bounded-exception",
		Proof: "strings.Split over a \"\\n\" separator walks the subject once " +
			"with no point inside it where a Step could run. The subject is a " +
			"core.String the allocation ledger has already sized, so MaxWork is " +
			"that ledger bound, and the lines alias its backing array rather " +
			"than copying it.",
		MaxWork: 67_108_848,
	},
	{
		Families:    []string{"string"},
		Fn:          "string/join",
		File:        "plugins/stdlib/strings.go",
		Func:        "joinPrecharged",
		PhaseLabel:  "part concatenation",
		Disposition: "bounded-exception",
		Proof: "strings.Join copies the parts and the separator into one buffer " +
			"with no point inside it where a Step could run, and that buffer is " +
			"a product rather than a maximum over the operands: the separator " +
			"lands between every part, so 2048 empty parts and a 65536-byte " +
			"separator write 134152192 bytes out of 98 KB of ledger. What " +
			"bounds it is the charge ahead of it. joinPrecharged sizes the " +
			"output as the parts summed by the caller's budgeted pass plus one " +
			"separator between each pair, saturating rather than wrapping, and " +
			"charges that whole size before strings.Join runs, so the copy " +
			"happens only for an output the allocation ledger accepted and " +
			"MaxWork is that ledger bound. It does not cover a part toString " +
			"rendered out of a container: that render is the unbounded walk " +
			"recorded on toString, and its row is where the defect is tracked.",
		MaxWork: 67_108_848,
	},
	{
		Families:    []string{"string"},
		Fn:          "string/replace",
		File:        "plugins/stdlib/strings.go",
		Func:        "replacePrecharged",
		PhaseLabel:  "subject scan",
		Disposition: "bounded-exception",
		Proof: "strings.ReplaceAll walks the subject once and writes the " +
			"replacement into a new buffer, with no point inside it where a " +
			"Step could run, and that buffer is a product rather than a maximum " +
			"over the operands: an empty old inserts the replacement before " +
			"every rune and once more at the end, so an 8192-byte subject and a " +
			"16384-byte replacement write 134242304 bytes out of 24 KB of " +
			"ledger. What bounds it is the charge ahead of it. " +
			"replaceOutputBytes computes the exact output length from the " +
			"operands - the subject's length plus one occurrence's growth per " +
			"strings.Count occurrence, saturating rather than wrapping - and " +
			"replacePrecharged charges it before strings.ReplaceAll runs, so " +
			"the walk happens only for an output the allocation ledger " +
			"accepted and MaxWork is that ledger bound.",
		MaxWork: 67_108_848,
	},
	{
		Families:    []string{"string"},
		Fn:          "string/contains?",
		File:        "plugins/stdlib/strings.go",
		Func:        "registerStrings",
		PhaseLabel:  "substring scan",
		Disposition: "bounded-exception",
		Proof: "strings.Contains scans the subject for the needle in one Go " +
			"call with no point inside it where a Step could run. Both operands " +
			"are core.String values the allocation ledger has already sized, so " +
			"MaxWork is that ledger bound.",
		MaxWork: 67_108_848,
	},
	{
		Families:    []string{"string"},
		Fn:          "string/starts-with?",
		File:        "plugins/stdlib/strings.go",
		Func:        "registerStrings",
		PhaseLabel:  "prefix scan",
		Disposition: "bounded-exception",
		Proof: "strings.HasPrefix compares at most the prefix's bytes in one Go " +
			"call with no point inside it where a Step could run. Both operands " +
			"are core.String values the allocation ledger has already sized, so " +
			"MaxWork is that ledger bound.",
		MaxWork: 67_108_848,
	},
	{
		Families:    []string{"string"},
		Fn:          "string/ends-with?",
		File:        "plugins/stdlib/strings.go",
		Func:        "registerStrings",
		PhaseLabel:  "suffix scan",
		Disposition: "bounded-exception",
		Proof: "strings.HasSuffix compares at most the suffix's bytes in one Go " +
			"call with no point inside it where a Step could run. Both operands " +
			"are core.String values the allocation ledger has already sized, so " +
			"MaxWork is that ledger bound.",
		MaxWork: 67_108_848,
	},
	{
		Families:    []string{"string"},
		Fn:          "string/length",
		File:        "plugins/stdlib/strings.go",
		Func:        "registerStrings",
		PhaseLabel:  "rune conversion",
		Disposition: "bounded-exception",
		Proof: "[]rune(s.V) converts the whole subject in one Go conversion, " +
			"with no point inside it where a Step could run, exactly as count's " +
			"subject scan does. The bound comes from the subject: a core.String " +
			"reaches the builtin only by already sitting in the allocation " +
			"ledger, so MaxWork is that ledger bound.",
		MaxWork: 67_108_848,
	},
	{
		Families:    []string{"string"},
		Fn:          "string->int",
		File:        "plugins/stdlib/strings.go",
		Func:        "registerStrings",
		PhaseLabel:  "integer parse scan",
		Disposition: "bounded-exception",
		Proof: "strconv.ParseInt reads the subject's digits in one Go call with " +
			"no point inside it where a Step could run. The subject is a " +
			"core.String the allocation ledger has already sized, so MaxWork is " +
			"that ledger bound. What the failure branch then renders is a " +
			"separate phase, recorded on wrapCause in plugins/stdlib/errors.go.",
		MaxWork: 67_108_848,
	},
	{
		Families:    []string{"string"},
		Fn:          "string->float",
		File:        "plugins/stdlib/strings.go",
		Func:        "registerStrings",
		PhaseLabel:  "float parse scan",
		Disposition: "bounded-exception",
		Proof: "strconv.ParseFloat reads the subject's mantissa and exponent in " +
			"one Go call with no point inside it where a Step could run. The " +
			"subject is a core.String the allocation ledger has already sized, " +
			"so MaxWork is that ledger bound. What the failure branch then " +
			"renders is a separate phase, recorded on wrapCause in " +
			"plugins/stdlib/errors.go.",
		MaxWork: 67_108_848,
	},
	{
		Families:    []string{"string"},
		Fn:          "string/trim",
		File:        "plugins/stdlib/strings.go",
		Func:        "unaryStringFunc",
		PhaseLabel:  "whitespace scan",
		Disposition: "bounded-exception",
		Proof: "strings.TrimSpace scans the subject from both ends in one Go " +
			"call with no point inside it where a Step could run. The subject " +
			"is a core.String the allocation ledger has already sized, so " +
			"MaxWork is that ledger bound.",
		MaxWork: 67_108_848,
	},
	{
		Families:    []string{"string"},
		Fn:          "string/upper",
		File:        "plugins/stdlib/strings.go",
		Func:        "unaryStringFunc",
		PhaseLabel:  "case scan",
		Disposition: "bounded-exception",
		Proof: "strings.ToUpper walks the subject rune by rune in one Go call " +
			"with no point inside it where a Step could run. The subject is a " +
			"core.String the allocation ledger has already sized, so MaxWork is " +
			"that ledger bound.",
		MaxWork: 67_108_848,
	},
	{
		Families:    []string{"string"},
		Fn:          "string/lower",
		File:        "plugins/stdlib/strings.go",
		Func:        "unaryStringFunc",
		PhaseLabel:  "case scan",
		Disposition: "bounded-exception",
		Proof: "strings.ToLower walks the subject rune by rune in one Go call " +
			"with no point inside it where a Step could run. The subject is a " +
			"core.String the allocation ledger has already sized, so MaxWork is " +
			"that ledger bound.",
		MaxWork: 67_108_848,
	},

	// The formatting boundary. A host value's own render is the host's code and
	// is trusted; a core container's is a tree walk over a graph, and no ceiling
	// covers it.
	{
		Families:    []string{"string"},
		Fn:          "str string/join",
		File:        "plugins/stdlib/strings.go",
		Func:        "toString",
		PhaseLabel:  "host value render",
		Disposition: "trusted-host",
		Proof: "A value an embedding host supplies is none of the 13 concrete " +
			"kernel types, so toString reaches it only through its own .String " +
			"method and uses that output verbatim. Bounding it is the host's " +
			"job, not stdlib's: the phase inherits whatever bound the host's " +
			"implementation already holds.",
	},
	{
		Families:    []string{"string"},
		Fn:          "str string/join",
		File:        "plugins/stdlib/strings.go",
		Func:        "toString",
		PhaseLabel:  "container render walk",
		Disposition: "unbounded-tracked",
		Proof: "A core container reaches core's boundedString, which descends " +
			"the value as a tree and not as a graph, so a node reached by two " +
			"references renders twice while the ledger charged it once. This " +
			"core shares structure by design: core.List.Cons on a shared-tail " +
			"list returns core.ListShallowBytes(1) whatever the list's length, " +
			"so consing a list onto itself doubles the render for a constant " +
			"charge. Measured: a ten-element list consed onto itself 26 times " +
			"costs 1040 ledger bytes and renders 1476395007 characters. str and " +
			"string/join reach the walk with no pre-charge in front of it, and " +
			"the allocation ledger does not bound it, so no static ceiling " +
			"replaces one. Owned by core-value-walk-sharing-bound.",
	},
	{
		Families:    []string{"string"},
		Fn:          "format",
		File:        "plugins/stdlib/strings.go",
		Func:        "toAny",
		PhaseLabel:  "host value render",
		Disposition: "trusted-host",
		Proof: "A value an embedding host supplies falls to the default arm, " +
			"which hands fmt the host's own .String output verbatim. Bounding " +
			"it is the host's job, not stdlib's: the phase inherits whatever " +
			"bound the host's implementation already holds.",
	},
	{
		Families:    []string{"string"},
		Fn:          "format",
		File:        "plugins/stdlib/strings.go",
		Func:        "toAny",
		PhaseLabel:  "non-scalar render walk",
		Disposition: "unbounded-tracked",
		Proof: "The default arm calls v.String on every non-scalar format " +
			"argument, in a loop that runs to completion before fmt.Sprintf, so " +
			"the render is materialized eagerly rather than lazily inside the " +
			"verb. What is unbounded there is the walk, not the escaping: the " +
			"render visits a shared node once per path that reaches it while " +
			"the ledger charged it once, exactly as toString's walk does. The " +
			"%q expansion is a separate defect of the same site and is not a " +
			"second unboundedness - core.String's own String is " +
			"fmt.Sprintf(\"%q\", V), which turns one source byte into at most " +
			"four, a bounded multiple of a ledger-bounded quantity, measured " +
			"3.9997 on a 1 MiB 0x80 leaf, 3.7409 at 1 KiB and 1.9999 on 1 MiB " +
			"of quotes. What that factor breaks is the charge, which counts one " +
			"byte per byte, which is why format bills the shortfall after the " +
			"render instead of treating the pre-charge as a bound. " +
			"Owned by core-value-walk-sharing-bound.",
	},

	// The format estimator. Its own scans are bounded by the format string; the
	// walk it reaches for a non-String operand is not, and it runs before the
	// pre-charge rather than under it.
	{
		Families:    []string{"string"},
		Fn:          "format",
		File:        "plugins/stdlib/strings.go",
		Func:        "estimateFormatAllocBytes",
		PhaseLabel:  "format string scan",
		Disposition: "bounded-exception",
		Proof: "The outer loop advances one byte at a time over the format " +
			"string and holds no Step of its own. That string is args[0] " +
			"asserted to core.String, so it reached the builtin only by already " +
			"sitting in the allocation ledger and MaxWork is that ledger bound.",
		MaxWork: 67_108_848,
	},
	{
		Families:    []string{"string"},
		Fn:          "format",
		File:        "plugins/stdlib/strings.go",
		Func:        "estimateFormatAllocBytes",
		PhaseLabel:  "verb flag scan",
		Disposition: "bounded-exception",
		Proof: "The flag loop inside estimateOne consumes the flag bytes of one " +
			"verb and cannot outrun the format string it indexes, so the same " +
			"ledger bound on that core.String is MaxWork.",
		MaxWork: 67_108_848,
	},
	{
		Families:    []string{"string"},
		Fn:          "format",
		File:        "plugins/stdlib/strings.go",
		Func:        "estimateFormatValueBytes",
		PhaseLabel:  "default verb estimate",
		Disposition: "unbounded-tracked",
		Proof: "The default arm sizes an operand by calling core.ValueDeepBytes " +
			"on it, which descends the value as a tree while the ledger charged " +
			"each shared node once, so a node reachable twice is measured " +
			"twice. It runs inside the estimator, ahead of the pre-charge the " +
			"estimate feeds, so nothing guards it. Owned by " +
			"core-value-walk-sharing-bound.",
	},
	{
		Families:    []string{"string"},
		Fn:          "format",
		File:        "plugins/stdlib/strings.go",
		Func:        "formatStringBytes",
		PhaseLabel:  "deep size walk",
		Disposition: "unbounded-tracked",
		Proof: "This is the call estimateFormatAllocBytes reaches " +
			"core.ValueDeepBytes through for every operand that is not a " +
			"core.String, on the %s, %q and %x arms alike. The walk descends " +
			"the value as a tree while the ledger charged each shared node " +
			"once, and it runs before the pre-charge rather than under it, so " +
			"the estimator itself is unbounded however small the estimate it " +
			"returns. Owned by core-value-walk-sharing-bound.",
	},
	{
		Families:    []string{"string"},
		Fn:          "format",
		File:        "plugins/stdlib/strings.go",
		Func:        "mismatchedVerbDiagnosticBytes",
		PhaseLabel:  "mismatch diagnostic bound",
		Disposition: "bounded-exception",
		Proof: "Its one fmt.Sprintf carries the %T verb, which renders the " +
			"name of one of the 13 concrete core.Value types and reads none of " +
			"a value's contents, so the render is constant work. The operand " +
			"length it adds comes from formatStringBytes over a core.String - " +
			"the caller dispatches on that type - which is a len of the string, " +
			"a single read, and the ledger already bounded that string when it " +
			"was built.",
		MaxWork: 256,
	},
	{
		Families:    []string{"string"},
		Fn:          "format",
		File:        "plugins/stdlib/strings.go",
		Func:        "parseFormatArgIndex",
		PhaseLabel:  "argument index scan",
		Disposition: "bounded-exception",
		Proof: "The loop consumes the digits of one [n] argument index inside " +
			"the format string and cannot outrun it, so MaxWork is the " +
			"allocation ledger's bound on that core.String.",
		MaxWork: 67_108_848,
	},
	{
		Families:    []string{"string"},
		Fn:          "format",
		File:        "plugins/stdlib/strings.go",
		Func:        "parseFormatInt",
		PhaseLabel:  "width and precision scan",
		Disposition: "bounded-exception",
		Proof: "The loop consumes the digits of one width or precision inside " +
			"the format string and cannot outrun it, so MaxWork is the " +
			"allocation ledger's bound on that core.String. The accumulator " +
			"saturates at maxFormatEstimate rather than overflowing.",
		MaxWork: 67_108_848,
	},

	// The failed-parse message the string family owns. The support-family row on
	// the same function states the ceiling for every other cause and points here
	// for this one.
	{
		Families:    []string{"string"},
		Fn:          "string->int string->float",
		File:        "plugins/stdlib/errors.go",
		Func:        "wrapCause",
		PhaseLabel:  "strconv message format",
		Disposition: "bounded-exception",
		Proof: "strconv.NumError.Error builds its text with " +
			"strconv.Quote(e.Num), quoting the whole parsed subject, and " +
			"wrapCause renders that text again through \"%s: %v\", so the " +
			"subject is materialized twice. The bound holds here where the " +
			"formatting boundary's does not: the subject is one flat " +
			"core.String with no children, so no structural sharing can " +
			"amplify it, and strconv.Quote's expansion is a constant factor of " +
			"at most four - \\xNN for a byte that is not valid UTF-8. Measured " +
			"at a 1 MiB subject: 1048620 bytes of ASCII at 1.0000x, 4194348 of " +
			"invalid UTF-8 at 4.0000x, 2097196 of quotes and newlines at " +
			"2.0000x; a 1-byte subject renders 45 bytes for ParseInt and 47 for " +
			"ParseFloat. MaxWork is two renders of four bytes per subject byte " +
			"plus 128 bytes of fixed wrapper text each, over the allocation " +
			"ledger's 67108848-byte ceiling on the subject. " +
			"plugins/stdlib/strings.go pre-charges both renders through " +
			"parseFailureMessageBytes, which bills " +
			"2 * core.StringShallowBytes(4n): four bytes per subject byte for " +
			"each render plus a 16-byte string header each, 8n+32 against the " +
			"8n+256 this ceiling states. The 224-byte gap is the fixed wrapper " +
			"text the ceiling allows each render and the pre-charge carries " +
			"only a header for.",
		MaxWork: 536_871_040,
	},
}
