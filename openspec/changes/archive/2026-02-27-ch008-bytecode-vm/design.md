# Design: Bytecode VM

**Change ID:** 008-bytecode-vm
**Status:** Design
**Created:** 2026-02-24

---

## 1. Package Layout

```
core/
  compiler/
    compiler.go      — AST → Chunk
    compiler_test.go
  vm/
    vm.go            — VM run loop
    opcode.go        — Opcode constants
    chunk.go         — Chunk type + serialization
    frame.go         — Frame type
    vm_test.go
  cache/
    cache.go         — BytecodeCache (disk)
    cache_test.go
```

All packages import only `core` (Value types) — no circular deps.

---

## 2. Opcode Definitions (opcode.go)

```go
package vm

type Opcode uint8

const (
    OpConst       Opcode = iota // push constants[A]
    OpGetGlobal                 // push globals[constants[A].(Symbol).V]
    OpSetGlobal                 // globals[constants[A].(Symbol).V] = pop()
    OpGetLocal                  // push stack[frame.base + A]
    OpSetLocal                  // stack[frame.base + A] = peek()
    OpCall                      // call stack[-A-1] with A args
    OpTailCall                  // tail-call: reuse frame
    OpReturn                    // pop result, restore previous frame
    OpJump                      // ip += A (unconditional)
    OpJumpIfFalse               // if !truthy(pop()): ip += A
    OpPop                       // discard top
    OpClosure                   // create closure from SubChunks[A]
    OpMakeList                  // build List from top A items
    OpMakeVector                // build Vector from top A items
    OpMakeMap                   // build HashMap from top A*2 items (key, val pairs)
    OpNil                       // push Nil{}
    OpTrue                      // push Bool{V: true}
    OpFalse                     // push Bool{V: false}
)

var opNames = [...]string{
    OpConst:       "CONST",
    OpGetGlobal:   "GET_GLOBAL",
    OpSetGlobal:   "SET_GLOBAL",
    OpGetLocal:    "GET_LOCAL",
    OpSetLocal:    "SET_LOCAL",
    OpCall:        "CALL",
    OpTailCall:    "TAIL_CALL",
    OpReturn:      "RETURN",
    OpJump:        "JUMP",
    OpJumpIfFalse: "JUMP_IF_FALSE",
    OpPop:         "POP",
    OpClosure:     "CLOSURE",
    OpMakeList:    "MAKE_LIST",
    OpMakeVector:  "MAKE_VECTOR",
    OpMakeMap:     "MAKE_MAP",
    OpNil:         "NIL",
    OpTrue:        "TRUE",
    OpFalse:       "FALSE",
}

func (o Opcode) String() string {
    if int(o) < len(opNames) {
        return opNames[o]
    }
    return fmt.Sprintf("OP_%d", o)
}
```

---

## 3. Instruction Encoding (chunk.go)

Each instruction is a single `uint32`: the high byte holds the opcode, the low 24 bits hold the operand `A`. This keeps the hot loop cache-friendly and limits the chunk to 16 777 215 constants or jump targets — sufficient for any realistic Lispico program.

```go
// Instruction packs opcode (high byte) + 24-bit operand.
type Instruction uint32

func Encode(op Opcode, a int) Instruction {
    return Instruction(uint32(op)<<24 | uint32(a)&0x00FFFFFF)
}

func (i Instruction) Op() Opcode { return Opcode(i >> 24) }
func (i Instruction) A() int     { return int(i & 0x00FFFFFF) }
func (i Instruction) String() string {
    return fmt.Sprintf("%-16s %d", i.Op(), i.A())
}
```

---

## 4. Chunk (chunk.go continued)

A `Chunk` is the unit of compilation — one per top-level form or `fn` body.

```go
type Chunk struct {
    Name      string
    Arity     int
    Variadic  bool
    Locals    int           // number of local variable slots
    Code      []Instruction
    Constants []core.Value  // literal pool (deduplicated)
    SubChunks []*Chunk      // nested function chunks (for closures)
}

func (c *Chunk) AddConstant(v core.Value) int {
    for i, existing := range c.Constants {
        if existing.Equals(v) {
            return i
        }
    }
    c.Constants = append(c.Constants, v)
    return len(c.Constants) - 1
}

func (c *Chunk) Emit(op Opcode, a int) int {
    c.Code = append(c.Code, Encode(op, a))
    return len(c.Code) - 1
}

func (c *Chunk) EmitJump(op Opcode) int {
    return c.Emit(op, 0xFFFFFF) // placeholder patched by PatchJump
}

func (c *Chunk) PatchJump(offset int) {
    target := len(c.Code) - offset - 1
    c.Code[offset] = Encode(c.Code[offset].Op(), target)
}
```

