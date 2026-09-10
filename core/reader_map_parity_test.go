package core

import (
	"context"
	"math/bits"
	"strconv"
	"strings"
	"testing"
)

// The reader builds collections through Parser.mapSet and Parser.buildList
// instead of HashMap.Set and NewList. Content and storage form both have to
// match: a map literal past hashMapSmallLimit read through core.Read lands in
// large.root, while the same content through HashMap.Set lands in large.m, and
// the two large forms are exclusive — getByHashKey tests root first and would
// never consult m.

// readerEntry is one public source-to-forms path. Both run the same Parser, so
// both must agree with the constructor-built collection.
type readerEntry struct {
	name string
	read func(src string) ([]Value, error)
}

func readerEntries() []readerEntry {
	return []readerEntry{
		{"read", Read},
		{"read-context", func(src string) ([]Value, error) {
			forms, _, err := readContextStats(context.Background(), FullDialect(), src, 0)
			return forms, err
		}},
	}
}

func readSingleForm(t *testing.T, e readerEntry, src string) Value {
	t.Helper()
	forms, err := e.read(src)
	if err != nil {
		t.Fatalf("%s failed: %v", e.name, err)
	}
	if len(forms) != 1 {
		t.Fatalf("%s read %d forms, want 1", e.name, len(forms))
	}
	return forms[0]
}

// collisionKeys hash to the same uint32 under hashOfKey, so the trie exhausts
// its hash bits and stores them in a collision node — the arm reached only past
// shift 32, and one of the duplicated insert paths.
var collisionKeys = [2]int64{3367, 6372}

type mapFixture struct {
	name   string
	src    string
	pairs  [][2]Value
	absent Value
	// collision marks the fixture whose keys reach the trie's collision node,
	// checked separately because no read builds a trie to reach it in.
	collision bool
}

// intMapFixture builds n pairs with Int keys 0..n-1, as source and as the same
// content in constructor form.
func intMapFixture(n int) mapFixture {
	pairs := make([][2]Value, 0, n)
	var src strings.Builder
	src.WriteByte('{')
	for i := range n {
		if i > 0 {
			src.WriteByte(' ')
		}
		k, v := Int{V: int64(i)}, Int{V: int64(i) * 10}
		src.WriteString(strconv.FormatInt(k.V, 10))
		src.WriteByte(' ')
		src.WriteString(strconv.FormatInt(v.V, 10))
		pairs = append(pairs, [2]Value{k, v})
	}
	src.WriteByte('}')
	return mapFixture{
		name:   "int-keys-" + strconv.Itoa(n),
		src:    src.String(),
		pairs:  pairs,
		absent: Int{V: -1},
	}
}

func collisionMapFixture() mapFixture {
	pairs := make([][2]Value, 0, hashMapSmallLimit+2)
	var src strings.Builder
	src.WriteByte('{')
	add := func(key int64, val int64) {
		if len(pairs) > 0 {
			src.WriteByte(' ')
		}
		src.WriteString(strconv.FormatInt(key, 10))
		src.WriteByte(' ')
		src.WriteString(strconv.FormatInt(val, 10))
		pairs = append(pairs, [2]Value{Int{V: key}, Int{V: val}})
	}
	for i := range hashMapSmallLimit {
		add(int64(i), int64(i)*10)
	}
	add(collisionKeys[0], 1)
	add(collisionKeys[1], 2)
	src.WriteByte('}')
	return mapFixture{
		name:      "hash-collision",
		src:       src.String(),
		pairs:     pairs,
		absent:    Int{V: -1},
		collision: true,
	}
}

func mixedKeyMapFixture() mapFixture {
	return mapFixture{
		name: "mixed-key-types",
		src:  `{:a 1 "b" 2 c 3 4 4.5 5.5 6 true 7 false 8 nil 9 :z 10}`,
		pairs: [][2]Value{
			{Keyword{V: "a"}, Int{V: 1}},
			{String{V: "b"}, Int{V: 2}},
			{Symbol{V: "c"}, Int{V: 3}},
			{Int{V: 4}, Float{V: 4.5}},
			{Float{V: 5.5}, Int{V: 6}},
			{Bool{V: true}, Int{V: 7}},
			{Bool{V: false}, Int{V: 8}},
			{Nil{}, Int{V: 9}},
			{Keyword{V: "z"}, Int{V: 10}},
		},
		absent: Keyword{V: "missing"},
	}
}

