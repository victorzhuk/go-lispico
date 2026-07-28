package compiler

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/cl"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/core/vm"
)

func requireFoldedLiteral(t *testing.T, chunk *vm.Chunk, depth int, want core.Value, charge int64) {
	t.Helper()

	require.Len(t, chunk.Code, 3)
	assert.Equal(t, vm.OpStructEnter, chunk.Code[0].Op())
	assert.Equal(t, depth, chunk.Code[0].A())
	assert.Equal(t, vm.OpConstCharged, chunk.Code[1].Op())
	assert.Equal(t, vm.OpStructLeave, chunk.Code[2].Op())
	assert.Equal(t, depth, chunk.Code[2].A())

	idx := chunk.Code[1].A()
	require.True(t, want.Equals(chunk.Constants[idx]), "got %v, want %v", chunk.Constants[idx], want)
	require.Contains(t, chunk.ConstCharges, idx)
	assert.Equal(t, charge, chunk.ConstCharges[idx])
}

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
	vec := core.NewVector([]core.Value{
		core.Int{V: 1},
		core.Int{V: 2},
		core.Int{V: 3},
	})
	require.NoError(t, c.Compile(vec))

	chunk := c.Chunk()
	requireFoldedLiteral(t, chunk, 1, vec, core.VectorShallowBytes(3))
}

func TestCompiler_FoldedVectorStackEffect(t *testing.T) {
	c := NewCompiler("test")
	require.NoError(t, c.Compile(core.NewVector([]core.Value{core.Keyword{V: "x"}})))
	c.MarkCaptures()

	assert.Equal(t, 1, c.Chunk().MaxStack)
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
	lst := core.NewList([]core.Value{
		core.Symbol{V: "list"},
		core.Int{V: 1},
		core.Int{V: 2},
	})
	require.NoError(t, c.Compile(lst))

	chunk := c.Chunk()
	require.Len(t, chunk.Code, 4)
	assert.Equal(t, vm.OpGetGlobal, chunk.Code[0].Op())
	assert.Equal(t, vm.OpConst, chunk.Code[1].Op())
	assert.Equal(t, vm.OpConst, chunk.Code[2].Op())
	assert.Equal(t, vm.OpCall, chunk.Code[3].Op())
	assert.Equal(t, 2, chunk.Code[3].A())
}

func TestCompiler_QuoteConstantListStaysPlainConst(t *testing.T) {
	c := NewCompiler("test")
	lst := core.NewList([]core.Value{
		core.Int{V: 1},
		core.Int{V: 2},
	})
	form := core.NewList([]core.Value{core.Symbol{V: "quote"}, lst})
	require.NoError(t, c.Compile(form))

	// Quote must not fold to a charged constant: the tree-walker's
	// evalQuote returns the datum with no construction charge, so the VM
	// charges nothing either or the ledger diverges across evaluators.
	chunk := c.Chunk()
	require.Len(t, chunk.Code, 1)
	assert.Equal(t, vm.OpConst, chunk.Code[0].Op())
	assert.Empty(t, chunk.ConstCharges)
}

func TestCompiler_QuoteSymbolListDoesNotFold(t *testing.T) {
	c := NewCompiler("test")
	lst := core.NewList([]core.Value{core.Symbol{V: "x"}})
	form := core.NewList([]core.Value{core.Symbol{V: "quote"}, lst})
	require.NoError(t, c.Compile(form))

	chunk := c.Chunk()
	require.Len(t, chunk.Code, 1)
	assert.Equal(t, vm.OpConst, chunk.Code[0].Op())
}

func TestCompiler_If(t *testing.T) {
	t.Run("with else", func(t *testing.T) {
		c := NewCompiler("test")
		form := core.NewList([]core.Value{
			core.Symbol{V: "if"},
			core.Bool{V: true},
			core.Int{V: 1},
			core.Int{V: 2},
		})
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
		form := core.NewList([]core.Value{
			core.Symbol{V: "if"},
			core.Bool{V: true},
			core.Int{V: 1},
		})
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
	form := core.NewList([]core.Value{
		core.Symbol{V: "def"},
		core.Symbol{V: "x"},
		core.Int{V: 42},
	})
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
		form := core.NewList([]core.Value{
			core.Symbol{V: "def"},
			core.Symbol{V: "x"},
		})
		err := c.Compile(form)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected 2 args")
	})

	t.Run("name not symbol", func(t *testing.T) {
		c := NewCompiler("test")
		form := core.NewList([]core.Value{
			core.Symbol{V: "def"},
			core.Int{V: 42},
			core.Int{V: 1},
		})
		err := c.Compile(form)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name must be symbol")
	})
}

func TestCompiler_Do(t *testing.T) {
	t.Run("multiple forms", func(t *testing.T) {
		c := NewCompiler("test")
		form := core.NewList([]core.Value{
			core.Symbol{V: "do"},
			core.Int{V: 1},
			core.Int{V: 2},
			core.Int{V: 3},
		})
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
		form := core.NewList([]core.Value{
			core.Symbol{V: "do"},
		})
		require.NoError(t, c.Compile(form))

		chunk := c.Chunk()
		require.Len(t, chunk.Code, 1)
		assert.Equal(t, vm.OpNil, chunk.Code[0].Op())
	})
}

