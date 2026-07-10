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
	require.Len(t, chunk.Code, 6)
	assert.Equal(t, vm.OpStructEnter, chunk.Code[0].Op())
	assert.Equal(t, vm.OpConst, chunk.Code[1].Op())
	assert.Equal(t, vm.OpConst, chunk.Code[2].Op())
	assert.Equal(t, vm.OpConst, chunk.Code[3].Op())
	assert.Equal(t, vm.OpMakeVector, chunk.Code[4].Op())
	assert.Equal(t, 3, chunk.Code[4].A())
	assert.Equal(t, vm.OpStructLeave, chunk.Code[5].Op())
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
	t.Run("params not vector or list", func(t *testing.T) {
		c := NewCompiler("test")
		form := core.List{Items: []core.Value{
			core.Symbol{V: "fn"},
			core.Int{V: 42},
		}}
		err := c.Compile(form)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "params must be vector or list")
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
	err := c.Compile(form)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "recur outside loop")
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

func TestCompiler_NativeOpAdd(t *testing.T) {
	c := NewCompiler("test")
	form := core.List{Items: []core.Value{
		core.Symbol{V: "+"},
		core.Int{V: 1},
		core.Int{V: 2},
	}}
	require.NoError(t, c.Compile(form))

	chunk := c.Chunk()
	require.Len(t, chunk.Code, 4)
	assert.Equal(t, vm.OpGetGlobal, chunk.Code[0].Op(), "head must be OpGetGlobal")
	assert.Equal(t, vm.OpConst, chunk.Code[1].Op())
	assert.Equal(t, vm.OpConst, chunk.Code[2].Op())
	assert.Equal(t, vm.OpAdd, chunk.Code[3].Op())
	assert.Equal(t, 2, chunk.Code[3].A(), "OpAdd operand = arg count (fn already consumed)")
}

func TestCompiler_NativeOpSub(t *testing.T) {
	form := core.List{Items: []core.Value{
		core.Symbol{V: "-"},
		core.Int{V: 10},
		core.Int{V: 3},
	}}
	c := NewCompiler("test")
	require.NoError(t, c.Compile(form))
	chunk := c.Chunk()
	require.Len(t, chunk.Code, 4)
	assert.Equal(t, vm.OpGetGlobal, chunk.Code[0].Op())
	assert.Equal(t, vm.OpSub, chunk.Code[3].Op())
}

func TestCompiler_NativeOpMul(t *testing.T) {
	form := core.List{Items: []core.Value{
		core.Symbol{V: "*"},
		core.Int{V: 6},
		core.Int{V: 7},
	}}
	c := NewCompiler("test")
	require.NoError(t, c.Compile(form))
	chunk := c.Chunk()
	require.Len(t, chunk.Code, 4)
	assert.Equal(t, vm.OpMul, chunk.Code[3].Op())
}

func TestCompiler_NativeOpDiv(t *testing.T) {
	form := core.List{Items: []core.Value{
		core.Symbol{V: "/"},
		core.Int{V: 10},
		core.Int{V: 2},
	}}
	c := NewCompiler("test")
	require.NoError(t, c.Compile(form))
	chunk := c.Chunk()
	require.Len(t, chunk.Code, 4)
	assert.Equal(t, vm.OpDiv, chunk.Code[3].Op())
}

func TestCompiler_NativeOpLt(t *testing.T) {
	form := core.List{Items: []core.Value{
		core.Symbol{V: "<"},
		core.Int{V: 1},
		core.Int{V: 2},
	}}
	c := NewCompiler("test")
	require.NoError(t, c.Compile(form))
	chunk := c.Chunk()
	require.Len(t, chunk.Code, 4)
	assert.Equal(t, vm.OpLt, chunk.Code[3].Op())
}

func TestCompiler_NativeOpGt(t *testing.T) {
	form := core.List{Items: []core.Value{
		core.Symbol{V: ">"},
		core.Int{V: 3},
		core.Int{V: 2},
	}}
	c := NewCompiler("test")
	require.NoError(t, c.Compile(form))
	chunk := c.Chunk()
	require.Len(t, chunk.Code, 4)
	assert.Equal(t, vm.OpGt, chunk.Code[3].Op())
}

func TestCompiler_NativeOpLe(t *testing.T) {
	form := core.List{Items: []core.Value{
		core.Symbol{V: "<="},
		core.Int{V: 2},
		core.Int{V: 2},
	}}
	c := NewCompiler("test")
	require.NoError(t, c.Compile(form))
	chunk := c.Chunk()
	require.Len(t, chunk.Code, 4)
	assert.Equal(t, vm.OpLe, chunk.Code[3].Op())
}

