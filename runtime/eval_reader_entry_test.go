package runtime

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/cl"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
)

// Reading charges the same ledger as compilation and execution, so a source
// can exhaust the budget before a single form runs. The reduction ceiling
// clears reading and running the one-call control; the allocation ceiling does
// not survive the wide literal.
const (
	readerEntryReductionCeiling = 1024
	readerEntryAllocCeiling     = 4 << 10
	readerEntryLiteralWidth     = 4096
)

// readerEntryDialect pairs a dialect with an over-budget source written in
// that dialect's own reader surface, so the charge covers what the surface
// actually produces rather than a syntax every dialect happens to share.
type readerEntryDialect struct {
	name       string
	dialect    core.Dialect
	overBudget string
}

func readerEntryDialects() []readerEntryDialect {
	ints := readerEntryItems("1", readerEntryLiteralWidth)
	refs := readerEntryItems("#'a", readerEntryLiteralWidth)
	return []readerEntryDialect{
		{name: "clojure", dialect: clojure.Dialect(), overBudget: "[" + ints + "]"},
		{name: "cl", dialect: cl.Dialect(), overBudget: "#(" + ints + ")"},
		{name: "reader-vector", dialect: core.FullDialect().WithReaderVector(), overBudget: "#(" + ints + ")"},
		{name: "function-ref", dialect: core.FullDialect().WithFunctionRef(), overBudget: "(quote (" + refs + "))"},
	}
}

func readerEntryItems(item string, n int) string {
	var b strings.Builder
	b.Grow(n * (len(item) + 1))
	for i := range n {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(item)
	}
	return b.String()
}

// readerEntryMeter grants every lease; it exists to prove which meter the
// ledger drew its lease from, not to refuse anything itself.
type readerEntryMeter struct {
	leases atomic.Int64
}

func (m *readerEntryMeter) LeaseEval(reductions, allocBytes int64) (int64, int64, error) {
	m.leases.Add(1)
	return reductions, allocBytes, nil
}

func (m *readerEntryMeter) ReturnEval(_, _ int64) {}

func (m *readerEntryMeter) ChargeRetained(_, _ int64) error { return nil }

func (m *readerEntryMeter) ReleaseRetained(_, _ int64) {}

type readerEntryMeterSource struct {
	name        string
	engineMeter bool
}

func readerEntryMeterSources() []readerEntryMeterSource {
	return []readerEntryMeterSource{
		{name: "context-meter", engineMeter: false},
		{name: "engine-meter", engineMeter: true},
	}
}

type readerEntryFixture struct {
	engine Engine
	ctx    context.Context
	calls  *atomic.Int64
	meter  *readerEntryMeter
}

func newReaderEntryEngine(t *testing.T, log *slog.Logger, bytecode bool, d core.Dialect, limits ResourceLimits, meter Meter) (Engine, *atomic.Int64) {
	t.Helper()

	opts := []EngineOption{WithDialect(d), WithResourceLimits(limits)}
	// The losing mode option goes first: a break in last-wins precedence
	// flips the evaluator the assertion below reads.
	if bytecode {
		opts = append(opts, WithTreeWalker(), WithBytecode())
	} else {
		opts = append(opts, WithBytecode(), WithTreeWalker())
	}
	if meter != nil {
		opts = append(opts, WithEngineMeter(meter))
	}

	eng, err := New(log, opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	require.Equal(t, bytecode, eng.(*engineImpl).bytecodeEvaluator != nil, "the last mode option must select the evaluator")

	calls := &atomic.Int64{}
	require.NoError(t, eng.Bind("mark", core.GoFunc{
		Name: "mark",
		Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
			calls.Add(1)
			return core.Int{V: 1}, nil
		},
	}))
	return eng, calls
}

func newReaderEntryFixture(t *testing.T, bytecode bool, d core.Dialect, src readerEntryMeterSource, limits ResourceLimits) readerEntryFixture {
	t.Helper()

	meter := &readerEntryMeter{}
	f := readerEntryFixture{ctx: context.Background(), meter: meter}
	if src.engineMeter {
		f.engine, f.calls = newReaderEntryEngine(t, nil, bytecode, d, limits, meter)
	} else {
		f.engine, f.calls = newReaderEntryEngine(t, nil, bytecode, d, limits, nil)
		f.ctx = WithMeter(f.ctx, meter)
	}
	return f
}

func readerEntryOverLimits(t *testing.T) ResourceLimits {
	t.Helper()
	return meteringLimits(t, 1_000_000, readerEntryAllocCeiling)
}

func readerEntryInLimits(t *testing.T) ResourceLimits {
	t.Helper()
	return meteringLimits(t, readerEntryReductionCeiling, 1<<20)
}