func TestCompiler_Let(t *testing.T) {
	c := NewCompiler("test")
	form := core.NewList([]core.Value{
		core.Symbol{V: "let"},
		core.NewVector([]core.Value{
			core.Symbol{V: "x"},
			core.Int{V: 1},
			core.Symbol{V: "y"},
			core.Int{V: 2},
		}),
		core.Symbol{V: "x"},
	})
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

func TestCompiler_Let_ListBindings(t *testing.T) {
	c := NewCompiler("test")
	form := core.NewList([]core.Value{
		core.Symbol{V: "let"},
		core.NewList([]core.Value{
			core.NewList([]core.Value{core.Symbol{V: "x"}, core.Int{V: 1}}),
			core.NewList([]core.Value{core.Symbol{V: "y"}, core.Int{V: 2}}),
		}),
		core.Symbol{V: "x"},
	})
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

func TestCompiler_LetStar_ListBindings(t *testing.T) {
	c := NewCompiler("test")
	form := core.NewList([]core.Value{
		core.Symbol{V: "let*"},
		core.NewList([]core.Value{
			core.NewList([]core.Value{core.Symbol{V: "x"}, core.Int{V: 1}}),
			core.NewList([]core.Value{core.Symbol{V: "y"}, core.Symbol{V: "x"}}),
		}),
		core.Symbol{V: "y"},
	})
	require.NoError(t, c.Compile(form))

	chunk := c.Chunk()
	assert.Equal(t, 2, chunk.Locals)
	require.Len(t, chunk.Code, 5)
	assert.Equal(t, vm.OpConst, chunk.Code[0].Op())
	assert.Equal(t, vm.OpSetLocal, chunk.Code[1].Op())
	assert.Equal(t, vm.OpGetLocal, chunk.Code[2].Op())
	assert.Equal(t, vm.OpSetLocal, chunk.Code[3].Op())
	assert.Equal(t, vm.OpGetLocal, chunk.Code[4].Op())
}

func TestCompiler_Let_Error(t *testing.T) {
	t.Run("empty list bindings", func(t *testing.T) {
		c := NewCompiler("test")
		form := core.NewList([]core.Value{
			core.Symbol{V: "let"},
			core.List{},
			core.Int{V: 1},
		})
		require.NoError(t, c.Compile(form))
	})

	t.Run("bindings not vector or pair list", func(t *testing.T) {
		c := NewCompiler("test")
		form := core.NewList([]core.Value{
			core.Symbol{V: "let"},
			core.Int{V: 42},
			core.Int{V: 1},
		})
		err := c.Compile(form)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "vector")
		assert.Contains(t, err.Error(), "pair")
	})

	t.Run("odd bindings count", func(t *testing.T) {
		c := NewCompiler("test")
		form := core.NewList([]core.Value{
			core.Symbol{V: "let"},
			core.NewVector([]core.Value{
				core.Symbol{V: "x"},
				core.Int{V: 1},
				core.Symbol{V: "y"},
			}),
			core.Int{V: 1},
		})
		err := c.Compile(form)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "even number")
	})

	t.Run("list element not pair", func(t *testing.T) {
		c := NewCompiler("test")
		form := core.NewList([]core.Value{
			core.Symbol{V: "let"},
			core.NewList([]core.Value{core.Symbol{V: "x"}}),
			core.Int{V: 1},
		})
		err := c.Compile(form)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "vector")
		assert.Contains(t, err.Error(), "pair")
	})

	t.Run("list pair head not symbol", func(t *testing.T) {
		c := NewCompiler("test")
		form := core.NewList([]core.Value{
			core.Symbol{V: "let"},
			core.NewList([]core.Value{
				core.NewList([]core.Value{core.Int{V: 1}, core.Int{V: 2}}),
			}),
			core.Int{V: 1},
		})
		err := c.Compile(form)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "vector")
		assert.Contains(t, err.Error(), "pair")
		assert.Contains(t, err.Error(), "must be a symbol")
	})
}

func TestCompiler_Set(t *testing.T) {
	t.Run("global", func(t *testing.T) {
		c := NewCompiler("test")
		form := core.NewList([]core.Value{
			core.Symbol{V: "set!"},
			core.Symbol{V: "x"},
			core.Int{V: 42},
		})
		require.NoError(t, c.Compile(form))

		chunk := c.Chunk()
		require.Len(t, chunk.Code, 2)
		assert.Equal(t, vm.OpConst, chunk.Code[0].Op())
		assert.Equal(t, vm.OpSetLexical, chunk.Code[1].Op())
	})

	t.Run("local", func(t *testing.T) {
		c := NewCompiler("test")
		c.addLocal("x")
		form := core.NewList([]core.Value{
			core.Symbol{V: "set!"},
			core.Symbol{V: "x"},
			core.Int{V: 42},
		})
		require.NoError(t, c.Compile(form))

		chunk := c.Chunk()
		require.Len(t, chunk.Code, 2)
		assert.Equal(t, vm.OpConst, chunk.Code[0].Op())
		assert.Equal(t, vm.OpSetLocal, chunk.Code[1].Op())
		assert.Equal(t, 0, chunk.Code[1].A())
	})
}

func TestCompiler_Try_LocalsAfterTry(t *testing.T) {
	c := NewCompiler("test")
	form := core.NewList([]core.Value{
		core.Symbol{V: "try"},
		core.Int{V: 5},
		core.NewList([]core.Value{
			core.Symbol{V: "catch"},
			core.Symbol{V: "e"},
			core.Symbol{V: "e"},
		}),
	})
	require.NoError(t, c.Compile(form))
	c.addLocal("post")
	assert.Equal(t, 0, c.resolveLocal("post"),
		"post-try local must not be shifted by dead catch slot")
}

func TestCompiler_Quote(t *testing.T) {
	c := NewCompiler("test")
	quoted := core.NewList([]core.Value{
		core.Symbol{V: "a"},
		core.Symbol{V: "b"},
	})
	form := core.NewList([]core.Value{
		core.Symbol{V: "quote"},
		quoted,
	})
	require.NoError(t, c.Compile(form))

	chunk := c.Chunk()
	require.Len(t, chunk.Code, 1)
	assert.Equal(t, vm.OpConst, chunk.Code[0].Op())
	require.Len(t, chunk.Constants, 1)
	assert.True(t, quoted.Equals(chunk.Constants[0]))
}

func TestCompiler_Fn(t *testing.T) {
	c := NewCompiler("test")
	form := core.NewList([]core.Value{
		core.Symbol{V: "fn"},
		core.NewVector([]core.Value{
			core.Symbol{V: "x"},
			core.Symbol{V: "y"},
		}),
		core.Symbol{V: "x"},
	})
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
	form := core.NewList([]core.Value{
		core.Symbol{V: "fn"},
		core.NewVector([]core.Value{
			core.Symbol{V: "x"},
			core.Symbol{V: "&"},
			core.Symbol{V: "rest"},
		}),
		core.Symbol{V: "rest"},
	})
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
		form := core.NewList([]core.Value{
			core.Symbol{V: "fn"},
			core.Int{V: 42},
		})
		err := c.Compile(form)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "params must be vector or list")
	})

	t.Run("param not symbol", func(t *testing.T) {
		c := NewCompiler("test")
		form := core.NewList([]core.Value{
			core.Symbol{V: "fn"},
			core.NewVector([]core.Value{core.Int{V: 1}}),
		})
		err := c.Compile(form)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "param must be symbol")
	})

	t.Run("& without rest param", func(t *testing.T) {
		c := NewCompiler("test")
		form := core.NewList([]core.Value{
			core.Symbol{V: "fn"},
			core.NewVector([]core.Value{core.Symbol{V: "&"}}),
		})
		err := c.Compile(form)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "& requires a rest param name")
	})
}

