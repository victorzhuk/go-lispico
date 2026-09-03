package core

import (
	"context"
	"fmt"
	"math"
	"sync/atomic"
)

const (
	DefaultMaxReductions      int64 = 10_000_000
	DefaultMaxAllocationBytes int64 = 64 * 1024 * 1024

	MeterValueSlotBytes        int64 = 16
	MeterScalarBytes           int64 = 16
	MeterStringHeaderBytes     int64 = 16
	MeterCollectionHeaderBytes int64 = 24
	MeterHashMapHeaderBytes    int64 = 32
	MeterHashMapEntryBytes     int64 = 64
	MeterEnvMapEntryBytes      int64 = 64
	MeterEnvCellBytes          int64 = 32
	MeterClosureHeaderBytes    int64 = 64
	MeterClosureCaptureBytes   int64 = 8
	// MeterTrieChildBytes sizes one child slot in a persistent map node. A
	// child is a bare node pointer, not an interface value, so it is one
	// word rather than MeterValueSlotBytes' two.
	MeterTrieChildBytes   int64 = 8
	MeterInstructionBytes int64 = 4
	MeterReaderNodeBytes  int64 = 32
	// MeterFusedOpBytes sizes one chunk.Fused entry (FusedOp) for the compile-time
	// allocation ledger — fixed per ADR 0011, never unsafe.Sizeof.
	MeterFusedOpBytes int64 = 40
)

const (
	maxEvalReductionLease = 1024
	maxEvalAllocLease     = 64 << 10
)

type sessionMeter interface {
	LeaseEval(reductions, allocBytes int64) (grantedRed, grantedAlloc int64, err error)
	ReturnEval(reductions, allocBytes int64)
	ChargeRetained(bytes, slots int64) error
	ReleaseRetained(bytes, slots int64)
}

type evalMeterContextKey struct{}

func WithEvalMeter(ctx context.Context, m any) context.Context {
	if m == nil {
		return ctx
	}
	meter, ok := m.(sessionMeter)
	if !ok || meter == nil {
		return ctx
	}
	if st, ok := ctx.Value(evalStateKey{}).(*evalState); ok {
		st.attachMeter(meter)
	}
	return context.WithValue(ctx, evalMeterContextKey{}, meter)
}

func EvalMeterContextValue(ctx context.Context) any {
	return sessionMeterFromContext(ctx)
}

func HasEvalMeter(ctx context.Context) bool {
	return sessionMeterFromContext(ctx) != nil
}

func sessionMeterFromContext(ctx context.Context) sessionMeter {
	if ctx == nil {
		return nil
	}
	meter, _ := ctx.Value(evalMeterContextKey{}).(sessionMeter)
	return meter
}

type EvalMeter struct {
	st *evalState
}

type EvalMeterSnapshot struct {
	MaxReductions      int64
	MaxAllocationBytes int64
	Reductions         int64
	AllocationBytes    int64
}

func normalizeEvalLimit(v, def int64) int64 {
	if v <= 0 {
		return def
	}
	return v
}

func newEvalState() *evalState {
	return newEvalStateWithLimits(DefaultMaxReductions, DefaultMaxAllocationBytes)
}

func newEvalStateWithLimits(maxReductions, maxAllocBytes int64) *evalState {
	return &evalState{
		maxReductions: normalizeEvalLimit(maxReductions, DefaultMaxReductions),
		maxAllocBytes: normalizeEvalLimit(maxAllocBytes, DefaultMaxAllocationBytes),
	}
}

func (st *evalState) setResourceLimits(maxReductions, maxAllocBytes int64) {
	st.maxReductions = normalizeEvalLimit(maxReductions, DefaultMaxReductions)
	st.maxAllocBytes = normalizeEvalLimit(maxAllocBytes, DefaultMaxAllocationBytes)
}

