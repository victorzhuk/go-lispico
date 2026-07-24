// Package vm implements the stack-based bytecode virtual machine that
// executes chunks produced by core/compiler.
package vm

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/victorzhuk/go-lispico/core"
)

var nowFunc = time.Now

// Closure is a compiled function: a Chunk paired with the flat capture array
// holding exactly the free variables it uses, plus the globals env its body
// resolves non-local names against. It implements core.Value.
type Closure struct {
	Chunk   *Chunk
	caps    []*cellBox
	globals *core.Env
}

// cellBox is the shared storage cell for a captured local: one allocation at
// the binding site, written through on every mutation, referenced directly by
// every closure over the variable. VM-internal: unlike core.Cell it carries no
// canonical flag or version, since captured locals never participate in
// name-keyed env lookup or site caching.
type cellBox struct {
	v core.Value
}

// cellBox sits in frame slots, so it must satisfy core.Value even though user
// code never observes one: OpGetCell/OpGetCap push the cell's content, never
// the cell itself.
func (b *cellBox) Type() core.Keyword { return core.Keyword{V: "cell"} }
func (b *cellBox) String() string     { return "#<cell>" }
func (b *cellBox) Equals(o core.Value) bool {
	other, ok := o.(*cellBox)
	return ok && b == other
}

// NewClosure creates a Closure over chunk with the given capture array and
// globals env.
func NewClosure(chunk *Chunk, caps []*cellBox, globals *core.Env) *Closure {
	return &Closure{Chunk: chunk, caps: caps, globals: globals}
}

// Type implements core.Value.
func (c *Closure) Type() core.Keyword { return core.Keyword{V: "fn"} }

// String implements core.Value.
func (c *Closure) String() string { return fmt.Sprintf("#<closure %s>", c.Chunk.Name) }

// Equals implements core.Value. Closures are equal only by identity.
func (c *Closure) Equals(o core.Value) bool {
	other, ok := o.(*Closure)
	return ok && c == other
}

type handler struct {
	addr        int
	frameDepth  int
	stackDepth  int
	freezeDepth int
	structDepth int64
}

type freezeRec struct {
	depth int
	op    Opcode
	val   core.Value
}

// VM is a stack-based bytecode virtual machine.
// It is not safe for concurrent use on the same instance; callers that need
// concurrency-safe evaluation should use a fresh VM per evaluation.
type VM struct {
	stack              []core.Value
	frames             []Frame
	handlers           []handler
	globals            *core.Env
	maxDepth           int
	depth              int
	eval               core.Evaluator
	structDepth        *atomic.Int64
	callDepth          *atomic.Int64
	maxStructuralDepth int
	// freezeStack is a LIFO of pending native-op head resolutions: each
	// OpFreezeNative/OpFreezeNativeFunc pushes a record at head-resolution
	// time, each fused OpAdd..OpEq pops the matching record at dispatch.
	// throw unwinds it to the handler's snapshot (set at OpSetupTry) so
	// records from aborted computations cannot misfire in a catch body.
	freezeStack []freezeRec
	// deadline is the engine-owned evaluation deadline enforced at the VM's
	// batched cancellation checks. Zero means no engine deadline is set.
	deadline      time.Time
	timeout       time.Duration
	deadlineArmed bool
	// budget counts instructions until the next batched cancellation check.
	budget        int
	flushedBudget int
	meter         core.EvalMeter
	maxReductions int64
	maxAllocBytes int64
	reductions    int64
	allocBytes    int64
	// ownStructDepth is the VM's private structural-depth counter. structDepth
	// points here until a re-entrant GoFunc dispatch adopts a shared counter
	// (see reentrantCtx); Reset restores the pointer and zeroes this back out.
	ownStructDepth atomic.Int64
	ownCallDepth   atomic.Int64
	// reentryCtx is per-invocation scratch (like budget), reset each run: the
	// ctx adopted for re-entrant GoFunc calls once one occurs, so repeated
	// callbacks within the same run share one evalState instead of adopting
	// afresh each time. Never a stored request context.
	reentryCtx context.Context
}

// VMOption configures a VM created by New.
type VMOption func(*VM)

// WithEvaluator sets the evaluator passed to GoFunc callbacks invoked by this VM.
// Defaults to a tree-walking evaluator so GoFuncs can recursively evaluate forms.
func WithEvaluator(e core.Evaluator) VMOption {
	return func(v *VM) { v.eval = e }
}

// WithMaxDepth sets the maximum call depth before the VM aborts with an
// error. Zero (the default) means unlimited.
func WithMaxDepth(d int) VMOption {
	return func(v *VM) { v.maxDepth = d }
}

// WithMaxStructuralDepth sets the maximum structural depth before the VM
// aborts with a resource limit error. Zero (the default) means unlimited.
func WithMaxStructuralDepth(n int) VMOption {
	return func(v *VM) { v.maxStructuralDepth = n }
}

// WithStructuralDepthCounter sets the shared structural-depth counter. When
// nil the VM uses its own private counter (set automatically in New).
func WithStructuralDepthCounter(c *atomic.Int64) VMOption {
	return func(v *VM) {
		if c != nil {
			v.structDepth = c
		}
	}
}

// WithCallDepthCounter sets the shared call-depth counter. When nil the VM
// uses its own private counter.
func WithCallDepthCounter(c *atomic.Int64) VMOption {
	return func(v *VM) {
		if c != nil {
			v.callDepth = c
		}
	}
}

// New creates a VM using globals as the root environment.
func New(globals *core.Env, opts ...VMOption) *VM {
	v := &VM{
		stack:   make([]core.Value, 0, 256),
		frames:  make([]Frame, 0, 64),
		globals: globals,
		eval:    core.NewEvaluator(),
	}
	v.structDepth = &v.ownStructDepth
	v.callDepth = &v.ownCallDepth
	for _, opt := range opts {
		opt(v)
	}
	return v
}

func (vm *VM) checkConstructionDepth(v core.Value) error {
	if vm.maxStructuralDepth <= 0 {
		return nil
	}
	if core.ValueDepthExceeds(v, vm.maxStructuralDepth) {
		return core.NewResourceLimitError(fmt.Sprintf("structural depth limit %d exceeded", vm.maxStructuralDepth))
	}
	return nil
}

func (vm *VM) stackSize() int  { return len(vm.stack) }
func (vm *VM) frameCount() int { return len(vm.frames) }
func (vm *VM) reset() {
	vm.stack = vm.stack[:0]
	vm.frames = vm.frames[:0]
	vm.handlers = vm.handlers[:0]
	vm.depth = 0
	vm.structDepth = &vm.ownStructDepth
	vm.ownStructDepth.Store(0)
	vm.callDepth = &vm.ownCallDepth
	vm.ownCallDepth.Store(0)
	vm.reentryCtx = nil
	vm.freezeStack = vm.freezeStack[:0]
	vm.deadline = time.Time{}
	vm.timeout = 0
	vm.deadlineArmed = false
	vm.meter = core.EvalMeter{}
	vm.maxReductions = 0
	vm.maxAllocBytes = 0
	vm.reductions = 0
	vm.allocBytes = 0
	vm.budget = 0
	vm.flushedBudget = 0
}

