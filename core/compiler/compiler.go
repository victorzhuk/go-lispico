// Package compiler compiles core AST Values into vm bytecode chunks.
package compiler

import (
	"context"
	"fmt"

	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/core/vm"
)

// CodeUnsupported identifies a *core.LispicoError for a form the bytecode
// compiler does not support (a defmacro nested inside a larger form, unquote-splicing).
// Callers use it to distinguish "fall back to the tree-walker" from a real
// compile error.
const CodeUnsupported = "BytecodeUnsupported"

func unsupportedErr(msg string) error {
	return &core.LispicoError{Code: CodeUnsupported, Message: msg}
}

// CodeCompileError identifies a *core.LispicoError reporting that a form
// failed arity or shape validation during bytecode compilation.
const CodeCompileError = "CompileError"

func compileErrf(format string, args ...any) error {
	return &core.LispicoError{Code: CodeCompileError, Message: fmt.Sprintf(format, args...)}
}

// Compiler compiles core.Value forms into a single vm.Chunk, tracking local
// variable scopes as it goes. It implements vm.FormCompiler.
type Compiler struct {
	chunk           *vm.Chunk
	locals          []local
	depth           int
	parent          *Compiler
	loops           []loopFrame
	dialect         *core.Dialect
	meter           core.EvalMeter
	ctx             context.Context
	err             error
	compileDepth    int
	maxCompileDepth int
	nodeCount       int
	// caps lists the free variables this chunk captures from its enclosing
	// context, parallel to chunk.Caps.
	caps []string
	// binds marks instruction indices that bind (rather than mutate) a local,
	// so finalize can tell a box-allocating store from a write-through.
	binds map[int]bool
}

type loopFrame struct {
	start int
	slots []int
}

type local struct {
	name  string
	depth int
}

// NewCompiler creates a Compiler that emits into a new chunk named name.
func NewCompiler(name string) *Compiler {
	return &Compiler{chunk: &vm.Chunk{Name: name}, maxCompileDepth: core.MaxCompileDepth}
}

// NewCompilerWithDialect creates a Compiler that emits into a new chunk named name
// with access to the dialect for dialect-dependent compilation.
func NewCompilerWithDialect(name string, dialect *core.Dialect) *Compiler {
	return &Compiler{chunk: &vm.Chunk{Name: name, Truthiness: dialect.TruthyFunc()}, dialect: dialect, maxCompileDepth: core.MaxCompileDepth}
}

// Chunk returns the chunk the compiler is emitting into.
func (c *Compiler) Chunk() *vm.Chunk { return c.chunk }

// SetEvalMeter attaches the evaluation meter charged by subsequent emitted instructions.
func (c *Compiler) SetEvalMeter(m core.EvalMeter) { c.meter = m }

func (c *Compiler) SetContext(ctx context.Context) { c.ctx = ctx }

// EmitReturn emits the trailing return instruction expected by executable chunks.
func (c *Compiler) EmitReturn() error {
	c.emit(vm.OpReturn, 0)
	return c.err
}

func (c *Compiler) emit(op vm.Opcode, a int) int {
	if c.err != nil {
		return 0
	}
	if err := c.meter.ChargeReductions(1); err != nil {
		c.err = err
		return 0
	}
	return c.chunk.Emit(op, a)
}

func (c *Compiler) emitJump(op vm.Opcode) int {
	return c.emit(op, 0xFFFFFF)
}

func (c *Compiler) emitLoop(start int) int {
	return c.emit(vm.OpLoop, start)
}

// MarkCaptures finalizes capture handling for the finished chunk: it rewrites
// every access to a captured local from slot opcodes to cell opcodes (boxing
// at binding sites, write-through at mutation sites) and computes MaxStack.
// Capture registration itself happens during compilation: a closure body's
// free-variable references resolve through the compiler parent chain at
// emission time. Must be called once Code is final. CompileAll does this per
// form; single-form callers must too.
func (c *Compiler) MarkCaptures() { _ = c.MarkCapturesContext(context.Background()) }

func (c *Compiler) MarkCapturesContext(ctx context.Context) error {
	if err := c.finalizeContext(ctx); err != nil {
		return err
	}
	return nil
}

// emitGetGlobal emits OpGetGlobal for sym. The VM's site cache is built later
// from Code (Chunk.EnsureSites), so no per-site bookkeeping is needed here.
func (c *Compiler) emitGetGlobal(sym core.Symbol) {
	c.emit(vm.OpGetGlobal, c.chunk.AddConstant(sym))
}

