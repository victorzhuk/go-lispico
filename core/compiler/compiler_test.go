package compiler

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/core/vm"
)

func TestCompiler_Nil(t *testing.T) {
	c := NewCompiler("test")
	require.NoError(t, c.Compile(core.Nil{}))

	chunk := c.Chunk()
	require.Len(t, chunk.Code, 1)
	assert.Equal(t, vm.OpNil, chunk.Code[0].Op())
}

func TestCompiler_Bool(t *testing.T) {
	t.Run("true", func(t *testing.T) {
		c := NewCompiler("test")
		require.NoError(t, c.Compile(core.Bool{V: true}))

		chunk := c.Chunk()
		require.Len(t, chunk.Code, 1)
		assert.Equal(t, vm.OpTrue, chunk.Code[0].Op())
	})

	t.Run("false", func(t *testing.T) {
		c := NewCompiler("test")
		require.NoError(t, c.Compile(core.Bool{V: false}))

		chunk := c.Chunk()
		require.Len(t, chunk.Code, 1)
		assert.Equal(t, vm.OpFalse, chunk.Code[0].Op())
	})
}

func TestCompiler_Int(t *testing.T) {
	c := NewCompiler("test")
	require.NoError(t, c.Compile(core.Int{V: 42}))

	chunk := c.Chunk()
	require.Len(t, chunk.Code, 1)
	assert.Equal(t, vm.OpConst, chunk.Code[0].Op())
	require.Len(t, chunk.Constants, 1)
	assert.Equal(t, core.Int{V: 42}, chunk.Constants[0])
}

func TestCompiler_Float(t *testing.T) {
	c := NewCompiler("test")
	require.NoError(t, c.Compile(core.Float{V: 3.14}))

	chunk := c.Chunk()
	require.Len(t, chunk.Code, 1)
	assert.Equal(t, vm.OpConst, chunk.Code[0].Op())
	require.Len(t, chunk.Constants, 1)
	assert.Equal(t, core.Float{V: 3.14}, chunk.Constants[0])
}

func TestCompiler_String(t *testing.T) {
	c := NewCompiler("test")
	require.NoError(t, c.Compile(core.String{V: "hello"}))

	chunk := c.Chunk()
	require.Len(t, chunk.Code, 1)
	assert.Equal(t, vm.OpConst, chunk.Code[0].Op())
	require.Len(t, chunk.Constants, 1)
	assert.Equal(t, core.String{V: "hello"}, chunk.Constants[0])
}

func TestCompiler_Keyword(t *testing.T) {
	c := NewCompiler("test")
	require.NoError(t, c.Compile(core.Keyword{V: "foo"}))

	chunk := c.Chunk()
	require.Len(t, chunk.Code, 1)
	assert.Equal(t, vm.OpConst, chunk.Code[0].Op())
	require.Len(t, chunk.Constants, 1)
	assert.Equal(t, core.Keyword{V: "foo"}, chunk.Constants[0])
}

func TestCompiler_Symbol(t *testing.T) {
	t.Run("global", func(t *testing.T) {
		c := NewCompiler("test")
		require.NoError(t, c.Compile(core.Symbol{V: "x"}))

		chunk := c.Chunk()
		require.Len(t, chunk.Code, 1)
		assert.Equal(t, vm.OpGetGlobal, chunk.Code[0].Op())
		require.Len(t, chunk.Constants, 1)
		assert.Equal(t, core.Symbol{V: "x"}, chunk.Constants[0])
	})

	t.Run("local", func(t *testing.T) {
		c := NewCompiler("test")
		c.addLocal("x")
		require.NoError(t, c.Compile(core.Symbol{V: "x"}))

		chunk := c.Chunk()
		require.Len(t, chunk.Code, 1)
		assert.Equal(t, vm.OpGetLocal, chunk.Code[0].Op())
		assert.Equal(t, 0, chunk.Code[0].A())
	})
}

