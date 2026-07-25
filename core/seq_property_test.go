package core_test

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
)

// seqPropertySeed pins the random-op-sequence PRNG so a failure reproduces
// deterministically from this constant alone.
const seqPropertySeed = 20260725

// testListFlatThreshold and testVectorFlatThreshold mirror core's private
// promotion thresholds (both 32 today, kept as separate names since they
// govern different types); testVecBranch mirrors the trie's branching
// factor (also 32). If core retunes any of these, these must follow.
const (
	testListFlatThreshold   = 32
	testVectorFlatThreshold = 32
	testVecBranch           = 32
)

// wantConsBytes is List.Cons's exact newly-allocated byte cost, given the
// list's length and shared-ness before the call — the same three branches
// core.List.Cons itself switches on: already shared (one new node),
// still-flat-after (a bounded whole-backing copy), or promoting across the
// threshold (a bounded one-time chain build). None of the three scales with
// how long the list has grown through earlier Cons calls.
func wantConsBytes(prevLen int, wasShared bool) int64 {
	newLen := prevLen + 1
	nodeBytes := core.ListShallowBytes(1)
	switch {
	case wasShared:
		return nodeBytes
	case newLen <= testListFlatThreshold:
		return core.ListShallowBytes(newLen)
	default:
		return nodeBytes * int64(newLen)
	}
}

// wantConjBytesFreshFlat is Vector.Conj's exact newly-allocated byte cost
// for a single value conj'd onto a vector built directly via NewVector —
// always flat regardless of length, the shape testBoundaryLength exercises.
// Below the threshold it's a bounded whole-backing copy; above it, a
// one-time promotion of the existing flat backing plus appending the one
// new value to whatever tail remainder that promotion leaves (never a
// trie push, since a single append can't fill a tail from scratch).
func wantConjBytesFreshFlat(n int) int64 {
	newLen := n + 1
	if newLen <= testVectorFlatThreshold {
		return core.VectorShallowBytes(newLen)
	}
	finalTailLen := n%testVecBranch + 1
	return core.VectorShallowBytes(n) + core.VectorShallowBytes(finalTailLen)
}

// maxVecPathLevels safely bounds a persistent-vector trie's height (in
// vecBits-sized steps) for any length within the stdlib collection limit
// (10_000_000): log32(10_000_000) < 5, so 8 is a generous margin, not a
// tight one.
const maxVecPathLevels = 8

// conjBytesUpperBound bounds Vector.Conj's newly-allocated bytes, computed
// from the operation alone, never from vec.Len(): mayPromote covers the
// one-time flat-to-trie promotion (a real O(prevLen) copy, paid once per
// rebuild — see wantConjBytesFreshFlat for its exact shape), and steady
// covers every push after that, bounded by trie height, which grows
// O(log32 n) and stays tiny for any length this walk reaches. Used instead
// of an exact formula because the random walk can chain many Conj calls
// between rebuilds, and reproducing the trie's shift state exactly would
// mean re-deriving core's own private growth algorithm here.
func conjBytesUpperBound(prevLen int, mayPromote bool) int64 {
	steady := int64(maxVecPathLevels+1) * core.VectorShallowBytes(testVecBranch)
	if mayPromote {
		return core.VectorShallowBytes(prevLen) + steady
	}
	return steady
}

// TestSequenceOperationsAgainstReference drives List and Vector through
// random operation sequences alongside a plain []core.Value reference model,
// asserting every observable (Len, At, iteration order, Equals, String)
// stays in lockstep. It also checks lengths that straddle 32 explicitly,
// since a later promotion threshold at that boundary must stay invisible.
// This is the oracle every subsequent representation change keeps green.
func TestSequenceOperationsAgainstReference(t *testing.T) {
	t.Run("list random walk", func(t *testing.T) {
		testListRandomWalk(t, rand.New(rand.NewSource(seqPropertySeed)), 5000)
	})
	t.Run("vector random walk", func(t *testing.T) {
		testVectorRandomWalk(t, rand.New(rand.NewSource(seqPropertySeed+1)), 5000)
	})
	for _, n := range []int{0, 1, 31, 32, 33, 64, 1000} {
		t.Run(fmt.Sprintf("boundary length %d", n), func(t *testing.T) {
			testBoundaryLength(t, n)
		})
	}
}

