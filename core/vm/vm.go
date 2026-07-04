// Package vm implements the stack-based bytecode virtual machine that
// executes chunks produced by core/compiler.
package vm

import (
	"context"
	"fmt"

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
	addr       int
	frameDepth int
	stackDepth int
}

// VM is a stack-based bytecode virtual machine.
// It is not safe for concurrent use on the same instance; callers that need
// concurrency-safe evaluation should use a fresh VM per evaluation.
type VM struct {
	stack    []core.Value
	frames   []Frame
	handlers []handler
	globals  *core.Env
	maxDepth int
	depth    int
	eval     core.Evaluator
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

// New creates a VM using globals as the root environment.
func New(globals *core.Env, opts ...VMOption) *VM {
	v := &VM{
		stack:   make([]core.Value, 0, 256),
		frames:  make([]Frame, 0, 64),
		globals: globals,
		eval:    core.NewEvaluator(),
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
}

func (vm *VM) push(v core.Value) { vm.stack = append(vm.stack, v) }
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
	fresh := New(env, WithMaxDepth(v.maxDepth), WithEvaluator(v.eval))
	return fresh.apply(ctx, fn, args, env)
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
		return vm.Run(ctx, wrapper)
	case core.GoFunc:
		eval := vm.eval
		if eval == nil {
			eval = core.NewEvaluator()
		}
		return f.Fn(ctx, eval, args, env)
	default:
		return nil, core.NewTypeError("callable", fn)
	}
}

// Run pushes a new frame for chunk and executes it to completion, returning
// the result of its top-level OpReturn.
func (vm *VM) Run(ctx context.Context, chunk *Chunk) (core.Value, error) {
	vm.frames = append(vm.frames, Frame{chunk: chunk, base: len(vm.stack), env: vm.globals})

	for {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("vm: %w", err)
		}

		frame := &vm.frames[len(vm.frames)-1]
		if frame.ip < 0 || frame.ip >= len(frame.chunk.Code) {
			return nil, &core.LispicoError{Code: "BytecodeError", Message: "instruction pointer out of range"}
		}
		instr := frame.chunk.Code[frame.ip]
		frame.ip++

		switch instr.Op() {
		case OpNil:
			vm.push(core.Nil{})

		case OpTrue:
			vm.push(core.Bool{V: true})

		case OpFalse:
			vm.push(core.Bool{V: false})

		case OpConst:
			v, err := frame.chunk.GetConstant(instr.A())
			if err != nil {
				return nil, &core.LispicoError{Code: "BytecodeError", Message: err.Error()}
			}
			vm.push(v)

		case OpGetLocal:
			slot := frame.base + instr.A()
			if slot < 0 || slot >= len(vm.stack) {
				return nil, &core.LispicoError{Code: "BytecodeError", Message: fmt.Sprintf("local slot %d out of range", instr.A())}
			}
			vm.push(vm.stack[slot])

		case OpSetLocal:
			idx := instr.A()
			slot := frame.base + idx
			if slot < 0 || slot >= len(vm.stack) {
				return nil, &core.LispicoError{Code: "BytecodeError", Message: fmt.Sprintf("local slot %d out of range", idx)}
			}
			top, err := vm.peek()
			if err != nil {
				return nil, err
			}
			vm.stack[slot] = top
			if idx < len(frame.chunk.LocalNames) {
				frame.env.Set(frame.chunk.LocalNames[idx], top)
			}

		case OpGetGlobal:
			sym, err := frame.chunk.GetSymbolConstant(instr.A())
			if err != nil {
				return nil, &core.LispicoError{Code: "BytecodeError", Message: err.Error()}
			}
			v, ok := frame.env.Get(sym.V)
			if !ok {
				return nil, core.NewUndefinedError(sym.V)
			}
			vm.push(v)

		case OpSetGlobal:
			sym, err := frame.chunk.GetSymbolConstant(instr.A())
			if err != nil {
				return nil, &core.LispicoError{Code: "BytecodeError", Message: err.Error()}
			}
			top, err := vm.peek()
			if err != nil {
				return nil, err
			}
			frame.env.Set(sym.V, top)

		case OpPop:
			if _, err := vm.pop(); err != nil {
				return nil, err
			}

		case OpJump:
			frame.ip += instr.A()

		case OpJumpIfFalse:
			top, err := vm.pop()
			if err != nil {
				return nil, err
			}
			if !core.IsTruthy(top) {
				frame.ip += instr.A()
			}

		case OpCall:
			if err := vm.call(ctx, instr.A(), false); err != nil {
				if !vm.throw(core.String{V: err.Error()}) {
					return nil, err
				}
			}

		case OpTailCall:
			if err := vm.call(ctx, instr.A(), true); err != nil {
				if !vm.throw(core.String{V: err.Error()}) {
					return nil, err
				}
			}

		case OpReturn:
			result, err := vm.pop()
			if err != nil {
				return nil, err
			}
			if frame.isClosure && vm.depth > 0 {
				vm.depth--
			}
			vm.frames = vm.frames[:len(vm.frames)-1]
			vm.stack = vm.stack[:frame.base]
			for len(vm.handlers) > 0 && vm.handlers[len(vm.handlers)-1].frameDepth > len(vm.frames) {
				vm.handlers = vm.handlers[:len(vm.handlers)-1]
			}
			if len(vm.frames) == 0 {
				return result, nil
			}
			vm.push(result)

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

		case OpClosure:
			sub, err := frame.chunk.GetSubChunk(instr.A())
			if err != nil {
				return nil, &core.LispicoError{Code: "BytecodeError", Message: err.Error()}
			}
			vm.push(NewClosure(sub, frame.env))

		case OpDup:
			top, err := vm.peek()
			if err != nil {
				return nil, err
			}
			vm.push(top)

		case OpLoop:
			frame.ip = instr.A()

		case OpSetupTry:
			vm.handlers = append(vm.handlers, handler{addr: instr.A(), frameDepth: len(vm.frames), stackDepth: len(vm.stack)})

		case OpPopTry:
			if len(vm.handlers) > 0 {
				vm.handlers = vm.handlers[:len(vm.handlers)-1]
			}

		case OpThrow:
			value, err := vm.pop()
			if err != nil {
				return nil, err
			}
			if !vm.throw(coerceThrow(value)) {
				return nil, core.NewTypeError("handler", core.Nil{})
			}
		}
	}
}