// Reset clears the VM state (stacks, frames, handlers, depth) so the
// instance can be reused for a new evaluation. It does not change the
// VM's configuration (globals, max depth, evaluator).
func (vm *VM) Reset() {
	vm.stack = vm.stack[:0]
	vm.frames = vm.frames[:0]
	vm.handlers = vm.handlers[:0]
	vm.depth = 0
	vm.structDepth = &vm.ownStructDepth
	vm.ownStructDepth.Store(0)
	vm.callDepth = &vm.ownCallDepth
	vm.ownCallDepth.Store(0)
	vm.reentryCtx = nil
	vm.freezeStack = vm.freezeStack[:0]
	vm.deadline = time.Time{}
	vm.timeout = 0
	vm.deadlineArmed = false
	vm.meter = core.EvalMeter{}
	vm.maxReductions = 0
	vm.maxAllocBytes = 0
	vm.reductions = 0
	vm.allocBytes = 0
	vm.budget = 0
	vm.flushedBudget = 0
}

// ResetIncremental clears only the dirtiable cross-call state left behind by a
// successful call (a GoFunc dispatch that adopted a shared structural-depth
// counter and deadline via reentrantCtx/AdoptEvalState) without re-truncating
// the stacks, frames, handlers, freezeStack and depth that the run/apply loop
// already restored on a clean top-level exit. Used by stateful handle paths
// (PinnedFn) that own a private VM and want the steady-state allocation cost
// to stay at the per-call overhead rather than the full Reset path.
//
// Callers MUST have verified a clean post-run invariant (empty frames,
// truncated stack, empty handlers and freezeStack, zero depth). If any of those
// is dirty, ResetIncremental falls through to a full Reset and reports which
// invariant was violated so the caller can investigate; it never silently
// reuses stale state.
func (vm *VM) ResetIncremental() error {
	if len(vm.frames) != 0 || len(vm.handlers) != 0 || len(vm.freezeStack) != 0 || vm.depth != 0 {
		vm.Reset()
		return fmt.Errorf("vm: ResetIncremental invariant violated (frames=%d handlers=%d freezeStack=%d depth=%d)",
			len(vm.frames), len(vm.handlers), len(vm.freezeStack), vm.depth)
	}
	if l := len(vm.stack); l != 0 {
		vm.Reset()
		return fmt.Errorf("vm: ResetIncremental invariant violated (stack len=%d, want 0)", l)
	}
	vm.ownStructDepth.Store(0)
	vm.structDepth = &vm.ownStructDepth
	vm.ownCallDepth.Store(0)
	vm.callDepth = &vm.ownCallDepth
	vm.reentryCtx = nil
	vm.deadline = time.Time{}
	vm.timeout = 0
	vm.deadlineArmed = false
	vm.meter = core.EvalMeter{}
	vm.maxReductions = 0
	vm.maxAllocBytes = 0
	vm.reductions = 0
	vm.allocBytes = 0
	vm.budget = 0
	vm.flushedBudget = 0
	return nil
}

// SetGlobals replaces the VM's globals (root environment) pointer.
// Used when reusing a pooled VM for a different environment.
func (vm *VM) SetGlobals(env *core.Env) {
	vm.globals = env
}

// SetDeadline sets the engine-owned evaluation deadline enforced at the VM's
// batched check points. A zero t means the caller's context is the only bound.
func (vm *VM) SetDeadline(t time.Time) {
	vm.deadline = t
	vm.deadlineArmed = true
}

// SetTimeout sets the lazy engine timeout. The deadline is armed at the first
// cancellation checkpoint, or before a re-entrant evaluator call adopts state.
func (vm *VM) SetTimeout(d time.Duration) {
	vm.deadline = time.Time{}
	vm.timeout = d
	vm.deadlineArmed = false
}

func (vm *VM) SetEvalMeter(m core.EvalMeter) {
	vm.meter = m
	snap := m.Snapshot()
	vm.maxReductions = snap.MaxReductions
	vm.maxAllocBytes = snap.MaxAllocationBytes
	vm.reductions = 0
	vm.allocBytes = 0
	vm.flushedBudget = vm.budget
}

func (vm *VM) SetResourceLimits(maxReductions, maxAllocBytes int) {
	vm.meter = core.EvalMeter{}
	vm.maxReductions = int64(maxReductions)
	vm.maxAllocBytes = int64(maxAllocBytes)
	if vm.maxReductions <= 0 {
		vm.maxReductions = core.DefaultMaxReductions
	}
	if vm.maxAllocBytes <= 0 {
		vm.maxAllocBytes = core.DefaultMaxAllocationBytes
	}
	vm.reductions = 0
	vm.allocBytes = 0
	vm.flushedBudget = vm.budget
}

func (vm *VM) chargeReductions(n int64) error {
	if n <= 0 {
		return nil
	}
	if vm.meter.Valid() {
		return vm.meter.ChargeReductions(n)
	}
	if vm.maxReductions <= 0 {
		return nil
	}
	vm.reductions += n
	if vm.reductions > vm.maxReductions {
		return core.NewResourceLimitError(fmt.Sprintf("reduction limit %d exceeded", vm.maxReductions))
	}
	return nil
}

func (vm *VM) flushConsumedReductions() error {
	used := vm.flushedBudget - vm.budget
	if used <= 0 {
		return nil
	}
	vm.flushedBudget = vm.budget
	return vm.chargeReductions(int64(used))
}

func (vm *VM) chargeAllocBytes(n int64) error {
	if n <= 0 {
		return nil
	}
	if vm.meter.Valid() {
		return vm.meter.ChargeAllocBytes(n)
	}
	if vm.maxAllocBytes <= 0 {
		return nil
	}
	vm.allocBytes += n
	if vm.allocBytes > vm.maxAllocBytes {
		return core.NewResourceLimitError(fmt.Sprintf("allocation limit %d bytes exceeded", vm.maxAllocBytes))
	}
	return nil
}

func (vm *VM) chargeValue(v core.Value) error {
	return vm.chargeAllocBytes(core.ValueShallowBytes(v))
}

func (vm *VM) meterSnapshot() core.EvalMeterSnapshot {
	if vm.meter.Valid() {
		return vm.meter.Snapshot()
	}
	return core.EvalMeterSnapshot{
		MaxReductions:      vm.maxReductions,
		MaxAllocationBytes: vm.maxAllocBytes,
		Reductions:         vm.reductions,
		AllocationBytes:    vm.allocBytes,
	}
}

func (vm *VM) syncMeterFromReentry() {
	if vm.reentryCtx == nil {
		return
	}
	m := core.EvalMeterIfMaterialized(vm.reentryCtx)
	if !m.Valid() {
		return
	}
	vm.meter = m
	snap := m.Snapshot()
	vm.maxReductions = snap.MaxReductions
	vm.maxAllocBytes = snap.MaxAllocationBytes
	vm.reductions = snap.Reductions
	vm.allocBytes = snap.AllocationBytes
}