func TestCompiler_When(t *testing.T) {
	c := NewCompiler("test")
	form := core.NewList([]core.Value{
		core.Symbol{V: "when"},
		core.Bool{V: true},
		core.Int{V: 1},
		core.Int{V: 2},
	})
	require.NoError(t, c.Compile(form))

	chunk := c.Chunk()
	assert.Equal(t, vm.OpTrue, chunk.Code[0].Op())
	assert.Equal(t, vm.OpJumpIfFalse, chunk.Code[1].Op())
	assert.Equal(t, vm.OpConst, chunk.Code[2].Op())
	assert.Equal(t, vm.OpPop, chunk.Code[3].Op())
	assert.Equal(t, vm.OpConst, chunk.Code[4].Op())
	assert.Equal(t, vm.OpJump, chunk.Code[5].Op())
	assert.Equal(t, vm.OpNil, chunk.Code[6].Op())
}

func TestCompiler_When_SkippedNil(t *testing.T) {
	c := NewCompiler("test")
	form := core.NewList([]core.Value{
		core.Symbol{V: "when"},
		core.Bool{V: false},
		core.Int{V: 1},
		core.Int{V: 2},
	})
	require.NoError(t, c.Compile(form))

	chunk := c.Chunk()
	var ops []vm.Opcode
	for _, instr := range chunk.Code {
		ops = append(ops, instr.Op())
	}
	assert.Contains(t, ops, vm.OpNil,
		"false when must emit OpNil on the skipped path; got %v", ops)
}

func TestCompiler_Recur(t *testing.T) {
	c := NewCompiler("test")
	form := core.NewList([]core.Value{
		core.Symbol{V: "recur"},
		core.Int{V: 1},
		core.Int{V: 2},
	})
	err := c.Compile(form)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "recur outside loop")
}

func TestCompiler_Loop(t *testing.T) {
	c := NewCompiler("test")
	form := core.NewList([]core.Value{
		core.Symbol{V: "loop"},
		core.NewList([]core.Value{
			core.NewList([]core.Value{core.Symbol{V: "x"}, core.Int{V: 0}}),
		}),
		core.Symbol{V: "x"},
	})
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
	form := core.NewList([]core.Value{
		core.Symbol{V: "+"},
		core.Int{V: 1},
		core.Int{V: 2},
	})
	require.NoError(t, c.Compile(form))

	chunk := c.Chunk()
	require.Len(t, chunk.Code, 1)
	assert.Equal(t, vm.OpFusedNativeOp, chunk.Code[0].Op())
	require.Len(t, chunk.Fused, 1)
	fo := chunk.Fused[0]
	assert.Equal(t, vm.OpAdd, fo.Op)
	assert.Equal(t, vm.OperandConst, fo.AKind)
	assert.Equal(t, vm.OperandConst, fo.BKind)
	assert.Equal(t, core.Int{V: 1}, chunk.Constants[fo.A])
	assert.Equal(t, core.Int{V: 2}, chunk.Constants[fo.B])
}

func TestCompiler_NativeOpSub(t *testing.T) {
	form := core.NewList([]core.Value{
		core.Symbol{V: "-"},
		core.Int{V: 10},
		core.Int{V: 3},
	})
	c := NewCompiler("test")
	require.NoError(t, c.Compile(form))
	chunk := c.Chunk()
	require.Len(t, chunk.Code, 1)
	assert.Equal(t, vm.OpFusedNativeOp, chunk.Code[0].Op())
	require.Len(t, chunk.Fused, 1)
	assert.Equal(t, vm.OpSub, chunk.Fused[0].Op)
}

func TestCompiler_NativeOpMul(t *testing.T) {
	form := core.NewList([]core.Value{
		core.Symbol{V: "*"},
		core.Int{V: 6},
		core.Int{V: 7},
	})
	c := NewCompiler("test")
	require.NoError(t, c.Compile(form))
	chunk := c.Chunk()
	require.Len(t, chunk.Code, 1)
	assert.Equal(t, vm.OpFusedNativeOp, chunk.Code[0].Op())
	require.Len(t, chunk.Fused, 1)
	assert.Equal(t, vm.OpMul, chunk.Fused[0].Op)
}

func TestCompiler_NativeOpDiv(t *testing.T) {
	form := core.NewList([]core.Value{
		core.Symbol{V: "/"},
		core.Int{V: 10},
		core.Int{V: 2},
	})
	c := NewCompiler("test")
	require.NoError(t, c.Compile(form))
	chunk := c.Chunk()
	require.Len(t, chunk.Code, 1)
	assert.Equal(t, vm.OpFusedNativeOp, chunk.Code[0].Op())
	require.Len(t, chunk.Fused, 1)
	assert.Equal(t, vm.OpDiv, chunk.Fused[0].Op)
}

// TestCompiler_NativeOpAdd/_Sub/_Mul/_Div/_Lt/_Gt/_Le/_Ge/_Eq use two Int
// constants, the eligible shape for OpFusedNativeOp: both operands collapse
// into the chunk's Fused side table instead of separate
// FREEZE_NATIVE/CONST/CONST/<op> instructions. TestCompiler_NativeOp_Unfused
// pins the non-eligible shape these no longer cover.
func TestCompiler_NativeOpLt(t *testing.T) {
	form := core.NewList([]core.Value{
		core.Symbol{V: "<"},
		core.Int{V: 1},
		core.Int{V: 2},
	})
	c := NewCompiler("test")
	require.NoError(t, c.Compile(form))
	chunk := c.Chunk()
	require.Len(t, chunk.Code, 1)
	assert.Equal(t, vm.OpFusedNativeOp, chunk.Code[0].Op())
	require.Len(t, chunk.Fused, 1)
	fo := chunk.Fused[0]
	assert.Equal(t, vm.OpLt, fo.Op)
	assert.Equal(t, vm.OperandConst, fo.AKind)
	assert.Equal(t, vm.OperandConst, fo.BKind)
	assert.Equal(t, core.Int{V: 1}, chunk.Constants[fo.A])
	assert.Equal(t, core.Int{V: 2}, chunk.Constants[fo.B])
}

