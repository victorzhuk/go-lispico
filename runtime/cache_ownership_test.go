package runtime

import (
	"context"
	"reflect"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/core/compiler"
	"github.com/victorzhuk/go-lispico/core/vm"
)

// strAddr returns the backing-storage address of a string payload.
func strAddr(s string) uintptr {
	return reflect.ValueOf(s).Pointer()
}

func inSourceRange(src string, s string) bool {
	if s == "" {
		return false
	}
	start := strAddr(src)
	return strAddr(s) >= start && strAddr(s) < start+uintptr(len(src))
}

// chunkPayloads collects every retained string payload in a chunk tree:
// names, local names, constant leaves, container elements and map keys and
// values, and macro and lambda prototypes, recursively through subchunks.
func chunkPayloads(c *vm.Chunk, out *[]string) {
	if c == nil {
		return
	}
	*out = append(*out, c.Name)
	*out = append(*out, c.LocalNames...)
	for _, v := range c.Constants {
		valuePayloads(v, out, core.DefaultMaxStructuralDepth)
	}
	for _, sub := range c.SubChunks {
		chunkPayloads(sub, out)
	}
}

func valuePayloads(v core.Value, out *[]string, depth int) {
	if depth < 0 {
		return
	}
	switch val := v.(type) {
	case core.String:
		*out = append(*out, val.V)
	case core.Symbol:
		*out = append(*out, val.V)
	case core.Keyword:
		*out = append(*out, val.V)
	case core.List:
		for i := range val.Len() {
			valuePayloads(val.At(i), out, depth-1)
		}
	case core.Vector:
		for i := range val.Len() {
			valuePayloads(val.At(i), out, depth-1)
		}
	case *core.HashMap:
		for _, pair := range val.Pairs() {
			valuePayloads(pair[0], out, depth-1)
			valuePayloads(pair[1], out, depth-1)
		}
	case core.Macro:
		*out = append(*out, val.Name)
		for _, p := range val.Params {
			*out = append(*out, p.V)
		}
		*out = append(*out, val.Variadic.V)
		for _, b := range val.Body {
			valuePayloads(b, out, depth-1)
		}
	case core.Lambda:
		*out = append(*out, val.Name)
		for _, p := range val.Params {
			*out = append(*out, p.V)
		}
		*out = append(*out, val.Variadic.V)
		for _, b := range val.Body {
			valuePayloads(b, out, depth-1)
		}
	}
}

func publishedChunks(be *bytecodeEvaluator) []*vm.Chunk {
	var out []*vm.Chunk
	for i := range be.stripes {
		s := &be.stripes[i]
		s.mu.Lock()
		for _, entry := range s.cache {
			out = append(out, entry.chunk)
		}
		s.mu.Unlock()
	}
	return out
}

// readForms reads src the way the engine does, so fixtures share the
// production allocation behavior the ownership table keys on.
func readForms(t *testing.T, src string) []core.Value {
	t.Helper()
	forms, _, err := clojure.Dialect().ReadWithContextStats(t.Context(), src, 200)
	require.NoError(t, err)
	return forms
}

func ownedIdentitiesFor(t *testing.T, forms []core.Value, src string) map[stringIdentity]core.Value {
	t.Helper()
	owned := make(map[stringIdentity]core.Value)
	for _, form := range forms {
		table, err := ownedStringIdentities(t.Context(), form, src)
		require.NoError(t, err)
		for id, v := range table {
			owned[id] = v
		}
	}
	return owned
}

// TestCacheOwnership_AllRetainedFields publishes a cache entry compiled from
// source carrying every retained string-bearing field, then walks the
// published chunk tree and asserts no payload aliases the source storage:
// chunk and closure names, local names, string, symbol, and keyword
// constants, list and vector elements, HashMap keys and values, and the
// defmacro prototype's name, params, and body.
func TestCacheOwnership_AllRetainedFields(t *testing.T) {
	src := `(def config '{:name "cfg-name" :mode :fast :tags ["a" "b"] :meta '("m1" m-sym)})` +
		`(defn worker [x] (let [label "local-label"] label))` +
		`(defmacro mac [f] [f "macro-literal"])` +
		`(worker "arg-string") (mac "expanded-input")`
	e := newCacheLimitsEngine(t, ResourceLimits{MaxCacheEntries: 16, MaxCacheBytes: 1 << 30, MaxCacheNodes: 1 << 30, MaxCollectionLen: 1 << 30})
	_, err := e.Eval(t.Context(), "owned", src)
	require.NoError(t, err)
	// The defmacro in src bumps the macro epoch at run time, so its own
	// and earlier forms' epoch-0 entries are flushed by design; a second
	// identical Eval republishes every form at the new epoch.
	_, err = e.Eval(t.Context(), "owned", src)
	require.NoError(t, err)

	chunks := publishedChunks(e.(*engineImpl).bytecodeEvaluator)
	require.NotEmpty(t, chunks, "expected a published cache entry")

	var payloads []string
	sawHashMap, sawMacro, sawNested := false, false, false
	for _, c := range chunks {
		chunkPayloads(c, &payloads)
		for _, sub := range c.SubChunks {
			sawNested = sawNested || len(sub.LocalNames) > 0
		}
		for _, v := range c.Constants {
			switch v.(type) {
			case *core.HashMap:
				sawHashMap = true
			case core.Macro:
				sawMacro = true
			}
		}
	}
	require.NotEmpty(t, payloads, "walker collected no payloads; assertions would be vacuous")
	require.True(t, sawHashMap, "fixture published no HashMap constant; map keys would go unverified")
	require.True(t, sawMacro, "fixture published no Macro prototype; defmacro fields would go unverified")
	require.True(t, sawNested, "fixture published no nested chunk; subchunk payloads would go unverified")
	for _, p := range payloads {
		require.False(t, inSourceRange(src, p), "published payload %q aliases the source", p)
	}

	// Semantics unchanged: re-expansion and re-evaluation behave the same.
	v, err := e.Eval(t.Context(), "owned", `(mac "again")`)
	require.NoError(t, err)
	require.NotNil(t, v)
}

