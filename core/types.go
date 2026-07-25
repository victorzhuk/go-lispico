package core

import (
	"context"
	"fmt"
	"maps"
	"math"
	"sort"
	"strconv"
)

// Value is the universal Lisp value interface.
type Value interface {
	Type() Keyword
	String() string
	Equals(other Value) bool
}

// Evaluator allows GoFunc implementations to recursively evaluate Lisp forms.
// Defined here (not in eval.go) to avoid circular imports — GoFunc needs it.
type Evaluator interface {
	Eval(ctx context.Context, form Value, env *Env) (Value, error)
	Apply(ctx context.Context, fn Value, args []Value, env *Env) (Value, error)
}

// CollectionLimiter is implemented by an Evaluator whose Engine caps the
// length of collections built by builtins such as range. Read-only: the value
// is fixed at Engine construction.
type CollectionLimiter interface {
	CollectionLimit() int
}

// Nil — the empty value; the only falsy value besides false.
type Nil struct{}

func (n Nil) Type() Keyword       { return Keyword{V: "nil"} }
func (n Nil) String() string      { return "nil" }
func (n Nil) Equals(o Value) bool { _, ok := o.(Nil); return ok }

// Bool — true or false.
type Bool struct{ V bool }

func (b Bool) Type() Keyword { return Keyword{V: "bool"} }
func (b Bool) String() string {
	if b.V {
		return "true"
	}
	return "false"
}

func (b Bool) Equals(o Value) bool {
	if v, ok := o.(Bool); ok {
		return b.V == v.V
	}
	return false
}

// True and False are shared Bool instances. BoxBool returns one of them
// instead of allocating a fresh Bool on every boolean result.
var (
	True  Value = Bool{V: true}
	False Value = Bool{V: false}
)

// BoxBool returns the shared True or False instance for b.
func BoxBool(b bool) Value {
	if b {
		return True
	}
	return False
}

// Int — fixed-precision signed 64-bit integer.
type Int struct{ V int64 }

func (i Int) Type() Keyword { return Keyword{V: "int"} }
func (i Int) String() string {
	return strconv.FormatInt(i.V, 10)
}

func (i Int) Equals(o Value) bool {
	if v, ok := o.(Int); ok {
		return i.V == v.V
	}
	return false
}

// minPreboxedInt and maxPreboxedInt bound the shared Int instances BoxInt
// returns for. 0..255 already boxes alloc-free via the Go runtime's own
// small-value interface cache; this range extends that to negatives and the
// rest of the common small-integer span (loop counters, small arithmetic).
const (
	minPreboxedInt = -128
	maxPreboxedInt = 1023
)

var preboxedInts [maxPreboxedInt - minPreboxedInt + 1]Value

func init() {
	for i := range preboxedInts {
		preboxedInts[i] = Int{V: int64(i) + minPreboxedInt}
	}
}

// BoxInt returns a Value wrapping v, reusing a shared instance when v is in
// [-128, 1023] to avoid a heap allocation on the hot arithmetic path. Outside
// that range it boxes a fresh Int as usual.
func BoxInt(v int64) Value {
	if v >= minPreboxedInt && v <= maxPreboxedInt {
		return preboxedInts[v-minPreboxedInt]
	}
	return Int{V: v}
}

// Float — IEEE 754 double.
type Float struct{ V float64 }

func (f Float) Type() Keyword { return Keyword{V: "float"} }
func (f Float) String() string {
	return strconv.FormatFloat(f.V, 'f', -1, 64)
}

func (f Float) Equals(o Value) bool {
	if v, ok := o.(Float); ok {
		return f.V == v.V
	}
	return false
}

// String — UTF-8 immutable string.
type String struct{ V string }

func (s String) Type() Keyword { return Keyword{V: "string"} }
func (s String) String() string {
	return fmt.Sprintf("%q", s.V)
}

func (s String) Equals(o Value) bool {
	if v, ok := o.(String); ok {
		return s.V == v.V
	}
	return false
}

// Symbol — resolves to a value in the environment.
type Symbol struct{ V string }

func (s Symbol) Type() Keyword  { return Keyword{V: "symbol"} }
func (s Symbol) String() string { return s.V }
func (s Symbol) Equals(o Value) bool {
	if v, ok := o.(Symbol); ok {
		return s.V == v.V
	}
	return false
}

