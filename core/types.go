package core

import (
	"context"
	"fmt"
	"math"
	"math/bits"
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
// The threshold promotion is not cosmetic and is not symmetric with
// NewVector by accident: it keeps Cons O(1) on a bulk-built list, which
// core-engine's sequence-extension bound requires. Building flat and
// promoting on first Cons instead would charge that call the whole list.
// NewVector faces the opposite constraint — indexed reads on a Vector must
// stay effectively constant-time — so it stays flat.
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

// hashMapSmallLimit caps the sorted-slice form: Assoc/Set promote to the trie
// form on the 9th distinct key. Frozen by BenchmarkHashMap_ScanVsMap — below
// this size, a linear scan beats a hashed lookup.
const hashMapSmallLimit = 8

// hashOfKey hashes a key with FNV-1a over fixed constants. The seed must not
// vary per process: a randomized seed (hash/maphash) would give identical input
// a different trie shape on every run, making failures unreproducible and
// contradicting the determinism core-engine requires. The 64-to-32 fold mixes
// the high half down, because the top levels of the trie discriminate on the
// low bits.
func hashOfKey(hk hashKey) uint32 {
	const (
		fnvOffset uint64 = 14695981039346656037
		fnvPrime  uint64 = 1099511628211
	)
	h := fnvOffset
	h = (h ^ uint64(hk.typ)) * fnvPrime
	for n := hk.num; ; n >>= 8 {
		h = (h ^ (n & 0xff)) * fnvPrime
		if n < 0x100 {
			break
		}
	}
	for i := range len(hk.str) {
		h = (h ^ uint64(hk.str[i])) * fnvPrime
	}
	return uint32(h ^ (h >> 32))
}

// hamtNode is one level of the persistent trie backing the large map form.
// dataMap and nodeMap are disjoint: a slot holds either an entry or a child,
// never both. Both slices are compacted, so slot f's payload lives at the
// population count of the bits below it — len(entries) == OnesCount32(dataMap)
// and len(children) == OnesCount32(nodeMap).
//
// It reuses Vector's trie geometry (vecBits/vecBranch) rather than declaring
// its own copy of 5 and 32; two names for one number is how they drift apart.
type hamtNode struct {
	dataMap  uint32
	nodeMap  uint32
	entries  []entry
	children []*hamtNode
}

// hamtNodeBytes approximates one node's allocation for the ledger, on the same
// arch-independent basis as the Meter* constants.
func hamtNodeBytes(n *hamtNode) int64 {
	return MeterCollectionHeaderBytes +
		int64(len(n.entries))*MeterHashMapEntryBytes +
		int64(len(n.children))*MeterTrieChildBytes
}

// isCollision reports whether n stores its entries as a flat scanned list
// because the trie ran out of hash bits to discriminate on. A node with
// neither bitmap set holds nothing in the ordinary layout, so entries being
// non-empty is unambiguous.
func (n *hamtNode) isCollision() bool {
	return n.dataMap == 0 && n.nodeMap == 0 && len(n.entries) > 0
}

func (n *hamtNode) clone() *hamtNode {
	out := &hamtNode{dataMap: n.dataMap, nodeMap: n.nodeMap}
	out.entries = append([]entry(nil), n.entries...)
	out.children = append([]*hamtNode(nil), n.children...)
	return out
}

func (n *hamtNode) get(hk hashKey, h uint32, shift uint) (Value, bool) {
	if n.isCollision() {
		for i := range n.entries {
			if n.entries[i].hk == hk {
				return n.entries[i].v, true
			}
		}
		return nil, false
	}
	bit := uint32(1) << ((h >> shift) & (vecBranch - 1))
	switch {
	case n.dataMap&bit != 0:
		e := n.entries[bits.OnesCount32(n.dataMap&(bit-1))]
		if e.hk == hk {
			return e.v, true
		}
		return nil, false
	case n.nodeMap&bit != 0:
		return n.children[bits.OnesCount32(n.nodeMap&(bit-1))].get(hk, h, shift+vecBits)
	}
	return nil, false
}

// mergeEntries builds the subtree holding two entries with distinct hash keys.
// Past shift 32 there are no bits left to tell them apart — `h >> 35` on a
// uint32 is 0 in Go, so descending further would recurse forever — and the
// pair becomes a collision node.
func mergeEntries(a entry, ha uint32, b entry, hb uint32, shift uint) *hamtNode {
	if shift >= 32 {
		return &hamtNode{entries: []entry{a, b}}
	}
	fa := (ha >> shift) & (vecBranch - 1)
	fb := (hb >> shift) & (vecBranch - 1)
	if fa == fb {
		return &hamtNode{
			nodeMap:  uint32(1) << fa,
			children: []*hamtNode{mergeEntries(a, ha, b, hb, shift+vecBits)},
		}
	}
	out := &hamtNode{dataMap: (uint32(1) << fa) | (uint32(1) << fb)}
	if fa < fb {
		out.entries = []entry{a, b}
	} else {
		out.entries = []entry{b, a}
	}
	return out
}

// assoc returns the node with e inserted, the bytes the insert allocated, and
// whether e introduced a key the subtree did not already hold. Untouched
// children are shared with the receiver; only the path down to e is copied.
func (n *hamtNode) assoc(e entry, h uint32, shift uint) (*hamtNode, int64, bool) {
	if n.isCollision() {
		for i := range n.entries {
			if n.entries[i].hk == e.hk {
				out := &hamtNode{entries: append([]entry(nil), n.entries...)}
				out.entries[i] = e
				return out, hamtNodeBytes(out), false
			}
		}
		out := &hamtNode{entries: append(append([]entry(nil), n.entries...), e)}
		return out, hamtNodeBytes(out), true
	}

	bit := uint32(1) << ((h >> shift) & (vecBranch - 1))
	switch {
	case n.dataMap&bit != 0:
		idx := bits.OnesCount32(n.dataMap & (bit - 1))
		existing := n.entries[idx]
		if existing.hk == e.hk {
			out := n.clone()
			out.entries[idx] = e
			return out, hamtNodeBytes(out), false
		}
		child := mergeEntries(existing, hashOfKey(existing.hk), e, h, shift+vecBits)
		cidx := bits.OnesCount32(n.nodeMap & (bit - 1))
		out := &hamtNode{dataMap: n.dataMap &^ bit, nodeMap: n.nodeMap | bit}
		out.entries = append(out.entries, n.entries[:idx]...)
		out.entries = append(out.entries, n.entries[idx+1:]...)
		out.children = append(out.children, n.children[:cidx]...)
		out.children = append(out.children, child)
		out.children = append(out.children, n.children[cidx:]...)
		return out, hamtNodeBytes(out) + hamtNodeBytes(child), true
	case n.nodeMap&bit != 0:
		cidx := bits.OnesCount32(n.nodeMap & (bit - 1))
		child, bytes, added := n.children[cidx].assoc(e, h, shift+vecBits)
		out := n.clone()
		out.children[cidx] = child
		return out, bytes + hamtNodeBytes(out), added
	default:
		idx := bits.OnesCount32(n.dataMap & (bit - 1))
		out := &hamtNode{dataMap: n.dataMap | bit, nodeMap: n.nodeMap, children: n.children}
		out.entries = append(out.entries, n.entries[:idx]...)
		out.entries = append(out.entries, e)
		out.entries = append(out.entries, n.entries[idx:]...)
		return out, hamtNodeBytes(out), true
	}
}

// dissoc returns the node without hk, the bytes the rebuild allocated, and
// whether hk was present. A nil node means the subtree is now empty and the
// caller drops it, so repeated removal cannot leave dead nodes behind.
func (n *hamtNode) dissoc(hk hashKey, h uint32, shift uint) (*hamtNode, int64, bool) {
	if n.isCollision() {
		for i := range n.entries {
			if n.entries[i].hk != hk {
				continue
			}
			if len(n.entries) == 1 {
				return nil, 0, true
			}
			out := &hamtNode{}
			out.entries = append(out.entries, n.entries[:i]...)
			out.entries = append(out.entries, n.entries[i+1:]...)
			return out, hamtNodeBytes(out), true
		}
		return n, 0, false
	}

	bit := uint32(1) << ((h >> shift) & (vecBranch - 1))
	switch {
	case n.dataMap&bit != 0:
		idx := bits.OnesCount32(n.dataMap & (bit - 1))
		if n.entries[idx].hk != hk {
			return n, 0, false
		}
		if n.dataMap == bit && n.nodeMap == 0 {
			return nil, 0, true
		}
		out := &hamtNode{dataMap: n.dataMap &^ bit, nodeMap: n.nodeMap, children: n.children}
		out.entries = append(out.entries, n.entries[:idx]...)
		out.entries = append(out.entries, n.entries[idx+1:]...)
		return out, hamtNodeBytes(out), true
	case n.nodeMap&bit != 0:
		cidx := bits.OnesCount32(n.nodeMap & (bit - 1))
		child, bytes, removed := n.children[cidx].dissoc(hk, h, shift+vecBits)
		if !removed {
			return n, 0, false
		}
		if child != nil {
			out := n.clone()
			out.children[cidx] = child
			return out, bytes + hamtNodeBytes(out), true
		}
		if n.dataMap == 0 && n.nodeMap == bit {
			return nil, 0, true
		}
		out := &hamtNode{dataMap: n.dataMap, nodeMap: n.nodeMap &^ bit, entries: n.entries}
		out.children = append(out.children, n.children[:cidx]...)
		out.children = append(out.children, n.children[cidx+1:]...)
		return out, hamtNodeBytes(out), true
	}
	return n, 0, false
}

// each walks every entry in trie order — unsorted, for callers that need
// membership rather than display order.
func (n *hamtNode) each(fn func(e entry)) {
	for i := range n.entries {
		fn(n.entries[i])
	}
	for _, c := range n.children {
		c.each(fn)
	}
}

// HashMap — immutable associative map. Keys must be comparable (Nil, Bool, Int,
// Float, String, Symbol, Keyword). Operations return new maps.
//
// It has three storage forms, exactly one of which is active.
//
// entries — at or below hashMapSmallLimit distinct keys: sorted by hashKey,
// Get is a linear scan, cheap at this size and already in iteration order.
//
// large.m — a plain Go map, reached only by growing past the limit through the
// mutable Set escape hatch. Set is the bulk-construction path (map literals,
// hash-map, merge, OpMakeMap, json/decode), where an in-place map assignment
// is O(1) and nothing is shared yet.
//
// large.root — a persistent trie, produced by Assoc and Dissoc. Copying one
// root-to-leaf path and sharing the rest is what keeps a chained assoc linear
// instead of quadratic. A map still in large.m form converts once, on its
// first Assoc; from there the chain stays in trie form.
//
// Promotion is one-way at both steps: a map that shrinks back below the limit
// through Dissoc stays in trie form.
//
// The large-form state sits behind one pointer rather than inline so that a
// small map — the overwhelmingly common shape, and the only one the gold set
// builds — stays exactly as wide as it was before the trie existed. Inlining
// the fields cost every map literal 16 bytes it never reads.
type HashMap struct {
	entries []entry
	large   *largeMap
}

// largeMap holds whichever large form is active. count tracks the entry total
// for the trie, because counting one is not O(1).
type largeMap struct {
	m     map[hashKey]entry
	root  *hamtNode
	count int
}

func NewHashMap() *HashMap {
	return &HashMap{}
}

func (h *HashMap) Type() Keyword { return Keyword{V: "map"} }

// find locates hk in the sorted small-form entries, or the index it would
// need to be inserted at to keep entries sorted. Unused when h.root != nil.
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
	if h.large != nil {
		if h.large.root != nil {
			return h.large.root.get(hk, hashOfKey(hk), 0)
		}
		e, ok := h.large.m[hk]
		return e.v, ok
	}
	if i, ok := h.find(hk); ok {
		return h.entries[i].v, true
	}
	return nil, false
}

