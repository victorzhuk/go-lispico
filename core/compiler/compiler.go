// Package compiler compiles core AST Values into vm bytecode chunks.
package compiler

import (
	"fmt"

	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/core/vm"
)

// CodeUnsupported identifies a *core.LispicoError for a form the bytecode
// compiler does not support (defmacro nested in a body, unquote-splicing).
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
	chunk   *vm.Chunk
	locals  []local
	depth   int
	parent  *Compiler
	loops   []loopFrame
	dialect *core.Dialect
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
	return &Compiler{chunk: &vm.Chunk{Name: name, FullEnv: true}}
}

// NewCompilerWithDialect creates a Compiler that emits into a new chunk named name
// with access to the dialect for dialect-dependent compilation.
func NewCompilerWithDialect(name string, dialect *core.Dialect) *Compiler {
	return &Compiler{chunk: &vm.Chunk{Name: name, FullEnv: true, Truthiness: dialect.TruthyFunc()}, dialect: dialect}
}

// Chunk returns the chunk the compiler is emitting into.
func (c *Compiler) Chunk() *vm.Chunk { return c.chunk }

// MarkCaptures runs capture analysis on the finished chunk, clearing the
// conservative FullEnv default when no local is captured by a nested closure.
// Without it a top-level chunk mirrors every local to the env on each write —
// wasteful for hot loops that only rebind stack slots. Must be called once Code
// is final. CompileAll does this per form; single-form callers must too.
func (c *Compiler) MarkCaptures() { markCaptures(c.chunk, nil) }

// emitGetGlobal emits OpGetGlobal for sym. The VM's site cache is built later
// from Code (Chunk.EnsureSites), so no per-site bookkeeping is needed here.
func (c *Compiler) emitGetGlobal(sym core.Symbol) {
	c.chunk.Emit(vm.OpGetGlobal, c.chunk.AddConstant(sym))
}

// Compile emits bytecode for form into the compiler's chunk.
func (c *Compiler) Compile(form core.Value) error {
	switch f := form.(type) {
	case core.Nil:
		c.chunk.Emit(vm.OpNil, 0)
	case core.Bool:
		if f.V {
			c.chunk.Emit(vm.OpTrue, 0)
		} else {
			c.chunk.Emit(vm.OpFalse, 0)
		}
	case core.Int, core.Float, core.String, core.Keyword:
		c.chunk.Emit(vm.OpConst, c.chunk.AddConstant(form))

	case core.Symbol:
		if idx := c.resolveLocal(f.V); idx >= 0 {
			c.chunk.Emit(vm.OpGetLocal, idx)
		} else {
			c.emitGetGlobal(f)
		}

	case core.List:
		return c.compileList(f)

	case core.Vector:
		c.chunk.Emit(vm.OpStructEnter, 1)
		for _, item := range f.Items {
			if err := c.Compile(item); err != nil {
				return err
			}
		}
		c.chunk.Emit(vm.OpMakeVector, len(f.Items))
		c.chunk.Emit(vm.OpStructLeave, 1)
	case *core.HashMap:
		c.chunk.Emit(vm.OpStructEnter, 1)
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
		c.chunk.Emit(vm.OpMakeMap, len(pairs))
		c.chunk.Emit(vm.OpStructLeave, 1)
	default:
		return compileErrf("compile: unknown form type %T", form)
	}
	return nil
}

