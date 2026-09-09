package core

import (
	"context"
	"fmt"
	"math"
	"time"
)

// readerTokenUnitBytes is the deterministic workspace one planned token costs,
// the terminal EOF token included. It budgets the token slice the second pass
// fills; it is not a measurement of the Go token struct.
const readerTokenUnitBytes int64 = 32

// readerConversionSlackBytes is the bounded diagnostic storage a numeric
// conversion carries on top of its two token copies.
const readerConversionSlackBytes int64 = 256

// readerListCellBytes is the storage one shared-tail list cell costs. A list
// at or below listFlatThreshold links none.
const readerListCellBytes int64 = 32

// readerLinkBatch bounds how many cells a list chain links between two
// terminal-state checks, so a long chain stays interruptible.
const readerLinkBatch = 128

// readerClearBatch bounds how many retained slots a scratch clears on release
// between two terminal-state checks, so returning a large scratch to the pool
// stays as interruptible as the read that filled it.
const readerClearBatch = 128

// readerScratchNoCeiling is the allocation ceiling an unguarded read releases
// its scratch under: with no allowance to respect, retained capacity can never
// be over it.
const readerScratchNoCeiling int64 = math.MaxInt64

// readerSourceRenderLimit bounds the source an invalid-number diagnostic
// renders, truncation marker included: the token that provoked it is only
// bounded by the input.
const readerSourceRenderLimit = 128

const readerTruncationMarker = "..."

// readerBudget is the per-read state the guarded reader entry point installs
// on its scratch. It batches reader work against the evaluation ledger carried
// by ctx and settles it at explicit checkpoints. It deliberately does not go
// through PollEvalState or pollCancel: those batch and charge evaluator work of
// their own, which would both delay observation here and add reductions the
// reader's charge table does not document.
//
// A nil *readerBudget is the absent budget: the context-free reader keeps its
// exact current behavior because every guarded site is a no-op without one.
type readerBudget struct {
	ctx      context.Context
	meter    EvalMeter
	deadline time.Time
	pending  int64
	failed   error
}

// init fills b in place with the per-read state ctx carries and returns it. A
// budget hosted by storage the caller already owns — the pooled reader scratch
// — costs no allocation of its own, so arming a guarded read is free.
func (b *readerBudget) init(ctx context.Context) *readerBudget {
	*b = readerBudget{
		ctx:      ctx,
		meter:    EvalMeterFrom(ctx),
		deadline: EvalDeadlineFrom(ctx),
	}
	return b
}

// newReaderBudget returns a standalone budget, for a caller with no storage of
// its own to host one.
func newReaderBudget(ctx context.Context) *readerBudget {
	return new(readerBudget).init(ctx)
}

// checkpoint settles the work accumulated since the previous checkpoint and
// re-reads the terminal state. Pending work is charged exactly once; the
// deadline and the caller's context are consulted even when nothing is
// pending, so a read that starts cancelled — empty input included — fails
// before doing any work. The first terminal failure is sticky: once observed
// it is reported by every later checkpoint without charging again.
func (b *readerBudget) checkpoint() error {
	if b == nil {
		return nil
	}
	if b.failed != nil {
		return b.failed
	}

	n := b.pending
	b.pending = 0
	if err := b.meter.ChargeReductions(n); err != nil {
		b.failed = err
		return err
	}
	if !b.deadline.IsZero() && !nowFunc().Before(b.deadline) {
		b.failed = context.DeadlineExceeded
		return b.failed
	}
	if err := b.ctx.Err(); err != nil {
		b.failed = err
		return err
	}
	return nil
}

// work records n reduction units of interruptible reader work. It settles at
// every checkInterval boundary rather than at the end of the run, so a span
// far larger than the interval — a long copy, a long token, a long comment —
// is still interrupted within checkInterval units of work.
func (b *readerBudget) work(n int64) error {
	if b == nil {
		return nil
	}
	if b.failed != nil {
		return b.failed
	}
	for n > 0 {
		room := checkInterval - b.pending
		if n < room {
			b.pending += n
			return nil
		}
		b.pending += room
		n -= room
		if err := b.checkpoint(); err != nil {
			return err
		}
	}
	return nil
}

