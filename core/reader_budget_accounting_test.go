package core

import (
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

		want := planBytes(1) + 2*MeterValueSlotBytes
		if got := quoted - bare; got != want {
			t.Fatalf("'a admitted %d bytes more than a, want %d for the quote token plus the two slots of the generated (quote a) node",
				got, want)
		}
	})

	t.Run("parser-workspace-doubling", func(t *testing.T) {
		wide := admittedForSource(t, listSource(5))
		narrow := admittedForSource(t, listSource(4))

		want := planBytes(1) + MeterValueSlotBytes + 8*MeterValueSlotBytes
		if got := wide - narrow; got != want {
			t.Fatalf("a 5-child list admitted %d bytes more than a 4-child one, want %d for its token, its copied slot, and the whole doubled 8-slot parser buffer",
				got, want)
		}
	})
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
			MeterCollectionHeaderBytes + 9*MeterHashMapEntryBytes,
			"the trie nodes holding 9 entries",
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
// must build the trie directly: the Go-map branch of Set cannot expose a
// checkpoint while it hashes and grows.
func TestGuardedRead_MapConstructionContracts(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantLen int
		trie    bool
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
			if !tc.trie {
				if m.large != nil {
					t.Fatalf("a %d-entry map is in large form, want the sorted small form", m.Len())
				}
				return
			}
			if m.large == nil {
				t.Fatalf("a %d-entry map is still in small form, want it promoted", m.Len())
			}
			if m.large.root == nil || m.large.m != nil {
				t.Fatalf("a promoted map built through the Go-map branch of Set, want it built directly into the trie")
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
// leaves on the ledger — only the unpaid delta — and that ReaderStats still
// reports the final output totals rather than the ledger's view of them.
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
		t.Fatalf("a prepaid payload admitted %d bytes, want the %d an equal-length zero-copy read pays: the copy is credited when its node lands",
			got, want)
	}
}
