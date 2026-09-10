package core

import (
	"context"
	"strconv"
	"strings"
	"testing"
)

// listCellBytes is the construction storage one persistent list cell costs
// once a list outgrows its flat form and is linked into a shared-tail chain.
const listCellBytes int64 = 32

func listSource(items int) string   { return "(" + strings.Repeat("a ", items) + ")" }
func vectorSource(items int) string { return "[" + strings.Repeat("a ", items) + "]" }

// pairSource builds a map literal of distinct keyword keys and symbol values,
// so nothing but collection construction is admitted on top of the token plan.
func pairSource(pairs int) string {
	var b strings.Builder
	b.WriteByte('{')
	for i := 0; i < pairs; i++ {
		b.WriteString(":k")
		b.WriteString(strconv.Itoa(i))
		b.WriteString(" v ")
	}
	b.WriteByte('}')
	return b.String()
}

// sameKeySource repeats one key, so every insert past the first rewrites an
// entry the map already holds.
func sameKeySource(pairs int) string {
	return "{" + strings.Repeat(":dup v ", pairs) + "}"
}

// promotionControlSource is pairSource(9) with its ninth key repeating the
// first: same token count, same key and value payloads, same output nodes, and
// one entry short of the small-map limit. Differencing the two isolates what
// promoting the ninth key admits from everything else the read pays.
func promotionControlSource() string {
	var b strings.Builder
	b.WriteByte('{')
	for i := 0; i < hashMapSmallLimit; i++ {
		b.WriteString(":k")
		b.WriteString(strconv.Itoa(i))
		b.WriteString(" v ")
	}
	b.WriteString(":k0 v ")
	b.WriteByte('}')
	return b.String()
}

// entryBufferBytes is the storage a small map's entry buffer charges to hold
// the given number of entries: the doubling schedule the reader work buffers
// follow, in entry units rather than value slots.
func entryBufferBytes(entries int64) int64 {
	total, capacity := int64(0), int64(0)
	for capacity < entries {
		if capacity == 0 {
			capacity = 1
		} else {
			capacity *= 2
		}
		total += capacity
	}
	return total * MeterHashMapEntryBytes
}

// allocationProbe records the allocation ledger at every terminal-state check
// the read makes, which is where a charge returned to the ledger would show.
type allocationProbe struct {
	context.Context
	meter EvalMeter
	seen  []int64
}

func (p *allocationProbe) Err() error {
	p.seen = append(p.seen, admittedBytes(p.meter))
	return p.Context.Err()
}

func admittedForSource(t *testing.T, src string) int64 {
	t.Helper()
	ctx, meter := allocCeilingContext(DefaultMaxAllocationBytes)
	if _, _, err := readContextStats(ctx, FullDialect(), src, 0); err != nil {
		t.Fatalf("read of %d bytes failed: %v", len(src), err)
	}
	return admittedBytes(meter)
}

// TestGuardedRead_ExactCharges pins the construction charges that differ
// between two reads sharing every other term, so the expected value is the
// charge under test alone rather than a whole-read model.
func TestGuardedRead_ExactCharges(t *testing.T) {
	t.Run("linked-list-cells", func(t *testing.T) {
		const items = 40
		list := admittedForSource(t, listSource(items))
		vector := admittedForSource(t, vectorSource(items))

		want := int64(items) * listCellBytes
		if got := list - vector; got != want {
			t.Fatalf("a %d-element list admitted %d bytes more than the identical vector, want %d for the %d cells it links",
				items, got, want, items)
		}
	})

	t.Run("flat-list-under-the-threshold", func(t *testing.T) {
		const items = listFlatThreshold
		list := admittedForSource(t, listSource(items))
		vector := admittedForSource(t, vectorSource(items))

		if got := list - vector; got != 0 {
			t.Fatalf("a %d-element flat list admitted %d bytes more than the identical vector, want 0: a flat list links no cells",
				items, got)
		}
	})

	t.Run("generated-quote-node", func(t *testing.T) {
		quoted := admittedForSource(t, "'a")
		bare := admittedForSource(t, "a")

		want := planBytes(1) + 2*MeterValueSlotBytes + 2*MeterReaderNodeBytes + int64(len("quote"))
		if got := quoted - bare; got != want {
			t.Fatalf("'a admitted %d bytes more than a, want %d for the quote token, the two slots of the generated (quote a) node, and the two output nodes it wraps them in — the quote symbol carrying its 5-byte payload and the list carrying none",
				got, want)
		}
	})

	t.Run("parser-workspace-doubling", func(t *testing.T) {
		wide := admittedForSource(t, listSource(5))
		narrow := admittedForSource(t, listSource(4))

		want := planBytes(1) + MeterValueSlotBytes + 8*MeterValueSlotBytes + MeterReaderNodeBytes + int64(len("a"))
		if got := wide - narrow; got != want {
			t.Fatalf("a 5-child list admitted %d bytes more than a 4-child one, want %d for its token, its copied slot, the whole doubled 8-slot parser buffer, and the output node of the extra child",
				got, want)
		}
	})
}