func (c *Compiler) compileList(f core.List) error {
	if len(f.Items) == 0 {
		c.chunk.Emit(vm.OpNil, 0)
		return nil
	}
	head, isSym := f.Items[0].(core.Symbol)
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
				return c.compileIf(f.Items[1:])
			case "def":
				return c.compileDef(f.Items[1:])
			case "defn":
				return c.compileDefn(f.Items[1:])
			case "fn":
				return c.compileFn(f.Items[1:])
			case "function":
				if len(f.Items[1:]) != 1 {
					return fmt.Errorf("function: requires exactly 1 argument")
				}
				sym, ok := f.Items[1].(core.Symbol)
				if !ok {
					return fmt.Errorf("function: argument must be symbol, got %T", f.Items[1])
				}
				c.chunk.Emit(vm.OpGetFunc, c.chunk.AddConstant(sym))
				return nil
			case "funcall":
				// funcall evaluates its first argument as a value expression and calls it.
				if len(f.Items[1:]) < 1 {
					return fmt.Errorf("funcall: requires at least 1 argument")
				}
				if err := c.Compile(f.Items[1]); err != nil {
					return err
				}
				for _, arg := range f.Items[2:] {
					if err := c.Compile(arg); err != nil {
						return err
					}
				}
				c.chunk.Emit(vm.OpCall, len(f.Items[2:]))
				return nil
			case "let":
				return c.compileLet(f.Items[1:])
			case "let*":
				return c.compileLetStar(f.Items[1:])
			case "do":
				return c.compileDo(f.Items[1:])
			case "quote":
				if len(f.Items) < 2 {
					return compileErrf("quote: missing value")
				}
				c.chunk.Emit(vm.OpConst, c.chunk.AddConstant(f.Items[1]))
				return nil
			case "cond":
				return c.compileCond(f.Items[1:])
			case "and":
				return c.compileAnd(f.Items[1:])
			case "or":
				return c.compileOr(f.Items[1:])
			case "not":
				return c.compileNot(f.Items[1:])
			case "quasiquote":
				return c.compileQuasiquote(f.Items[1:])
			case "set!":
				return c.compileSet(f.Items[1:])
			case "when":
				return c.compileWhen(f.Items[1:])
			case "unless":
				return c.compileUnless(f.Items[1:])
			case "loop":
				return c.compileLoop(f.Items[1:])
			case "recur":
				return c.compileRecur(f.Items[1:])
			case "try":
				return c.compileTry(f.Items[1:])
			case "throw":
				return c.compileThrow(f.Items[1:])
			case "catch":
				return compileErrf("catch used outside of try")
			case "defmacro":
				return unsupportedErr("defmacro is not supported by the bytecode compiler")
			}
		}
		// +, -, *, etc. aren't special forms, so a configured dialect marks
		// isSpecial false for them and the switch above never runs; a form
		// that IS a real special form already returned from the switch, so
		// this can't misfire. Skip only when locally shadowed, falling back
		// to compileCall/OpCall.
		if op, ok := nativeOp(canonicalName); ok && !c.isLocallyShadowed(canonicalName) {
			return c.compileNativeOp(f.Items, op)
		}
	}
	return c.compileCall(f.Items)
}

func (c *Compiler) compileIf(args []core.Value) error {
	if len(args) < 2 {
		return compileErrf("if: expected condition and then branch, got %d args", len(args))
	}
	if err := c.Compile(args[0]); err != nil {
		return err
	}
	jumpFalse := c.chunk.EmitJump(vm.OpJumpIfFalse)
	if err := c.Compile(args[1]); err != nil {
		return err
	}
	jumpEnd := c.chunk.EmitJump(vm.OpJump)
	c.chunk.PatchJump(jumpFalse)
	if len(args) > 2 {
		if err := c.Compile(args[2]); err != nil {
			return err
		}
	} else {
		c.chunk.Emit(vm.OpNil, 0)
	}
	c.chunk.PatchJump(jumpEnd)
	return nil
}

func (c *Compiler) compileDef(args []core.Value) error {
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
	c.chunk.Emit(vm.OpSetGlobal, c.chunk.AddConstant(sym))
	return nil
}

func (c *Compiler) compileFn(args []core.Value) error {
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
	sub.chunk.Emit(vm.OpReturn, 0)
	sub.chunk.Arity = len(params)
	sub.chunk.Variadic = variadic.V != ""
	sub.chunk.EnsureSites()
	markCaptures(sub.chunk, collectAncestors(c))
	idx := len(c.chunk.SubChunks)
	c.chunk.SubChunks = append(c.chunk.SubChunks, sub.chunk)
	c.chunk.Emit(vm.OpClosure, idx)
	return nil
}

func (c *Compiler) compileDo(args []core.Value) error {
	for i, form := range args {
		if err := c.Compile(form); err != nil {
			return err
		}
		if i < len(args)-1 {
			c.chunk.Emit(vm.OpPop, 0)
		}
	}
	if len(args) == 0 {
		c.chunk.Emit(vm.OpNil, 0)
	}
	return nil
}