// markThen puts the site marker ahead of the over-budget literal: the marker
// is the first form of the source, so a non-zero call count means forms ran
// before the reader's charge was settled.
func markThen(literal string) string {
	return "(mark)\n" + literal
}

// TestPublicEntries_ReaderBudget pins that an over-budget source is refused at
// every public entry point before any form executes, across both evaluator
// modes, all four dialect reader surfaces and both meter sources.
func TestPublicEntries_ReaderBudget(t *testing.T) {
	skipUntilMeteringFields(t)

	for _, bytecode := range []bool{false, true} {
		t.Run(evalModeName(bytecode), func(t *testing.T) {
			for _, d := range readerEntryDialects() {
				t.Run(d.name, func(t *testing.T) {
					for _, src := range readerEntryMeterSources() {
						t.Run(src.name, func(t *testing.T) {
							t.Run("Eval", func(t *testing.T) {
								readerEntryEvalCases(t, bytecode, d, src)
							})
							t.Run("EvalWithBindings", func(t *testing.T) {
								readerEntryEvalWithBindingsCases(t, bytecode, d, src)
							})
							t.Run("LoadScope", func(t *testing.T) {
								readerEntryLoadScopeCases(t, bytecode, d, src)
							})
						})
					}
				})
			}
		})
	}
}

func readerEntryEvalCases(t *testing.T, bytecode bool, d readerEntryDialect, src readerEntryMeterSource) {
	t.Helper()

	t.Run("over-budget", func(t *testing.T) {
		f := newReaderEntryFixture(t, bytecode, d.dialect, src, readerEntryOverLimits(t))
		_, err := f.engine.Eval(f.ctx, "over-budget", markThen(d.overBudget))

		assert.True(t, isResourceLimit(t, err), "expected ResourceLimitError, got %v", err)
		assert.Equal(t, int64(0), f.calls.Load(), "no form may execute after a refused read")
		assert.Greater(t, f.meter.leases.Load(), int64(0), "the ledger must lease from the configured meter")
	})

	t.Run("in-budget", func(t *testing.T) {
		f := newReaderEntryFixture(t, bytecode, d.dialect, src, readerEntryInLimits(t))
		got, err := f.engine.Eval(f.ctx, "in-budget", "(mark)")

		require.NoError(t, err)
		assert.True(t, core.Int{V: 1}.Equals(got), "got %v", got)
		assert.Equal(t, int64(1), f.calls.Load(), "the in-budget control must execute its form")
	})
}

func readerEntryEvalWithBindingsCases(t *testing.T, bytecode bool, d readerEntryDialect, src readerEntryMeterSource) {
	t.Helper()

	bindings := map[string]core.Value{"seed": core.Int{V: 7}}

	t.Run("over-budget", func(t *testing.T) {
		f := newReaderEntryFixture(t, bytecode, d.dialect, src, readerEntryOverLimits(t))
		_, err := f.engine.EvalWithBindings(f.ctx, markThen(d.overBudget), bindings)

		assert.True(t, isResourceLimit(t, err), "expected ResourceLimitError, got %v", err)
		assert.Equal(t, int64(0), f.calls.Load(), "no form may execute after a refused read")
	})

	t.Run("in-budget", func(t *testing.T) {
		f := newReaderEntryFixture(t, bytecode, d.dialect, src, readerEntryInLimits(t))
		got, err := f.engine.EvalWithBindings(f.ctx, "(mark)", bindings)

		require.NoError(t, err)
		assert.True(t, core.Int{V: 1}.Equals(got), "got %v", got)
		assert.Equal(t, int64(1), f.calls.Load(), "the in-budget control must execute its form")
	})
}

func readerEntryLoadScopeCases(t *testing.T, bytecode bool, d readerEntryDialect, src readerEntryMeterSource) {
	t.Helper()

	bindings := map[string]core.Value{"seed": core.Int{V: 7}}

	t.Run("over-budget", func(t *testing.T) {
		f := newReaderEntryFixture(t, bytecode, d.dialect, src, readerEntryOverLimits(t))
		_, scope, err := f.engine.LoadScope(f.ctx, markThen(d.overBudget), bindings)

		assert.True(t, isResourceLimit(t, err), "expected ResourceLimitError, got %v", err)
		assert.Equal(t, int64(0), f.calls.Load(), "no form may execute after a refused read")
		assert.Nil(t, scope, "a refused read must publish no scope")
	})

	t.Run("in-budget", func(t *testing.T) {
		f := newReaderEntryFixture(t, bytecode, d.dialect, src, readerEntryInLimits(t))
		got, scope, err := f.engine.LoadScope(f.ctx, "(mark)", bindings)

		require.NoError(t, err)
		assert.True(t, core.Int{V: 1}.Equals(got), "got %v", got)
		assert.Equal(t, int64(1), f.calls.Load(), "the in-budget control must execute its form")
		require.NotNil(t, scope)
		seed, ok := scope.Get("seed")
		require.True(t, ok, "the in-budget control must publish its bindings")
		assert.True(t, core.Int{V: 7}.Equals(seed), "got %v", seed)
	})
}