// TestGuardedRead_AdmitsOutputStorage pins the whole allowance a successful
// read admits: the token plan, the parser workspace, the construction storage
// of the collections it builds, and the output nodes themselves.
func TestGuardedRead_AdmitsOutputStorage(t *testing.T) {
	cases := []struct {
		name   string
		src    string
		want   int64
		reason string
	}{
		{
			"flat-list", "(a b c)",
			planBytes(6) + flatFormBytes(3) + outputBytes(t, "(a b c)"),
			"6 tokens, the workspace and slots of a 3-child list, and its 4 output nodes",
		},
		{
			"generated-quote", "'a",
			planBytes(3) + workBufferBytes(1) + ValueSlotsBytes(2) + outputBytes(t, "'a"),
			"3 tokens, the form buffer, the two slots of (quote a), and its 3 output nodes",
		},
		{
			"zero-copy-string", `"ab"`,
			planBytes(2) + workBufferBytes(1) + outputBytes(t, `"ab"`),
			"2 tokens, the form buffer, and the string node carrying its aliased payload",
		},
		{
			"linked-list", listSource(40),
			planBytes(43) + flatFormBytes(40) + 40*listCellBytes + outputBytes(t, listSource(40)),
			"43 tokens, the doubling workspace, the copied slots, 40 linked cells, and 41 output nodes",
		},
		{
			"small-map", pairSource(4),
			planBytes(11) + workBufferBytes(1) + entryBufferBytes(4) + outputBytes(t, pairSource(4)),
			"11 tokens, the form buffer, the doubled 4-entry buffer, and 9 output nodes",
		},
		{
			"promoted-map", pairSource(9),
			planBytes(21) + workBufferBytes(1) + MeterCollectionHeaderBytes + entryBufferBytes(9) + outputBytes(t, pairSource(9)),
			"21 tokens, the form buffer, the promoted map's header, the doubled 9-entry buffer, and 19 output nodes",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, meter := allocCeilingContext(DefaultMaxAllocationBytes)
			_, stats, err := readContextStats(ctx, FullDialect(), tc.src, 0)
			if err != nil {
				t.Fatalf("read failed: %v", err)
			}
			if got := admittedBytes(meter); got != tc.want {
				t.Fatalf("admitted %d bytes, want %d for %s", got, tc.want, tc.reason)
			}
			if got, want := ReaderAllocationBytes(stats), outputBytes(t, tc.src); got != want {
				t.Fatalf("the read reports %d bytes of output storage, want the %d the context-free reader reports: the output term is admitted, not redefined",
					got, want)
			}
		})
	}
}

// TestGuardedRead_AdmittedBytesNeverDecrease holds the allocation ledger to a
// total that only grows. A payload reserved before decoding is charged once
// and never returned, so no terminal-state check may see a lower total than
// the one before it.
func TestGuardedRead_AdmittedBytesNeverDecrease(t *testing.T) {
	escaped := `"` + strings.Repeat("a", 5000) + `\n` + strings.Repeat("b", 5000) + `"`
	src := "(" + escaped + ` "plain" ` + pairSource(9) + " " + listSource(40) + ")"

	ctx, meter := allocCeilingContext(DefaultMaxAllocationBytes)
	probe := &allocationProbe{Context: ctx, meter: meter}

	if _, _, err := readContextStats(probe, FullDialect(), src, 0); err != nil {
		t.Fatalf("read of %d bytes failed: %v", len(src), err)
	}
	if len(probe.seen) < 2 {
		t.Fatalf("the read checked its terminal state %d times, want it checked while building so the ledger can be observed moving", len(probe.seen))
	}

	for i := 1; i < len(probe.seen); i++ {
		if probe.seen[i] < probe.seen[i-1] {
			t.Fatalf("the ledger fell from %d to %d bytes between checks %d and %d of %d, want a total that only grows: an admitted charge is never returned to the ledger",
				probe.seen[i-1], probe.seen[i], i-1, i, len(probe.seen))
		}
	}
	if got, last := admittedBytes(meter), probe.seen[len(probe.seen)-1]; got < last {
		t.Fatalf("the read settled at %d bytes below its last check of %d, want the final total to be the high point", got, last)
	}
}