func testListRandomWalk(t *testing.T, rng *rand.Rand, ops int) {
	t.Helper()
	const maxSize = 800

	var ref []core.Value
	lst := core.List{}
	shared := false // mirrors whether lst currently holds core's private shared-tail representation

	for i := range ops {
		op := rng.Intn(10)
		switch {
		case len(ref) == 0 || (len(ref) < maxSize && op < 6):
			v := core.Int{V: int64(rng.Intn(1000))}
			ref = append([]core.Value{v}, ref...)
			prevLen := lst.Len()
			var bytes int64
			lst, bytes = lst.Cons(v)
			if want := wantConsBytes(prevLen, shared); bytes != want {
				t.Fatalf("op %d: Cons byte cost = %d, want %d", i, bytes, want)
			}
			shared = shared || prevLen+1 > testListFlatThreshold
		case op < 8 || len(ref) >= maxSize:
			ref = ref[1:]
			lst = lst.Rest()
			if len(ref) == 0 {
				// Rest of a one-node shared list returns the canonical
				// empty List{} — flat again, the one point where the
				// no-demotion rule doesn't apply.
				shared = false
			}
		default:
			lst = core.NewList(append([]core.Value(nil), ref...))
			shared = len(ref) > testListFlatThreshold
		}
		assertListMatchesRef(t, i, lst, ref)
	}
}

func testVectorRandomWalk(t *testing.T, rng *rand.Rand, ops int) {
	t.Helper()
	const maxSize = 800

	var ref []core.Value
	vec := core.Vector{}
	justFlat := true // vec is in its NewVector-produced flat state; the next Conj may pay a one-time promotion charge

	for i := range ops {
		op := rng.Intn(10)
		switch {
		case len(ref) < maxSize && op < 8:
			n := 1 + rng.Intn(3)
			vs := make([]core.Value, n)
			for j := range vs {
				vs[j] = core.Int{V: int64(rng.Intn(1000))}
			}
			ref = append(ref, vs...)
			prevLen := vec.Len()
			var bytes int64
			vec, bytes = vec.Conj(vs...)
			if bytes <= 0 {
				t.Fatalf("op %d: Conj byte cost = %d, want > 0", i, bytes)
			}
			if want := conjBytesUpperBound(prevLen, justFlat); bytes > want {
				t.Fatalf("op %d: Conj byte cost = %d, want <= %d (must not scale with vec.Len() past one promotion)", i, bytes, want)
			}
			justFlat = false
		default:
			k := rng.Intn(len(ref) + 1)
			ref = append([]core.Value(nil), ref[:k]...)
			vec = core.NewVector(append([]core.Value(nil), ref...))
			justFlat = true
		}
		assertVectorMatchesRef(t, i, vec, ref)
	}
}

// testBoundaryLength builds a List and a Vector of exactly n elements via
// construction-from-slice, then exercises Cons/Rest/Conj and the
// stdlib-level operations at that exact length.
func testBoundaryLength(t *testing.T, n int) {
	t.Helper()
	ref := make([]core.Value, n)
	for i := range ref {
		ref[i] = core.Int{V: int64(i)}
	}

	lst := core.NewList(append([]core.Value(nil), ref...))
	vec := core.NewVector(append([]core.Value(nil), ref...))
	assertListMatchesRef(t, -1, lst, ref)
	assertVectorMatchesRef(t, -1, vec, ref)

	consVal := core.Int{V: -1}
	newLst, bytes := lst.Cons(consVal)
	if newLst.Len() != n+1 {
		t.Fatalf("Cons: Len() = %d, want %d", newLst.Len(), n+1)
	}
	if want := wantConsBytes(n, n > testListFlatThreshold); bytes != want {
		t.Fatalf("Cons: byte cost = %d, want %d", bytes, want)
	}
	if !newLst.At(0).Equals(consVal) {
		t.Fatalf("Cons: At(0) = %v, want %v", newLst.At(0), consVal)
	}
	for i, want := range ref {
		if got := newLst.At(i + 1); !got.Equals(want) {
			t.Fatalf("Cons: At(%d) = %v, want %v", i+1, got, want)
		}
	}

	restLst := lst.Rest()
	wantRestLen := max(n-1, 0)
	if restLst.Len() != wantRestLen {
		t.Fatalf("Rest: Len() = %d, want %d", restLst.Len(), wantRestLen)
	}
	if n > 1 {
		assertListMatchesRef(t, -1, restLst, ref[1:])
	}

	appendVal := core.Int{V: -1}
	newVec, vbytes := vec.Conj(appendVal)
	if newVec.Len() != n+1 {
		t.Fatalf("Conj: Len() = %d, want %d", newVec.Len(), n+1)
	}
	if want := wantConjBytesFreshFlat(n); vbytes != want {
		t.Fatalf("Conj: byte cost = %d, want %d", vbytes, want)
	}
	if !newVec.At(n).Equals(appendVal) {
		t.Fatalf("Conj: At(%d) = %v, want %v", n, newVec.At(n), appendVal)
	}
	for i, want := range ref {
		if got := newVec.At(i); !got.Equals(want) {
			t.Fatalf("Conj: At(%d) = %v, want %v", i, got, want)
		}
	}

	testStdlibParity(t, ref)
}