func TestReaderBuiltMapMatchesSetBuilt(t *testing.T) {
	t.Parallel()

	ka, err := toHashKey(Int{V: collisionKeys[0]})
	if err != nil {
		t.Fatalf("toHashKey(%d): %v", collisionKeys[0], err)
	}
	kb, err := toHashKey(Int{V: collisionKeys[1]})
	if err != nil {
		t.Fatalf("toHashKey(%d): %v", collisionKeys[1], err)
	}
	if hashOfKey(ka) != hashOfKey(kb) {
		t.Fatalf("collision fixture is stale: hash(%d) = %d, hash(%d) = %d",
			collisionKeys[0], hashOfKey(ka), collisionKeys[1], hashOfKey(kb))
	}

	// 8 is the last size the sorted-slice form holds, 9 promotes, 33 exceeds the
	// root's 32 slots so at least one child node exists, 128 deepens it further.
	fixtures := []mapFixture{
		intMapFixture(1),
		intMapFixture(hashMapSmallLimit),
		intMapFixture(hashMapSmallLimit + 1),
		intMapFixture(33),
		intMapFixture(128),
		collisionMapFixture(),
		mixedKeyMapFixture(),
	}

	for _, e := range readerEntries() {
		for _, f := range fixtures {
			t.Run(e.name+"/"+f.name, func(t *testing.T) {
				t.Parallel()

				want := NewHashMap()
				for _, p := range f.pairs {
					if err := want.Set(p[0], p[1]); err != nil {
						t.Fatalf("Set(%v): %v", p[0], err)
					}
				}

				form := readSingleForm(t, e, f.src)
				got, ok := form.(*HashMap)
				if !ok {
					t.Fatalf("read %T, want *HashMap", form)
				}
				assertMapParity(t, got, want, f.pairs, f.absent)
				if f.collision {
					assertCollisionArm(t, "reader-built", got)
					assertCollisionArm(t, "constructor-built", want)
				}
			})
		}
	}
}

// mapForm names the storage form a map is in. Exactly one is active.
func mapForm(h *HashMap) string {
	switch {
	case h.large == nil:
		return "entries"
	case h.large.root != nil:
		return "trie"
	case h.large.m != nil:
		return "builder"
	}
	return "large-empty"
}

func assertMapFormInvariants(t *testing.T, label string, h *HashMap) {
	t.Helper()

	if h.large == nil {
		return
	}
	if h.large.root != nil && h.large.m != nil {
		t.Fatalf("%s map holds both large forms at once", label)
	}
	if h.entries != nil {
		t.Fatalf("%s map holds %d small entries alongside its %s form",
			label, len(h.entries), mapForm(h))
	}
}

