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

type Closure struct {
	Chunk *Chunk
	Env   *core.Env
}

func NewClosure(chunk *Chunk, env *core.Env) *Closure {
	return &Closure{Chunk: chunk, Env: env}
}

func (c *Closure) Type() core.Keyword { return core.Keyword{V: "closure"} }
func (c *Closure) String() string     { return fmt.Sprintf("#<closure %s>", c.Chunk.Name) }
func (c *Closure) Equals(o core.Value) bool {
	other, ok := o.(*Closure)
	return ok && c == other
}

const cacheVersion = 1

type cacheEntry struct {
	Version int
	Chunks  []*Chunk
}

type BytecodeCache struct {
	dir string
}

func NewBytecodeCache(dir string) (*BytecodeCache, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("bytecode cache mkdir %s: %w", dir, err)
	}
	return &BytecodeCache{dir: dir}, nil
}

func (bc *BytecodeCache) Dir() string { return bc.dir }

func (bc *BytecodeCache) key(path string, content []byte) string {
	h := sha256.Sum256(content)
	base := filepath.Base(path)
	return filepath.Join(bc.dir, fmt.Sprintf("%s.%x.lbc", base, h[:8]))
}

func (bc *BytecodeCache) Load(path string, content []byte) ([]*Chunk, error) {
	f, err := os.Open(bc.key(path, content))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var entry cacheEntry
	if err := gob.NewDecoder(f).Decode(&entry); err != nil {
		return nil, err
	}
	if entry.Version != cacheVersion {
		return nil, fmt.Errorf("cache version mismatch: %d != %d", entry.Version, cacheVersion)
	}
	return entry.Chunks, nil
}

func (bc *BytecodeCache) Store(path string, content []byte, chunks []*Chunk) {
	go func() {
		key := bc.key(path, content)
		f, err := os.Create(key)
		if err != nil {
			return
		}
		defer f.Close()
		gob.NewEncoder(f).Encode(cacheEntry{Version: cacheVersion, Chunks: chunks})
	}()
}

type FormCompiler interface {
	Compile(form core.Value) error
	Chunk() *Chunk
}

type VM struct {
	stack    []core.Value
	frames   []Frame
	globals  *core.Env
	cache    *BytecodeCache
	compiler FormCompiler
	maxDepth int
	depth    int
}

type VMOption func(*VM)

func WithCompiler(c FormCompiler) VMOption {
	return func(v *VM) { v.compiler = c }
}

func WithMaxDepth(d int) VMOption {
	return func(v *VM) { v.maxDepth = d }
}

func New(globals *core.Env, bc *BytecodeCache, opts ...VMOption) *VM {
	v := &VM{
		stack:   make([]core.Value, 0, 256),
		frames:  make([]Frame, 0, 64),
		globals: globals,
		cache:   bc,
	}
	for _, opt := range opts {
		opt(v)
	}
	return v
}

func (vm *VM) Cache() *BytecodeCache { return vm.cache }
func (vm *VM) stackSize() int        { return len(vm.stack) }
func (vm *VM) frameCount() int       { return len(vm.frames) }
func (vm *VM) reset()                { vm.stack = vm.stack[:0]; vm.frames = vm.frames[:0] }

func (vm *VM) push(v core.Value) { vm.stack = append(vm.stack, v) }
func (vm *VM) pop() core.Value {
	top := vm.stack[len(vm.stack)-1]
	vm.stack = vm.stack[:len(vm.stack)-1]
	return top
}
func (vm *VM) peek() core.Value { return vm.stack[len(vm.stack)-1] }

func (v *VM) Eval(ctx context.Context, form core.Value, env *core.Env) (core.Value, error) {
	return vmEvaluator{vm: v}.Eval(ctx, form, env)
}

func (v *VM) Apply(ctx context.Context, fn core.Value, args []core.Value, env *core.Env) (core.Value, error) {
	return vmEvaluator{vm: v}.Apply(ctx, fn, args, env)
}

type vmEvaluator struct{ vm *VM }

