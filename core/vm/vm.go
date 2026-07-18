// Package vm implements the stack-based bytecode virtual machine that
// executes chunks produced by core/compiler.
package vm

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/victorzhuk/go-lispico/core"
)

// Closure is a compiled function: a Chunk paired with the lexical
// environment it closed over. It implements core.Value.
type Closure struct {
	Chunk *Chunk
	Env   *core.Env
}

// NewClosure creates a Closure over chunk in env.
func NewClosure(chunk *Chunk, env *core.Env) *Closure {
	return &Closure{Chunk: chunk, Env: env}
}

// Type implements core.Value.
func (c *Closure) Type() core.Keyword { return core.Keyword{V: "closure"} }

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
	structDepth int64
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
	maxStructuralDepth int
	// nativeOp freezes, per operand-stack slot, the native opcode a canonical
	// operator was resolved to when OpGetGlobal pushed it — matching the
	// tree-walker's resolve-head-before-args order. dispatchNativeOp consumes
	// it instead of re-reading the cell after arguments (which could have
	// rebound the operator). A zero entry (OpConst, never a native op) means
	// "not a canonical native operator"; push zeroes each slot it writes.
	nativeOp []Opcode
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

// New creates a VM using globals as the root environment.
func New(globals *core.Env, opts ...VMOption) *VM {
	v := &VM{
		stack:       make([]core.Value, 0, 256),
		frames:      make([]Frame, 0, 64),
		globals:     globals,
		eval:        core.NewEvaluator(),
		structDepth: &atomic.Int64{},
	}
	for _, opt := range opts {
		opt(v)
	}
	return v
}

func (vm *VM) stackSize() int  { return len(vm.stack) }
func (vm *VM) frameCount() int { return len(vm.frames) }
func (vm *VM) reset() {
	vm.stack = vm.stack[:0]
	vm.frames = vm.frames[:0]
	vm.handlers = vm.handlers[:0]
	vm.depth = 0
	vm.structDepth.Store(0)
	vm.nativeOp = vm.nativeOp[:0]
}

// Reset clears the VM state (stacks, frames, handlers, depth) so the
// instance can be reused for a new evaluation. It does not change the
// VM's configuration (globals, max depth, evaluator).
func (vm *VM) Reset() {
	vm.stack = vm.stack[:0]
	vm.frames = vm.frames[:0]
	vm.handlers = vm.handlers[:0]
	vm.depth = 0
	vm.structDepth.Store(0)
	vm.nativeOp = vm.nativeOp[:0]
}

// SetGlobals replaces the VM's globals (root environment) pointer.
// Used when reusing a pooled VM for a different environment.
func (vm *VM) SetGlobals(env *core.Env) {
	vm.globals = env
}