// Compile emits bytecode for form into the compiler's chunk.
func (c *Compiler) Compile(form core.Value) error {
	if c.err != nil {
		return c.err
	}
	if c.compileDepth == 0 {
		n, err := core.ValueNodeCountContext(c.ctx, form)
		if err != nil {
			c.err = err
			return err
		}
		c.nodeCount += n
	}
	c.compileDepth++
	defer func() { c.compileDepth-- }()
	if c.maxCompileDepth > 0 && c.compileDepth > c.maxCompileDepth {
		return core.NewResourceLimitError("compile depth limit exceeded")
	}
	switch f := form.(type) {
	case core.Nil:
		c.emit(vm.OpNil, 0)
	case core.Bool:
		if f.V {
			c.emit(vm.OpTrue, 0)
		} else {
			c.emit(vm.OpFalse, 0)
		}
	case core.Int, core.Float, core.String, core.Keyword:
		c.emit(vm.OpConst, c.chunk.AddConstant(form))

	case core.Symbol:
		if idx := c.resolveLocal(f.V); idx >= 0 {
			c.emit(vm.OpGetLocal, idx)
		} else if c.parent != nil && c.ancestorBinds(f.V) {
			c.emit(vm.OpGetCap, c.ensureCapture(f.V))
		} else {
			c.emitGetGlobal(f)
		}

	case core.List:
		if err := c.compileList(f); err != nil {
			return err
		}

	case core.Vector:
		if ok, err := c.compileConstantCollection(f, false); ok || err != nil {
			return err
		}
		c.emit(vm.OpStructEnter, 1)
		items := f.ToSlice()
		for _, item := range items {
			if err := c.Compile(item); err != nil {
				return err
			}
		}
		c.emit(vm.OpMakeVector, len(items))
		c.emit(vm.OpStructLeave, 1)
	case *core.HashMap:
		if ok, err := c.compileConstantCollection(f, false); ok || err != nil {
			return err
		}
		c.emit(vm.OpStructEnter, 1)
		var pairs [][2]core.Value
		f.Each(func(k, v core.Value) {
			pairs = append(pairs, [2]core.Value{k, v})
		})
		for _, kv := range pairs {
			if err := c.Compile(kv[0]); err != nil {
				return err
			}
			if err := c.Compile(kv[1]); err != nil {
				return err
			}
		}
		c.emit(vm.OpMakeMap, len(pairs))
		c.emit(vm.OpStructLeave, 1)
	default:
		return compileErrf("compile: unknown form type %T", form)
	}
	if c.err != nil {
		return c.err
	}
	return nil
}

func (c *Compiler) compileList(f core.List) error {
	if c.err != nil {
		return c.err
	}
	items := f.ToSlice()
	if len(items) == 0 {
		c.emit(vm.OpNil, 0)
		return nil
	}
	head, isSym := items[0].(core.Symbol)
	if isSym {
		canonicalName := head.V
		isSpecial := true
		if c.dialect != nil {
			canonical, removed, ok := c.dialect.CanonicalName(head.V)
			if removed {
				return compileErrf("compile: undefined form %q", head.V)
			}
			if ok {
				canonicalName = canonical
			}
			isSpecial = ok
		}
		if isSpecial {
			switch canonicalName {
			case "if":
				return c.compileIf(items[1:])
			case "def":
				return c.compileDef(items[1:])
			case "defn":
				return c.compileDefn(items[1:])
			case "fn":
				return c.compileFn(items[1:])
			case "function":
				if len(items[1:]) != 1 {
					return fmt.Errorf("function: requires exactly 1 argument")
				}
				sym, ok := items[1].(core.Symbol)
				if !ok {
					return fmt.Errorf("function: argument must be symbol, got %T", items[1])
				}
				c.emit(vm.OpGetFunc, c.chunk.AddConstant(sym))
				return nil
			case "funcall":
				// funcall evaluates its first argument as a value expression and calls it.
				if len(items[1:]) < 1 {
					return fmt.Errorf("funcall: requires at least 1 argument")
				}
				if err := c.Compile(items[1]); err != nil {
					return err
				}
				for _, arg := range items[2:] {
					if err := c.Compile(arg); err != nil {
						return err
					}
				}
				c.emit(vm.OpCall, len(items[2:]))
				return nil
			case "let":
				return c.compileLet(items[1:])
			case "let*":
				return c.compileLetStar(items[1:])
			case "do":
				return c.compileDo(items[1:])
			case "quote":
				if len(items) < 2 {
					return compileErrf("quote: missing value")
				}
				// Quote must stay a plain OpConst: the tree-walker's evalQuote
				// returns the datum with no construction charge or depth check,
				// so charging here would diverge the ledger across evaluators.
				c.emit(vm.OpConst, c.chunk.AddConstant(items[1]))
				return nil
			case "cond":
				return c.compileCond(items[1:])
			case "and":
				return c.compileAnd(items[1:])
			case "or":
				return c.compileOr(items[1:])
			case "not":
				return c.compileNot(items[1:])
			case "quasiquote":
				return c.compileQuasiquote(items[1:])
			case "set!":
				return c.compileSet(items[1:])
			case "when":
				return c.compileWhen(items[1:])
			case "loop":
				return c.compileLoop(items[1:])
			case "recur":
				return c.compileRecur(items[1:])
			case "try":
				return c.compileTry(items[1:])
			case "throw":
				return c.compileThrow(items[1:])
			case "catch":
				return compileErrf("catch used outside of try")
			case "defmacro":
				return c.compileDefmacro(items[1:])
			}
		}
		// +, -, *, etc. aren't special forms, so a configured dialect marks
		// isSpecial false for them and the switch above never runs; a form
		// that IS a real special form already returned from the switch, so
		// this can't misfire. Skip only when locally shadowed, falling back
		// to compileCall/OpCall.
		if op, ok := nativeOp(canonicalName); ok && !c.isLocallyShadowed(canonicalName) {
			return c.compileNativeOp(items, op)
		}
	}
	return c.compileCall(items)
}

func (c *Compiler) compileIf(args []core.Value) error {
	if c.err != nil {
		return c.err
	}
	if len(args) < 2 {
		return compileErrf("if: expected condition and then branch, got %d args", len(args))
	}
	if err := c.Compile(args[0]); err != nil {
		return err
	}
	jumpFalse := c.emitJump(vm.OpJumpIfFalse)
	if err := c.Compile(args[1]); err != nil {
		return err
	}
	jumpEnd := c.emitJump(vm.OpJump)
	c.chunk.PatchJump(jumpFalse)
	if len(args) > 2 {
		if err := c.Compile(args[2]); err != nil {
			return err
		}
	} else {
		c.emit(vm.OpNil, 0)
	}
	c.chunk.PatchJump(jumpEnd)
	return nil
}