---

## 5. Closure Type (core extension)

The tree-walker uses `Lambda{Params []Symbol, Variadic Symbol, Body []Value, Env *Env}`. The VM needs a compiled counterpart that pairs a `*Chunk` with the enclosing environment:

```go
// Closure is a compiled function value: a Chunk bound to the env at definition time.
type Closure struct {
    Chunk *vm.Chunk
    Env   *Env
}

func NewClosure(chunk *vm.Chunk, env *Env) *Closure {
    return &Closure{Chunk: chunk, Env: env}
}

func (c *Closure) Type() Keyword  { return Keyword{V: "closure"} }
func (c *Closure) String() string { return fmt.Sprintf("#<closure %s>", c.Chunk.Name) }
func (c *Closure) Equals(o Value) bool { return c == o.(*Closure) }
```

`GoFunc` remains unchanged — it is callable by both the tree-walker and the VM.

---

## 6. VM Frame and Stack (frame.go, vm.go)

```go
type Frame struct {
    chunk *Chunk
    ip    int
    base  int // stack index of the first local slot for this frame
}

type VM struct {
    stack   []core.Value
    frames  []Frame
    globals *core.Env
    cache   *cache.BytecodeCache // nil if caching disabled
}

func New(globals *core.Env, bc *cache.BytecodeCache) *VM {
    return &VM{
        stack:   make([]core.Value, 0, 256),
        frames:  make([]Frame, 0, 64),
        globals: globals,
        cache:   bc,
    }
}

func (vm *VM) Cache() *cache.BytecodeCache { return vm.cache }

func (vm *VM) push(v core.Value) { vm.stack = append(vm.stack, v) }
func (vm *VM) pop() core.Value {
    top := vm.stack[len(vm.stack)-1]
    vm.stack = vm.stack[:len(vm.stack)-1]
    return top
}
func (vm *VM) peek() core.Value { return vm.stack[len(vm.stack)-1] }
```

---

## 7. VM Run Loop (vm.go)

```go
func (vm *VM) Run(ctx context.Context, chunk *Chunk) (core.Value, error) {
    vm.frames = append(vm.frames, Frame{chunk: chunk, base: len(vm.stack)})

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
            vm.stack[frame.base+instr.A()] = vm.peek()

        case OpGetGlobal:
            sym := frame.chunk.Constants[instr.A()].(core.Symbol)
            v, err := vm.globals.Get(sym.V)
            if err != nil {
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
            vm.push(core.NewClosure(sub, vm.globals))
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

    case *core.Closure:
        if tail && len(vm.frames) > 0 {
            frame := &vm.frames[len(vm.frames)-1]
            copy(vm.stack[frame.base:], args)
            vm.stack = vm.stack[:frame.base+len(args)]
            frame.chunk = f.Chunk
            frame.ip = 0
        } else {
            base := len(vm.stack) - argc
            vm.stack = vm.stack[:base]
            vm.frames = append(vm.frames, Frame{
                chunk: f.Chunk,
                ip:    0,
                base:  base,
            })
        }

    default:
        return core.NewTypeError("callable", fn)
    }
    return nil
}
```

`IsTruthy` mirrors the tree-walker rule: only `Nil{}` and `Bool{V: false}` are falsy.

```go
func IsTruthy(v core.Value) bool {
    switch val := v.(type) {
    case core.Nil:
        return false
    case core.Bool:
        return val.V
    default:
        return true
    }
}
```

---

## 8. Compiler (compiler.go)