// TestCacheOwnership_EscapedStringReused pins the escaped-identity seam: a
// current-read escaped literal is published with its original reader-owned
// storage; an unescaped literal with identical contents — a literal newline
// in the source versus the escaped \n spelling — aliases the source and is
// cloned, so only the escaped payload keeps its address; empty strings need
// no copy. The input form is never mutated.
func TestCacheOwnership_EscapedStringReused(t *testing.T) {
	src := `"a\nb" "plain" "plain" "" "a` + "\n" + `b"`
	forms := readForms(t, src)
	require.Len(t, forms, 5)

	var esc, plain1, plain2, empty, bor core.String
	require.IsType(t, core.String{}, forms[0])
	esc = forms[0].(core.String)
	plain1 = forms[1].(core.String)
	plain2 = forms[2].(core.String)
	empty = forms[3].(core.String)
	bor = forms[4].(core.String)
	require.Equal(t, "a\nb", esc.V)
	// Equal contents, distinct storage: escaped owns its bytes, the
	// unescaped spelling aliases the source.
	require.Equal(t, esc.V, bor.V)
	require.NotEqual(t, strAddr(esc.V), strAddr(bor.V), "escaped and borrowed spellings must not share storage")
	require.True(t, inSourceRange(src, bor.V), "unescaped payload must alias the source")
	require.False(t, inSourceRange(src, esc.V), "escaped payload must be reader-owned")

	chunk := &vm.Chunk{Name: "t", Constants: []core.Value{esc, plain1, plain2, empty, bor}}
	owned := ownedIdentitiesFor(t, forms, src)

	// The ownership table records the escaped payloads and nothing borrowed.
	for _, id := range []stringIdentity{
		{strAddr(esc.V), len(esc.V)},
	} {
		_, ok := owned[id]
		require.True(t, ok, "escaped payload %q missing from the ownership table", esc.V)
	}
	_, borrowed := owned[stringIdentity{strAddr(plain1.V), len(plain1.V)}]
	require.False(t, borrowed, "source-aliasing payload must not be classified owned")

	be := &bytecodeEvaluator{}
	out, err := be.detachCachedChunk(context.Background(), chunk, owned)
	require.NoError(t, err)

	outEsc := out.Constants[0].(core.String)
	require.Equal(t, strAddr(esc.V), strAddr(outEsc.V), "escaped payload was recopied; the existing reader allocation must be reused")
	require.Equal(t, "a\nb", outEsc.V)

	outPlain1 := out.Constants[1].(core.String)
	require.NotEqual(t, strAddr(plain1.V), strAddr(outPlain1.V), "source-aliasing payload published uncloned")
	require.Equal(t, "plain", outPlain1.V)

	outPlain2 := out.Constants[2].(core.String)
	require.NotEqual(t, strAddr(plain2.V), strAddr(outPlain2.V), "second borrowed payload published uncloned")
	require.Equal(t, "plain", outPlain2.V)

	require.Equal(t, "", out.Constants[3].(core.String).V)

	outBor := out.Constants[4].(core.String)
	require.Equal(t, esc.V, outBor.V, "borrowed spelling must decode to the same text")
	require.NotEqual(t, strAddr(bor.V), strAddr(outBor.V), "borrowed payload published uncloned")
	require.NotEqual(t, strAddr(esc.V), strAddr(outBor.V), "cloned borrowed payload must not merge with the escaped one")

	// Input untouched.
	require.Equal(t, strAddr(plain1.V), strAddr(chunk.Constants[1].(core.String).V), "input form was mutated")
	require.Equal(t, esc.V, chunk.Constants[0].(core.String).V)
	require.Equal(t, chunk.NodeCount, out.NodeCount)
	require.Equal(t, chunk.DeepBytes, out.DeepBytes)
}

