package runtime

import (
	"context"
	"reflect"
	"strings"
	"sync/atomic"

	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/core/vm"
)

// stringIdentity keys a decoded string payload by backing-storage address and
// length. Contents alone cannot identify storage: an escaped string the
// reader allocated and an unescaped string aliasing the source may decode to
// equal text.
type stringIdentity struct {
	addr uintptr
	n    int
}

// ownedStringIdentities walks the original reader form — the form as read,
// before any macro expansion — and records the storage identity of every
// nonempty String, Symbol, and Keyword payload that does not alias the
// current input's backing array. The reader hands unescaped literals out as
// substrings of the input; escaped literals come from its own strings.Builder
// and already own their bytes. Identities recorded here survive macro
// expansion because expansion copies Values, never string storage, so the
// detacher can reuse those payloads as-is. Holding the Values in the map
// keeps their backing storage live for the detacher's lifetime.
func ownedStringIdentities(ctx context.Context, form core.Value, input string) (map[stringIdentity]core.Value, error) {
	if err := core.PollEvalState(ctx); err != nil {
		return nil, err
	}
	start := reflect.ValueOf(input).Pointer()
	end := start + uintptr(len(input))
	owned := make(map[stringIdentity]core.Value)
	w := ownershipWalk{ctx: ctx, start: start, end: end, owned: owned}
	if err := w.collect(form, core.DefaultMaxStructuralDepth); err != nil {
		return nil, err
	}
	return owned, nil
}

// ownershipWalk gathers reader-owned payload identities, polling evaluation
// cancellation every ownershipPollInterval visited nodes so a huge input
// form cannot burn CPU past a canceled evaluation.
type ownershipWalk struct {
	ctx        context.Context
	start, end uintptr
	owned      map[stringIdentity]core.Value
	steps      int
}

const ownershipPollInterval = 64

func (w *ownershipWalk) collect(v core.Value, depth int) error {
	if depth < 0 {
		return nil
	}
	if err := w.poll(); err != nil {
		return err
	}
	switch val := v.(type) {
	case core.String:
		recordOwnedPayload(val.V, val, w.start, w.end, w.owned)
	case core.Symbol:
		recordOwnedPayload(val.V, val, w.start, w.end, w.owned)
	case core.Keyword:
		recordOwnedPayload(val.V, val, w.start, w.end, w.owned)
	case core.List:
		for rem := val; rem.Len() > 0; rem = rem.Rest() {
			if err := w.collect(rem.At(0), depth-1); err != nil {
				return err
			}
		}
	case core.Vector:
		for i := range val.Len() {
			if err := w.collect(val.At(i), depth-1); err != nil {
				return err
			}
		}
	case *core.HashMap:
		for _, pair := range val.Pairs() {
			if err := w.collect(pair[0], depth-1); err != nil {
				return err
			}
			if err := w.collect(pair[1], depth-1); err != nil {
				return err
			}
		}
	case core.Macro:
		for _, p := range val.Params {
			if err := w.collect(p, depth-1); err != nil {
				return err
			}
		}
		if err := w.collect(val.Variadic, depth-1); err != nil {
			return err
		}
		for _, b := range val.Body {
			if err := w.collect(b, depth-1); err != nil {
				return err
			}
		}
	case core.Lambda:
		for _, p := range val.Params {
			if err := w.collect(p, depth-1); err != nil {
				return err
			}
		}
		if err := w.collect(val.Variadic, depth-1); err != nil {
			return err
		}
		for _, b := range val.Body {
			if err := w.collect(b, depth-1); err != nil {
				return err
			}
		}
	}
	return nil
}

func (w *ownershipWalk) poll() error {
	w.steps++
	if w.steps%ownershipPollInterval != 0 {
		return nil
	}
	return core.PollEvalState(w.ctx)
}

func recordOwnedPayload(s string, owner core.Value, start, end uintptr, owned map[stringIdentity]core.Value) {
	if s == "" {
		return
	}
	addr := reflect.ValueOf(s).Pointer()
	if addr >= start && addr < end {
		return
	}
	owned[stringIdentity{addr: addr, n: len(s)}] = owner
}