func TestCompiler_NativeOpGt(t *testing.T) {
	form := core.NewList([]core.Value{
		core.Symbol{V: ">"},
		core.Int{V: 3},
		core.Int{V: 2},
	})
	c := NewCompiler("test")
	require.NoError(t, c.Compile(form))
	chunk := c.Chunk()
	require.Len(t, chunk.Code, 1)
	assert.Equal(t, vm.OpFusedNativeOp, chunk.Code[0].Op())
	require.Len(t, chunk.Fused, 1)
	assert.Equal(t, vm.OpGt, chunk.Fused[0].Op)
}

func TestCompiler_NativeOpLe(t *testing.T) {
	form := core.NewList([]core.Value{
		core.Symbol{V: "<="},
		core.Int{V: 2},
		core.Int{V: 2},
	})
	c := NewCompiler("test")
	require.NoError(t, c.Compile(form))
	chunk := c.Chunk()
	require.Len(t, chunk.Code, 1)
	assert.Equal(t, vm.OpFusedNativeOp, chunk.Code[0].Op())
	require.Len(t, chunk.Fused, 1)
	assert.Equal(t, vm.OpLe, chunk.Fused[0].Op)
}

func TestCompiler_NativeOpGe(t *testing.T) {
	form := core.NewList([]core.Value{
		core.Symbol{V: ">="},
		core.Int{V: 2},
		core.Int{V: 2},
	})
	c := NewCompiler("test")
	require.NoError(t, c.Compile(form))
	chunk := c.Chunk()
	require.Len(t, chunk.Code, 1)
	assert.Equal(t, vm.OpFusedNativeOp, chunk.Code[0].Op())
	require.Len(t, chunk.Fused, 1)
	assert.Equal(t, vm.OpGe, chunk.Fused[0].Op)
}

func TestCompiler_NativeOpEq(t *testing.T) {
	form := core.NewList([]core.Value{
		core.Symbol{V: "="},
		core.Int{V: 5},
		core.Int{V: 5},
	})
	c := NewCompiler("test")
	require.NoError(t, c.Compile(form))
	chunk := c.Chunk()
	require.Len(t, chunk.Code, 1)
	assert.Equal(t, vm.OpFusedNativeOp, chunk.Code[0].Op())
	require.Len(t, chunk.Fused, 1)
	assert.Equal(t, vm.OpEq, chunk.Fused[0].Op)
}

// TestCompiler_NativeOp_Unfused pins the freeze-based shape for a comparison
// whose operands are neither locals nor scalar constants (two globals here),
// the case OpFusedNativeOp does not cover.
func TestCompiler_NativeOp_Unfused(t *testing.T) {
	form := core.NewList([]core.Value{
		core.Symbol{V: "<"},
		core.Symbol{V: "a"},
		core.Symbol{V: "b"},
	})
	c := NewCompiler("test")
	require.NoError(t, c.Compile(form))
	chunk := c.Chunk()
	require.Len(t, chunk.Code, 4)
	assert.Equal(t, vm.OpFreezeNative, chunk.Code[0].Op(), "head must be OpFreezeNative")
	assert.Equal(t, vm.OpGetGlobal, chunk.Code[1].Op())
	assert.Equal(t, vm.OpGetGlobal, chunk.Code[2].Op())
	assert.Equal(t, vm.OpLt, chunk.Code[3].Op())
	assert.Equal(t, 2, chunk.Code[3].A())
	assert.Empty(t, chunk.Fused)
}

func TestCompiler_NativeOp_ShadowedByLet(t *testing.T) {
	c := NewCompiler("test")
	form := core.NewList([]core.Value{
		core.Symbol{V: "let"},
		core.NewVector([]core.Value{
			core.Symbol{V: "+"},
			core.Int{V: 5},
		}),
		core.NewList([]core.Value{
			core.Symbol{V: "+"},
			core.Int{V: 1},
			core.Int{V: 2},
		}),
	})
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
	form := core.NewList([]core.Value{
		core.Symbol{V: "let"},
		core.NewVector([]core.Value{
			core.Symbol{V: "x"},
			core.Int{V: 5},
		}),
		core.NewList([]core.Value{
			core.Symbol{V: "+"},
			core.Int{V: 1},
			core.Int{V: 2},
		}),
	})
	require.NoError(t, c.Compile(form))

	chunk := c.Chunk()
	require.Len(t, chunk.Fused, 1, "expected + to fuse when not locally shadowed")
	assert.Equal(t, vm.OpAdd, chunk.Fused[0].Op)
}