func assertMapParity(t *testing.T, got, want *HashMap, pairs [][2]Value, absent Value) {
	t.Helper()

	assertMapFormInvariants(t, "reader-built", got)
	assertMapFormInvariants(t, "constructor-built", want)
	if gf, wf := mapForm(got), mapForm(want); gf != wf {
		t.Fatalf("storage form = %s, constructor-built = %s", gf, wf)
	}
	if got.large != nil && got.large.root != nil && got.large.count != len(pairs) {
		t.Fatalf("large.count = %d, fixture has %d pairs", got.large.count, len(pairs))
	}

	if got.Len() != want.Len() {
		t.Fatalf("Len() = %d, constructor-built Len() = %d", got.Len(), want.Len())
	}
	if got.Len() != len(pairs) {
		t.Fatalf("Len() = %d, fixture has %d pairs", got.Len(), len(pairs))
	}

	for _, p := range pairs {
		gv, gok := got.Get(p[0])
		wv, wok := want.Get(p[0])
		if !gok || !wok {
			t.Fatalf("Get(%v) present = %t, constructor-built present = %t", p[0], gok, wok)
		}
		if !gv.Equals(wv) {
			t.Fatalf("Get(%v) = %v, constructor-built = %v", p[0], gv, wv)
		}
		if !gv.Equals(p[1]) {
			t.Fatalf("Get(%v) = %v, fixture value = %v", p[0], gv, p[1])
		}
	}

	gv, gok := got.Get(absent)
	wv, wok := want.Get(absent)
	if gok || wok {
		t.Fatalf("Get(%v) = (%v, %t), constructor-built = (%v, %t), want absent from both",
			absent, gv, gok, wv, wok)
	}

	gotPairs, wantPairs := got.Pairs(), want.Pairs()
	if len(gotPairs) != len(wantPairs) {
		t.Fatalf("Pairs() length = %d, constructor-built = %d", len(gotPairs), len(wantPairs))
	}
	for i := range wantPairs {
		if !gotPairs[i][0].Equals(wantPairs[i][0]) || !gotPairs[i][1].Equals(wantPairs[i][1]) {
			t.Fatalf("Pairs()[%d] = (%v %v), constructor-built = (%v %v)",
				i, gotPairs[i][0], gotPairs[i][1], wantPairs[i][0], wantPairs[i][1])
		}
	}

	gotEach, wantEach := eachPairs(got), eachPairs(want)
	if len(gotEach) != len(wantEach) {
		t.Fatalf("Each visited %d pairs, constructor-built %d", len(gotEach), len(wantEach))
	}
	for i := range wantEach {
		if !gotEach[i][0].Equals(wantEach[i][0]) || !gotEach[i][1].Equals(wantEach[i][1]) {
			t.Fatalf("Each pair %d = (%v %v), constructor-built = (%v %v)",
				i, gotEach[i][0], gotEach[i][1], wantEach[i][0], wantEach[i][1])
		}
	}

	if got.String() != want.String() {
		t.Fatalf("String() = %s, constructor-built = %s", got.String(), want.String())
	}
	if !got.Equals(want) {
		t.Fatalf("reader-built map does not Equal the constructor-built map")
	}
	if !want.Equals(got) {
		t.Fatalf("constructor-built map does not Equal the reader-built map")
	}
}

// assertCollisionArm drives a map into trie form through Assoc — the one
// builder that still reaches the collision node once neither the reader nor Set
// reads into a trie — and checks both colliding keys land in that node.
func assertCollisionArm(t *testing.T, label string, h *HashMap) {
	t.Helper()

	probe, _, err := h.Assoc(Int{V: -2}, Int{V: 0})
	if err != nil {
		t.Fatalf("%s Assoc: %v", label, err)
	}
	if probe.large == nil || probe.large.root == nil {
		t.Fatalf("%s Assoc produced %s form, want trie", label, mapForm(probe))
	}
	if probe.large.count != h.Len()+1 {
		t.Fatalf("%s Assoc large.count = %d, want %d", label, probe.large.count, h.Len()+1)
	}
	for _, key := range collisionKeys {
		hk, err := toHashKey(Int{V: key})
		if err != nil {
			t.Fatalf("toHashKey(%d): %v", key, err)
		}
		n := nodeHolding(probe.large.root, hk, hashOfKey(hk), 0)
		if n == nil {
			t.Fatalf("%s trie holds no node for colliding key %d", label, key)
		}
		if !n.isCollision() {
			t.Fatalf("%s node holding colliding key %d is not a collision node", label, key)
		}
	}
}

// nodeHolding walks the same slot arithmetic hamtNode.get walks, returning the
// node that stores hk rather than its value.
func nodeHolding(n *hamtNode, hk hashKey, h uint32, shift uint) *hamtNode {
	if n.isCollision() {
		for i := range n.entries {
			if n.entries[i].hk == hk {
				return n
			}
		}
		return nil
	}
	bit := uint32(1) << ((h >> shift) & (vecBranch - 1))
	switch {
	case n.dataMap&bit != 0:
		if n.entries[bits.OnesCount32(n.dataMap&(bit-1))].hk == hk {
			return n
		}
	case n.nodeMap&bit != 0:
		return nodeHolding(n.children[bits.OnesCount32(n.nodeMap&(bit-1))], hk, h, shift+vecBits)
	}
	return nil
}