// chunkDetacher rebuilds a compiled chunk tree so no retained string payload
// aliases the Eval input, and no shared collection storage is mutated. It
// clones every retained nonempty payload with strings.Clone except payloads
// whose identity the miss-path ownership walk already proved owned, and empty
// strings, which retain no source bytes. Strings introduced by macro
// expansion are not in the ownership table and are conservatively cloned.
type chunkDetacher struct {
	ctx           context.Context
	owned         map[stringIdentity]core.Value
	maxDepth      int
	steps         int
	hashMemo      map[*core.HashMap]*core.HashMap
	unchangedMemo map[*core.HashMap]bool
}

// hashMapDetachVisits counts *core.HashMap nodes the detacher visited. It
// exists so tests can prove a shared DAG visits each distinct map once; it
// never gates behavior.
var hashMapDetachVisits atomic.Int64

// detachDepthLimit bounds detachment recursion independently of the user
// eval structural-depth ceiling: deep uncalled bodies must compile and cache
// even when MaxStructuralDepth is tiny, and chunk nesting is bounded by the
// compiler (MaxCompileDepth), so a fixed multiple of it can never be reached
// by a legitimate chunk tree. Cancellation still polls through the context.
const detachDepthLimit = 4 * core.MaxCompileDepth

func (be *bytecodeEvaluator) detachCachedChunk(ctx context.Context, chunk *vm.Chunk, owned map[stringIdentity]core.Value) (*vm.Chunk, error) {
	d := &chunkDetacher{ctx: ctx, owned: owned, maxDepth: detachDepthLimit,
		hashMemo: make(map[*core.HashMap]*core.HashMap), unchangedMemo: make(map[*core.HashMap]bool)}
	return d.chunk(chunk, 0)
}

func (d *chunkDetacher) chunk(c *vm.Chunk, depth int) (*vm.Chunk, error) {
	if err := d.poll(); err != nil {
		return nil, err
	}
	if depth > d.maxDepth {
		return nil, core.NewResourceLimitError("cache detachment structural depth limit exceeded")
	}
	// A field-literal copy leaves the unexported site cache and constant
	// fingerprint index zero, so both rebuild lazily from the detached
	// fields — the same shape CopyTreeFreshSites produces. Code, Captured,
	// Caps, Fused, and ConstCharges carry no source text and are shared;
	// the borrowed candidate is discarded once this copy is published.
	out := &vm.Chunk{
		Name:         c.Name,
		Arity:        c.Arity,
		Variadic:     c.Variadic,
		Locals:       c.Locals,
		MaxStack:     c.MaxStack,
		Captured:     c.Captured,
		Caps:         c.Caps,
		Code:         c.Code,
		Fused:        c.Fused,
		DeepBytes:    c.DeepBytes,
		NodeCount:    c.NodeCount,
		ConstCharges: c.ConstCharges,
	}
	var err error
	if out.Name, _, err = d.string(c.Name); err != nil {
		return nil, err
	}
	if c.LocalNames != nil {
		names, _, err := d.strings(c.LocalNames)
		if err != nil {
			return nil, err
		}
		out.LocalNames = names
	}
	if c.Constants != nil {
		constants, _, err := d.values(c.Constants, depth)
		if err != nil {
			return nil, err
		}
		out.Constants = constants
	}
	if c.SubChunks != nil {
		var subs []*vm.Chunk
		for i, sub := range c.SubChunks {
			var ds *vm.Chunk
			if ds, err = d.chunk(sub, depth+1); err != nil {
				return nil, err
			}
			if ds == sub {
				continue
			}
			if subs == nil {
				if err := d.charge(core.ValueSlotsBytes(len(c.SubChunks))); err != nil {
					return nil, err
				}
				subs = make([]*vm.Chunk, len(c.SubChunks))
				for j := range i {
					subs[j] = c.SubChunks[j]
				}
			}
			subs[i] = ds
		}
		if subs == nil {
			subs = c.SubChunks
		}
		out.SubChunks = subs
	}
	return out, nil
}