func (vm *VM) armDeadline(ctx context.Context) {
	if vm.deadlineArmed || vm.timeout <= 0 {
		return
	}
	vm.deadlineArmed = true
	bound := nowFunc().Add(vm.timeout)
	if d, ok := ctx.Deadline(); ok && !d.After(bound) {
		return
	}
	vm.deadline = bound
}

// reentrantCtx lazily builds (once per run) a context carrying an evalState that
// shares the VM's structural-depth counter and deadline, so a GoFunc that calls
// back into the evaluator enforces the same resource budget across the
// boundary. Only reached on a real GoFunc dispatch — the native-op fast path
// never calls it.
func (vm *VM) reentrantCtx(ctx context.Context) (context.Context, error) {
	if err := vm.flushConsumedReductions(); err != nil {
		return nil, err
	}
	if vm.reentryCtx != nil {
		return vm.reentryCtx, nil
	}
	vm.armDeadline(ctx)
	adopted, structCounter, meter := core.AdoptEvalStateWithMeter(ctx, vm.deadline, vm.structDepth.Load(), vm.meterSnapshot(), vm.callDepth.Load())
	vm.structDepth = structCounter
	vm.callDepth = core.EvalCallCounter(adopted)
	if meter.Valid() {
		vm.meter = meter
	}
	vm.reentryCtx = adopted
	return adopted, nil
}

func (vm *VM) pushReentrantDepth(reCtx context.Context) (*atomic.Int64, int64) {
	if vm.depth == 0 {
		return nil, 0
	}
	d := int64(vm.depth)
	c := core.EvalCallCounter(reCtx)
	c.Add(d)
	return c, d
}

func (vm *VM) push(v core.Value) {
	vm.stack = append(vm.stack, v)
}

// growStack ensures vm.stack has capacity for base+maxStack, so pushes within
// a newly entered frame don't trigger a reallocation mid-execution. It never
// changes len(vm.stack), only cap.
func (vm *VM) growStack(base, maxStack int) {
	need := base + maxStack
	if need <= cap(vm.stack) {
		return
	}
	grown := make([]core.Value, len(vm.stack), need)
	copy(grown, vm.stack)
	vm.stack = grown
}

// reloadFrame reads the top frame's state into Run's per-frame dispatch
// locals after a helper that can push, pop, or replace frames (vm.call,
// vm.throw) returns. Callers must only call it when vm.frames is non-empty.
func (vm *VM) reloadFrame() (chunk *Chunk, code []Instruction, ip, base int, env *core.Env, caps []*cellBox, truthy func(core.Value) bool) {
	frame := &vm.frames[len(vm.frames)-1]
	truthy = core.IsTruthy
	if frame.chunk.Truthiness != nil {
		truthy = frame.chunk.Truthiness
	}
	return frame.chunk, frame.chunk.Code, frame.ip, frame.base, frame.env, frame.caps, truthy
}

func (vm *VM) pushFreeze(depth int, op Opcode, val core.Value) {
	vm.freezeStack = append(vm.freezeStack, freezeRec{depth: depth, op: op, val: val})
}

func (vm *VM) pop() (core.Value, error) {
	if len(vm.stack) == 0 {
		return nil, &core.LispicoError{Code: "BytecodeError", Message: "stack underflow"}
	}
	top := vm.stack[len(vm.stack)-1]
	vm.stack = vm.stack[:len(vm.stack)-1]
	return top, nil
}

func (vm *VM) peek() (core.Value, error) {
	if len(vm.stack) == 0 {
		return nil, &core.LispicoError{Code: "BytecodeError", Message: "stack underflow"}
	}
	return vm.stack[len(vm.stack)-1], nil
}

// Apply calls fn with args in a fresh isolated VM and returns the result.
// The receiver is used only for configuration (globals, max depth, evaluator).
func (v *VM) Apply(ctx context.Context, fn core.Value, args []core.Value, env *core.Env) (core.Value, error) {
	fresh := New(env, WithMaxDepth(v.maxDepth), WithEvaluator(v.eval), WithMaxStructuralDepth(v.maxStructuralDepth), WithStructuralDepthCounter(v.structDepth), WithCallDepthCounter(core.EvalCallCounter(ctx)))
	fresh.deadline = v.deadline
	fresh.timeout = v.timeout
	fresh.deadlineArmed = v.deadlineArmed
	fresh.meter = v.meter
	fresh.maxReductions = v.maxReductions
	fresh.maxAllocBytes = v.maxAllocBytes
	return fresh.ApplyPooled(ctx, fn, args, env)
}

// ApplyPooled calls fn with args on this VM instance (no fresh VM allocation).
// The caller MUST have called Reset (or obtained this VM from a pool that
// resets) before calling ApplyPooled, and MUST NOT reuse this VM concurrently.
// For fresh-isolation semantics use Apply instead.
func (v *VM) ApplyPooled(ctx context.Context, fn core.Value, args []core.Value, env *core.Env) (core.Value, error) {
	counter := v.callDepth
	if counter == nil {
		counter = &v.ownCallDepth
		v.callDepth = counter
	}
	sharedDepth := counter.Add(1)
	defer counter.Add(-1)
	if v.maxDepth > 0 && (int64(v.depth) >= int64(v.maxDepth) || sharedDepth > int64(v.maxDepth)) {
		return nil, &core.LispicoError{Code: "EvalError", Message: "maximum call depth exceeded"}
	}
	return v.apply(ctx, fn, args, env)
}

