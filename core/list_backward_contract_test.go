package core

import (
	"context"
	"errors"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// backwardBoundarySizes walks the flat/chain threshold: 32 stays flat, 33
// links the first shared-tail chain, the larger sizes exercise longer chains.
var backwardBoundarySizes = []int{0, 1, 32, 33, 128, 129, 257}

func backwardFixtureItems(n int) ([]Value, string) {
	items := make([]Value, n)
	var src strings.Builder
	src.WriteByte('(')
	for i := range items {
		items[i] = Int{V: int64(i)}
		if i > 0 {
			src.WriteByte(' ')
		}
		src.WriteString(strconv.Itoa(i))
	}
	src.WriteByte(')')
	return items, src.String()
}

// assertChainShape checks forward order, Len, At, ToSlice, Rest and equality,
// and that every shared-tail node carries the exact remaining count with no
// flat slice alongside it.
func assertChainShape(t *testing.T, got List, items []Value) {
	t.Helper()

	if got.flat != nil && got.shared != nil {
		t.Fatalf("list holds flat and shared storage at once")
	}
	if got.Len() != len(items) {
		t.Fatalf("Len() = %d, want %d", got.Len(), len(items))
	}
	for i, want := range items {
		if v := got.At(i); !v.Equals(want) {
			t.Fatalf("At(%d) = %v, want %v", i, v, want)
		}
	}
	if sl := got.ToSlice(); len(sl) != len(items) {
		t.Fatalf("ToSlice() length = %d, want %d", len(sl), len(items))
	} else {
		for i, want := range items {
			if !sl[i].Equals(want) {
				t.Fatalf("ToSlice()[%d] = %v, want %v", i, sl[i], want)
			}
		}
	}
	if !got.Equals(NewList(items)) {
		t.Fatalf("list does not equal constructor-built list of %d items", len(items))
	}
	if wantRest := len(items) - 1; wantRest >= 0 {
		if rest := got.Rest(); rest.Len() != wantRest {
			t.Fatalf("Rest().Len() = %d, want %d", rest.Len(), wantRest)
		}
	}
	// Walk the chain once: every node counts the elements from itself to the
	// end, and the chain holds exactly one node per element.
	if got.shared != nil {
		nodes, want := 0, len(items)
		for node := got.shared; node != nil; node = node.tail {
			if node.count != want {
				t.Fatalf("node count = %d, want %d", node.count, want)
			}
			nodes++
			want--
		}
		if nodes != len(items) {
			t.Fatalf("chain linked %d nodes, want %d", nodes, len(items))
		}
	}
}

func TestList_BackwardConstruction(t *testing.T) {
	t.Parallel()

	for _, n := range backwardBoundarySizes {
		t.Run("len-"+strconv.Itoa(n), func(t *testing.T) {
			t.Parallel()

			items, src := backwardFixtureItems(n)

			l := NewList(items)
			wantFlat := n <= listFlatThreshold
			if (l.shared == nil) != wantFlat {
				t.Fatalf("NewList(%d items) storage form = shared=%v, want flat=%v", n, l.shared != nil, wantFlat)
			}
			assertChainShape(t, l, items)

			forms, _, err := readContextStats(context.Background(), Dialect{}, src, 0)
			if err != nil {
				t.Fatalf("read %q: %v", src, err)
			}
			if len(forms) != 1 {
				t.Fatalf("read produced %d forms, want 1", len(forms))
			}
			readList, ok := forms[0].(List)
			if !ok {
				t.Fatalf("read form is %T, want List", forms[0])
			}
			if (readList.shared == nil) != wantFlat {
				t.Fatalf("reader-built list(%d items) storage form disagrees with NewList", n)
			}
			assertChainShape(t, readList, items)
		})
	}
}

func TestList_BackwardLinkCheckpoint(t *testing.T) {
	t.Parallel()

	ctx, meter := budgetContext(DefaultMaxReductions)
	cancelCtx := cancelAtReductions{Context: ctx, meter: meter, at: 0}

	// Parser.buildList on 800 items: admission charges the cells without
	// consulting the context, then the first linking checkpoint after 128
	// cells observes the cancellation.
	items, _ := backwardFixtureItems(800)
	p := NewParser(nil)
	p.budget = newReaderBudget(cancelCtx)
	v, err := p.buildList(items)
	if v != nil {
		t.Fatalf("buildList returned a value, want nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("buildList error = %v, want context.Canceled", err)
	}
	if err != context.Canceled {
		t.Fatalf("buildList error identity: %T, want bare context.Canceled", err)
	}

	// newGuardedListChain at 127 items links the whole chain: no checkpoint
	// fires below 128 cells.
	shortItems, _ := backwardFixtureItems(127)
	chain, err := newGuardedListChain(shortItems, newReaderBudget(cancelCtx))
	if err != nil {
		t.Fatalf("newGuardedListChain(127) error = %v", err)
	}
	if chain == nil || chain.count != 127 {
		t.Fatalf("newGuardedListChain(127) chain = %v, want full 127-cell chain", chain)
	}

	// At 128 items the first checkpoint fires after the 128th cell and the
	// builder returns no partial chain.
	fullItems, _ := backwardFixtureItems(128)
	chain, err = newGuardedListChain(fullItems, newReaderBudget(cancelCtx))
	if chain != nil {
		t.Fatalf("newGuardedListChain(128) returned a chain, want nil")
	}
	if err != context.Canceled {
		t.Fatalf("newGuardedListChain(128) error = %v, want context.Canceled", err)
	}
}

// linkProbeContext returns context.Canceled only when the call stack carries
// the core.newGuardedListChain frame; otherwise it delegates to the parent.
// It counts the targeted calls so the test pins the checkpoint to exactly one
// cancellation observation.
type linkProbeContext struct {
	context.Context
	hits int
}

const linkingFrame = "github.com/victorzhuk/go-lispico/core.newGuardedListChain"

func (c *linkProbeContext) inLinkingFrame() bool {
	var pc [64]uintptr
	n := runtime.Callers(2, pc[:])
	frames := runtime.CallersFrames(pc[:n])
	for {
		f, more := frames.Next()
		if f.Function == linkingFrame {
			return true
		}
		if !more {
			return false
		}
	}
}

func (c *linkProbeContext) Err() error {
	if c.inLinkingFrame() {
		c.hits++
		return context.Canceled
	}
	return c.Context.Err()
}

func TestReader_BackwardLinkCancellation(t *testing.T) {
	t.Parallel()

	ctx := &linkProbeContext{Context: context.Background()}
	src := "(" + strings.Repeat("item ", 800) + ")"

	forms, _, err := readContextStats(ctx, Dialect{}, src, 0)
	if len(forms) != 0 {
		t.Fatalf("read returned %d forms, want 0", len(forms))
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("read error = %v, want context.Canceled", err)
	}
	if ctx.hits != 1 {
		t.Fatalf("linking checkpoint observed %d cancellations, want 1", ctx.hits)
	}
}