func (c *Compiler) compileDef(args []core.Value) error {
	if c.err != nil {
		return c.err
	}
	if len(args) != 2 {
		return compileErrf("compile def: expected 2 args, got %d", len(args))
	}
	sym, ok := args[0].(core.Symbol)
	if !ok {
		return compileErrf("compile def: name must be symbol, got %T", args[0])
	}
	if err := c.Compile(args[1]); err != nil {
		return err
	}
	c.emit(vm.OpSetGlobal, c.chunk.AddConstant(sym))
	return nil
}

func (c *Compiler) compileFn(args []core.Value) error {
	if c.err != nil {
		return c.err
	}
	if len(args) == 0 {
		return compileErrf("fn requires at least 2 arguments (params body...)")
	}
	params, variadic, err := parseParams(args[0])
	if err != nil {
		return err
	}
	if len(args) < 2 {
		return compileErrf("fn requires at least 2 arguments (params body...)")
	}
	sub := NewCompiler("<fn>")
	if c.dialect != nil {
		sub = NewCompilerWithDialect("<fn>", c.dialect)
	}
	sub.parent = c
	sub.meter = c.meter
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
	sub.emit(vm.OpReturn, 0)
	if sub.err != nil {
		return sub.err
	}
	sub.chunk.Arity = len(params)
	sub.chunk.Variadic = variadic.V != ""
	sub.chunk.EnsureSites()
	if err := sub.finalizeContext(c.ctx); err != nil {
		return err
	}
	idx := len(c.chunk.SubChunks)
	c.chunk.SubChunks = append(c.chunk.SubChunks, sub.chunk)
	c.emit(vm.OpClosure, idx)
	return nil
}

func (c *Compiler) compileDo(args []core.Value) error {
	if c.err != nil {
		return c.err
	}
	for i, form := range args {
		if err := c.Compile(form); err != nil {
			return err
		}
		if i < len(args)-1 {
			c.emit(vm.OpPop, 0)
		}
	}
	if len(args) == 0 {
		c.emit(vm.OpNil, 0)
	}
	return nil
}

func (c *Compiler) compileLet(args []core.Value) error {
	if c.err != nil {
		return c.err
	}
	if len(args) < 2 {
		return compileErrf("compile let: expected bindings and body")
	}
	bindings, err := core.NormalizeBindings("let", args[0])
	if err != nil {
		return compileErrf("%s", err)
	}
	c.depth++
	base := len(c.locals)
	for _, binding := range bindings {
		if err := c.Compile(binding.Value); err != nil {
			return err
		}
		c.addLocal(binding.Name.V)
		c.emitBind(len(c.locals) - 1)
	}
	if err := c.compileDo(args[1:]); err != nil {
		return err
	}
	c.locals = c.locals[:base]
	c.depth--
	return nil
}

func (c *Compiler) compileLetStar(args []core.Value) error {
	if c.err != nil {
		return c.err
	}
	if len(args) < 2 {
		return compileErrf("let*: expected bindings and body")
	}
	bindings, err := core.NormalizeBindings("let*", args[0])
	if err != nil {
		return compileErrf("%s", err)
	}
	c.depth++
	base := len(c.locals)
	for _, binding := range bindings {
		if err := c.Compile(binding.Value); err != nil {
			return err
		}
		c.addLocal(binding.Name.V)
		c.emitBind(len(c.locals) - 1)
	}
	if err := c.compileDo(args[1:]); err != nil {
		return err
	}
	c.locals = c.locals[:base]
	c.depth--
	return nil
}

func (c *Compiler) compileSet(args []core.Value) error {
	if c.err != nil {
		return c.err
	}
	if len(args) != 2 {
		return compileErrf("compile set!: expected 2 args, got %d", len(args))
	}
	sym, ok := args[0].(core.Symbol)
	if !ok {
		return compileErrf("compile set!: name must be symbol, got %T", args[0])
	}
	if err := c.Compile(args[1]); err != nil {
		return err
	}
	if idx := c.resolveLocal(sym.V); idx >= 0 {
		c.emit(vm.OpSetLocal, idx)
	} else if c.parent != nil && c.ancestorBinds(sym.V) {
		c.emit(vm.OpSetCap, c.ensureCapture(sym.V))
	} else {
		c.emit(vm.OpSetLexical, c.chunk.AddConstant(sym))
	}
	return nil
}

func (c *Compiler) compileWhen(args []core.Value) error {
	if c.err != nil {
		return c.err
	}
	if len(args) == 0 {
		return compileErrf("when: missing condition")
	}
	if err := c.Compile(args[0]); err != nil {
		return err
	}
	jumpFalse := c.emitJump(vm.OpJumpIfFalse)
	if err := c.compileDo(args[1:]); err != nil {
		return err
	}
	jumpEnd := c.emitJump(vm.OpJump)
	c.chunk.PatchJump(jumpFalse)
	c.emit(vm.OpNil, 0)
	c.chunk.PatchJump(jumpEnd)
	return nil
}

func (c *Compiler) compileLoop(args []core.Value) error {
	if c.err != nil {
		return c.err
	}
	if len(args) < 2 {
		return compileErrf("loop: expected bindings and body")
	}
	bindings, err := core.NormalizeBindings("loop", args[0])
	if err != nil {
		return compileErrf("%s", err)
	}
	var slots []int
	for _, binding := range bindings {
		if err := c.Compile(binding.Value); err != nil {
			return err
		}
		slots = append(slots, len(c.locals))
		c.addLocal(binding.Name.V)
		c.emitBind(len(c.locals) - 1)
	}
	startIP := len(c.chunk.Code)
	c.loops = append(c.loops, loopFrame{start: startIP, slots: slots})
	if err := c.compileDo(args[1:]); err != nil {
		return err
	}
	c.loops = c.loops[:len(c.loops)-1]
	return nil
}

