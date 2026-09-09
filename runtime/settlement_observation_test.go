package runtime

import (
	"context"
	"errors"
	"sync"
	"testing"

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
