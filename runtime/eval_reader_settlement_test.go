package runtime

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
)

// readerSettlementWidth makes the token plan alone outgrow both the tight
// ceiling below and one whole allocation lease, so the read fails while it is
// still counting rather than after the parse is built.
const readerSettlementWidth = 4096

func readerSettlementSource() string { return vectorLiteral("1", readerSettlementWidth) }

// tightReaderLimits refuse the wide source on the ledger's own ceiling.
func tightReaderLimits(t *testing.T) ResourceLimits {
	t.Helper()
	return meteringLimits(t, 1_000_000, 4<<10)
}

// leaseReaderLimits leave the ledger enough room for the wide source, so a
// refusal under them can only come from the host meter refusing to lease.
func leaseReaderLimits(t *testing.T) ResourceLimits {
	t.Helper()
	return meteringLimits(t, 1_000_000, 1<<20)
}

// readerFailure is one way a guarded read fails, with the cause the caller,
// the event and the error statistics all have to report.
type readerFailure struct {
	name      string
	source    string
	limits    func(t *testing.T) ResourceLimits
	cancelled bool
	denyLease bool
	wantCode  string
	wantIs    error
}

func readerFailures() []readerFailure {
	return []readerFailure{
		{name: "source-error", source: "(def ok", limits: leaseReaderLimits, wantCode: "ReadError"},
		{name: "budget-refusal", source: readerSettlementSource(), limits: tightReaderLimits, wantCode: core.CodeResourceLimit},
		{name: "meter-denial", source: readerSettlementSource(), limits: leaseReaderLimits, denyLease: true, wantCode: core.CodeResourceLimit},
		{name: "cancelled-empty", source: "", limits: leaseReaderLimits, cancelled: true, wantIs: context.Canceled},
		{name: "cancelled-comment-only", source: "; only a comment\n", limits: leaseReaderLimits, cancelled: true, wantIs: context.Canceled},
	}
}

// TestGuardedRead_SingleSettledOutcome pins the settlement of a failed read at
// every public entry point: one event, one evaluation counted, one error
// counted, one lease returned, and the same cause in every place it is
// published.
func TestGuardedRead_SingleSettledOutcome(t *testing.T) {
	for _, ev := range precedenceEvaluators() {
		for _, entry := range outcomeEntries() {
			for _, tc := range readerFailures() {
				t.Run(ev.name+"/"+entry.name+"/"+tc.name, func(t *testing.T) {
					name := ev.name + "/" + entry.name + "/" + tc.name
					meter := &recordingMeter{}
					if tc.denyLease {
						meter.mu.Lock()
						meter.denyAfter = 1
						meter.mu.Unlock()
					}
					eng := newSettlementEngine(t, ev.opt, WithResourceLimits(tc.limits(t)))
					seen := collectEvents(eng)
					base := eng.Stats()

					ctx := WithMeter(t.Context(), meter)
					if tc.cancelled {
						cancelled, cancel := context.WithCancel(ctx)
						cancel()
						ctx = cancelled
					}

					_, err := entry.invoke(t, ctx, eng, tc.source, nil)
					if err == nil {
						t.Fatalf("%s: error = nil, want a refused read", name)
					}
					// Every entry point wraps a read failure and nothing else
					// behind this prefix, so it is what says the settled
					// outcome is the read's rather than a later failure's.
					if !strings.HasPrefix(err.Error(), "read: ") {
						t.Fatalf("%s: error = %v, want the failure to come from the read", name, err)
					}

					events := seen()
					if len(events) != 1 {
						t.Fatalf("%s: OnEval events = %d, want exactly 1", name, len(events))
					}
					snap := eng.Stats()
					if d := snap.TotalEvals - base.TotalEvals; d != 1 {
						t.Fatalf("%s: TotalEvals delta = %d, want 1", name, d)
					}
					if d := snap.TotalErrors - base.TotalErrors; d != 1 {
						t.Fatalf("%s: TotalErrors delta = %d, want 1", name, d)
					}
					if !errors.Is(err, events[0].Error) {
						t.Fatalf("%s: event error = %v, want the cause the caller got (%v)", name, events[0].Error, err)
					}
					forEachPublishedError(t, err, events[0], func(where string, got error) {
						if tc.wantCode != "" {
							var lerr *core.LispicoError
							if !errors.As(got, &lerr) || lerr.Code != tc.wantCode {
								t.Fatalf("%s: %s = %v, want %s cause", name, where, got, tc.wantCode)
							}
						}
						if tc.wantIs != nil && !errors.Is(got, tc.wantIs) {
							t.Fatalf("%s: %s = %v, want %v", name, where, got, tc.wantIs)
						}
					})
					if returns := meter.snapshot().returnCalls; returns != 1 {
						t.Fatalf("%s: ReturnEval calls = %d, want 1; a refused read must settle its lease exactly once", name, returns)
					}
				})
			}
		}
	}
}