// TestGuardedRead_WorkspaceHighWaterSpansNesting pins the parser node scratch
// as one schedule for the whole read: a nested collection's children stack on
// the outer collection's, so the charge follows the shared high-water mark.
func TestGuardedRead_WorkspaceHighWaterSpansNesting(t *testing.T) {
	src := "(" + pairSource(3) + " " + vectorSource(4) + ")"

	want := planBytes(17) + outputBytes(t, src) +
		workBufferBytes(1) + workBufferBytes(5) +
		entryBufferBytes(3) + ValueSlotsBytes(4) + ValueSlotsBytes(2)

	if got := admittedForSource(t, src); got != want {
		t.Fatalf("admitted %d bytes, want %d: the vector's 4 children stack on the map already held at index 0, so the shared node scratch reaches 5 slots (%d bytes) rather than 4 (%d)",
			got, want, workBufferBytes(5), workBufferBytes(4))
	}
}

// TestGuardedRead_ConstructionStorage pins the storage each collection form
// must admit on top of its token plan. Every want is the storage the finished
// collection holds, which the construction path can only exceed.
func TestGuardedRead_ConstructionStorage(t *testing.T) {
	cases := []struct {
		name   string
		src    string
		tokens int64
		want   int64
		reason string
	}{
		{"small-map", pairSource(4), 11, 4 * MeterHashMapEntryBytes, "4 sorted small-map entries"},
		{"small-map-at-the-limit", pairSource(8), 19, 8 * MeterHashMapEntryBytes, "8 sorted small-map entries"},
		{
			"promoted-map", pairSource(9), 21,
			MeterCollectionHeaderBytes + entryBufferBytes(9),
			"the promoted map's header and the entry buffer holding 9 entries",
		},
		{
			"duplicate-keys", sameKeySource(12), 27,
			MeterHashMapEntryBytes,
			"the entry buffer every repeated insert rewrites",
		},
		{
			"linked-list", listSource(40), 43,
			40*listCellBytes + 40*MeterValueSlotBytes,
			"40 linked cells and the 40 slots copied into them",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := admittedForSource(t, tc.src) - planBytes(tc.tokens)
			if got < tc.want {
				t.Fatalf("admitted %d bytes beyond the %d-token plan, want at least %d for %s",
					got, tc.tokens, tc.want, tc.reason)
			}
		})
	}
}

// TestGuardedRead_AllowanceBoundary pins the whole allowance a construction
// read needs: the read fits an allowance of exactly that many bytes and is
// refused one byte below it, so no checkpoint carries hidden overhead.
func TestGuardedRead_AllowanceBoundary(t *testing.T) {
	cases := []struct {
		name   string
		src    string
		tokens int64
	}{
		{"linked-list", listSource(40), 43},
		{"promoted-map", pairSource(9), 21},
		{"nested-collections", "(" + pairSource(3) + " " + vectorSource(4) + ")", 17},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			total := admittedForSource(t, tc.src)
			if plan := planBytes(tc.tokens); total <= plan {
				t.Fatalf("the whole allowance is %d bytes against a %d-token plan of %d, want construction storage admitted on top of it",
					total, tc.tokens, plan)
			}

			ctx, _ := allocCeilingContext(total)
			if _, _, err := readContextStats(ctx, FullDialect(), tc.src, 0); err != nil {
				t.Fatalf("read under an allowance of exactly its own %d bytes failed: %v", total, err)
			}

			ctx, _ = allocCeilingContext(total - 1)
			_, _, err := readContextStats(ctx, FullDialect(), tc.src, 0)
			if code := readErrorCode(err); code != CodeResourceLimit {
				t.Fatalf("read needing %d bytes under a %d-byte allowance returned %v (code %q), want a %s",
					total, total-1, err, code, CodeResourceLimit)
			}
		})
	}
}