func (c *Compiler) compileLet(args []core.Value) error {
	if len(args) == 0 {
		return compileErrf("compile let: missing bindings vector")
	}
	bindings, ok := args[0].(core.Vector)
	if !ok {
		return compileErrf("compile let: bindings must be vector")
	}
	if len(bindings.Items)%2 != 0 {
		return compileErrf("compile let: bindings must have even count")
	}
	c.depth++
	base := len(c.locals)
	// Parallel binding: compile every init before registering any local, so an
	// init resolves names in the enclosing scope, not in a sibling binding.
	for i := 0; i < len(bindings.Items); i += 2 {
		if err := c.Compile(bindings.Items[i+1]); err != nil {
			return err
		}
		if _, ok := bindings.Items[i].(core.Symbol); !ok {
			return core.NewTypeError("symbol", bindings.Items[i])
		}
		c.chunk.Emit(vm.OpSetLocal, base+i/2)
	}
	for i := 0; i < len(bindings.Items); i += 2 {
		c.addLocal(bindings.Items[i].(core.Symbol).V)
	}
	if err := c.compileDo(args[1:]); err != nil {
		return err
	}
	c.locals = c.locals[:base]
	c.depth--
	return nil
}

func (c *Compiler) compileLetStar(args []core.Value) error {
	if len(args) < 2 {
		return compileErrf("let*: expected bindings and body")
	}
	bindings, ok := args[0].(core.Vector)
	if !ok {
		return compileErrf("compile let*: bindings must be vector")
	}
	if len(bindings.Items)%2 != 0 {
		return compileErrf("compile let*: bindings must have even count")
	}
	c.depth++
	base := len(c.locals)
	for i := 0; i < len(bindings.Items); i += 2 {
		if err := c.Compile(bindings.Items[i+1]); err != nil {
			return err
		}
		sym, ok := bindings.Items[i].(core.Symbol)
		if !ok {
			return core.NewTypeError("symbol", bindings.Items[i])
		}
		c.addLocal(sym.V)
		c.chunk.Emit(vm.OpSetLocal, len(c.locals)-1)
	}
	if err := c.compileDo(args[1:]); err != nil {
		return err
	}
	c.locals = c.locals[:base]
	c.depth--
	return nil
}

func (c *Compiler) compileSet(args []core.Value) error {
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
		c.chunk.Emit(vm.OpSetLocal, idx)
	} else {
		c.chunk.Emit(vm.OpSetLexical, c.chunk.AddConstant(sym))
	}
	return nil
}

func (c *Compiler) compileWhen(args []core.Value) error {
	if len(args) == 0 {
		return compileErrf("when: missing condition")
	}
	if err := c.Compile(args[0]); err != nil {
		return err
	}
	jumpFalse := c.chunk.EmitJump(vm.OpJumpIfFalse)
	if err := c.compileDo(args[1:]); err != nil {
		return err
	}
	jumpEnd := c.chunk.EmitJump(vm.OpJump)
	c.chunk.PatchJump(jumpFalse)
	c.chunk.Emit(vm.OpNil, 0)
	c.chunk.PatchJump(jumpEnd)
	return nil
}

func (c *Compiler) compileUnless(args []core.Value) error {
	if len(args) == 0 {
		return compileErrf("unless: missing condition")
	}
	if err := c.Compile(args[0]); err != nil {
		return err
	}
	jumpFalse := c.chunk.EmitJump(vm.OpJumpIfFalse)
	c.chunk.Emit(vm.OpNil, 0)
	jumpEnd := c.chunk.EmitJump(vm.OpJump)
	c.chunk.PatchJump(jumpFalse)
	if err := c.compileDo(args[1:]); err != nil {
		return err
	}
	c.chunk.PatchJump(jumpEnd)
	return nil
}

func (c *Compiler) compileLoop(args []core.Value) error {
	if len(args) < 2 {
		return compileErrf("loop: expected binding vector and body")
	}
	bindings, ok := args[0].(core.Vector)
	if !ok || len(bindings.Items)%2 != 0 {
		return compileErrf("loop: first argument must be an even-length binding vector")
	}
	var slots []int
	for i := 0; i < len(bindings.Items); i += 2 {
		name, ok := bindings.Items[i].(core.Symbol)
		if !ok {
			return compileErrf("loop: binding names must be symbols")
		}
		if err := c.Compile(bindings.Items[i+1]); err != nil {
			return err
		}
		slots = append(slots, len(c.locals))
		c.addLocal(name.V)
		c.chunk.Emit(vm.OpSetLocal, len(c.locals)-1)
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
		c.chunk.Emit(vm.OpSetLocal, loop.slots[i])
		c.chunk.Emit(vm.OpPop, 0)
	}
	c.chunk.EmitLoop(loop.start)
	return nil
}