func TestCompiler_NativeOpGe(t *testing.T) {
	form := core.List{Items: []core.Value{
		core.Symbol{V: ">="},
		core.Int{V: 2},
		core.Int{V: 2},
	}}
	c := NewCompiler("test")
	require.NoError(t, c.Compile(form))
	chunk := c.Chunk()
	require.Len(t, chunk.Code, 4)
	assert.Equal(t, vm.OpGe, chunk.Code[3].Op())
}

func TestCompiler_NativeOpEq(t *testing.T) {
	form := core.List{Items: []core.Value{
		core.Symbol{V: "="},
		core.Int{V: 5},
		core.Int{V: 5},
	}}
	c := NewCompiler("test")
	require.NoError(t, c.Compile(form))
	chunk := c.Chunk()
	require.Len(t, chunk.Code, 4)
	assert.Equal(t, vm.OpEq, chunk.Code[3].Op())
}

func TestCompiler_NativeOp_ShadowedByLet(t *testing.T) {
	c := NewCompiler("test")
	form := core.List{Items: []core.Value{
		core.Symbol{V: "let"},
		core.Vector{Items: []core.Value{
			core.Symbol{V: "+"},
			core.Int{V: 5},
		}},
		core.List{Items: []core.Value{
			core.Symbol{V: "+"},
			core.Int{V: 1},
			core.Int{V: 2},
		}},
	}}
	require.NoError(t, c.Compile(form))

	chunk := c.Chunk()
	hasCall := false
	for _, instr := range chunk.Code {
		if instr.Op() == vm.OpCall {
			hasCall = true
			break
		}
	}
	assert.True(t, hasCall, "expected OpCall when + is locally shadowed")
}

func TestCompiler_NativeOp_NotShadowed(t *testing.T) {
	c := NewCompiler("test")
	form := core.List{Items: []core.Value{
		core.Symbol{V: "let"},
		core.Vector{Items: []core.Value{
			core.Symbol{V: "x"},
			core.Int{V: 5},
		}},
		core.List{Items: []core.Value{
			core.Symbol{V: "+"},
			core.Int{V: 1},
			core.Int{V: 2},
		}},
	}}
	require.NoError(t, c.Compile(form))

	chunk := c.Chunk()
	hasAdd := false
	for _, instr := range chunk.Code {
		if instr.Op() == vm.OpAdd {
			hasAdd = true
			break
		}
	}
	assert.True(t, hasAdd, "expected OpAdd when + is not locally shadowed")
}

func TestCompiler_NativeOp_ShadowedByEnclosingFn(t *testing.T) {
	c := NewCompiler("test")
	// (fn [+] ((fn [] (+ 1 2))))
	innerFn := core.List{Items: []core.Value{
		core.Symbol{V: "fn"},
		core.Vector{Items: []core.Value{}},
		core.List{Items: []core.Value{
			core.Symbol{V: "+"},
			core.Int{V: 1},
			core.Int{V: 2},
		}},
	}}
	outerFn := core.List{Items: []core.Value{
		core.Symbol{V: "fn"},
		core.Vector{Items: []core.Value{core.Symbol{V: "+"}}},
		innerFn,
	}}
	require.NoError(t, c.Compile(outerFn))
	// Nesting: top chunk → sub[0] (outer fn body, has + param) → sub[0] (inner fn body)
	require.Len(t, c.chunk.SubChunks, 1)
	outerBody := c.chunk.SubChunks[0]
	require.Len(t, outerBody.SubChunks, 1, "outer fn has one sub-chunk (inner fn)")
	innerBody := outerBody.SubChunks[0]
	hasCall := false
	for _, instr := range innerBody.Code {
		if instr.Op() == vm.OpCall {
			hasCall = true
			break
		}
	}
	assert.True(t, hasCall, "expected OpCall in inner fn body when + is shadowed by enclosing fn param")
}

func TestCompiler_HashMap(t *testing.T) {
	c := NewCompiler("test")
	hm := core.NewHashMap()
	hm, _ = hm.Assoc(core.Keyword{V: "a"}, core.Int{V: 1})
	hm, _ = hm.Assoc(core.Keyword{V: "b"}, core.Int{V: 2})

	require.NoError(t, c.Compile(hm))
	chunk := c.Chunk()
	require.Len(t, chunk.Code, 7)
	assert.Equal(t, vm.OpStructEnter, chunk.Code[0].Op())
	assert.Equal(t, vm.OpConst, chunk.Code[1].Op())
	assert.Equal(t, vm.OpConst, chunk.Code[2].Op())
	assert.Equal(t, vm.OpConst, chunk.Code[3].Op())
	assert.Equal(t, vm.OpConst, chunk.Code[4].Op())
	assert.Equal(t, vm.OpMakeMap, chunk.Code[5].Op())
	assert.Equal(t, 2, chunk.Code[5].A())
	assert.Equal(t, vm.OpStructLeave, chunk.Code[6].Op())
}