func (c *Compiler) compileRecur(args []core.Value) error {
	if c.err != nil {
		return c.err
	}
	if len(c.loops) == 0 {
		return compileErrf("recur outside loop")
	}
	loop := c.loops[len(c.loops)-1]
	if len(args) != len(loop.slots) {
		return compileErrf("recur: expected %d args, got %d", len(loop.slots), len(args))
	}
	for _, arg := range args {
		if err := c.Compile(arg); err != nil {
			return err
		}
	}
	for i := len(loop.slots) - 1; i >= 0; i-- {
		ip := c.emit(vm.OpSetLocal, loop.slots[i])
		if loop.slots[i] < len(c.chunk.Captured) && c.chunk.Captured[loop.slots[i]] {
			if c.binds == nil {
				c.binds = map[int]bool{}
			}
			c.binds[ip] = true
		}
		c.emit(vm.OpPop, 0)
	}
	c.emitLoop(loop.start)
	return nil
}

func (c *Compiler) compileTry(args []core.Value) error {
	if c.err != nil {
		return c.err
	}
	if len(args) < 2 {
		return compileErrf("try: expected body and catch clause")
	}
	catchClause, ok := args[len(args)-1].(core.List)
	if !ok || catchClause.Len() < 3 {
		return compileErrf("try: last argument must be (catch <sym> <handler>...)")
	}
	items := catchClause.ToSlice()
	head, ok := items[0].(core.Symbol)
	if !ok || head.V != "catch" {
		return compileErrf("try: expected catch clause, got %v", items[0])
	}
	errSymIndex := 1
	bodyStart := 2
	if len(items) >= 4 {
		errSymIndex = 2
		bodyStart = 3
	}
	errSym, ok := items[errSymIndex].(core.Symbol)
	if !ok {
		return compileErrf("catch: error binding must be a symbol")
	}
	body := args[:len(args)-1]

	base := len(c.locals)
	setup := c.emitJump(vm.OpSetupTry)
	if err := c.compileDo(body); err != nil {
		return err
	}
	c.emit(vm.OpPopTry, 0)
	skip := c.emitJump(vm.OpJump)
	handlerAddr := len(c.chunk.Code)
	c.chunk.PatchJumpTo(setup, handlerAddr)
	catchSlot := len(c.locals)
	c.addLocal(errSym.V)
	c.emitBind(catchSlot)
	if err := c.compileDo(items[bodyStart:]); err != nil {
		return err
	}
	c.locals = c.locals[:base]
	c.chunk.PatchJump(skip)
	return nil
}

func (c *Compiler) compileThrow(args []core.Value) error {
	if c.err != nil {
		return c.err
	}
	if len(args) != 1 {
		return compileErrf("throw: expected 1 argument, got %d", len(args))
	}
	if err := c.Compile(args[0]); err != nil {
		return err
	}
	c.emit(vm.OpThrow, 0)
	return nil
}

func (c *Compiler) compileCall(items []core.Value) error {
	if c.err != nil {
		return c.err
	}
	// Lisp-2: emit OpGetFunc for the head symbol instead of OpGetGlobal.
	if c.dialect != nil && c.dialect.IsLisp2() {
		if sym, ok := items[0].(core.Symbol); ok {
			c.emit(vm.OpGetFunc, c.chunk.AddConstant(sym))
		} else {
			if err := c.Compile(items[0]); err != nil {
				return err
			}
		}
	} else {
		if err := c.Compile(items[0]); err != nil {
			return err
		}
	}
	for _, arg := range items[1:] {
		if err := c.Compile(arg); err != nil {
			return err
		}
	}
	argc := len(items) - 1
	c.emit(vm.OpCall, argc)
	return nil
}

// compileNativeOp emits a zero-stack-effect head freeze, then each argument, then
// the fused native opcode. Lisp-2 freezes the function cell; Lisp-1 freezes the
// value cell. The VM dispatches natively only when that frozen head was canonical.
//
// A 2-arg comparison or arithmetic op whose operands are both a local or a
// scalar constant instead collapses the whole shape into one
// OpFusedNativeOp — see fuseNativeOp.
func (c *Compiler) compileNativeOp(items []core.Value, op vm.Opcode) error {
	if c.err != nil {
		return c.err
	}
	sym := items[0].(core.Symbol)
	args := items[1:]
	if ok, err := c.fuseNativeOp(sym, op, args); ok || err != nil {
		return err
	}

	if c.dialect != nil && c.dialect.IsLisp2() {
		c.emit(vm.OpFreezeNativeFunc, c.chunk.AddConstant(sym))
	} else {
		c.emit(vm.OpFreezeNative, c.chunk.AddConstant(sym))
	}

	for _, arg := range args {
		if err := c.Compile(arg); err != nil {
			return err
		}
	}
	c.emit(op, len(args))
	return nil
}