func TestCompiler_Vector(t *testing.T) {
	c := NewCompiler("test")
	vec := core.Vector{Items: []core.Value{
		core.Int{V: 1},
		core.Int{V: 2},
		core.Int{V: 3},
	}}
	require.NoError(t, c.Compile(vec))

	chunk := c.Chunk()
	require.Len(t, chunk.Code, 4)
	assert.Equal(t, vm.OpConst, chunk.Code[0].Op())
	assert.Equal(t, vm.OpConst, chunk.Code[1].Op())
	assert.Equal(t, vm.OpConst, chunk.Code[2].Op())
	assert.Equal(t, vm.OpMakeVector, chunk.Code[3].Op())
	assert.Equal(t, 3, chunk.Code[3].A())
}

func TestCompiler_List_Empty(t *testing.T) {
	c := NewCompiler("test")
	require.NoError(t, c.Compile(core.List{}))

	chunk := c.Chunk()
	require.Len(t, chunk.Code, 1)
	assert.Equal(t, vm.OpNil, chunk.Code[0].Op())
}

func TestCompiler_List_Literal(t *testing.T) {
	c := NewCompiler("test")
	lst := core.List{Items: []core.Value{
		core.Symbol{V: "list"},
		core.Int{V: 1},
		core.Int{V: 2},
	}}
	require.NoError(t, c.Compile(lst))

	chunk := c.Chunk()
	require.Len(t, chunk.Code, 4)
	assert.Equal(t, vm.OpGetGlobal, chunk.Code[0].Op())
	assert.Equal(t, vm.OpConst, chunk.Code[1].Op())
	assert.Equal(t, vm.OpConst, chunk.Code[2].Op())
	assert.Equal(t, vm.OpCall, chunk.Code[3].Op())
	assert.Equal(t, 2, chunk.Code[3].A())
}

func TestCompiler_If(t *testing.T) {
	t.Run("with else", func(t *testing.T) {
		c := NewCompiler("test")
		form := core.List{Items: []core.Value{
			core.Symbol{V: "if"},
			core.Bool{V: true},
			core.Int{V: 1},
			core.Int{V: 2},
		}}
		require.NoError(t, c.Compile(form))

		chunk := c.Chunk()
		require.Len(t, chunk.Code, 5)
		assert.Equal(t, vm.OpTrue, chunk.Code[0].Op())
		assert.Equal(t, vm.OpJumpIfFalse, chunk.Code[1].Op())
		assert.Equal(t, vm.OpConst, chunk.Code[2].Op())
		assert.Equal(t, vm.OpJump, chunk.Code[3].Op())
		assert.Equal(t, vm.OpConst, chunk.Code[4].Op())
	})

	t.Run("without else", func(t *testing.T) {
		c := NewCompiler("test")
		form := core.List{Items: []core.Value{
			core.Symbol{V: "if"},
			core.Bool{V: true},
			core.Int{V: 1},
		}}
		require.NoError(t, c.Compile(form))

		chunk := c.Chunk()
		require.Len(t, chunk.Code, 5)
		assert.Equal(t, vm.OpTrue, chunk.Code[0].Op())
		assert.Equal(t, vm.OpJumpIfFalse, chunk.Code[1].Op())
		assert.Equal(t, vm.OpConst, chunk.Code[2].Op())
		assert.Equal(t, vm.OpJump, chunk.Code[3].Op())
		assert.Equal(t, vm.OpNil, chunk.Code[4].Op())
	})
}

func TestCompiler_Def(t *testing.T) {
	c := NewCompiler("test")
	form := core.List{Items: []core.Value{
		core.Symbol{V: "def"},
		core.Symbol{V: "x"},
		core.Int{V: 42},
	}}
	require.NoError(t, c.Compile(form))

	chunk := c.Chunk()
	require.Len(t, chunk.Code, 2)
	assert.Equal(t, vm.OpConst, chunk.Code[0].Op())
	assert.Equal(t, vm.OpSetGlobal, chunk.Code[1].Op())
	require.Len(t, chunk.Constants, 2)
	assert.Equal(t, core.Int{V: 42}, chunk.Constants[0])
	assert.Equal(t, core.Symbol{V: "x"}, chunk.Constants[1])
}