// TestCacheOwnership_DoesNotMutateInput pins that detachment rebuilds
// containers instead of writing through shared storage: the original list and
// map keep their own payloads, and the map is rebuilt with both detached keys
// and detached values.
func TestCacheOwnership_DoesNotMutateInput(t *testing.T) {
	src := `"orig-key" "orig-val"`
	forms := readForms(t, src)
	origKey := forms[0].(core.String).V
	origVal := forms[1].(core.String).V
	l := core.NewList([]core.Value{forms[0].(core.String), core.Int{V: 1}})
	m := core.NewHashMap()
	require.NoError(t, m.Set(forms[0].(core.String), forms[1].(core.String)))

	chunk := &vm.Chunk{Name: "t", Constants: []core.Value{l, m}}
	be := &bytecodeEvaluator{}
	out, err := be.detachCachedChunk(context.Background(), chunk, nil)
	require.NoError(t, err)

	// Original list untouched; the detached list carries a cloned payload.
	outList := out.Constants[0].(core.List)
	require.Equal(t, strAddr(origKey), strAddr(l.ToSlice()[0].(core.String).V), "input list was mutated")
	require.NotEqual(t, strAddr(origKey), strAddr(outList.ToSlice()[0].(core.String).V), "list payload published uncloned")

	// Original map untouched; detached map carries detached keys AND values.
	outMap := out.Constants[1].(*core.HashMap)
	require.NotSame(t, m, outMap, "detached map must be a new container")
	pairs := m.Pairs()
	require.Len(t, pairs, 1)
	require.Equal(t, strAddr(origKey), strAddr(pairs[0][0].(core.String).V), "input map key was mutated")
	require.Equal(t, strAddr(origVal), strAddr(pairs[0][1].(core.String).V), "input map value was mutated")
	outPairs := outMap.Pairs()
	require.Len(t, outPairs, 1)
	require.NotEqual(t, strAddr(origKey), strAddr(outPairs[0][0].(core.String).V), "map key published uncloned")
	require.NotEqual(t, strAddr(origVal), strAddr(outPairs[0][1].(core.String).V), "map value published uncloned")
}