// fuseNativeOp emits a single OpFusedNativeOp for a 2-arg comparison or
// arithmetic op whose operands are both either a local or a scalar constant,
// in place of compileNativeOp's usual freeze+operand+operand+op sequence.
// Reports ok == false for any shape it doesn't cover — N-ary forms, or an
// operand that's neither a local nor a scalar constant — leaving the caller
// to fall through to that unfused emission unchanged. op is always one of
// the 9 native opcodes here: compileCall's only caller resolves it via
// nativeOp(canonicalName) before reaching compileNativeOp, which is
// fuseNativeOp's only caller.
func (c *Compiler) fuseNativeOp(sym core.Symbol, op vm.Opcode, args []core.Value) (ok bool, err error) {
	if len(args) != 2 {
		return false, nil
	}
	aKind, aIdx, ok := c.fusedOperand(args[0])
	if !ok {
		return false, nil
	}
	bKind, bIdx, ok := c.fusedOperand(args[1])
	if !ok {
		return false, nil
	}
	idx := len(c.chunk.Fused)
	c.chunk.Fused = append(c.chunk.Fused, vm.FusedOp{
		Op:    op,
		Sym:   c.chunk.AddConstant(sym),
		Func:  c.dialect != nil && c.dialect.IsLisp2(),
		AKind: aKind,
		A:     aIdx,
		BKind: bKind,
		B:     bIdx,
	})
	c.emit(vm.OpFusedNativeOp, idx)
	return true, c.err
}

// fusedOperand reports whether v is eligible as a FusedOp operand: a local
// slot (an ancestor-captured free variable resolves through OpGetCap
// instead, a different address space than a stack slot, so it's not
// eligible here) or a scalar constant.
func (c *Compiler) fusedOperand(v core.Value) (vm.OperandKind, int, bool) {
	if sym, isSym := v.(core.Symbol); isSym {
		if idx := c.resolveLocal(sym.V); idx >= 0 {
			return vm.OperandLocal, idx, true
		}
		return 0, 0, false
	}
	switch v.(type) {
	case core.Int, core.Float, core.String, core.Keyword, core.Bool, core.Nil:
		return vm.OperandConst, c.chunk.AddConstant(v), true
	default:
		return 0, 0, false
	}
}

func (c *Compiler) resolveLocal(name string) int {
	for i := len(c.locals) - 1; i >= 0; i-- {
		if c.locals[i].name == name {
			return i
		}
	}
	return -1
}

// isLocallyShadowed returns true if name is bound as a local in this compiler
// or any parent compiler. Used to prevent native opcode emission when an
// enclosing scope shadows a native operator.
func (c *Compiler) isLocallyShadowed(name string) bool {
	if c.resolveLocal(name) >= 0 {
		return true
	}
	if c.parent != nil {
		return c.parent.isLocallyShadowed(name)
	}
	return false
}

func (c *Compiler) addLocal(name string) {
	c.locals = append(c.locals, local{name: name, depth: c.depth})
	c.chunk.Locals++
	c.chunk.LocalNames = append(c.chunk.LocalNames, name)
}

// emitBind emits the store for a local binding site, recording it so
// finalize boxes (rather than writes through) when the slot is captured.
func (c *Compiler) emitBind(slot int) {
	if c.err != nil {
		return
	}
	ip := c.emit(vm.OpSetLocal, slot)
	if c.err != nil {
		return
	}
	if c.binds == nil {
		c.binds = map[int]bool{}
	}
	c.binds[ip] = true
}

// markCaptured flags an own local slot as captured by a nested closure.
func (c *Compiler) markCaptured(slot int) {
	for len(c.chunk.Captured) <= slot {
		c.chunk.Captured = append(c.chunk.Captured, false)
	}
	c.chunk.Captured[slot] = true
}

// ancestorBinds reports whether any enclosing compiler binds name as a local.
func (c *Compiler) ancestorBinds(name string) bool {
	for p := c.parent; p != nil; p = p.parent {
		if p.resolveLocal(name) >= 0 {
			return true
		}
	}
	return false
}

// ensureCapture registers name in this chunk's capture list, returning its
// index, and extends chunk.Caps with the descriptor OpClosure needs to
// materialize it: the enclosing frame's slot when the parent binds name
// (marking that slot captured), or the parent's own capture index when the
// binding lives further out (transitive capture). Callers must have confirmed
// via ancestorBinds that the parent chain can supply name.
func (c *Compiler) ensureCapture(name string) int {
	for i, n := range c.caps {
		if n == name {
			return i
		}
	}
	p := c.parent
	var desc vm.CapDesc
	if slot := p.resolveLocal(name); slot >= 0 {
		p.markCaptured(slot)
		desc = vm.CapDesc{Slot: slot}
	} else {
		desc = vm.CapDesc{FromCaps: true, Cap: p.ensureCapture(name)}
	}
	c.caps = append(c.caps, name)
	c.chunk.Caps = append(c.chunk.Caps, desc)
	return len(c.caps) - 1
}

// finalize rewrites the finished chunk's code for captured locals: reads
// become OpGetCell, mutation stores OpSetCell, and recorded binding sites
// OpBindCell, leaving uncaptured slots on the plain local opcodes. It then
// computes MaxStack. Nested chunks are finalized by their own compilers right
// after their bodies complete, so a chunk's Captured set is complete by the
// time finalize runs.
func (c *Compiler) finalize() { _ = c.finalizeContext(context.Background()) }

func (c *Compiler) finalizeContext(ctx context.Context) error {
	chunk := c.chunk
	for ip, inst := range chunk.Code {
		op, a := inst.Op(), inst.A()
		if a >= len(chunk.Captured) || !chunk.Captured[a] {
			continue
		}
		switch op {
		case vm.OpGetLocal:
			chunk.Code[ip] = vm.Encode(vm.OpGetCell, a)
		case vm.OpSetLocal:
			if c.binds[ip] {
				chunk.Code[ip] = vm.Encode(vm.OpBindCell, a)
			} else {
				chunk.Code[ip] = vm.Encode(vm.OpSetCell, a)
			}
		}
	}
	chunk.MaxStack = computeMaxStack(chunk)
	chunk.NodeCount = c.nodeCount
	chunk.DeepBytes = chunkDeepBytesContext(ctx, chunk)
	return nil
}

