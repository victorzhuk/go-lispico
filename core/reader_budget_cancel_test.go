package core

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// budgetContext builds a context carrying an evaluation ledger with the given
// reduction ceiling and returns the meter that reads it back.
func budgetContext(maxReductions int64) (context.Context, EvalMeter) {
	ctx := WithEvalResourceLimits(context.Background(), int(maxReductions), int(DefaultMaxAllocationBytes))
	return ctx, EvalMeterFrom(ctx)
}

func chargedReductions(m EvalMeter) int64 { return m.Snapshot().Reductions }

// cancelAtReductions cancels once the ledger behind meter has been charged at
// least at reductions. Charged work, not wall time, decides where a read is
// interrupted, so every case lands in the same scanning or parsing phase on
// every machine.
type cancelAtReductions struct {
	context.Context
	meter EvalMeter
	at    int64
}

func (c cancelAtReductions) Err() error {
	if chargedReductions(c.meter) >= c.at {
		return context.Canceled
	}
	return c.Context.Err()
}

// reductionProbe records the ledger total at every terminal-state check the
// read makes, which is what bounds the work run between two checks.
type reductionProbe struct {
	context.Context
	meter EvalMeter
	seen  []int64
}

func (p *reductionProbe) Err() error {
	p.seen = append(p.seen, chargedReductions(p.meter))
	return p.Context.Err()
}

// stepClock pins the reader's clock to base for the first n readings and moves
// it an hour past base afterwards, so a deadline fires at a fixed number of
// checkpoints instead of after wall time.
func stepClock(t *testing.T, base time.Time, n int) {
	t.Helper()
	restore := nowFunc
	calls := 0
	nowFunc = func() time.Time {
		calls++
		if calls > n {
			return base.Add(time.Hour)
		}
		return base
	}
	t.Cleanup(func() { nowFunc = restore })
}

func readErrorCode(err error) string {
	var le *LispicoError
	if errors.As(err, &le) {
		return le.Code
	}
	return ""
}

func mixedSource(reps int) string {
	return strings.Repeat("(alpha [beta :gamma] \"delta\")\n", reps)
}

func longKeySource(keys int) string {
	var b strings.Builder
	b.WriteByte('{')
	for i := 0; i < keys; i++ {
		b.WriteString(":key-with-a-long-name-")
		b.WriteString(strings.Repeat("x", 20))
		b.WriteString(strings.Repeat("y", i%7))
		b.WriteString(" val ")
	}
	b.WriteByte('}')
	return b.String()
}

func collidingKeySource(pairs int) string {
	key := ":" + strings.Repeat("same-long-key-", 4)
	var b strings.Builder
	b.WriteByte('{')
	for i := 0; i < pairs; i++ {
		b.WriteString(key)
		b.WriteString(" val ")
	}
	b.WriteByte('}')
	return b.String()
}

func TestGuardedRead_CancellationAndDeadline(t *testing.T) {
	mixed := mixedSource(140)
	list := "(" + strings.Repeat("item ", 800) + ")"
	escaped := "\"" + strings.Repeat("a", 1500) + "\\n" + strings.Repeat("b", 1500) + "\""
	comments := strings.Repeat("; note about the form\n", 180)
	keys := longKeySource(80)
	collisions := collidingKeySource(120)

	cancelled := []struct {
		name string
		src  string
		at   int64
	}{
		{"first-scan-pass", mixed, int64(len(mixed)) / 2},
		{"second-scan-pass", mixed, int64(len(mixed)) + int64(len(mixed))/2},
		{"parsing", mixed, 2*int64(len(mixed)) + 128},
		{"final-list-linking", list, 2*int64(len(list)) + 64},
		{"escaped-prefix-copy", escaped, int64(len(escaped)) + 1600},
		{"long-map-keys", keys, 2*int64(len(keys)) + 64},
		{"map-key-collisions", collisions, 2*int64(len(collisions)) + 64},
		{"comment-only", comments, int64(len(comments)) / 2},
		{"empty-input", "", 0},
	}

	for _, tc := range cancelled {
		t.Run("cancel/"+tc.name, func(t *testing.T) {
			base, meter := budgetContext(DefaultMaxReductions)
			ctx := cancelAtReductions{Context: base, meter: meter, at: tc.at}

			forms, _, err := readContextStats(ctx, FullDialect(), tc.src, 0)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("read after %d reductions returned %v, want context.Canceled", tc.at, err)
			}
			if len(forms) != 0 {
				t.Fatalf("cancelled read returned %d forms, want none", len(forms))
			}
			if got := chargedReductions(meter); got >= tc.at+checkInterval {
				t.Fatalf("cancelled read charged %d reductions, want less than %d", got, tc.at+checkInterval)
			}
		})
	}

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	expired := []struct {
		name     string
		src      string
		readings int
	}{
		{"before-the-read", mixed, 0},
		{"during-the-scan-passes", mixed, 6},
		{"during-parsing", mixed, 60},
		{"comment-only", comments, 4},
	}

	for _, tc := range expired {
		t.Run("deadline/"+tc.name, func(t *testing.T) {
			stepClock(t, base, tc.readings)
			ctx, _ := budgetContext(DefaultMaxReductions)
			ctx = WithEvalDeadline(ctx, base.Add(time.Minute))

			forms, _, err := readContextStats(ctx, FullDialect(), tc.src, 0)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("read past the deadline on clock reading %d returned %v, want context.DeadlineExceeded", tc.readings+1, err)
			}
			if len(forms) != 0 {
				t.Fatalf("expired read returned %d forms, want none", len(forms))
			}
		})
	}
}

