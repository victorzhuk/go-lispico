package vm

import (
	"math"
	"testing"

	"github.com/victorzhuk/go-lispico/core"
)

func TestAddConstant_ScalarDuplicateReuse(t *testing.T) {
	c := &Chunk{Name: "test"}
	values := []core.Value{
		core.Nil{},
		core.Bool{V: true},
		core.Int{V: 42},
		core.Float{V: 1.5},
		core.String{V: "hello"},
		core.Symbol{V: "foo"},
		core.Keyword{V: "bar"},
	}
	for _, v := range values {
		first := c.AddConstant(v)
		second := c.AddConstant(v)
		if first != second {
			t.Fatalf("%s: first add %d, second add %d", v.Type().V, first, second)
		}
	}
	if got := len(c.Constants); got != len(values) {
		t.Fatalf("pool grew to %d, want %d", got, len(values))
	}
}

func TestAddConstant_NaNNeverInterned(t *testing.T) {
	c := &Chunk{Name: "test"}
	nan := math.NaN()
	first := c.AddConstant(core.Float{V: nan})
	second := c.AddConstant(core.Float{V: nan})
	if first == second {
		t.Fatalf("NaN reused index %d; NaN is not equal to itself", first)
	}
	if got := len(c.Constants); got != 2 {
		t.Fatalf("pool size %d, want 2", got)
	}
}

func TestAddConstant_SignedZeroSharesIndex(t *testing.T) {
	c := &Chunk{Name: "test"}
	first := c.AddConstant(core.Float{V: 0})
	second := c.AddConstant(core.Float{V: math.Copysign(0, -1)})
	if first != second {
		t.Fatalf("+0 index %d, -0 index %d; they compare equal", first, second)
	}
}

func TestAddConstant_CompositeDuplicateReuse(t *testing.T) {
	c := &Chunk{Name: "test"}

	vec1 := core.NewVector([]core.Value{core.Int{V: 1}, core.Float{V: 2.5}})
	vec2 := core.NewVector([]core.Value{core.Int{V: 1}, core.Float{V: 2.5}})
	if idx := c.AddConstant(vec1); idx != c.AddConstant(vec2) {
		t.Fatal("equal vectors did not share an index")
	}

	list1 := core.NewList([]core.Value{core.Symbol{V: "a"}, core.Int{V: 7}})
	list2 := core.NewList([]core.Value{core.Symbol{V: "a"}, core.Int{V: 7}})
	if idx := c.AddConstant(list1); idx != c.AddConstant(list2) {
		t.Fatal("equal lists did not share an index")
	}

	m1, _, _ := core.NewHashMap().Assoc(core.Keyword{V: "k"}, core.String{V: "v"})
	m2, _, _ := core.NewHashMap().Assoc(core.Keyword{V: "k"}, core.String{V: "v"})
	if idx := c.AddConstant(m1); idx != c.AddConstant(m2) {
		t.Fatal("equal maps did not share an index")
	}

	nested1 := core.NewVector([]core.Value{core.Float{V: math.Copysign(0, -1)}, vec1})
	nested2 := core.NewVector([]core.Value{core.Float{V: 0}, vec2})
	if idx := c.AddConstant(nested1); idx != c.AddConstant(nested2) {
		t.Fatal("composites equal under signed-zero normalization did not share an index")
	}

	if got := len(c.Constants); got != 4 {
		t.Fatalf("pool size %d, want 4 distinct composites", got)
	}
}

func TestAddConstant_CompositeDifferenceNotInterned(t *testing.T) {
	c := &Chunk{Name: "test"}
	a := c.AddConstant(core.NewVector([]core.Value{core.Int{V: 1}, core.Int{V: 2}}))
	b := c.AddConstant(core.NewVector([]core.Value{core.Int{V: 1}, core.Int{V: 3}}))
	cKeyMap, _, _ := core.NewHashMap().Assoc(core.Keyword{V: "a"}, core.Int{V: 1})
	cKey := c.AddConstant(cKeyMap)
	d := c.AddConstant(core.NewVector([]core.Value{core.Int{V: 1}, core.Int{V: 2}, core.Int{V: 3}}))
	if a == b || a == cKey || a == d {
		t.Fatal("distinct composites shared an index")
	}
}

func TestAddConstant_PrepopulatedPool(t *testing.T) {
	pre := []core.Value{
		core.Symbol{V: "x"},
		core.NewVector([]core.Value{core.Int{V: 9}}),
		core.Int{V: -3},
	}
	c := &Chunk{Name: "test", Constants: pre}
	if idx := c.AddConstant(core.Symbol{V: "x"}); idx != 0 {
		t.Fatalf("reused prepopulated symbol at %d, want 0", idx)
	}
	if idx := c.AddConstant(core.NewVector([]core.Value{core.Int{V: 9}})); idx != 1 {
		t.Fatalf("reused prepopulated vector at %d, want 1", idx)
	}
	if idx := c.AddConstant(core.Int{V: -3}); idx != 2 {
		t.Fatalf("reused prepopulated int at %d, want 2", idx)
	}
	if got := len(c.Constants); got != 3 {
		t.Fatalf("pool grew to %d, want 3", got)
	}
	if idx := c.AddConstant(core.Int{V: 1000}); idx != 3 {
		t.Fatalf("new constant at %d, want 3", idx)
	}
	if idx := c.AddConstant(core.Int{V: 1000}); idx != 3 {
		t.Fatalf("post-prepopulation duplicate at %d, want 3", idx)
	}
}