func chunkDeepBytes(chunk *vm.Chunk) int64 { return chunkDeepBytesContext(context.Background(), chunk) }

func chunkDeepBytesContext(ctx context.Context, chunk *vm.Chunk) int64 {
	if chunk == nil {
		return 0
	}
	bytes := int64(len(chunk.Code))*core.MeterInstructionBytes + int64(len(chunk.Fused))*core.MeterFusedOpBytes + core.ValueSlotsBytes(len(chunk.Constants))
	for _, name := range chunk.LocalNames {
		bytes += core.StringShallowBytes(len(name))
	}
	for _, constant := range chunk.Constants {
		deep, err := core.ValueDeepBytesContext(ctx, constant)
		if err != nil {
			return 0
		}
		bytes += deep
	}
	bytes += core.ValueSlotsBytes(len(chunk.SubChunks))
	for _, sub := range chunk.SubChunks {
		bytes += chunkDeepBytes(sub)
	}
	return bytes
}

func isElse(v core.Value) bool {
	kw, ok := v.(core.Keyword)
	return ok && kw.V == "else"
}

// compileDefmacro emits a macro definition. Unlike a function, a macro's body
// is not compiled: it is evaluated at expansion time against the scope the
// macro was defined in, so the body travels as data. Everything except that
// scope is fixed here, in a prototype constant; the opcode fills the scope in
// at run time and binds through core.BindMacro, the same path the tree-walker
// takes, so both agree on when the chunk cache is invalidated.
func (c *Compiler) compileDefmacro(args []core.Value) error {
	if c.err != nil {
		return c.err
	}
	if len(args) < 3 {
		return compileErrf("defmacro requires at least 3 arguments (name params body...)")
	}
	// Only a defmacro that IS the form being compiled can be compiled. Macro
	// expansion is a pre-pass over the whole form, so a macro defined inside a
	// larger form is not yet bound when a sibling use within that same form is
	// expanded — the use would compile as a plain call and fail at run time
	// with "expected callable, got core.Macro". At the top of a form there is
	// no sibling to get this wrong. Nested definitions defer to the
	// tree-walker, which binds and expands in evaluation order.
	if c.compileDepth > 1 {
		return unsupportedErr("defmacro nested in a form is not supported by the bytecode compiler")
	}
	name, ok := args[0].(core.Symbol)
	if !ok {
		return compileErrf("defmacro: first argument must be a symbol")
	}
	fixed, variadic, err := parseParams(args[1])
	if err != nil {
		return err
	}
	proto := core.Macro{
		Name:     name.V,
		Params:   fixed,
		Variadic: variadic,
		Body:     args[2:],
	}
	op := vm.OpDefMacro
	if c.dialect != nil && c.dialect.IsLisp2() {
		op = vm.OpDefMacroFunc
	}
	c.emit(op, c.chunk.AddConstant(proto))
	return nil
}

func (c *Compiler) compileDefn(args []core.Value) error {
	if c.err != nil {
		return c.err
	}
	if len(args) < 2 {
		return compileErrf("defn: expected name and params")
	}
	name, ok := args[0].(core.Symbol)
	if !ok {
		return compileErrf("defn: name must be symbol, got %T", args[0])
	}
	if c.dialect != nil && c.dialect.IsLisp2() {
		// Lisp-2: compile fn closure then emit OpSetFunc for the function cell.
		params, variadic, err := parseParams(args[1])
		if err != nil {
			return err
		}
		body := args[2:]
		sub := NewCompiler("<fn>")
		if c.dialect != nil {
			sub = NewCompilerWithDialect("<fn>", c.dialect)
		}
		sub.parent = c
		sub.meter = c.meter
		for _, p := range params {
			sub.addLocal(p.V)
		}
		if variadic.V != "" {
			sub.addLocal(variadic.V)
		}
		for _, b := range body {
			if err := sub.Compile(b); err != nil {
				return err
			}
		}
		sub.emit(vm.OpReturn, 0)
		if sub.err != nil {
			return sub.err
		}
		sub.chunk.Arity = len(params)
		sub.chunk.Variadic = variadic.V != ""
		sub.chunk.EnsureSites()
		if err := sub.finalizeContext(c.ctx); err != nil {
			return err
		}
		idx := len(c.chunk.SubChunks)
		c.chunk.SubChunks = append(c.chunk.SubChunks, sub.chunk)
		c.emit(vm.OpClosure, idx)
		c.emit(vm.OpSetFunc, c.chunk.AddConstant(name))
		return nil
	}
	fnItems := append([]core.Value{core.Symbol{V: "fn"}, args[1]}, args[2:]...)
	def := core.NewList([]core.Value{core.Symbol{V: "def"}, name, core.NewList(fnItems)})
	return c.Compile(def)
}

func (c *Compiler) compileNot(args []core.Value) error {
	if c.err != nil {
		return c.err
	}
	if len(args) != 1 {
		return compileErrf("not: expected 1 argument, got %d", len(args))
	}
	if err := c.Compile(args[0]); err != nil {
		return err
	}
	jumpFalse := c.emitJump(vm.OpJumpIfFalse)
	c.emit(vm.OpFalse, 0)
	jumpEnd := c.emitJump(vm.OpJump)
	c.chunk.PatchJump(jumpFalse)
	c.emit(vm.OpTrue, 0)
	c.chunk.PatchJump(jumpEnd)
	return nil
}

