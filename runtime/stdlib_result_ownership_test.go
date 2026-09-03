package runtime

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/cl"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/internal/inventory"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
)

// ownershipArmed selects the inventory rows a proof arm must exist for. A
// scalar singleton owns no bytes, an error return never reaches the result
// ledger, and a purely internal support row is not dispatched from Lisp — none
// of the three can move a result-allocation differential.
func ownershipArmed(row inventory.ResultBranch) bool {
	return row.Class != "scalar-singleton" &&
		!strings.HasSuffix(row.BranchLabel, "error return") &&
		!(len(row.Families) == 1 && row.Families[0] == "support")
}

// ownershipArm names one inventory row and the differential that proves its
// charging decision. file/fnc/label are the row's own fields; rowFn is stated
// only where that triple is ambiguous among armed rows.
type ownershipArm struct {
	file  string
	fnc   string
	label string
	rowFn string
	// rep is the name the arm dispatches through, and must be one of the
	// row's own Fn names: a representative that drifts onto a builtin the row
	// does not cover proves nothing about the row.
	rep   string
	class string
	// dia selects the dialect the source is written in: "" for Clojure,
	// "cl" for the Common Lisp adapters.
	dia string
	// kind is the differential:
	//   noop   - payload size 1 vs borrowedLen must not move the ledger,
	//            and where srcB is set, neither may the subject's length
	//   grow   - payload size must move the ledger, and force a budget trip
	//   shape  - src returns the empty branch, srcB its one-element sibling
	//   count  - the same source over n vs 2n synthesized elements
	//   linear - the same source applied n and 2n times, charged linearly
	kind string
	src  string
	srcB string
}

func (a ownershipArm) matches(row inventory.ResultBranch) bool {
	return row.File == a.file && row.Func == a.fnc && row.BranchLabel == a.label &&
		(a.rowFn == "" || row.Fn == a.rowFn)
}

func (a ownershipArm) key() string {
	return a.file + " :: " + a.fnc + " :: " + a.label + " :: " + a.rep
}