// Keyword — self-evaluating named constant; used as map keys and option flags.
type Keyword struct{ V string }

func (k Keyword) Type() Keyword { return Keyword{V: "keyword"} }
func (k Keyword) String() string {
	return ":" + k.V
}

func (k Keyword) Equals(o Value) bool {
	if v, ok := o.(Keyword); ok {
		return k.V == v.V
	}
	return false
}

// listFlatThreshold is the largest length List stores as a flat slice.
// Above it, List switches to a shared-tail node chain so Cons stops copying
// the whole backing array on every prepend — see listNode.
const listFlatThreshold = 32

// listNode is one persistent cons cell in a shared-tail List. Immutable once
// built: Cons only ever allocates a new head node pointing at an existing
// tail, so any List holding a reference to that tail can safely alias it.
type listNode struct {
	head  Value
	tail  *listNode
	count int // elements from this node to the end of the chain
}

// List — immutable sequence. At or below listFlatThreshold elements it is a
// flat slice (cheap random access; matches reader output and other small,
// short-lived forms). Above the threshold it is a shared-tail node chain, so
// Cons is O(1) instead of O(n). Exactly one of flat/shared is set; the zero
// value List{} is the empty list. There is no demotion: Rest of a shared
// list stays shared even once its length drops back at or below the
// threshold.
type List struct {
	flat   []Value
	shared *listNode
}

func (l List) Type() Keyword  { return Keyword{V: "list"} }
func (l List) String() string { return boundedString(l, 0) }

func (l List) Equals(o Value) bool { return boundedEquals(l, o, 0) }

// NewList wraps items as a List. Below listFlatThreshold it stores items
// without copying — same contract as before: the caller must not mutate
// items afterward. At or above the threshold it builds a shared-tail chain,
// copying each element once into node storage.
func NewList(items []Value) List {
	if len(items) <= listFlatThreshold {
		return List{flat: items}
	}
	return List{shared: newListChain(items)}
}

// newListChain builds a shared-tail chain from items in index order:
// items[0] becomes the outermost node's head, items[len-1] the innermost.
func newListChain(items []Value) *listNode {
	var node *listNode
	for i := len(items) - 1; i >= 0; i-- {
		node = &listNode{head: items[i], tail: node, count: len(items) - i}
	}
	return node
}

func (l List) Len() int {
	if l.shared != nil {
		return l.shared.count
	}
	return len(l.flat)
}

// At returns the element at i, panicking out of range like indexing a slice.
// O(1) flat, O(i) shared — a list already costs that much for random
// access.
//
// The flat branch panics via Go's native slice indexing; the shared branch
// panics explicitly with a plain string. Unifying them costs 52-340% on the
// flat path, so this stays asymmetric — every caller bounds-checks first.
func (l List) At(i int) Value {
	if l.shared == nil {
		return l.flat[i]
	}
	if i < 0 || i >= l.shared.count {
		panic("index out of range")
	}
	node := l.shared
	for ; i > 0; i-- {
		node = node.tail
	}
	return node.head
}

// ToSlice returns a fresh copy of l's elements the caller may mutate.
func (l List) ToSlice() []Value {
	if l.shared == nil {
		out := make([]Value, len(l.flat))
		copy(out, l.flat)
		return out
	}
	out := make([]Value, l.shared.count)
	node := l.shared
	for i := range out {
		out[i] = node.head
		node = node.tail
	}
	return out
}

// slice returns the backing storage without copying when flat — the common
// case for core's hot paths, which read CODE forms off the reader (small
// and flat in practice) — and materializes a fresh slice when shared.
// Callers inside core must not retain or mutate a flat result. Cost: O(1)
// flat, O(n) shared.
func (l List) slice() []Value {
	if l.shared == nil {
		return l.flat
	}
	return l.ToSlice()
}

// each calls fn for every element in order, stopping early if fn returns
// false. O(n) total on both representations — unlike an indexed At() loop,
// which is O(n) per call on the shared form.
func (l List) each(fn func(Value) bool) {
	if l.shared == nil {
		for _, v := range l.flat {
			if !fn(v) {
				return
			}
		}
		return
	}
	for node := l.shared; node != nil; node = node.tail {
		if !fn(node.head) {
			return
		}
	}
}