func (c *Compiler) compileCond(args []core.Value) error {
	if c.err != nil {
		return c.err
	}
	clauses, err := c.condNormalizer()(args)
	if err != nil {
		return err
	}
	if len(clauses) == 0 {
		c.emit(vm.OpNil, 0)
		return nil
	}
	var jumps []int
	hasElse := false
	for _, clause := range clauses {
		items := clause.(core.List).ToSlice()
		test, expr := items[0], items[1]
		if isElse(test) {
			if err := c.Compile(expr); err != nil {
				return err
			}
			hasElse = true
			break
		}
		if err := c.Compile(test); err != nil {
			return err
		}
		jumpFalse := c.emitJump(vm.OpJumpIfFalse)
		if err := c.Compile(expr); err != nil {
			return err
		}
		jumps = append(jumps, c.emitJump(vm.OpJump))
		c.chunk.PatchJump(jumpFalse)
	}
	if !hasElse {
		c.emit(vm.OpNil, 0)
	}
	for _, jump := range jumps {
		c.chunk.PatchJump(jump)
	}
	return nil
}

func (c *Compiler) condNormalizer() func([]core.Value) ([]core.Value, error) {
	if c.dialect != nil {
		return c.dialect.NormalizeCond
	}
	return core.Dialect{}.NormalizeCond
}

func (c *Compiler) compileAnd(args []core.Value) error {
	if c.err != nil {
		return c.err
	}
	if len(args) == 0 {
		c.emit(vm.OpTrue, 0)
		return nil
	}
	if err := c.Compile(args[0]); err != nil {
		return err
	}
	var jumps []int
	for i := 1; i < len(args); i++ {
		c.emit(vm.OpDup, 0)
		jump := c.emitJump(vm.OpJumpIfFalse)
		jumps = append(jumps, jump)
		c.emit(vm.OpPop, 0)
		if err := c.Compile(args[i]); err != nil {
			return err
		}
	}
	for _, jump := range jumps {
		c.chunk.PatchJump(jump)
	}
	return nil
}

func (c *Compiler) compileOr(args []core.Value) error {
	if c.err != nil {
		return c.err
	}
	if len(args) == 0 {
		c.emit(vm.OpNil, 0)
		return nil
	}
	if err := c.Compile(args[0]); err != nil {
		return err
	}
	var jumpEnds []int
	for i := 1; i < len(args); i++ {
		c.emit(vm.OpDup, 0)
		jumpIfFalse := c.emitJump(vm.OpJumpIfFalse)
		jumpEnds = append(jumpEnds, c.emitJump(vm.OpJump))
		c.chunk.PatchJump(jumpIfFalse)
		c.emit(vm.OpPop, 0)
		if err := c.Compile(args[i]); err != nil {
			return err
		}
	}
	for _, jump := range jumpEnds {
		c.chunk.PatchJump(jump)
	}
	return nil
}

func (c *Compiler) compileQuasiquote(args []core.Value) error {
	if c.err != nil {
		return c.err
	}
	if len(args) != 1 {
		return compileErrf("quasiquote: expected 1 argument, got %d", len(args))
	}
	return c.compileQuasiquoteValue(args[0])
}

func (c *Compiler) compileQuasiquoteValue(v core.Value) error {
	if c.err != nil {
		return c.err
	}
	switch val := v.(type) {
	case core.List:
		n := val.Len()
		if n > 0 {
			if sym, ok := val.At(0).(core.Symbol); ok {
				if sym.V == "unquote" {
					if n != 2 {
						return fmt.Errorf("unquote: expected 1 argument")
					}
					return c.Compile(val.At(1))
				}
				if sym.V == "unquote-splicing" {
					return unsupportedErr("unquote-splicing: not yet supported in bytecode compiler")
				}
			}
		}
		if ok, err := c.compileConstantCollection(val, true); ok || err != nil {
			return err
		}
		if d := literalDepth(val, 0); d < 0 {
			return core.NewResourceLimitError("compile depth limit exceeded")
		}
		c.emit(vm.OpStructEnter, 1)
		items := val.ToSlice()
		for _, item := range items {
			if err := c.compileQuasiquoteValue(item); err != nil {
				return err
			}
		}
		c.emit(vm.OpMakeList, len(items))
		c.emit(vm.OpStructLeave, 1)
	case core.Vector:
		if ok, err := c.compileConstantCollection(val, true); ok || err != nil {
			return err
		}
		if d := literalDepth(val, 0); d < 0 {
			return core.NewResourceLimitError("compile depth limit exceeded")
		}
		c.emit(vm.OpStructEnter, 1)
		items := val.ToSlice()
		for _, item := range items {
			if err := c.compileQuasiquoteValue(item); err != nil {
				return err
			}
		}
		c.emit(vm.OpMakeVector, len(items))
		c.emit(vm.OpStructLeave, 1)
	case *core.HashMap:
		if ok, err := c.compileConstantCollection(val, true); ok || err != nil {
			return err
		}
		d := literalDepth(val, 0)
		if d < 0 {
			return core.NewResourceLimitError("compile depth limit exceeded")
		}
		c.emit(vm.OpStructEnter, d)
		c.emit(vm.OpConst, c.chunk.AddConstant(val))
		c.emit(vm.OpStructLeave, d)
	default:
		c.emit(vm.OpConst, c.chunk.AddConstant(val))
	}
	return nil
}

func (c *Compiler) compileConstantCollection(v core.Value, allowLists bool) (bool, error) {
	if !constantCollectionCandidate(v, allowLists) {
		return false, nil
	}
	folded, charge, ok, err := foldCompileTimeConstant(v, allowLists)
	if err != nil || !ok {
		return ok, err
	}
	d := literalDepth(folded, 0)
	if d < 0 {
		return true, core.NewResourceLimitError("compile depth limit exceeded")
	}
	c.emit(vm.OpStructEnter, d)
	c.emit(vm.OpConstCharged, c.chunk.AddChargedConstant(folded, charge))
	c.emit(vm.OpStructLeave, d)
	return true, c.err
}