func (c *Compiler) compileTry(args []core.Value) error {
	if len(args) < 2 {
		return compileErrf("try: expected body and catch clause")
	}
	catchClause, ok := args[len(args)-1].(core.List)
	if !ok || len(catchClause.Items) < 3 {
		return compileErrf("try: last argument must be (catch <sym> <handler>...)")
	}
	head, ok := catchClause.Items[0].(core.Symbol)
	if !ok || head.V != "catch" {
		return compileErrf("try: expected catch clause, got %v", catchClause.Items[0])
	}
	errSymIndex := 1
	bodyStart := 2
	if len(catchClause.Items) >= 4 {
		errSymIndex = 2
		bodyStart = 3
	}
	errSym, ok := catchClause.Items[errSymIndex].(core.Symbol)
	if !ok {
		return compileErrf("catch: error binding must be a symbol")
	}
	body := args[:len(args)-1]

	base := len(c.locals)
	setup := c.chunk.EmitJump(vm.OpSetupTry)
	if err := c.compileDo(body); err != nil {
		return err
	}
	c.chunk.Emit(vm.OpPopTry, 0)
	skip := c.chunk.EmitJump(vm.OpJump)
	handlerAddr := len(c.chunk.Code)
	c.chunk.PatchJumpTo(setup, handlerAddr)
	catchSlot := len(c.locals)
	c.addLocal(errSym.V)
	c.chunk.Emit(vm.OpSetLocal, catchSlot)
	if err := c.compileDo(catchClause.Items[bodyStart:]); err != nil {
		return err
	}
	c.locals = c.locals[:base]
	c.chunk.PatchJump(skip)
	return nil
}

func (c *Compiler) compileThrow(args []core.Value) error {
	if len(args) != 1 {
		return compileErrf("throw: expected 1 argument, got %d", len(args))
	}
	if err := c.Compile(args[0]); err != nil {
		return err
	}
	c.chunk.Emit(vm.OpThrow, 0)
	return nil
}