// TestGuardedRead_PromotionRefusedBeforeStorage pins what promoting a map
// literal past the small-map limit costs and when that cost is settled: the
// ninth key admits the header plus the doubling step its entry buffer takes,
// and under an allowance of exactly what the same literal needs without
// promoting — one the control read just fits — the read is refused with no map
// and a ledger that never rises above the allowance, so the promotion alone is
// what refuses and the promoted storage never exists.
func TestGuardedRead_PromotionRefusedBeforeStorage(t *testing.T) {
	promoting, control := pairSource(9), promotionControlSource()

	t.Run("promotion-charge", func(t *testing.T) {
		want := MeterCollectionHeaderBytes + entryBufferBytes(9) - entryBufferBytes(hashMapSmallLimit)
		if got := admittedForSource(t, promoting) - admittedForSource(t, control); got != want {
			t.Fatalf("promoting the ninth key admitted %d bytes over the same literal rewriting a key it already holds, want %d: the header plus the doubling step the entry buffer takes to hold 9 entries",
				got, want)
		}
	})

	t.Run("refused-before-storage", func(t *testing.T) {
		ceiling := admittedForSource(t, control)
		ctx, meter := allocCeilingContext(ceiling)
		probe := &allocationProbe{Context: ctx, meter: meter}

		forms, _, err := readContextStats(probe, FullDialect(), promoting, 0)
		if code := readErrorCode(err); code != CodeResourceLimit {
			t.Fatalf("a promoting read under an allowance of %d bytes returned %v (code %q), want a %s: the promotion cannot fit and must be refused",
				ceiling, err, code, CodeResourceLimit)
		}
		if len(forms) != 0 {
			t.Fatalf("the refused read returned %d forms, want none: no map may exist once its storage is refused", len(forms))
		}
		if len(probe.seen) == 0 {
			t.Fatalf("the refused read never checked its terminal state, want the ledger observable while it builds")
		}
		for i, seen := range probe.seen {
			if seen > ceiling {
				t.Fatalf("the ledger stood at %d bytes at check %d of %d, above the %d-byte allowance, want every check the read survives taken under it",
					seen, i, len(probe.seen), ceiling)
			}
		}
		if got, full := admittedBytes(meter), admittedForSource(t, promoting); got >= full {
			t.Fatalf("the refused read admitted %d bytes, want less than the %d the same read pays under an ample allowance: the refusal is terminal at the promotion, so no storage past it is claimed",
				got, full)
		}
	})
}

// TestGuardedRead_PoolReuseChargesTheSameConstruction drives a scratch the
// test owns, so the second and third reads run against retained capacity
// rather than whichever entry sync.Pool would hand back.
func TestGuardedRead_PoolReuseChargesTheSameConstruction(t *testing.T) {
	const tokens = 43
	src := listSource(40)
	ctx, meter := allocCeilingContext(DefaultMaxAllocationBytes)
	s := &readerScratch{}

	var wantAlloc, wantWork, seenAlloc, seenWork int64
	for read := 1; read <= 3; read++ {
		if _, _, err := readOwnedScratch(ctx, s, src); err != nil {
			t.Fatalf("read %d failed: %v", read, err)
		}
		alloc := admittedBytes(meter) - seenAlloc
		work := chargedReductions(meter) - seenWork
		seenAlloc, seenWork = admittedBytes(meter), chargedReductions(meter)

		if read == 1 {
			wantAlloc, wantWork = alloc, work
			if plan := planBytes(tokens); alloc <= plan {
				t.Fatalf("read 1 admitted %d bytes against a token plan of %d, want construction storage admitted on top of it", alloc, plan)
			}
			continue
		}
		if alloc != wantAlloc {
			t.Fatalf("read %d admitted %d bytes, want the %d the first read paid: retained capacity earns no discount", read, alloc, wantAlloc)
		}
		if work != wantWork {
			t.Fatalf("read %d charged %d reductions, want the %d the first read paid: a partial flush carries no residual into the next read", read, work, wantWork)
		}
	}
}