func constantCollectionCandidate(v core.Value, allowLists bool) bool {
	switch v.(type) {
	case core.Vector, *core.HashMap:
		return true
	case core.List:
		return allowLists
	default:
		return false
	}
}

func foldCompileTimeConstant(v core.Value, allowLists bool) (core.Value, int64, bool, error) {
	switch val := v.(type) {
	case core.Nil, core.Bool, core.Int, core.Float, core.String, core.Keyword:
		return val, 0, true, nil
	case core.List:
		if !allowLists {
			return nil, 0, false, nil
		}
		items := val.ToSlice()
		folded := make([]core.Value, len(items))
		charge := core.ListShallowBytes(len(items))
		for i, item := range items {
			v, itemCharge, ok, err := foldCompileTimeConstant(item, allowLists)
			if err != nil || !ok {
				return nil, 0, ok, err
			}
			folded[i] = v
			charge += itemCharge
		}
		return core.NewList(folded), charge, true, nil
	case core.Vector:
		items := val.ToSlice()
		folded := make([]core.Value, len(items))
		charge := core.VectorShallowBytes(len(items))
		for i, item := range items {
			v, itemCharge, ok, err := foldCompileTimeConstant(item, allowLists)
			if err != nil || !ok {
				return nil, 0, ok, err
			}
			folded[i] = v
			charge += itemCharge
		}
		return core.NewVector(folded), charge, true, nil
	case *core.HashMap:
		pairs := val.Pairs()
		folded := core.NewHashMap()
		charge := core.HashMapShallowBytes(len(pairs))
		for _, pair := range pairs {
			k, keyCharge, ok, err := foldCompileTimeConstant(pair[0], allowLists)
			if err != nil || !ok {
				return nil, 0, ok, err
			}
			v, valueCharge, ok, err := foldCompileTimeConstant(pair[1], allowLists)
			if err != nil || !ok {
				return nil, 0, ok, err
			}
			if err := folded.Set(k, v); err != nil {
				return nil, 0, false, err
			}
			charge += keyCharge + valueCharge
		}
		return folded, charge, true, nil
	default:
		return nil, 0, false, nil
	}
}

func literalDepth(v core.Value, depth int) int {
	if depth > core.MaxCompileDepth {
		return -1
	}
	switch val := v.(type) {
	case core.List:
		max := 0
		for _, item := range val.ToSlice() {
			d := literalDepth(item, depth+1)
			if d < 0 {
				return -1
			}
			if d > max {
				max = d
			}
		}
		return max + 1
	case core.Vector:
		max := 0
		for _, item := range val.ToSlice() {
			d := literalDepth(item, depth+1)
			if d < 0 {
				return -1
			}
			if d > max {
				max = d
			}
		}
		return max + 1
	case *core.HashMap:
		max := 0
		for _, pair := range val.Pairs() {
			d := literalDepth(pair[0], depth+1)
			if d < 0 {
				return -1
			}
			if d > max {
				max = d
			}
			d = literalDepth(pair[1], depth+1)
			if d < 0 {
				return -1
			}
			if d > max {
				max = d
			}
		}
		return max + 1
	default:
		return 0
	}
}

// CompileAll compiles each of forms into its own top-level vm.Chunk.
func CompileAll(forms []core.Value) ([]*vm.Chunk, error) {
	chunks := make([]*vm.Chunk, 0, len(forms))
	for _, form := range forms {
		comp := NewCompiler("<top>")
		if err := comp.Compile(form); err != nil {
			return nil, err
		}
		comp.emit(vm.OpReturn, 0)
		if comp.err != nil {
			return nil, comp.err
		}
		comp.chunk.EnsureSites()
		comp.finalize()
		if err := comp.chunk.Validate(); err != nil {
			return nil, err
		}
		chunks = append(chunks, comp.chunk)
	}
	return chunks, nil
}

func parseParams(v core.Value) (params []core.Symbol, variadic core.Symbol, err error) {
	var items []core.Value
	switch val := v.(type) {
	case core.Vector:
		items = val.ToSlice()
	case core.List:
		items = val.ToSlice()
	default:
		return nil, core.Symbol{}, compileErrf("fn params must be vector or list, got %T", v)
	}
	for i, item := range items {
		sym, ok := item.(core.Symbol)
		if !ok {
			return nil, core.Symbol{}, compileErrf("fn param must be symbol, got %T", item)
		}
		if sym.V == "&" {
			if i+1 >= len(items) {
				return nil, core.Symbol{}, compileErrf("fn: & requires a rest param name")
			}
			rest, ok := items[i+1].(core.Symbol)
			if !ok {
				return nil, core.Symbol{}, core.NewTypeError("symbol", items[i+1])
			}
			variadic = rest
			break
		}
		params = append(params, sym)
	}
	return params, variadic, nil
}

// nativeOp returns the VM opcode for a native operator name, or false if
// the name is not a compile-time native operator.
func nativeOp(name string) (vm.Opcode, bool) {
	switch name {
	case "+":
		return vm.OpAdd, true
	case "-":
		return vm.OpSub, true
	case "*":
		return vm.OpMul, true
	case "/":
		return vm.OpDiv, true
	case "<":
		return vm.OpLt, true
	case ">":
		return vm.OpGt, true
	case "<=":
		return vm.OpLe, true
	case ">=":
		return vm.OpGe, true
	case "=":
		return vm.OpEq, true
	default:
		return 0, false
	}
}