// listCursor walks a List once, in order, without At()'s O(n)-per-call cost
// on the shared representation — for lock-step pairwise comparison of two
// lists.
type listCursor struct {
	flat []Value
	node *listNode
}

func (l List) cursor() listCursor {
	if l.shared == nil {
		return listCursor{flat: l.flat}
	}
	return listCursor{node: l.shared}
}

func (c *listCursor) next() (Value, bool) {
	if c.node != nil {
		v := c.node.head
		c.node = c.node.tail
		return v, true
	}
	if len(c.flat) == 0 {
		return nil, false
	}
	v := c.flat[0]
	c.flat = c.flat[1:]
	return v, true
}

// Rest returns l without its first element. O(1) on the shared form (return
// the tail chain) and a reslice on flat. An empty or single-element list
// returns an empty list.
func (l List) Rest() List {
	if l.shared != nil {
		if l.shared.tail == nil {
			return List{}
		}
		return List{shared: l.shared.tail}
	}
	if len(l.flat) <= 1 {
		return List{}
	}
	return List{flat: l.flat[1:]}
}

// Cons prepends v to l, returning the new list and the bytes this call
// newly allocated. Below the threshold it still copies the whole flat
// backing — cheap at this size, so the charge matches that whole copy. At
// or past it, Cons allocates a single node referencing the existing chain —
// O(1) instead of O(n) — and the charge is exactly that one node's cost,
// never the whole chain: a caller consing onto an already-long shared list
// is billed the same as one consing onto a short list.
func (l List) Cons(v Value) (List, int64) {
	if l.shared != nil {
		newLen := l.shared.count + 1
		return List{shared: &listNode{head: v, tail: l.shared, count: newLen}}, ListShallowBytes(1)
	}
	newLen := len(l.flat) + 1
	if newLen <= listFlatThreshold {
		items := make([]Value, newLen)
		items[0] = v
		copy(items[1:], l.flat)
		return List{flat: items}, ListShallowBytes(newLen)
	}
	// Promotion: flat crosses the threshold and becomes a chain of newLen
	// fresh nodes. A one-time cost bounded by listFlatThreshold+1, not by
	// how long the list grows through later Cons calls.
	return List{shared: &listNode{head: v, tail: newListChain(l.flat), count: newLen}}, ListShallowBytes(1) * int64(newLen)
}

// vectorFlatThreshold governs Conj only, not NewVector: below it, Conj
// still copies the whole flat backing; at or above it, Conj promotes to
// the trie, chunking any existing flat backing regardless of how it got
// that long. A separate constant from listFlatThreshold so the two can be
// retuned independently, even though both start at 32.
const vectorFlatThreshold = 32

// vecBits is the trie branching factor's log2: each level dispatches 5 bits
// of the index, for 32-way (vecBranch) fan-out.
const (
	vecBits   = 5
	vecBranch = 1 << vecBits
)

// vecNode is one level of a persistent vector trie. Exactly one of
// kids/vals is populated: internal nodes hold up to vecBranch children,
// leaves hold up to vecBranch values. Leaves are always full — only a full
// tail buffer is ever pushed into the trie. Immutable once built: growing
// the trie copies only the path to the new leaf, sharing every other
// subtree.
type vecNode struct {
	kids []*vecNode
	vals []Value
}

// Vector — random-access sequence. At or below vectorFlatThreshold elements
// it is a flat slice. Above it, elements split between a trie (root,
// holding a multiple of vecBranch elements at height shift) and a tail
// buffer of 0..vecBranch pending elements not yet folded into the trie.
// Exactly one of flat/root is set; the zero value Vector{} is the empty
// vector. There is no demotion.
type Vector struct {
	flat  []Value
	root  *vecNode
	shift uint
	count int
	tail  []Value
}

func (v Vector) Type() Keyword  { return Keyword{V: "vector"} }
func (v Vector) String() string { return boundedString(v, 0) }

func (v Vector) Equals(o Value) bool { return boundedEquals(v, o, 0) }