func TestCompiler_NativeOp_ShadowedByEnclosingFn(t *testing.T) {
	c := NewCompiler("test")
	// (fn [+] ((fn [] (+ 1 2))))
	innerFn := core.NewList([]core.Value{
		core.Symbol{V: "fn"},
		core.NewVector([]core.Value{}),
		core.NewList([]core.Value{
			core.Symbol{V: "+"},
			core.Int{V: 1},
			core.Int{V: 2},
		}),
	})
	outerFn := core.NewList([]core.Value{
		core.Symbol{V: "fn"},
		core.NewVector([]core.Value{core.Symbol{V: "+"}}),
		innerFn,
	})
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

func TestCompiler_NativeOp_Dialect(t *testing.T) {
	clDialect := cl.Dialect()
	clojureDialect := clojure.Dialect()
	// CL is Lisp-2: a native op head resolves through the function cell
	// (OpFreezeNativeFunc), same as any other call head under compileCall.
	// Clojure is Lisp-1: the value cell (OpFreezeNative) is the only namespace.
	cases := []struct {
		name    string
		dialect *core.Dialect
		headOp  vm.Opcode
	}{
		{"cl", &clDialect, vm.OpFreezeNativeFunc},
		{"clojure", &clojureDialect, vm.OpFreezeNative},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("add", func(t *testing.T) {
				c := NewCompilerWithDialect("test", tc.dialect)
				form := core.NewList([]core.Value{
					core.Symbol{V: "+"},
					core.Symbol{V: "a"},
					core.Symbol{V: "b"},
				})
				require.NoError(t, c.Compile(form))

				chunk := c.Chunk()
				require.Len(t, chunk.Code, 4)
				assert.Equal(t, tc.headOp, chunk.Code[0].Op(), "head opcode")
				assert.Equal(t, vm.OpAdd, chunk.Code[3].Op())
			})

			t.Run("lt", func(t *testing.T) {
				c := NewCompilerWithDialect("test", tc.dialect)
				form := core.NewList([]core.Value{
					core.Symbol{V: "<"},
					core.Symbol{V: "a"},
					core.Symbol{V: "b"},
				})
				require.NoError(t, c.Compile(form))

				chunk := c.Chunk()
				require.Len(t, chunk.Code, 4)
				assert.Equal(t, tc.headOp, chunk.Code[0].Op(), "head opcode")
				assert.Equal(t, vm.OpLt, chunk.Code[3].Op())
			})
		})
	}
}

// TestCompiler_NativeOp_DialectShadowedByLet asserts the shadow fallback
// under Clojure (Lisp-1), where a let-bound "+" unambiguously shadows the
// single namespace. CL's let only binds the value cell, not the function
// cell a native-op head resolves through, so the analogous CL case doesn't
// shadow at all — that's covered by crossval instead of an opcode assertion.
func TestCompiler_NativeOp_DialectShadowedByLet(t *testing.T) {
	clojureDialect := clojure.Dialect()
	c := NewCompilerWithDialect("test", &clojureDialect)
	form := core.NewList([]core.Value{
		core.Symbol{V: "let"},
		core.NewVector([]core.Value{
			core.Symbol{V: "+"},
			core.Int{V: 5},
		}),
		core.NewList([]core.Value{
			core.Symbol{V: "+"},
			core.Int{V: 1},
			core.Int{V: 2},
		}),
	})
	require.NoError(t, c.Compile(form))

	chunk := c.Chunk()
	hasCall := false
	for _, instr := range chunk.Code {
		if instr.Op() == vm.OpCall {
			hasCall = true
			break
		}
	}
	assert.True(t, hasCall, "expected OpCall when + is locally shadowed under a dialect")
}

// TestCompiler_NativeOp_DialectRebindStillNative: under CL, a value-cell
// `(def + f)` doesn't touch the function cell a native-op head resolves
// through (OpGetFunc), so the head still compiles to the native opcode.
// Rebind-safety for an actual function-cell rebind (defun) is a VM runtime
// concern, proven by crossval, not a compile-time one.
func TestCompiler_NativeOp_DialectRebindStillNative(t *testing.T) {
	clDialect := cl.Dialect()
	c := NewCompilerWithDialect("test", &clDialect)
	form := core.NewList([]core.Value{
		core.Symbol{V: "progn"},
		core.NewList([]core.Value{
			core.Symbol{V: "def"},
			core.Symbol{V: "+"},
			core.Symbol{V: "f"},
		}),
		core.NewList([]core.Value{
			core.Symbol{V: "+"},
			core.Int{V: 1},
			core.Int{V: 2},
		}),
	})
	require.NoError(t, c.Compile(form))

	chunk := c.Chunk()
	require.Len(t, chunk.Fused, 1, "value-cell def of + doesn't touch the function cell; head still fuses")
	assert.Equal(t, vm.OpAdd, chunk.Fused[0].Op)
}

func TestCompiler_HashMap(t *testing.T) {
	c := NewCompiler("test")
	hm := core.NewHashMap()
	hm, _, _ = hm.Assoc(core.Keyword{V: "a"}, core.Int{V: 1})
	hm, _, _ = hm.Assoc(core.Keyword{V: "b"}, core.Int{V: 2})

	require.NoError(t, c.Compile(hm))
	chunk := c.Chunk()
	requireFoldedLiteral(t, chunk, 1, hm, core.HashMapShallowBytes(2))
}

func TestCompiler_NestedConstantMapFoldsAsOne(t *testing.T) {
	c := NewCompiler("test")
	tools := core.NewVector([]core.Value{core.Keyword{V: "read"}, core.Keyword{V: "grep"}})
	hm := core.NewHashMap()
	require.NoError(t, hm.Set(core.Keyword{V: "model"}, core.Keyword{V: "large"}))
	require.NoError(t, hm.Set(core.Keyword{V: "tools"}, tools))

	require.NoError(t, c.Compile(hm))

	requireFoldedLiteral(t, c.Chunk(), 2, hm, core.HashMapShallowBytes(2)+core.VectorShallowBytes(2))
}

func TestCompiler_MixedHashMapUsesConstructionPath(t *testing.T) {
	c := NewCompiler("test")
	hm := core.NewHashMap()
	require.NoError(t, hm.Set(core.Keyword{V: "model"}, core.Symbol{V: "m"}))

	require.NoError(t, c.Compile(hm))

	chunk := c.Chunk()
	require.Len(t, chunk.Code, 5)
	assert.Equal(t, vm.OpStructEnter, chunk.Code[0].Op())
	assert.Equal(t, vm.OpConst, chunk.Code[1].Op())
	assert.Equal(t, vm.OpGetGlobal, chunk.Code[2].Op())
	assert.Equal(t, vm.OpMakeMap, chunk.Code[3].Op())
	assert.Equal(t, 1, chunk.Code[3].A())
	assert.Equal(t, vm.OpStructLeave, chunk.Code[4].Op())
}

func TestCompiler_CaptureAnalysis_Uncaptured(t *testing.T) {
	c := NewCompiler("test")
	form := core.NewList([]core.Value{
		core.Symbol{V: "fn"},
		core.NewVector([]core.Value{core.Symbol{V: "x"}}),
		core.Symbol{V: "x"},
	})
	require.NoError(t, c.Compile(form))
	sub := c.Chunk().SubChunks[0]
	assert.Nil(t, sub.Captured)
	assert.Empty(t, sub.Caps)
}