func TestCompiler_Def_Error(t *testing.T) {
	t.Run("wrong arg count", func(t *testing.T) {
		c := NewCompiler("test")
		form := core.List{Items: []core.Value{
			core.Symbol{V: "def"},
			core.Symbol{V: "x"},
		}}
		err := c.Compile(form)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected 2 args")
	})

	t.Run("name not symbol", func(t *testing.T) {
		c := NewCompiler("test")
		form := core.List{Items: []core.Value{
			core.Symbol{V: "def"},
			core.Int{V: 42},
			core.Int{V: 1},
		}}
		err := c.Compile(form)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name must be symbol")
	})
}

func TestCompiler_Do(t *testing.T) {
	t.Run("multiple forms", func(t *testing.T) {
		c := NewCompiler("test")
		form := core.List{Items: []core.Value{
			core.Symbol{V: "do"},
			core.Int{V: 1},
			core.Int{V: 2},
			core.Int{V: 3},
		}}
		require.NoError(t, c.Compile(form))

		chunk := c.Chunk()
		require.Len(t, chunk.Code, 5)
		assert.Equal(t, vm.OpConst, chunk.Code[0].Op())
		assert.Equal(t, vm.OpPop, chunk.Code[1].Op())
		assert.Equal(t, vm.OpConst, chunk.Code[2].Op())
		assert.Equal(t, vm.OpPop, chunk.Code[3].Op())
		assert.Equal(t, vm.OpConst, chunk.Code[4].Op())
	})

	t.Run("empty", func(t *testing.T) {
		c := NewCompiler("test")
		form := core.List{Items: []core.Value{
			core.Symbol{V: "do"},
		}}
		require.NoError(t, c.Compile(form))

		chunk := c.Chunk()
		require.Len(t, chunk.Code, 1)
		assert.Equal(t, vm.OpNil, chunk.Code[0].Op())
	})
}

func TestCompiler_Let(t *testing.T) {
	c := NewCompiler("test")
	form := core.List{Items: []core.Value{
		core.Symbol{V: "let"},
		core.Vector{Items: []core.Value{
			core.Symbol{V: "x"},
			core.Int{V: 1},
			core.Symbol{V: "y"},
			core.Int{V: 2},
		}},
		core.Symbol{V: "x"},
	}}
	require.NoError(t, c.Compile(form))

	chunk := c.Chunk()
	assert.Equal(t, 2, chunk.Locals)
	require.Len(t, chunk.Code, 5)
	assert.Equal(t, vm.OpConst, chunk.Code[0].Op())
	assert.Equal(t, vm.OpSetLocal, chunk.Code[1].Op())
	assert.Equal(t, vm.OpConst, chunk.Code[2].Op())
	assert.Equal(t, vm.OpSetLocal, chunk.Code[3].Op())
	assert.Equal(t, vm.OpGetLocal, chunk.Code[4].Op())
}

func TestCompiler_Let_Error(t *testing.T) {
	t.Run("bindings not vector", func(t *testing.T) {
		c := NewCompiler("test")
		form := core.List{Items: []core.Value{
			core.Symbol{V: "let"},
			core.List{},
		}}
		err := c.Compile(form)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "bindings must be vector")
	})

	t.Run("odd bindings count", func(t *testing.T) {
		c := NewCompiler("test")
		form := core.List{Items: []core.Value{
			core.Symbol{V: "let"},
			core.Vector{Items: []core.Value{
				core.Symbol{V: "x"},
				core.Int{V: 1},
				core.Symbol{V: "y"},
			}},
		}}
		err := c.Compile(form)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "even count")
	})
}