// NewVector wraps items as a Vector, always flat regardless of length — the
// caller must not mutate items afterward. Bulk construction never promotes
// to a trie: promotion happens on Conj crossing vectorFlatThreshold, not on
// length alone, because reader output and other build-once-never-conj'd
// vectors gain nothing from sharing and would only pay for it.
func NewVector(items []Value) Vector {
	return Vector{flat: items}
}

func (v Vector) Len() int {
	if v.root != nil {
		return v.count
	}
	return len(v.flat)
}

// At returns the element at i, panicking out of range like indexing a
// slice. O(1) flat or tail, O(log32 n) in the trie. See List.At for why the
// flat and trie/tail branches panic with different values rather than a
// uniform one.
func (v Vector) At(i int) Value {
	if v.root == nil {
		return v.flat[i]
	}
	if i < 0 || i >= v.count {
		panic("index out of range")
	}
	trieLen := v.count - len(v.tail)
	if i >= trieLen {
		return v.tail[i-trieLen]
	}
	node := v.root
	for shift := v.shift; shift > 0; shift -= vecBits {
		node = node.kids[(i>>shift)&(vecBranch-1)]
	}
	return node.vals[i&(vecBranch-1)]
}

// ToSlice returns a fresh copy of v's elements the caller may mutate.
func (v Vector) ToSlice() []Value {
	if v.root == nil {
		out := make([]Value, len(v.flat))
		copy(out, v.flat)
		return out
	}
	out := make([]Value, v.count)
	i := flattenVecNode(v.root, v.shift, out)
	copy(out[i:], v.tail)
	return out
}

// flattenVecNode writes node's elements, in order, into out starting at
// index 0, and returns the count written.
func flattenVecNode(node *vecNode, shift uint, out []Value) int {
	if shift == 0 {
		return copy(out, node.vals)
	}
	i := 0
	for _, kid := range node.kids {
		i += flattenVecNode(kid, shift-vecBits, out[i:])
	}
	return i
}

// slice returns the backing storage without copying when flat, and
// materializes a fresh slice when the trie form is in play. Callers inside
// core must not retain or mutate a flat result. Cost: O(1) flat, O(n) trie.
func (v Vector) slice() []Value {
	if v.root == nil {
		return v.flat
	}
	return v.ToSlice()
}

// Conj appends vs to the end of v, returning the new vector and the bytes
// this call newly allocated. Below the threshold it still copies the whole
// flat backing — cheap at this size, so the charge matches that whole copy.
// Past it, each element folds into a vecBranch-wide tail buffer; a full
// tail is pushed into the trie as one new leaf, sharing every existing
// subtree, and the charge reflects only that leaf plus the path to it —
// never the whole trie, however large it has grown by sharing.
func (v Vector) Conj(vs ...Value) (Vector, int64) {
	newLen := v.Len() + len(vs)
	if v.root == nil && newLen <= vectorFlatThreshold {
		items := make([]Value, len(v.flat)+len(vs))
		copy(items, v.flat)
		copy(items[len(v.flat):], vs)
		return Vector{flat: items}, VectorShallowBytes(len(items))
	}

	var root *vecNode
	var shift uint
	var trieLen int
	var tail []Value
	var bytes int64
	if v.root != nil {
		root, shift, trieLen = v.root, v.shift, v.count-len(v.tail)
		tail = append([]Value(nil), v.tail...)
	} else {
		// Promotion: flat drains into full leaves. A one-time cost bounded
		// by len(v.flat), not by how large the trie grows afterward.
		root, shift, trieLen, tail = buildVecTrieFromFlat(v.flat)
		bytes += VectorShallowBytes(len(v.flat))
	}
	for _, val := range vs {
		if len(tail) == vecBranch {
			bytes += vecPathBytes(shift)
			root, shift = pushVecTail(root, shift, trieLen, tail)
			trieLen += vecBranch
			tail = nil
		}
		tail = append(tail, val)
	}
	bytes += VectorShallowBytes(len(tail))
	return Vector{root: root, shift: shift, count: trieLen + len(tail), tail: tail}, bytes
}

