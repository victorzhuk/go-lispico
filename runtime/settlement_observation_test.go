package runtime

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
)

// TestEval_RetainedDenialIsSettledFailure pins the settled outcome of an
// evaluation whose retained charge is denied: the caller, the OnEval event and
// the error count must agree, and the binding written before settlement stays.
func TestEval_RetainedDenialIsSettledFailure(t *testing.T) {
	tests := []struct {
		name      string
		evaluator EngineOption
		ctxMeter  bool
	}{
		{name: "bytecode/engine-meter", evaluator: WithBytecode()},
		{name: "bytecode/context-meter", evaluator: WithBytecode(), ctxMeter: true},
		{name: "treewalker/engine-meter", evaluator: WithTreeWalker()},
		{name: "treewalker/context-meter", evaluator: WithTreeWalker(), ctxMeter: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meter := &recordingMeter{chargeErr: errors.New("retained denied")}
			opts := []EngineOption{WithDialect(clojure.Dialect()), tt.evaluator}
			if !tt.ctxMeter {
				opts = append(opts, WithEngineMeter(meter))
			}
			eng, err := New(nil, opts...)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			t.Cleanup(func() { _ = eng.Close() })

			ctx := t.Context()
			if tt.ctxMeter {
				ctx = WithMeter(ctx, meter)
			}
			meter.reset()
			base := eng.Stats()

			var mu sync.Mutex
			var events []EvalEvent
			eng.OnEval(func(e EvalEvent) {
				mu.Lock()
				events = append(events, e)
				mu.Unlock()
			})

			_, err = eng.Eval(ctx, "retained-denied", "(def denied [1 2 3])")
			var lerr *core.LispicoError
			if !errors.As(err, &lerr) || lerr.Code != core.CodeResourceLimit {
				t.Fatalf("Eval error = %v, want %s", err, core.CodeResourceLimit)
			}

			mu.Lock()
			got := append([]EvalEvent(nil), events...)
			mu.Unlock()
			if len(got) != 1 {
				t.Fatalf("OnEval events = %d, want exactly 1", len(got))
			}

			var everr *core.LispicoError
			eventMatches := got[0].Error != nil && errors.As(got[0].Error, &everr) && everr.Code == core.CodeResourceLimit
			snap := eng.Stats()
			errDelta := snap.TotalErrors - base.TotalErrors
			evalDelta := snap.TotalEvals - base.TotalEvals
			if !eventMatches || errDelta != 1 || evalDelta != 1 {
				t.Fatalf("settled outcome not published: event error = %v, TotalErrors delta = %d, TotalEvals delta = %d; want %s cause, 1, 1",
					got[0].Error, errDelta, evalDelta, core.CodeResourceLimit)
			}

			if _, ok := eng.RootEnv().Get("denied"); !ok {
				t.Fatal("denied binding missing; retained denial must not roll back the evaluation write")
			}
		})
	}
}

// denyLeaseMeter refuses the initial evaluation lease, so an evaluation fails
// during setup without ever acquiring one.
type denyLeaseMeter struct {
	recordingMeter
}

func (m *denyLeaseMeter) LeaseEval(int64, int64) (int64, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.leaseCalls++
	return 0, 0, errors.New("lease denied")
}

type settlementRecorder interface {
	Meter
	snapshot() recordingMeter
}

// settledAt is the settlement a callback could observe at the moment it ran.
type settledAt struct {
	returnCalls int
	chargeCalls int
}

// settlementProbe records, for every OnEval callback, both the event and the
// settlement the meter had already seen at the moment that callback ran.
type settlementProbe struct {
	meter    settlementRecorder
	mu       sync.Mutex
	events   []EvalEvent
	observed []settledAt
}

func (p *settlementProbe) watch(eng Engine) {
	eng.OnEval(func(e EvalEvent) {
		snap := p.meter.snapshot()
		at := settledAt{returnCalls: snap.returnCalls, chargeCalls: snap.chargeCalls}
		p.mu.Lock()
		p.events = append(p.events, e)
		p.observed = append(p.observed, at)
		p.mu.Unlock()
	})
}

func (p *settlementProbe) seen() ([]EvalEvent, []settledAt) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]EvalEvent(nil), p.events...), append([]settledAt(nil), p.observed...)
}

type settlementOutcome struct {
	name        string
	source      string
	denyLease   bool
	wantErr     bool
	wantCode    string
	wantReturns int
	wantCharges int
	checkCharge bool
}

