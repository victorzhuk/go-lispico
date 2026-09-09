package runtime

import (
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