func (st *evalState) attachMeter(m sessionMeter) {
	current := st.currentMeter()
	if m == nil {
		st.meter.Store(nil)
		return
	}
	if current == m {
		return
	}
	if st.evalDepth.Load() > 0 && current != nil {
		return
	}
	st.setMeter(m)
}

func (st *evalState) setMeter(m sessionMeter) {
	st.meter.Store(&m)
}

func (st *evalState) currentMeter() sessionMeter {
	if p := st.meter.Load(); p != nil {
		return *p
	}
	return nil
}

func WithEvalResourceLimits(ctx context.Context, maxReductions, maxAllocBytes int) context.Context {
	ctx = ensureEvalState(ctx)
	evalStateFrom(ctx).setResourceLimits(int64(maxReductions), int64(maxAllocBytes))
	return ctx
}

func EvalMeterFrom(ctx context.Context) EvalMeter {
	if st, ok := ctx.Value(evalStateKey{}).(*evalState); ok {
		return EvalMeter{st: st}
	}
	return EvalMeter{}
}

func (m EvalMeter) Valid() bool { return m.st != nil }

func (m EvalMeter) Snapshot() EvalMeterSnapshot {
	if m.st == nil {
		return EvalMeterSnapshot{}
	}
	return EvalMeterSnapshot{
		MaxReductions:      m.st.maxReductions,
		MaxAllocationBytes: m.st.maxAllocBytes,
		Reductions:         publishedTotal(&m.st.reductions),
		AllocationBytes:    publishedTotal(&m.st.allocBytes),
	}
}

func (m EvalMeter) ChargeReductions(n int64) error {
	if m.st == nil {
		return nil
	}
	return m.st.chargeReductions(n)
}

// EvalMeterIfMaterialized never forces materialization (see BeginGoFuncDispatch),
// so a live() pass here says only that the wrapper had not gone stale at
// entry — a rearm can still land between that check and the state.Load()
// below and hand back a LATER generation's already-materialized state. The
// observedGen re-check after the load catches exactly that: it does not
// detect tearing (a single pointer load cannot tear), only "this pointer no
// longer belongs to the generation live() just confirmed."
func EvalMeterIfMaterialized(ctx context.Context) EvalMeter {
	if w, ok := ctx.(*lazyEvalStateCtx); ok {
		if !w.live() {
			return EvalMeter{}
		}
		observedGen := w.gen.Load()
		st := w.state.Load()
		if st == nil || w.gen.Load() != observedGen {
			return EvalMeter{}
		}
		return EvalMeter{st: st}
	}
	if st, ok := ctx.Value(evalStateKey{}).(*evalState); ok {
		return EvalMeter{st: st}
	}
	return EvalMeter{}
}

func (m EvalMeter) ChargeAllocBytes(n int64) error {
	if m.st == nil {
		return nil
	}
	return m.st.chargeAllocBytes(n)
}

func ChargeEvalReductions(ctx context.Context, n int64) error {
	return evalStateFrom(ctx).chargeReductions(n)
}

func ChargeEvalAllocBytes(ctx context.Context, n int64) error {
	return evalStateFrom(ctx).chargeAllocBytes(n)
}

// ChargeGoFuncResultBytes charges n bytes and marks that the active GoFunc
// dispatch already accounted for its own return value, so the apply site's
// fallback shallow charge (ValueShallowBytes(result)) is skipped for it.
// n == 0 marks a wholly borrowed result — an existing argument, stored
// member, or caller-supplied default returned as-is — in which case the
// centralized apply sites skip the fallback shallow charge without adding
// any bytes. Non-zero n charges exactly that many bytes; mixed results
// that combine fresh and borrowed components must pass only the fresh
// delta. Call exactly once, immediately before returning the value n describes.
func ChargeGoFuncResultBytes(ctx context.Context, n int64) error {
	st := evalStateFrom(ctx)
	st.calleeCharged = true
	return st.chargeAllocBytes(n)
}