// eachRaw walks every entry in storage order — unsorted for the trie form.
// For callers like Equals that only need membership, not display order.
func (h *HashMap) eachRaw(fn func(e entry)) {
	if h.large != nil {
		if h.large.root != nil {
			h.large.root.each(fn)
			return
		}
		for _, e := range h.large.m {
			fn(e)
		}
		return
	}
	for _, e := range h.entries {
		fn(e)
	}
}

// sortedEntries returns every entry in deterministic (typ, num, str) order.
// The small form is already sorted and returned as-is; the trie form re-sorts
// on each call, same cost as before the rewrite.
func (h *HashMap) sortedEntries() []entry {
	if h.large == nil {
		return h.entries
	}
	entries := make([]entry, 0, h.Len())
	h.eachRaw(func(e entry) { entries = append(entries, e) })
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].hk.less(entries[j].hk)
	})
	return entries
}

func (h *HashMap) String() string { return boundedString(h, 0) }

func (h *HashMap) Equals(o Value) bool { return boundedEquals(h, o, 0) }

// newTrieFromEntries builds trie-form storage from small-form entries plus one
// more, used when Assoc/Set crosses hashMapSmallLimit. Returns the root and the
// bytes it allocated.
func newTrieFromEntries(entries []entry, extra entry) (*hamtNode, int64) {
	root := &hamtNode{}
	var bytes int64
	add := func(e entry) {
		next, b, _ := root.assoc(e, hashOfKey(e.hk), 0)
		root, bytes = next, bytes+b
	}
	for _, e := range entries {
		add(e)
	}
	add(extra)
	return root, bytes
}

