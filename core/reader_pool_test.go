package core

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"unsafe"
)

// readNonPooled reads src the pre-pooling way, through the still-exported
// Reader/Parser constructors, entirely independent of readerScratch — the
// baseline TestReadWithMaxDepthStats_PooledOutputMatchesNonPooledBaseline
// compares the pooled path against.
func readNonPooled(src string, flags readerFlags, maxDepth int) ([]Value, error) {
	if maxDepth <= 0 {
		maxDepth = defaultReaderDepth
	}
	r := NewReaderWithFlags(src, flags)
	tokens, err := r.Tokenize()
	if err != nil {
		return nil, err
	}
	p := NewParserWithDepth(tokens, maxDepth)
	var forms []Value
	for p.peek().typ != tokenEOF {
		form, err := p.Parse()
		if err != nil {
			return nil, err
		}
		forms = append(forms, form)
	}
	return forms, nil
}

func TestReaderScratch_ResetClearsAllFields(t *testing.T) {
	t.Parallel()
	s := &readerScratch{}
	s.reader.input = "garbage"
	s.reader.pos = 5
	s.reader.line = 9
	s.reader.col = 9
	s.reader.flags = readerFlags{bracketLiterals: true, functionRef: true, readerVector: true}
	s.reader.countOnly = true
	s.parser.tokens = []token{{typ: tokenSymbol, val: "x"}}
	s.parser.pos = 3
	s.parser.maxDepth = 5
	s.parser.depth = 7
	s.parser.stats = ReaderStats{Nodes: 42, Bytes: 100}
	s.parser.nodes = []Value{Nil{}, Bool{V: true}}
	s.tokens = []token{{typ: tokenLParen}}

	s.Reset()

	if s.reader.pos != 0 {
		t.Errorf("reader.pos = %d, want 0", s.reader.pos)
	}
	if s.reader.line != 1 {
		t.Errorf("reader.line = %d, want 1", s.reader.line)
	}
	if s.reader.col != 1 {
		t.Errorf("reader.col = %d, want 1", s.reader.col)
	}
	if s.reader.countOnly {
		t.Error("reader.countOnly = true, want false")
	}
	if s.parser.pos != 0 {
		t.Errorf("parser.pos = %d, want 0", s.parser.pos)
	}
	if s.parser.depth != 0 {
		t.Errorf("parser.depth = %d, want 0", s.parser.depth)
	}
	if s.parser.stats != (ReaderStats{}) {
		t.Errorf("parser.stats = %+v, want zero value", s.parser.stats)
	}
	if len(s.parser.nodes) != 0 {
		t.Errorf("len(parser.nodes) = %d, want 0", len(s.parser.nodes))
	}
	if len(s.tokens) != 0 {
		t.Errorf("len(s.tokens) = %d, want 0", len(s.tokens))
	}
}

func TestReaderScratch_RetainedTreeSurvivesReuse(t *testing.T) {
	t.Parallel()
	s := &readerScratch{}
	s.Reset()
	formsA, _, err := s.read(`(+ 1 2 "hello")`, defaultReaderFlags(), 0)
	if err != nil {
		t.Fatalf("parse A: %v", err)
	}
	wantA := formsA[0].String()

	s.Reset()

	formsB, _, err := s.read(`(- 3 4 "world")`, defaultReaderFlags(), 0)
	if err != nil {
		t.Fatalf("parse B: %v", err)
	}

	if got := formsA[0].String(); got != wantA {
		t.Errorf("source A's retained tree changed after reuse: got %q, want %q", got, wantA)
	}
	if formsB[0].String() == wantA {
		t.Fatalf("test invalid: source B produced the same tree as A")
	}
}

func TestReaderScratch_NodeScratchIsolatedAcrossReuse(t *testing.T) {
	t.Parallel()
	s := &readerScratch{}
	s.Reset()
	forms, _, err := s.read(`((1 2 3) [4 5 6])`, defaultReaderFlags(), 0)
	if err != nil {
		t.Fatalf("parse A: %v", err)
	}
	outer, ok := forms[0].(List)
	if !ok || outer.Len() != 2 {
		t.Fatalf("forms[0] = %#v, want a 2-element List", forms[0])
	}
	listVal, ok := outer.At(0).(List)
	if !ok {
		t.Fatalf("outer.At(0) = %T, want List", outer.At(0))
	}
	vecVal, ok := outer.At(1).(Vector)
	if !ok {
		t.Fatalf("outer.At(1) = %T, want Vector", outer.At(1))
	}
	wantList, wantVec := listVal.String(), vecVal.String()

	if scratchData := unsafe.SliceData(s.parser.nodes); scratchData != nil {
		if unsafe.SliceData(listVal.flat) == scratchData {
			t.Error("list elements alias the parser's node scratch buffer")
		}
		if unsafe.SliceData(vecVal.flat) == scratchData {
			t.Error("vector elements alias the parser's node scratch buffer")
		}
	}

	s.Reset()
	if _, _, err := s.read(`(9 8 7 6 5 4 3 2 1 0 -1 -2 -3 -4 -5 -6 -7 -8 -9 -10)`, defaultReaderFlags(), 0); err != nil {
		t.Fatalf("parse B: %v", err)
	}

	if got := listVal.String(); got != wantList {
		t.Errorf("list elements mutated after reuse: got %s, want %s", got, wantList)
	}
	if got := vecVal.String(); got != wantVec {
		t.Errorf("vector elements mutated after reuse: got %s, want %s", got, wantVec)
	}
}