// err reports the terminal failure already observed, without charging or
// re-reading anything.
func (b *readerBudget) err() error {
	if b == nil {
		return nil
	}
	return b.failed
}

// settle closes out a read that is returning err. The terminal state is read
// one last time first and outranks err: a cancelled, expired or exhausted read
// truncates its own input, and the syntax error that truncation produces must
// never be what the caller sees.
func (b *readerBudget) settle(err error) error {
	if b == nil {
		return err
	}
	if failed := b.checkpoint(); failed != nil {
		return failed
	}
	return err
}

// checkedTokenPlanBytes returns the workspace bytes a token plan of the given
// number of tokens costs, the terminal EOF token included, and false when the
// product overflows or the count is negative. A refusal is a resource limit at
// the call site, never a wrapped value handed to ChargeAllocBytes.
func checkedTokenPlanBytes(tokens int64) (int64, bool) {
	if tokens < 0 || tokens > math.MaxInt64/readerTokenUnitBytes {
		return 0, false
	}
	return tokens * readerTokenUnitBytes, true
}

// checkedConversionBytes returns the temporary storage a numeric conversion of
// a token of tokenBytes bytes costs, and false when the sum overflows or the
// length is negative.
func checkedConversionBytes(tokenBytes int64) (int64, bool) {
	if tokenBytes < 0 || tokenBytes > (math.MaxInt64-readerConversionSlackBytes)/2 {
		return 0, false
	}
	return 2*tokenBytes + readerConversionSlackBytes, true
}

// boundedSource renders at most readerSourceRenderLimit bytes of src, marking a
// truncated one. Numeric tokens are ASCII, so the cut never splits a rune.
func boundedSource(src string) string {
	if len(src) <= readerSourceRenderLimit {
		return src
	}
	return src[:readerSourceRenderLimit-len(readerTruncationMarker)] + readerTruncationMarker
}

// admitAlloc reserves n bytes of storage on the ledger before the reader
// allocates them. A refusal is terminal for the read.
func (b *readerBudget) admitAlloc(n int64) error {
	if b == nil {
		return nil
	}
	if b.failed != nil {
		return b.failed
	}
	if err := b.meter.ChargeAllocBytes(n); err != nil {
		b.failed = err
		return err
	}
	return nil
}

// admitOutputNode reserves one parsed node: the node unit plus the payload it
// carries. A payload the counting pass already reserved is passed as zero, so
// the ledger records it once and never has to move back down.
func (b *readerBudget) admitOutputNode(payload int64) error {
	return b.admitAlloc(MeterReaderNodeBytes + payload)
}

// admitSlots reserves the value slots a collection copies its children into.
func (b *readerBudget) admitSlots(n int) error {
	return b.admitAlloc(ValueSlotsBytes(n))
}

// admitListCells reserves the shared-tail cells a list past listFlatThreshold
// links, before the first of them is allocated.
func (b *readerBudget) admitListCells(n int) error {
	return b.admitAlloc(int64(n) * readerListCellBytes)
}

// growthPlan is the logical doubling schedule a reader work buffer charges
// against: an initial slot, then the whole doubled capacity before the growth
// that fills it, because the old and new buffers coexist during the copy. The
// schedule is logical, so a pooled buffer's retained capacity avoids the Go
// allocation but never the charge.
type growthPlan struct {
	unit     int64
	capacity int64
}

// admit reserves whatever growth reaching a logical capacity of n entries
// costs. Capacity only ever rises, so a buffer shared across nested forms
// follows the read's high-water mark rather than one schedule per form.
func (g *growthPlan) admit(b *readerBudget, n int64) error {
	for g.capacity < n {
		if g.capacity == 0 {
			g.capacity = 1
		} else {
			g.capacity *= 2
		}
		if err := b.admitAlloc(g.capacity * g.unit); err != nil {
			return err
		}
	}
	return nil
}

// allocLimit normalizes the allocation ceiling a snapshot arms: an unset one
// runs under the package default.
func allocLimit(snap EvalMeterSnapshot) int64 {
	if snap.MaxAllocationBytes <= 0 {
		return DefaultMaxAllocationBytes
	}
	return snap.MaxAllocationBytes
}