func TestCompiler_CaptureAnalysis_Uncaptured(t *testing.T) {
	c := NewCompiler("test")
	form := core.List{Items: []core.Value{
		core.Symbol{V: "fn"},
		core.Vector{Items: []core.Value{core.Symbol{V: "x"}}},
		core.Symbol{V: "x"},
	}}
	require.NoError(t, c.Compile(form))
	sub := c.Chunk().SubChunks[0]
	assert.Nil(t, sub.Captured)
	assert.False(t, sub.FullEnv)
}

func TestCompiler_CaptureAnalysis_DirectCapture(t *testing.T) {
	c := NewCompiler("test")
	form := core.List{Items: []core.Value{
		core.Symbol{V: "fn"},
		core.Vector{Items: []core.Value{core.Symbol{V: "x"}}},
		core.List{Items: []core.Value{
			core.Symbol{V: "fn"},
			core.Vector{},
			core.Symbol{V: "x"},
		}},
	}}
	require.NoError(t, c.Compile(form))
	sub := c.Chunk().SubChunks[0]

	require.NotNil(t, sub.Captured)
	assert.True(t, sub.Captured[0])
	assert.True(t, sub.FullEnv)

	inner := sub.SubChunks[0]
	assert.Nil(t, inner.Captured)
	assert.False(t, inner.FullEnv)
}

func TestCompiler_CaptureAnalysis_TransitiveCapture(t *testing.T) {
	c := NewCompiler("test")
	form := core.List{Items: []core.Value{
		core.Symbol{V: "fn"},
		core.Vector{Items: []core.Value{core.Symbol{V: "x"}}},
		core.List{Items: []core.Value{
			core.Symbol{V: "fn"},
			core.Vector{},
			core.List{Items: []core.Value{
				core.Symbol{V: "fn"},
				core.Vector{},
				core.Symbol{V: "x"},
			}},
		}},
	}}
	require.NoError(t, c.Compile(form))
	sub := c.Chunk().SubChunks[0]

	require.NotNil(t, sub.Captured)
	assert.True(t, sub.Captured[0])
	assert.True(t, sub.FullEnv)

	middle := sub.SubChunks[0]
	assert.Nil(t, middle.Captured)
	assert.False(t, middle.FullEnv)

	inner := middle.SubChunks[0]
	assert.Nil(t, inner.Captured)
	assert.False(t, inner.FullEnv)
}

func TestCompiler_CaptureAnalysis_LexicalShadowing(t *testing.T) {
	c := NewCompiler("test")
	form := core.List{Items: []core.Value{
		core.Symbol{V: "fn"},
		core.Vector{Items: []core.Value{core.Symbol{V: "x"}}},
		core.List{Items: []core.Value{
			core.Symbol{V: "fn"},
			core.Vector{Items: []core.Value{core.Symbol{V: "x"}}},
			core.Symbol{V: "x"},
		}},
	}}
	require.NoError(t, c.Compile(form))
	sub := c.Chunk().SubChunks[0]

	assert.Nil(t, sub.Captured)
	assert.False(t, sub.FullEnv)

	inner := sub.SubChunks[0]
	assert.Nil(t, inner.Captured)
	assert.False(t, inner.FullEnv)
}

func TestCompiler_CaptureAnalysis_FnParamVariadic(t *testing.T) {
	c := NewCompiler("test")
	form := core.List{Items: []core.Value{
		core.Symbol{V: "fn"},
		core.Vector{Items: []core.Value{
			core.Symbol{V: "x"},
			core.Symbol{V: "&"},
			core.Symbol{V: "rest"},
		}},
		core.List{Items: []core.Value{
			core.Symbol{V: "fn"},
			core.Vector{},
			core.Symbol{V: "rest"},
		}},
	}}
	require.NoError(t, c.Compile(form))
	sub := c.Chunk().SubChunks[0]

	require.NotNil(t, sub.Captured)
	assert.False(t, sub.Captured[0])
	assert.True(t, sub.Captured[1])
	assert.True(t, sub.FullEnv)

	inner := sub.SubChunks[0]
	assert.Nil(t, inner.Captured)
	assert.False(t, inner.FullEnv)
}