func (vm *VM) apply(ctx context.Context, fn core.Value, args []core.Value, env *core.Env) (core.Value, error) {
	switch f := fn.(type) {
	case *Closure:
		if f.Chunk.Variadic {
			if len(args) < f.Chunk.Arity {
				return nil, core.NewArityError(f.Chunk.Arity, len(args))
			}
		} else {
			if len(args) != f.Chunk.Arity {
				return nil, core.NewArityError(f.Chunk.Arity, len(args))
			}
		}
		// Seed the stack with [closure, arg0, arg1, ...] — the layout vm.call
		// expects — and let it push the closure's frame directly, skipping the
		// per-call wrapper Chunk the old implementation synthesized here.
		vm.push(f)
		for _, arg := range args {
			vm.push(arg)
		}
		if err := vm.call(ctx, len(args), false); err != nil {
			if core.IsTerminalEvalError(err) {
				vm.Reset()
				return nil, err
			}
			if !vm.throw(core.String{V: err.Error()}) {
				return nil, err
			}
		}
		result, err := vm.run(ctx)
		if core.IsTerminalEvalError(err) {
			vm.Reset()
		}
		return result, err
	case core.Lambda:
		eval := vm.eval
		if eval == nil {
			eval = core.NewEvaluator()
		}
		reCtx, err := vm.reentrantCtx(ctx)
		if err != nil {
			return nil, err
		}
		if c, d := vm.pushReentrantDepth(reCtx); c != nil {
			defer c.Add(-d)
		}

		result, err := eval.Apply(reCtx, f, args, env)
		vm.syncMeterFromReentry()
		return result, err

	case core.GoFunc:
		eval := vm.eval
		if eval == nil {
			eval = core.NewEvaluator()
		}
		reCtx, err := vm.reentrantCtx(ctx)
		if err != nil {
			return nil, err
		}
		if c, d := vm.pushReentrantDepth(reCtx); c != nil {
			defer c.Add(-d)
		}

		if err := vm.chargeReductions(1); err != nil {
			return nil, err
		}
		result, err := f.Fn(reCtx, eval, args, env)
		vm.syncMeterFromReentry()
		if err != nil {
			return nil, err
		}
		if err := vm.chargeValue(result); err != nil {
			return nil, err
		}
		return result, nil
	case core.Keyword:
		if len(args) != 1 {
			return nil, keywordArityError(len(args))
		}
		m, ok := args[0].(*core.HashMap)
		if !ok {
			return core.Nil{}, nil
		}
		v, _ := m.Get(f)
		if v == nil {
			return core.Nil{}, nil
		}
		return v, nil
	default:
		return nil, core.NewTypeError("callable", fn)
	}
}

// keywordArityError reports a keyword-as-function call with an argument
// count other than 1, matching the tree-walker's evalErrorf shape exactly
// (Code "EvalError") so cross-val tests can assert equality.
func keywordArityError(got int) *core.LispicoError {
	return &core.LispicoError{Code: "EvalError", Message: fmt.Sprintf("keyword lookup requires exactly 1 argument, got %d", got)}
}

// checkInterval bounds how many instructions/nodes run between batched
// cancellation checks. A fresh run starts with a full checkInterval budget
// before the first check, then every checkInterval thereafter.
const checkInterval = 128

// pollCancel checks the engine deadline and ctx for cancellation, resetting
// vm.budget for the next batch. Errors are wrapped with the "vm: " prefix so
// errors.Is(err, context.DeadlineExceeded/Canceled) still holds.
func (vm *VM) pollCancel(ctx context.Context) error {
	if err := vm.flushConsumedReductions(); err != nil {
		return err
	}
	vm.budget = checkInterval
	vm.flushedBudget = checkInterval
	if !vm.deadlineArmed {
		vm.armDeadline(ctx)
	}
	if !vm.deadline.IsZero() && !time.Now().Before(vm.deadline) {
		return fmt.Errorf("vm: %w", context.DeadlineExceeded)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("vm: %w", err)
	}
	return nil
}

// Run pushes a new frame for chunk and executes it to completion, returning
// the result of its top-level OpReturn.
func (vm *VM) Run(ctx context.Context, chunk *Chunk) (core.Value, error) {
	base := len(vm.stack)
	vm.frames = append(vm.frames, Frame{chunk: chunk, base: base, env: vm.globals})
	vm.growStack(base, chunk.MaxStack)
	result, err := vm.run(ctx)
	if core.IsTerminalEvalError(err) {
		vm.Reset()
	}
	return result, err
}

