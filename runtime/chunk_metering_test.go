package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

// walkHorizonChain builds a value whose contextual walks cost far more than
// the value itself. Every doubling level holds the same child twice, so k
// levels cost k cons cells to build but 2^k visits to walk, and the leading
// single-element chain places the widest levels around the walks' fixed
// DefaultMaxStructuralDepth horizon. A walk entered at the value's own root
// therefore reaches levels that a walk entered above it prunes, which is what
// lets a chunk constant exceed the walk ceiling the form it came from stays
// under.
func walkHorizonChain(chain, doublings, leafWidth int) core.Value {
	leaf := make([]core.Value, leafWidth)
	for i := range leaf {
		leaf[i] = core.Int{V: int64(i)}
	}
	var v core.Value = core.NewList(leaf)
	for range doublings {
		v = core.NewList([]core.Value{v, v})
	}
	for range chain {
		v = core.NewList([]core.Value{v})
	}
	return v
}

// defaultWalkCeiling is the ceiling a walk handed a context without evaluation
// limits runs under — the ceiling the sizing code takes whenever it recurses
// with a context the caller never supplied.
const defaultWalkCeiling = int(core.DefaultMaxAllocationBytes / core.MeterValueSlotBytes)

// walkCeilingCtx returns a context whose contextual walks stop after ceiling
// visits, the same derivation the engine applies to MaxAllocationBytes.
func walkCeilingCtx(t *testing.T, ceiling int) context.Context {
	t.Helper()
	return core.WithEvalResourceLimits(t.Context(), 1<<30, ceiling*int(core.MeterValueSlotBytes))
}

func walkVisits(t *testing.T, ctx context.Context, v core.Value) (int, error) {
	t.Helper()
	return core.ValueNodeCountContext(ctx, v)
}

// chunkMeteringEngine builds a bytecode engine under ceiling and hands form to
// its compiler: form is bound and returned by a macro, so the compiler sees it
// verbatim without the reader or any construction call charging for it.
func chunkMeteringEngine(t *testing.T, ceiling int, form core.Value) Engine {
	t.Helper()
	limits := meteringLimits(t, 1<<30, ceiling*int(core.MeterValueSlotBytes))
	limits.MaxCacheNodes = 100_000_000
	eng := newMeteringStdlibEngine(t, true, limits)
	require.NoError(t, eng.Bind("chunk-form", form))
	_, err := eng.Eval(t.Context(), "chunk-form-macro", `(defmacro chunk-form-macro [] chunk-form)`)
	require.NoError(t, err, "the macro must expand to the bound form without charging for it")
	return eng
}

// TestChunkMetering_TerminalPublishesNoChunk pins the chunk-sizing refusal: a
// constant whose contextual walk refuses terminally while the chunk is being
// sized must surface that refusal to the caller. Nothing may be published in
// its place — no chunk carrying a zero byte count, no allocation charge of
// zero, no compile-cache entry, and no evaluation result.
func TestChunkMetering_TerminalPublishesNoChunk(t *testing.T) {
	skipUntilMeteringFields(t)

	// 12 doublings under a chain that ends exactly on the walk horizon: the
	// quote form prunes the widest level the constant itself still visits.
	constant := walkHorizonChain(1024-12, 12, 1)
	form := core.NewList([]core.Value{core.Symbol{V: "quote"}, constant})

	constantVisits, err := walkVisits(t, walkCeilingCtx(t, defaultWalkCeiling), constant)
	require.NoError(t, err)
	formVisits, err := walkVisits(t, walkCeilingCtx(t, defaultWalkCeiling), form)
	require.NoError(t, err)
	require.Greater(t, constantVisits, formVisits,
		"fixture: the constant must outvisit the form it sits in (constant=%d form=%d)", constantVisits, formVisits)

	ceiling := (formVisits + constantVisits) / 2
	_, err = walkVisits(t, walkCeilingCtx(t, ceiling), form)
	require.NoError(t, err, "fixture: the form must compile past the compiler's own node walk")
	_, err = walkVisits(t, walkCeilingCtx(t, ceiling), constant)
	require.Error(t, err, "fixture: the constant must refuse under the engine ceiling, or nothing is being pinned")

	eng := chunkMeteringEngine(t, ceiling, form)
	_, err = eng.Eval(t.Context(), "warm", "(+ 1 2)")
	require.NoError(t, err)
	before := eng.Stats().Cache

	got, err := eng.Eval(t.Context(), "chunk-sizing", `(chunk-form-macro)`)
	after := eng.Stats().Cache

	require.Error(t, err,
		"a Terminal raised while sizing a chunk constant must surface to the caller (result published=%v, cache entries %d->%d, bytes %d->%d)",
		got != nil, before.Entries, after.Entries, before.Bytes, after.Bytes)
	assert.True(t, isResourceLimit(t, err), "the sizing refusal must keep its %s code, got %v", core.CodeResourceLimit, err)
	assert.True(t, core.IsTerminalEvalError(err), "the sizing refusal must stay Terminal, got %v", err)
	assert.Nil(t, got, "no result may be published after a Terminal chunk-sizing refusal, got %T", got)
	assert.Equal(t, before.Entries, after.Entries,
		"no chunk may be admitted to the compile cache after a Terminal chunk-sizing refusal (entries %d->%d, bytes %d->%d)",
		before.Entries, after.Entries, before.Bytes, after.Bytes)
}

