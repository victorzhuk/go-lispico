package runtime

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/cl"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
)

// TestRuntimeScopedDefinitionFallbackOnce pins the once-only side-effect
// clause of the vm-scoped-definition-fallback spec delta at the public Engine
// seam: a top-level form that performs an observable mutation before reaching
// a scoped definition, evaluated cold and repeatedly under a VM engine, must
// perform that mutation exactly once per evaluation. A compiled prefix that
// executed before the whole-form tree-walker fallback — or a fallback retried
// after bytecode had begun — would double the count. Each evaluation must
// also return the same value and binding effects as the tree-walker, and the
// function created by the form's top-level definition under fallback must
// stay callable through Eval and named Call, repeatedly.
func TestRuntimeScopedDefinitionFallbackOnce(t *testing.T) {
	t.Parallel()

	for _, dia := range []struct {
		name string
		d    core.Dialect
		src  string
	}{
		{
			name: "clojure",
			d:    clojure.Dialect(),
			src:  `(do (tick) (defn f [x] x) (let [] (def scoped 42)))`,
		},
		{
			name: "cl",
			d:    cl.Dialect(),
			src:  `(progn (tick) (defn f (x) x) (let () (def scoped 42)))`,
		},
	} {
		t.Run(dia.name, func(t *testing.T) {
			t.Parallel()

			vmEng, vmTicks := newScopedOnceEngine(t, dia.d, WithBytecode())
			twEng, twTicks := newScopedOnceEngine(t, dia.d, WithTreeWalker())
			ctx := context.Background()

			// The tree-walker is the spec's source of truth for the value
			// and binding effects; pin it cold and repeated first.
			twCold, err := twEng.Eval(ctx, "tw-cold", dia.src)
			require.NoErrorf(t, err, "TestRuntimeScopedDefinitionFallbackOnce/%s: tree-walker cold Eval must succeed, got %v", dia.name, err)
			twWarm, err := twEng.Eval(ctx, "tw-warm", dia.src)
			require.NoErrorf(t, err, "TestRuntimeScopedDefinitionFallbackOnce/%s: tree-walker repeated Eval must succeed, got %v", dia.name, err)
			assert.Truef(t, twCold.Equals(twWarm),
				"TestRuntimeScopedDefinitionFallbackOnce/%s: tree-walker repeated Eval = %v, want the cold value %v", dia.name, twWarm, twCold)

			// Cold and repeated Eval under the VM engine: the scoped
			// definition must force whole-form fallback every time.
			vmCold, err := vmEng.Eval(ctx, "vm-cold", dia.src)
			require.NoErrorf(t, err, "TestRuntimeScopedDefinitionFallbackOnce/%s: VM cold Eval must succeed, got %v", dia.name, err)
			vmWarm, err := vmEng.Eval(ctx, "vm-warm", dia.src)
			require.NoErrorf(t, err, "TestRuntimeScopedDefinitionFallbackOnce/%s: VM repeated Eval must succeed, got %v", dia.name, err)

			// Once-only side effects: exactly one mutation per evaluation.
			// Two evaluations must leave the counter at two — a compiled
			// prefix running before the fallback, or a retried fallback,
			// would land at four.
			assert.Equal(t, int64(2), vmTicks.Load(),
				"TestRuntimeScopedDefinitionFallbackOnce/%s: two Evaluations must mutate exactly once each (ticks=2); a compiled prefix executed before the tree-walker fallback would double the count", dia.name)
			assert.Equal(t, int64(2), twTicks.Load(),
				"TestRuntimeScopedDefinitionFallbackOnce/%s: tree-walker control must also mutate exactly once per evaluation (ticks=2)", dia.name)

			// Same value and binding effects as the tree-walker.
			assert.Truef(t, twCold.Equals(vmCold),
				"TestRuntimeScopedDefinitionFallbackOnce/%s: VM cold Eval = %v, want the tree-walker value %v", dia.name, vmCold, twCold)
			assert.Truef(t, twWarm.Equals(vmWarm),
				"TestRuntimeScopedDefinitionFallbackOnce/%s: VM repeated Eval = %v, want the tree-walker value %v", dia.name, vmWarm, twWarm)

			// Binding effect: the scoped definition must stay in its lexical
			// scope on both engines, never leaking to the top level.
			for _, leak := range []struct {
				label string
				eng   Engine
			}{
				{"vm", vmEng},
				{"tree-walker", twEng},
			} {
				_, err := leak.eng.Eval(ctx, "leak-check", "scoped")
				require.Errorf(t, err, "TestRuntimeScopedDefinitionFallbackOnce/%s: %s engine must not leak the scoped definition to the top level", dia.name, leak.label)
				var le *core.LispicoError
				require.ErrorAsf(t, err, &le, "TestRuntimeScopedDefinitionFallbackOnce/%s: %s engine leak-check error must be typed", dia.name, leak.label)
				assert.Equal(t, "UndefinedError", le.Code,
					"TestRuntimeScopedDefinitionFallbackOnce/%s: %s engine must report the scoped definition as undefined at top level", dia.name, leak.label)
			}

			// The fallback-created function (bound by the form's top-level
			// defn while the whole form ran on the tree-walker) must remain
			// callable under the VM engine: through Eval and through named
			// Call, pinned against the tree-walker engine.
			vmCalled, err := vmEng.Eval(ctx, "vm-call-f", "(f 21)")
			require.NoErrorf(t, err, "TestRuntimeScopedDefinitionFallbackOnce/%s: VM Eval of the fallback-created function must succeed, got %v", dia.name, err)
			assert.Truef(t, core.Int{V: 21}.Equals(vmCalled),
				"TestRuntimeScopedDefinitionFallbackOnce/%s: (f 21) under the VM engine = %v, want 21", dia.name, vmCalled)

			vmNamed, err := vmEng.Call(ctx, "f", core.Int{V: 7})
			require.NoErrorf(t, err, "TestRuntimeScopedDefinitionFallbackOnce/%s: VM named Call of the fallback-created function must succeed, got %v", dia.name, err)
			assert.Truef(t, core.Int{V: 7}.Equals(vmNamed),
				"TestRuntimeScopedDefinitionFallbackOnce/%s: Call(f 7) under the VM engine = %v, want 7", dia.name, vmNamed)

			twNamed, err := twEng.Call(ctx, "f", core.Int{V: 7})
			require.NoErrorf(t, err, "TestRuntimeScopedDefinitionFallbackOnce/%s: tree-walker named Call of the fallback-created function must succeed, got %v", dia.name, err)
			assert.Truef(t, twNamed.Equals(vmNamed),
				"TestRuntimeScopedDefinitionFallbackOnce/%s: VM named Call = %v, want the tree-walker result %v", dia.name, vmNamed, twNamed)
		})
	}
}

// newScopedOnceEngine builds a stdlib-free engine under d with the given
// evaluator mode and a bound "tick" GoFunc whose call count the test reads as
// the observable mutation performed before the scoped definition.
func newScopedOnceEngine(t *testing.T, d core.Dialect, mode EngineOption) (Engine, *atomic.Int64) {
	t.Helper()
	var ticks atomic.Int64
	eng, err := New(nil, mode, WithDialect(d))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	require.NoError(t, eng.Bind("tick", core.GoFunc{
		Name: "tick",
		Fn: func(_ context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			ticks.Add(1)
			return core.Nil{}, nil
		},
	}))
	return eng, &ticks
}