func TestCompiler_Set(t *testing.T) {
	t.Run("global", func(t *testing.T) {
		c := NewCompiler("test")
		form := core.List{Items: []core.Value{
			core.Symbol{V: "set!"},
			core.Symbol{V: "x"},
			core.Int{V: 42},
		}}
		require.NoError(t, c.Compile(form))

		chunk := c.Chunk()
		require.Len(t, chunk.Code, 2)
		assert.Equal(t, vm.OpConst, chunk.Code[0].Op())
		assert.Equal(t, vm.OpSetGlobal, chunk.Code[1].Op())
	})

	t.Run("local", func(t *testing.T) {
		c := NewCompiler("test")
		c.addLocal("x")
		form := core.List{Items: []core.Value{
			core.Symbol{V: "set!"},
			core.Symbol{V: "x"},
			core.Int{V: 42},
		}}
		require.NoError(t, c.Compile(form))

		chunk := c.Chunk()
		require.Len(t, chunk.Code, 2)
		assert.Equal(t, vm.OpConst, chunk.Code[0].Op())
		assert.Equal(t, vm.OpSetLocal, chunk.Code[1].Op())
		assert.Equal(t, 0, chunk.Code[1].A())
	})
}

func TestCompiler_Quote(t *testing.T) {
	c := NewCompiler("test")
	quoted := core.List{Items: []core.Value{
		core.Symbol{V: "a"},
		core.Symbol{V: "b"},
	}}
	form := core.List{Items: []core.Value{
		core.Symbol{V: "quote"},
		quoted,
	}}
	require.NoError(t, c.Compile(form))

	chunk := c.Chunk()
	require.Len(t, chunk.Code, 1)
	assert.Equal(t, vm.OpConst, chunk.Code[0].Op())
	require.Len(t, chunk.Constants, 1)
	assert.True(t, quoted.Equals(chunk.Constants[0]))
}

func TestCompiler_Fn(t *testing.T) {
	c := NewCompiler("test")
	form := core.List{Items: []core.Value{
		core.Symbol{V: "fn"},
		core.Vector{Items: []core.Value{
			core.Symbol{V: "x"},
			core.Symbol{V: "y"},
		}},
		core.Symbol{V: "x"},
	}}
	require.NoError(t, c.Compile(form))

	chunk := c.Chunk()
	require.Len(t, chunk.Code, 1)
	assert.Equal(t, vm.OpClosure, chunk.Code[0].Op())
	require.Len(t, chunk.SubChunks, 1)

	sub := chunk.SubChunks[0]
	assert.Equal(t, "<fn>", sub.Name)
	assert.Equal(t, 2, sub.Arity)
	assert.False(t, sub.Variadic)
	assert.Equal(t, 2, sub.Locals)
	require.Len(t, sub.Code, 2)
	assert.Equal(t, vm.OpGetLocal, sub.Code[0].Op())
	assert.Equal(t, vm.OpReturn, sub.Code[1].Op())
}

func TestCompiler_Fn_Variadic(t *testing.T) {
	c := NewCompiler("test")
	form := core.List{Items: []core.Value{
		core.Symbol{V: "fn"},
		core.Vector{Items: []core.Value{
			core.Symbol{V: "x"},
			core.Symbol{V: "&"},
			core.Symbol{V: "rest"},
		}},
		core.Symbol{V: "rest"},
	}}
	require.NoError(t, c.Compile(form))

	chunk := c.Chunk()
	require.Len(t, chunk.SubChunks, 1)

	sub := chunk.SubChunks[0]
	assert.Equal(t, 1, sub.Arity)
	assert.True(t, sub.Variadic)
	assert.Equal(t, 2, sub.Locals)
}