func TestCompiler_CaptureAnalysis_Quote(t *testing.T) {
	c := NewCompiler("test")
	form := core.List{Items: []core.Value{
		core.Symbol{V: "fn"},
		core.Vector{Items: []core.Value{core.Symbol{V: "x"}}},
		core.List{Items: []core.Value{
			core.Symbol{V: "quote"},
			core.Symbol{V: "x"},
		}},
	}}
	require.NoError(t, c.Compile(form))
	sub := c.Chunk().SubChunks[0]

	// quote treats x as data, not a symbol reference
	assert.Nil(t, sub.Captured)
	assert.False(t, sub.FullEnv)
}

func TestCompiler_CaptureAnalysis_QuasiquoteUnquote(t *testing.T) {
	c := NewCompiler("test")
	form := core.List{Items: []core.Value{
		core.Symbol{V: "fn"},
		core.Vector{Items: []core.Value{core.Symbol{V: "x"}}},
		core.List{Items: []core.Value{
			core.Symbol{V: "quasiquote"},
			core.List{Items: []core.Value{
				core.Symbol{V: "list"},
				core.List{Items: []core.Value{
					core.Symbol{V: "unquote"},
					core.Symbol{V: "x"},
				}},
			}},
		}},
	}}
	require.NoError(t, c.Compile(form))
	sub := c.Chunk().SubChunks[0]

	// quasiquote compiles ~x, producing OpGetLocal (not a constant)
	assert.Nil(t, sub.Captured)
	assert.False(t, sub.FullEnv)

	// Verify x is actually compiled to distinguish from quote
	found := false
	for _, inst := range sub.Code {
		if inst.Op() == vm.OpGetLocal {
			found = true
			break
		}
	}
	assert.True(t, found, "x should compile as OpGetLocal inside quasiquote/unquote")
}

func TestCompiler_CaptureAnalysis_FullEnv(t *testing.T) {
	c := NewCompiler("test")
	form := core.List{Items: []core.Value{
		core.Symbol{V: "fn"},
		core.Vector{},
		core.Int{V: 42},
	}}
	require.NoError(t, c.Compile(form))
	sub := c.Chunk().SubChunks[0]

	// fn with no captures and no locals should have FullEnv=false
	assert.Nil(t, sub.Captured)
	assert.False(t, sub.FullEnv)
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

func TestCompiler_Fn_EmptyBody(t *testing.T) {
	t.Run("no body forms", func(t *testing.T) {
		c := NewCompiler("test")
		form := core.List{Items: []core.Value{
			core.Symbol{V: "fn"},
			core.Vector{},
		}}
		err := c.Compile(form)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "at least 2 arguments")
	})

	t.Run("no params at all", func(t *testing.T) {
		c := NewCompiler("test")
		form := core.List{Items: []core.Value{
			core.Symbol{V: "fn"},
		}}
		err := c.Compile(form)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "at least 2 arguments")
	})
}

func TestCompiler_Defn_EmptyBody(t *testing.T) {
	c := NewCompiler("test")
	form := core.List{Items: []core.Value{
		core.Symbol{V: "defn"},
		core.Symbol{V: "f"},
		core.Vector{},
	}}
	err := c.Compile(form)
	require.Error(t, err)
}

func TestCompiler_Defmacro_Unsupported(t *testing.T) {
	c := NewCompiler("test")
	form := core.List{Items: []core.Value{
		core.Symbol{V: "defmacro"},
		core.Symbol{V: "id"},
		core.Vector{Items: []core.Value{core.Symbol{V: "x"}}},
		core.Symbol{V: "x"},
	}}
	err := c.Compile(form)
	require.Error(t, err)

	var lispErr *core.LispicoError
	require.ErrorAs(t, err, &lispErr)
	assert.Equal(t, CodeUnsupported, lispErr.Code)
}

func TestCompiler_UnquoteSplicing_Unsupported(t *testing.T) {
	c := NewCompiler("test")
	form := core.List{Items: []core.Value{
		core.Symbol{V: "quasiquote"},
		core.List{Items: []core.Value{
			core.List{Items: []core.Value{
				core.Symbol{V: "unquote-splicing"},
				core.Symbol{V: "xs"},
			}},
		}},
	}}
	err := c.Compile(form)
	require.Error(t, err)

	var lispErr *core.LispicoError
	require.ErrorAs(t, err, &lispErr)
	assert.Equal(t, CodeUnsupported, lispErr.Code)
}