// trieFromBuildMap converts Set-built staging storage into trie form. Called
// once, on the first Assoc or Dissoc after bulk construction: the builder path
// stays O(1) per Set, and the persistent path gets structural sharing from
// there on. The receiver is left untouched, so no map is mutated behind a
// caller's back and concurrent readers of h are unaffected.
func (h *HashMap) trieFromBuildMap() (*hamtNode, int64) {
	root := &hamtNode{}
	var bytes int64
	for _, e := range h.large.m {
		next, b, _ := root.assoc(e, hashOfKey(e.hk), 0)
		root, bytes = next, bytes+b
	}
	return root, bytes
}

// newTrie wraps trie storage as a large-form map.
func newTrie(root *hamtNode, count int) *HashMap {
	return &HashMap{large: &largeMap{root: root, count: count}}
}

// Assoc returns the map with key bound to val, plus the bytes the update
// allocated. In trie form only the path to the key is copied, so the charge is
// bounded by the trie's depth rather than by the map's size — a caller
// assoc'ing onto an already-large map is billed roughly the same as one
// assoc'ing onto a small one.
func (h *HashMap) Assoc(key, val Value) (*HashMap, int64, error) {
	hk, err := toHashKey(key)
	if err != nil {
		return nil, 0, err
	}
	e := entry{hk: hk, k: key, v: val}
	if h.large != nil {
		root, bytes := h.large.root, int64(0)
		count := h.large.count
		if root == nil {
			root, bytes = h.trieFromBuildMap()
			count = len(h.large.m)
		}
		next, b, added := root.assoc(e, hashOfKey(hk), 0)
		if added {
			count++
		}
		return newTrie(next, count), bytes + b, nil
	}
	i, found := h.find(hk)
	if found {
		entries := make([]entry, len(h.entries))
		copy(entries, h.entries)
		entries[i] = e
		return &HashMap{entries: entries}, HashMapShallowBytes(len(entries)), nil
	}
	if len(h.entries) >= hashMapSmallLimit {
		root, bytes := newTrieFromEntries(h.entries, e)
		return newTrie(root, len(h.entries)+1), bytes, nil
	}
	entries := make([]entry, len(h.entries)+1)
	copy(entries, h.entries[:i])
	entries[i] = e
	copy(entries[i+1:], h.entries[i:])
	return &HashMap{entries: entries}, HashMapShallowBytes(len(entries)), nil
}