// testStdlibParity drives concat/reverse/nth/count/= through a real engine
// over a list built from ref, checking each result against the same
// operation applied to ref directly.
func testStdlibParity(t *testing.T, ref []core.Value) {
	t.Helper()
	env := core.NewEnv(nil)
	if err := stdlib.New().Init(env); err != nil {
		t.Fatalf("stdlib init: %v", err)
	}
	ev := core.NewEvaluator()
	ctx := context.Background()

	var src strings.Builder
	src.WriteString("(def xs (list")
	for _, v := range ref {
		src.WriteByte(' ')
		src.WriteString(v.String())
	}
	src.WriteString("))")
	evalSource(t, ev, env, ctx, src.String())

	n := len(ref)

	count := evalSource(t, ev, env, ctx, "(count xs)")
	if want := (core.Int{V: int64(n)}); !count.Equals(want) {
		t.Fatalf("(count xs) = %v, want %v", count, want)
	}

	reversed := evalSource(t, ev, env, ctx, "(reverse xs)")
	revLst, ok := reversed.(core.List)
	if !ok {
		t.Fatalf("(reverse xs) returned %T, want core.List", reversed)
	}
	if revLst.Len() != n {
		t.Fatalf("(reverse xs) len = %d, want %d", revLst.Len(), n)
	}
	for i := range n {
		if !revLst.At(i).Equals(ref[n-1-i]) {
			t.Fatalf("(reverse xs)[%d] = %v, want %v", i, revLst.At(i), ref[n-1-i])
		}
	}

	if n > 0 {
		first := evalSource(t, ev, env, ctx, "(nth xs 0)")
		if !first.Equals(ref[0]) {
			t.Fatalf("(nth xs 0) = %v, want %v", first, ref[0])
		}
		last := evalSource(t, ev, env, ctx, fmt.Sprintf("(nth xs %d)", n-1))
		if !last.Equals(ref[n-1]) {
			t.Fatalf("(nth xs %d) = %v, want %v", n-1, last, ref[n-1])
		}
	}

	concatRes := evalSource(t, ev, env, ctx, "(concat xs (list 999999))")
	concatLst, ok := concatRes.(core.List)
	if !ok {
		t.Fatalf("(concat xs (list 999999)) returned %T, want core.List", concatRes)
	}
	if concatLst.Len() != n+1 {
		t.Fatalf("(concat xs (list 999999)) len = %d, want %d", concatLst.Len(), n+1)
	}
	if !concatLst.At(n).Equals(core.Int{V: 999999}) {
		t.Fatalf("(concat xs (list 999999))[%d] = %v, want 999999", n, concatLst.At(n))
	}

	eq := evalSource(t, ev, env, ctx, "(= xs xs)")
	if !eq.Equals(core.BoxBool(true)) {
		t.Fatalf("(= xs xs) = %v, want true", eq)
	}
}

func evalSource(t *testing.T, ev core.Evaluator, env *core.Env, ctx context.Context, src string) core.Value {
	t.Helper()
	forms, err := core.Read(src)
	if err != nil {
		t.Fatalf("read %q: %v", src, err)
	}
	var result core.Value = core.Nil{}
	for _, form := range forms {
		result, err = ev.Eval(ctx, form, env)
		if err != nil {
			t.Fatalf("eval %q: %v", src, err)
		}
	}
	return result
}

// assertListMatchesRef checks every observable of lst against ref. step
// identifies the random-walk iteration in failure messages, or -1 for a
// one-shot check outside a walk.
func assertListMatchesRef(t *testing.T, step int, lst core.List, ref []core.Value) {
	t.Helper()
	if lst.Len() != len(ref) {
		t.Fatalf("op %d: Len() = %d, want %d", step, lst.Len(), len(ref))
	}
	for i, want := range ref {
		if got := lst.At(i); !got.Equals(want) {
			t.Fatalf("op %d: At(%d) = %v, want %v", step, i, got, want)
		}
	}
	got := lst.ToSlice()
	if len(got) != len(ref) {
		t.Fatalf("op %d: ToSlice() len = %d, want %d", step, len(got), len(ref))
	}
	for i := range ref {
		if !got[i].Equals(ref[i]) {
			t.Fatalf("op %d: ToSlice()[%d] = %v, want %v", step, i, got[i], ref[i])
		}
	}
	refList := core.NewList(append([]core.Value(nil), ref...))
	if !lst.Equals(refList) {
		t.Fatalf("op %d: Equals mismatch, lst=%s ref=%s", step, lst.String(), refList.String())
	}
	if lst.String() != refList.String() {
		t.Fatalf("op %d: String() = %q, want %q", step, lst.String(), refList.String())
	}
}