func TestGuardedRead_ChargesEveryScannedByte(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"forms", mixedSource(40)},
		{"comment-only", strings.Repeat("; a comment nobody reads\n", 60)},
		{"whitespace", strings.Repeat("   ,\t\n", 200) + "(a)"},
		{"long-symbol", strings.Repeat("s", 3000)},
		{"string-escapes", "\"" + strings.Repeat("a\\n", 500) + "\""},
		{"nested-collections", strings.Repeat("[1 {:a [2 3]} \"s\"]\n", 60)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, meter := budgetContext(DefaultMaxReductions)

			forms, stats, err := readContextStats(ctx, FullDialect(), tc.src, 0)
			if err != nil {
				t.Fatalf("read failed: %v", err)
			}

			wantForms, wantStats, wantErr := FullDialect().ReadWithMaxDepthStats(tc.src, 0)
			if wantErr != nil {
				t.Fatalf("legacy read failed: %v", wantErr)
			}
			if len(forms) != len(wantForms) {
				t.Fatalf("read %d forms, legacy read %d", len(forms), len(wantForms))
			}
			if stats != wantStats {
				t.Fatalf("stats = %+v, legacy stats = %+v", stats, wantStats)
			}

			want := 2 * int64(len(tc.src))
			if got := chargedReductions(meter); got < want {
				t.Fatalf("charged %d reductions, want at least %d for both scan passes", got, want)
			}
		})
	}
}

func TestGuardedRead_SynchronizesWithinBound(t *testing.T) {
	src := mixedSource(140)
	base, meter := budgetContext(DefaultMaxReductions)
	probe := &reductionProbe{Context: base, meter: meter}

	if _, _, err := readContextStats(probe, FullDialect(), src, 0); err != nil {
		t.Fatalf("read failed: %v", err)
	}

	total := chargedReductions(meter)
	if want := 2 * int64(len(src)); total < want {
		t.Fatalf("charged %d reductions, want at least %d for both scan passes", total, want)
	}
	if len(probe.seen) == 0 {
		t.Fatalf("read checked terminal state %d times, want at least one check before any work", len(probe.seen))
	}
	if probe.seen[0] > checkInterval {
		t.Fatalf("first terminal-state check ran after %d reductions, want at most %d", probe.seen[0], checkInterval)
	}
	for i := 1; i < len(probe.seen); i++ {
		if gap := probe.seen[i] - probe.seen[i-1]; gap > checkInterval {
			t.Fatalf("check %d ran %d reductions after the previous one, want at most %d", i, gap, checkInterval)
		}
	}
	if last := probe.seen[len(probe.seen)-1]; last != total {
		t.Fatalf("last terminal-state check saw %d reductions, read charged %d in total; want a check on return", last, total)
	}
}