// TestChunkMetering_SubChunkHonoursContext pins the same refusal one level
// down: a fn body compiles into a sub-chunk, and sizing that sub-chunk's
// constants must run under the caller's context. The fixture is calibrated so
// the constant outvisits both the caller's ceiling and the default ceiling any
// unsupplied context would take, while every walk the compiler performs before
// the sizing stays under its own ceiling — so only a sub-chunk sized with the
// caller's context can refuse, and only a swallowed refusal can publish.
func TestChunkMetering_SubChunkHonoursContext(t *testing.T) {
	skipUntilMeteringFields(t)

	constant := walkHorizonChain(1024-20-1, 20, 4)
	body := core.NewList([]core.Value{core.Symbol{V: "quote"}, constant})
	form := core.NewList([]core.Value{core.Symbol{V: "fn"}, core.NewVector(nil), body})

	const ceiling = 2_000_000

	formVisits, err := walkVisits(t, walkCeilingCtx(t, ceiling), form)
	require.NoError(t, err, "fixture: the fn form must compile past the compiler's own node walk")
	_, err = walkVisits(t, walkCeilingCtx(t, defaultWalkCeiling), body)
	require.NoError(t, err, "fixture: the fn body must compile past the sub-compiler's own node walk")
	_, err = walkVisits(t, walkCeilingCtx(t, ceiling), constant)
	require.Error(t, err, "fixture: the constant must refuse under the caller's ceiling (form visits=%d)", formVisits)
	_, err = walkVisits(t, walkCeilingCtx(t, defaultWalkCeiling), constant)
	require.Error(t, err, "fixture: the constant must refuse under the default ceiling too, so a swallow cannot hide behind it")

	eng := chunkMeteringEngine(t, ceiling, form)
	_, err = eng.Eval(t.Context(), "warm", "(+ 1 2)")
	require.NoError(t, err)
	before := eng.Stats().Cache

	got, err := eng.Eval(t.Context(), "sub-chunk-sizing", `(chunk-form-macro)`)
	after := eng.Stats().Cache

	require.Error(t, err,
		"a Terminal raised while sizing a fn body's sub-chunk must surface to the caller (result published=%v, cache entries %d->%d, bytes %d->%d)",
		got != nil, before.Entries, after.Entries, before.Bytes, after.Bytes)
	assert.True(t, isResourceLimit(t, err), "the sub-chunk sizing refusal must keep its %s code, got %v", core.CodeResourceLimit, err)
	assert.True(t, core.IsTerminalEvalError(err), "the sub-chunk sizing refusal must stay Terminal, got %v", err)
	assert.Nil(t, got, "no closure may be published after a Terminal sub-chunk sizing refusal, got %T", got)
	assert.Equal(t, before.Entries, after.Entries,
		"no chunk may be admitted to the compile cache after a Terminal sub-chunk sizing refusal (entries %d->%d, bytes %d->%d)",
		before.Entries, after.Entries, before.Bytes, after.Bytes)
}