func TestCompiler_CaptureAnalysis_DirectCapture(t *testing.T) {
	c := NewCompiler("test")
	form := core.NewList([]core.Value{
		core.Symbol{V: "fn"},
		core.NewVector([]core.Value{core.Symbol{V: "x"}}),
		core.NewList([]core.Value{
			core.Symbol{V: "fn"},
			core.Vector{},
			core.Symbol{V: "x"},
		}),
	})
	require.NoError(t, c.Compile(form))
	sub := c.Chunk().SubChunks[0]

	require.NotNil(t, sub.Captured)
	assert.True(t, sub.Captured[0])

	inner := sub.SubChunks[0]
	assert.Nil(t, inner.Captured)
	require.Equal(t, []vm.CapDesc{{Slot: 0}}, inner.Caps)
	require.Equal(t, vm.OpGetCap, inner.Code[0].Op())
	assert.Equal(t, 0, inner.Code[0].A())
}

func TestCompiler_CaptureAnalysis_TransitiveCapture(t *testing.T) {
	c := NewCompiler("test")
	form := core.NewList([]core.Value{
		core.Symbol{V: "fn"},
		core.NewVector([]core.Value{core.Symbol{V: "x"}}),
		core.NewList([]core.Value{
			core.Symbol{V: "fn"},
			core.Vector{},
			core.NewList([]core.Value{
				core.Symbol{V: "fn"},
				core.Vector{},
				core.Symbol{V: "x"},
			}),
		}),
	})
	require.NoError(t, c.Compile(form))
	sub := c.Chunk().SubChunks[0]

	require.NotNil(t, sub.Captured)
	assert.True(t, sub.Captured[0])

	middle := sub.SubChunks[0]
	assert.Nil(t, middle.Captured)
	require.Equal(t, []vm.CapDesc{{Slot: 0}}, middle.Caps)

	inner := middle.SubChunks[0]
	assert.Nil(t, inner.Captured)
	require.Equal(t, []vm.CapDesc{{FromCaps: true, Cap: 0}}, inner.Caps)
	require.Equal(t, vm.OpGetCap, inner.Code[0].Op())
}

func TestCompiler_CaptureAnalysis_LexicalShadowing(t *testing.T) {
	c := NewCompiler("test")
	form := core.NewList([]core.Value{
		core.Symbol{V: "fn"},
		core.NewVector([]core.Value{core.Symbol{V: "x"}}),
		core.NewList([]core.Value{
			core.Symbol{V: "fn"},
			core.NewVector([]core.Value{core.Symbol{V: "x"}}),
			core.Symbol{V: "x"},
		}),
	})
	require.NoError(t, c.Compile(form))
	sub := c.Chunk().SubChunks[0]

	assert.Nil(t, sub.Captured)
	assert.Empty(t, sub.Caps)

	inner := sub.SubChunks[0]
	assert.Nil(t, inner.Captured)
	assert.Empty(t, inner.Caps)
	require.Equal(t, vm.OpGetLocal, inner.Code[0].Op())
}

func TestCompiler_CaptureAnalysis_FnParamVariadic(t *testing.T) {
	c := NewCompiler("test")
	form := core.NewList([]core.Value{
		core.Symbol{V: "fn"},
		core.NewVector([]core.Value{
			core.Symbol{V: "x"},
			core.Symbol{V: "&"},
			core.Symbol{V: "rest"},
		}),
		core.NewList([]core.Value{
			core.Symbol{V: "fn"},
			core.Vector{},
			core.Symbol{V: "rest"},
		}),
	})
	require.NoError(t, c.Compile(form))
	sub := c.Chunk().SubChunks[0]

	require.NotNil(t, sub.Captured)
	assert.False(t, sub.Captured[0])
	assert.True(t, sub.Captured[1])

	inner := sub.SubChunks[0]
	assert.Nil(t, inner.Captured)
	require.Equal(t, []vm.CapDesc{{Slot: 1}}, inner.Caps)
}

func TestCompiler_CaptureAnalysis_Quote(t *testing.T) {
	c := NewCompiler("test")
	form := core.NewList([]core.Value{
		core.Symbol{V: "fn"},
		core.NewVector([]core.Value{core.Symbol{V: "x"}}),
		core.NewList([]core.Value{
			core.Symbol{V: "quote"},
			core.Symbol{V: "x"},
		}),
	})
	require.NoError(t, c.Compile(form))
	sub := c.Chunk().SubChunks[0]

	// quote treats x as data, not a symbol reference
	assert.Nil(t, sub.Captured)
	assert.Empty(t, sub.Caps)
}