func TestCompiler_Fn_Error(t *testing.T) {
	t.Run("params not vector", func(t *testing.T) {
		c := NewCompiler("test")
		form := core.List{Items: []core.Value{
			core.Symbol{V: "fn"},
			core.List{},
		}}
		err := c.Compile(form)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "params must be vector")
	})

	t.Run("param not symbol", func(t *testing.T) {
		c := NewCompiler("test")
		form := core.List{Items: []core.Value{
			core.Symbol{V: "fn"},
			core.Vector{Items: []core.Value{core.Int{V: 1}}},
		}}
		err := c.Compile(form)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "param must be symbol")
	})

	t.Run("& without rest param", func(t *testing.T) {
		c := NewCompiler("test")
		form := core.List{Items: []core.Value{
			core.Symbol{V: "fn"},
			core.Vector{Items: []core.Value{core.Symbol{V: "&"}}},
		}}
		err := c.Compile(form)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "& requires a rest param name")
	})
}

func TestCompiler_When(t *testing.T) {
	c := NewCompiler("test")
	form := core.List{Items: []core.Value{
		core.Symbol{V: "when"},
		core.Bool{V: true},
		core.Int{V: 1},
		core.Int{V: 2},
	}}
	require.NoError(t, c.Compile(form))

	chunk := c.Chunk()
	assert.Equal(t, vm.OpTrue, chunk.Code[0].Op())
	assert.Equal(t, vm.OpJumpIfFalse, chunk.Code[1].Op())
	assert.Equal(t, vm.OpConst, chunk.Code[2].Op())
	assert.Equal(t, vm.OpPop, chunk.Code[3].Op())
	assert.Equal(t, vm.OpConst, chunk.Code[4].Op())
}

func TestCompiler_Unless(t *testing.T) {
	c := NewCompiler("test")
	form := core.List{Items: []core.Value{
		core.Symbol{V: "unless"},
		core.Bool{V: false},
		core.Int{V: 1},
	}}
	require.NoError(t, c.Compile(form))

	chunk := c.Chunk()
	assert.Equal(t, vm.OpFalse, chunk.Code[0].Op())
	assert.Equal(t, vm.OpJumpIfFalse, chunk.Code[1].Op())
	assert.Equal(t, vm.OpJump, chunk.Code[2].Op())
	assert.Equal(t, vm.OpConst, chunk.Code[3].Op())
}

func TestCompiler_Recur(t *testing.T) {
	c := NewCompiler("test")
	form := core.List{Items: []core.Value{
		core.Symbol{V: "recur"},
		core.Int{V: 1},
		core.Int{V: 2},
	}}
	require.NoError(t, c.Compile(form))

	chunk := c.Chunk()
	require.Len(t, chunk.Code, 3)
	assert.Equal(t, vm.OpConst, chunk.Code[0].Op())
	assert.Equal(t, vm.OpConst, chunk.Code[1].Op())
	assert.Equal(t, vm.OpTailCall, chunk.Code[2].Op())
	assert.Equal(t, 2, chunk.Code[2].A())
}

func TestCompiler_Loop(t *testing.T) {
	c := NewCompiler("test")
	form := core.List{Items: []core.Value{
		core.Symbol{V: "loop"},
		core.Vector{Items: []core.Value{
			core.Symbol{V: "x"},
			core.Int{V: 0},
		}},
		core.Symbol{V: "x"},
	}}
	require.NoError(t, c.Compile(form))

	chunk := c.Chunk()
	assert.Equal(t, 1, chunk.Locals)
	require.Len(t, chunk.Code, 3)
	assert.Equal(t, vm.OpConst, chunk.Code[0].Op())
	assert.Equal(t, vm.OpSetLocal, chunk.Code[1].Op())
	assert.Equal(t, vm.OpGetLocal, chunk.Code[2].Op())
}