func (c *Compiler) compileCall(items []core.Value) error {
	// Lisp-2: emit OpGetFunc for the head symbol instead of OpGetGlobal.
	if c.dialect != nil && c.dialect.IsLisp2() {
		if sym, ok := items[0].(core.Symbol); ok {
			c.chunk.Emit(vm.OpGetFunc, c.chunk.AddConstant(sym))
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
	c.chunk.Emit(vm.OpCall, argc)
	return nil
}

// compileNativeOp emits a native arithmetic/comparison opcode for a list form
// whose head is a non-shadowed symbol. It emits the operator's head lookup
// (OpGetFunc under a Lisp-2 dialect — CL resolves callable heads through the
// function cell, not the value cell, so a defun rebind must be observed there
// too; OpGetGlobal otherwise), preserving head-resolution order and freezing
// the operator's canonical-native eligibility at that point, then compiles
// each argument, then the native opcode with operand = number of arguments.
// The VM dispatches natively only when the operator was frozen as canonical,
// else it calls the pushed operator value.
func (c *Compiler) compileNativeOp(items []core.Value, op vm.Opcode) error {
	sym := items[0].(core.Symbol)
	if c.dialect != nil && c.dialect.IsLisp2() {
		c.chunk.Emit(vm.OpGetFunc, c.chunk.AddConstant(sym))
	} else {
		c.emitGetGlobal(sym)
	}

	for _, arg := range items[1:] {
		if err := c.Compile(arg); err != nil {
			return err
		}
	}
	c.chunk.Emit(op, len(items[1:]))
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

// captureAncestor carries a chunk and its local names for capture analysis.
// Used by markCaptures to walk the ancestor chain of nested closures.
type captureAncestor struct {
	chunk  *vm.Chunk
	locals []string
}

// collectAncestors walks the Compiler parent chain to build a slice of
// captureAncestor entries. The caller's own chunk appears first, then its
// parent, etc., matching the order capture checks apply.
func collectAncestors(c *Compiler) []captureAncestor {
	var ancestors []captureAncestor
	for p := c; p != nil; p = p.parent {
		ancestors = append(ancestors, captureAncestor{chunk: p.chunk, locals: p.chunk.LocalNames})
	}
	return ancestors
}

// markCaptures walks the chunk tree rooted at root, identifying locals in each
// ancestor that are referenced by OpGetGlobal instructions in descendant
// (closure) chunks. Those locals are marked as captured so the VM knows to
// mirror them to an Env. After analysis, FullEnv is cleared for chunks where
// no captures were found.
// The ancestors slice holds chunks from the compiler parent chain that may
// contain locals referenced by closures within the root.
func markCaptures(root *vm.Chunk, ancestors []captureAncestor) {
	walkCaptureTree(root, ancestors)
}

func walkCaptureTree(chunk *vm.Chunk, ancestors []captureAncestor) {
	// Recurse into children first — they report captures into ancestors.
	for _, child := range chunk.SubChunks {
		childAncestors := make([]captureAncestor, 0, len(ancestors)+1)
		childAncestors = append(childAncestors, captureAncestor{chunk: chunk, locals: chunk.LocalNames})
		childAncestors = append(childAncestors, ancestors...)
		walkCaptureTree(child, childAncestors)
	}

	// Check each OpGetGlobal/OpSetLexical against ancestor local names.
	for _, inst := range chunk.Code {
		if inst.Op() != vm.OpGetGlobal && inst.Op() != vm.OpSetLexical {
			continue
		}
		sym, err := chunk.GetSymbolConstant(inst.A())
		if err != nil {
			continue
		}
		for _, anc := range ancestors {
			for slot, name := range anc.locals {
				if name == sym.V {
					if anc.chunk.Captured == nil {
						anc.chunk.Captured = make([]bool, anc.chunk.Locals)
					}
					anc.chunk.Captured[slot] = true
				}
			}
		}
	}

	// Clear FullEnv when capture analysis found nothing to capture.
	if chunk.FullEnv {
		has := false
		for _, c := range chunk.Captured {
			if c {
				has = true
				break
			}
		}
		if !has {
			chunk.FullEnv = false
		}
	}

	chunk.MaxStack = computeMaxStack(chunk)
}

func isElse(v core.Value) bool {
	switch x := v.(type) {
	case core.Symbol:
		return x.V == "else" || x.V == ":else"
	case core.Keyword:
		return x.V == "else"
	}
	return false
}

func (c *Compiler) compileDefn(args []core.Value) error {
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
		sub.chunk.Emit(vm.OpReturn, 0)
		sub.chunk.Arity = len(params)
		sub.chunk.Variadic = variadic.V != ""
		sub.chunk.EnsureSites()
		markCaptures(sub.chunk, collectAncestors(c))
		idx := len(c.chunk.SubChunks)
		c.chunk.SubChunks = append(c.chunk.SubChunks, sub.chunk)
		c.chunk.Emit(vm.OpClosure, idx)
		c.chunk.Emit(vm.OpSetFunc, c.chunk.AddConstant(name))
		return nil
	}
	fnItems := append([]core.Value{core.Symbol{V: "fn"}, args[1]}, args[2:]...)
	def := core.List{Items: []core.Value{core.Symbol{V: "def"}, name, core.List{Items: fnItems}}}
	return c.Compile(def)
}

func (c *Compiler) compileNot(args []core.Value) error {
	if len(args) != 1 {
		return compileErrf("not: expected 1 argument, got %d", len(args))
	}
	if err := c.Compile(args[0]); err != nil {
		return err
	}
	jumpFalse := c.chunk.EmitJump(vm.OpJumpIfFalse)
	c.chunk.Emit(vm.OpFalse, 0)
	jumpEnd := c.chunk.EmitJump(vm.OpJump)
	c.chunk.PatchJump(jumpFalse)
	c.chunk.Emit(vm.OpTrue, 0)
	c.chunk.PatchJump(jumpEnd)
	return nil
}

func (c *Compiler) compileCond(args []core.Value) error {
	clauses, err := c.condNormalizer()(args)
	if err != nil {
		return err
	}
	if len(clauses) == 0 {
		c.chunk.Emit(vm.OpNil, 0)
		return nil
	}
	var jumps []int
	hasElse := false
	for _, clause := range clauses {
		items := clause.(core.List).Items
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
		jumpFalse := c.chunk.EmitJump(vm.OpJumpIfFalse)
		if err := c.Compile(expr); err != nil {
			return err
		}
		jumps = append(jumps, c.chunk.EmitJump(vm.OpJump))
		c.chunk.PatchJump(jumpFalse)
	}
	if !hasElse {
		c.chunk.Emit(vm.OpNil, 0)
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
	if len(args) == 0 {
		c.chunk.Emit(vm.OpTrue, 0)
		return nil
	}
	if err := c.Compile(args[0]); err != nil {
		return err
	}
	var jumps []int
	for i := 1; i < len(args); i++ {
		c.chunk.Emit(vm.OpDup, 0)
		jump := c.chunk.EmitJump(vm.OpJumpIfFalse)
		jumps = append(jumps, jump)
		c.chunk.Emit(vm.OpPop, 0)
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
	if len(args) == 0 {
		c.chunk.Emit(vm.OpNil, 0)
		return nil
	}
	if err := c.Compile(args[0]); err != nil {
		return err
	}
	var jumpEnds []int
	for i := 1; i < len(args); i++ {
		c.chunk.Emit(vm.OpDup, 0)
		jumpIfFalse := c.chunk.EmitJump(vm.OpJumpIfFalse)
		jumpEnds = append(jumpEnds, c.chunk.EmitJump(vm.OpJump))
		c.chunk.PatchJump(jumpIfFalse)
		c.chunk.Emit(vm.OpPop, 0)
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
	if len(args) != 1 {
		return compileErrf("quasiquote: expected 1 argument, got %d", len(args))
	}
	return c.compileQuasiquoteValue(args[0])
}

func (c *Compiler) compileQuasiquoteValue(v core.Value) error {
	switch val := v.(type) {
	case core.List:
		if len(val.Items) > 0 {
			if sym, ok := val.Items[0].(core.Symbol); ok {
				if sym.V == "unquote" {
					if len(val.Items) != 2 {
						return fmt.Errorf("unquote: expected 1 argument")
					}
					return c.Compile(val.Items[1])
				}
				if sym.V == "unquote-splicing" {
					return unsupportedErr("unquote-splicing: not yet supported in bytecode compiler")
				}
			}
		}
		c.chunk.Emit(vm.OpStructEnter, 1)
		for _, item := range val.Items {
			if err := c.compileQuasiquoteValue(item); err != nil {
				return err
			}
		}
		c.chunk.Emit(vm.OpMakeList, len(val.Items))
		c.chunk.Emit(vm.OpStructLeave, 1)
	case core.Vector:
		c.chunk.Emit(vm.OpStructEnter, 1)
		for _, item := range val.Items {
			if err := c.compileQuasiquoteValue(item); err != nil {
				return err
			}
		}
		c.chunk.Emit(vm.OpMakeVector, len(val.Items))
		c.chunk.Emit(vm.OpStructLeave, 1)
	case *core.HashMap:
		d := literalDepth(val)
		c.chunk.Emit(vm.OpStructEnter, d)
		c.chunk.Emit(vm.OpConst, c.chunk.AddConstant(val))
		c.chunk.Emit(vm.OpStructLeave, d)
	default:
		c.chunk.Emit(vm.OpConst, c.chunk.AddConstant(val))
	}
	return nil
}

func literalDepth(v core.Value) int {
	switch val := v.(type) {
	case core.List:
		max := 0
		for _, item := range val.Items {
			if d := literalDepth(item); d > max {
				max = d
			}
		}
		return max + 1
	case core.Vector:
		max := 0
		for _, item := range val.Items {
			if d := literalDepth(item); d > max {
				max = d
			}
		}
		return max + 1
	case *core.HashMap:
		max := 0
		for _, pair := range val.Pairs() {
			if d := literalDepth(pair[0]); d > max {
				max = d
			}
			if d := literalDepth(pair[1]); d > max {
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
		comp.chunk.Emit(vm.OpReturn, 0)
		comp.chunk.EnsureSites()
		markCaptures(comp.chunk, nil)
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
		items = val.Items
	case core.List:
		items = val.Items
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