func (vm *VM) push(v core.Value) {
	vm.stack = append(vm.stack, v)
	slot := len(vm.stack) - 1
	if slot < len(vm.nativeOp) {
		vm.nativeOp[slot] = 0
	}
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
func (vm *VM) reloadFrame() (chunk *Chunk, code []Instruction, ip, base int, env *core.Env, truthy func(core.Value) bool) {
	frame := &vm.frames[len(vm.frames)-1]
	truthy = core.IsTruthy
	if frame.chunk.Truthiness != nil {
		truthy = frame.chunk.Truthiness
	}
	return frame.chunk, frame.chunk.Code, frame.ip, frame.base, frame.env, truthy
}

// freezeNativeOp records that the operator at slot resolved to a canonical
// native op, so its later dispatch takes the native fast path regardless of an
// intervening rebind. Called by OpGetGlobal after push, overriding push's zero.
func (vm *VM) freezeNativeOp(slot int, op Opcode) {
	if slot >= len(vm.nativeOp) {
		vm.nativeOp = append(vm.nativeOp, make([]Opcode, slot+1-len(vm.nativeOp))...)
	}
	vm.nativeOp[slot] = op
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
	fresh := New(env, WithMaxDepth(v.maxDepth), WithEvaluator(v.eval), WithMaxStructuralDepth(v.maxStructuralDepth))
	fresh.structDepth = v.structDepth
	return fresh.apply(ctx, fn, args, env)
}

// ApplyPooled calls fn with args on this VM instance (no fresh VM allocation).
// The caller MUST have called Reset (or obtained this VM from a pool that
// resets) before calling ApplyPooled, and MUST NOT reuse this VM concurrently.
// For fresh-isolation semantics use Apply instead.
func (v *VM) ApplyPooled(ctx context.Context, fn core.Value, args []core.Value, env *core.Env) (core.Value, error) {
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
		// Build a tiny wrapper chunk: push closure, push args, call, return.
		wrapper := &Chunk{
			Name:       "<apply>",
			Constants:  make([]core.Value, 0, len(args)+1),
			Code:       make([]Instruction, 0, len(args)+3),
			LocalNames: []string{},
			Arity:      0,
		}
		wrapper.Constants = append(wrapper.Constants, f)
		wrapper.Code = append(wrapper.Code, Encode(OpConst, 0))
		for i, arg := range args {
			wrapper.Constants = append(wrapper.Constants, arg)
			wrapper.Code = append(wrapper.Code, Encode(OpConst, i+1))
		}
		wrapper.Code = append(wrapper.Code, Encode(OpCall, len(args)), Encode(OpReturn, 0))
		wrapper.MaxStack = len(args) + 1
		if err := wrapper.Validate(); err != nil {
			return nil, err
		}
		return vm.Run(ctx, wrapper)
	case core.GoFunc:
		eval := vm.eval
		if eval == nil {
			eval = core.NewEvaluator()
		}
		return f.Fn(ctx, eval, args, env)
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

// Run pushes a new frame for chunk and executes it to completion, returning
// the result of its top-level OpReturn.
func (vm *VM) Run(ctx context.Context, chunk *Chunk) (core.Value, error) {
	base := len(vm.stack)
	vm.frames = append(vm.frames, Frame{chunk: chunk, base: base, env: vm.globals})
	vm.growStack(base, chunk.MaxStack)

	code := chunk.Code
	ip := 0
	env := vm.globals
	truthy := core.IsTruthy
	if chunk.Truthiness != nil {
		truthy = chunk.Truthiness
	}

	for {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("vm: %w", err)
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
			if idx < len(chunk.LocalNames) {
				if chunk.FullEnv || (idx < len(chunk.Captured) && chunk.Captured[idx]) {
					env.Set(chunk.LocalNames[idx], top)
				}
			}

		case OpGetGlobal:
			sym := chunk.Constants[instr.A()].(core.Symbol)
			val, canon, ok := vm.resolveGlobalValue(chunk.site(ip-1), env, sym)
			if !ok {
				return nil, core.NewUndefinedError(sym.V)
			}
			vm.push(val)
			// Native operators freeze their canonical eligibility here, at
			// head-resolution time, so a rebind during argument evaluation
			// cannot flip the fast-path decision (tree-walker parity).
			if canon && isNativeOpSymbol(sym.V) {
				if op, isOp := nativeSymbolToOp(sym.V); isOp {
					vm.freezeNativeOp(len(vm.stack)-1, op)
				}
			}

		case OpSetGlobal:
			sym := chunk.Constants[instr.A()].(core.Symbol)
			top, err := vm.peek()
			if err != nil {
				return nil, err
			}
			env.Set(sym.V, top)

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
			owner.Set(sym.V, top)

		case OpGetFunc:
			sym := chunk.Constants[instr.A()].(core.Symbol)
			v, found := env.GetFunc(sym.V)
			if !found {
				return nil, core.NewUndefinedError(sym.V)
			}
			vm.push(v)

		case OpSetFunc:
			sym := chunk.Constants[instr.A()].(core.Symbol)
			top, err := vm.peek()
			if err != nil {
				return nil, err
			}
			env.SetFunc(sym.V, top)

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
				if !vm.throw(core.String{V: err.Error()}) {
					return nil, err
				}
			}
			chunk, code, ip, base, env, truthy = vm.reloadFrame()

		case OpTailCall:
			vm.frames[len(vm.frames)-1].ip = ip
			if err := vm.call(ctx, instr.A(), true); err != nil {
				if !vm.throw(core.String{V: err.Error()}) {
					return nil, err
				}
			}
			chunk, code, ip, base, env, truthy = vm.reloadFrame()

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
				return result, nil
			}
			vm.push(result)
			chunk, code, ip, base, env, truthy = vm.reloadFrame()

		case OpMakeList:
			n := instr.A()
			if n < 0 || n > len(vm.stack) {
				return nil, &core.LispicoError{Code: "BytecodeError", Message: fmt.Sprintf("make-list: %d items exceeds stack", n)}
			}
			items := make([]core.Value, n)
			copy(items, vm.stack[len(vm.stack)-n:])
			vm.stack = vm.stack[:len(vm.stack)-n]
			vm.push(core.List{Items: items})

		case OpMakeVector:
			n := instr.A()
			if n < 0 || n > len(vm.stack) {
				return nil, &core.LispicoError{Code: "BytecodeError", Message: fmt.Sprintf("make-vector: %d items exceeds stack", n)}
			}
			items := make([]core.Value, n)
			copy(items, vm.stack[len(vm.stack)-n:])
			vm.stack = vm.stack[:len(vm.stack)-n]
			vm.push(core.Vector{Items: items})

		case OpMakeMap:
			n := instr.A() * 2
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
			vm.push(NewClosure(chunk.SubChunks[instr.A()], env))

		case OpDup:
			top, err := vm.peek()
			if err != nil {
				return nil, err
			}
			vm.push(top)

		case OpLoop:
			ip = instr.A()

		case OpSetupTry:
			vm.handlers = append(vm.handlers, handler{addr: instr.A(), frameDepth: len(vm.frames), stackDepth: len(vm.stack), structDepth: vm.structDepth.Load()})

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
			chunk, code, ip, base, env, truthy = vm.reloadFrame()

		case OpAdd, OpSub, OpMul, OpDiv, OpLt, OpGt, OpLe, OpGe, OpEq:
			vm.frames[len(vm.frames)-1].ip = ip
			if err := vm.dispatchNativeOp(ctx, env, instr.Op(), instr.A()); err != nil {
				if !vm.throw(core.String{V: err.Error()}) {
					return nil, err
				}
			}
			chunk, code, ip, base, env, truthy = vm.reloadFrame()

		}
	}
}

