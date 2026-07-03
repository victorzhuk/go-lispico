// Package vm implements the stack-based bytecode virtual machine that
// executes chunks produced by core/compiler.
package vm

import (
	"context"
	"crypto/sha256"
	"encoding/gob"
	"fmt"
	"os"
	"path/filepath"

	"github.com/victorzhuk/go-lispico/core"
)

func init() {
	gob.Register(&Chunk{})
	gob.Register(&cacheEntry{})
	gob.Register(core.Nil{})
	gob.Register(core.Bool{})
	gob.Register(core.Int{})
	gob.Register(core.Float{})
	gob.Register(core.String{})
	gob.Register(core.Symbol{})
	gob.Register(core.Keyword{})
	gob.Register(core.List{})
	gob.Register(core.Vector{})
	gob.Register(&core.HashMap{})
	gob.Register([]core.Value{})
	gob.Register([]*Chunk{})
}

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

const cacheVersion = 2

type cacheEntry struct {
	Version int
	Chunks  []*Chunk
}

// BytecodeCache persists compiled Chunks to disk, keyed by source path and
// content hash, so repeated runs can skip recompilation.
type BytecodeCache struct {
	dir string
}

// NewBytecodeCache creates a BytecodeCache rooted at dir, creating the
// directory if it does not exist.
func NewBytecodeCache(dir string) (*BytecodeCache, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("bytecode cache mkdir %s: %w", dir, err)
	}
	return &BytecodeCache{dir: dir}, nil
}

// Dir returns the cache's root directory.
func (bc *BytecodeCache) Dir() string { return bc.dir }

func (bc *BytecodeCache) key(path string, content []byte) string {
	h := sha256.Sum256(content)
	base := filepath.Base(path)
	return filepath.Join(bc.dir, fmt.Sprintf("%s.%x.lbc", base, h[:8]))
}

// Load reads the cached chunks for path/content, if present. It returns an
// error (including one of type version mismatch) if no valid entry exists.
func (bc *BytecodeCache) Load(path string, content []byte) ([]*Chunk, error) {
	f, err := os.Open(bc.key(path, content))
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var entry cacheEntry
	if err := gob.NewDecoder(f).Decode(&entry); err != nil {
		return nil, err
	}
	if entry.Version != cacheVersion {
		return nil, fmt.Errorf("cache version mismatch: %d != %d", entry.Version, cacheVersion)
	}
	return entry.Chunks, nil
}

// Store persists chunks for path/content asynchronously. The write is
// atomic: it encodes to a temp file in the cache directory and renames it
// into place, so a crash or concurrent read never observes a partial file.
// Errors are swallowed — a failed store just means the next Load misses.
func (bc *BytecodeCache) Store(path string, content []byte, chunks []*Chunk) {
	go func() {
		key := bc.key(path, content)
		tmp, err := os.CreateTemp(bc.dir, "*.lbc.tmp")
		if err != nil {
			return
		}
		name := tmp.Name()
		if err := gob.NewEncoder(tmp).Encode(cacheEntry{Version: cacheVersion, Chunks: chunks}); err != nil {
			_ = tmp.Close()
			_ = os.Remove(name)
			return
		}
		if err := tmp.Close(); err != nil {
			_ = os.Remove(name)
			return
		}
		if err := os.Rename(name, key); err != nil {
			_ = os.Remove(name)
			return
		}
	}()
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
	cache    *BytecodeCache
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

// New creates a VM using globals as the root environment and bc as its
// bytecode cache.
func New(globals *core.Env, bc *BytecodeCache, opts ...VMOption) *VM {
	v := &VM{
		stack:   make([]core.Value, 0, 256),
		frames:  make([]Frame, 0, 64),
		globals: globals,
		cache:   bc,
		eval:    core.NewEvaluator(),
	}
	for _, opt := range opts {
		opt(v)
	}
	return v
}

// Cache returns the VM's bytecode cache, or nil if none was configured.
func (vm *VM) Cache() *BytecodeCache { return vm.cache }
func (vm *VM) stackSize() int        { return len(vm.stack) }
func (vm *VM) frameCount() int       { return len(vm.frames) }
func (vm *VM) reset() {
	vm.stack = vm.stack[:0]
	vm.frames = vm.frames[:0]
	vm.handlers = vm.handlers[:0]
	vm.depth = 0
}

func (vm *VM) push(v core.Value) { vm.stack = append(vm.stack, v) }
func (vm *VM) pop() core.Value {
	top := vm.stack[len(vm.stack)-1]
	vm.stack = vm.stack[:len(vm.stack)-1]
	return top
}
func (vm *VM) peek() core.Value { return vm.stack[len(vm.stack)-1] }

// Apply calls fn with args in a fresh isolated VM and returns the result.
// The receiver is used only for configuration (globals, cache, max depth, evaluator).
func (v *VM) Apply(ctx context.Context, fn core.Value, args []core.Value, env *core.Env) (core.Value, error) {
	fresh := New(env, v.cache, WithMaxDepth(v.maxDepth), WithEvaluator(v.eval))
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
			vm.push(vm.stack[frame.base+instr.A()])

		case OpSetLocal:
			idx := instr.A()
			vm.stack[frame.base+idx] = vm.peek()
			if idx < len(frame.chunk.LocalNames) {
				frame.env.Set(frame.chunk.LocalNames[idx], vm.peek())
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
			frame.env.Set(sym.V, vm.peek())

		case OpPop:
			vm.pop()

		case OpJump:
			frame.ip += instr.A()

		case OpJumpIfFalse:
			if !core.IsTruthy(vm.pop()) {
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
			result := vm.pop()
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
			items := make([]core.Value, n)
			copy(items, vm.stack[len(vm.stack)-n:])
			vm.stack = vm.stack[:len(vm.stack)-n]
			vm.push(core.List{Items: items})

		case OpMakeVector:
			n := instr.A()
			items := make([]core.Value, n)
			copy(items, vm.stack[len(vm.stack)-n:])
			vm.stack = vm.stack[:len(vm.stack)-n]
			vm.push(core.Vector{Items: items})

		case OpMakeMap:
			n := instr.A() * 2
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
			vm.push(vm.peek())

		case OpLoop:
			frame.ip = instr.A()

		case OpSetupTry:
			vm.handlers = append(vm.handlers, handler{addr: instr.A(), frameDepth: len(vm.frames), stackDepth: len(vm.stack)})

		case OpPopTry:
			if len(vm.handlers) > 0 {
				vm.handlers = vm.handlers[:len(vm.handlers)-1]
			}

		case OpThrow:
			value := vm.pop()
			if !vm.throw(value) {
				return nil, core.NewTypeError("handler", core.Nil{})
			}
		}
	}
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