// run drives the dispatch loop from the current top frame until vm.frames
// empties, returning the result of that frame's terminating OpReturn.
// Callers must have already pushed the frame to execute (and, for a call,
// its callee + args below it on vm.stack) — see Run and apply.
func (vm *VM) run(ctx context.Context) (core.Value, error) {
	chunk, code, ip, base, env, caps, truthy := vm.reloadFrame()
	vm.budget = checkInterval
	vm.flushedBudget = checkInterval
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("vm: %w", err)
	}

	for {
		if vm.budget--; vm.budget <= 0 {
			if err := vm.pollCancel(ctx); err != nil {
				return nil, err
			}
		}

		instr := code[ip]
		ip++

		switch instr.Op() {
		case OpNil:
			vm.push(core.Nil{})

		case OpTrue:
			vm.push(core.Bool{V: true})

		case OpFalse:
			vm.push(core.Bool{V: false})

		case OpConst:
			vm.push(chunk.Constants[instr.A()])

		case OpGetLocal:
			slot := base + instr.A()
			if slot < 0 || slot >= len(vm.stack) {
				return nil, &core.LispicoError{Code: "BytecodeError", Message: fmt.Sprintf("local slot %d out of range", instr.A())}
			}
			vm.push(vm.stack[slot])

		case OpSetLocal:
			idx := instr.A()
			slot := base + idx
			if slot < 0 || slot >= len(vm.stack) {
				return nil, &core.LispicoError{Code: "BytecodeError", Message: fmt.Sprintf("local slot %d out of range", idx)}
			}
			top, err := vm.peek()
			if err != nil {
				return nil, err
			}
			vm.stack[slot] = top

		case OpGetCell:
			slot := base + instr.A()
			if slot < 0 || slot >= len(vm.stack) {
				return nil, &core.LispicoError{Code: "BytecodeError", Message: fmt.Sprintf("cell slot %d out of range", instr.A())}
			}
			box, ok := vm.stack[slot].(*cellBox)
			if !ok {
				return nil, &core.LispicoError{Code: "BytecodeError", Message: fmt.Sprintf("cell slot %d does not hold a cell", instr.A())}
			}
			vm.push(box.v)

		case OpSetCell:
			slot := base + instr.A()
			if slot < 0 || slot >= len(vm.stack) {
				return nil, &core.LispicoError{Code: "BytecodeError", Message: fmt.Sprintf("cell slot %d out of range", instr.A())}
			}
			box, ok := vm.stack[slot].(*cellBox)
			if !ok {
				return nil, &core.LispicoError{Code: "BytecodeError", Message: fmt.Sprintf("cell slot %d does not hold a cell", instr.A())}
			}
			top, err := vm.peek()
			if err != nil {
				return nil, err
			}
			box.v = top

		case OpBindCell:
			slot := base + instr.A()
			if slot < 0 || slot >= len(vm.stack) {
				return nil, &core.LispicoError{Code: "BytecodeError", Message: fmt.Sprintf("cell slot %d out of range", instr.A())}
			}
			top, err := vm.peek()
			if err != nil {
				return nil, err
			}
			vm.stack[slot] = &cellBox{v: top}

		case OpGetCap:
			idx := instr.A()
			if idx < 0 || idx >= len(caps) {
				return nil, &core.LispicoError{Code: "BytecodeError", Message: fmt.Sprintf("capture index %d out of range", idx)}
			}
			vm.push(caps[idx].v)

		case OpSetCap:
			idx := instr.A()
			if idx < 0 || idx >= len(caps) {
				return nil, &core.LispicoError{Code: "BytecodeError", Message: fmt.Sprintf("capture index %d out of range", idx)}
			}
			top, err := vm.peek()
			if err != nil {
				return nil, err
			}
			caps[idx].v = top

		case OpGetGlobal:
			sym := chunk.Constants[instr.A()].(core.Symbol)
			val, _, ok := vm.resolveGlobalValue(chunk.site(ip-1), env, sym)
			if !ok {
				return nil, core.NewUndefinedError(sym.V)
			}
			vm.push(val)

		case OpFreezeNative:
			sym := chunk.Constants[instr.A()].(core.Symbol)
			val, canon, ok := vm.resolveGlobalValue(chunk.site(ip-1), env, sym)
			if !ok {
				return nil, core.NewUndefinedError(sym.V)
			}
			// No push. Record the freeze marker (or the head-time value) at the
			// current stack depth — the depth that the upcoming fused native op
			// will use as its argument base.
			d := len(vm.stack)
			if canon && isNativeOpSymbol(sym.V) {
				if op, isOp := nativeSymbolToOp(sym.V); isOp {
					vm.pushFreeze(d, op, val)
				} else {
					vm.pushFreeze(d, OpConst, val)
				}
			} else {
				vm.pushFreeze(d, OpConst, val)
			}

		case OpSetGlobal:
			sym := chunk.Constants[instr.A()].(core.Symbol)
			top, err := vm.peek()
			if err != nil {
				return nil, err
			}
			if err := env.SetWithContext(ctx, sym.V, top); err != nil {
				return nil, err
			}

		case OpSetLexical:
			sym := chunk.Constants[instr.A()].(core.Symbol)
			top, err := vm.peek()
			if err != nil {
				return nil, err
			}
			owner, ok := env.Find(sym.V)
			if !ok {
				return nil, core.NewUndefinedError(sym.V)
			}
			if err := owner.SetWithContext(ctx, sym.V, top); err != nil {
				return nil, err
			}

		case OpGetFunc:
			sym := chunk.Constants[instr.A()].(core.Symbol)
			v, found, _ := env.GetFuncCanonical(sym.V)
			if !found {
				return nil, core.NewUndefinedError(sym.V)
			}
			vm.push(v)

		case OpFreezeNativeFunc:
			sym := chunk.Constants[instr.A()].(core.Symbol)
			v, found, canon := env.GetFuncCanonical(sym.V)
			if !found {
				return nil, core.NewUndefinedError(sym.V)
			}
			d := len(vm.stack)
			if canon && isNativeOpSymbol(sym.V) {
				if op, isOp := nativeSymbolToOp(sym.V); isOp {
					vm.pushFreeze(d, op, v)
				} else {
					vm.pushFreeze(d, OpConst, v)
				}
			} else {
				vm.pushFreeze(d, OpConst, v)
			}

		case OpSetFunc:
			sym := chunk.Constants[instr.A()].(core.Symbol)
			top, err := vm.peek()
			if err != nil {
				return nil, err
			}
			if err := env.SetFuncWithContext(ctx, sym.V, top); err != nil {
				return nil, err
			}

		case OpPop:
			if _, err := vm.pop(); err != nil {
				return nil, err
			}

		case OpJump:
			ip += instr.A()

		case OpJumpIfFalse:
			top, err := vm.pop()
			if err != nil {
				return nil, err
			}
			if !truthy(top) {
				ip += instr.A()
			}

		case OpCall:
			vm.frames[len(vm.frames)-1].ip = ip
			if err := vm.call(ctx, instr.A(), false); err != nil {
				if core.IsTerminalEvalError(err) {
					vm.Reset()
					return nil, err
				}
				if !vm.throw(core.String{V: err.Error()}) {
					return nil, err
				}
			}
			chunk, code, ip, base, env, caps, truthy = vm.reloadFrame()

		case OpTailCall:
			vm.frames[len(vm.frames)-1].ip = ip
			if err := vm.call(ctx, instr.A(), true); err != nil {
				if core.IsTerminalEvalError(err) {
					vm.Reset()
					return nil, err
				}
				if !vm.throw(core.String{V: err.Error()}) {
					return nil, err
				}
			}
			chunk, code, ip, base, env, caps, truthy = vm.reloadFrame()

		case OpReturn:
			result, err := vm.pop()
			if err != nil {
				return nil, err
			}
			frame := &vm.frames[len(vm.frames)-1]
			frame.ip = ip
			if frame.isClosure && vm.depth > 0 {
				vm.depth--
			}
			vm.frames = vm.frames[:len(vm.frames)-1]
			vm.stack = vm.stack[:base]
			for len(vm.handlers) > 0 && vm.handlers[len(vm.handlers)-1].frameDepth > len(vm.frames) {
				vm.handlers = vm.handlers[:len(vm.handlers)-1]
			}
			if len(vm.frames) == 0 {
				if err := vm.flushConsumedReductions(); err != nil {
					return nil, err
				}
				return result, nil
			}
			vm.push(result)
			chunk, code, ip, base, env, caps, truthy = vm.reloadFrame()

		case OpMakeList:
			n := instr.A()
			if n < 0 || n > len(vm.stack) {
				return nil, &core.LispicoError{Code: "BytecodeError", Message: fmt.Sprintf("make-list: %d items exceeds stack", n)}
			}
			items := make([]core.Value, n)
			copy(items, vm.stack[len(vm.stack)-n:])
			res := core.List{Items: items}
			if err := vm.checkConstructionDepth(res); err != nil {
				return nil, err
			}
			if err := vm.chargeAllocBytes(core.ListShallowBytes(n)); err != nil {
				return nil, err
			}
			vm.stack = vm.stack[:len(vm.stack)-n]
			vm.push(res)
		case OpMakeVector:
			n := instr.A()
			if n < 0 || n > len(vm.stack) {
				return nil, &core.LispicoError{Code: "BytecodeError", Message: fmt.Sprintf("make-vector: %d items exceeds stack", n)}
			}
			items := make([]core.Value, n)
			copy(items, vm.stack[len(vm.stack)-n:])
			res := core.Vector{Items: items}
			if err := vm.checkConstructionDepth(res); err != nil {
				return nil, err
			}
			if err := vm.chargeAllocBytes(core.VectorShallowBytes(n)); err != nil {
				return nil, err
			}
			vm.stack = vm.stack[:len(vm.stack)-n]
			vm.push(res)

		case OpMakeMap:
			pairCount := instr.A()
			n := pairCount * 2
			if n < 0 || n > len(vm.stack) {
				return nil, &core.LispicoError{Code: "BytecodeError", Message: fmt.Sprintf("make-map: %d items exceeds stack", n)}
			}
			pairs := vm.stack[len(vm.stack)-n:]
			hm := core.NewHashMap()
			for i := 0; i < len(pairs); i += 2 {
				if err := hm.Set(pairs[i], pairs[i+1]); err != nil {
					return nil, err
				}
			}
			if err := vm.checkConstructionDepth(hm); err != nil {
				return nil, err
			}
			if err := vm.chargeAllocBytes(core.HashMapShallowBytes(pairCount)); err != nil {
				return nil, err
			}
			vm.stack = vm.stack[:len(vm.stack)-n]
			vm.push(hm)

		case OpStructEnter:
			n := instr.A()
			vm.structDepth.Add(int64(n))
			if vm.maxStructuralDepth > 0 && int(vm.structDepth.Load()) > vm.maxStructuralDepth {
				vm.structDepth.Add(-int64(n))
				return nil, &core.LispicoError{
					Code:    core.CodeResourceLimit,
					Message: fmt.Sprintf("structural depth limit %d exceeded", vm.maxStructuralDepth),
				}
			}

		case OpStructLeave:
			n := instr.A()
			vm.structDepth.Add(-int64(n))

		case OpClosure:
			sub := chunk.SubChunks[instr.A()]
			if err := vm.chargeAllocBytes(core.ClosureShallowBytes(len(sub.Caps))); err != nil {
				return nil, err
			}
			var subCaps []*cellBox
			if len(sub.Caps) > 0 {
				subCaps = make([]*cellBox, len(sub.Caps))
				for i, d := range sub.Caps {
					if d.FromCaps {
						subCaps[i] = caps[d.Cap]
					} else {
						box, ok := vm.stack[base+d.Slot].(*cellBox)
						if !ok {
							return nil, &core.LispicoError{Code: "BytecodeError", Message: fmt.Sprintf("capture slot %d does not hold a cell", d.Slot)}
						}
						subCaps[i] = box
					}
				}
			}
			vm.push(NewClosure(sub, subCaps, env))

		case OpDup:
			top, err := vm.peek()
			if err != nil {
				return nil, err
			}
			vm.push(top)

		case OpLoop:
			ip = instr.A()

		case OpSetupTry:
			vm.handlers = append(vm.handlers, handler{addr: instr.A(), frameDepth: len(vm.frames), stackDepth: len(vm.stack), freezeDepth: len(vm.freezeStack), structDepth: vm.structDepth.Load()})
		case OpPopTry:
			if len(vm.handlers) > 0 {
				vm.handlers = vm.handlers[:len(vm.handlers)-1]
			}
		case OpThrow:
			value, err := vm.pop()
			if err != nil {
				return nil, err
			}
			vm.frames[len(vm.frames)-1].ip = ip
			if !vm.throw(coerceThrow(value)) {
				return nil, core.NewTypeError("handler", core.Nil{})
			}
			chunk, code, ip, base, env, caps, truthy = vm.reloadFrame()

		case OpAdd, OpSub, OpMul, OpDiv, OpLt, OpGt, OpLe, OpGe, OpEq:
			vm.frames[len(vm.frames)-1].ip = ip
			if err := vm.dispatchNativeOp(ctx, env, instr.Op(), instr.A()); err != nil {
				if core.IsTerminalEvalError(err) {
					vm.Reset()
					return nil, err
				}
				if !vm.throw(core.String{V: err.Error()}) {
					return nil, err
				}
			}
			chunk, code, ip, base, env, caps, truthy = vm.reloadFrame()
		}
	}
}