type slowValue struct{ n int }

func (s slowValue) Type() core.Keyword       { return core.Keyword{V: "slow"} }
func (s slowValue) String() string           { return "#<slow>" }
func (s slowValue) Equals(o core.Value) bool { other, ok := o.(slowValue); return ok && s.n == other.n }

func TestAddConstant_SlowPathValues(t *testing.T) {
	c := &Chunk{Name: "test"}
	first := c.AddConstant(slowValue{n: 1})
	if second := c.AddConstant(slowValue{n: 1}); second != first {
		t.Fatal("equal slow-path values did not share an index")
	}
	if third := c.AddConstant(slowValue{n: 2}); third == first {
		t.Fatal("distinct slow-path values shared an index")
	}
	if got := len(c.Constants); got != 2 {
		t.Fatalf("pool size %d, want 2", got)
	}
}

type lateSlowValue struct{ n int }

func (s lateSlowValue) Type() core.Keyword { return core.Keyword{V: "late-slow"} }
func (s lateSlowValue) String() string     { return "#<late-slow>" }

func (s lateSlowValue) Equals(o core.Value) bool {
	switch other := o.(type) {
	case lateSlowValue:
		return s.n == other.n
	case core.Int:
		return s.n == int(other.V)
	}
	return false
}

func TestAddConstant_LateUnfingerprintableReused(t *testing.T) {
	c := &Chunk{Name: "test"}
	c.AddConstant(core.Int{V: 1})
	hostIdx := c.AddConstant(lateSlowValue{n: 7})
	if got := c.AddConstant(core.Int{V: 7}); got != hostIdx {
		t.Fatalf("core.Int equal to post-build host value resolved to %d, want %d", got, hostIdx)
	}
	if again := c.AddConstant(lateSlowValue{n: 7}); again != hostIdx {
		t.Fatalf("late host duplicate resolved to %d, want %d", again, hostIdx)
	}
	if got := len(c.Constants); got != 2 {
		t.Fatalf("pool size %d, want 2", got)
	}
}

func TestAddConstant_FingerprintableFindsSlowPathDuplicate(t *testing.T) {
	c := &Chunk{Name: "test"}
	c.AddConstant(slowValue{n: 5})
	intIdx := c.AddConstant(core.Int{V: 11})
	if again := c.AddConstant(core.Int{V: 11}); again != intIdx {
		t.Fatalf("int duplicate resolved to %d, want %d", again, intIdx)
	}
	if got := len(c.Constants); got != 2 {
		t.Fatalf("pool size %d, want 2", got)
	}
}

// countedValue participates in the fingerprint index via ConstantFingerprint
// and counts Equals calls, exposing the scan strategy as an operation count.
type countedValue struct {
	n     int
	tag   int // distinguishes values sharing the same fingerprint
	calls *int
}

func (c countedValue) Type() core.Keyword { return core.Keyword{V: "counted"} }
func (c countedValue) String() string     { return "#<counted>" }
func (c countedValue) Equals(o core.Value) bool {
	*c.calls++
	other, ok := o.(countedValue)
	return ok && c.n == other.n && c.tag == other.tag
}
func (c countedValue) ConstantFingerprint() uint64 { return uint64(c.n) }

func TestAddConstant_OperationCountLinear(t *testing.T) {
	const n = 4096
	calls := 0
	c := &Chunk{Name: "test"}
	for i := range n {
		c.AddConstant(countedValue{n: i, calls: &calls})
	}
	if got := len(c.Constants); got != n {
		t.Fatalf("pool size %d, want %d", got, n)
	}
	if limit := 8 * n; calls > limit {
		t.Fatalf("Equals called %d times for %d adds, want <= %d (quadratic scan)", calls, n, limit)
	}
}

func TestAddConstant_CollisionResolvedByEquals(t *testing.T) {
	calls := 0
	c := &Chunk{Name: "test"}
	first := c.AddConstant(countedValue{n: 1, tag: 0, calls: &calls})
	second := c.AddConstant(countedValue{n: 1, tag: 1, calls: &calls})
	if calls == 0 {
		t.Fatal("Equals never ran; collision test did not exercise the Equals path")
	}
	if first == second {
		t.Fatal("fingerprint collision reused an index despite Equals being false")
	}
	if got := len(c.Constants); got != 2 {
		t.Fatalf("pool size %d, want 2", got)
	}
}

func TestCopyTreeFreshSites_AddConstantIndependentIndex(t *testing.T) {
	c := &Chunk{Name: "test"}
	c.AddConstant(core.String{V: "shared"})
	copied := c.CopyTreeFreshSites()
	if idx := copied.AddConstant(core.String{V: "shared"}); idx != 0 {
		t.Fatalf("copy reused prepopulated constant at %d, want 0", idx)
	}
	if idx := copied.AddConstant(core.String{V: "copy-only"}); idx != 1 {
		t.Fatalf("copy appended at %d, want 1", idx)
	}
	if got := len(c.Constants); got != 1 {
		t.Fatalf("original pool grew to %d, want 1", got)
	}
}