func settlementOutcomes() []settlementOutcome {
	return []settlementOutcome{
		{name: "success", source: "(def ok [1 2 3])", wantReturns: 1, wantCharges: 1, checkCharge: true},
		{name: "setup-failure", source: "1", denyLease: true, wantErr: true},
		{name: "parse-error", source: "(def ok", wantErr: true, wantReturns: 1},
		{name: "eval-error", source: "(fail)", wantErr: true, wantReturns: 1},
		{name: "recovered-panic", source: "(boom)", wantErr: true, wantCode: core.CodePanic, wantReturns: 1},
	}
}

func settlementFixtures() map[string]core.Value {
	return map[string]core.Value{
		"fail": core.GoFunc{
			Name: "fail",
			Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
				return nil, errors.New("settlement failure")
			},
		},
		"boom": core.GoFunc{
			Name: "boom",
			Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
				panic("settlement panic")
			},
		},
	}
}

type settlementInvoke func(t *testing.T, ctx context.Context, eng Engine, source string, bindings map[string]core.Value) (*core.Env, error)

// runSettlementOutcomes drives one entry point through every settled outcome and
// checks that each publishes exactly one event and one evaluation count, with
// settlement already complete when the observer runs.
func runSettlementOutcomes(t *testing.T, entry string, invoke settlementInvoke, extra func(t *testing.T, tc settlementOutcome, scope *core.Env)) {
	t.Helper()
	for _, ev := range []struct {
		name string
		opt  EngineOption
	}{
		{name: "bytecode", opt: WithBytecode()},
		{name: "treewalker", opt: WithTreeWalker()},
	} {
		for _, tc := range settlementOutcomes() {
			t.Run(ev.name+"/"+tc.name, func(t *testing.T) {
				var meter settlementRecorder = &recordingMeter{}
				if tc.denyLease {
					meter = &denyLeaseMeter{}
				}
				eng, err := New(nil, WithDialect(clojure.Dialect()), ev.opt)
				if err != nil {
					t.Fatalf("New: %v", err)
				}
				t.Cleanup(func() { _ = eng.Close() })

				probe := &settlementProbe{meter: meter}
				base := eng.Stats()
				probe.watch(eng)

				scope, evalErr := invoke(t, WithMeter(t.Context(), meter), eng, tc.source, settlementFixtures())
				assertSettledOnce(t, tc, entry, base, eng.Stats(), probe, evalErr)
				if extra != nil {
					extra(t, tc, scope)
				}
			})
		}
	}
}

func assertSettledOnce(t *testing.T, tc settlementOutcome, entry string, base, snap EngineStats, probe *settlementProbe, err error) {
	t.Helper()
	name := entry + "/" + tc.name
	switch {
	case tc.wantErr && err == nil:
		t.Fatalf("%s: error = nil, want a failed outcome", name)
	case !tc.wantErr && err != nil:
		t.Fatalf("%s: error = %v, want success", name, err)
	}
	if tc.wantCode != "" {
		var lerr *core.LispicoError
		if !errors.As(err, &lerr) || lerr.Code != tc.wantCode {
			t.Fatalf("%s: error = %v, want %s cause", name, err, tc.wantCode)
		}
	}

	events, observed := probe.seen()
	if len(events) != 1 {
		t.Fatalf("%s: OnEval events = %d, want exactly 1", name, len(events))
	}
	if (events[0].Error != nil) != tc.wantErr {
		t.Fatalf("%s: event error = %v, want failure = %v", name, events[0].Error, tc.wantErr)
	}
	if tc.wantCode != "" {
		var everr *core.LispicoError
		if !errors.As(events[0].Error, &everr) || everr.Code != tc.wantCode {
			t.Fatalf("%s: event error = %v, want %s cause", name, events[0].Error, tc.wantCode)
		}
	}

	wantErrors := int64(0)
	if tc.wantErr {
		wantErrors = 1
	}
	if got := snap.TotalEvals - base.TotalEvals; got != 1 {
		t.Fatalf("%s: TotalEvals delta = %d, want 1", name, got)
	}
	if got := snap.TotalErrors - base.TotalErrors; got != wantErrors {
		t.Fatalf("%s: TotalErrors delta = %d, want %d", name, got, wantErrors)
	}

	at := observed[0]
	if at.returnCalls != tc.wantReturns {
		t.Fatalf("%s: ReturnEval calls seen by the callback = %d, want %d; the observer must run after the lease is settled",
			name, at.returnCalls, tc.wantReturns)
	}
	if tc.checkCharge && at.chargeCalls != tc.wantCharges {
		t.Fatalf("%s: ChargeRetained calls seen by the callback = %d, want %d; retained settlement must complete before the event",
			name, at.chargeCalls, tc.wantCharges)
	}
}