// vecPathBytes is the charge for pushing one full tail into the trie as a
// new leaf at the given pre-push shift: the leaf itself plus a freshly
// copied internal-node chain from root down to it, one node per level.
// Height — shift/vecBits — grows O(log32 n) with vector length, so a Conj
// call that fills k tails costs O(k * log32 n), never O(n).
func vecPathBytes(shift uint) int64 {
	levels := int64(shift)/vecBits + 1
	return levels * VectorShallowBytes(vecBranch)
}

// buildVecTrieFromFlat drains flat into full vecBranch-element leaves and
// returns the resulting trie plus the remainder (fewer than vecBranch
// elements) as a pending tail. flat's length and vectorFlatThreshold are
// independent — a flat vector can hold many more than vecBranch elements by
// the time Conj first promotes it — so this always chunks in exact
// vecBranch strides rather than assuming flat already fits in one tail
// buffer. One-time O(len(flat)) cost, paid once per vector on its first
// Conj past the flat form.
func buildVecTrieFromFlat(flat []Value) (root *vecNode, shift uint, trieLen int, tail []Value) {
	full := len(flat) / vecBranch * vecBranch
	for i := 0; i < full; i += vecBranch {
		chunk := append([]Value(nil), flat[i:i+vecBranch]...)
		root, shift = pushVecTail(root, shift, trieLen, chunk)
		trieLen += vecBranch
	}
	tail = append([]Value(nil), flat[full:]...)
	return root, shift, trieLen, tail
}

// pushVecTail incorporates a full tail (vecBranch elements) as a new leaf,
// growing the tree by one level when root is already at capacity for
// shift. Returns the new root and its shift; every existing subtree not on
// the path to the new leaf is shared, not copied. Every caller must drain
// tail to exactly vecBranch first — a leaf is always full, so a short or
// empty tail here means an upstream chunking bug, not a valid partial leaf.
func pushVecTail(root *vecNode, shift uint, trieLen int, tail []Value) (*vecNode, uint) {
	if len(tail) != vecBranch {
		panic(fmt.Sprintf("pushVecTail: tail has %d elements, want exactly %d", len(tail), vecBranch))
	}
	leaf := &vecNode{vals: tail}
	if root == nil {
		return leaf, 0
	}
	if trieLen>>vecBits >= 1<<shift {
		return &vecNode{kids: []*vecNode{root, newVecPath(shift, leaf)}}, shift + vecBits
	}
	return pushVecLeaf(root, shift, trieLen, leaf), shift
}

// newVecPath builds a chain of single-child internal nodes from shift down
// to leaf — the shape a brand-new sibling subtree needs to match root's
// height when the tree grows a level.
func newVecPath(shift uint, leaf *vecNode) *vecNode {
	if shift == 0 {
		return leaf
	}
	return &vecNode{kids: []*vecNode{newVecPath(shift-vecBits, leaf)}}
}

// pushVecLeaf copies the path from node to the insertion point for leaf,
// appending a new child (or new subtree) at the next sequential slot and
// sharing every other subtree. Append-only growth guarantees the target
// slot is always either an existing child needing further descent, or
// exactly the next free one.
func pushVecLeaf(node *vecNode, shift uint, trieLen int, leaf *vecNode) *vecNode {
	idx := (trieLen >> shift) & (vecBranch - 1)
	kids := append([]*vecNode(nil), node.kids...)
	switch {
	case shift == vecBits:
		kids = append(kids, leaf)
	case idx < len(kids):
		kids[idx] = pushVecLeaf(kids[idx], shift-vecBits, trieLen, leaf)
	default:
		kids = append(kids, newVecPath(shift-vecBits, leaf))
	}
	return &vecNode{kids: kids}
}

// hashKey is the internal map key — disambiguates equal string representations
// across types (e.g. symbol "true" vs bool true). Numeric and bool keys are
// derived from their bit pattern, never formatted through strconv/fmt, so
// Get/Set/Assoc stay allocation-free on the hot path.
type hashKey struct {
	typ uint8
	num uint64
	str string
}

// Cross-type ordering group. Assigned in the old type-name strings' sort
// order (bool < float < int < keyword < nil < string < symbol) to minimize
// iteration-order churn from the rewrite.
const (
	hkBool uint8 = iota
	hkFloat
	hkInt
	hkKeyword
	hkNil
	hkString
	hkSymbol
)

