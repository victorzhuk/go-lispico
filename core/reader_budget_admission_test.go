package core

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
)

// readerTokenPlanBytes is the deterministic workspace unit the counting pass
// reserves for one planned token, the terminal EOF token included. It is a
// budgeting unit, not a measurement of the Go token struct.
const readerTokenPlanBytes int64 = 32

// invalidNumberSourceBytes is the most source a guarded invalid-number
// diagnostic may render; a longer token is truncated to it.
const invalidNumberSourceBytes = 128

func planBytes(tokens int64) int64 { return tokens * readerTokenPlanBytes }

// conversionBytes is the deterministic temporary-storage charge a numeric
// token of n bytes carries into strconv: the conversion and error token copies
// plus bounded diagnostic storage.
func conversionBytes(n int64) int64 { return 2*n + 256 }

// workBufferBytes is the value-slot storage a reader work buffer — the parser
// node scratch or the top-level form buffer — charges to reach a logical
// capacity of n entries: an initial required slot, then every doubled buffer
// charged whole before the growth that fills it. The schedule is logical, so
// retained physical capacity never discounts it.
func workBufferBytes(n int) int64 {
	total, capacity := 0, 0
	for capacity < n {
		if capacity == 0 {
			capacity = 1
		} else {
			capacity *= 2
		}
		total += capacity
	}
	return ValueSlotsBytes(total)
}

// flatFormBytes is the construction storage one top-level flat collection of
// the given number of children admits beyond its token plan: the form buffer
// holding the finished value, the node scratch its children stack on, and the
// slots they are copied into.
func flatFormBytes(children int) int64 {
	return workBufferBytes(1) + workBufferBytes(children) + ValueSlotsBytes(children)
}

// allocCeilingContext builds a context whose ledger carries an ample reduction
// budget and exactly maxAllocBytes of allocation allowance, so a read under it
// fails on storage admission and never on reader work.
func allocCeilingContext(maxAllocBytes int64) (context.Context, EvalMeter) {
	ctx := WithEvalResourceLimits(context.Background(), int(DefaultMaxReductions), int(maxAllocBytes))
	return ctx, EvalMeterFrom(ctx)
}

func admittedBytes(m EvalMeter) int64 { return m.Snapshot().AllocationBytes }

// readOwnedScratch drives a scratch the test owns instead of one the shared
// pool hands out, so a case can read twice through the same retained buffers
// rather than assume which entry sync.Pool returns.
func readOwnedScratch(ctx context.Context, s *readerScratch, src string) ([]Value, ReaderStats, error) {
	s.Reset()
	s.budget = newReaderBudget(ctx)
	defer func() { s.budget = nil }()

	if err := s.budget.checkpoint(); err != nil {
		return nil, ReaderStats{}, err
	}
	return s.read(src, FullDialect().readerFlags(), 0)
}