func TestEval_SettledOutcomeIsPublishedOnce(t *testing.T) {
	runSettlementOutcomes(t, "Eval", func(t *testing.T, ctx context.Context, eng Engine, source string, bindings map[string]core.Value) (*core.Env, error) {
		t.Helper()
		for name, val := range bindings {
			if err := eng.Bind(name, val); err != nil {
				t.Fatalf("Bind %s: %v", name, err)
			}
		}
		_, err := eng.Eval(ctx, "settlement", source)
		return nil, err
	}, nil)
}

func TestEvalWithBindings_SettledOutcomeIsPublishedOnce(t *testing.T) {
	runSettlementOutcomes(t, "EvalWithBindings", func(t *testing.T, ctx context.Context, eng Engine, source string, bindings map[string]core.Value) (*core.Env, error) {
		t.Helper()
		_, err := eng.EvalWithBindings(ctx, source, bindings)
		return nil, err
	}, nil)
}

func TestLoadScope_SettledOutcomeIsPublishedOnce(t *testing.T) {
	runSettlementOutcomes(t, "LoadScope", func(t *testing.T, ctx context.Context, eng Engine, source string, bindings map[string]core.Value) (*core.Env, error) {
		t.Helper()
		_, scope, err := eng.LoadScope(ctx, source, bindings)
		return scope, err
	}, func(t *testing.T, tc settlementOutcome, scope *core.Env) {
		t.Helper()
		if !tc.wantErr && scope == nil {
			t.Fatal("LoadScope/success: scope = nil, want the evaluated child scope")
		}
	})
}

type retainedDenialCase struct {
	name      string
	evaluator EngineOption
	ctxMeter  bool
}

func retainedDenialCases() []retainedDenialCase {
	return []retainedDenialCase{
		{name: "bytecode/engine-meter", evaluator: WithBytecode()},
		{name: "bytecode/context-meter", evaluator: WithBytecode(), ctxMeter: true},
		{name: "treewalker/engine-meter", evaluator: WithTreeWalker()},
		{name: "treewalker/context-meter", evaluator: WithTreeWalker(), ctxMeter: true},
	}
}

// newRetainedDenialEngine builds an engine whose retained charge is denied at
// settlement, with the meter attached the way the case asks for.
func newRetainedDenialEngine(t *testing.T, tc retainedDenialCase) (Engine, *recordingMeter, context.Context) {
	t.Helper()
	meter := &recordingMeter{chargeErr: errors.New("retained denied")}
	opts := []EngineOption{WithDialect(clojure.Dialect()), tc.evaluator}
	if !tc.ctxMeter {
		opts = append(opts, WithEngineMeter(meter))
	}
	eng, err := New(nil, opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })

	ctx := t.Context()
	if tc.ctxMeter {
		ctx = WithMeter(ctx, meter)
	}
	meter.reset()
	return eng, meter, ctx
}

func assertRetainedDenialPublished(t *testing.T, entry string, base, snap EngineStats, events []EvalEvent, err error) {
	t.Helper()
	var lerr *core.LispicoError
	if !errors.As(err, &lerr) || lerr.Code != core.CodeResourceLimit {
		t.Fatalf("%s error = %v, want %s", entry, err, core.CodeResourceLimit)
	}
	if len(events) != 1 {
		t.Fatalf("%s OnEval events = %d, want exactly 1", entry, len(events))
	}
	var everr *core.LispicoError
	eventMatches := events[0].Error != nil && errors.As(events[0].Error, &everr) && everr.Code == core.CodeResourceLimit
	errDelta := snap.TotalErrors - base.TotalErrors
	evalDelta := snap.TotalEvals - base.TotalEvals
	if !eventMatches || errDelta != 1 || evalDelta != 1 {
		t.Fatalf("%s settled outcome not published: event error = %v, TotalErrors delta = %d, TotalEvals delta = %d; want %s cause, 1, 1",
			entry, events[0].Error, errDelta, evalDelta, core.CodeResourceLimit)
	}
}

// TestEvalWithBindings_RetainedDenialIsSettledFailure pins the settled outcome
// of a binding-scope evaluation whose retained charge is denied: caller, event
// and error count agree, and the write charged before the denial stays charged.
func TestEvalWithBindings_RetainedDenialIsSettledFailure(t *testing.T) {
	for _, tc := range retainedDenialCases() {
		t.Run(tc.name, func(t *testing.T) {
			eng, meter, ctx := newRetainedDenialEngine(t, tc)
			base := eng.Stats()

			var mu sync.Mutex
			var events []EvalEvent
			eng.OnEval(func(e EvalEvent) {
				mu.Lock()
				events = append(events, e)
				mu.Unlock()
			})

			_, err := eng.EvalWithBindings(ctx, "(def denied [1 2 3])", nil)

			mu.Lock()
			got := append([]EvalEvent(nil), events...)
			mu.Unlock()
			assertRetainedDenialPublished(t, "EvalWithBindings", base, eng.Stats(), got, err)

			if charges := meter.snapshot().chargeCalls; charges != 1 {
				t.Fatalf("ChargeRetained calls = %d, want 1; the denied charge must follow the evaluation write", charges)
			}
			if _, ok := eng.RootEnv().Get("denied"); ok {
				t.Fatal("denied binding leaked into the root env; EvalWithBindings evaluates in a child scope")
			}
		})
	}
}