func (e vmEvaluator) Eval(ctx context.Context, form core.Value, env *core.Env) (core.Value, error) {
	if e.vm.compiler == nil {
		return nil, fmt.Errorf("vm eval: no compiler configured (use vm.WithCompiler)")
	}
	if err := e.vm.compiler.Compile(form); err != nil {
		return nil, fmt.Errorf("vm eval compile: %w", err)
	}
	e.vm.compiler.Chunk().Emit(OpReturn, 0)
	return e.vm.Run(ctx, e.vm.compiler.Chunk())
}

func (e vmEvaluator) Apply(ctx context.Context, fn core.Value, args []core.Value, env *core.Env) (core.Value, error) {
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
		for _, arg := range args {
			e.vm.push(arg)
		}
		e.vm.push(f)
		if err := e.vm.call(ctx, len(args), false); err != nil {
			return nil, err
		}
		return e.vm.pop(), nil
	case core.GoFunc:
		return f.Fn(ctx, e, args, env)
	default:
		return nil, core.NewTypeError("callable", fn)
	}
}

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
			vm.push(frame.chunk.Constants[instr.A()])

		case OpGetLocal:
			vm.push(vm.stack[frame.base+instr.A()])

		case OpSetLocal:
			idx := instr.A()
			vm.stack[frame.base+idx] = vm.peek()
			if idx < len(frame.chunk.LocalNames) {
				frame.env.Set(frame.chunk.LocalNames[idx], vm.peek())
			}

		case OpGetGlobal:
			sym := frame.chunk.Constants[instr.A()].(core.Symbol)
			v, ok := frame.env.Get(sym.V)
			if !ok {
				return nil, core.NewUndefinedError(sym.V)
			}
			vm.push(v)

		case OpSetGlobal:
			sym := frame.chunk.Constants[instr.A()].(core.Symbol)
			vm.globals.Set(sym.V, vm.peek())

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
				return nil, err
			}

		case OpTailCall:
			if err := vm.call(ctx, instr.A(), true); err != nil {
				return nil, err
			}

		case OpReturn:
			result := vm.pop()
			vm.frames = vm.frames[:len(vm.frames)-1]
			if vm.depth > 0 {
				vm.depth--
			}
			vm.stack = vm.stack[:frame.base]
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
			sub := frame.chunk.SubChunks[instr.A()]
			vm.push(NewClosure(sub, frame.env))
		}
	}
}

func (vm *VM) call(ctx context.Context, argc int, tail bool) error {
	fn := vm.stack[len(vm.stack)-argc-1]
	args := vm.stack[len(vm.stack)-argc:]

	switch f := fn.(type) {
	case core.GoFunc:
		result, err := f.Fn(ctx, vmEvaluator{vm: vm}, args, vm.globals)
		if err != nil {
			return err
		}
		vm.stack = vm.stack[:len(vm.stack)-argc-1]
		vm.push(result)

	case *Closure:
		if vm.maxDepth > 0 && vm.depth >= vm.maxDepth {
			return fmt.Errorf("maximum call depth %d exceeded", vm.maxDepth)
		}
		vm.depth++
		callEnv := core.NewEnv(f.Env)
		paramCount := f.Chunk.Arity
		if f.Chunk.Variadic {
			paramCount++
		}
		for i := 0; i < len(args) && i < len(f.Chunk.LocalNames); i++ {
			callEnv.Set(f.Chunk.LocalNames[i], args[i])
		}
		if tail && len(vm.frames) > 0 {
			vm.depth--
			frame := &vm.frames[len(vm.frames)-1]
			copy(vm.stack[frame.base:], args)
			vm.stack = vm.stack[:frame.base+len(args)]
			frame.chunk = f.Chunk
			frame.ip = 0
			frame.env = callEnv
		} else {
			base := len(vm.stack) - argc - 1
			copy(vm.stack[base:], args)
			vm.stack = vm.stack[:base+argc]
			vm.frames = append(vm.frames, Frame{
				chunk: f.Chunk,
				ip:    0,
				base:  base,
				env:   callEnv,
			})
		}

	default:
		return core.NewTypeError("callable", fn)
	}
	return nil
}