func TestGuardedRead_TokenPlanAdmission(t *testing.T) {
	wide := "(" + strings.Repeat("a ", 4000) + ")"
	const wideTokens = 4003

	t.Run("wide-flat-form/rejected-before-the-token-slice", func(t *testing.T) {
		ctx, meter := allocCeilingContext(1024)

		_, _, err := readContextStats(ctx, FullDialect(), wide, 0)
		if code := readErrorCode(err); code != CodeResourceLimit {
			t.Fatalf("read of a %d-token flat form needing %d bytes under a 1024-byte ceiling returned %v (code %q), want a %s",
				wideTokens, planBytes(wideTokens), err, code, CodeResourceLimit)
		}
		if got, limit := chargedReductions(meter), int64(len(wide)); got >= limit {
			t.Fatalf("rejected read charged %d reductions, want fewer than %d: counting must stop as soon as the plan cannot fit", got, limit)
		}
	})

	t.Run("long-trivia/costs-no-token-storage", func(t *testing.T) {
		src := strings.Repeat("; note about the form\n", 3000) + "(a)"
		ctx, meter := allocCeilingContext(1024)

		forms, _, err := readContextStats(ctx, FullDialect(), src, 0)
		if err != nil {
			t.Fatalf("read of 4 tokens behind %d bytes of trivia failed: %v", len(src), err)
		}
		if len(forms) != 1 {
			t.Fatalf("read %d forms, want 1", len(forms))
		}
		if got, want := admittedBytes(meter), planBytes(4)+flatFormBytes(1); got != want {
			t.Fatalf("admitted %d bytes, want %d for the 4-token plan and the construction of a 1-child list: trivia carries no token storage", got, want)
		}
	})

	t.Run("long-token/costs-one-workspace-unit", func(t *testing.T) {
		src := strings.Repeat("s", 8000)
		ctx, meter := allocCeilingContext(1024)

		forms, _, err := readContextStats(ctx, FullDialect(), src, 0)
		if err != nil {
			t.Fatalf("read of one %d-byte symbol failed: %v", len(src), err)
		}
		if len(forms) != 1 {
			t.Fatalf("read %d forms, want 1", len(forms))
		}
		if got, want := admittedBytes(meter), planBytes(2)+workBufferBytes(1); got != want {
			t.Fatalf("admitted %d bytes for a symbol and an EOF token, want %d for their plan and the top-level form buffer: token storage is per token, not per byte", got, want)
		}
	})

	t.Run("exact-ceiling/admits-the-plan", func(t *testing.T) {
		want := planBytes(6) + flatFormBytes(3)
		ctx, meter := allocCeilingContext(want)

		forms, _, err := readContextStats(ctx, FullDialect(), "(a b c)", 0)
		if err != nil {
			t.Fatalf("read under a ceiling of exactly %d bytes failed: %v", want, err)
		}
		if len(forms) != 1 {
			t.Fatalf("read %d forms, want 1", len(forms))
		}
		if got := admittedBytes(meter); got != want {
			t.Fatalf("admitted %d bytes for 6 tokens and a 3-child list, want %d", got, want)
		}
	})

	t.Run("one-byte-below/rejects-the-plan", func(t *testing.T) {
		want := planBytes(6) + flatFormBytes(3)
		ctx, _ := allocCeilingContext(want - 1)

		_, _, err := readContextStats(ctx, FullDialect(), "(a b c)", 0)
		if code := readErrorCode(err); code != CodeResourceLimit {
			t.Fatalf("read of a %d-byte plan and construction under a %d-byte ceiling returned %v (code %q), want a %s",
				want, want-1, err, code, CodeResourceLimit)
		}
	})
}

func TestGuardedRead_PoolReuseKeepsPayingThePlan(t *testing.T) {
	src := "(a b c)"
	ctx, meter := allocCeilingContext(DefaultMaxAllocationBytes)
	s := &readerScratch{}

	for read := int64(1); read <= 2; read++ {
		forms, _, err := readOwnedScratch(ctx, s, src)
		if err != nil {
			t.Fatalf("read %d failed: %v", read, err)
		}
		if len(forms) != 1 {
			t.Fatalf("read %d returned %d forms, want 1", read, len(forms))
		}
		if got, want := admittedBytes(meter), read*(planBytes(6)+flatFormBytes(3)); got != want {
			t.Fatalf("after read %d the scratch admitted %d bytes, want %d: a reused buffer pays the same plan and construction", read, got, want)
		}
	}
}

func TestGuardedRead_UnreadSuffixDoesNotRaiseAdmission(t *testing.T) {
	prefix := "(" + strings.Repeat("a ", 4000) + ")"

	admittedFor := func(t *testing.T, suffix string) int64 {
		t.Helper()
		ctx, meter := allocCeilingContext(1024)
		_, _, err := readOwnedScratch(ctx, &readerScratch{}, prefix+suffix)
		if code := readErrorCode(err); code != CodeResourceLimit {
			t.Fatalf("read with a %d-byte suffix returned %v (code %q), want a %s", len(suffix), err, code, CodeResourceLimit)
		}
		return admittedBytes(meter)
	}

	short := strings.Repeat(" x", 500)
	long := strings.Repeat(" x", 50000)

	if got, want := admittedFor(t, long), admittedFor(t, short); got != want {
		t.Fatalf("admitted %d bytes with a %d-byte suffix and %d with a %d-byte one, want the unread suffix to cost nothing",
			got, len(long), want, len(short))
	}
}

func TestGuardedRead_MalformedSuffixAdmission(t *testing.T) {
	const malformed = "1.2.3"
	src := "(a b c) " + malformed

	_, _, legacyErr := FullDialect().ReadWithMaxDepthStats(src, 0)
	if legacyErr == nil {
		t.Fatalf("legacy read of %q succeeded, want a read error on the malformed suffix", src)
	}

	ctx, meter := allocCeilingContext(DefaultMaxAllocationBytes)
	_, _, err := readContextStats(ctx, FullDialect(), src, 0)
	if got, want := readErrorCode(err), readErrorCode(legacyErr); got != want {
		t.Fatalf("guarded read returned %v (code %q), want the legacy code %q", err, got, want)
	}

	want := planBytes(7) + flatFormBytes(3) + conversionBytes(int64(len(malformed)))
	if got := admittedBytes(meter); got != want {
		t.Fatalf("admitted %d bytes, want %d for 7 tokens, the 3-child list built before the suffix, and the refused conversion", got, want)
	}
}