// BeginGoFuncDispatch saves and clears the active evalState's
// callee-charged marker, returning the previous value. Pair with
// EndGoFuncDispatch around a single GoFunc.Fn call so a callee's
// ChargeGoFuncResultBytes is visible only to that call's own apply-site
// fallback check, not to an outer frame's. Required under reentrancy: a
// GoFunc like map calls back into apply/vm.call on the same evalState once
// per element, and a naive ledger-moved check would mistake an inner
// element-lambda's charge for map's own result already being billed.
//
// Uses EvalMeterIfMaterialized, not evalStateFrom: forcing a lazily-wrapped
// context (the VM's reentrant ctx, built by AdoptReentrantEvalState and
// rearmed by RearmReentrantEvalState — AdoptEvalStateWithMeter shares the
// same lazy shape but no production call site reaches here through it
// anymore) to materialize its evalState here would allocate one for every
// GoFunc dispatch, including callees that never touch resource limits at
// all — exactly the laziness TestCall_GoFuncDispatchKeepsEvalStateLazy
// guards. A callee that does charge something materializes the state
// itself, and EndGoFuncDispatch picks that up.
func BeginGoFuncDispatch(ctx context.Context) bool {
	st := EvalMeterIfMaterialized(ctx).st
	if st == nil {
		return false
	}
	prev := st.calleeCharged
	st.calleeCharged = false
	return prev
}

// EndGoFuncDispatch reads the active evalState's callee-charged marker —
// true if the GoFunc.Fn call bracketed by the matching BeginGoFuncDispatch
// charged its own result via ChargeGoFuncResultBytes — and restores prev,
// the value that BeginGoFuncDispatch returned. See BeginGoFuncDispatch for
// why this doesn't force materialization either.
func EndGoFuncDispatch(ctx context.Context, prev bool) (charged bool) {
	st := EvalMeterIfMaterialized(ctx).st
	if st == nil {
		return false
	}
	charged = st.calleeCharged
	st.calleeCharged = prev
	return charged
}

func ChargeEvalReader(ctx context.Context, stats ReaderStats) error {
	return ChargeEvalAllocBytes(ctx, ReaderAllocationBytes(stats))
}

func FlushEvalState(ctx context.Context) error {
	return evalStateFrom(ctx).flushReductions()
}

func StartEval(ctx context.Context) (bool, error) {
	if st, ok := ctx.Value(evalStateKey{}).(*evalState); ok && st.evalDepth.Load() > 0 {
		return false, nil
	}
	activeMeter := sessionMeterFromContext(ctx)
	st := evalStateFrom(ctx)
	top := st.evalDepth.Add(1) == 1
	if top {
		if activeMeter == nil {
			st.meter.Store(nil)
		} else {
			st.setMeter(activeMeter)
		}
		if err := st.drawInitialLease(); err != nil {
			st.evalDepth.Add(-1)
			return false, err
		}
	}
	return top, nil
}

func FinishEval(ctx context.Context, top bool) error {
	if !top {
		return nil
	}
	return evalStateFrom(ctx).finishEval()
}

func BeginEval(ctx context.Context) (func() error, error) {
	top, err := StartEval(ctx)
	if err != nil {
		return nil, err
	}
	return func() error {
		return FinishEval(ctx, top)
	}, nil
}

func (st *evalState) finishEval() error {
	st.evalDepth.Add(-1)
	flushErr := st.flushReductions()
	retainedErr := st.settleRetained()
	st.returnEvalLease()
	if flushErr != nil {
		return flushErr
	}
	return retainedErr
}

type retainedCharge struct {
	meter sessionMeter
	bytes int64
	slots int64
}

type retainedRelease struct {
	meter sessionMeter
	bytes int64
	slots int64
}

