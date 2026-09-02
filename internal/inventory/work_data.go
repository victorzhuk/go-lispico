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
}