// coerceThrow mirrors the tree-walker's throw/catch coercion (evalThrow in
// core/eval.go): a thrown String keeps its raw text, anything else is
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
		result, err := f.Fn(ctx, eval, args, vm.globals)
		if err != nil {
			return err
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
			return fmt.Errorf("maximum call depth %d exceeded", vm.maxDepth)
		}
		vm.depth++
		callEnv := core.NewEnv(f.Env)
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
				for i := range fixed {
					callEnv.Set(f.Chunk.LocalNames[i], args[i])
				}
				callEnv.Set(f.Chunk.LocalNames[fixed], rest)
			} else {
				copy(vm.stack[target:], args)
				vm.stack = vm.stack[:target+len(args)]
				for i := range min(len(args), len(f.Chunk.LocalNames)) {
					callEnv.Set(f.Chunk.LocalNames[i], args[i])
				}
			}
			frame.chunk = f.Chunk
			frame.ip = 0
			frame.env = callEnv
			frame.isClosure = true
		} else {
			base := len(vm.stack) - argc - 1
			if f.Chunk.Variadic {
				fixed := f.Chunk.Arity
				rest := core.List{Items: append([]core.Value(nil), args[fixed:]...)}
				for i := range fixed {
					vm.stack[base+i] = args[i]
					callEnv.Set(f.Chunk.LocalNames[i], args[i])
				}
				vm.stack[base+fixed] = rest
				callEnv.Set(f.Chunk.LocalNames[fixed], rest)
				vm.stack = vm.stack[:base+fixed+1]
			} else {
				copy(vm.stack[base:], args)
				for i := range min(len(args), len(f.Chunk.LocalNames)) {
					callEnv.Set(f.Chunk.LocalNames[i], args[i])
				}
				vm.stack = vm.stack[:base+argc]
			}
			vm.frames = append(vm.frames, Frame{
				chunk:     f.Chunk,
				ip:        0,
				base:      base,
				env:       callEnv,
				isClosure: true,
			})
		}

	default:
		return core.NewTypeError("callable", fn)
	}
	return nil
}