// settleRetained charges pending retained cells in eval order by first-seen meter.
// If a later meter denies the charge, every earlier successful meter is released
// with the exact charged amounts; meters must treat ChargeRetained/ReleaseRetained
// as symmetric for those amounts.
func (st *evalState) settleRetained() error {
	if len(st.pendingCellAllocs) == 0 {
		return nil
	}
	defer func() {
		st.pendingCellAllocs = nil
		st.retainedBytes = 0
		st.retainedSlots = 0
	}()

	for _, pending := range st.pendingCellAllocs {
		if pending.env.RetainedMeter() == nil && pending.meter != nil {
			pending.env.SetRetainedMeter(pending.meter)
		}
	}
	charges := make(map[sessionMeter]*retainedCharge, len(st.pendingCellAllocs))
	var chargeOrder []*retainedCharge
	for _, pending := range st.pendingCellAllocs {
		if pending.meter == nil || (pending.bytes == 0 && pending.slots == 0) {
			continue
		}
		charge, ok := charges[pending.meter]
		if !ok {
			charge = &retainedCharge{meter: pending.meter}
			charges[pending.meter] = charge
			chargeOrder = append(chargeOrder, charge)
		}
		charge.bytes += pending.bytes
		charge.slots += pending.slots
	}
	var charged []*retainedCharge
	for _, charge := range chargeOrder {
		if err := charge.meter.ChargeRetained(charge.bytes, charge.slots); err != nil {
			for _, prev := range charged {
				prev.meter.ReleaseRetained(prev.bytes, prev.slots)
			}
			return NewResourceLimitError(fmt.Sprintf("retained meter: %v", err))
		}
		charged = append(charged, charge)
	}

	var releases []retainedRelease
	for _, pending := range st.pendingCellAllocs {
		pending.env.mu.Lock()
		if pending.cell.rebuilt {
			if pending.meter != nil && (pending.bytes > 0 || pending.slots > 0) {
				releases = append(releases, retainedRelease{meter: pending.meter, bytes: pending.bytes, slots: pending.slots})
			}
		} else {
			pending.cell.retainedMeter = pending.meter
			pending.cell.retainedBytes = pending.bytes
		}
		pending.env.mu.Unlock()
	}
	for _, release := range releases {
		release.meter.ReleaseRetained(release.bytes, release.slots)
	}
	return nil
}

func (st *evalState) drawInitialLease() error {
	if st.currentMeter() == nil {
		return nil
	}
	return st.leaseEval(maxEvalReductionLease, maxEvalAllocLease)
}

func (st *evalState) chargeReductions(n int64) error {
	if n <= 0 {
		return nil
	}
	if st.currentMeter() != nil {
		return st.consumeReductionLease(n)
	}
	max := st.maxReductions
	if max <= 0 {
		max = DefaultMaxReductions
	}
	if total, exact := addCharge(&st.reductions, max, n); exact {
		if total <= max {
			return nil
		}
	} else {
		saturateCounter(&st.reductions, max, n)
	}
	return NewResourceLimitError(fmt.Sprintf("reduction limit %d exceeded", max))
}

func (st *evalState) chargeAllocBytes(n int64) error {
	if n <= 0 {
		return nil
	}
	if st.currentMeter() != nil {
		return st.consumeAllocLease(n)
	}
	max := st.maxAllocBytes
	if max <= 0 {
		max = DefaultMaxAllocationBytes
	}
	if total, exact := addCharge(&st.allocBytes, max, n); exact {
		if total <= max {
			return nil
		}
	} else {
		saturateCounter(&st.allocBytes, max, n)
	}
	return NewResourceLimitError(fmt.Sprintf("allocation limit %d bytes exceeded", max))
}

