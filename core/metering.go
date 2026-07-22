package core

import (
	"context"
	"fmt"
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
	MeterInstructionBytes      int64 = 4
	MeterReaderNodeBytes       int64 = 32
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
	if st.meter == nil && m != nil {
		st.meter = m
	}
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
		Reductions:         m.st.reductions.Load(),
		AllocationBytes:    m.st.allocBytes.Load(),
	}
}

func (m EvalMeter) ChargeReductions(n int64) error {
	if m.st == nil {
		return nil
	}
	return m.st.chargeReductions(n)
}

func EvalMeterIfMaterialized(ctx context.Context) EvalMeter {
	if w, ok := ctx.(*lazyEvalStateCtx); ok {
		if st := w.state.Load(); st != nil {
			return EvalMeter{st: st}
		}
		return EvalMeter{}
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
	st := evalStateFrom(ctx)
	top := st.evalDepth.Add(1) == 1
	if top {
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

func (st *evalState) pendingBytesFor(cell *Cell) int64 {
	var bytes int64
	for _, pending := range st.pendingCellAllocs {
		if pending.cell == cell {
			bytes += pending.bytes
		}
	}
	return bytes
}

func (st *evalState) settleRetained() error {
	if len(st.pendingCellAllocs) == 0 {
		return nil
	}
	charges := make([]retainedCharge, 0, 1)
	for _, pending := range st.pendingCellAllocs {
		if pending.meter == nil || (pending.bytes == 0 && pending.slots == 0) {
			continue
		}
		found := false
		for i := range charges {
			if charges[i].meter == pending.meter {
				charges[i].bytes += pending.bytes
				charges[i].slots += pending.slots
				found = true
				break
			}
		}
		if !found {
			charges = append(charges, retainedCharge{meter: pending.meter, bytes: pending.bytes, slots: pending.slots})
		}
	}
	charged := make([]retainedCharge, 0, len(charges))
	for _, charge := range charges {
		if err := charge.meter.ChargeRetained(charge.bytes, charge.slots); err != nil {
			for _, prev := range charged {
				prev.meter.ReleaseRetained(prev.bytes, prev.slots)
			}
			st.pendingCellAllocs = nil
			st.retainedBytes = 0
			st.retainedSlots = 0
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
	st.pendingCellAllocs = nil
	st.retainedBytes = 0
	st.retainedSlots = 0
	return nil
}

func (st *evalState) drawInitialLease() error {
	if st.meter == nil {
		return nil
	}
	return st.leaseEval(maxEvalReductionLease, maxEvalAllocLease)
}

func (st *evalState) chargeReductions(n int64) error {
	if n <= 0 {
		return nil
	}
	if st.meter != nil {
		return st.consumeReductionLease(n)
	}
	max := st.maxReductions
	if max <= 0 {
		max = DefaultMaxReductions
	}
	used := st.reductions.Add(n)
	if used > max {
		return NewResourceLimitError(fmt.Sprintf("reduction limit %d exceeded", max))
	}
	return nil
}

func (st *evalState) chargeAllocBytes(n int64) error {
	if n <= 0 {
		return nil
	}
	if st.meter != nil {
		return st.consumeAllocLease(n)
	}
	max := st.maxAllocBytes
	if max <= 0 {
		max = DefaultMaxAllocationBytes
	}
	used := st.allocBytes.Add(n)
	if used > max {
		return NewResourceLimitError(fmt.Sprintf("allocation limit %d bytes exceeded", max))
	}
	return nil
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
	if st.meter == nil {
		return nil
	}
	reductions = min(max(reductions, 0), maxEvalReductionLease)
	allocBytes = min(max(allocBytes, 0), maxEvalAllocLease)
	if reductions == 0 && allocBytes == 0 {
		return nil
	}
	grantedRed, grantedAlloc, err := st.meter.LeaseEval(reductions, allocBytes)
	grantedRed = max(grantedRed, 0)
	grantedAlloc = max(grantedAlloc, 0)
	if err != nil {
		if grantedRed > 0 || grantedAlloc > 0 {
			st.meter.ReturnEval(grantedRed, grantedAlloc)
		}
		return NewResourceLimitError(fmt.Sprintf("evaluation meter exhausted: %v", err))
	}
	if reductions > 0 && grantedRed <= 0 {
		if grantedRed > 0 || grantedAlloc > 0 {
			st.meter.ReturnEval(grantedRed, grantedAlloc)
		}
		return NewResourceLimitError("evaluation reduction meter exhausted")
	}
	if allocBytes > 0 && grantedAlloc <= 0 {
		if grantedRed > 0 || grantedAlloc > 0 {
			st.meter.ReturnEval(grantedRed, grantedAlloc)
		}
		return NewResourceLimitError("evaluation allocation meter exhausted")
	}
	st.leasedReductions += grantedRed
	st.leasedAllocBytes += grantedAlloc
	return nil
}

func (st *evalState) returnEvalLease() {
	if st.meter == nil {
		return
	}
	red, alloc := st.leasedReductions, st.leasedAllocBytes
	st.leasedReductions, st.leasedAllocBytes = 0, 0
	st.meter.ReturnEval(red, alloc)
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
		return ListShallowBytes(len(val.Items))
	case Vector:
		return VectorShallowBytes(len(val.Items))
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