// ownershipArms holds exactly one arm per armed inventory row.
var ownershipArms = []ownershipArm{
	// borrowed
	{file: "cl/charges.go", fnc: "finishAdapter", label: "settled value return", rep: "cl/sort@1", class: "borrowed", dia: "cl", kind: "noop", src: `(sort l2 (fn (a b) (< 1 0)))`},
	{file: "cl/cl.go", fnc: "clNth", label: "hit return", rep: "cl/nth@1", class: "borrowed", dia: "cl", kind: "noop", src: `(nth 0 l2)`},
	{file: "cl/cl.go", fnc: "clSort", label: "key projection return", rep: "cl/sort@1", class: "borrowed", dia: "cl", kind: "noop", src: `(sort l2 (fn (a b) (< 1 0)) :key (fn (x) x))`},
	{file: "internal/collections/kernels.go", fnc: "IndexedAccess", label: "list hit return", rep: "nth", class: "borrowed", kind: "noop", src: `(nth l2 0)`},
	{file: "internal/collections/kernels.go", fnc: "IndexedAccess", label: "vector hit return", rep: "nth", class: "borrowed", kind: "noop", src: `(nth v2 0)`},
	{file: "internal/collections/kernels.go", fnc: "StableSort", label: "sorted slice return", rep: "sort", class: "borrowed", kind: "noop", src: `(sort l2)`},
	{file: "internal/collections/kernels.go", fnc: "finishSort", label: "settled slice return", rep: "sort", class: "borrowed", kind: "noop", src: `(sort l2)`},
	// last, not first: the row's Fn list is the 34 builtins that settle
	// through finishBuiltin, and first is not one of them.
	{file: "plugins/stdlib/charges.go", fnc: "finishBuiltin", label: "settled value return", rep: "last", class: "borrowed", kind: "noop", src: `(last l2)`},
	{file: "plugins/stdlib/collections.go", fnc: "collectionBuiltins", label: "caller default return", rep: "nth", class: "borrowed", kind: "noop", src: `(nth l2 5 d)`},
	{file: "plugins/stdlib/collections.go", fnc: "collectionBuiltins", label: "delegated lookup return", rep: "get-in", class: "borrowed", kind: "noop", src: `(get-in m pa)`},
	{file: "plugins/stdlib/collections.go", fnc: "collectionBuiltins", label: "indexed element return", rep: "nth", class: "borrowed", kind: "noop", src: `(nth l2 0)`},
	{file: "plugins/stdlib/collections.go", fnc: "collectionBuiltins", label: "list head return", rep: "first", class: "borrowed", kind: "noop", src: `(first l2)`},
	{file: "plugins/stdlib/collections.go", fnc: "collectionBuiltins", label: "list tail element return", rep: "last", class: "borrowed", kind: "noop", src: `(last l2)`},
	// A list tail is a container, so its shallow size tracks the subject's
	// length and not the payload: only a longer subject can show whether the
	// borrowed tail is charged.
	{file: "plugins/stdlib/collections.go", fnc: "collectionBuiltins", label: "list tail return", rep: "rest", class: "borrowed", kind: "noop", src: `(rest l2)`, srcB: `(rest l5)`},
	{file: "plugins/stdlib/collections.go", fnc: "collectionBuiltins", label: "stored or default value return", rep: "get", class: "borrowed", kind: "noop", src: `(get m :a)`},
	{file: "plugins/stdlib/collections.go", fnc: "collectionBuiltins", label: "vector head return", rep: "first", class: "borrowed", kind: "noop", src: `(first v2)`},
	{file: "plugins/stdlib/collections.go", fnc: "collectionBuiltins", label: "vector tail element return", rep: "last", class: "borrowed", kind: "noop", src: `(last v2)`},
	{file: "plugins/stdlib/collections.go", fnc: "getInLookup", label: "missing key default return", rep: "get-in", class: "borrowed", kind: "noop", src: `(get-in m pz d)`},
	{file: "plugins/stdlib/collections.go", fnc: "getInLookup", label: "nil subject default return", rep: "get-in", class: "borrowed", kind: "noop", src: `(get-in nada pa d)`},
	{file: "plugins/stdlib/collections.go", fnc: "getInLookup", label: "path exhausted subject return", rep: "get-in", class: "borrowed", kind: "noop", src: `(get-in d pe)`},
	{file: "plugins/stdlib/collections.go", fnc: "getInResult", label: "settled value return", rep: "get-in", class: "borrowed", kind: "noop", src: `(get-in m pa)`},
	{file: "plugins/stdlib/collections.go", fnc: "next", label: "list key return", rep: "get-in", class: "borrowed", kind: "noop", src: `(get-in m pa)`},
	{file: "plugins/stdlib/collections.go", fnc: "next", label: "vector key return", rep: "get-in", class: "borrowed", kind: "noop", src: `(get-in m pav)`},
	// seqInput returns a borrowed []core.Value that its caller consumes
	// internally, so no result of its own crosses a dispatch boundary and it
	// has no charge site: the differential these two arms observe is reverse's
	// own fresh-container charge. A red here means that container size
	// expression moved, not that seqInput began charging.
	{file: "plugins/stdlib/collections.go", fnc: "seqInput", label: "list elements return", rep: "reverse", class: "borrowed", kind: "noop", src: `(reverse l2)`},
	{file: "plugins/stdlib/collections.go", fnc: "seqInput", label: "vector elements return", rep: "reverse", class: "borrowed", kind: "noop", src: `(reverse v2)`},
	{file: "plugins/stdlib/higher_order.go", fnc: "registerHigherOrder", label: "accumulator return", rep: "reduce", class: "borrowed", kind: "noop", src: `(reduce (fn [a b] a) l2)`},
	{file: "plugins/stdlib/higher_order.go", fnc: "registerHigherOrder", label: "callee result return", rep: "apply", class: "borrowed", kind: "noop", src: `(apply (fn [x] x) l1)`},
	{file: "plugins/stdlib/types.go", fnc: "registerTypes", label: "retyped keyword return", rep: "str->keyword", class: "borrowed", kind: "noop", src: `(str->keyword d)`},
	{file: "plugins/stdlib/types.go", fnc: "registerTypes", label: "retyped string return", rep: "keyword->str", class: "borrowed", kind: "noop", src: `(keyword->str kw)`},

	// fresh-container
	{file: "cl/cl.go", fnc: "clMapcar", label: "mapped list return", rep: "cl/mapcar@1", class: "fresh-container", dia: "cl", kind: "noop", src: `(mapcar (fn (x) x) l2)`},
	{file: "cl/cl.go", fnc: "clSort", label: "sorted list return", rep: "cl/sort@1", class: "fresh-container", dia: "cl", kind: "noop", src: `(sort l2 (fn (a b) (< 1 0)))`},
	{file: "cl/cl.go", fnc: "clSort", label: "sorted vector return", rep: "cl/sort@1", class: "fresh-container", dia: "cl", kind: "noop", src: `(sort v2 (fn (a b) (< 1 0)))`},
	{file: "internal/collections/kernels.go", fnc: "MapSequences", label: "mapped list return", rep: "map", class: "fresh-container", kind: "noop", src: `(map (fn [x] x) l2)`},
	// An empty container holds no element, so it has no payload axis: its
	// charge is proved against the one-element sibling branch instead.
	{file: "plugins/stdlib/collections.go", fnc: "collectionBuiltins", label: "empty list return", rowFn: "concat", rep: "concat", class: "fresh-container", kind: "shape", src: `(concat)`, srcB: `(concat v1)`},
	{file: "plugins/stdlib/collections.go", fnc: "collectionBuiltins", label: "empty list return", rowFn: "sort", rep: "sort", class: "fresh-container", kind: "shape", src: `(sort nada)`, srcB: `(sort l1)`},
	{file: "plugins/stdlib/collections.go", fnc: "collectionBuiltins", label: "flattened list return", rep: "concat", class: "fresh-container", kind: "noop", src: `(concat v1 v1)`},
	{file: "plugins/stdlib/collections.go", fnc: "collectionBuiltins", label: "key list return", rep: "keys", class: "fresh-container", kind: "noop", src: `(keys mk)`},
	{file: "plugins/stdlib/collections.go", fnc: "collectionBuiltins", label: "nil subject empty list return", rep: "rest", class: "fresh-container", kind: "shape", src: `(rest nada)`, srcB: `(rest v2)`},
	{file: "plugins/stdlib/collections.go", fnc: "collectionBuiltins", label: "reversed list return", rep: "reverse", class: "fresh-container", kind: "noop", src: `(reverse l2)`},
	{file: "plugins/stdlib/collections.go", fnc: "collectionBuiltins", label: "short vector empty list return", rep: "rest", class: "fresh-container", kind: "shape", src: `(rest v1)`, srcB: `(rest v2)`},
	{file: "plugins/stdlib/collections.go", fnc: "collectionBuiltins", label: "sorted list", rep: "sort", class: "fresh-container", kind: "noop", src: `(sort l2)`},
	{file: "plugins/stdlib/collections.go", fnc: "collectionBuiltins", label: "value list return", rep: "vals", class: "fresh-container", kind: "noop", src: `(vals m)`},
	{file: "plugins/stdlib/collections.go", fnc: "collectionBuiltins", label: "vector prefixed list return", rep: "cons", class: "fresh-container", kind: "noop", src: `(cons d v2)`},
	{file: "plugins/stdlib/collections.go", fnc: "collectionBuiltins", label: "vector tail list return", rep: "rest", class: "fresh-container", kind: "noop", src: `(rest v2)`},
	{file: "plugins/stdlib/higher_order.go", fnc: "registerHigherOrder", label: "mapped list return", rep: "map", class: "fresh-container", kind: "noop", src: `(map (fn [x] x) l2)`},
	{file: "plugins/stdlib/higher_order.go", fnc: "registerHigherOrder", label: "retained list return", rep: "filter", class: "fresh-container", kind: "noop", src: `(filter (fn [x] x) l2)`},
	{file: "plugins/stdlib/strings.go", fnc: "registerStrings", label: "line list return", rep: "string/lines", class: "fresh-container", kind: "noop", src: `(string/lines d)`},
	{file: "plugins/stdlib/strings.go", fnc: "registerStrings", label: "part list return", rep: "string/split", class: "fresh-container", kind: "noop", src: `(string/split d "|")`},

	// fresh-deep
	{file: "plugins/stdlib/collections.go", fnc: "collectionBuiltins", label: "fresh list return", rep: "list", class: "fresh-deep", kind: "grow", src: `(list d)`},
	{file: "plugins/stdlib/collections.go", fnc: "collectionBuiltins", label: "fresh map return", rep: "hash-map", class: "fresh-deep", kind: "grow", src: `(hash-map :a d)`},
	{file: "plugins/stdlib/collections.go", fnc: "collectionBuiltins", label: "fresh vector return", rep: "vector", class: "fresh-deep", kind: "grow", src: `(vector d)`},
	// range synthesizes its own Ints, so no caller payload reaches the
	// result: its deep charge is only observable on the element count.
	{file: "plugins/stdlib/collections.go", fnc: "collectionBuiltins", label: "generated list return", rep: "range", class: "fresh-deep", kind: "count", src: `(range nn)`},
	{file: "plugins/stdlib/collections.go", fnc: "collectionBuiltins", label: "merged map return", rep: "merge", class: "fresh-deep", kind: "grow", src: `(merge m)`},

	// incremental-persistent
	{file: "plugins/stdlib/collections.go", fnc: "collectionBuiltins", label: "assoced map return", rep: "assoc", class: "incremental-persistent", kind: "linear", src: `(loop [i 0 acc m] (if (= i nn) acc (recur (+ i 1) (assoc acc i i))))`},
	{file: "plugins/stdlib/collections.go", fnc: "collectionBuiltins", label: "conjed list return", rep: "conj", class: "incremental-persistent", kind: "linear", src: `(loop [i 0 acc l1] (if (= i nn) acc (recur (+ i 1) (conj acc i))))`},
	{file: "plugins/stdlib/collections.go", fnc: "collectionBuiltins", label: "conjed map return", rep: "conj", class: "incremental-persistent", kind: "linear", src: `(loop [i 0 acc m] (if (= i nn) acc (recur (+ i 1) (conj acc i i))))`},
	{file: "plugins/stdlib/collections.go", fnc: "collectionBuiltins", label: "conjed vector return", rep: "conj", class: "incremental-persistent", kind: "linear", src: `(loop [i 0 acc v1] (if (= i nn) acc (recur (+ i 1) (conj acc i))))`},
	{file: "plugins/stdlib/collections.go", fnc: "collectionBuiltins", label: "consed list return", rep: "cons", class: "incremental-persistent", kind: "linear", src: `(loop [i 0 acc l1] (if (= i nn) acc (recur (+ i 1) (cons i acc))))`},
	{file: "plugins/stdlib/collections.go", fnc: "collectionBuiltins", label: "dissoced map return", rep: "dissoc", class: "incremental-persistent", kind: "linear", src: `(loop [i 0 acc bigm] (if (= i nn) acc (recur (+ i 1) (dissoc acc i))))`},
	{file: "plugins/stdlib/collections.go", fnc: "collectionBuiltins", label: "shared-tail list return", rep: "concat", class: "incremental-persistent", kind: "linear", src: `(loop [i 0 acc l1] (if (= i nn) acc (recur (+ i 1) (concat v1 acc))))`},

	// mixed-string
	{file: "plugins/stdlib/strings.go", fnc: "registerStrings", label: "concatenated string return", rep: "str", class: "mixed-string", kind: "grow", src: `(str d)`},
	{file: "plugins/stdlib/strings.go", fnc: "registerStrings", label: "joined string return", rep: "string/join", class: "mixed-string", kind: "grow", src: `(string/join "," l2)`},
	{file: "plugins/stdlib/strings.go", fnc: "registerStrings", label: "rendered string return", rep: "format", class: "mixed-string", kind: "grow", src: `(format "%s" d)`},
	{file: "plugins/stdlib/strings.go", fnc: "registerStrings", label: "replaced string return", rep: "string/replace", class: "mixed-string", kind: "grow", src: `(string/replace d "x" "y")`},
	{file: "plugins/stdlib/strings.go", fnc: "unaryStringFunc", label: "primitive output return", rep: "string/upper", class: "mixed-string", kind: "grow", src: `(string/upper d)`},
}