// TestLoadScope_RetainedDenialIsSettledFailure pins the same settled outcome for
// LoadScope, which must still hand back the scope it evaluated in.
func TestLoadScope_RetainedDenialIsSettledFailure(t *testing.T) {
	for _, tc := range retainedDenialCases() {
		t.Run(tc.name, func(t *testing.T) {
			eng, _, ctx := newRetainedDenialEngine(t, tc)
			base := eng.Stats()

			var mu sync.Mutex
			var events []EvalEvent
			eng.OnEval(func(e EvalEvent) {
				mu.Lock()
				events = append(events, e)
				mu.Unlock()
			})

			_, scope, err := eng.LoadScope(ctx, "(def denied [1 2 3])", nil)

			mu.Lock()
			got := append([]EvalEvent(nil), events...)
			mu.Unlock()
			assertRetainedDenialPublished(t, "LoadScope", base, eng.Stats(), got, err)

			if scope == nil {
				t.Fatal("LoadScope scope = nil, want the evaluated child scope on a settled failure")
			}
			if _, ok := scope.Get("denied"); !ok {
				t.Fatal("denied binding missing from the scope; retained denial must not roll back the evaluation write")
			}
		})
	}
}

// errNonterminalEval is the ordinary evaluation failure the precedence cases
// race against settlement. Matching it by identity keeps the assertions on the
// selected cause instead of on a wrapper's pointer or its formatted message.
var errNonterminalEval = errors.New("nonterminal evaluation failure")

// precedenceSource retains a binding and then fails, so settlement has a
// retained charge left to deny after the evaluation has already produced a
// nonterminal error of its own.
const precedenceSource = "(def kept [1 2 3]) (fail-soft)"

func precedenceBindings() map[string]core.Value {
	fixtures := settlementFixtures()
	fixtures["fail-soft"] = core.GoFunc{
		Name: "fail-soft",
		Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
			return nil, errNonterminalEval
		},
	}
	return fixtures
}

// precedenceEntry drives one public entry point through the precedence and
// wrapping cases. panicWrapped records the entry's public panic shape: Eval
// wraps a recovered panic behind its eval prefix, while the binding-scope
// entries hand back the panic error itself.
type precedenceEntry struct {
	name         string
	panicWrapped bool
	prepare      func(t *testing.T, eng Engine, bindings map[string]core.Value)
	invoke       settlementInvoke
}

func precedenceEntries() []precedenceEntry {
	return []precedenceEntry{
		{
			name:         "Eval",
			panicWrapped: true,
			prepare: func(t *testing.T, eng Engine, bindings map[string]core.Value) {
				t.Helper()
				for name, val := range bindings {
					if err := eng.Bind(name, val); err != nil {
						t.Fatalf("Bind %s: %v", name, err)
					}
				}
			},
			invoke: func(t *testing.T, ctx context.Context, eng Engine, source string, _ map[string]core.Value) (*core.Env, error) {
				t.Helper()
				_, err := eng.Eval(ctx, "precedence", source)
				return nil, err
			},
		},
		{
			name: "EvalWithBindings",
			invoke: func(t *testing.T, ctx context.Context, eng Engine, source string, bindings map[string]core.Value) (*core.Env, error) {
				t.Helper()
				_, err := eng.EvalWithBindings(ctx, source, bindings)
				return nil, err
			},
		},
		{
			name: "LoadScope",
			invoke: func(t *testing.T, ctx context.Context, eng Engine, source string, bindings map[string]core.Value) (*core.Env, error) {
				t.Helper()
				_, scope, err := eng.LoadScope(ctx, source, bindings)
				return scope, err
			},
		},
	}
}

func precedenceEvaluators() []struct {
	name string
	opt  EngineOption
} {
	return []struct {
		name string
		opt  EngineOption
	}{
		{name: "bytecode", opt: WithBytecode()},
		{name: "treewalker", opt: WithTreeWalker()},
	}
}