func TestReaderScratch_StatsIdenticalAcrossReuse(t *testing.T) {
	t.Parallel()
	const src = `(+ 1 2 (str "hello" :world))`

	fresh := NewReaderWithFlags(src, defaultReaderFlags())
	tokens, err := fresh.Tokenize()
	if err != nil {
		t.Fatalf("fresh Tokenize: %v", err)
	}
	freshParser := NewParser(tokens)
	for freshParser.peek().typ != tokenEOF {
		if _, err := freshParser.Parse(); err != nil {
			t.Fatalf("fresh Parse: %v", err)
		}
	}
	want := freshParser.Stats()

	s := &readerScratch{}
	for i := 0; i < 3; i++ {
		s.Reset()
		_, got, err := s.read(src, defaultReaderFlags(), 0)
		if err != nil {
			t.Fatalf("pooled parse %d: %v", i, err)
		}
		if got != want {
			t.Errorf("iteration %d: stats = %+v, want %+v (fresh non-pooled)", i, got, want)
		}
	}
}

// TestReadWithMaxDepthStats_CrossDialectPoolReuseNoFlagLeak runs two dialects
// with differing bracketLiterals through the same package-global pool
// concurrently, under -race, and checks each goroutine's own dialect got the
// correct result — a stale-flags leak would make the no-brackets dialect
// silently accept the brackets the other dialect's checkout enabled, which
// -race alone would never catch.
func TestReadWithMaxDepthStats_CrossDialectPoolReuseNoFlagLeak(t *testing.T) {
	withBrackets := FullDialect()
	withoutBrackets := FullDialect().WithoutBracketLiterals()

	const iterations = 200
	var wg sync.WaitGroup
	errs := make(chan error, iterations*2)

	runBrackets := func() {
		defer wg.Done()
		forms, _, err := withBrackets.ReadWithMaxDepthStats("[1 2 3]", 0)
		if err != nil {
			errs <- fmt.Errorf("brackets-on dialect: unexpected error: %w", err)
			return
		}
		v, ok := forms[0].(Vector)
		if !ok || v.Len() != 3 {
			errs <- fmt.Errorf("brackets-on dialect: forms[0] = %#v, want a 3-element Vector", forms[0])
		}
	}
	runNoBrackets := func() {
		defer wg.Done()
		forms, _, err := withoutBrackets.ReadWithMaxDepthStats("[1 2 3]", 0)
		if err == nil {
			errs <- fmt.Errorf("brackets-off dialect: expected an error reading [1 2 3], got forms %v", forms)
		}
	}

	for i := 0; i < iterations; i++ {
		wg.Add(2)
		go runBrackets()
		go runNoBrackets()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	s := &readerScratch{}
	s.Reset()
	if _, _, err := s.read("(+ 1 2)", defaultReaderFlags(), 0); err != nil {
		t.Fatalf("sequential success 1: %v", err)
	}
	s.Reset()
	if _, _, err := s.read(`(unterminated`, defaultReaderFlags(), 0); err == nil {
		t.Fatal("sequential error step: expected an error for an unclosed list")
	}
	s.Reset()
	forms, _, err := s.read("(+ 3 4)", defaultReaderFlags(), 0)
	if err != nil {
		t.Fatalf("sequential success 2: %v", err)
	}
	if len(forms) != 1 || forms[0].String() != "(+ 3 4)" {
		t.Errorf("sequential success 2: forms = %v, want [(+ 3 4)]", forms)
	}
}

func TestReadWithMaxDepthStats_ErrorPositionOneBasedAfterPooledReuse(t *testing.T) {
	t.Parallel()
	d := FullDialect()
	if _, _, err := d.ReadWithMaxDepthStats("(+ 1 2)", 0); err != nil {
		t.Fatalf("warm-up read: %v", err)
	}

	_, _, err := d.ReadWithMaxDepthStats("(+ 1 2)\n\"unterminated", 0)
	if err == nil {
		t.Fatal("expected an unterminated-string error")
	}
	lerr, ok := err.(*LispicoError)
	if !ok {
		t.Fatalf("err = %T, want *LispicoError", err)
	}
	if lerr.Line != 2 {
		t.Errorf("Line = %d, want 2 (1-based, not carried over from the previous read)", lerr.Line)
	}
	if lerr.Col < 1 {
		t.Errorf("Col = %d, want >= 1 (1-based)", lerr.Col)
	}
}

func TestReadWithMaxDepthStats_ZeroMaxDepthUsesDefault(t *testing.T) {
	t.Parallel()
	const depth = 50
	var b strings.Builder
	for i := 0; i < depth; i++ {
		b.WriteString("(list ")
	}
	b.WriteString("1")
	for i := 0; i < depth; i++ {
		b.WriteString(")")
	}

	d := FullDialect()
	forms, _, err := d.ReadWithMaxDepthStats(b.String(), 0)
	if err != nil {
		t.Fatalf("ReadWithMaxDepthStats(src, 0): %v", err)
	}
	if len(forms) != 1 {
		t.Fatalf("got %d forms, want 1", len(forms))
	}
}

// TestReadWithMaxDepthStats_PooledNoEscapeStringSharesBackingArray proves the
// D2 aliasing contract (see TestReadString_NoEscapeSharesBackingArray in
// reader_test.go) survives the pooled path too: an escape-free literal's
// String.V must alias the source string even when read off a scratch object
// recycled from readerScratchPool, not just off a fresh Reader.
func TestReadWithMaxDepthStats_PooledNoEscapeStringSharesBackingArray(t *testing.T) {
	d := FullDialect()

	for i := 0; i < 2; i++ {
		if _, _, err := d.ReadWithMaxDepthStats(`"warmup"`, 0); err != nil {
			t.Fatalf("warm-up read %d: %v", i, err)
		}
	}

	src := `"hello"`
	forms, _, err := d.ReadWithMaxDepthStats(src, 0)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	s, ok := forms[0].(String)
	if !ok {
		t.Fatalf("forms[0] = %T, want String", forms[0])
	}
	wantData := unsafe.StringData(src[1:6])
	gotData := unsafe.StringData(s.V)
	if gotData != wantData {
		t.Error("String.V does not share backing memory with the source under the pooled path; fast path did not engage")
	}
}

// TestReadWithMaxDepthStats_PooledOutputMatchesNonPooledBaseline pins that
// pooling only changes allocation shape, never observable output: every case
// below is read once through readNonPooled's pre-pooling construction, then
// repeatedly through the pooled Dialect.ReadWithMaxDepthStats, comparing each
// pooled result against the non-pooled baseline by both Equals and String().
func TestReadWithMaxDepthStats_PooledOutputMatchesNonPooledBaseline(t *testing.T) {
	t.Parallel()
	d := FullDialect()
	cases := []string{
		`(+ 1 (* 2 3) (- 4 5))`,                // nested list
		`[1 2 [3 4] 5]`,                        // nested vector
		`{:a 1 :b [2 3] :c "x"}`,               // hash-map literal
		`'(a b c)`,                             // quoted form
		`"line one\nline two\ttab \"quoted\""`, // string with escapes
		"(" + strings.Repeat("1 ", 40) + ")",   // > listFlatThreshold: shared-tail chain
		`()`,                                   // empty list: zero-length non-nil backing slice
		`[]`,                                   // empty vector: zero-length non-nil backing slice
	}

	for _, src := range cases {
		baseline, err := readNonPooled(src, d.readerFlags(), 0)
		if err != nil {
			t.Fatalf("non-pooled baseline for %q: %v", src, err)
		}

		const iterations = 50
		for i := 0; i < iterations; i++ {
			got, _, err := d.ReadWithMaxDepthStats(src, 0)
			if err != nil {
				t.Fatalf("pooled read %d for %q: %v", i, src, err)
			}
			if len(got) != len(baseline) {
				t.Fatalf("pooled read %d for %q: got %d forms, want %d", i, src, len(got), len(baseline))
			}
			for j := range got {
				if !got[j].Equals(baseline[j]) {
					t.Errorf("pooled read %d for %q, form %d: Equals mismatch: got %s, want %s", i, src, j, got[j].String(), baseline[j].String())
				}
				if got[j].String() != baseline[j].String() {
					t.Errorf("pooled read %d for %q, form %d: String mismatch: got %q, want %q", i, src, j, got[j].String(), baseline[j].String())
				}
			}
		}
	}
}

func TestTokenizeInto_ErrorReturnsBufferNotNil(t *testing.T) {
	t.Parallel()
	buf := make([]token, 0, 64)
	r := NewReaderWithFlags(`"unterminated`, defaultReaderFlags())
	got, err := r.tokenizeInto(buf)
	if err == nil {
		t.Fatal("expected an unterminated-string error")
	}
	if cap(got) < cap(buf) {
		t.Errorf("tokenizeInto discarded retained capacity on error: cap(got) = %d, want >= %d", cap(got), cap(buf))
	}
}
