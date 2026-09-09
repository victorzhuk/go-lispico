package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"unsafe"
)

// guardedScratchRead runs one guarded read on the caller's scratch exactly the
// way Dialect.ReadWithContextStats does, minus the pool: the budget is still
// installed when the test calls release, which is what decides whether the
// scratch is cleared or dropped.
func guardedScratchRead(ctx context.Context, s *readerScratch, src string) ([]Value, error) {
	s.Reset()
	s.budget = newReaderBudget(ctx)
	if err := s.budget.checkpoint(); err != nil {
		return nil, err
	}
	forms, _, err := s.read(src, FullDialect().readerFlags(), 0)
	return forms, err
}

// assertScratchCleared checks that a released scratch pins nothing the read
// touched: not the source, not the token vals, not the parsed nodes, not the
// budget. The token and node buffers are checked to their whole retained
// capacity, since the parser stacks children well past the length it returns.
func assertScratchCleared(t *testing.T, s *readerScratch) {
	t.Helper()
	if s.reader.input != "" {
		t.Errorf("released scratch still pins %d bytes of source", len(s.reader.input))
	}
	if s.parser.tokens != nil {
		t.Error("released scratch still holds the parser's view of the token buffer")
	}
	for i, tok := range s.tokens[:cap(s.tokens)] {
		if tok != (token{}) {
			t.Fatalf("token slot %d of %d still holds %+v after release", i, cap(s.tokens), tok)
		}
	}
	for i, node := range s.parser.nodes[:cap(s.parser.nodes)] {
		if node != nil {
			t.Fatalf("node slot %d of %d still holds %v after release", i, cap(s.parser.nodes), node)
		}
	}
	if s.budget != nil || s.reader.budget != nil || s.parser.budget != nil {
		t.Error("released scratch still holds the read's budget")
	}
}

func TestGuardedRead_ScratchReleaseDropsOversizedCapacity(t *testing.T) {
	wide := "(" + strings.Repeat("a ", 400) + ")"

	t.Run("low-budget-ceiling/drops-high-water-storage", func(t *testing.T) {
		const lowCeiling = 512

		ctx, _ := allocCeilingContext(DefaultMaxAllocationBytes)
		high := &readerScratch{}
		if _, err := guardedScratchRead(ctx, high, wide); err != nil {
			t.Fatalf("read of a %d-byte form failed: %v", len(wide), err)
		}
		if retained := high.retained(); retained <= lowCeiling {
			t.Fatalf("test invalid: the wide read retained %d bytes, want more than the %d-byte ceiling", retained, lowCeiling)
		}
		if high.release(lowCeiling) {
			t.Errorf("a scratch retaining %d bytes went back in the pool under a %d-byte ceiling", high.retained(), lowCeiling)
		}

		narrow := &readerScratch{}
		if _, err := guardedScratchRead(ctx, narrow, "(a b c)"); err != nil {
			t.Fatalf("read of a 3-child form failed: %v", err)
		}
		if retained := narrow.retained(); retained > lowCeiling {
			t.Fatalf("test invalid: the narrow read retained %d bytes, want no more than the %d-byte ceiling", retained, lowCeiling)
		}
		if !narrow.release(lowCeiling) {
			t.Errorf("a scratch retaining %d bytes was dropped under a %d-byte ceiling", narrow.retained(), lowCeiling)
		}
	})

	t.Run("within-the-ceiling/clears-and-keeps-the-capacity", func(t *testing.T) {
		ctx, _ := allocCeilingContext(DefaultMaxAllocationBytes)
		s := &readerScratch{}
		if _, err := guardedScratchRead(ctx, s, wide); err != nil {
			t.Fatalf("read of a %d-byte form failed: %v", len(wide), err)
		}
		tokens, nodes := cap(s.tokens), cap(s.parser.nodes)
		if tokens == 0 || nodes == 0 {
			t.Fatalf("test invalid: the read retained %d token and %d node slots, want both buffers grown", tokens, nodes)
		}

		if !s.release(s.retained()) {
			t.Fatal("a scratch retaining exactly its ceiling was dropped, want it cleared and pooled")
		}
		if got, want := cap(s.tokens), tokens; got != want {
			t.Errorf("release left %d token slots, want the %d it started with", got, want)
		}
		if got, want := cap(s.parser.nodes), nodes; got != want {
			t.Errorf("release left %d node slots, want the %d it started with", got, want)
		}
		if len(s.tokens) != 0 || len(s.parser.nodes) != 0 {
			t.Errorf("release left %d tokens and %d nodes in use, want both empty", len(s.tokens), len(s.parser.nodes))
		}
		assertScratchCleared(t, s)
	})

	t.Run("clearing/charges-the-read-being-released", func(t *testing.T) {
		ctx, meter := allocCeilingContext(DefaultMaxAllocationBytes)
		s := &readerScratch{}
		if _, err := guardedScratchRead(ctx, s, wide); err != nil {
			t.Fatalf("read of a %d-byte form failed: %v", len(wide), err)
		}
		slots := int64(cap(s.tokens) + cap(s.parser.nodes))
		before := chargedReductions(meter)

		if !s.release(s.retained()) {
			t.Fatal("a scratch retaining exactly its ceiling was dropped, want it cleared and pooled")
		}
		if got := chargedReductions(meter) - before; got != slots {
			t.Errorf("clearing %d retained slots charged %d reductions, want one per slot", slots, got)
		}
	})
}