// TestCacheOwnership_AdmissionBranches pins that only a successfully reserved
// candidate is detached: disabled, oversized, and reservation-refused
// candidates never publish; an exact byte ceiling admits while one byte short
// refuses before any copy; a stale-epoch replacement releases exactly the old
// reservation.
func TestCacheOwnership_AdmissionBranches(t *testing.T) {
	src := `(def seven " 七 ")`

	// ResourceLimits floors MaxCacheEntries at 4096, so the entry ceiling
	// cannot express "cache off"; the node ceiling refuses the same
	// cacheFitsAlone gate and keeps the candidate borrowed.
	t.Run("node-ceiling-refusal-stays-borrowed", func(t *testing.T) {
		e := newCacheLimitsEngine(t, ResourceLimits{MaxCacheEntries: 16, MaxCacheBytes: 1 << 30, MaxCacheNodes: 1, MaxCollectionLen: 1 << 30})
		v, err := e.Eval(t.Context(), "branch", src)
		require.NoError(t, err)
		require.Equal(t, " 七 ", v.(core.String).V)
		require.Equal(t, 0, cacheStats(t, e).Entries)
	})

	t.Run("oversized-stays-borrowed", func(t *testing.T) {
		e := newCacheLimitsEngine(t, ResourceLimits{MaxCacheEntries: 16, MaxCacheBytes: 1, MaxCacheNodes: 1 << 30, MaxCollectionLen: 1 << 30})
		_, err := e.Eval(t.Context(), "branch", src)
		require.NoError(t, err)
		require.Equal(t, 0, cacheStats(t, e).Entries)
	})

	charged := func(t *testing.T) int64 {
		t.Helper()
		m := &recordingMeter{}
		e, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()), WithEngineMeter(m), WithResourceLimits(ResourceLimits{MaxCacheEntries: 16, MaxCacheBytes: 1 << 30, MaxCacheNodes: 1 << 30, MaxCollectionLen: 1 << 30}))
		require.NoError(t, err)
		t.Cleanup(func() { _ = e.Close() })
		// A metered first touch also evaluates the deferred stdlib
		// load, so identify the candidate by its payload rather than
		// by charge deltas.
		_, err = e.Eval(t.Context(), "warm", `(def warm "w")`)
		require.NoError(t, err)
		_, err = e.Eval(t.Context(), "branch", src)
		require.NoError(t, err)
		for _, c := range publishedChunks(e.(*engineImpl).bytecodeEvaluator) {
			var ps []string
			chunkPayloads(c, &ps)
			for _, p := range ps {
				if p == " 七 " {
					return c.DeepBytes
				}
			}
		}
		t.Fatal("candidate chunk not found among published entries")
		return 0
	}
	size := charged(t)

	t.Run("exact-ceiling-admits", func(t *testing.T) {
		e := newCacheLimitsEngine(t, ResourceLimits{MaxCacheEntries: 16, MaxCacheBytes: int(size), MaxCacheNodes: 1 << 30, MaxCollectionLen: 1 << 30})
		_, err := e.Eval(t.Context(), "branch", src)
		require.NoError(t, err)
		require.Equal(t, 1, cacheStats(t, e).Entries)
	})

	t.Run("one-byte-short-refuses", func(t *testing.T) {
		e := newCacheLimitsEngine(t, ResourceLimits{MaxCacheEntries: 16, MaxCacheBytes: int(size) - 1, MaxCacheNodes: 1 << 30, MaxCollectionLen: 1 << 30})
		_, err := e.Eval(t.Context(), "branch", src)
		require.NoError(t, err)
		require.Equal(t, 0, cacheStats(t, e).Entries)
	})

	t.Run("retained-refusal-releases", func(t *testing.T) {
		// Engine-level blanket denials trip the deferred stdlib load and
		// env settlement before admission; the refusal path itself is a
		// reserveCacheOccupancy contract, so pin it directly: a denied
		// ChargeRetained rolls the budget charge back and reports no
		// reservation, leaving the candidate borrowed.
		globals := core.NewEnv(nil)
		chunk := &vm.Chunk{Name: "r", Code: []vm.Instruction{vm.Encode(vm.OpReturn, 0)}, DeepBytes: 8, NodeCount: 1}
		denied := 0
		be := &bytecodeEvaluator{
			globals:            globals,
			maxCacheEntries:    16,
			maxCacheBytes:      1 << 30,
			maxCacheNodes:      1 << 30,
			maxStructuralDepth: core.DefaultMaxStructuralDepth,
			engineMeter:        denyMeter{onCharge: func() { denied++ }},
		}
		require.False(t, be.reserveCacheOccupancy(chunk), "denied retained charge must not reserve")
		require.Equal(t, 1, denied, "the reservation must have reached the meter once")
		require.Equal(t, int64(0), be.cacheEntries.Load(), "budget counters must roll back after denial")
		require.Equal(t, int64(0), be.cacheBytes.Load(), "budget counters must roll back after denial")
		require.Equal(t, int64(0), be.cacheNodes.Load(), "budget counters must roll back after denial")
	})

	t.Run("stale-epoch-replacement-releases-old-reservation", func(t *testing.T) {
		m := &recordingMeter{}
		e, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()), WithEngineMeter(m), WithResourceLimits(ResourceLimits{MaxCacheEntries: 16, MaxCacheBytes: 1 << 30, MaxCacheNodes: 1 << 30, MaxCollectionLen: 1 << 30}))
		require.NoError(t, err)
		t.Cleanup(func() { _ = e.Close() })
		macroSrc := `(defmacro bump [x] x) (bump "once")`
		_, err = e.Eval(t.Context(), "branch", macroSrc)
		require.NoError(t, err)
		_, err = e.Eval(t.Context(), "branch", macroSrc)
		require.NoError(t, err)
		snap := m.snapshot()
		// Form 0 (the defmacro) publishes at epoch 0 and goes stale when
		// its own runtime bind bumps the epoch; form 1 is compiled after
		// the bump and publishes at epoch 1. The replacement releases
		// exactly the one stale reservation and nothing else.
		require.Equal(t, 1, snap.releaseCalls, "the stale-epoch entry must release exactly its own reservation")
		require.Equal(t, 2, cacheStats(t, e).Entries)
		// The counters describe exactly the published tree — no stale
		// reservation leaked into cacheBytes.
		var published int64
		var nodes int64
		for _, c := range publishedChunks(e.(*engineImpl).bytecodeEvaluator) {
			published += c.DeepBytes
			nodes += int64(c.NodeCount)
		}
		require.Equal(t, published, cacheStats(t, e).Bytes)
		require.Equal(t, nodes, int64(cacheStats(t, e).Nodes))
	})
}

// TestCacheOwnership_LowStructuralDepthStillCaches pins that detachment is
// not governed by the user eval structural-depth ceiling: a deep uncalled
// function body compiles, publishes, and re-evaluates under a tiny
// MaxStructuralDepth.
func TestCacheOwnership_LowStructuralDepthStillCaches(t *testing.T) {
	body := "x"
	for range 64 {
		body = "[" + body + "]"
	}
	src := `(defn deep [x] ` + body + `)`
	e := newCacheLimitsEngine(t, ResourceLimits{MaxCacheEntries: 16, MaxCacheBytes: 1 << 30, MaxCacheNodes: 1 << 30, MaxCollectionLen: 1 << 30, MaxStructuralDepth: 8})
	_, err := e.Eval(t.Context(), "deep", src)
	require.NoError(t, err)
	require.Positive(t, cacheStats(t, e).Entries)
	_, err = e.Eval(t.Context(), "deep", src)
	require.NoError(t, err)
}