```go
type Compiler struct {
    chunk  *Chunk
    locals []local
    depth  int       // scope nesting depth
    parent *Compiler // non-nil for nested fn bodies
}

type local struct {
    name  string
    depth int
}

func NewCompiler(name string) *Compiler {
    return &Compiler{chunk: &Chunk{Name: name}}
}

func (c *Compiler) Chunk() *Chunk { return c.chunk }

func (c *Compiler) Compile(form core.Value) error {
    switch f := form.(type) {

    case core.Nil:
        c.chunk.Emit(OpNil, 0)
    case core.Bool:
        if f.V {
            c.chunk.Emit(OpTrue, 0)
        } else {
            c.chunk.Emit(OpFalse, 0)
        }
    case core.Int, core.Float, core.String, core.Keyword:
        c.chunk.Emit(OpConst, c.chunk.AddConstant(form))

    case core.Symbol:
        if idx := c.resolveLocal(f.V); idx >= 0 {
            c.chunk.Emit(OpGetLocal, idx)
        } else {
            c.chunk.Emit(OpGetGlobal, c.chunk.AddConstant(f))
        }

    case core.List:
        return c.compileList(f)

    case core.Vector:
        for _, item := range f.Items {
            if err := c.Compile(item); err != nil {
                return err
            }
        }
        c.chunk.Emit(OpMakeVector, len(f.Items))

    case *core.HashMap:
        pairs := f.Pairs()
        for _, kv := range pairs {
            if err := c.Compile(kv[0]); err != nil {
                return err
            }
            if err := c.Compile(kv[1]); err != nil {
                return err
            }
        }
        c.chunk.Emit(OpMakeMap, len(pairs))

    default:
        return fmt.Errorf("compile: unknown form type %T", form)
    }
    return nil
}

func (c *Compiler) compileList(f core.List) error {
    if len(f.Items) == 0 {
        c.chunk.Emit(OpNil, 0)
        return nil
    }
    head, isSym := f.Items[0].(core.Symbol)
    if isSym {
        switch head.V {
        case "if":     return c.compileIf(f.Items[1:])
        case "def":    return c.compileDef(f.Items[1:])
        case "fn":     return c.compileFn(f.Items[1:])
        case "let":    return c.compileLet(f.Items[1:])
        case "let*":   return c.compileLetStar(f.Items[1:])
        case "do":     return c.compileDo(f.Items[1:])
        case "quote":  c.chunk.Emit(OpConst, c.chunk.AddConstant(f.Items[1])); return nil
        case "set!":   return c.compileSet(f.Items[1:])
        case "when":   return c.compileWhen(f.Items[1:])
        case "unless": return c.compileUnless(f.Items[1:])
        case "loop":   return c.compileLoop(f.Items[1:])
        case "recur":  return c.compileRecur(f.Items[1:])
        case "try":    return c.compileTry(f.Items[1:])
        }
    }
    return c.compileCall(f.Items)
}

func (c *Compiler) compileIf(args []core.Value) error {
    if err := c.Compile(args[0]); err != nil {
        return err
    }
    jumpFalse := c.chunk.EmitJump(OpJumpIfFalse)
    if err := c.Compile(args[1]); err != nil {
        return err
    }
    jumpEnd := c.chunk.EmitJump(OpJump)
    c.chunk.PatchJump(jumpFalse)
    if len(args) > 2 {
        if err := c.Compile(args[2]); err != nil {
            return err
        }
    } else {
        c.chunk.Emit(OpNil, 0)
    }
    c.chunk.PatchJump(jumpEnd)
    return nil
}

func (c *Compiler) compileDef(args []core.Value) error {
    if len(args) != 2 {
        return fmt.Errorf("compile def: expected 2 args, got %d", len(args))
    }
    sym, ok := args[0].(core.Symbol)
    if !ok {
        return fmt.Errorf("compile def: name must be symbol, got %T", args[0])
    }
    if err := c.Compile(args[1]); err != nil {
        return err
    }
    c.chunk.Emit(OpSetGlobal, c.chunk.AddConstant(sym))
    return nil
}

func (c *Compiler) compileFn(args []core.Value) error {
    params, variadic, err := parseParams(args[0])
    if err != nil {
        return err
    }
    sub := NewCompiler("<fn>")
    sub.parent = c
    for _, p := range params {
        sub.addLocal(p.V)
    }
    if variadic.V != "" {
        sub.addLocal(variadic.V)
    }
    for _, body := range args[1:] {
        if err := sub.Compile(body); err != nil {
            return err
        }
    }
    sub.chunk.Emit(OpReturn, 0)
    sub.chunk.Arity = len(params)
    sub.chunk.Variadic = variadic.V != ""

    idx := len(c.chunk.SubChunks)
    c.chunk.SubChunks = append(c.chunk.SubChunks, sub.chunk)
    c.chunk.Emit(OpClosure, idx)
    return nil
}

func (c *Compiler) compileDo(args []core.Value) error {
    for i, form := range args {
        if err := c.Compile(form); err != nil {
            return err
        }
        if i < len(args)-1 {
            c.chunk.Emit(OpPop, 0)
        }
    }
    if len(args) == 0 {
        c.chunk.Emit(OpNil, 0)
    }
    return nil
}

func (c *Compiler) compileLet(args []core.Value) error {
    bindings, ok := args[0].(core.Vector)
    if !ok {
        return fmt.Errorf("compile let: bindings must be vector")
    }
    if len(bindings.Items)%2 != 0 {
        return fmt.Errorf("compile let: bindings must have even count")
    }
    c.depth++
    base := len(c.locals)
    for i := 0; i < len(bindings.Items); i += 2 {
        if err := c.Compile(bindings.Items[i+1]); err != nil {
            return err
        }
        sym := bindings.Items[i].(core.Symbol)
        c.addLocal(sym.V)
    }
    if err := c.compileDo(args[1:]); err != nil {
        return err
    }
    c.locals = c.locals[:base]
    c.depth--
    return nil
}

func (c *Compiler) compileLetStar(args []core.Value) error {
    return c.compileLet(args) // let* is identical when locals shadow sequentially
}

func (c *Compiler) compileSet(args []core.Value) error {
    if len(args) != 2 {
        return fmt.Errorf("compile set!: expected 2 args, got %d", len(args))
    }
    sym, ok := args[0].(core.Symbol)
    if !ok {
        return fmt.Errorf("compile set!: name must be symbol, got %T", args[0])
    }
    if err := c.Compile(args[1]); err != nil {
        return err
    }
    if idx := c.resolveLocal(sym.V); idx >= 0 {
        c.chunk.Emit(OpSetLocal, idx)
    } else {
        c.chunk.Emit(OpSetGlobal, c.chunk.AddConstant(sym))
    }
    return nil
}

func (c *Compiler) compileWhen(args []core.Value) error {
    if err := c.Compile(args[0]); err != nil {
        return err
    }
    jump := c.chunk.EmitJump(OpJumpIfFalse)
    if err := c.compileDo(args[1:]); err != nil {
        return err
    }
    c.chunk.PatchJump(jump)
    return nil
}

func (c *Compiler) compileUnless(args []core.Value) error {
    if err := c.Compile(args[0]); err != nil {
        return err
    }
    jumpFalse := c.chunk.EmitJump(OpJumpIfFalse)
    jumpOver := c.chunk.EmitJump(OpJump)
    c.chunk.PatchJump(jumpFalse)
    if err := c.compileDo(args[1:]); err != nil {
        return err
    }
    c.chunk.PatchJump(jumpOver)
    return nil
}

func (c *Compiler) compileLoop(args []core.Value) error {
    // loop is syntactic sugar: (loop [x init] body) → (let [x init] (recur-target body))
    // The compiler records a loop entry ip so recur can jump back.
    return c.compileLet(args) // placeholder; recur support added alongside
}

func (c *Compiler) compileRecur(args []core.Value) error {
    for _, arg := range args {
        if err := c.Compile(arg); err != nil {
            return err
        }
    }
    c.chunk.Emit(OpTailCall, len(args))
    return nil
}

func (c *Compiler) compileTry(args []core.Value) error {
    // Deferred to runtime integration; emits OpCall for now with try semantics handled by GoFunc wrapper.
    return c.compileDo(args)
}

func (c *Compiler) compileCall(items []core.Value) error {
    if err := c.Compile(items[0]); err != nil {
        return err
    }
    for _, arg := range items[1:] {
        if err := c.Compile(arg); err != nil {
            return err
        }
    }
    argc := len(items) - 1
    c.chunk.Emit(OpCall, argc)
    return nil
}

func (c *Compiler) resolveLocal(name string) int {
    for i := len(c.locals) - 1; i >= 0; i-- {
        if c.locals[i].name == name {
            return i
        }
    }
    return -1
}

func (c *Compiler) addLocal(name string) {
    c.locals = append(c.locals, local{name: name, depth: c.depth})
    c.chunk.Locals++
}

// CompileAll compiles a sequence of top-level forms into one Chunk each.
func CompileAll(forms []core.Value) ([]*Chunk, error) {
    chunks := make([]*Chunk, 0, len(forms))
    for _, form := range forms {
        comp := NewCompiler("<top>")
        if err := comp.Compile(form); err != nil {
            return nil, err
        }
        comp.chunk.Emit(OpReturn, 0)
        chunks = append(chunks, comp.chunk)
    }
    return chunks, nil
}

func parseParams(v core.Value) (params []core.Symbol, variadic core.Symbol, err error) {
    vec, ok := v.(core.Vector)
    if !ok {
        return nil, core.Symbol{}, fmt.Errorf("fn params must be vector, got %T", v)
    }
    for i, item := range vec.Items {
        sym, ok := item.(core.Symbol)
        if !ok {
            return nil, core.Symbol{}, fmt.Errorf("fn param must be symbol, got %T", item)
        }
        if sym.V == "&" {
            if i+1 >= len(vec.Items) {
                return nil, core.Symbol{}, fmt.Errorf("fn: & requires a rest param name")
            }
            variadic = vec.Items[i+1].(core.Symbol)
            break
        }
        params = append(params, sym)
    }
    return params, variadic, nil
}
```