func TestCompiler_CaptureAnalysis_QuasiquoteUnquote(t *testing.T) {
	c := NewCompiler("test")
	form := core.NewList([]core.Value{
		core.Symbol{V: "fn"},
		core.NewVector([]core.Value{core.Symbol{V: "x"}}),
		core.NewList([]core.Value{
			core.Symbol{V: "quasiquote"},
			core.NewList([]core.Value{
				core.Symbol{V: "list"},
				core.NewList([]core.Value{
					core.Symbol{V: "unquote"},
					core.Symbol{V: "x"},
				}),
			}),
		}),
	})
	require.NoError(t, c.Compile(form))
	sub := c.Chunk().SubChunks[0]

	// quasiquote compiles ~x, producing OpGetLocal (not a constant)
	assert.Nil(t, sub.Captured)

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

func TestCompiler_CaptureAnalysis_NoLocals(t *testing.T) {
	c := NewCompiler("test")
	form := core.NewList([]core.Value{
		core.Symbol{V: "fn"},
		core.Vector{},
		core.Int{V: 42},
	})
	require.NoError(t, c.Compile(form))
	sub := c.Chunk().SubChunks[0]

	assert.Nil(t, sub.Captured)
	assert.Empty(t, sub.Caps)
}

// TestCompiler_CellEmission_CapturedSlot proves finalize rewrites a captured
// let's binding to OpBindCell and leaves uncaptured slots on plain opcodes.
func TestCompiler_CellEmission_CapturedSlot(t *testing.T) {
	c := NewCompiler("test")
	forms, err := core.Read("(let [x 1 y 2] (fn [] (+ x y)))")
	require.NoError(t, err)
	require.NoError(t, c.Compile(forms[0]))
	c.Chunk().Emit(vm.OpReturn, 0)
	c.MarkCaptures()

	chunk := c.Chunk()
	require.NotNil(t, chunk.Captured)
	assert.True(t, chunk.Captured[0], "x captured")
	assert.True(t, chunk.Captured[1], "y captured")

	var binds, closures int
	for _, inst := range chunk.Code {
		switch inst.Op() {
		case vm.OpBindCell:
			binds++
		case vm.OpClosure:
			closures++
		case vm.OpSetLocal:
			t.Fatalf("captured slot store not rewritten: %v", inst)
		}
	}
	assert.Equal(t, 2, binds)
	assert.Equal(t, 1, closures)

	inner := chunk.SubChunks[0]
	require.Len(t, inner.Caps, 2)
	assert.Equal(t, vm.CapDesc{Slot: 0}, inner.Caps[0])
	assert.Equal(t, vm.CapDesc{Slot: 1}, inner.Caps[1])
}

// TestCompiler_CellEmission_UncapturedSlotUntouched proves slots no closure
// references keep the plain local opcodes byte-for-byte. Uses (list x y)
// rather than (+ x y): a native op over two locals now fuses into
// OpFusedNativeOp, which reads its operands directly and never emits
// OpGetLocal at all — the wrong vehicle for probing finalize's rewrite pass.
func TestCompiler_CellEmission_UncapturedSlotUntouched(t *testing.T) {
	c := NewCompiler("test")
	forms, err := core.Read("(let [x 1 y 2] (list x y))")
	require.NoError(t, err)
	require.NoError(t, c.Compile(forms[0]))
	c.Chunk().Emit(vm.OpReturn, 0)
	c.MarkCaptures()

	assert.Nil(t, c.Chunk().Captured)
	var sets, gets int
	for _, inst := range c.Chunk().Code {
		switch inst.Op() {
		case vm.OpSetLocal:
			sets++
		case vm.OpGetLocal:
			gets++
		case vm.OpGetCell, vm.OpSetCell, vm.OpBindCell, vm.OpGetCap, vm.OpSetCap:
			t.Fatalf("cell opcode on uncaptured chunk: %v", inst)
		}
	}
	assert.Equal(t, 2, sets)
	assert.Equal(t, 2, gets)
}

// TestCompiler_CellEmission_SetOnCaptured proves set! on a captured own local
// rewrites to OpSetCell (write-through), not OpBindCell.
func TestCompiler_CellEmission_SetOnCaptured(t *testing.T) {
	c := NewCompiler("test")
	forms, err := core.Read("(let [x 1] (fn [] x) (set! x 5) x)")
	require.NoError(t, err)
	require.NoError(t, c.Compile(forms[0]))
	c.Chunk().Emit(vm.OpReturn, 0)
	c.MarkCaptures()

	var bindCell, setCell, getCell int
	for _, inst := range c.Chunk().Code {
		switch inst.Op() {
		case vm.OpBindCell:
			bindCell++
		case vm.OpSetCell:
			setCell++
		case vm.OpGetCell:
			getCell++
		}
	}
	assert.Equal(t, 1, bindCell, "binding site boxes once")
	assert.Equal(t, 1, setCell, "set! writes through")
	assert.Equal(t, 1, getCell, "own read derefs")
}

func TestCompiler_CellEmission_RecurOnCapturedSlotBindsFreshCell(t *testing.T) {
	c := NewCompiler("test")
	forms, err := core.Read("(loop [i 0 acc []] (if (< i 2) (recur (+ i 1) (conj acc (fn [] i))) acc))")
	require.NoError(t, err)
	require.NoError(t, c.Compile(forms[0]))
	c.Chunk().Emit(vm.OpReturn, 0)
	c.MarkCaptures()

	var bindCell, setCell int
	for _, inst := range c.Chunk().Code {
		switch inst.Op() {
		case vm.OpBindCell:
			bindCell++
		case vm.OpSetCell:
			setCell++
		}
	}
	assert.Equal(t, 2, bindCell, "initial bind and recur rebind both box captured i")
	assert.Zero(t, setCell, "recur rebinds captured loop slot instead of writing through")
}

func TestCompiler_CellEmission_RecurOnUncapturedSlotUntouched(t *testing.T) {
	c := NewCompiler("test")
	forms, err := core.Read("(loop [i 0 acc 0] (if (< i 2) (recur (+ i 1) (+ acc i)) acc))")
	require.NoError(t, err)
	require.NoError(t, c.Compile(forms[0]))
	c.Chunk().Emit(vm.OpReturn, 0)
	c.MarkCaptures()

	for _, inst := range c.Chunk().Code {
		switch inst.Op() {
		case vm.OpGetCell, vm.OpSetCell, vm.OpBindCell:
			t.Fatalf("uncaptured loop emitted cell opcode: %v", inst)
		}
	}
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

func TestCompiler_ChargesReductionsPerInstruction(t *testing.T) {
	ctx := core.WithEvalResourceLimits(t.Context(), 2, 1<<20)
	meter := core.EvalMeterFrom(ctx)
	c := NewCompiler("test")
	c.SetEvalMeter(meter)

	err := c.Compile(core.NewVector([]core.Value{core.Int{V: 1}, core.Int{V: 2}}))
	require.Error(t, err)

	var lerr *core.LispicoError
	require.ErrorAs(t, err, &lerr)
	assert.Equal(t, core.CodeResourceLimit, lerr.Code)
	assert.Equal(t, int64(3), meter.Snapshot().Reductions)
}

func TestCompiler_DeepEmitStopsAtReductionLimit(t *testing.T) {
	ctx := core.WithEvalResourceLimits(t.Context(), 1, 1<<20)
	meter := core.EvalMeterFrom(ctx)
	c := NewCompiler("test")
	c.SetEvalMeter(meter)

	items := make([]core.Value, 0, 257)
	items = append(items, core.Symbol{V: "and"})
	for i := range 256 {
		items = append(items, core.Int{V: int64(i)})
	}
	err := c.Compile(core.NewList(items))
	require.Error(t, err)

	var lerr *core.LispicoError
	require.ErrorAs(t, err, &lerr)
	assert.Equal(t, core.CodeResourceLimit, lerr.Code)
	assert.Equal(t, int64(2), meter.Snapshot().Reductions)
	require.Len(t, c.Chunk().Code, 1)
	assert.Equal(t, vm.OpConst, c.Chunk().Code[0].Op())

	require.Error(t, c.EmitReturn())
	require.Len(t, c.Chunk().Code, 1)
}

func nestedCallForm(depth int) core.Value {
	var v core.Value = core.Symbol{V: "x"}
	for range depth {
		v = core.NewList([]core.Value{core.Symbol{V: "f"}, v})
	}
	return v
}

func TestCompiler_MacroExpandedFormDepthLimit(t *testing.T) {
	c := NewCompiler("test")
	err := c.Compile(nestedCallForm(core.MaxCompileDepth + 1))
	require.Error(t, err)

	var lerr *core.LispicoError
	require.ErrorAs(t, err, &lerr)
	assert.Equal(t, core.CodeResourceLimit, lerr.Code)
}

func TestCompiler_LiteralDepthLimit(t *testing.T) {
	var v core.Value = core.Int{V: 1}
	for range core.MaxCompileDepth + 1 {
		v = core.NewVector([]core.Value{v})
	}
	m := core.NewHashMap()
	require.NoError(t, m.Set(core.Keyword{V: "x"}, v))
	c := NewCompiler("test")
	err := c.Compile(core.NewList([]core.Value{core.Symbol{V: "quasiquote"}, m}))
	require.Error(t, err)

	var lerr *core.LispicoError
	require.ErrorAs(t, err, &lerr)
	assert.Equal(t, core.CodeResourceLimit, lerr.Code)
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
	form := core.NewList([]core.Value{
		core.Symbol{V: "let"},
		core.NewVector([]core.Value{
			core.Int{V: 1},
			core.Int{V: 2},
		}),
		core.Int{V: 1},
	})
	err := c.Compile(form)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "vector")
	assert.Contains(t, err.Error(), "pair")
	assert.Contains(t, err.Error(), "must be a symbol")
}

func TestCompiler_FnNonSymbolRestParam(t *testing.T) {
	c := NewCompiler("test")
	form := core.NewList([]core.Value{
		core.Symbol{V: "fn"},
		core.NewVector([]core.Value{
			core.Symbol{V: "a"},
			core.Symbol{V: "&"},
			core.Int{V: 5},
		}),
		core.Symbol{V: "a"},
	})
	err := c.Compile(form)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected symbol")
}

func TestCompiler_Fn_EmptyBody(t *testing.T) {
	t.Run("no body forms", func(t *testing.T) {
		c := NewCompiler("test")
		form := core.NewList([]core.Value{
			core.Symbol{V: "fn"},
			core.Vector{},
		})
		err := c.Compile(form)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "at least 2 arguments")
	})

	t.Run("no params at all", func(t *testing.T) {
		c := NewCompiler("test")
		form := core.NewList([]core.Value{
			core.Symbol{V: "fn"},
		})
		err := c.Compile(form)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "at least 2 arguments")
	})
}