func eachPairs(h *HashMap) [][2]Value {
	pairs := make([][2]Value, 0, h.Len())
	h.Each(func(k, v Value) {
		pairs = append(pairs, [2]Value{k, v})
	})
	return pairs
}

func TestReaderBuiltListMatchesNewList(t *testing.T) {
	t.Parallel()

	// 32 is the last flat length, 33 links a shared-tail chain, 100 links a
	// longer one.
	sizes := []int{1, listFlatThreshold, listFlatThreshold + 1, 100}

	for _, e := range readerEntries() {
		for _, n := range sizes {
			t.Run(e.name+"/len-"+strconv.Itoa(n), func(t *testing.T) {
				t.Parallel()

				items := make([]Value, 0, n)
				var src strings.Builder
				src.WriteByte('(')
				for i := range n {
					if i > 0 {
						src.WriteByte(' ')
					}
					src.WriteString(strconv.Itoa(i * 3))
					items = append(items, Int{V: int64(i) * 3})
				}
				src.WriteByte(')')

				form := readSingleForm(t, e, src.String())
				got, ok := form.(List)
				if !ok {
					t.Fatalf("read %T, want List", form)
				}
				assertListParity(t, got, NewList(items), items)
			})
		}
	}
}

// listForm names the storage form a list is in. Exactly one is active.
func listForm(l List) string {
	if l.shared != nil {
		return "shared-tail"
	}
	return "flat"
}

func assertListParity(t *testing.T, got, want List, items []Value) {
	t.Helper()

	if got.flat != nil && got.shared != nil {
		t.Fatalf("reader-built list holds both storage forms at once")
	}
	if gf, wf := listForm(got), listForm(want); gf != wf {
		t.Fatalf("storage form = %s, constructor-built = %s", gf, wf)
	}
	if got.shared != nil && got.shared.count != len(items) {
		t.Fatalf("shared-tail head count = %d, fixture has %d items", got.shared.count, len(items))
	}

	if got.Len() != want.Len() {
		t.Fatalf("Len() = %d, constructor-built Len() = %d", got.Len(), want.Len())
	}
	if got.Len() != len(items) {
		t.Fatalf("Len() = %d, fixture has %d items", got.Len(), len(items))
	}

	for i := range items {
		gv, wv := got.At(i), want.At(i)
		if !gv.Equals(wv) {
			t.Fatalf("At(%d) = %v, constructor-built = %v", i, gv, wv)
		}
		if !gv.Equals(items[i]) {
			t.Fatalf("At(%d) = %v, fixture item = %v", i, gv, items[i])
		}
	}

	gotSlice, wantSlice := got.ToSlice(), want.ToSlice()
	if len(gotSlice) != len(wantSlice) {
		t.Fatalf("ToSlice() length = %d, constructor-built = %d", len(gotSlice), len(wantSlice))
	}
	for i := range wantSlice {
		if !gotSlice[i].Equals(wantSlice[i]) {
			t.Fatalf("ToSlice()[%d] = %v, constructor-built = %v", i, gotSlice[i], wantSlice[i])
		}
	}

	gotRest, wantRest := got.Rest(), want.Rest()
	if gotRest.Len() != wantRest.Len() {
		t.Fatalf("Rest().Len() = %d, constructor-built = %d", gotRest.Len(), wantRest.Len())
	}
	if !gotRest.Equals(wantRest) {
		t.Fatalf("Rest() = %s, constructor-built = %s", gotRest.String(), wantRest.String())
	}

	if got.String() != want.String() {
		t.Fatalf("String() = %s, constructor-built = %s", got.String(), want.String())
	}
	if !got.Equals(want) {
		t.Fatalf("reader-built list does not Equal the constructor-built list")
	}
	if !want.Equals(got) {
		t.Fatalf("constructor-built list does not Equal the reader-built list")
	}
}