---

## 9. Macro Expansion at Compile Time

Macros are defined with `defmacro` and stored as `Macro{Params, Variadic, Body, Env}` in the global env — identical to the tree-walker representation. The compiler does **not** compile macros; it expands them first via the tree-walker's `MacroExpand` pass, then compiles the expanded form.

```go
// CompileExpanded macro-expands form before emitting bytecode.
func (c *Compiler) CompileExpanded(expander MacroExpander, form core.Value) error {
    expanded, err := expander.Expand(form)
    if err != nil {
        return err
    }
    return c.Compile(expanded)
}

// MacroExpander is the subset of core.Evaluator needed for macro expansion.
// The tree-walker implements this; no VM involvement required.
type MacroExpander interface {
    Expand(form core.Value) (core.Value, error)
}
```

Rules:
- `defmacro` forms are evaluated eagerly by the tree-walker to register the `Macro` value in globals.
- All subsequent forms are passed through `MacroExpand` before the compiler sees them.
- `Macro` values are never compiled to bytecode — only their expansions are.

---

## 10. vmEvaluator Adapter

`GoFunc.Fn` signature is `func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error)`. The VM satisfies `core.Evaluator` through a thin adapter so GoFuncs can call back into it (e.g. `apply`, `map`, `eval`):

```go
type vmEvaluator struct{ vm *VM }

func (e vmEvaluator) Eval(ctx context.Context, form core.Value, env *core.Env) (core.Value, error) {
    comp := compiler.NewCompiler("<eval>")
    if err := comp.Compile(form); err != nil {
        return nil, err
    }
    comp.Chunk().Emit(OpReturn, 0)
    return e.vm.Run(ctx, comp.Chunk())
}
```