func TestCompiler_Defn_EmptyBody(t *testing.T) {
	c := NewCompiler("test")
	form := core.NewList([]core.Value{
		core.Symbol{V: "defn"},
		core.Symbol{V: "f"},
		core.Vector{},
	})
	err := c.Compile(form)
	require.Error(t, err)
}

// TestCompiler_Defmacro_TopLevelCompiles: a defmacro that is the whole form
// compiles. Nested ones are covered by TestUnsupported_NestedDefmacro.
func TestCompiler_Defmacro_TopLevelCompiles(t *testing.T) {
	c := NewCompiler("test")
	form := core.NewList([]core.Value{
		core.Symbol{V: "defmacro"},
		core.Symbol{V: "id"},
		core.NewVector([]core.Value{core.Symbol{V: "x"}}),
		core.Symbol{V: "x"},
	})
	require.NoError(t, c.Compile(form))
	require.NoError(t, c.EmitReturn())
	require.NoError(t, c.Chunk().Validate())
}

func TestCompiler_UnquoteSplicing_Unsupported(t *testing.T) {
	c := NewCompiler("test")
	form := core.NewList([]core.Value{
		core.Symbol{V: "quasiquote"},
		core.NewList([]core.Value{
			core.NewList([]core.Value{
				core.Symbol{V: "unquote-splicing"},
				core.Symbol{V: "xs"},
			}),
		}),
	})
	err := c.Compile(form)
	require.Error(t, err)

	var lispErr *core.LispicoError
	require.ErrorAs(t, err, &lispErr)
	assert.Equal(t, CodeUnsupported, lispErr.Code)
}

// TestUnsupported_NestedDefmacro pins the fallback's real boundary. A defmacro
// that is the whole form compiles; one nested inside a larger form does not,
// and the reason is not incidental: macro expansion is a pre-pass over the
// whole form, so a sibling use of a macro defined in that same form would
// compile as a plain call and fail at run time. unquote-splicing is the only
// other trigger.
func TestUnsupported_NestedDefmacro(t *testing.T) {
	mustUnsupported := func(t *testing.T, src string) {
		t.Helper()
		forms, err := core.Read(src)
		require.NoError(t, err)
		err = NewCompiler("test").Compile(forms[0])
		var le *core.LispicoError
		if !errors.As(err, &le) || le.Code != CodeUnsupported {
			t.Fatalf("%s: err = %v, want code %s", src, err, CodeUnsupported)
		}
	}

	forms, err := core.Read(`(defmacro m (x) x)`)
	require.NoError(t, err)
	require.NoError(t, NewCompiler("test").Compile(forms[0]),
		"a top-level defmacro must compile")

	mustUnsupported(t, `(do (defmacro m (x) x))`)
	mustUnsupported(t, `(if true (defmacro m (x) x) nil)`)
	mustUnsupported(t, "`(a ~@b)")
}