// TestListFlatSharedEquivalence builds the same logical list two ways — a
// native flat list at or below the promotion threshold, and a list built
// well above the threshold then reduced back down via Rest — and asserts
// every observable agrees. List has no demotion, so the reduced list stays
// on its shared-tail representation even once its length matches the flat
// one; this is exactly the case where representation could otherwise leak
// through Len, At, Equals, String, or ToSlice.
func TestListFlatSharedEquivalence(t *testing.T) {
	const n, tail = 1000, 20
	items := make([]core.Value, n)
	for i := range items {
		items[i] = core.Int{V: int64(i)}
	}

	flat := core.NewList(append([]core.Value(nil), items[n-tail:]...))
	shared := core.NewList(append([]core.Value(nil), items...))
	for shared.Len() > tail {
		shared = shared.Rest()
	}

	if flat.Len() != tail || shared.Len() != tail {
		t.Fatalf("Len() = flat:%d shared:%d, want both %d", flat.Len(), shared.Len(), tail)
	}
	for i := 0; i < tail; i++ {
		if !flat.At(i).Equals(shared.At(i)) {
			t.Fatalf("At(%d) = flat:%v shared:%v, want equal", i, flat.At(i), shared.At(i))
		}
	}
	if !flat.Equals(shared) || !shared.Equals(flat) {
		t.Fatalf("Equals mismatch: flat=%s shared=%s", flat.String(), shared.String())
	}
	if flat.String() != shared.String() {
		t.Fatalf("String() = flat:%q shared:%q, want equal", flat.String(), shared.String())
	}
	flatSlice, sharedSlice := flat.ToSlice(), shared.ToSlice()
	if len(flatSlice) != len(sharedSlice) {
		t.Fatalf("ToSlice() len = flat:%d shared:%d", len(flatSlice), len(sharedSlice))
	}
	for i := range flatSlice {
		if !flatSlice[i].Equals(sharedSlice[i]) {
			t.Fatalf("ToSlice()[%d] = flat:%v shared:%v, want equal", i, flatSlice[i], sharedSlice[i])
		}
	}
}

// TestVectorBulkVsIncrementalEquivalence builds the same above-threshold
// vector two ways — one NewVector call and one element at a time via Conj —
// and asserts they agree on every observable. Both paths promote through
// the same trie/tail machinery, but from different call shapes.
func TestVectorBulkVsIncrementalEquivalence(t *testing.T) {
	const n = 130 // several trie levels past the threshold
	items := make([]core.Value, n)
	for i := range items {
		items[i] = core.Int{V: int64(i)}
	}

	bulk := core.NewVector(append([]core.Value(nil), items...))
	incremental := core.Vector{}
	for _, v := range items {
		incremental, _ = incremental.Conj(v)
	}

	if bulk.Len() != incremental.Len() {
		t.Fatalf("Len() = bulk:%d incremental:%d", bulk.Len(), incremental.Len())
	}
	for i := 0; i < n; i++ {
		if !bulk.At(i).Equals(incremental.At(i)) {
			t.Fatalf("At(%d) = bulk:%v incremental:%v, want equal", i, bulk.At(i), incremental.At(i))
		}
	}
	if !bulk.Equals(incremental) || !incremental.Equals(bulk) {
		t.Fatalf("Equals mismatch: bulk=%s incremental=%s", bulk.String(), incremental.String())
	}
	if bulk.String() != incremental.String() {
		t.Fatalf("String() = bulk:%q incremental:%q, want equal", bulk.String(), incremental.String())
	}
}