const (
	// ownershipGenerousBytes is high enough that no baseline run can trip it,
	// so a measured total is the charge and not a truncation.
	ownershipGenerousBytes = 16 << 20
	// ownershipN is large enough that a quadratic charge separates from a
	// linear one by more than the loop's own per-iteration background.
	ownershipN = 256
	// ownershipMaxDoubling is the doubling ratio a linear charge may reach.
	// Measured linear arms land between 1.67 and 2.49; a quadratic charge
	// approaches 4.
	ownershipMaxDoubling = 3
)

// ownershipEngine binds every fixture a proof source can name, all built in Go
// so the subject's own construction never enters the ledger the arm measures.
func ownershipEngine(t *testing.T, bytecode bool, dia string, budget int, payload string, n int) Engine {
	t.Helper()

	dialect := clojure.Dialect()
	if dia == "cl" {
		dialect = cl.Dialect()
	}
	opts := []EngineOption{WithResourceLimits(meteringLimits(t, 1_000_000, budget)), WithDialect(dialect)}
	if bytecode {
		opts = append(opts, WithBytecode())
	} else {
		opts = append(opts, WithTreeWalker())
	}
	e, err := New(nil, opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })
	require.NoError(t, e.Use(stdlib.New()))

	d := core.String{V: payload}
	kw := core.Keyword{V: payload}
	m := core.NewHashMap()
	require.NoError(t, m.Set(core.Keyword{V: "a"}, d))
	keyed := core.NewHashMap()
	require.NoError(t, keyed.Set(kw, core.Int{V: 1}))
	big := core.NewHashMap()
	for i := range 2*n + 1 {
		require.NoError(t, big.Set(core.Int{V: int64(i)}, core.Int{V: int64(i)}))
	}

	for name, v := range map[string]core.Value{
		"d":    d,
		"kw":   kw,
		"m":    m,
		"mk":   keyed,
		"bigm": big,
		"l1":   core.NewList([]core.Value{d}),
		"l2":   core.NewList([]core.Value{d, d}),
		"l5":   core.NewList([]core.Value{d, d, d, d, d}),
		"v1":   core.NewVector([]core.Value{d}),
		"v2":   core.NewVector([]core.Value{d, d}),
		"nada": core.Nil{},
		"pa":   core.NewList([]core.Value{core.Keyword{V: "a"}}),
		"pav":  core.NewVector([]core.Value{core.Keyword{V: "a"}}),
		"pz":   core.NewList([]core.Value{core.Keyword{V: "zz"}}),
		"pe":   core.NewList(nil),
		"nn":   core.Int{V: int64(n)},
	} {
		require.NoError(t, e.Bind(name, v))
	}
	return e
}