// TestPublicEntries_ReaderBudgetHotReload pins the fourth entry point: a
// background reload whose source is refused by the reader publishes no
// replacement bindings and leaves the previous definitions resolvable.
func TestPublicEntries_ReaderBudgetHotReload(t *testing.T) {
	skipUntilMeteringFields(t)

	for _, bytecode := range []bool{false, true} {
		t.Run(evalModeName(bytecode), func(t *testing.T) {
			for _, d := range readerEntryDialects() {
				t.Run(d.name, func(t *testing.T) {
					for _, src := range readerEntryMeterSources() {
						t.Run(src.name, func(t *testing.T) {
							t.Run("over-budget", func(t *testing.T) {
								readerEntryReloadRefused(t, bytecode, d, src)
							})
							t.Run("in-budget", func(t *testing.T) {
								readerEntryReloadPublishes(t, bytecode, d, src)
							})
						})
					}
				})
			}
		})
	}
}

func newReaderEntryWatcher(t *testing.T, bytecode bool, d core.Dialect, src readerEntryMeterSource, limits ResourceLimits) (*fileWatcher, Engine, *atomic.Int64, *bytes.Buffer) {
	t.Helper()

	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))

	var eng Engine
	var calls *atomic.Int64
	ctx := context.Background()
	if src.engineMeter {
		eng, calls = newReaderEntryEngine(t, log, bytecode, d, limits, &readerEntryMeter{})
	} else {
		eng, calls = newReaderEntryEngine(t, log, bytecode, d, limits, nil)
		ctx = WithMeter(ctx, &readerEntryMeter{})
	}

	w := newFileWatcher(eng.(*engineImpl), t.TempDir(), 10*time.Millisecond)
	w.ctx = ctx
	return w, eng, calls, &buf
}

func writeReaderEntryFile(t *testing.T, w *fileWatcher, name, content string) string {
	t.Helper()
	path := filepath.Join(w.dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func readerEntryReloadRefused(t *testing.T, bytecode bool, d readerEntryDialect, src readerEntryMeterSource) {
	t.Helper()

	w, eng, calls, buf := newReaderEntryWatcher(t, bytecode, d.dialect, src, readerEntryOverLimits(t))
	require.NoError(t, eng.Bind("existing", core.Int{V: 42}))

	path := writeReaderEntryFile(t, w, "over.lisp", "(mark)\n(def existing 99)\n(def replacement 1)\n"+d.overBudget)
	w.reloadFile(path)

	assert.Equal(t, int64(0), calls.Load(), "no form may execute after a refused read")

	val, ok := eng.RootEnv().Get("existing")
	require.True(t, ok, "the previous definition must survive a refused reload")
	assert.Equal(t, int64(42), val.(core.Int).V, "a refused reload must publish no replacement binding")

	_, hasReplacement := eng.RootEnv().Get("replacement")
	assert.False(t, hasReplacement, "a refused reload must publish no new binding")
	assert.NotContains(t, buf.String(), "reloaded file")
	assert.Contains(t, buf.String(), "parse file")
}

func readerEntryReloadPublishes(t *testing.T, bytecode bool, d readerEntryDialect, src readerEntryMeterSource) {
	t.Helper()

	w, eng, calls, buf := newReaderEntryWatcher(t, bytecode, d.dialect, src, readerEntryInLimits(t))
	require.NoError(t, eng.Bind("existing", core.Int{V: 42}))

	path := writeReaderEntryFile(t, w, "in.lisp", "(mark)\n(def existing 99)\n(def replacement 1)")
	w.reloadFile(path)

	assert.Equal(t, int64(1), calls.Load(), "the in-budget control must execute its form")

	val, ok := eng.RootEnv().Get("existing")
	require.True(t, ok)
	assert.Equal(t, int64(99), val.(core.Int).V, "an in-budget reload must publish its replacement binding")

	replacement, ok := eng.RootEnv().Get("replacement")
	require.True(t, ok, "an in-budget reload must publish its new bindings")
	assert.Equal(t, int64(1), replacement.(core.Int).V)
	assert.Contains(t, buf.String(), "reloaded file")
}