// TestCacheOwnership_ExactRetainedBytes recomputes a compiled chunk's
// retained size term by term from meter constants and literal lengths — not
// by calling the production estimator — and pins that Chunk.Name is charged
// exactly once.
func TestCacheOwnership_ExactRetainedBytes(t *testing.T) {
	form := core.NewList([]core.Value{core.Symbol{V: "quote"}, core.String{V: "abcd"}})
	comp := compiler.NewCompilerWithDialect("<eval>", clojure.Dialect())
	comp.SetContext(t.Context())
	require.NoError(t, comp.Compile(form))
	require.NoError(t, comp.EmitReturn())
	require.NoError(t, comp.MarkCapturesContext(t.Context()))
	chunk := comp.Chunk()
	require.NoError(t, chunk.Validate())

	require.Equal(t, "<eval>", chunk.Name)
	require.Empty(t, chunk.LocalNames)
	require.Empty(t, chunk.SubChunks)
	require.Len(t, chunk.Constants, 1)
	require.Equal(t, "abcd", chunk.Constants[0].(core.String).V)

	var expected int64
	expected += int64(len(chunk.Code)) * core.MeterInstructionBytes
	expected += int64(len(chunk.Fused)) * core.MeterFusedOpBytes
	expected += core.ValueSlotsBytes(len(chunk.Constants))
	expected += core.StringShallowBytes(len(chunk.Name))
	for _, name := range chunk.LocalNames {
		expected += core.StringShallowBytes(len(name))
	}
	for _, constant := range chunk.Constants {
		switch c := constant.(type) {
		case core.String:
			expected += core.StringShallowBytes(len(c.V))
		case core.Symbol:
			expected += core.StringShallowBytes(len(c.V))
		case core.Keyword:
			expected += core.StringShallowBytes(len(c.V))
		default:
			t.Fatalf("unexpected constant type %T in the hand-computed fixture", constant)
		}
	}
	expected += core.ValueSlotsBytes(len(chunk.SubChunks))
	for _, sub := range chunk.SubChunks {
		expected += core.StringShallowBytes(len(sub.Name))
		expected += int64(len(sub.Code)) * core.MeterInstructionBytes
	}
	require.Equal(t, expected, chunk.DeepBytes,
		"retained size must include Chunk.Name exactly once, with no double-charged payload")

	withoutName := expected - core.StringShallowBytes(len(chunk.Name))
	require.NotEqual(t, withoutName, chunk.DeepBytes, "the Name term must be nonzero for this fixture")
}

var errRetainedDenied = &core.LispicoError{Code: "RetainedDenied", Message: "retained meter denied"}

// denyMeter denies every retained charge after invoking the hook once.
type denyMeter struct {
	onCharge func()
}

func (m denyMeter) LeaseEval(reductions, allocBytes int64) (int64, int64, error) {
	return reductions, allocBytes, nil
}

func (m denyMeter) ReturnEval(reductions, allocBytes int64) {}

func (m denyMeter) ChargeRetained(bytes, slots int64) error {
	m.onCharge()
	return errRetainedDenied
}

func (m denyMeter) ReleaseRetained(bytes, slots int64) {}

// oneByteContext builds a context whose allocation ceiling is one byte: any
// real detachment charge fails, so a green traversal proves the detacher
// allocated and charged no scratch storage.
func oneByteContext(t *testing.T) context.Context {
	t.Helper()
	return core.WithEvalResourceLimits(context.Background(), 1<<30, 1)
}

// TestCacheOwnership_ScratchCharged pins the allocation seam: an unchanged
// collection and an all-scalar constant tree detach without charging a byte,
// while a borrowed leaf makes the detacher charge its scratch slice before
// building it, so a zero-byte budget refuses before the buffer exists.
func TestCacheOwnership_ScratchCharged(t *testing.T) {
	owned := core.String{V: "owned"}
	ownedSym := core.Symbol{V: "owned-sym"}
	ownedTable := map[stringIdentity]core.Value{
		{strAddr(owned.V), len(owned.V)}:       owned,
		{strAddr(ownedSym.V), len(ownedSym.V)}: ownedSym,
	}

	t.Run("unchanged-collection-charges-nothing", func(t *testing.T) {
		l := core.NewList([]core.Value{owned, core.Int{V: 1}})
		v := core.NewVector([]core.Value{core.Int{V: 2}, owned})
		m := core.NewHashMap()
		require.NoError(t, m.Set(ownedSym, owned))
		chunk := &vm.Chunk{Constants: []core.Value{l, v, m}}
		out, err := (&bytecodeEvaluator{}).detachCachedChunk(oneByteContext(t), chunk, ownedTable)
		require.NoError(t, err, "unchanged containers must not allocate or charge scratch copies")
		require.Equal(t, l, out.Constants[0], "unchanged list must be reused as-is")
		require.Equal(t, v, out.Constants[1], "unchanged vector must be reused as-is")
		require.Equal(t, m, out.Constants[2], "unchanged map must be reused as-is")
	})

	t.Run("all-scalar-constant-charges-nothing", func(t *testing.T) {
		chunk := &vm.Chunk{Constants: []core.Value{core.Int{V: 7}, owned, ownedSym}}
		out, err := (&bytecodeEvaluator{}).detachCachedChunk(oneByteContext(t), chunk, ownedTable)
		require.NoError(t, err)
		require.Equal(t, core.Int{V: 7}, out.Constants[0])
		require.Equal(t, strAddr(owned.V), strAddr(out.Constants[1].(core.String).V))
	})

	t.Run("borrowed-leaf-refused-before-buffer-built", func(t *testing.T) {
		borrowed := core.String{V: "borrowed"}
		l := core.NewList([]core.Value{core.Int{V: 1}, borrowed})
		chunk := &vm.Chunk{Constants: []core.Value{l}}
		_, err := (&bytecodeEvaluator{}).detachCachedChunk(oneByteContext(t), chunk, nil)
		require.Error(t, err, "a one-byte allocation budget must refuse before the scratch slice is built")
		// Input still intact: nothing was written through.
		require.Equal(t, strAddr(borrowed.V), strAddr(l.ToSlice()[1].(core.String).V))
	})

	t.Run("borrowed-leaf-admitted-under-real-budget", func(t *testing.T) {
		borrowed := core.String{V: "borrowed"}
		l := core.NewList([]core.Value{core.Int{V: 1}, borrowed})
		chunk := &vm.Chunk{Constants: []core.Value{l}}
		ctx := core.WithEvalResourceLimits(context.Background(), 1<<30, 1<<20)
		out, err := (&bytecodeEvaluator{}).detachCachedChunk(ctx, chunk, nil)
		require.NoError(t, err)
		outList := out.Constants[0].(core.List)
		require.Equal(t, borrowed.V, outList.ToSlice()[1].(core.String).V)
		require.NotEqual(t, strAddr(borrowed.V), strAddr(outList.ToSlice()[1].(core.String).V))
	})
}

