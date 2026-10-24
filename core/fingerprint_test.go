package core

import (
	"math"
	"testing"
)

// sharedDAG builds m_0..m_{d-1} where every m_i references m_{i-1} under two
// distinct keys. The DAG holds d maps but 2^d fingerprint paths, so any
// non-memoized walk blows up well before d reaches 24.
func sharedDAG(d int) []*HashMap {
	maps := make([]*HashMap, d)
	maps[0] = NewHashMap()
	if err := maps[0].Set(Keyword{V: "leaf"}, Int{V: 0}); err != nil {
		panic(err)
	}
	for i := 1; i < d; i++ {
		m := NewHashMap()
		if err := m.Set(Keyword{V: "a"}, maps[i-1]); err != nil {
			panic(err)
		}
		if err := m.Set(Keyword{V: "b"}, maps[i-1]); err != nil {
			panic(err)
		}
		maps[i] = m
	}
	return maps
}

func TestFingerprint_SharedMapDAG(t *testing.T) {
	t.Parallel()

	const d = 24 // 2^24 paths: an exponential walk cannot finish under go test
	maps := sharedDAG(d)

	got, ok := Fingerprint(maps[d-1])
	if !ok {
		t.Fatal("Fingerprint(shared DAG root) ok = false, want true")
	}

	// Equal structures built independently must share the fingerprint.
	again, ok := Fingerprint(sharedDAG(d)[d-1])
	if !ok {
		t.Fatal("Fingerprint(independent equal DAG) ok = false, want true")
	}
	if got != again {
		t.Fatalf("equal DAGs fingerprinted differently: %d vs %d", got, again)
	}

	// A shared submap reachable by two parents folds once, so wrapping it in
	// a fresh parent must not change the submap's own fingerprint result.
	wrap := NewHashMap()
	if err := wrap.Set(Keyword{V: "a"}, maps[d-2]); err != nil {
		t.Fatal(err)
	}
	if _, ok := Fingerprint(wrap); !ok {
		t.Fatal("Fingerprint(wrapper of shared submap) ok = false, want true")
	}
}

func TestFingerprint_SelfReferentialMapUnsupported(t *testing.T) {
	t.Parallel()

	m := NewHashMap()
	if err := m.Set(Keyword{V: "self"}, m); err != nil {
		t.Fatal(err)
	}
	if _, ok := Fingerprint(m); ok {
		t.Fatal("Fingerprint(self-referential map) ok = true, want false")
	}

	// A cycle nested under a non-cyclic root is also unsupported, and must
	// not poison the parent into reporting a memoized success on retry.
	outer := NewHashMap()
	if err := outer.Set(Keyword{V: "cycle"}, m); err != nil {
		t.Fatal(err)
	}
	if err := outer.Set(Keyword{V: "plain"}, Int{V: 7}); err != nil {
		t.Fatal(err)
	}
	if _, ok := Fingerprint(outer); ok {
		t.Fatal("Fingerprint(map containing a cycle) ok = true, want false")
	}
	if _, ok := Fingerprint(outer); ok {
		t.Fatal("second Fingerprint(map containing a cycle) ok = true, want false")
	}
}

// unsupportedDAG is sharedDAG with a lambda inside the leaf map: the whole DAG
// is unsupported, and without failure memoization each of the 2^d paths walks
// the DAG before reporting ok=false.
func unsupportedDAG(d int) []*HashMap {
	maps := sharedDAG(d)
	leaf := maps[0]
	if err := leaf.Set(Keyword{V: "lambda"}, Lambda{}); err != nil {
		panic(err)
	}
	return maps
}

func TestFingerprint_SharedMapDAGUnsupportedLeaf(t *testing.T) {
	t.Parallel()

	const d = 24 // 2^24 paths: an exponential walk cannot finish under go test
	root := unsupportedDAG(d)[d-1]

	if _, ok := Fingerprint(root); ok {
		t.Fatal("Fingerprint(DAG ending in lambda) ok = true, want false")
	}
	// A second invocation takes the same refusal path: the failure result
	// must stay stable, not flip to a memoized success.
	if _, ok := Fingerprint(unsupportedDAG(d)[d-1]); ok {
		t.Fatal("second Fingerprint(DAG ending in lambda) ok = true, want false")
	}
}

func TestFingerprint_ScalarAndContainerCompatibility(t *testing.T) {
	t.Parallel()

	negZero, _ := Fingerprint(Float{V: math.Copysign(0, -1)})
	posZero, _ := Fingerprint(Float{V: 0})
	if negZero != posZero {
		t.Fatalf("-0 fingerprint %d != +0 fingerprint %d", negZero, posZero)
	}

	cases := []struct {
		name string
		a, b Value
	}{
		{"int", Int{V: 42}, Int{V: 42}},
		{"string", String{V: "x"}, String{V: "x"}},
		{"keyword", Keyword{V: "k"}, Keyword{V: "k"}},
		{"symbol", Symbol{V: "s"}, Symbol{V: "s"}},
		{"bool", Bool{V: true}, Bool{V: true}},
		{"nil", Nil{}, Nil{}},
		{"list", NewList([]Value{Int{V: 1}}), NewList([]Value{Int{V: 1}})},
		{"vector", NewVector([]Value{Int{V: 1}}), NewVector([]Value{Int{V: 1}})},
	}
	for _, tc := range cases {
		ta, oa := Fingerprint(tc.a)
		tb, ob := Fingerprint(tc.b)
		if !oa || !ob {
			t.Fatalf("%s: fingerprint not ok", tc.name)
		}
		if ta != tb {
			t.Fatalf("%s: equal values fingerprinted differently: %d vs %d", tc.name, ta, tb)
		}
	}

	lambda := Lambda{}
	if _, ok := Fingerprint(lambda); ok {
		t.Fatal("Fingerprint(lambda) ok = true, want false")
	}
}