// dispatchNativeOp executes a native arithmetic/comparison opcode from [args...] on
// the current operand stack, using the frozen marker when available.
func (vm *VM) dispatchNativeOp(ctx context.Context, env *core.Env, op Opcode, argc int) error {
	if argc < 0 || argc > len(vm.stack) {
		return &core.LispicoError{Code: "BytecodeError", Message: fmt.Sprintf("native: argc=%d exceeds stack", argc)}
	}
	d := len(vm.stack) - argc
	if len(vm.freezeStack) > 0 && vm.freezeStack[len(vm.freezeStack)-1].depth == d {
		rec := vm.freezeStack[len(vm.freezeStack)-1]
		vm.freezeStack = vm.freezeStack[:len(vm.freezeStack)-1]
		if rec.op == op {
			return vm.execNativeFastFused(op, argc, env)
		}
		if rec.val != nil {
			// Splice the head-time operator under the args; vm.call expects
			// [operator, arg1, ...] on top of the stack.
			vm.stack = append(vm.stack, core.Nil{})
			copy(vm.stack[d+1:], vm.stack[d:len(vm.stack)-1])
			vm.stack[d] = rec.val
			return vm.call(ctx, argc, false)
		}
	}
	// Recovery: hand-built chunk without a preceding freeze. Resolve the
	// operator symbol via the opcode identity (Lisp-1 default — Lisp-2
	// hand-built chunks are expected to emit OpFreezeNativeFunc first).
	sym, ok := opToNativeSymbol(op)
	if !ok {
		return &core.LispicoError{Code: "BytecodeError", Message: fmt.Sprintf("native: no marker and op %v not native", op)}
	}
	val, found, _ := env.GetCanonical(sym)
	if !found {
		return core.NewUndefinedError(sym)
	}
	vm.stack = append(vm.stack, core.Nil{})
	copy(vm.stack[d+1:], vm.stack[d:len(vm.stack)-1])
	vm.stack[d] = val
	return vm.call(ctx, argc, false)
}

// execNativeFastFused runs op over the argc values already on top of the stack,
// replacing them with the result (the operator head slot was already dropped).
func (vm *VM) execNativeFastFused(op Opcode, argc int, env *core.Env) error {
	d := len(vm.stack) - argc
	args := vm.stack[d:]
	eval := vm.eval
	if eval == nil {
		eval = core.NewEvaluator()
	}
	result, err := execNative(eval, op, args, env)
	if err != nil {
		return err
	}
	vm.stack = vm.stack[:d]
	vm.push(result)
	return nil
}

// resolveGlobalValue resolves sym to its value and canonical flag for env. A
// depth-0 site hit serves its immutable snapshot only while env identity,
// name generation, and cell version still match; a version mismatch falls back
// to the cell's locked read without publishing a replacement. A miss publishes
// only live root-env cells; ancestor-owned names and site-less chunks fall back
// to the ordinary chain walk.
func (vm *VM) resolveGlobalValue(site *siteCache, env *core.Env, sym core.Symbol) (val core.Value, canonical bool, ok bool) {
	if site != nil {
		if entry := site.entry.Load(); entry != nil && entry.env == env {
			gen := env.NameGen()
			ver := entry.cell.Version()
			if entry.gen == gen && ver == entry.ver {
				return entry.val, entry.canonical, true
			}
			if ver != entry.ver {
				if v, live, canon := entry.env.ReadCell(entry.cell); live {
					return v, canon, true
				}
				v, found, canon := env.GetCanonical(sym.V)
				return v, canon, found
			}
		}
		if env == vm.globals {
			if cell, found := env.CellLocal(sym.V); found {
				v, live, canon, ver := env.ReadCellSnapshot(cell)
				if live {
					site.entry.Store(&siteEntry{env: env, gen: env.NameGen(), cell: cell, val: v, canonical: canon, ver: ver})
					return v, canon, true
				}
			}
		}
	}
	v, found, canon := env.GetCanonical(sym.V)
	return v, canon, found
}