// strings detaches a slice of string payloads lazily: the input slice is
// returned untouched when no payload changed, otherwise a charged scratch
// slice is allocated before any copy happens.
func (d *chunkDetacher) strings(names []string) ([]string, bool, error) {
	for i, name := range names {
		nd, ch, err := d.string(name)
		if err != nil {
			return nil, false, err
		}
		if !ch {
			continue
		}
		if err := d.charge(core.ValueSlotsBytes(len(names))); err != nil {
			return nil, false, err
		}
		out := make([]string, len(names))
		copy(out, names)
		out[i] = nd
		for j := i + 1; j < len(names); j++ {
			if out[j], _, err = d.string(names[j]); err != nil {
				return nil, false, err
			}
		}
		return out, true, nil
	}
	return names, false, nil
}

// value returns the detached replacement for v and reports whether the
// replacement holds any storage not held by the input — cloned payload,
// rebuilt container, or both. A container whose every leaf was already owned
// is returned unchanged; a container with any detached leaf is rebuilt
// through the existing constructors so shared backing storage is never
// mutated.
func (d *chunkDetacher) value(v core.Value, depth int) (out core.Value, changed bool, err error) {
	if err = d.poll(); err != nil {
		return v, false, err
	}
	if depth > d.maxDepth {
		return v, false, core.NewResourceLimitError("cache detachment structural depth limit exceeded")
	}
	switch val := v.(type) {
	case core.String:
		var s string
		if s, changed, err = d.string(val.V); err != nil {
			return v, false, err
		}
		return core.String{V: s}, changed, nil
	case core.Symbol:
		var s string
		if s, changed, err = d.string(val.V); err != nil {
			return v, false, err
		}
		return core.Symbol{V: s}, changed, nil
	case core.Keyword:
		var s string
		if s, changed, err = d.string(val.V); err != nil {
			return v, false, err
		}
		return core.Keyword{V: s}, changed, nil
	case core.List:
		n := val.Len()
		var items []core.Value
		i := 0
		for rem := val; rem.Len() > 0; rem = rem.Rest() {
			nv, ch, err := d.value(rem.At(0), depth+1)
			if err != nil {
				return v, false, err
			}
			if ch && items == nil {
				if err := d.charge(core.ValueSlotsBytes(n)); err != nil {
					return v, false, err
				}
				items = make([]core.Value, n)
				for j, pre := 0, val; j < i; j, pre = j+1, pre.Rest() {
					items[j] = pre.At(0)
				}
			}
			if items != nil {
				items[i] = nv
			}
			i++
		}
		if items == nil {
			return val, false, nil
		}
		if err := d.charge(core.ListShallowBytes(n)); err != nil {
			return v, false, err
		}
		return core.NewList(items), true, nil
	case core.Vector:
		n := val.Len()
		var items []core.Value
		for i := range n {
			nv, ch, err := d.value(val.At(i), depth+1)
			if err != nil {
				return v, false, err
			}
			if ch && items == nil {
				if err := d.charge(core.ValueSlotsBytes(n)); err != nil {
					return v, false, err
				}
				items = make([]core.Value, n)
				for j := range i {
					items[j] = val.At(j)
				}
			}
			if items != nil {
				items[i] = nv
			}
		}
		if items == nil {
			return val, false, nil
		}
		if err := d.charge(core.VectorShallowBytes(n)); err != nil {
			return v, false, err
		}
		return core.NewVector(items), true, nil
	case *core.HashMap:
		if rep, ok := d.hashMemo[val]; ok {
			if rep == nil {
				return v, false, core.NewResourceLimitError("cache detachment cycle in shared HashMap")
			}
			return rep, rep != val, nil
		}
		u, err := d.unchangedHashMap(val, depth)
		if err != nil {
			return v, false, err
		}
		if u {
			d.hashMemo[val] = val
			return val, false, nil
		}
		d.hashMemo[val] = nil
		hashMapDetachVisits.Add(1)
		n := val.Len()
		if err = d.charge(core.ValueSlotsBytes(2 * n)); err != nil {
			return v, false, err
		}
		pairs := make([][2]core.Value, 0, n)
		val.Each(func(k, v core.Value) {
			pairs = append(pairs, [2]core.Value{k, v})
		})
		detachedAny := false
		for i, pair := range pairs {
			nk, chK, err := d.value(pair[0], depth+1)
			if err != nil {
				return v, false, err
			}
			nv, chV, err := d.value(pair[1], depth+1)
			if err != nil {
				return v, false, err
			}
			pairs[i] = [2]core.Value{nk, nv}
			detachedAny = detachedAny || chK || chV
		}
		if !detachedAny {
			d.hashMemo[val] = val
			return val, false, nil
		}
		if err = d.charge(core.HashMapShallowBytes(len(pairs))); err != nil {
			return v, false, err
		}
		m := core.NewHashMap()
		for _, pair := range pairs {
			if err = m.Set(pair[0], pair[1]); err != nil {
				return v, false, err
			}
		}
		d.hashMemo[val] = m
		return m, true, nil
	case core.Macro:
		var nm core.Macro
		if nm, changed, err = d.macro(val, depth); err != nil {
			return v, false, err
		}
		if !changed {
			return val, false, nil
		}
		if err = d.charge(core.ClosureShallowBytes(len(nm.Params) + len(nm.Body))); err != nil {
			return v, false, err
		}
		return nm, true, nil
	case core.Lambda:
		var nl core.Lambda
		if nl, changed, err = d.lambda(val, depth); err != nil {
			return v, false, err
		}
		if !changed {
			return val, false, nil
		}
		if err = d.charge(core.ClosureShallowBytes(len(nl.Params) + len(nl.Body))); err != nil {
			return v, false, err
		}
		return nl, true, nil
	default:
		return v, false, nil
	}
}