func TestGuardedRead_EscapedPayloadAdmission(t *testing.T) {
	escaped := `"` + strings.Repeat("a", 5000) + `\n` + strings.Repeat("b", 5000) + `"`

	t.Run("reserves-the-payload-before-decoding", func(t *testing.T) {
		ctx, meter := allocCeilingContext(1024)

		_, _, err := readContextStats(ctx, FullDialect(), escaped, 0)
		if code := readErrorCode(err); code != CodeResourceLimit {
			t.Fatalf("read of a 10001-byte decoded payload under a 1024-byte ceiling returned %v (code %q), want a %s", err, code, CodeResourceLimit)
		}
		if got, limit := chargedReductions(meter), 2*int64(len(escaped)); got >= limit {
			t.Fatalf("rejected read charged %d reductions, want fewer than %d: the payload must be reserved before the decoding pass runs", got, limit)
		}
	})

	t.Run("credits-the-copy-when-the-node-lands", func(t *testing.T) {
		ctx, meter := allocCeilingContext(DefaultMaxAllocationBytes)

		forms, _, err := readContextStats(ctx, FullDialect(), `"a\nb"`, 0)
		if err != nil {
			t.Fatalf("read failed: %v", err)
		}
		if len(forms) != 1 {
			t.Fatalf("read %d forms, want 1", len(forms))
		}
		if got, want := admittedBytes(meter), planBytes(2)+workBufferBytes(1); got != want {
			t.Fatalf("admitted %d bytes for an escaped string, want %d for its plan and form buffer: the copy charge is credited when the string node is created", got, want)
		}
	})

	t.Run("zero-copy-payload-gains-no-copy-charge", func(t *testing.T) {
		ctx, meter := allocCeilingContext(DefaultMaxAllocationBytes)

		forms, _, err := readContextStats(ctx, FullDialect(), `"ab"`, 0)
		if err != nil {
			t.Fatalf("read failed: %v", err)
		}
		if len(forms) != 1 {
			t.Fatalf("read %d forms, want 1", len(forms))
		}
		if got, want := admittedBytes(meter), planBytes(2)+workBufferBytes(1); got != want {
			t.Fatalf("admitted %d bytes for a zero-copy string, want %d for its plan and form buffer: an aliased payload is never copied", got, want)
		}
	})
}

func TestGuardedRead_NumericConversionStorage(t *testing.T) {
	const src = "12345"
	want := planBytes(2) + workBufferBytes(1) + conversionBytes(int64(len(src)))

	t.Run("charges-the-temporary-on-success", func(t *testing.T) {
		ctx, meter := allocCeilingContext(DefaultMaxAllocationBytes)

		forms, _, err := readContextStats(ctx, FullDialect(), src, 0)
		if err != nil {
			t.Fatalf("read failed: %v", err)
		}
		if len(forms) != 1 {
			t.Fatalf("read %d forms, want 1", len(forms))
		}
		if got := admittedBytes(meter); got != want {
			t.Fatalf("admitted %d bytes for a successful conversion, want %d: admission is pre-paid whether or not the conversion succeeds", got, want)
		}
	})

	t.Run("exact-ceiling-admits", func(t *testing.T) {
		ctx, _ := allocCeilingContext(want)

		if _, _, err := readContextStats(ctx, FullDialect(), src, 0); err != nil {
			t.Fatalf("read under a ceiling of exactly %d bytes failed: %v", want, err)
		}
	})

	t.Run("one-byte-below-rejects", func(t *testing.T) {
		ctx, _ := allocCeilingContext(want - 1)

		_, _, err := readContextStats(ctx, FullDialect(), src, 0)
		if code := readErrorCode(err); code != CodeResourceLimit {
			t.Fatalf("read needing %d bytes under a %d-byte ceiling returned %v (code %q), want a %s", want, want-1, err, code, CodeResourceLimit)
		}
	})

	t.Run("overflowing-token-refused-before-conversion", func(t *testing.T) {
		long := strings.Repeat("9", 400)
		ctx, _ := allocCeilingContext(1024)

		_, _, err := readContextStats(ctx, FullDialect(), long, 0)
		if code := readErrorCode(err); code != CodeResourceLimit {
			t.Fatalf("read of a %d-digit overflowing number needing %d bytes under a 1024-byte ceiling returned %v (code %q), want a %s before conversion",
				len(long), conversionBytes(int64(len(long))), err, code, CodeResourceLimit)
		}
	})
}