func isNativeOpSymbol(name string) bool {
	switch name {
	case "+", "-", "*", "/", "<", ">", "<=", ">=", "=":
		return true
	}
	return false
}

func nativeSymbolToOp(name string) (Opcode, bool) {
	switch name {
	case "+":
		return OpAdd, true
	case "-":
		return OpSub, true
	case "*":
		return OpMul, true
	case "/":
		return OpDiv, true
	case "<":
		return OpLt, true
	case ">":
		return OpGt, true
	case "<=":
		return OpLe, true
	case ">=":
		return OpGe, true
	case "=":
		return OpEq, true
	}
	return 0, false
}

// opToNativeSymbol returns the symbol name for op, or "" if op is not a
// native arithmetic/comparison opcode. Inverse of nativeSymbolToOp.
func opToNativeSymbol(op Opcode) (string, bool) {
	switch op {
	case OpAdd:
		return "+", true
	case OpSub:
		return "-", true
	case OpMul:
		return "*", true
	case OpDiv:
		return "/", true
	case OpLt:
		return "<", true
	case OpGt:
		return ">", true
	case OpLe:
		return "<=", true
	case OpGe:
		return ">=", true
	case OpEq:
		return "=", true
	}
	return "", false
}

func execNative(eval core.Evaluator, op Opcode, args []core.Value, env *core.Env) (core.Value, error) {
	switch op {
	case OpAdd:
		return nativeAdd(args)
	case OpSub:
		return nativeSub(args)
	case OpMul:
		return nativeMul(args)
	case OpDiv:
		return nativeDiv(args)
	case OpLt:
		return nativeOrder("<", args, func(c int) bool { return c < 0 })
	case OpGt:
		return nativeOrder(">", args, func(c int) bool { return c > 0 })
	case OpLe:
		return nativeOrder("<=", args, func(c int) bool { return c <= 0 })
	case OpGe:
		return nativeOrder(">=", args, func(c int) bool { return c >= 0 })
	case OpEq:
		return nativeEq(args)
	default:
		return nil, &core.LispicoError{Code: "BytecodeError", Message: fmt.Sprintf("execNative: unknown op %v", op)}
	}
}

func nativeAdd(args []core.Value) (core.Value, error) {
	var intSum int64
	var floatSum float64
	hasFloat := false
	for _, arg := range args {
		switch v := arg.(type) {
		case core.Int:
			if hasFloat {
				floatSum += float64(v.V)
			} else {
				intSum += v.V
			}
		case core.Float:
			if !hasFloat {
				floatSum = float64(intSum)
				hasFloat = true
			}
			floatSum += v.V
		default:
			return nil, fmt.Errorf("+: expected number, got %T", arg)
		}
	}
	if hasFloat {
		return core.Float{V: floatSum}, nil
	}
	return core.BoxInt(intSum), nil
}

func nativeSub(args []core.Value) (core.Value, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("-: requires at least 1 argument")
	}
	var intR int64
	var floatR float64
	hasFloat := false
	switch v := args[0].(type) {
	case core.Int:
		intR = v.V
	case core.Float:
		floatR = v.V
		hasFloat = true
	default:
		return nil, fmt.Errorf("-: expected number, got %T", args[0])
	}
	if len(args) == 1 {
		if hasFloat {
			return core.Float{V: -floatR}, nil
		}
		return core.BoxInt(-intR), nil
	}
	for _, arg := range args[1:] {
		switch v := arg.(type) {
		case core.Int:
			if hasFloat {
				floatR -= float64(v.V)
			} else {
				intR -= v.V
			}
		case core.Float:
			if !hasFloat {
				floatR = float64(intR)
				hasFloat = true
			}
			floatR -= v.V
		default:
			return nil, fmt.Errorf("-: expected number, got %T", arg)
		}
	}
	if hasFloat {
		return core.Float{V: floatR}, nil
	}
	return core.BoxInt(intR), nil
}

func nativeMul(args []core.Value) (core.Value, error) {
	var intP int64 = 1
	var floatP float64 = 1
	hasFloat := false
	for _, arg := range args {
		switch v := arg.(type) {
		case core.Int:
			if hasFloat {
				floatP *= float64(v.V)
			} else {
				intP *= v.V
			}
		case core.Float:
			if !hasFloat {
				floatP = float64(intP)
				hasFloat = true
			}
			floatP *= v.V
		default:
			return nil, fmt.Errorf("*: expected number, got %T", arg)
		}
	}
	if hasFloat {
		return core.Float{V: floatP}, nil
	}
	return core.BoxInt(intP), nil
}

func nativeDiv(args []core.Value) (core.Value, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("/: requires at least 2 arguments")
	}
	var dividend float64
	switch v := args[0].(type) {
	case core.Int:
		dividend = float64(v.V)
	case core.Float:
		dividend = v.V
	default:
		return nil, fmt.Errorf("/: expected number, got %T", args[0])
	}
	for _, arg := range args[1:] {
		var divisor float64
		switch v := arg.(type) {
		case core.Int:
			if v.V == 0 {
				return nil, fmt.Errorf("/: division by zero")
			}
			divisor = float64(v.V)
		case core.Float:
			if v.V == 0 {
				return nil, fmt.Errorf("/: division by zero")
			}
			divisor = v.V
		default:
			return nil, fmt.Errorf("/: expected number, got %T", arg)
		}
		dividend /= divisor
	}
	return core.Float{V: dividend}, nil
}

func nativeOrder(name string, args []core.Value, ok func(int) bool) (core.Value, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("%s: requires at least 1 argument", name)
	}
	if _, err := toFloat(name, args[0]); err != nil {
		return nil, err
	}
	for i := 1; i < len(args); i++ {
		cmp, err := numCmp(name, args[i-1], args[i])
		if err != nil {
			return nil, err
		}
		if !ok(cmp) {
			return core.BoxBool(false), nil
		}
	}
	return core.BoxBool(true), nil
}

func nativeEq(args []core.Value) (core.Value, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("=: requires at least 1 argument")
	}
	for _, arg := range args[1:] {
		if !args[0].Equals(arg) {
			return core.BoxBool(false), nil
		}
	}
	return core.BoxBool(true), nil
}

func numCmp(name string, a, b core.Value) (int, error) {
	ai, aInt := a.(core.Int)
	bi, bInt := b.(core.Int)
	if aInt && bInt {
		switch {
		case ai.V < bi.V:
			return -1, nil
		case ai.V > bi.V:
			return 1, nil
		}
		return 0, nil
	}
	af, err := toFloat(name, a)
	if err != nil {
		return 0, err
	}
	bf, err := toFloat(name, b)
	if err != nil {
		return 0, err
	}
	switch {
	case af < bf:
		return -1, nil
	case af > bf:
		return 1, nil
	}
	return 0, nil
}

func toFloat(name string, v core.Value) (float64, error) {
	switch n := v.(type) {
	case core.Int:
		return float64(n.V), nil
	case core.Float:
		return n.V, nil
	default:
		return 0, fmt.Errorf("%s: expected number, got %T", name, v)
	}
}

