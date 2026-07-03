// Package compiler compiles core AST Values into vm bytecode chunks.
package compiler

import (
	"fmt"

	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/core/vm"
)

// Compiler compiles core.Value forms into a single vm.Chunk, tracking local
// variable scopes as it goes. It implements vm.FormCompiler.
type Compiler struct {
	chunk  *vm.Chunk
	locals []local
	depth  int
	parent *Compiler
}

type local struct {
	name  string
	depth int
}

// NewCompiler creates a Compiler that emits into a new chunk named name.
func NewCompiler(name string) *Compiler {
	return &Compiler{chunk: &vm.Chunk{Name: name}}
}

// Chunk returns the chunk the compiler is emitting into.
func (c *Compiler) Chunk() *vm.Chunk { return c.chunk }

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
			c.chunk.Emit(vm.OpGetGlobal, c.chunk.AddConstant(f))
		}

	case core.List:
		return c.compileList(f)

	case core.Vector:
		for _, item := range f.Items {
			if err := c.Compile(item); err != nil {
				return err
			}
		}
		c.chunk.Emit(vm.OpMakeVector, len(f.Items))

	case *core.HashMap:
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

	default:
		return fmt.Errorf("compile: unknown form type %T", form)
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
		switch head.V {
		case "if":
			return c.compileIf(f.Items[1:])
		case "def":
			return c.compileDef(f.Items[1:])
		case "fn":
			return c.compileFn(f.Items[1:])
		case "let":
			return c.compileLet(f.Items[1:])
		case "let*":
			return c.compileLetStar(f.Items[1:])
		case "do":
			return c.compileDo(f.Items[1:])
		case "quote":
			c.chunk.Emit(vm.OpConst, c.chunk.AddConstant(f.Items[1]))
			return nil
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
		}
	}
	return c.compileCall(f.Items)
}

func (c *Compiler) compileIf(args []core.Value) error {
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
		return fmt.Errorf("compile def: expected 2 args, got %d", len(args))
	}
	sym, ok := args[0].(core.Symbol)
	if !ok {
		return fmt.Errorf("compile def: name must be symbol, got %T", args[0])
	}
	if err := c.Compile(args[1]); err != nil {
		return err
	}
	c.chunk.Emit(vm.OpSetGlobal, c.chunk.AddConstant(sym))
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
	sub.chunk.Emit(vm.OpReturn, 0)
	sub.chunk.Arity = len(params)
	sub.chunk.Variadic = variadic.V != ""

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

func (c *Compiler) compileLetStar(args []core.Value) error {
	return c.compileLet(args)
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
		c.chunk.Emit(vm.OpSetLocal, idx)
	} else {
		c.chunk.Emit(vm.OpSetGlobal, c.chunk.AddConstant(sym))
	}
	return nil
}

func (c *Compiler) compileWhen(args []core.Value) error {
	if err := c.Compile(args[0]); err != nil {
		return err
	}
	jump := c.chunk.EmitJump(vm.OpJumpIfFalse)
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
	jumpFalse := c.chunk.EmitJump(vm.OpJumpIfFalse)
	jumpOver := c.chunk.EmitJump(vm.OpJump)
	c.chunk.PatchJump(jumpFalse)
	if err := c.compileDo(args[1:]); err != nil {
		return err
	}
	c.chunk.PatchJump(jumpOver)
	return nil
}

func (c *Compiler) compileLoop(args []core.Value) error {
	return c.compileLet(args)
}

func (c *Compiler) compileRecur(args []core.Value) error {
	for _, arg := range args {
		if err := c.Compile(arg); err != nil {
			return err
		}
	}
	c.chunk.Emit(vm.OpTailCall, len(args))
	return nil
}

func (c *Compiler) compileTry(args []core.Value) error {
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
	c.chunk.Emit(vm.OpCall, argc)
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
	c.chunk.LocalNames = append(c.chunk.LocalNames, name)
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
			rest, ok := vec.Items[i+1].(core.Symbol)
			if !ok {
				return nil, core.Symbol{}, core.NewTypeError("symbol", vec.Items[i+1])
			}
			variadic = rest
			break
		}
		params = append(params, sym)
	}
	return params, variadic, nil
}

// MacroExpander expands macro forms before compilation.
type MacroExpander interface {
	Expand(form core.Value) (core.Value, error)
}

// CompileExpanded expands form through expander, then compiles the result.
func (c *Compiler) CompileExpanded(expander MacroExpander, form core.Value) error {
	expanded, err := expander.Expand(form)
	if err != nil {
		return err
	}
	return c.Compile(expanded)
}
