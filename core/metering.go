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
	MeterClosureHeaderBytes    int64 = 64
	MeterClosureCaptureBytes   int64 = 8
	MeterInstructionBytes      int64 = 4
	MeterReaderNodeBytes       int64 = 32
)

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

func (st *evalState) chargeReductions(n int64) error {
	if n <= 0 {
		return nil
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