// allocHeadroom reports the storage the ledger can still admit. Nothing charges
// storage while the counting pass runs, so one reading covers the whole pass.
func (b *readerBudget) allocHeadroom() int64 {
	if b == nil || !b.meter.Valid() {
		return math.MaxInt64
	}
	snap := b.meter.Snapshot()
	limit := allocLimit(snap)
	if snap.AllocationBytes >= limit {
		return 0
	}
	return limit - snap.AllocationBytes
}

// allocCeiling reports the allocation limit this read runs under, which is what
// its scratch is released against: a buffer larger than the whole allowance is
// storage no read under this limit may inherit. Headroom is deliberately not
// that number — it falls as a long-lived ledger fills, and releasing against it
// would make a warm engine discard its pooled scratch on every read.
func (b *readerBudget) allocCeiling() int64 {
	if b == nil || !b.meter.Valid() {
		return readerScratchNoCeiling
	}
	return allocLimit(b.meter.Snapshot())
}

// checkPlan refuses a plan that cannot fit headroom, so the counting pass stops
// at the token that overruns it instead of allocating the token slice to
// discover the failure. It charges nothing; reservePlan admits the plan once
// the count is final.
func (b *readerBudget) checkPlan(tokens, payload, headroom int64) error {
	if b == nil {
		return nil
	}
	if b.failed != nil {
		return b.failed
	}
	if plan, ok := checkedTokenPlanBytes(tokens); ok && payload >= 0 && plan <= headroom && payload <= headroom-plan {
		return nil
	}
	b.failed = NewResourceLimitError(fmt.Sprintf(
		"reader storage for %d tokens and %d decoded bytes exceeds the remaining allocation allowance of %d bytes",
		tokens, payload, headroom,
	))
	return b.failed
}

// reservePlan admits the counted token plan and the payload the second pass
// decodes, before either is allocated. A reused scratch buffer pays the same
// plan: the charge is the deterministic count, never the storage the pool
// happens to have retained.
func (b *readerBudget) reservePlan(tokens, payload int64) error {
	if b == nil {
		return nil
	}
	if b.failed != nil {
		return b.failed
	}
	plan, ok := checkedTokenPlanBytes(tokens)
	if !ok {
		b.failed = NewResourceLimitError(
			fmt.Sprintf("reader token plan of %d tokens overflows the storage budget", tokens),
		)
		return b.failed
	}
	if err := b.admitAlloc(plan); err != nil {
		return err
	}
	return b.admitAlloc(payload)
}

// admitConversionStorage reserves the temporary storage a numeric conversion
// carries into strconv — the conversion and error token copies plus bounded
// diagnostic storage — before entry, so neither a successful conversion nor a
// pooled buffer can bypass admission.
func (b *readerBudget) admitConversionStorage(tokenBytes int64) error {
	if b == nil {
		return nil
	}
	if b.failed != nil {
		return b.failed
	}
	n, ok := checkedConversionBytes(tokenBytes)
	if !ok {
		b.failed = NewResourceLimitError(
			fmt.Sprintf("numeric token of %d bytes overflows the reader conversion storage budget", tokenBytes),
		)
		return b.failed
	}
	return b.admitAlloc(n)
}

// admitConversion pre-admits an opaque numeric conversion of n bytes. strconv
// runs uninterrupted once entered, so the token is charged before entry and
// refused outright when it alone would claim more than a third of the reduction
// budget; earlier reader work tightens that further, since the charge below has
// to fit in what the budget has left. The diagnostic reports lengths only —
// the token that provoked it can be arbitrarily long.
func (b *readerBudget) admitConversion(n int64) error {
	if b == nil {
		return nil
	}
	if b.failed != nil {
		return b.failed
	}
	if limit := b.meter.Snapshot().MaxReductions / 3; limit > 0 && n > limit {
		b.failed = NewResourceLimitError(
			fmt.Sprintf("numeric token of %d bytes exceeds the reader conversion limit of %d", n, limit),
		)
		return b.failed
	}
	return b.work(n)
}