// TestReaderPlanArithmetic_RefusesOverflow pins the checked arithmetic behind
// the plan and conversion charges. The magnitudes it refuses are unreachable
// through a source string, so the helpers are driven directly.
func TestReaderPlanArithmetic_RefusesOverflow(t *testing.T) {
	t.Run("token-plan", func(t *testing.T) {
		const maxTokens = math.MaxInt64 / readerTokenPlanBytes

		cases := []struct {
			name   string
			tokens int64
			want   int64
			ok     bool
		}{
			{"ordinary", 6, planBytes(6), true},
			{"zero", 0, 0, true},
			{"largest-fitting-plan", maxTokens, maxTokens * readerTokenPlanBytes, true},
			{"one-token-past-the-ceiling", maxTokens + 1, 0, false},
			{"max-int", math.MaxInt64, 0, false},
			{"negative", -1, 0, false},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				got, ok := checkedTokenPlanBytes(tc.tokens)
				if ok != tc.ok {
					t.Fatalf("checkedTokenPlanBytes(%d) admitted = %v, want %v", tc.tokens, ok, tc.ok)
				}
				if ok && got != tc.want {
					t.Fatalf("checkedTokenPlanBytes(%d) = %d bytes, want %d", tc.tokens, got, tc.want)
				}
			})
		}
	})

	t.Run("conversion", func(t *testing.T) {
		const maxTokenBytes = (math.MaxInt64 - 256) / 2

		cases := []struct {
			name       string
			tokenBytes int64
			want       int64
			ok         bool
		}{
			{"ordinary", 5, conversionBytes(5), true},
			{"zero", 0, conversionBytes(0), true},
			{"largest-fitting-token", maxTokenBytes, 2*maxTokenBytes + 256, true},
			{"diagnostic-storage-overflows", math.MaxInt64 / 2, 0, false},
			{"doubling-overflows", math.MaxInt64/2 + 1, 0, false},
			{"negative", -1, 0, false},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				got, ok := checkedConversionBytes(tc.tokenBytes)
				if ok != tc.ok {
					t.Fatalf("checkedConversionBytes(%d) admitted = %v, want %v", tc.tokenBytes, ok, tc.ok)
				}
				if ok && got != tc.want {
					t.Fatalf("checkedConversionBytes(%d) = %d bytes, want %d", tc.tokenBytes, got, tc.want)
				}
			})
		}
	})
}

func TestGuardedRead_InvalidNumberDiagnosticIsBounded(t *testing.T) {
	tok := strings.Repeat("9", 300)

	_, _, legacyErr := FullDialect().ReadWithMaxDepthStats(tok, 0)
	var legacy *LispicoError
	if !errors.As(legacyErr, &legacy) {
		t.Fatalf("legacy read returned %v, want a *LispicoError", legacyErr)
	}
	if !strings.Contains(legacy.Message, tok) {
		t.Fatalf("legacy diagnostic %q no longer renders the whole token, want the context-free diagnostic unchanged", legacy.Message)
	}

	ctx, _ := allocCeilingContext(DefaultMaxAllocationBytes)
	_, _, err := readContextStats(ctx, FullDialect(), tok, 0)
	var got *LispicoError
	if !errors.As(err, &got) {
		t.Fatalf("guarded read returned %v, want a *LispicoError", err)
	}
	if got.Code != legacy.Code {
		t.Fatalf("guarded diagnostic code = %q, want the legacy kind %q", got.Code, legacy.Code)
	}
	if got.Line != legacy.Line || got.Col != legacy.Col {
		t.Fatalf("guarded diagnostic reported %d:%d, want the legacy position %d:%d", got.Line, got.Col, legacy.Line, legacy.Col)
	}
	if strings.Contains(got.Message, tok) {
		t.Fatalf("guarded diagnostic renders all %d source bytes, want at most %d with a truncation marker", len(tok), invalidNumberSourceBytes)
	}
	dropped := len(legacy.Message) - len(got.Message)
	if wantDrop := len(tok) - invalidNumberSourceBytes; dropped < wantDrop {
		t.Fatalf("guarded diagnostic dropped %d bytes of the %d-byte token, want at least %d so at most %d source bytes render",
			dropped, len(tok), wantDrop, invalidNumberSourceBytes)
	}
}