// readerPanicMeter is a host meter that panics from the re-lease the reader
// draws mid-read, the way a buggy embedder implementation would.
type readerPanicMeter struct {
	recordingMeter
}

func (m *readerPanicMeter) LeaseEval(reductions, allocBytes int64) (int64, int64, error) {
	red, alloc, err := m.recordingMeter.LeaseEval(reductions, allocBytes)
	if m.snapshot().leaseCalls > 1 {
		panic("reader meter failure")
	}
	return red, alloc, err
}

// TestGuardedRead_MeterPanicDuringReadIsContained pins that a host meter panic
// raised while the read is still running is contained the same way one raised
// at settlement is: the entry point returns an error, publishes one settled
// outcome, and still returns the evaluation lease it drew.
func TestGuardedRead_MeterPanicDuringReadIsContained(t *testing.T) {
	for _, ev := range precedenceEvaluators() {
		for _, entry := range outcomeEntries() {
			t.Run(ev.name+"/"+entry.name, func(t *testing.T) {
				name := ev.name + "/" + entry.name
				meter := &readerPanicMeter{}
				eng := newSettlementEngine(t, ev.opt, WithResourceLimits(leaseReaderLimits(t)))
				seen := collectEvents(eng)
				base := eng.Stats()

				var evalErr error
				escaped := catchPanic(func() {
					_, evalErr = entry.invoke(t, WithMeter(t.Context(), meter), eng, readerSettlementSource(), nil)
				})
				if escaped != nil {
					t.Fatalf("%s: reader meter panic escaped the entry point (%v); a host meter panic must come back as an error", name, escaped)
				}

				var lerr *core.LispicoError
				if !errors.As(evalErr, &lerr) || lerr.Code != core.CodePanic {
					t.Fatalf("%s: error = %v, want %s cause", name, evalErr, core.CodePanic)
				}
				if events := seen(); len(events) != 1 {
					t.Fatalf("%s: OnEval events = %d, want exactly 1", name, len(events))
				}
				snap := eng.Stats()
				if d := snap.TotalEvals - base.TotalEvals; d != 1 {
					t.Fatalf("%s: TotalEvals delta = %d, want 1", name, d)
				}
				if d := snap.TotalErrors - base.TotalErrors; d != 1 {
					t.Fatalf("%s: TotalErrors delta = %d, want 1", name, d)
				}
				if returns := meter.snapshot().returnCalls; returns != 1 {
					t.Fatalf("%s: ReturnEval calls = %d, want 1; a reader panic must not leak the evaluation lease", name, returns)
				}
			})
		}
	}
}

// TestGuardedRead_ReloadSettlesOutsideTheEventContract pins the documented
// exception: the watcher reports a refused reload through the log, and
// publishes neither an EvalEvent nor an evaluation count.
func TestGuardedRead_ReloadSettlesOutsideTheEventContract(t *testing.T) {
	for _, ev := range precedenceEvaluators() {
		t.Run(ev.name, func(t *testing.T) {
			var buf bytes.Buffer
			eng, err := New(
				slog.New(slog.NewTextHandler(&buf, nil)),
				WithDialect(clojure.Dialect()),
				ev.opt,
				WithResourceLimits(tightReaderLimits(t)),
			)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			t.Cleanup(func() { _ = eng.Close() })

			w := newFileWatcher(eng.(*engineImpl), t.TempDir(), 10*time.Millisecond)
			w.ctx = context.Background()
			path := filepath.Join(w.dir, "over.lisp")
			if err := os.WriteFile(path, []byte(readerSettlementSource()), 0o644); err != nil {
				t.Fatalf("write source: %v", err)
			}

			seen := collectEvents(eng)
			base := eng.Stats()
			w.reloadFile(path)

			if events := seen(); len(events) != 0 {
				t.Fatalf("%s: OnEval events = %d, want 0; the reload path publishes no event", ev.name, len(events))
			}
			snap := eng.Stats()
			if d := snap.TotalEvals - base.TotalEvals; d != 0 {
				t.Fatalf("%s: TotalEvals delta = %d, want 0", ev.name, d)
			}
			if d := snap.TotalErrors - base.TotalErrors; d != 0 {
				t.Fatalf("%s: TotalErrors delta = %d, want 0", ev.name, d)
			}
			if !strings.Contains(buf.String(), "parse file") {
				t.Fatalf("%s: the refused reload was not reported through the log: %s", ev.name, buf.String())
			}
		})
	}
}