// runPrecedence drives every entry point and evaluator through an evaluation
// that fails with a nonterminal error while settlement either denies or accepts
// the retained charge that same evaluation left pending, then hands the settled
// outcome to check.
func runPrecedence(t *testing.T, denyRetained bool, check func(t *testing.T, name string, err error, event EvalEvent)) {
	t.Helper()
	runPrecedenceSource(t, precedenceSource, denyRetained, check)
}

// runPrecedenceSource is runPrecedence over a caller-chosen source, so a case
// can pick which kind of evaluation failure settlement has to rule against.
func runPrecedenceSource(t *testing.T, source string, denyRetained bool, check func(t *testing.T, name string, err error, event EvalEvent)) {
	t.Helper()
	for _, ev := range precedenceEvaluators() {
		for _, entry := range precedenceEntries() {
			t.Run(ev.name+"/"+entry.name, func(t *testing.T) {
				name := ev.name + "/" + entry.name
				// A context meter, not an engine meter: the chunk cache charges
				// an engine meter for every chunk it admits, which would bury
				// the one charge settlement itself has to rule on.
				meter := &recordingMeter{}
				eng, err := New(nil, WithDialect(clojure.Dialect()), ev.opt)
				if err != nil {
					t.Fatalf("New: %v", err)
				}
				t.Cleanup(func() { _ = eng.Close() })

				bindings := precedenceBindings()
				if entry.prepare != nil {
					entry.prepare(t, eng, bindings)
				}
				if denyRetained {
					meter.mu.Lock()
					meter.chargeErr = errors.New("retained denied")
					meter.mu.Unlock()
				}
				meter.reset()

				var mu sync.Mutex
				var events []EvalEvent
				eng.OnEval(func(e EvalEvent) {
					mu.Lock()
					events = append(events, e)
					mu.Unlock()
				})
				base := eng.Stats()

				_, evalErr := entry.invoke(t, WithMeter(t.Context(), meter), eng, source, bindings)
				snap := eng.Stats()

				// Without a retained charge to settle there is nothing for the
				// evaluation error to lose to, and either direction would pass
				// on an outcome settlement never took part in.
				if charges := meter.snapshot().chargeCalls; charges != 1 {
					t.Fatalf("%s: ChargeRetained calls = %d, want 1; the case must leave settlement a charge to rule on", name, charges)
				}

				mu.Lock()
				got := append([]EvalEvent(nil), events...)
				mu.Unlock()
				if len(got) != 1 {
					t.Fatalf("%s: OnEval events = %d, want exactly 1", name, len(got))
				}
				if d := snap.TotalEvals - base.TotalEvals; d != 1 {
					t.Fatalf("%s: TotalEvals delta = %d, want 1", name, d)
				}
				if d := snap.TotalErrors - base.TotalErrors; d != 1 {
					t.Fatalf("%s: TotalErrors delta = %d, want 1", name, d)
				}
				check(t, name, evalErr, got[0])
			})
		}
	}
}

// forEachPublishedError applies check to the two places a settled failure has to
// agree on: what the caller got back and what the OnEval event carried.
func forEachPublishedError(t *testing.T, err error, event EvalEvent, check func(where string, got error)) {
	t.Helper()
	check("returned error", err)
	check("event error", event.Error)
}

// TestEval_TerminalSettlementErrorWinsOverEvalError pins the terminal half of
// the precedence rule: a terminal settlement failure replaces the nonterminal
// error the evaluation returned, and the caller, the event and the error
// statistics all report that one selected cause.
func TestEval_TerminalSettlementErrorWinsOverEvalError(t *testing.T) {
	runPrecedence(t, true, func(t *testing.T, name string, err error, event EvalEvent) {
		t.Helper()
		forEachPublishedError(t, err, event, func(where string, got error) {
			var lerr *core.LispicoError
			if !errors.As(got, &lerr) || lerr.Code != core.CodeResourceLimit {
				t.Fatalf("%s: %s = %v, want %s cause", name, where, got, core.CodeResourceLimit)
			}
			if !core.IsTerminalEvalError(got) {
				t.Fatalf("%s: %s = %v, want a terminal cause", name, where, got)
			}
			if errors.Is(got, errNonterminalEval) {
				t.Fatalf("%s: %s = %v, want the terminal settlement cause to replace the nonterminal evaluation error", name, where, got)
			}
		})
	})
}

// TestEval_NonterminalEvalErrorSurvivesSettlement pins the other direction: a
// settlement that raises nothing leaves the evaluation's own nonterminal cause
// as the published outcome, so settlement never wins by default.
func TestEval_NonterminalEvalErrorSurvivesSettlement(t *testing.T) {
	runPrecedence(t, false, func(t *testing.T, name string, err error, event EvalEvent) {
		t.Helper()
		forEachPublishedError(t, err, event, func(where string, got error) {
			if !errors.Is(got, errNonterminalEval) {
				t.Fatalf("%s: %s = %v, want the evaluation's own cause to survive settlement", name, where, got)
			}
			if core.IsTerminalEvalError(got) {
				t.Fatalf("%s: %s = %v, want a nonterminal cause", name, where, got)
			}
		})
	})
}

