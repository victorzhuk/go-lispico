package runtime

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/victorzhuk/go-lispico/core"
)

type Meter interface {
	LeaseEval(reductions, allocBytes int64) (grantedRed, grantedAlloc int64, err error)
	ReturnEval(reductions, allocBytes int64)
	ChargeRetained(bytes, slots int64) error
	ReleaseRetained(bytes, slots int64)
}

type NoopMeter struct{}

func (NoopMeter) LeaseEval(reductions, allocBytes int64) (int64, int64, error) {
	return reductions, allocBytes, nil
}

func (NoopMeter) ReturnEval(_, _ int64) {}

func (NoopMeter) ChargeRetained(_, _ int64) error { return nil }

func (NoopMeter) ReleaseRetained(_, _ int64) {}

type limitMeter struct {
	maxRed           int64
	maxAlloc         int64
	maxRetainedBytes int64
	maxRetainedSlots int64
	usedRed          atomic.Int64
	usedAlloc        atomic.Int64
	retainedBytes    atomic.Int64
	retainedSlots    atomic.Int64
}

func NewLimitMeter(maxRed, maxAlloc, maxRetainedBytes, maxRetainedSlots int64) Meter {
	return &limitMeter{maxRed: maxRed, maxAlloc: maxAlloc, maxRetainedBytes: maxRetainedBytes, maxRetainedSlots: maxRetainedSlots}
}

func (m *limitMeter) LeaseEval(reductions, allocBytes int64) (int64, int64, error) {
	grantedRed := leaseCounter(&m.usedRed, m.maxRed, reductions)
	grantedAlloc := leaseCounter(&m.usedAlloc, m.maxAlloc, allocBytes)
	if reductions > 0 && grantedRed == 0 {
		return grantedRed, grantedAlloc, core.NewResourceLimitError("evaluation reduction meter exhausted")
	}
	if allocBytes > 0 && grantedAlloc == 0 {
		return grantedRed, grantedAlloc, core.NewResourceLimitError("evaluation allocation meter exhausted")
	}
	return grantedRed, grantedAlloc, nil
}

func (m *limitMeter) ReturnEval(reductions, allocBytes int64) {
	returnCounter(&m.usedRed, reductions)
	returnCounter(&m.usedAlloc, allocBytes)
}

func (m *limitMeter) ChargeRetained(bytes, slots int64) error {
	if err := chargeCounter(&m.retainedBytes, m.maxRetainedBytes, bytes); err != nil {
		return fmt.Errorf("retained bytes: %w", err)
	}
	if err := chargeCounter(&m.retainedSlots, m.maxRetainedSlots, slots); err != nil {
		returnCounter(&m.retainedBytes, bytes)
		return fmt.Errorf("retained slots: %w", err)
	}
	return nil
}

func (m *limitMeter) ReleaseRetained(bytes, slots int64) {
	returnCounter(&m.retainedBytes, bytes)
	returnCounter(&m.retainedSlots, slots)
}

func leaseCounter(counter *atomic.Int64, max, requested int64) int64 {
	if requested <= 0 {
		return 0
	}
	if max <= 0 {
		counter.Add(requested)
		return requested
	}
	for {
		used := counter.Load()
		remaining := max - used
		if remaining <= 0 {
			return 0
		}
		granted := min(requested, remaining)
		if counter.CompareAndSwap(used, used+granted) {
			return granted
		}
	}
}

func chargeCounter(counter *atomic.Int64, max, n int64) error {
	if n <= 0 {
		return nil
	}
	if max <= 0 {
		counter.Add(n)
		return nil
	}
	for {
		used := counter.Load()
		if used+n > max {
			return core.NewResourceLimitError("meter limit exceeded")
		}
		if counter.CompareAndSwap(used, used+n) {
			return nil
		}
	}
}

func returnCounter(counter *atomic.Int64, n int64) {
	if n <= 0 {
		return
	}
	for {
		used := counter.Load()
		next := used - n
		if next < 0 {
			next = 0
		}
		if counter.CompareAndSwap(used, next) {
			return
		}
	}
}

func WithMeter(ctx context.Context, m Meter) context.Context {
	return core.WithEvalMeter(ctx, m)
}

func MeterFromContext(ctx context.Context) Meter {
	meter, _ := core.EvalMeterContextValue(ctx).(Meter)
	return meter
}

func WithEngineMeter(m Meter) EngineOption {
	return func(cfg *engineConfig) {
		cfg.engineMeter = m
	}
}