func TestGuardedRead_FailedReadReleasesScratch(t *testing.T) {
	t.Run("syntax-failure/clears-and-pools", func(t *testing.T) {
		ctx, _ := allocCeilingContext(DefaultMaxAllocationBytes)
		s := &readerScratch{}

		if _, err := guardedScratchRead(ctx, s, "(a b "+strings.Repeat("c ", 200)); err == nil {
			t.Fatal("expected an unclosed-list error")
		}
		if !s.release(readerScratchNoCeiling) {
			t.Fatal("a scratch whose read failed on syntax refused the pool, want it cleared and pooled")
		}
		assertScratchCleared(t, s)

		forms, err := guardedScratchRead(ctx, s, "(+ 1 2)")
		if err != nil {
			t.Fatalf("reuse after a failed read: %v", err)
		}
		if len(forms) != 1 || forms[0].String() != "(+ 1 2)" {
			t.Fatalf("reuse after a failed read returned %v, want [(+ 1 2)]", forms)
		}
		if !s.release(readerScratchNoCeiling) {
			t.Fatal("a scratch whose read succeeded refused the pool")
		}
		assertScratchCleared(t, s)
	})

	t.Run("terminal-failure/drops-without-traversing", func(t *testing.T) {
		src := "(" + strings.Repeat("a ", 2000) + ")"
		base, meter := budgetContext(DefaultMaxReductions)
		ctx := cancelAtReductions{Context: base, meter: meter, at: 64}
		s := &readerScratch{}

		if _, err := guardedScratchRead(ctx, s, src); !errors.Is(err, context.Canceled) {
			t.Fatalf("read under a context cancelled at 64 reductions returned %v, want context.Canceled", err)
		}
		before := chargedReductions(meter)

		if s.release(readerScratchNoCeiling) {
			t.Error("a cancelled read returned its scratch to the pool, want it dropped")
		}
		if got := chargedReductions(meter) - before; got != 0 {
			t.Errorf("dropping a cancelled read's scratch charged %d reductions, want none: a failed read traverses nothing", got)
		}
	})

	t.Run("exhausted-budget/drops-without-traversing", func(t *testing.T) {
		src := "(" + strings.Repeat("a ", 2000) + ")"
		ctx, meter := budgetContext(256)
		s := &readerScratch{}

		if _, err := guardedScratchRead(ctx, s, src); readErrorCode(err) != CodeResourceLimit {
			t.Fatalf("read under a 256-reduction budget returned %v, want a %s", err, CodeResourceLimit)
		}
		before := chargedReductions(meter)

		if s.release(readerScratchNoCeiling) {
			t.Error("an exhausted read returned its scratch to the pool, want it dropped")
		}
		if got := chargedReductions(meter) - before; got != 0 {
			t.Errorf("dropping an exhausted read's scratch charged %d reductions, want none", got)
		}
	})
}