// wrappingCase is one public failure shape the settlement point must leave
// alone: the typed cause it carries, and whether the entry point returns that
// cause behind a wrapper.
type wrappingCase struct {
	name      string
	source    string
	wantCode  string
	wantIs    error
	panicPath bool
	wantScope bool
}

func wrappingCases() []wrappingCase {
	return []wrappingCase{
		{name: "parse-error", source: "(def ok", wantCode: "ReadError"},
		{name: "eval-error", source: "(fail-soft)", wantIs: errNonterminalEval, wantScope: true},
		{name: "recovered-panic", source: "(boom)", wantCode: core.CodePanic, panicPath: true},
	}
}

func newPrecedenceEngine(t *testing.T, opt EngineOption) Engine {
	t.Helper()
	eng, err := New(nil, WithDialect(clojure.Dialect()), opt, WithEngineMeter(&recordingMeter{}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	return eng
}

// TestEval_PublicErrorWrappingSurvivesSettlement pins the public error shapes
// the settlement point must not change: the read and eval paths still return
// their typed cause behind a wrapper, while the binding-scope panic path still
// returns the panic error itself.
func TestEval_PublicErrorWrappingSurvivesSettlement(t *testing.T) {
	for _, ev := range precedenceEvaluators() {
		for _, entry := range precedenceEntries() {
			for _, tc := range wrappingCases() {
				t.Run(ev.name+"/"+entry.name+"/"+tc.name, func(t *testing.T) {
					name := ev.name + "/" + entry.name + "/" + tc.name
					eng := newPrecedenceEngine(t, ev.opt)
					bindings := precedenceBindings()
					if entry.prepare != nil {
						entry.prepare(t, eng, bindings)
					}

					_, err := entry.invoke(t, t.Context(), eng, tc.source, bindings)
					if err == nil {
						t.Fatalf("%s: error = nil, want a failure", name)
					}
					if tc.wantCode != "" {
						var lerr *core.LispicoError
						if !errors.As(err, &lerr) || lerr.Code != tc.wantCode {
							t.Fatalf("%s: error = %v, want %s cause", name, err, tc.wantCode)
						}
					}
					if tc.wantIs != nil && !errors.Is(err, tc.wantIs) {
						t.Fatalf("%s: error = %v, want the evaluation's own cause", name, err)
					}

					wrapped := errors.Unwrap(err) != nil
					wantWrapped := !tc.panicPath || entry.panicWrapped
					if wrapped != wantWrapped {
						t.Fatalf("%s: error wraps its cause = %v, want %v; the entry point's public error wrapping must not change",
							name, wrapped, wantWrapped)
					}
				})
			}
		}
	}
}

// TestLoadScope_ScopeReturnSurvivesSettlement pins which failures still hand the
// caller the child scope the evaluation ran in, and which hand back none.
func TestLoadScope_ScopeReturnSurvivesSettlement(t *testing.T) {
	for _, ev := range precedenceEvaluators() {
		for _, tc := range wrappingCases() {
			t.Run(ev.name+"/"+tc.name, func(t *testing.T) {
				name := ev.name + "/" + tc.name
				eng := newPrecedenceEngine(t, ev.opt)

				_, scope, err := eng.LoadScope(t.Context(), tc.source, precedenceBindings())
				if err == nil {
					t.Fatalf("%s: error = nil, want a failure", name)
				}
				if (scope != nil) != tc.wantScope {
					t.Fatalf("%s: LoadScope returned a scope = %v, want %v", name, scope != nil, tc.wantScope)
				}
			})
		}
	}
}

// retainingSource leaves settlement a retained charge to rule on and nothing
// else to fail against, so the outcome under test is settlement's alone.
const retainingSource = "(def kept [1 2 3])"

// precedencePanicSource retains a binding and then panics, so a recovered
// GoFunc panic and a settlement verdict race for the published cause.
const precedencePanicSource = "(def kept [1 2 3]) (boom)"

// catchPanic runs fn and hands back whatever escaped it, so a panic crossing a
// public entry point becomes an assertion instead of a dead test binary.
func catchPanic(fn func()) (escaped any) {
	defer func() { escaped = recover() }()
	fn()
	return nil
}

// panicSettlementMeter is a host meter that panics from inside the settlement
// path, the way a buggy embedder implementation would.
type panicSettlementMeter struct {
	recordingMeter
}

func (m *panicSettlementMeter) ChargeRetained(bytes, slots int64) error {
	_ = m.recordingMeter.ChargeRetained(bytes, slots)
	panic("settlement meter failure")
}

// outcomeEntry drives one public entry point and hands back the result value
// too, which the precedence entries drop.
type outcomeEntry struct {
	name   string
	invoke func(t *testing.T, ctx context.Context, eng Engine, source string, bindings map[string]core.Value) (core.Value, error)
}

func outcomeEntries() []outcomeEntry {
	return []outcomeEntry{
		{
			name: "Eval",
			invoke: func(t *testing.T, ctx context.Context, eng Engine, source string, bindings map[string]core.Value) (core.Value, error) {
				t.Helper()
				for name, val := range bindings {
					if err := eng.Bind(name, val); err != nil {
						t.Fatalf("Bind %s: %v", name, err)
					}
				}
				return eng.Eval(ctx, "outcome", source)
			},
		},
		{
			name: "EvalWithBindings",
			invoke: func(t *testing.T, ctx context.Context, eng Engine, source string, bindings map[string]core.Value) (core.Value, error) {
				t.Helper()
				return eng.EvalWithBindings(ctx, source, bindings)
			},
		},
		{
			name: "LoadScope",
			invoke: func(t *testing.T, ctx context.Context, eng Engine, source string, bindings map[string]core.Value) (core.Value, error) {
				t.Helper()
				result, _, err := eng.LoadScope(ctx, source, bindings)
				return result, err
			},
		},
	}
}

func newSettlementEngine(t *testing.T, opts ...EngineOption) Engine {
	t.Helper()
	eng, err := New(nil, append([]EngineOption{WithDialect(clojure.Dialect())}, opts...)...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	return eng
}

// collectEvents registers an observer and returns a reader for what it saw.
func collectEvents(eng Engine) func() []EvalEvent {
	var mu sync.Mutex
	var events []EvalEvent
	eng.OnEval(func(e EvalEvent) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	})
	return func() []EvalEvent {
		mu.Lock()
		defer mu.Unlock()
		return append([]EvalEvent(nil), events...)
	}
}

// TestPanicBoundary_SettlementPanicIsContained pins the boundary a host meter
// panic must not cross: the entry point returns an error instead of unwinding
// into the embedder, publishes one settled outcome, and still returns the
// evaluation lease it drew.
func TestPanicBoundary_SettlementPanicIsContained(t *testing.T) {
	for _, ev := range precedenceEvaluators() {
		for _, entry := range precedenceEntries() {
			t.Run(ev.name+"/"+entry.name, func(t *testing.T) {
				name := ev.name + "/" + entry.name
				meter := &panicSettlementMeter{}
				eng := newSettlementEngine(t, ev.opt)
				bindings := precedenceBindings()
				if entry.prepare != nil {
					entry.prepare(t, eng, bindings)
				}
				seen := collectEvents(eng)
				base := eng.Stats()

				var evalErr error
				escaped := catchPanic(func() {
					_, evalErr = entry.invoke(t, WithMeter(t.Context(), meter), eng, retainingSource, bindings)
				})
				if escaped != nil {
					t.Fatalf("%s: settlement panic escaped the entry point (%v); a host meter panic must come back as an error", name, escaped)
				}

				var lerr *core.LispicoError
				if !errors.As(evalErr, &lerr) || lerr.Code != core.CodePanic {
					t.Fatalf("%s: error = %v, want %s cause", name, evalErr, core.CodePanic)
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
				if returns := meter.snapshot().returnCalls; returns != 1 {
					t.Fatalf("%s: ReturnEval calls = %d, want 1; a settlement panic must not leak the evaluation lease", name, returns)
				}
			})
		}
	}
}

// TestEngine_OnEvalCallbackPanicIsContained pins that an embedder observer
// which panics is contained at the publication point: it never reaches the
// caller, never replaces the evaluation's own outcome, and never buys the
// evaluation a second count.
func TestEngine_OnEvalCallbackPanicIsContained(t *testing.T) {
	cases := []struct {
		name    string
		source  string
		wantErr error
	}{
		{name: "success", source: retainingSource},
		{name: "failure", source: "(fail-soft)", wantErr: errNonterminalEval},
	}
	for _, entry := range outcomeEntries() {
		for _, tc := range cases {
			t.Run(entry.name+"/"+tc.name, func(t *testing.T) {
				name := entry.name + "/" + tc.name
				eng := newSettlementEngine(t)
				eng.OnEval(func(EvalEvent) { panic("observer failure") })
				base := eng.Stats()

				var result core.Value
				var evalErr error
				escaped := catchPanic(func() {
					result, evalErr = entry.invoke(t, t.Context(), eng, tc.source, precedenceBindings())
				})
				if escaped != nil {
					t.Fatalf("%s: observer panic escaped the entry point (%v); a callback panic must not reach the caller", name, escaped)
				}

				switch {
				case tc.wantErr == nil && evalErr != nil:
					t.Fatalf("%s: error = %v, want the evaluation's own success", name, evalErr)
				case tc.wantErr == nil && result == nil:
					t.Fatalf("%s: result = nil, want the evaluation's own result", name)
				case tc.wantErr != nil && !errors.Is(evalErr, tc.wantErr):
					t.Fatalf("%s: error = %v, want the evaluation's own cause", name, evalErr)
				}
				if d := eng.Stats().TotalEvals - base.TotalEvals; d != 1 {
					t.Fatalf("%s: TotalEvals delta = %d, want exactly 1; a recovered observer panic must not count the evaluation twice", name, d)
				}
			})
		}
	}
}

// TestEval_TerminalSettlementErrorWinsOverRecoveredPanic pins which cause wins
// when a recovered GoFunc panic meets a settlement that denies the retained
// charge the same evaluation left pending: the terminal settlement error is the
// one the caller, the event and the error statistics all report.
func TestEval_TerminalSettlementErrorWinsOverRecoveredPanic(t *testing.T) {
	runPrecedenceSource(t, precedencePanicSource, true, func(t *testing.T, name string, err error, event EvalEvent) {
		t.Helper()
		forEachPublishedError(t, err, event, func(where string, got error) {
			var lerr *core.LispicoError
			if !errors.As(got, &lerr) || lerr.Code != core.CodeResourceLimit {
				t.Fatalf("%s: %s = %v, want %s cause", name, where, got, core.CodeResourceLimit)
			}
			if !core.IsTerminalEvalError(got) {
				t.Fatalf("%s: %s = %v, want a terminal cause", name, where, got)
			}
		})
	})
}

// settlementBurn and callbackBurn are the two spans the reported duration has
// to treat differently. Both are lower bounds a spin loop guarantees, so the
// assertions hold on any machine speed without a clock race.
const (
	settlementBurn = 5 * time.Millisecond
	callbackBurn   = 50 * time.Millisecond
)

func burnFor(d time.Duration) {
	end := time.Now().Add(d)
	for time.Now().Before(end) {
	}
}

// burningSettlementMeter spends a known minimum inside the settlement path.
type burningSettlementMeter struct {
	recordingMeter
}

func (m *burningSettlementMeter) ChargeRetained(bytes, slots int64) error {
	burnFor(settlementBurn)
	return m.recordingMeter.ChargeRetained(bytes, slots)
}

// TestEval_DurationCoversSettlementAndExcludesCallbacks pins the span the
// reported duration measures. Settlement burns inside that span, so the
// duration is at least that long; the observer burns after it, so the whole
// call is at least that much longer than the duration it was handed.
func TestEval_DurationCoversSettlementAndExcludesCallbacks(t *testing.T) {
	for _, entry := range outcomeEntries() {
		t.Run(entry.name, func(t *testing.T) {
			meter := &burningSettlementMeter{}
			eng := newSettlementEngine(t)

			var mu sync.Mutex
			var events []EvalEvent
			eng.OnEval(func(e EvalEvent) {
				burnFor(callbackBurn)
				mu.Lock()
				events = append(events, e)
				mu.Unlock()
			})

			started := time.Now()
			_, err := entry.invoke(t, WithMeter(t.Context(), meter), eng, retainingSource, precedenceBindings())
			total := time.Since(started)
			if err != nil {
				t.Fatalf("%s: error = %v, want success", entry.name, err)
			}

			mu.Lock()
			got := append([]EvalEvent(nil), events...)
			mu.Unlock()
			if len(got) != 1 {
				t.Fatalf("%s: OnEval events = %d, want exactly 1", entry.name, len(got))
			}
			if charges := meter.snapshot().chargeCalls; charges != 1 {
				t.Fatalf("%s: ChargeRetained calls = %d, want 1; the case must leave settlement work to time", entry.name, charges)
			}
			if got[0].Duration < settlementBurn {
				t.Fatalf("%s: event Duration = %v, want at least the %v settlement spent; the reported duration must cover settlement",
					entry.name, got[0].Duration, settlementBurn)
			}
			if total-got[0].Duration < callbackBurn {
				t.Fatalf("%s: call took %v against an event Duration of %v; the reported duration must exclude the %v spent in the callback",
					entry.name, total, got[0].Duration, callbackBurn)
			}
		})
	}
}