func ownershipUsage(t *testing.T, bytecode bool, a ownershipArm, payload string, n, budget int, src string) (int64, error) {
	t.Helper()
	e := ownershipEngine(t, bytecode, a.dia, budget, payload, n)
	ctx := core.WithEvalResourceLimits(t.Context(), 1_000_000, budget)
	_, err := e.Eval(ctx, "result-ownership", src)
	return core.EvalMeterFrom(ctx).Snapshot().AllocationBytes, err
}

func ownershipBaseline(t *testing.T, bytecode bool, a ownershipArm, payload string, n int, src string) int64 {
	t.Helper()
	total, err := ownershipUsage(t, bytecode, a, payload, n, ownershipGenerousBytes, src)
	require.NoError(t, err, "%s: baseline run must not trip the generous budget", a.key())
	return total
}

func TestResultOwnership_EveryInventoriedBranchHasAnArm(t *testing.T) {
	skipUntilMeteringFields(t)

	var armed []inventory.ResultBranch
	for _, row := range inventory.ResultBranches {
		if ownershipArmed(row) {
			armed = append(armed, row)
			continue
		}
		// The exempt set is sealed by this: a row that names a charge
		// expression is making a charging claim and cannot sit outside the
		// armed set unproved.
		assert.Empty(t, row.ChargeExpr,
			"exempt row names a charge expression: %s :: %s :: %s :: %s", row.File, row.Func, row.BranchLabel, row.Fn)
	}
	require.NotEmpty(t, armed, "inventory.ResultBranches yielded no armed rows")

	t.Run("forward coverage", func(t *testing.T) {
		for _, row := range armed {
			n := 0
			for _, a := range ownershipArms {
				if a.matches(row) {
					n++
				}
			}
			assert.Equal(t, 1, n,
				"armed row must have exactly one arm, got %d: %s :: %s :: %s :: %s", n, row.File, row.Func, row.BranchLabel, row.Fn)
		}
	})

	t.Run("reverse coverage", func(t *testing.T) {
		for _, a := range ownershipArms {
			var hits []inventory.ResultBranch
			for _, row := range armed {
				if a.matches(row) {
					hits = append(hits, row)
				}
			}
			if !assert.Len(t, hits, 1, "arm must resolve to exactly one armed row: %s", a.key()) {
				continue
			}
			row := hits[0]
			assert.Equal(t, row.Class, a.class, "arm records the wrong class for %s", a.key())
			if row.Fn != "" {
				assert.Contains(t, strings.Fields(row.Fn), a.rep,
					"arm's representative is not one of the row's own names: %s (row Fn %q)", a.key(), row.Fn)
			}
		}
	})

	large := strings.Repeat("x", borrowedLen)
	for _, bytecode := range []bool{false, true} {
		t.Run(evalModeName(bytecode), func(t *testing.T) {
			for _, a := range ownershipArms {
				t.Run(a.rep+" "+a.label, func(t *testing.T) {
					switch a.kind {
					case "noop":
						tiny := ownershipBaseline(t, bytecode, a, "x", ownershipN, a.src)
						big := ownershipBaseline(t, bytecode, a, large, ownershipN, a.src)
						require.Equal(t, tiny, big,
							"borrowed result must add zero bytes to the ledger: a payload %d times larger moved the total by %d (tiny=%d large=%d)",
							borrowedLen, big-tiny, tiny, big)

						if a.srcB != "" {
							longer := ownershipBaseline(t, bytecode, a, "x", ownershipN, a.srcB)
							require.Equal(t, tiny, longer,
								"borrowed result must add zero bytes however long the subject is: %q and %q charged differently (%d vs %d)",
								a.src, a.srcB, tiny, longer)
						}

						tight := int(tiny + borrowedShallowBytes()/2)
						_, err := ownershipUsage(t, bytecode, a, large, ownershipN, tight, a.src)
						require.NoError(t, err,
							"borrowed result must not trip a budget tighter than its own shallow size (budget=%d)", tight)

					case "grow":
						tiny := ownershipBaseline(t, bytecode, a, "x", ownershipN, a.src)
						big := ownershipBaseline(t, bytecode, a, large, ownershipN, a.src)
						require.Greater(t, big, tiny,
							"fresh result must charge the payload it owns: a payload %d times larger did not move the total (tiny=%d large=%d)",
							borrowedLen, tiny, big)

						tight := int(tiny + (big-tiny)/2)
						_, err := ownershipUsage(t, bytecode, a, large, ownershipN, tight, a.src)
						require.True(t, isResourceLimit(t, err),
							"fresh result must trip a budget below its fresh delta (budget=%d delta=%d), got %v", tight, big-tiny, err)

					case "shape":
						small := ownershipBaseline(t, bytecode, a, "x", ownershipN, a.src)
						grown := ownershipBaseline(t, bytecode, a, "x", ownershipN, a.srcB)
						require.Greater(t, grown, small,
							"the fresh container must be charged per element: %q and %q charged the same (small=%d grown=%d)",
							a.src, a.srcB, small, grown)

						tight := int(small + (grown-small)/2)
						_, err := ownershipUsage(t, bytecode, a, "x", ownershipN, tight, a.srcB)
						require.True(t, isResourceLimit(t, err),
							"the larger container must trip a budget below its fresh delta (budget=%d delta=%d), got %v", tight, grown-small, err)

					case "count":
						small := ownershipBaseline(t, bytecode, a, "x", ownershipN, a.src)
						grown := ownershipBaseline(t, bytecode, a, "x", 2*ownershipN, a.src)
						// Doubling the count doubles the container too, so
						// growth alone cannot tell a deep charge from a
						// container one: the delta has to outrun the list
						// header the elements are held in.
						floor := core.ListShallowBytes(2*ownershipN) - core.ListShallowBytes(ownershipN)
						require.Greater(t, grown-small, floor,
							"a fresh-deep result must charge the elements it synthesized, not just the container holding them (delta=%d container-only=%d)",
							grown-small, floor)

						tight := int(small + (grown-small)/2)
						_, err := ownershipUsage(t, bytecode, a, "x", 2*ownershipN, tight, a.src)
						require.True(t, isResourceLimit(t, err),
							"the larger result must trip a budget below its fresh delta (budget=%d delta=%d), got %v", tight, grown-small, err)

					case "linear":
						base := ownershipBaseline(t, bytecode, a, "x", 0, a.src)
						once := ownershipBaseline(t, bytecode, a, "x", ownershipN, a.src)
						twice := ownershipBaseline(t, bytecode, a, "x", 2*ownershipN, a.src)
						d1, d2 := once-base, twice-base
						require.Positive(t, d1,
							"an incremental charge must grow with the number of applications (base=%d n=%d total=%d)", base, ownershipN, once)
						require.LessOrEqual(t, d2, int64(ownershipMaxDoubling)*d1,
							"doubling the applications must not more than %dx the charge — a per-call charge over the whole accumulated result is quadratic (d1=%d d2=%d)",
							ownershipMaxDoubling, d1, d2)

						tight := int(base + d1/2)
						_, err := ownershipUsage(t, bytecode, a, "x", ownershipN, tight, a.src)
						require.True(t, isResourceLimit(t, err),
							"the accumulated fresh delta must trip a budget below it (budget=%d delta=%d), got %v", tight, d1, err)

					default:
						t.Fatalf("arm %s declares an unknown differential kind %q", a.key(), a.kind)
					}
				})
			}
		})
	}
}