func TestCompiler_Call(t *testing.T) {
	c := NewCompiler("test")
	form := core.List{Items: []core.Value{
		core.Symbol{V: "+"},
		core.Int{V: 1},
		core.Int{V: 2},
	}}
	require.NoError(t, c.Compile(form))

	chunk := c.Chunk()
	require.Len(t, chunk.Code, 4)
	assert.Equal(t, vm.OpGetGlobal, chunk.Code[0].Op())
	assert.Equal(t, vm.OpConst, chunk.Code[1].Op())
	assert.Equal(t, vm.OpConst, chunk.Code[2].Op())
	assert.Equal(t, vm.OpCall, chunk.Code[3].Op())
	assert.Equal(t, 2, chunk.Code[3].A())
}

func TestCompiler_HashMap(t *testing.T) {
	c := NewCompiler("test")
	hm := core.NewHashMap()
	hm, _ = hm.Assoc(core.Keyword{V: "a"}, core.Int{V: 1})
	hm, _ = hm.Assoc(core.Keyword{V: "b"}, core.Int{V: 2})

	require.NoError(t, c.Compile(hm))

	chunk := c.Chunk()
	require.Len(t, chunk.Code, 5)
	assert.Equal(t, vm.OpConst, chunk.Code[0].Op())
	assert.Equal(t, vm.OpConst, chunk.Code[1].Op())
	assert.Equal(t, vm.OpConst, chunk.Code[2].Op())
	assert.Equal(t, vm.OpConst, chunk.Code[3].Op())
	assert.Equal(t, vm.OpMakeMap, chunk.Code[4].Op())
	assert.Equal(t, 2, chunk.Code[4].A())
}

func TestCompileAll(t *testing.T) {
	forms := []core.Value{
		core.Int{V: 1},
		core.Int{V: 2},
		core.Int{V: 3},
	}
	chunks, err := CompileAll(forms)
	require.NoError(t, err)
	require.Len(t, chunks, 3)

	for i, chunk := range chunks {
		require.Len(t, chunk.Code, 2)
		assert.Equal(t, vm.OpConst, chunk.Code[0].Op())
		assert.Equal(t, vm.OpReturn, chunk.Code[1].Op())
		assert.Equal(t, forms[i], chunk.Constants[0])
	}
}

func TestCompiler_ConstantDedup(t *testing.T) {
	c := NewCompiler("test")
	require.NoError(t, c.Compile(core.Int{V: 42}))
	require.NoError(t, c.Compile(core.Int{V: 42}))
	require.NoError(t, c.Compile(core.String{V: "hello"}))

	chunk := c.Chunk()
	require.Len(t, chunk.Constants, 2)
	require.Len(t, chunk.Code, 3)
	assert.Equal(t, 0, chunk.Code[0].A())
	assert.Equal(t, 0, chunk.Code[1].A())
	assert.Equal(t, 1, chunk.Code[2].A())
}

type unknownValue struct{}

func (unknownValue) Type() core.Keyword     { return core.Keyword{V: "unknown"} }
func (unknownValue) String() string         { return "unknown" }
func (unknownValue) Equals(core.Value) bool { return false }

func TestCompiler_UnknownType(t *testing.T) {
	c := NewCompiler("test")
	err := c.Compile(unknownValue{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown form type")
}

func TestCompiler_LetNonSymbolBinding(t *testing.T) {
	c := NewCompiler("test")
	form := core.List{Items: []core.Value{
		core.Symbol{V: "let"},
		core.Vector{Items: []core.Value{
			core.Int{V: 1},
			core.Int{V: 2},
		}},
		core.Int{V: 1},
	}}
	err := c.Compile(form)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected symbol")
}

func TestCompiler_FnNonSymbolRestParam(t *testing.T) {
	c := NewCompiler("test")
	form := core.List{Items: []core.Value{
		core.Symbol{V: "fn"},
		core.Vector{Items: []core.Value{
			core.Symbol{V: "a"},
			core.Symbol{V: "&"},
			core.Int{V: 5},
		}},
		core.Symbol{V: "a"},
	}}
	err := c.Compile(form)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected symbol")
}