func (d *chunkDetacher) macro(m core.Macro, depth int) (core.Macro, bool, error) {
	changed := false
	params, ch, err := d.symbols(m.Params, depth)
	if err != nil {
		return m, false, err
	}
	changed = changed || ch
	variadic := m.Variadic
	if nv, ch, err := d.symbol(m.Variadic, depth); err != nil {
		return m, false, err
	} else if ch {
		variadic, changed = nv, true
	}
	body, ch, err := d.values(m.Body, depth)
	if err != nil {
		return m, false, err
	}
	changed = changed || ch
	name, ch, err := d.string(m.Name)
	if err != nil {
		return m, false, err
	}
	changed = changed || ch
	return core.Macro{Name: name, Params: params, Variadic: variadic, Body: body, Env: m.Env}, changed, nil
}

func (d *chunkDetacher) lambda(l core.Lambda, depth int) (core.Lambda, bool, error) {
	changed := false
	params, ch, err := d.symbols(l.Params, depth)
	if err != nil {
		return l, false, err
	}
	changed = changed || ch
	variadic := l.Variadic
	if nv, ch, err := d.symbol(l.Variadic, depth); err != nil {
		return l, false, err
	} else if ch {
		variadic, changed = nv, true
	}
	body, ch, err := d.values(l.Body, depth)
	if err != nil {
		return l, false, err
	}
	changed = changed || ch
	name, ch, err := d.string(l.Name)
	if err != nil {
		return l, false, err
	}
	changed = changed || ch
	return core.Lambda{Name: name, Params: params, Variadic: variadic, Body: body, Env: l.Env}, changed, nil
}

func (d *chunkDetacher) symbols(params []core.Symbol, depth int) ([]core.Symbol, bool, error) {
	for i, p := range params {
		nv, ch, err := d.symbol(p, depth)
		if err != nil {
			return nil, false, err
		}
		if !ch {
			continue
		}
		if err := d.charge(core.ValueSlotsBytes(len(params))); err != nil {
			return nil, false, err
		}
		out := make([]core.Symbol, len(params))
		copy(out, params)
		out[i] = nv
		for j := i + 1; j < len(out); j++ {
			if out[j], _, err = d.symbol(params[j], depth); err != nil {
				return nil, false, err
			}
		}
		return out, true, nil
	}
	return params, false, nil
}

func (d *chunkDetacher) symbol(s core.Symbol, depth int) (core.Symbol, bool, error) {
	v, changed, err := d.value(s, depth)
	if err != nil {
		return s, false, err
	}
	return v.(core.Symbol), changed, nil
}