// TestCacheOwnership_StringChargedBeforeClone pins the charge-before-clone
// order for string payloads: a budget one byte below the payload's shallow
// string allocation refuses the charge, and the process allocation accounting
// proves the clone was never attempted — total allocated bytes grow by less
// than one clone's worth. A full budget admits the payload and the detached
// constant holds fresh storage.
func TestCacheOwnership_StringChargedBeforeClone(t *testing.T) {
	bigLen := 1 << 20
	required := core.StringShallowBytes(bigLen)

	t.Run("refused-below-clone-allocation", func(t *testing.T) {
		payload := strings.Repeat("x", bigLen)
		chunk := &vm.Chunk{Constants: []core.Value{core.String{V: payload}}}
		ctx := core.WithEvalResourceLimits(context.Background(), 1<<30, int(required)-1)
		var before goruntime.MemStats
		goruntime.ReadMemStats(&before)
		_, err := (&bytecodeEvaluator{}).detachCachedChunk(ctx, chunk, nil)
		require.Error(t, err, "a budget one byte short of the clone allocation must refuse the charge")
		var after goruntime.MemStats
		goruntime.ReadMemStats(&after)
		delta := after.TotalAlloc - before.TotalAlloc
		require.Less(t, delta, uint64(required),
			"allocation accounting must show the clone was never attempted")
		require.Equal(t, strAddr(payload), strAddr(chunk.Constants[0].(core.String).V),
			"the refused payload must keep its original storage")
	})

	t.Run("admitted-at-full-allocation", func(t *testing.T) {
		payload := strings.Repeat("x", bigLen)
		chunk := &vm.Chunk{Constants: []core.Value{core.String{V: payload}}}
		ctx := core.WithEvalResourceLimits(context.Background(), 1<<30, int(required)+1024)
		out, err := (&bytecodeEvaluator{}).detachCachedChunk(ctx, chunk, nil)
		require.NoError(t, err)
		got := out.Constants[0].(core.String).V
		require.Equal(t, payload, got)
		require.NotEqual(t, strAddr(payload), strAddr(got),
			"an admitted payload must be cloned to fresh storage")
	})
}

// TestCacheOwnership_SharedHashMapDiamond pins the identity memo: a DAG of
// HashMaps where every level references the same child twice detaches each
// distinct map exactly once — the visit counter stays linear in the number
// of distinct maps, not exponential in depth — and both references at every
// level point at the same detached replacement.
func TestCacheOwnership_SharedHashMapDiamond(t *testing.T) {
	const depth = 12
	borrowed := core.String{V: "leaf-payload"}
	child := core.NewHashMap()
	require.NoError(t, child.Set(core.Keyword{V: "k"}, borrowed))
	cur := core.Value(child)
	for range depth {
		m := core.NewHashMap()
		require.NoError(t, m.Set(core.Keyword{V: "l"}, cur))
		require.NoError(t, m.Set(core.Keyword{V: "r"}, cur))
		cur = m
	}
	root := cur.(*core.HashMap)

	chunk := &vm.Chunk{Constants: []core.Value{root}}
	hashMapDetachVisits.Store(0)
	out, err := (&bytecodeEvaluator{}).detachCachedChunk(context.Background(), chunk, nil)
	require.NoError(t, err)

	// Each distinct map visited once: depth levels plus the shared child.
	require.Equal(t, int64(depth+1), hashMapDetachVisits.Load(),
		"shared HashMaps must be memoized, not revisited exponentially")

	// The shared child is detached once and both references agree at
	// every level; the walk ends at the single detached child.
	outRoot := out.Constants[0].(*core.HashMap)
	node := outRoot
	for range depth {
		left, _ := node.Get(core.Keyword{V: "l"})
		right, _ := node.Get(core.Keyword{V: "r"})
		lm := left.(*core.HashMap)
		require.Same(t, lm, right.(*core.HashMap), "both references must share one detached replacement")
		node = lm
	}
	require.NotSame(t, child, node, "the borrowed-leaf child must have been rebuilt")

	// The borrowed leaf is cloned inside the detached child.
	leaf, _ := node.Get(core.Keyword{V: "k"})
	require.NotEqual(t, strAddr(borrowed.V), strAddr(leaf.(core.String).V), "leaf payload published uncloned")
	require.Equal(t, borrowed.V, leaf.(core.String).V)

	// Retained accounting is unchanged: detachment never resizes the tree.
	require.Equal(t, chunk.DeepBytes, out.DeepBytes)
	require.Equal(t, chunk.NodeCount, out.NodeCount)
}