// addCharge records a positive charge on a meter counter with one atomic add
// and returns the running total. exact reports that the total is the real one;
// when it is false the counter cannot answer for this charge and the caller must
// refuse it and close it out through saturateCounter.
//
// The counter must never wrap: a wrapped total reads as under budget, so a
// charge refused near the ceiling would leave the counter admitting every charge
// after it and the limit would hold for nothing. total >= n is the whole guard.
// n is positive, so a total below it means the counter did not hold a plain
// non-negative total when the add landed - the add overflowed, or an earlier one
// already had.
//
// Keeping a charge larger than the whole budget off the add is what makes that
// safe while other goroutines charge the same counter. Such a charge is refused
// on its own and is the only one big enough to wrap the counter from an
// arbitrary total, so saturateCounter records it without ever storing a wrapped
// value. Every add here is then at most max, so one that does overflow leaves
// the counter no higher than max-2^63: far below zero, never the small positive
// residue a concurrent charge could mistake for a fresh budget, and an add onto
// a negative counter fails the same total >= n guard rather than accumulating
// on it.
func addCharge(counter *atomic.Int64, max, n int64) (total int64, exact bool) {
	if n > max {
		return 0, false
	}
	total = counter.Add(n)
	return total, total >= n
}

// saturateCounter closes out a charge the plain add could not record, and is
// the only writer that may run with the counter in an unusable state. Each
// attempt derives the total from the value the counter still holds and commits
// it only if the counter has not moved since, so a concurrent charge is re-read
// rather than lost. An oversized charge is recorded at its full amount, pinned
// at math.MaxInt64 the moment it would pass it; anything else got here because
// its own add already wrapped, and only the pin is left to do.
func saturateCounter(counter *atomic.Int64, max, n int64) {
	oversized := n > max
	for {
		used := counter.Load()
		total := int64(math.MaxInt64)
		if oversized && used >= 0 && used+n >= used {
			total = used + n
		}
		if total == used || counter.CompareAndSwap(used, total) {
			return
		}
	}
}

// publishedTotal reads a meter counter as a running total an embedder can act
// on. addCharge commits its add before it can tell whether the counter still
// held a plain total, so a counter already at the ceiling carries the wrapped
// sum until saturateCounter pins it back, and a Snapshot taken in that window
// would hand that sum out as the total charged so far.
//
// A counter reaches a negative value only by passing math.MaxInt64, and every
// total after that is the ceiling too, so answering the ceiling for a wrapped
// value is both the truthful reading and a monotone one: what a reader sees
// climbs with the real total and then stays at the ceiling. This belongs here
// and not in addCharge because it costs a charge nothing - the charge path
// stays one atomic add, and only the far colder read pays a compare.
func publishedTotal(counter *atomic.Int64) int64 {
	total := counter.Load()
	if total < 0 {
		return math.MaxInt64
	}
	return total
}

func (st *evalState) consumeReductionLease(n int64) error {
	for n > 0 {
		if st.leasedReductions == 0 {
			req := min(n, maxEvalReductionLease)
			if err := st.leaseEval(req, 0); err != nil {
				return err
			}
		}
		if st.leasedReductions <= 0 {
			return NewResourceLimitError("evaluation reduction meter exhausted")
		}
		used := min(n, st.leasedReductions)
		st.leasedReductions -= used
		n -= used
	}
	return nil
}

func (st *evalState) consumeAllocLease(n int64) error {
	for n > 0 {
		if st.leasedAllocBytes == 0 {
			req := min(n, maxEvalAllocLease)
			if err := st.leaseEval(0, req); err != nil {
				return err
			}
		}
		if st.leasedAllocBytes <= 0 {
			return NewResourceLimitError("evaluation allocation meter exhausted")
		}
		used := min(n, st.leasedAllocBytes)
		st.leasedAllocBytes -= used
		n -= used
	}
	return nil
}