// assertVectorMatchesRef mirrors assertListMatchesRef for Vector.
func assertVectorMatchesRef(t *testing.T, step int, vec core.Vector, ref []core.Value) {
	t.Helper()
	if vec.Len() != len(ref) {
		t.Fatalf("op %d: Len() = %d, want %d", step, vec.Len(), len(ref))
	}
	for i, want := range ref {
		if got := vec.At(i); !got.Equals(want) {
			t.Fatalf("op %d: At(%d) = %v, want %v", step, i, got, want)
		}
	}
	got := vec.ToSlice()
	if len(got) != len(ref) {
		t.Fatalf("op %d: ToSlice() len = %d, want %d", step, len(got), len(ref))
	}
	for i := range ref {
		if !got[i].Equals(ref[i]) {
			t.Fatalf("op %d: ToSlice()[%d] = %v, want %v", step, i, got[i], ref[i])
		}
	}
	refVec := core.NewVector(append([]core.Value(nil), ref...))
	if !vec.Equals(refVec) {
		t.Fatalf("op %d: Equals mismatch, vec=%s ref=%s", step, vec.String(), refVec.String())
	}
	if vec.String() != refVec.String() {
		t.Fatalf("op %d: String() = %q, want %q", step, vec.String(), refVec.String())
	}
}

// TestConsMonotonic_ChargesPerNewNodeOnly loops Cons onto the same growing
// list n times, asserting every call's charge matches wantConsBytes exactly
// — never re-charging the shared prefix, never zero — and that the running
// total grows linearly with n rather than quadratically. The escape this
// guards against: an embedder building an arbitrarily large shared list
// while being billed once per new node, not once per node in the whole
// chain on every call.
func TestConsMonotonic_ChargesPerNewNodeOnly(t *testing.T) {
	const n = 5000
	lst := core.List{}
	shared := false
	var total int64
	for i := range n {
		prevLen := lst.Len()
		newLst, bytes := lst.Cons(core.Int{V: int64(i)})
		if bytes <= 0 {
			t.Fatalf("iteration %d: Cons byte cost = %d, want > 0", i, bytes)
		}
		if want := wantConsBytes(prevLen, shared); bytes != want {
			t.Fatalf("iteration %d: Cons byte cost = %d, want %d", i, bytes, want)
		}
		shared = shared || prevLen+1 > testListFlatThreshold
		total += bytes
		lst = newLst
	}
	if lst.Len() != n {
		t.Fatalf("Len() = %d, want %d", lst.Len(), n)
	}
	// Steady state is exactly n*nodeBytes, plus one bounded promotion and a
	// bounded run of flat copies below the threshold — never n^2. The old
	// whole-copy formula (ListShallowBytes(Len())) would total tens of MB
	// here; this bound is a small, n-scaled multiple of one node's cost.
	nodeBytes := core.ListShallowBytes(1)
	maxLinear := int64(n)*nodeBytes*2 + core.ListShallowBytes(testListFlatThreshold)*int64(testListFlatThreshold)
	if total > maxLinear {
		t.Fatalf("total charge over %d Cons calls = %d, want <= %d (must grow linearly, not quadratically)", n, total, maxLinear)
	}
}

// TestConjMonotonic_ChargesPerPathOnly mirrors
// TestConsMonotonic_ChargesPerNewNodeOnly for Vector.Conj: every call's
// charge stays within conjBytesUpperBound (never zero, never scaling with
// vec.Len() past the one-time promotion), and the average charge per call
// stays near one trie path's worth of bytes rather than drifting toward
// VectorShallowBytes(n) as n grows.
func TestConjMonotonic_ChargesPerPathOnly(t *testing.T) {
	const n = 5000
	vec := core.Vector{}
	justFlat := true
	var total int64
	for i := range n {
		prevLen := vec.Len()
		newVec, bytes := vec.Conj(core.Int{V: int64(i)})
		if bytes <= 0 {
			t.Fatalf("iteration %d: Conj byte cost = %d, want > 0", i, bytes)
		}
		if want := conjBytesUpperBound(prevLen, justFlat); bytes > want {
			t.Fatalf("iteration %d: Conj byte cost = %d, want <= %d", i, bytes, want)
		}
		justFlat = false
		total += bytes
		vec = newVec
	}
	if vec.Len() != n {
		t.Fatalf("Len() = %d, want %d", vec.Len(), n)
	}
	avgPerCall := total / n
	// log32(5000) < 3, so each push touches at most a few trie levels —
	// average charge per call should stay a small multiple of one node's
	// bytes, never drift toward VectorShallowBytes(n) (~80KB at n=5000, the
	// old whole-copy formula's shape).
	maxAvg := core.VectorShallowBytes(testVecBranch) * (maxVecPathLevels + 1)
	if avgPerCall > maxAvg {
		t.Fatalf("average charge per Conj call = %d, want <= %d (total must grow linearly, not quadratically)", avgPerCall, maxAvg)
	}
}