// sharedIntChain builds an n-element List on the shared representation — the
// reader only produces flat lists, so one is consed together tail-first.
func sharedIntChain(n int) core.List {
	l := core.List{}
	for i := n; i >= 1; i-- {
		l, _ = l.Cons(core.Int{V: int64(i)})
	}
	return l
}

// TestCacheOwnership_SharedTailLinear walks a 200k-element shared-tail list
// through ownership collection and detachment. Every element is an Int, so
// the detach verdict is "unchanged" end to end: a one-byte allocation budget
// must admit the reuse with no charge at all. An At(i) loop costs O(i) per
// access on the shared form and cannot finish this walk inside the test
// timeout; the At(0)/Rest walk is linear by construction.
func TestCacheOwnership_SharedTailLinear(t *testing.T) {
	l := sharedIntChain(200_000)

	owned, err := ownedStringIdentities(t.Context(), l, "irrelevant")
	require.NoError(t, err)
	require.Empty(t, owned)

	be := &bytecodeEvaluator{}
	chunk := &vm.Chunk{Constants: []core.Value{l}}
	out, err := be.detachCachedChunk(oneByteContext(t), chunk, owned)
	require.NoError(t, err, "an unchanged shared-tail list must detach without allocating or charging")
	outList := out.Constants[0].(core.List)
	require.Equal(t, l.Len(), outList.Len(), "unchanged shared-tail list must be reused as-is")
	require.Equal(t, core.Int{V: 1}, outList.At(0))
}

// TestCacheOwnership_SharedTailRebuiltDetached drives the rebuild path on a
// shared-tail list carrying one borrowed payload: the list is rebuilt with
// the payload cloned and the rest of the chain carried over.
func TestCacheOwnership_SharedTailRebuiltDetached(t *testing.T) {
	borrowed := "borrowed-tail-payload"
	l := sharedIntChain(5_000)
	l, _ = l.Cons(core.String{V: borrowed})

	be := &bytecodeEvaluator{}
	chunk := &vm.Chunk{Constants: []core.Value{l}}
	out, err := be.detachCachedChunk(context.Background(), chunk, nil)
	require.NoError(t, err)
	outList := out.Constants[0].(core.List)
	require.Equal(t, l.Len(), outList.Len())
	require.Equal(t, borrowed, outList.At(0).(core.String).V)
	require.NotEqual(t, strAddr(borrowed), strAddr(outList.At(0).(core.String).V),
		"borrowed payload at the head of a shared-tail list must be cloned")
	require.Equal(t, core.Int{V: 1}, outList.At(1))
}

// TestCacheOwnership_OwnershipWalkCanceled pins that cancellation is polled
// during the initial ownership walk and detachment, so a canceled evaluation
// returns immediately instead of walking or publishing.
func TestCacheOwnership_OwnershipWalkCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ownedStringIdentities(ctx, sharedIntChain(1000), "irrelevant")
	require.ErrorIs(t, err, context.Canceled)

	be := &bytecodeEvaluator{}
	chunk := &vm.Chunk{Constants: []core.Value{sharedIntChain(200)}}
	_, derr := be.detachCachedChunk(ctx, chunk, nil)
	require.ErrorIs(t, derr, context.Canceled)
}

// intHashMap builds an n-entry map of Int keys to Int values: every leaf is
// scalar, so the detachment verdict is the unchanged fast path and the only
// work a canceled evaluation can observe is the fast-path traversal itself.
func intHashMap(t *testing.T, n int) *core.HashMap {
	t.Helper()
	m := core.NewHashMap()
	for i := range n {
		require.NoError(t, m.Set(core.Int{V: int64(i)}, core.Int{V: int64(i)}))
	}
	return m
}