func (st *evalState) leaseEval(reductions, allocBytes int64) error {
	meter := st.currentMeter()
	if meter == nil {
		return nil
	}
	reductions = min(max(reductions, 0), maxEvalReductionLease)
	allocBytes = min(max(allocBytes, 0), maxEvalAllocLease)
	if reductions == 0 && allocBytes == 0 {
		return nil
	}
	grantedRed, grantedAlloc, err := meter.LeaseEval(reductions, allocBytes)
	grantedRed = max(grantedRed, 0)
	grantedAlloc = max(grantedAlloc, 0)
	if err != nil {
		if grantedRed > 0 || grantedAlloc > 0 {
			meter.ReturnEval(grantedRed, grantedAlloc)
		}
		return NewResourceLimitError(fmt.Sprintf("evaluation meter exhausted: %v", err))
	}
	if reductions > 0 && grantedRed <= 0 {
		if grantedRed > 0 || grantedAlloc > 0 {
			meter.ReturnEval(grantedRed, grantedAlloc)
		}
		return NewResourceLimitError("evaluation reduction meter exhausted")
	}
	if allocBytes > 0 && grantedAlloc <= 0 {
		if grantedRed > 0 || grantedAlloc > 0 {
			meter.ReturnEval(grantedRed, grantedAlloc)
		}
		return NewResourceLimitError("evaluation allocation meter exhausted")
	}
	st.leasedReductions += grantedRed
	st.leasedAllocBytes += grantedAlloc
	return nil
}

func (st *evalState) returnEvalLease() {
	meter := st.currentMeter()
	if meter == nil {
		return
	}
	red, alloc := st.leasedReductions, st.leasedAllocBytes
	st.leasedReductions, st.leasedAllocBytes = 0, 0
	meter.ReturnEval(red, alloc)
}

func (st *evalState) flushReductions() error {
	remaining := st.budget.Swap(checkInterval)
	if remaining <= 0 || remaining >= checkInterval {
		return nil
	}
	return st.chargeReductions(checkInterval - remaining)
}

func ValueSlotsBytes(n int) int64 {
	if n <= 0 {
		return 0
	}
	return int64(n) * MeterValueSlotBytes
}

func StringShallowBytes(n int) int64 {
	if n < 0 {
		n = 0
	}
	return MeterStringHeaderBytes + int64(n)
}

func ListShallowBytes(n int) int64 {
	return MeterCollectionHeaderBytes + ValueSlotsBytes(n)
}

func VectorShallowBytes(n int) int64 {
	return MeterCollectionHeaderBytes + ValueSlotsBytes(n)
}

func HashMapShallowBytes(n int) int64 {
	if n <= 0 {
		return MeterHashMapHeaderBytes
	}
	return MeterHashMapHeaderBytes + int64(n)*MeterHashMapEntryBytes
}

func ClosureShallowBytes(captures int) int64 {
	if captures <= 0 {
		return MeterClosureHeaderBytes
	}
	return MeterClosureHeaderBytes + int64(captures)*MeterClosureCaptureBytes
}

func ReaderAllocationBytes(stats ReaderStats) int64 {
	return stats.Nodes*MeterReaderNodeBytes + stats.Bytes
}

func ValueShallowBytes(v Value) int64 {
	switch val := v.(type) {
	case nil:
		return 0
	case Nil, Bool, Int, Float:
		return MeterScalarBytes
	case String:
		return StringShallowBytes(len(val.V))
	case Symbol:
		return StringShallowBytes(len(val.V))
	case Keyword:
		return StringShallowBytes(len(val.V))
	case List:
		return ListShallowBytes(val.Len())
	case Vector:
		return VectorShallowBytes(val.Len())
	case *HashMap:
		return HashMapShallowBytes(val.Len())
	case GoFunc:
		return ClosureShallowBytes(0) + StringShallowBytes(len(val.Name))
	case Lambda:
		return ClosureShallowBytes(len(val.Params)+len(val.Body)) + StringShallowBytes(len(val.Name))
	case Macro:
		return ClosureShallowBytes(len(val.Params)+len(val.Body)) + StringShallowBytes(len(val.Name))
	default:
		return MeterScalarBytes
	}
}

func ValueDeepBytes(v Value) int64 {
	return boundedDeepBytes(v, 0)
}

func ValueNodeCount(v Value) int {
	return boundedNodeCount(v, 0)
}