This keeps GoFuncs evaluator-agnostic — they work unchanged under both the tree-walker and the VM.

---

## 11. Bytecode Cache (cache/cache.go)

The cache stores compiled chunks on disk keyed by `sha256(content)[0:8]` so hot-reload skips re-compilation when source has not changed. A monotonic `cacheVersion` constant invalidates all cached files when the bytecode format changes.

```go
type BytecodeCache struct {
    dir string
}

func New(dir string) (*BytecodeCache, error) {
    if err := os.MkdirAll(dir, 0755); err != nil {
        return nil, fmt.Errorf("bytecode cache mkdir %s: %w", dir, err)
    }
    return &BytecodeCache{dir: dir}, nil
}

const cacheVersion = 1

type cacheEntry struct {
    Version int
    Chunks  []*vm.Chunk
}

func (bc *BytecodeCache) key(path string, content []byte) string {
    h := sha256.Sum256(content)
    base := filepath.Base(path)
    return filepath.Join(bc.dir, fmt.Sprintf("%s.%x.lbc", base, h[:8]))
}

func (bc *BytecodeCache) Load(path string, content []byte) ([]*vm.Chunk, error) {
    f, err := os.Open(bc.key(path, content))
    if err != nil {
        return nil, err // cache miss
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

// Store writes chunks to disk asynchronously so hot-reload is not blocked by I/O.
func (bc *BytecodeCache) Store(path string, content []byte, chunks []*vm.Chunk) {
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
```