// dispatchNativeOp executes a native arithmetic/comparison opcode. The stack
// holds [operator, arg1, ...] with the operator at fnIdx. The fast path is
// taken only when OpGetGlobal froze this operator as a canonical native op for
// this opcode (nativeOp[fnIdx]) — the decision made at head-resolution time,
// before arguments ran, matching the tree-walker. Otherwise it falls back to
// calling the operator value already on the stack.
func (vm *VM) dispatchNativeOp(ctx context.Context, env *core.Env, op Opcode, argc int) error {
	if argc < 0 || argc+1 > len(vm.stack) {
		return &core.LispicoError{Code: "BytecodeError", Message: fmt.Sprintf("native: argc=%d exceeds stack", argc)}
	}

	fnIdx := len(vm.stack) - argc - 1
	if fnIdx >= 0 && fnIdx < len(vm.nativeOp) && vm.nativeOp[fnIdx] == op {
		vm.nativeOp[fnIdx] = 0
		return vm.execNativeFast(op, argc, env)
	}

	return vm.call(ctx, argc, false)
}

// execNativeFast runs op over the argc values already on top of the stack,
// replacing them and the operator below them with the result.
func (vm *VM) execNativeFast(op Opcode, argc int, env *core.Env) error {
	fnIdx := len(vm.stack) - argc - 1
	args := vm.stack[len(vm.stack)-argc:]
	eval := vm.eval
	if eval == nil {
		eval = core.NewEvaluator()
	}
	result, err := execNative(eval, op, args, env)
	if err != nil {
		return err
	}
	vm.stack = vm.stack[:fnIdx]
	vm.push(result)
	return nil
}