// TestGuardedRead_MapConstructionContracts pins the storage form the guarded
// builder produces on both sides of the small-map limit. Past the limit it
// must promote into the same Go-map form HashMap.Set builds, so one content
// has one representation however it was produced.
func TestGuardedRead_MapConstructionContracts(t *testing.T) {
	cases := []struct {
		name     string
		src      string
		wantLen  int
		promoted bool
	}{
		{"small-form-at-the-limit", pairSource(8), 8, false},
		{"promoted-past-the-limit", pairSource(9), 9, true},
		{"duplicate-keys-collapse", sameKeySource(12), 1, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, _ := allocCeilingContext(DefaultMaxAllocationBytes)
			forms, _, err := readContextStats(ctx, FullDialect(), tc.src, 0)
			if err != nil {
				t.Fatalf("read failed: %v", err)
			}
			if len(forms) != 1 {
				t.Fatalf("read %d forms, want 1", len(forms))
			}
			m, ok := forms[0].(*HashMap)
			if !ok {
				t.Fatalf("read %T, want a *HashMap", forms[0])
			}
			if m.Len() != tc.wantLen {
				t.Fatalf("map holds %d entries, want %d", m.Len(), tc.wantLen)
			}
			if !tc.promoted {
				if m.large != nil {
					t.Fatalf("a %d-entry map is in large form, want the sorted small form", m.Len())
				}
				return
			}
			if m.large == nil {
				t.Fatalf("a %d-entry map is still in small form, want it promoted", m.Len())
			}
			if m.large.m == nil || m.large.root != nil {
				t.Fatalf("a promoted map built into the trie, want the Go-map form HashMap.Set builds")
			}
		})
	}
}

// TestGuardedRead_StatsUnchanged holds the guarded reader's output and stats
// to what the context-free reader reports on the same fixtures.
func TestGuardedRead_StatsUnchanged(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"linked-list", listSource(40)},
		{"flat-list", listSource(listFlatThreshold)},
		{"small-map", pairSource(8)},
		{"promoted-map", pairSource(9)},
		{"duplicate-keys", sameKeySource(12)},
		{"generated-quotes", "'(a b) `(c ~d ~@e)"},
		{"prepaid-payload", `("a\nb" "plain")`},
		{"nested-collections", "(" + pairSource(3) + " " + vectorSource(4) + ")"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wantForms, wantStats, wantErr := FullDialect().ReadWithMaxDepthStats(tc.src, 0)
			if wantErr != nil {
				t.Fatalf("legacy read failed: %v", wantErr)
			}

			ctx, _ := allocCeilingContext(DefaultMaxAllocationBytes)
			forms, stats, err := readContextStats(ctx, FullDialect(), tc.src, 0)
			if err != nil {
				t.Fatalf("read failed: %v", err)
			}
			if stats != wantStats {
				t.Fatalf("stats = %+v, legacy stats = %+v", stats, wantStats)
			}
			if len(forms) != len(wantForms) {
				t.Fatalf("read %d forms, legacy read %d", len(forms), len(wantForms))
			}
			for i, want := range wantForms {
				if !want.Equals(forms[i]) {
					t.Fatalf("form %d = %v, legacy form = %v", i, forms[i], want)
				}
			}
		})
	}
}

// TestGuardedRead_PrepaidPayloadKeepsOutputTotals pins what a prepaid payload
// leaves on the ledger: the payload is charged once, at reservation, and the
// node it becomes admits its node unit alone, so an escaped read costs exactly
// what an equal-length zero-copy one does. ReaderStats still reports the final
// output totals rather than the ledger's view of them.
func TestGuardedRead_PrepaidPayloadKeepsOutputTotals(t *testing.T) {
	const (
		escaped = `("a\nb")`
		plain   = `("axb")`
	)

	_, wantStats, wantErr := FullDialect().ReadWithMaxDepthStats(escaped, 0)
	if wantErr != nil {
		t.Fatalf("legacy read failed: %v", wantErr)
	}

	ctx, meter := allocCeilingContext(DefaultMaxAllocationBytes)
	_, stats, err := readContextStats(ctx, FullDialect(), escaped, 0)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if stats != wantStats {
		t.Fatalf("stats = %+v, legacy stats = %+v", stats, wantStats)
	}
	if stats.Bytes != 3 {
		t.Fatalf("stats report %d output bytes for a 3-byte decoded string, want the final output total", stats.Bytes)
	}

	if got, want := admittedBytes(meter), admittedForSource(t, plain); got != want {
		t.Fatalf("a prepaid payload admitted %d bytes, want the %d an equal-length zero-copy read pays: the reservation is the only charge the copy carries",
			got, want)
	}
}