---

## 12. Runtime Integration (ch003 extension)

Two new `EngineOption` values select the VM path and enable caching:

```go
func WithBytecode() EngineOption {
    return func(cfg *engineConfig) { cfg.bytecode = true }
}

func WithBytecodeCache(dir string) EngineOption {
    return func(cfg *engineConfig) { cfg.cacheDir = dir }
}
```

`Engine.New` selects the evaluator at construction time:

```go
func New(log *slog.Logger, opts ...EngineOption) (*Engine, error) {
    cfg := defaultConfig()
    for _, o := range opts {
        o(cfg)
    }

    var eval core.Evaluator
    if cfg.bytecode {
        bc, err := cache.New(cfg.cacheDir)
        if err != nil {
            return nil, err
        }
        eval = vm.New(rootEnv, bc)
    } else {
        eval = treewalk.New(rootEnv)
    }
    // ...
}
```

The hot-reload path in `Watch()` checks for a VM evaluator and uses the cache:

```go
func (e *Engine) reloadFile(path string) error {
    content, err := os.ReadFile(path)
    if err != nil {
        return fmt.Errorf("reload %s: %w", path, err)
    }

    vmEval, ok := e.eval.(*vm.VM)
    if !ok {
        return e.loadFileTreeWalk(path, content)
    }

    chunks, err := vmEval.Cache().Load(path, content)
    if err != nil {
        // cache miss — compile from source
        forms, err := core.ReadAll(string(content))
        if err != nil {
            return err
        }
        chunks, err = compiler.CompileAll(forms)
        if err != nil {
            return err
        }
        vmEval.Cache().Store(path, content, chunks)
    }

    for _, chunk := range chunks {
        if _, err := vmEval.RunChunk(context.Background(), chunk); err != nil {
            return err
        }
    }
    return nil
}
```

---

## 13. File Organization

```
core/
├── compiler/
│   ├── compiler.go      — Compiler struct, Compile, CompileAll, parseParams
│   └── compiler_test.go
├── vm/
│   ├── opcode.go        — Opcode constants and opNames table
│   ├── chunk.go         — Instruction encoding, Chunk, AddConstant, Emit, PatchJump
│   ├── frame.go         — Frame struct
│   ├── vm.go            — VM struct, Run, call, vmEvaluator adapter
│   └── vm_test.go
└── cache/
    ├── cache.go         — BytecodeCache, cacheEntry, Load, Store
    └── cache_test.go
```

The `core` package gains one new exported type (`Closure`) and one helper (`IsTruthy` if not already present). No existing types change.

---

## 14. Testing Strategy

### Unit Tests

- `Opcode.String()` for all defined opcodes
- `Encode`/`Op`/`A` round-trip
- `Chunk.AddConstant` deduplication
- `Chunk.PatchJump` offset correctness
- `Compiler.Compile` for each literal type (`Nil{}`, `Bool{}`, `Int{}`, `Float{}`, `String{}`, `Keyword{}`, `Symbol{}`)
- `compileIf` with and without else branch
- `compileLet` local slot resolution and scoping
- `compileFn` sub-chunk creation and `OpClosure` emission
- `VM.Run` for each opcode in isolation
- `IsTruthy` boundary cases (`Nil{}`, `Bool{V:false}`, `Int{V:0}`, `String{V:""}`)

### Integration Tests

- Full compile-and-run: `(+ 1 2)` → `Int{V:3}`
- Recursive fibonacci via `OpTailCall`
- `let` binding and shadowing
- `if` with all three arities (no else, false branch, true branch)
- `GoFunc` callback through `vmEvaluator.Eval`
- Cache hit: second load of same content returns cached chunks
- Cache miss: version mismatch causes re-compile
- Context cancellation halts the run loop

### Property-Based Tests

- Compile → run result equals tree-walker result for the same form
- Constant pool deduplication: same literal never added twice

---