// less orders hash keys deterministically: (typ, num, str). Within a numeric
// type this is bit-pattern order, not true numeric order — a negative Int or
// Float has its sign bit set, so e.g. -1 sorts after positive values. The
// spec only requires deterministic, evaluator-identical order, not true
// numeric order.
func (hk hashKey) less(other hashKey) bool {
	if hk.typ != other.typ {
		return hk.typ < other.typ
	}
	if hk.num != other.num {
		return hk.num < other.num
	}
	return hk.str < other.str
}

// negZeroBits is math.Float64bits(-0.0). toHashKey folds it to 0 so +0.0 and
// -0.0 hash to one key — key identity then matches Float.Equals, where
// 0.0 == -0.0. (The old string-formatted keys kept them distinct: "0" vs "-0".)
const negZeroBits = uint64(1) << 63

func toHashKey(v Value) (hashKey, error) {
	switch val := v.(type) {
	case Nil:
		return hashKey{typ: hkNil}, nil
	case Bool:
		var n uint64
		if val.V {
			n = 1
		}
		return hashKey{typ: hkBool, num: n}, nil
	case Int:
		return hashKey{typ: hkInt, num: uint64(val.V)}, nil
	case Float:
		bits := math.Float64bits(val.V)
		switch {
		case math.IsNaN(val.V):
			bits = math.Float64bits(math.NaN())
		case bits == negZeroBits:
			bits = 0
		}
		return hashKey{typ: hkFloat, num: bits}, nil
	case String:
		return hashKey{typ: hkString, str: val.V}, nil
	case Symbol:
		return hashKey{typ: hkSymbol, str: val.V}, nil
	case Keyword:
		return hashKey{typ: hkKeyword, str: val.V}, nil
	default:
		return hashKey{}, fmt.Errorf("unhashable type: %T", v)
	}
}

// entry is one key-value pair. It keeps the original key Value alongside its
// hashKey so both storage forms below can render and iterate without a
// second parallel map.
type entry struct {
	hk hashKey
	k  Value
	v  Value
}

// hashMapSmallLimit caps the sorted-slice form: Assoc/Set promote to the map
// form on the 9th distinct key. Frozen by BenchmarkHashMap_ScanVsMap — below
// this size, a linear scan beats a Go map lookup.
const hashMapSmallLimit = 8

// HashMap — immutable associative map. Keys must be comparable (Nil, Bool, Int,
// Float, String, Symbol, Keyword). Operations return new maps.
//
// Below hashMapSmallLimit distinct keys, entries holds them sorted by hashKey
// and Get is a linear scan — cheap at this size and already iteration-order.
// Past the limit, m takes over as storage. Promotion is one-way: a map that
// shrinks back below the limit through Dissoc stays in map form.
type HashMap struct {
	entries []entry
	m       map[hashKey]entry
}

func NewHashMap() *HashMap {
	return &HashMap{}
}

func (h *HashMap) Type() Keyword { return Keyword{V: "map"} }

// find locates hk in the sorted small-form entries, or the index it would
// need to be inserted at to keep entries sorted. Unused when h.m != nil.
func (h *HashMap) find(hk hashKey) (int, bool) {
	for i := range h.entries {
		if h.entries[i].hk == hk {
			return i, true
		}
		if hk.less(h.entries[i].hk) {
			return i, false
		}
	}
	return len(h.entries), false
}

func (h *HashMap) getByHashKey(hk hashKey) (Value, bool) {
	if h.m != nil {
		e, ok := h.m[hk]
		return e.v, ok
	}
	if i, ok := h.find(hk); ok {
		return h.entries[i].v, true
	}
	return nil, false
}

// eachRaw walks every entry in storage order — unsorted for the map form.
// For callers like Equals that only need membership, not display order.
func (h *HashMap) eachRaw(fn func(e entry)) {
	if h.m != nil {
		for _, e := range h.m {
			fn(e)
		}
		return
	}
	for _, e := range h.entries {
		fn(e)
	}
}

// sortedEntries returns every entry in deterministic (typ, num, str) order.
// The small form is already sorted and returned as-is; the map form re-sorts
// on each call, same cost as before the rewrite.
func (h *HashMap) sortedEntries() []entry {
	if h.m == nil {
		return h.entries
	}
	entries := make([]entry, 0, len(h.m))
	for _, e := range h.m {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].hk.less(entries[j].hk)
	})
	return entries
}