func TestGuardedRead_TerminalStateOutranksSyntaxError(t *testing.T) {
	scanFailure := mixedSource(70) + "\"unterminated"
	parseFailure := mixedSource(70) + "(unclosed"

	t.Run("cancel/at-entry", func(t *testing.T) {
		base, meter := budgetContext(DefaultMaxReductions)
		ctx := cancelAtReductions{Context: base, meter: meter, at: 0}

		if _, _, err := readContextStats(ctx, FullDialect(), scanFailure, 0); !errors.Is(err, context.Canceled) {
			t.Fatalf("read returned %v, want context.Canceled to outrank the read failure", err)
		}
	})

	t.Run("cancel/inside-the-failing-scan-pass", func(t *testing.T) {
		base, meter := budgetContext(DefaultMaxReductions)
		ctx := cancelAtReductions{Context: base, meter: meter, at: int64(len(scanFailure)) / 2}

		if _, _, err := readContextStats(ctx, FullDialect(), scanFailure, 0); !errors.Is(err, context.Canceled) {
			t.Fatalf("read returned %v, want context.Canceled to outrank the read failure", err)
		}
	})

	t.Run("deadline/at-entry", func(t *testing.T) {
		ctx, _ := budgetContext(DefaultMaxReductions)
		ctx = WithEvalDeadline(ctx, time.Now().Add(-time.Hour))

		if _, _, err := readContextStats(ctx, FullDialect(), scanFailure, 0); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("read returned %v, want context.DeadlineExceeded to outrank the read failure", err)
		}
	})

	t.Run("budget/before-a-scan-failure", func(t *testing.T) {
		ctx, _ := budgetContext(64)

		_, _, err := readContextStats(ctx, FullDialect(), scanFailure, 0)
		if code := readErrorCode(err); code != CodeResourceLimit {
			t.Fatalf("read returned %v (code %q), want a %s to outrank the read failure", err, code, CodeResourceLimit)
		}
	})

	t.Run("budget/before-a-parse-failure", func(t *testing.T) {
		ctx, _ := budgetContext(64)

		_, _, err := readContextStats(ctx, FullDialect(), parseFailure, 0)
		if code := readErrorCode(err); code != CodeResourceLimit {
			t.Fatalf("read returned %v (code %q), want a %s to outrank the read failure", err, code, CodeResourceLimit)
		}
	})
}

func TestGuardedRead_NumericConversionAdmission(t *testing.T) {
	// An oversized token that is also not a number: a resource-limit answer
	// proves the token was refused before conversion, since conversion would
	// have reported the malformed number instead.
	oversized := "1" + strings.Repeat(".1", 200)
	admitted := "1." + strings.Repeat("0", 298)

	t.Run("refuses-a-token-over-a-third-of-the-budget", func(t *testing.T) {
		ctx, _ := budgetContext(900)

		_, _, err := readContextStats(ctx, FullDialect(), oversized, 0)
		if code := readErrorCode(err); code != CodeResourceLimit {
			t.Fatalf("read of a %d-byte numeric token returned %v (code %q), want a %s", len(oversized), err, code, CodeResourceLimit)
		}
	})

	t.Run("charges-the-token-length-before-entry", func(t *testing.T) {
		ctx, meter := budgetContext(DefaultMaxReductions)

		forms, _, err := readContextStats(ctx, FullDialect(), admitted, 0)
		if err != nil {
			t.Fatalf("read failed: %v", err)
		}
		if len(forms) != 1 {
			t.Fatalf("read %d forms, want 1", len(forms))
		}
		want := 3 * int64(len(admitted))
		if got := chargedReductions(meter); got < want {
			t.Fatalf("charged %d reductions, want at least %d for both scan passes plus the conversion token", got, want)
		}
	})

	t.Run("admits-a-token-within-the-bound", func(t *testing.T) {
		ctx, _ := budgetContext(DefaultMaxReductions)

		forms, _, err := readContextStats(ctx, FullDialect(), admitted, 0)
		if err != nil {
			t.Fatalf("read failed: %v", err)
		}
		wantForms, _, wantErr := FullDialect().ReadWithMaxDepthStats(admitted, 0)
		if wantErr != nil {
			t.Fatalf("legacy read failed: %v", wantErr)
		}
		if len(forms) != len(wantForms) || !wantForms[0].Equals(forms[0]) {
			t.Fatalf("read %v, legacy read %v", forms, wantForms)
		}
	})
}