// TestCacheOwnership_UnchangedHashMapCanceled pins that the unchanged-HashMap
// fast path polls cancellation during its traversal: a pre-canceled context
// and a map large enough to pass the poll cadence return an error and no
// published chunk, while the same map detaches unchanged under a live
// context.
func TestCacheOwnership_UnchangedHashMapCanceled(t *testing.T) {
	m := intHashMap(t, 500)

	live := &bytecodeEvaluator{}
	unchanged, err := live.detachCachedChunk(context.Background(), &vm.Chunk{Constants: []core.Value{m}}, nil)
	require.NoError(t, err)
	require.Empty(t, publishedChunks(live), "detachment alone must not publish")
	require.Same(t, m, unchanged.Constants[0].(*core.HashMap), "an all-scalar map detaches unchanged")

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	bec := &bytecodeEvaluator{}
	out, derr := bec.detachCachedChunk(canceled, &vm.Chunk{Constants: []core.Value{m}}, nil)
	require.ErrorIs(t, derr, context.Canceled)
	require.Nil(t, out)
	require.Empty(t, publishedChunks(bec), "a canceled fast path must not publish")
}

// TestCacheOwnership_MapWithClosureDetached pins that an unchanged HashMap
// verdict does not skip closure payloads: a map whose value is a Lambda with
// a borrowed Name is rebuilt, and the name payload is cloned.
func TestCacheOwnership_MapWithClosureDetached(t *testing.T) {
	borrowed := "borrowed-lambda-name"
	lambda := core.Lambda{Name: borrowed, Params: []core.Symbol{{V: "x"}}, Body: []core.Value{core.Int{V: 1}}}
	m := core.NewHashMap()
	require.NoError(t, m.Set(core.Int{V: 1}, lambda))

	be := &bytecodeEvaluator{}
	chunk := &vm.Chunk{Constants: []core.Value{m}}
	out, err := be.detachCachedChunk(context.Background(), chunk, nil)
	require.NoError(t, err)
	outMap := out.Constants[0].(*core.HashMap)
	require.NotSame(t, m, outMap, "a map holding a borrowed closure name must be rebuilt")
	pairs := outMap.Pairs()
	require.Len(t, pairs, 1)
	outLambda := pairs[0][1].(core.Lambda)
	require.Equal(t, borrowed, outLambda.Name)
	require.NotEqual(t, strAddr(borrowed), strAddr(outLambda.Name),
		"borrowed closure name must be cloned")
}

// TestCacheOwnership_SubChunkSliceNotMutated pins that subchunk detachment
// never writes through the borrowed candidate's SubChunks slice: the
// destination slice is separately allocated before the first replacement, so
// both a full success and a failure after one detached sibling leave the
// candidate and every nested chunk unchanged.
func TestCacheOwnership_SubChunkSliceNotMutated(t *testing.T) {
	newChunk := func(name string) *vm.Chunk {
		return &vm.Chunk{Name: name}
	}

	t.Run("success-leaves-borrowed-candidate-unchanged", func(t *testing.T) {
		borrowed := "sub-borrowed"
		sub := newChunk(borrowed)
		chunk := &vm.Chunk{SubChunks: []*vm.Chunk{sub}}
		ctx := core.WithEvalResourceLimits(context.Background(), 1<<30, 1<<20)
		out, err := (&bytecodeEvaluator{}).detachCachedChunk(ctx, chunk, nil)
		require.NoError(t, err)
		require.Same(t, sub, chunk.SubChunks[0],
			"the borrowed candidate's slice must keep the original subchunk pointer")
		require.Equal(t, strAddr(borrowed), strAddr(sub.Name),
			"the borrowed subchunk must keep its original name storage")
		require.Len(t, out.SubChunks, 1)
		require.NotSame(t, sub, out.SubChunks[0],
			"the published subchunk must be a fresh chunk")
		require.Equal(t, borrowed, out.SubChunks[0].Name)
		require.NotEqual(t, strAddr(borrowed), strAddr(out.SubChunks[0].Name),
			"the published subchunk name must be cloned")
	})

	t.Run("failure-after-detached-sibling-leaves-candidate-intact", func(t *testing.T) {
		first := "first-borrowed"
		second := "second-borrowed-longer"
		sub1 := newChunk(first)
		sub2 := newChunk(second)
		chunk := &vm.Chunk{SubChunks: []*vm.Chunk{sub1, sub2}}
		// Detachment charges, in order: sub1's name clone, the scratch
		// subchunk slice, then sub2's name clone. A budget one byte short
		// of the last charge fails only after sub1 has detached.
		budget := core.StringShallowBytes(len(first)) +
			core.ValueSlotsBytes(2) +
			core.StringShallowBytes(len(second)) - 1
		ctx := core.WithEvalResourceLimits(context.Background(), 1<<30, int(budget))
		_, err := (&bytecodeEvaluator{}).detachCachedChunk(ctx, chunk, nil)
		require.Error(t, err,
			"the exhausted budget must refuse while detaching the second subchunk")
		require.Same(t, sub1, chunk.SubChunks[0],
			"a failed fallback must not leave the candidate partially detached")
		require.Same(t, sub2, chunk.SubChunks[1],
			"a failed fallback must not touch the undetached sibling slot")
		require.Equal(t, strAddr(first), strAddr(sub1.Name),
			"the detached sibling's original chunk must keep its name storage")
		require.Equal(t, strAddr(second), strAddr(sub2.Name))
	})
}