// Dissoc returns the map without key, plus the bytes the update allocated.
func (h *HashMap) Dissoc(key Value) (*HashMap, int64, error) {
	hk, err := toHashKey(key)
	if err != nil {
		return nil, 0, err
	}
	if h.large != nil {
		root, bytes := h.large.root, int64(0)
		count := h.large.count
		if root == nil {
			root, bytes = h.trieFromBuildMap()
			count = len(h.large.m)
		}
		next, b, removed := root.dissoc(hk, hashOfKey(hk), 0)
		if removed {
			count--
		}
		return newTrie(next, count), bytes + b, nil
	}
	entries := make([]entry, 0, len(h.entries))
	for _, e := range h.entries {
		if e.hk != hk {
			entries = append(entries, e)
		}
	}
	return &HashMap{entries: entries}, HashMapShallowBytes(len(entries)), nil
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
	if h.large != nil {
		if h.large.root != nil {
			return h.large.count
		}
		return len(h.large.m)
	}
	return len(h.entries)
}

// Set mutably inserts a key-value pair. It is an in-place escape hatch for
// building a fresh map before it is shared; callers holding a HashMap that may
// already be referenced elsewhere must use the copy-on-write Assoc/Dissoc
// instead to preserve immutability.
//
// Growing past hashMapSmallLimit it builds into a plain Go map, where an
// insert is O(1) and nothing is shared yet — bulk construction is the reason
// this method exists. Only a map already converted to trie form pays the
// path-copying insert, which happens when a caller Sets after having derived
// something through Assoc. Mutating trie nodes in place is not an option
// there: they may be shared with the derived map, and telling that case apart
// would need an ownership flag cleared on the receiver, which is a write to a
// value shared across goroutines.
func (h *HashMap) Set(key, val Value) error {
	hk, err := toHashKey(key)
	if err != nil {
		return err
	}
	e := entry{hk: hk, k: key, v: val}
	if h.large != nil {
		if h.large.root != nil {
			root, _, added := h.large.root.assoc(e, hashOfKey(hk), 0)
			h.large.root = root
			if added {
				h.large.count++
			}
			return nil
		}
		h.large.m[hk] = e
		return nil
	}
	i, found := h.find(hk)
	if found {
		h.entries[i] = e
		return nil
	}
	if len(h.entries) >= hashMapSmallLimit {
		m := make(map[hashKey]entry, len(h.entries)+1)
		for _, existing := range h.entries {
			m[existing.hk] = existing
		}
		m[hk] = e
		h.large = &largeMap{m: m}
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
			m, _, err = m.Assoc(Keyword{V: k}, value)
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