func TestGuardedRead_RetainedASTSurvivesScratchReuse(t *testing.T) {
	t.Run("tree/unchanged-across-two-releases", func(t *testing.T) {
		ctx, _ := allocCeilingContext(DefaultMaxAllocationBytes)
		s := &readerScratch{}

		formsA, err := guardedScratchRead(ctx, s, `(alpha "kept" [1 2 3] {:k "v"})`)
		if err != nil {
			t.Fatalf("read A: %v", err)
		}
		wantA := formsA[0].String()
		if !s.release(readerScratchNoCeiling) {
			t.Fatal("read A's scratch was dropped, want it cleared and pooled")
		}
		if got := formsA[0].String(); got != wantA {
			t.Fatalf("release mutated the tree it returned: got %s, want %s", got, wantA)
		}

		formsB, err := guardedScratchRead(ctx, s, `(beta "other" [4 5 6] {:k "w"})`)
		if err != nil {
			t.Fatalf("read B: %v", err)
		}
		if !s.release(readerScratchNoCeiling) {
			t.Fatal("read B's scratch was dropped, want it cleared and pooled")
		}
		if formsB[0].String() == wantA {
			t.Fatalf("test invalid: source B produced the same tree as A")
		}
		if got := formsA[0].String(); got != wantA {
			t.Errorf("read A's tree changed after the scratch was cleared and reused: got %s, want %s", got, wantA)
		}
	})

	t.Run("aliased-literal/still-shares-the-source", func(t *testing.T) {
		const src = `"kept"`
		ctx, _ := allocCeilingContext(DefaultMaxAllocationBytes)
		s := &readerScratch{}

		forms, err := guardedScratchRead(ctx, s, src)
		if err != nil {
			t.Fatalf("read of a string literal: %v", err)
		}
		lit, ok := forms[0].(String)
		if !ok {
			t.Fatalf("forms[0] = %T, want String", forms[0])
		}
		if !s.release(readerScratchNoCeiling) {
			t.Fatal("the literal's scratch was dropped, want it cleared and pooled")
		}

		if _, err := guardedScratchRead(ctx, s, `"other"`); err != nil {
			t.Fatalf("reuse after the literal read: %v", err)
		}
		if !s.release(readerScratchNoCeiling) {
			t.Fatal("the reused scratch was dropped, want it cleared and pooled")
		}

		if lit.V != "kept" {
			t.Errorf("literal read %q after the scratch was cleared and reused, want %q", lit.V, "kept")
		}
		if unsafe.StringData(lit.V) != unsafe.StringData(src[1:5]) {
			t.Error("literal stopped sharing the caller's source string: release touched storage the returned value owns")
		}
	})
}

func TestGuardedRead_ConcurrentCrossDialectReads(t *testing.T) {
	withBrackets := FullDialect()
	withoutBrackets := FullDialect().WithoutBracketLiterals()
	wide := "(" + strings.Repeat("a ", 400) + ")"

	const iterations = 200
	var wg sync.WaitGroup
	errs := make(chan error, 3*iterations)

	readBrackets := func() {
		defer wg.Done()
		ctx, _ := allocCeilingContext(DefaultMaxAllocationBytes)
		forms, _, err := withBrackets.ReadWithContextStats(ctx, `[1 2 3]`, 0)
		if err != nil {
			errs <- fmt.Errorf("brackets-on dialect: unexpected error: %w", err)
			return
		}
		v, ok := forms[0].(Vector)
		if !ok || v.Len() != 3 {
			errs <- fmt.Errorf("brackets-on dialect: forms[0] = %#v, want a 3-element Vector", forms[0])
			return
		}
		if got := forms[0].String(); got != "[1 2 3]" {
			errs <- fmt.Errorf("brackets-on dialect: read %s, want [1 2 3]", got)
		}
	}
	readNoBrackets := func() {
		defer wg.Done()
		ctx, _ := allocCeilingContext(DefaultMaxAllocationBytes)
		forms, _, err := withoutBrackets.ReadWithContextStats(ctx, `[1 2 3]`, 0)
		if err == nil {
			errs <- fmt.Errorf("brackets-off dialect: read %v, want an error", forms)
		}
	}
	// A read whose allowance cannot hold the form it is given is refused, so
	// its scratch is dropped rather than cleared. Running it alongside the two
	// succeeding dialects puts both release outcomes on the shared pool at once.
	readRefused := func() {
		defer wg.Done()
		ctx, _ := allocCeilingContext(512)
		_, _, err := withBrackets.ReadWithContextStats(ctx, wide, 0)
		if code := readErrorCode(err); code != CodeResourceLimit {
			errs <- fmt.Errorf("low-allowance read returned %v (code %q), want a %s", err, code, CodeResourceLimit)
		}
	}

	for i := 0; i < iterations; i++ {
		wg.Add(3)
		go readBrackets()
		go readNoBrackets()
		go readRefused()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