func (d *chunkDetacher) values(body []core.Value, depth int) ([]core.Value, bool, error) {
	for i, b := range body {
		nv, ch, err := d.value(b, depth+1)
		if err != nil {
			return nil, false, err
		}
		if !ch {
			continue
		}
		if err := d.charge(core.ValueSlotsBytes(len(body))); err != nil {
			return nil, false, err
		}
		out := make([]core.Value, len(body))
		copy(out, body)
		out[i] = nv
		for j := i + 1; j < len(out); j++ {
			if out[j], _, err = d.value(body[j], depth+1); err != nil {
				return nil, false, err
			}
		}
		return out, true, nil
	}
	return body, false, nil
}

// string clones s unless its storage identity was proven owned on this read
// or it is empty. The bool reports whether the returned string holds
// different storage than the input.
func (d *chunkDetacher) string(s string) (string, bool, error) {
	if s == "" {
		return s, false, nil
	}
	if _, ok := d.owned[stringIdentity{addr: reflect.ValueOf(s).Pointer(), n: len(s)}]; ok {
		return s, false, nil
	}
	if err := d.charge(core.StringShallowBytes(len(s))); err != nil {
		return s, false, err
	}
	return strings.Clone(s), true, nil
}

// unchangedHashMap reports whether every leaf payload in the map is already
// owned or empty, so detachment can reuse the map as-is without allocating
// or charging any scratch copy. Verdicts are memoized by map identity so a
// shared DAG is scanned once per distinct map. A traversal aborted by
// cancellation returns the error without memoizing a partial verdict.
func (d *chunkDetacher) unchangedHashMap(h *core.HashMap, depth int) (bool, error) {
	if u, ok := d.unchangedMemo[h]; ok {
		return u, nil
	}
	unchanged := true
	var perr error
	h.Each(func(k, v core.Value) {
		if !unchanged || perr != nil {
			return
		}
		kOK, err := d.ownedLeaf(k, depth)
		if err != nil {
			perr = err
			return
		}
		vOK, err := d.ownedLeaf(v, depth)
		if err != nil {
			perr = err
			return
		}
		unchanged = kOK && vOK
	})
	if perr != nil {
		return false, perr
	}
	d.unchangedMemo[h] = unchanged
	return unchanged, nil
}

// ownedLeaf reports whether v already holds detached-or-owned storage at the
// given depth budget. Shared HashMaps consult the unchanged memo, so a DAG
// costs one scan per distinct map, not one per reference. Each visited node
// polls cancellation on the detacher's cadence so an owned structure cannot
// burn input-sized work past a canceled evaluation.
func (d *chunkDetacher) ownedLeaf(v core.Value, depth int) (bool, error) {
	if err := d.poll(); err != nil {
		return false, err
	}
	if depth < 0 {
		return false, nil
	}
	switch val := v.(type) {
	case core.String:
		return val.V == "" || d.ownedPayload(val.V), nil
	case core.Symbol:
		return val.V == "" || d.ownedPayload(val.V), nil
	case core.Keyword:
		return val.V == "" || d.ownedPayload(val.V), nil
	case core.List:
		for rem := val; rem.Len() > 0; rem = rem.Rest() {
			ok, err := d.ownedLeaf(rem.At(0), depth-1)
			if err != nil || !ok {
				return false, err
			}
		}
		return true, nil
	case core.Vector:
		for i := range val.Len() {
			ok, err := d.ownedLeaf(val.At(i), depth-1)
			if err != nil || !ok {
				return false, err
			}
		}
		return true, nil
	case *core.HashMap:
		if rep, ok := d.hashMemo[val]; ok {
			return rep == val, nil
		}
		return d.unchangedHashMap(val, depth-1)
	case core.Lambda, core.Macro:
		// Name, Params, and Body carry their own payloads, so an
		// unchanged verdict must not skip them; a map holding a closure is
		// never reused as-is.
		return false, nil
	default:
		return true, nil
	}
}

func (d *chunkDetacher) ownedPayload(s string) bool {
	_, ok := d.owned[stringIdentity{addr: reflect.ValueOf(s).Pointer(), n: len(s)}]
	return ok
}

func (d *chunkDetacher) charge(n int64) error {
	if n <= 0 {
		return nil
	}
	return core.ChargeEvalAllocBytes(d.ctx, n)
}

func (d *chunkDetacher) poll() error {
	d.steps++
	if d.steps%64 != 0 {
		return nil
	}
	return core.PollEvalState(d.ctx)
}