func (h *HashMap) String() string { return boundedString(h, 0) }

func (h *HashMap) Equals(o Value) bool { return boundedEquals(h, o, 0) }

// newMapFromEntries builds map-form storage from small-form entries plus one
// more, used when Assoc/Set crosses hashMapSmallLimit.
func newMapFromEntries(entries []entry, extra entry) map[hashKey]entry {
	m := make(map[hashKey]entry, len(entries)+1)
	for _, e := range entries {
		m[e.hk] = e
	}
	m[extra.hk] = extra
	return m
}

func (h *HashMap) Assoc(key, val Value) (*HashMap, error) {
	hk, err := toHashKey(key)
	if err != nil {
		return nil, err
	}
	e := entry{hk: hk, k: key, v: val}
	if h.m != nil {
		out := make(map[hashKey]entry, len(h.m)+1)
		maps.Copy(out, h.m)
		out[hk] = e
		return &HashMap{m: out}, nil
	}
	i, found := h.find(hk)
	if found {
		entries := make([]entry, len(h.entries))
		copy(entries, h.entries)
		entries[i] = e
		return &HashMap{entries: entries}, nil
	}
	if len(h.entries) >= hashMapSmallLimit {
		return &HashMap{m: newMapFromEntries(h.entries, e)}, nil
	}
	entries := make([]entry, len(h.entries)+1)
	copy(entries, h.entries[:i])
	entries[i] = e
	copy(entries[i+1:], h.entries[i:])
	return &HashMap{entries: entries}, nil
}

func (h *HashMap) Dissoc(key Value) (*HashMap, error) {
	hk, err := toHashKey(key)
	if err != nil {
		return nil, err
	}
	if h.m != nil {
		out := make(map[hashKey]entry, len(h.m))
		for k, e := range h.m {
			if k != hk {
				out[k] = e
			}
		}
		return &HashMap{m: out}, nil
	}
	entries := make([]entry, 0, len(h.entries))
	for _, e := range h.entries {
		if e.hk != hk {
			entries = append(entries, e)
		}
	}
	return &HashMap{entries: entries}, nil
}

func (h *HashMap) Get(key Value) (Value, bool) {
	hk, err := toHashKey(key)
	if err != nil {
		return nil, false
	}
	v, ok := h.getByHashKey(hk)
	if !ok {
		return Nil{}, false
	}
	return v, true
}

func (h *HashMap) Len() int {
	if h.m != nil {
		return len(h.m)
	}
	return len(h.entries)
}

// Set mutably inserts a key-value pair. It is an in-place escape hatch for
// building a fresh map before it is shared; callers holding a HashMap that may
// already be referenced elsewhere must use the copy-on-write Assoc/Dissoc
// instead to preserve immutability.
func (h *HashMap) Set(key, val Value) error {
	hk, err := toHashKey(key)
	if err != nil {
		return err
	}
	e := entry{hk: hk, k: key, v: val}
	if h.m != nil {
		h.m[hk] = e
		return nil
	}
	i, found := h.find(hk)
	if found {
		h.entries[i] = e
		return nil
	}
	if len(h.entries) >= hashMapSmallLimit {
		h.m = newMapFromEntries(h.entries, e)
		h.entries = nil
		return nil
	}
	h.entries = append(h.entries, entry{})
	copy(h.entries[i+1:], h.entries[i:len(h.entries)-1])
	h.entries[i] = e
	return nil
}

// Each calls fn for every key-value pair in the map, in deterministic order.
func (h *HashMap) Each(fn func(k, v Value)) {
	for _, e := range h.sortedEntries() {
		fn(e.k, e.v)
	}
}

// Pairs returns all key-value pairs as [2]Value arrays, in deterministic order.
func (h *HashMap) Pairs() [][2]Value {
	entries := h.sortedEntries()
	pairs := make([][2]Value, len(entries))
	for i, e := range entries {
		pairs[i] = [2]Value{e.k, e.v}
	}
	return pairs
}

