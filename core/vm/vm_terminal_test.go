package vm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

func terminalTryCallChunk(fnName string) *Chunk {
	chunk := &Chunk{
		Name: "terminal-try-call",
		Constants: []core.Value{
			core.Symbol{V: fnName},
			core.String{V: "caught"},
		},
		Code: []Instruction{
			Encode(OpSetupTry, 5),
			Encode(OpGetGlobal, 0),
			Encode(OpCall, 0),
			Encode(OpPopTry, 0),
			Encode(OpReturn, 0),
			Encode(OpPop, 0),
			Encode(OpConst, 1),
			Encode(OpReturn, 0),
		},
	}
	chunk.EnsureSites()
	return chunk
}

func terminalTryClosureCallChunk(fnName string) *Chunk {
	inner := &Chunk{
		Name: "terminal-inner-call",
		Constants: []core.Value{
			core.Symbol{V: fnName},
		},
		Code: []Instruction{
			Encode(OpGetGlobal, 0),
			Encode(OpCall, 0),
			Encode(OpReturn, 0),
		},
	}
	inner.EnsureSites()

	outer := &Chunk{
		Name:      "terminal-outer-call",
		SubChunks: []*Chunk{inner},
		Constants: []core.Value{
			core.String{V: "caught"},
		},
		Code: []Instruction{
			Encode(OpSetupTry, 5),
			Encode(OpClosure, 0),
			Encode(OpCall, 0),
			Encode(OpPopTry, 0),
			Encode(OpReturn, 0),
			Encode(OpPop, 0),
			Encode(OpConst, 0),
			Encode(OpReturn, 0),
		},
	}
	outer.EnsureSites()
	return outer
}

func terminalTryTailCallChunk(fnName string) *Chunk {
	inner := &Chunk{
		Name: "terminal-inner-tail-call",
		Constants: []core.Value{
			core.Symbol{V: fnName},
		},
		Code: []Instruction{
			Encode(OpGetGlobal, 0),
			Encode(OpTailCall, 0),
			Encode(OpReturn, 0),
		},
	}
	inner.EnsureSites()

	outer := &Chunk{
		Name:      "terminal-outer-tail-call",
		SubChunks: []*Chunk{inner},
		Constants: []core.Value{
			core.String{V: "caught"},
		},
		Code: []Instruction{
			Encode(OpSetupTry, 5),
			Encode(OpClosure, 0),
			Encode(OpCall, 0),
			Encode(OpPopTry, 0),
			Encode(OpReturn, 0),
			Encode(OpPop, 0),
			Encode(OpConst, 0),
			Encode(OpReturn, 0),
		},
	}
	outer.EnsureSites()
	return outer
}

func terminalTryNativeChunk() *Chunk {
	chunk := &Chunk{
		Name: "terminal-try-native",
		Constants: []core.Value{
			core.Symbol{V: "+"},
			core.Int{V: 1},
			core.Int{V: 2},
			core.String{V: "caught"},
		},
		Code: []Instruction{
			Encode(OpSetupTry, 7),
			Encode(OpFreezeNative, 0),
			Encode(OpConst, 1),
			Encode(OpConst, 2),
			Encode(OpAdd, 2),
			Encode(OpPopTry, 0),
			Encode(OpReturn, 0),
			Encode(OpPop, 0),
			Encode(OpConst, 3),
			Encode(OpReturn, 0),
		},
	}
	chunk.EnsureSites()
	return chunk
}

func terminalClosureWithFreezeChunk(fnName string) *Chunk {
	inner := &Chunk{
		Name: "terminal-freeze-inner",
		Constants: []core.Value{
			core.Symbol{V: "+"},
			core.Symbol{V: fnName},
		},
		Code: []Instruction{
			Encode(OpFreezeNative, 0),
			Encode(OpGetGlobal, 1),
			Encode(OpCall, 0),
			Encode(OpReturn, 0),
		},
	}
	inner.EnsureSites()

	outer := &Chunk{
		Name:      "terminal-freeze-outer",
		SubChunks: []*Chunk{inner},
		Constants: []core.Value{
			core.String{V: "caught"},
		},
		Code: []Instruction{
			Encode(OpSetupTry, 5),
			Encode(OpClosure, 0),
			Encode(OpCall, 0),
			Encode(OpPopTry, 0),
			Encode(OpReturn, 0),
			Encode(OpPop, 0),
			Encode(OpConst, 0),
			Encode(OpReturn, 0),
		},
	}
	outer.EnsureSites()
	return outer
}

func throwingStringChunk() *Chunk {
	return &Chunk{
		Name: "throwing-string",
		Constants: []core.Value{
			core.String{V: "context deadline exceeded"},
		},
		Code: []Instruction{
			Encode(OpSetupTry, 5),
			Encode(OpConst, 0),
			Encode(OpThrow, 0),
			Encode(OpPopTry, 0),
			Encode(OpReturn, 0),
			Encode(OpReturn, 0),
		},
	}
}

func cancelingGoFunc(name string, cancel context.CancelFunc) core.GoFunc {
	return core.GoFunc{Name: name, Fn: func(ctx context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
		cancel()
		return nil, ctx.Err()
	}}
}

func TestVM_ResourceLimitInClosureNotCaught(t *testing.T) {
	t.Parallel()

	env := core.NewEnv(nil)
	env.Set("limit", core.GoFunc{Name: "limit", Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
		return nil, core.NewResourceLimitError("limit")
	}})

	v := New(env)
	_, err := v.Run(context.Background(), terminalTryClosureCallChunk("limit"))
	require.Error(t, err)

	var lerr *core.LispicoError
	require.ErrorAs(t, err, &lerr)
	assert.Equal(t, core.CodeResourceLimit, lerr.Code)
}

func TestVM_CanceledThroughOpCallNotCaught(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		chunk func() *Chunk
		bind  string
	}{
		{name: "op call", chunk: func() *Chunk { return terminalTryCallChunk("cancel-now") }, bind: "cancel-now"},
		{name: "op tail call", chunk: func() *Chunk { return terminalTryTailCallChunk("cancel-now") }, bind: "cancel-now"},
		{name: "native op fallback", chunk: terminalTryNativeChunk, bind: "+"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			env := core.NewEnv(nil)
			env.Set(tt.bind, cancelingGoFunc(tt.bind, cancel))

			v := New(env)
			_, err := v.Run(ctx, tt.chunk())
			require.ErrorIs(t, err, context.Canceled)
		})
	}
}

func TestVM_TerminalErrorUnwindsStacks(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	env := core.NewEnv(nil)
	env.SetCanonical("+", core.GoFunc{Name: "+", Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
		return core.Int{V: 0}, nil
	}})
	env.Set("cancel-now", cancelingGoFunc("cancel-now", cancel))

	v := New(env)
	_, err := v.Run(ctx, terminalClosureWithFreezeChunk("cancel-now"))
	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, v.frames)
	assert.Empty(t, v.handlers)
	assert.Empty(t, v.freezeStack)
	assert.Equal(t, 0, v.depth)
	assert.Equal(t, 0, v.stackSize())
}

func TestVM_ThrowStringStaysCatchable(t *testing.T) {
	t.Parallel()

	v := New(core.NewEnv(nil))
	result, err := v.Run(context.Background(), throwingStringChunk())
	require.NoError(t, err)
	assert.True(t, result.Equals(core.String{V: "context deadline exceeded"}), "got %v", result)
}