// coerceThrow mirrors the tree-walker's throw/catch coercion (evalThrow in
// formatted with %v, so catch binds the same core.String under both evaluators.
func coerceThrow(value core.Value) core.String {
	if s, ok := value.(core.String); ok {
		return core.String{V: s.V}
	}
	return core.String{V: fmt.Sprintf("%v", value)}
}

// throw unwinds the VM to the nearest active exception handler and leaves
// value on the handler frame's stack. It returns true if a handler was found.
func (vm *VM) throw(value core.Value) bool {
	for len(vm.handlers) > 0 && vm.handlers[len(vm.handlers)-1].frameDepth > len(vm.frames) {
		vm.handlers = vm.handlers[:len(vm.handlers)-1]
	}
	if len(vm.handlers) == 0 {
		return false
	}
	h := vm.handlers[len(vm.handlers)-1]
	vm.handlers = vm.handlers[:len(vm.handlers)-1]
	vm.structDepth.Store(h.structDepth)
	for len(vm.frames) > h.frameDepth {
		f := &vm.frames[len(vm.frames)-1]
		if f.isClosure && vm.depth > 0 {
			vm.depth--
		}
		vm.frames = vm.frames[:len(vm.frames)-1]
	}
	if len(vm.frames) == 0 {
		return false
	}
	vm.stack = vm.stack[:h.stackDepth]
	vm.freezeStack = vm.freezeStack[:h.freezeDepth]
	vm.push(value)
	frame := &vm.frames[len(vm.frames)-1]
	frame.ip = h.addr
	return true
}

func (vm *VM) call(ctx context.Context, argc int, tail bool) error {
	if argc < 0 || argc+1 > len(vm.stack) {
		return &core.LispicoError{Code: "BytecodeError", Message: fmt.Sprintf("call: %d args exceeds stack", argc)}
	}
	fn := vm.stack[len(vm.stack)-argc-1]
	args := vm.stack[len(vm.stack)-argc:]

	switch f := fn.(type) {
	case core.GoFunc:
		eval := vm.eval
		if eval == nil {
			eval = core.NewEvaluator()
		}
		frameEnv := vm.globals
		if len(vm.frames) > 0 {
			frameEnv = vm.frames[len(vm.frames)-1].env
		}
		reCtx, err := vm.reentrantCtx(ctx)
		if err != nil {
			return err
		}
		if c, d := vm.pushReentrantDepth(reCtx); c != nil {
			defer c.Add(-d)
		}

		if err := vm.chargeReductions(1); err != nil {
			return err
		}
		result, err := f.Fn(reCtx, eval, args, frameEnv)
		vm.syncMeterFromReentry()
		if err != nil {
			return err
		}
		if err := vm.chargeValue(result); err != nil {
			return err
		}
		vm.stack = vm.stack[:len(vm.stack)-argc-1]
		vm.push(result)

	case core.Lambda:
		eval := vm.eval
		if eval == nil {
			eval = core.NewEvaluator()
		}
		frameEnv := vm.globals
		if len(vm.frames) > 0 {
			frameEnv = vm.frames[len(vm.frames)-1].env
		}
		reCtx, err := vm.reentrantCtx(ctx)
		if err != nil {
			return err
		}
		if c, d := vm.pushReentrantDepth(reCtx); c != nil {
			defer c.Add(-d)
		}
		result, err := eval.Apply(reCtx, f, args, frameEnv)
		vm.syncMeterFromReentry()
		if err != nil {
			return err
		}
		vm.stack = vm.stack[:len(vm.stack)-argc-1]
		vm.push(result)

	case core.Keyword:
		if argc != 1 {
			return keywordArityError(argc)
		}
		var result core.Value = core.Nil{}
		if m, ok := args[0].(*core.HashMap); ok {
			if v, _ := m.Get(f); v != nil {
				result = v
			}
		}
		vm.stack = vm.stack[:len(vm.stack)-argc-1]
		vm.push(result)

	case *Closure:
		if f.Chunk.Variadic {
			if argc < f.Chunk.Arity {
				return core.NewArityError(f.Chunk.Arity, argc)
			}
		} else {
			if argc != f.Chunk.Arity {
				return core.NewArityError(f.Chunk.Arity, argc)
			}
		}
		sharedDepth := int64(0)
		if vm.callDepth != nil {
			sharedDepth = vm.callDepth.Load()
		}
		if vm.maxDepth > 0 && (int64(vm.depth) >= int64(vm.maxDepth) || (sharedDepth > 0 && sharedDepth+int64(vm.depth) >= int64(vm.maxDepth))) {
			return &core.LispicoError{Code: "EvalError", Message: "maximum call depth exceeded"}
		}
		vm.depth++

		if tail && len(vm.frames) > 0 {
			vm.depth--
			frame := &vm.frames[len(vm.frames)-1]
			target := frame.base
			if f.Chunk.Variadic {
				fixed := f.Chunk.Arity
				rest := core.List{Items: append([]core.Value(nil), args[fixed:]...)}
				copy(vm.stack[target:], args[:fixed])
				vm.stack[target+fixed] = rest
				vm.stack = vm.stack[:target+fixed+1]
			} else {
				copy(vm.stack[target:], args)
				vm.stack = vm.stack[:target+len(args)]
			}
			boxCaptured(f.Chunk, vm.stack[target:])
			frame.chunk = f.Chunk
			frame.ip = 0
			frame.env = f.globals
			frame.caps = f.caps
			frame.isClosure = true
			vm.growStack(target, f.Chunk.MaxStack)
		} else {
			base := len(vm.stack) - argc - 1
			if f.Chunk.Variadic {
				fixed := f.Chunk.Arity
				rest := core.List{Items: append([]core.Value(nil), args[fixed:]...)}
				for i := range fixed {
					vm.stack[base+i] = args[i]
				}
				vm.stack[base+fixed] = rest
				vm.stack = vm.stack[:base+fixed+1]
			} else {
				copy(vm.stack[base:], args)
				vm.stack = vm.stack[:base+argc]
			}
			boxCaptured(f.Chunk, vm.stack[base:])
			vm.frames = append(vm.frames, Frame{
				chunk:     f.Chunk,
				ip:        0,
				base:      base,
				env:       f.globals,
				caps:      f.caps,
				isClosure: true,
			})
			vm.growStack(base, f.Chunk.MaxStack)
		}

	default:
		return core.NewTypeError("callable", fn)
	}
	return nil
}

// boxCaptured replaces each captured parameter's value in slots with a shared
// cell, so the frame and every closure over it read and write one storage
// location. Captured slots beyond the parameters are boxed later, at their
// OpBindCell binding site.
func boxCaptured(chunk *Chunk, slots []core.Value) {
	for i, captured := range chunk.Captured {
		if i >= len(slots) {
			break
		}
		if captured {
			slots[i] = &cellBox{v: slots[i]}
		}
	}
}