// GoFunc — native Go function callable from Lisp.
// Receives context, the evaluator (for recursive eval), args, and the current env.
type GoFunc struct {
	Name string
	Fn   func(ctx context.Context, eval Evaluator, args []Value, env *Env) (Value, error)
}

func (g GoFunc) Type() Keyword { return Keyword{V: "fn"} }
func (g GoFunc) String() string {
	return "#<builtin:" + g.Name + ">"
}

func (g GoFunc) Equals(o Value) bool {
	v, ok := o.(GoFunc)
	return ok && g.Name == v.Name
}

// Lambda — user-defined closure.
type Lambda struct {
	Params   []Symbol
	Variadic Symbol // non-empty V = variadic; bound as List to remaining args
	Body     []Value
	Env      *Env
	Name     string // optional, enables self-recursion by name
}

func (l Lambda) Type() Keyword       { return Keyword{V: "fn"} }
func (l Lambda) String() string      { return boundedString(l, 0) }
func (l Lambda) Equals(o Value) bool { return false }

// Macro — syntax transformer; body receives unevaluated forms.
type Macro struct {
	Params   []Symbol
	Variadic Symbol
	Body     []Value
	Env      *Env
	Name     string
}

func (m Macro) Type() Keyword       { return Keyword{V: "macro"} }
func (m Macro) String() string      { return boundedString(m, 0) }
func (m Macro) Equals(o Value) bool { return false }

// IsTruthy returns true for all values except Nil and false.
func IsTruthy(v Value) bool {
	switch val := v.(type) {
	case Nil:
		return false
	case Bool:
		return val.V
	default:
		return true
	}
}

// FromGoValue converts a native Go value to a Lisp Value.
func FromGoValue(v any) (Value, error) {
	switch val := v.(type) {
	case nil:
		return Nil{}, nil
	case bool:
		return Bool{V: val}, nil
	case int:
		return Int{V: int64(val)}, nil
	case int64:
		return Int{V: val}, nil
	case float64:
		return Float{V: val}, nil
	case string:
		return String{V: val}, nil
	case []any:
		items := make([]Value, len(val))
		for i, item := range val {
			v, err := FromGoValue(item)
			if err != nil {
				return nil, err
			}
			items[i] = v
		}
		return NewVector(items), nil
	case map[string]any:
		m := NewHashMap()
		var err error
		for k, v := range val {
			value, ferr := FromGoValue(v)
			if ferr != nil {
				return nil, ferr
			}
			m, err = m.Assoc(Keyword{V: k}, value)
			if err != nil {
				return nil, err
			}
		}
		return m, nil
	default:
		return nil, fmt.Errorf("unsupported Go type: %T", v)
	}
}

// ToGoValue converts a Lisp Value to a native Go value.
func ToGoValue(v Value) (any, error) {
	switch val := v.(type) {
	case Nil:
		return nil, nil
	case Bool:
		return val.V, nil
	case Int:
		return val.V, nil
	case Float:
		return val.V, nil
	case String:
		return val.V, nil
	case Keyword:
		return val.V, nil
	case Symbol:
		return val.V, nil
	case Vector:
		items := val.ToSlice()
		result := make([]any, len(items))
		for i, item := range items {
			v, err := ToGoValue(item)
			if err != nil {
				return nil, err
			}
			result[i] = v
		}
		return result, nil
	case List:
		items := val.ToSlice()
		result := make([]any, len(items))
		for i, item := range items {
			v, err := ToGoValue(item)
			if err != nil {
				return nil, err
			}
			result[i] = v
		}
		return result, nil
	case *HashMap:
		result := make(map[string]any)
		var convErr error
		val.Each(func(k, v Value) {
			if convErr != nil {
				return
			}
			keyVal, err := ToGoValue(k)
			if err != nil {
				convErr = err
				return
			}
			keyStr, ok := keyVal.(string)
			if !ok {
				convErr = fmt.Errorf("map key must convert to string, got %T", keyVal)
				return
			}
			value, err := ToGoValue(v)
			if err != nil {
				convErr = err
				return
			}
			result[keyStr] = value
		})
		if convErr != nil {
			return nil, convErr
		}
		return result, nil
	default:
		return nil, fmt.Errorf("unsupported Lisp type: %T", v)
	}
}