// resolveGlobalValue resolves sym to its value and canonical flag for env,
// reading them coherently under the owning env's read lock. A cached depth-0
// site (env owns the name) skips the chain walk and reads through the cached
// cell pointer; a miss publishes a fresh entry; an ancestor-owned name or a
// site-less (hand-built) chunk falls back to an uncached chain walk. Only
// depth-0 resolutions are cached — an ancestor cell could later be shadowed
// by a new local bind of the same name without the site observing it. ok is
// false when sym is unbound.
func (vm *VM) resolveGlobalValue(site *siteCache, env *core.Env, sym core.Symbol) (val core.Value, canonical bool, ok bool) {
	if site != nil {
		if entry := site.entry.Load(); entry != nil && entry.env == env && entry.gen == env.NameGen() {
			if v, live, canon := entry.env.ReadCell(entry.cell); live {
				return v, canon, true
			}
		}
		// Publish only for the stable root env: a per-call child env is fresh
		// every invocation, so caching it never hits again and just churns a
		// siteEntry per call. Those fall through to the uncached chain walk.
		if env == vm.globals {
			if cell, found := env.CellLocal(sym.V); found {
				site.entry.Store(&siteEntry{env: env, gen: env.NameGen(), cell: cell})
				if v, live, canon := env.ReadCell(cell); live {
					return v, canon, true
				}
				return nil, false, false
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
		result, err := f.Fn(ctx, eval, args, frameEnv)
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
		if vm.maxDepth > 0 && vm.depth >= vm.maxDepth {
			return &core.LispicoError{Code: "EvalError", Message: fmt.Sprintf("maximum call depth %d exceeded", vm.maxDepth)}
		}
		vm.depth++

		needsEnv := needsCallEnv(f.Chunk)

		var callEnv *core.Env
		if needsEnv {
			callEnv = core.NewEnv(f.Env)
		}

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
				if needsEnv {
					for i := range fixed {
						callEnv.Set(f.Chunk.LocalNames[i], args[i])
					}
					callEnv.Set(f.Chunk.LocalNames[fixed], rest)
				}
			} else {
				copy(vm.stack[target:], args)
				vm.stack = vm.stack[:target+len(args)]
				if needsEnv {
					for i := range min(len(args), len(f.Chunk.LocalNames)) {
						callEnv.Set(f.Chunk.LocalNames[i], args[i])
					}
				}
			}
			frame.chunk = f.Chunk
			frame.ip = 0
			if needsEnv {
				frame.env = callEnv
			} else {
				frame.env = f.Env
			}
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
				if needsEnv {
					for i := range fixed {
						callEnv.Set(f.Chunk.LocalNames[i], args[i])
					}
					callEnv.Set(f.Chunk.LocalNames[fixed], rest)
				}
			} else {
				copy(vm.stack[base:], args)
				vm.stack = vm.stack[:base+argc]
				if needsEnv {
					for i := range min(len(args), len(f.Chunk.LocalNames)) {
						callEnv.Set(f.Chunk.LocalNames[i], args[i])
					}
				}
			}
			frameEnv := f.Env
			if needsEnv {
				frameEnv = callEnv
			}
			vm.frames = append(vm.frames, Frame{
				chunk:     f.Chunk,
				ip:        0,
				base:      base,
				env:       frameEnv,
				isClosure: true,
			})
			vm.growStack(base, f.Chunk.MaxStack)
		}

	default:
		return core.NewTypeError("callable", fn)
	}
	return nil
}

// needsCallEnv returns true if the chunk requires per-call Env allocation —
// either because capture analysis was inconclusive (FullEnv) or because at
// least one local is captured. When false, the frame reuses the closure's
// parent env and allocates no Env for local bindings.
func needsCallEnv(chunk *Chunk) bool {
	return chunk.FullEnv || !allLocalsUncaptured(chunk)
}

// allLocalsUncaptured returns true when no local slot in the chunk is marked
// as captured and FullEnv is false.
func allLocalsUncaptured(chunk *Chunk) bool {
	if chunk.FullEnv {
		return false
	}
	for _, c := range chunk.Captured {
		if c {
			return false
		}
	}
	return true
}
