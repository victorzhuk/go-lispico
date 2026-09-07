package vm

import (
	"context"
	"fmt"
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

// terminalTryClosureCounterCatchChunk mirrors terminalTryClosureCallChunk but
// gives the handler body a side effect: it invokes counterFn before returning
// "caught", so a test can prove a terminal error never reached the handler.
func terminalTryClosureCounterCatchChunk(fnName, counterFn string) *Chunk {
	inner := &Chunk{
		Name: "terminal-counter-inner-call",
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
		Name:      "terminal-counter-outer-call",
		SubChunks: []*Chunk{inner},
		Constants: []core.Value{
			core.Symbol{V: counterFn},
			core.String{V: "caught"},
		},
		Code: []Instruction{
			Encode(OpSetupTry, 5),  // 0: handler at 5
			Encode(OpClosure, 0),   // 1
			Encode(OpCall, 0),      // 2
			Encode(OpPopTry, 0),    // 3
			Encode(OpReturn, 0),    // 4
			Encode(OpPop, 0),       // 5: handler — discard caught value
			Encode(OpGetGlobal, 0), // 6: counterFn
			Encode(OpCall, 0),      // 7
			Encode(OpPop, 0),       // 8
			Encode(OpConst, 1),     // 9
			Encode(OpReturn, 0),    // 10
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

	t.Run("handler side effects stay absent", func(t *testing.T) {
		t.Parallel()

		env := core.NewEnv(nil)
		handlerCalls := 0
		env.Set("count", core.GoFunc{Name: "count", Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
			handlerCalls++
			return core.Nil{}, nil
		}})
		env.Set("limit", core.GoFunc{Name: "limit", Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
			return nil, core.NewResourceLimitError("limit")
		}})

		v := New(env)
		_, err := v.Run(context.Background(), terminalTryClosureCounterCatchChunk("limit", "count"))
		require.Error(t, err)

		var lerr *core.LispicoError
		require.ErrorAs(t, err, &lerr)
		assert.Equal(t, core.CodeResourceLimit, lerr.Code)
		assert.Zero(t, handlerCalls, "want 0 handler side effects for a terminal resource-limit error, got %d", handlerCalls)
		assert.Empty(t, v.frames, "want 0 frames after the terminal reset, got %d", len(v.frames))
		assert.Empty(t, v.handlers, "want 0 handlers after the terminal reset, got %d", len(v.handlers))
		assert.Empty(t, v.freezeStack, "want 0 freeze records after the terminal reset, got %d", len(v.freezeStack))
		assert.Equal(t, 0, v.depth, "want closure depth 0 after the terminal reset, got %d", v.depth)
		assert.Equal(t, 0, v.stackSize(), "want 0 operands after the terminal reset, got %d", v.stackSize())
	})
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

	t.Run("wrapped terminal errors leave handler side effects absent", func(t *testing.T) {
		t.Parallel()

		for _, tt := range []struct {
			name  string
			cause error
		}{
			{name: "wrapped canceled", cause: context.Canceled},
			{name: "wrapped deadline", cause: context.DeadlineExceeded},
		} {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				env := core.NewEnv(nil)
				handlerCalls := 0
				env.Set("count", core.GoFunc{Name: "count", Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
					handlerCalls++
					return core.Nil{}, nil
				}})
				env.Set("cancel-now", core.GoFunc{Name: "cancel-now", Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
					return nil, fmt.Errorf("call failed: %w", tt.cause)
				}})

				v := New(env)
				_, err := v.Run(context.Background(), terminalTryClosureCounterCatchChunk("cancel-now", "count"))
				require.ErrorIs(t, err, tt.cause)
				assert.Zero(t, handlerCalls, "want 0 handler side effects for a wrapped terminal error, got %d", handlerCalls)
				assert.Empty(t, v.frames, "want 0 frames after the terminal reset, got %d", len(v.frames))
				assert.Empty(t, v.handlers, "want 0 handlers after the terminal reset, got %d", len(v.handlers))
				assert.Empty(t, v.freezeStack, "want 0 freeze records after the terminal reset, got %d", len(v.freezeStack))
				assert.Equal(t, 0, v.depth, "want closure depth 0 after the terminal reset, got %d", v.depth)
				assert.Equal(t, 0, v.stackSize(), "want 0 operands after the terminal reset, got %d", v.stackSize())
			})
		}
	})
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

// TestVMErrorUnwindReuse seals the error-unwind/reuse seam: an ordinary
// failure during native argument evaluation under nested closure and try must
// unwind to the nearest handler with operands, callee freeze records, and the
// structural-depth allowance restored; pending charges must settle without
// being outranked except by a terminal settlement error, which must bypass
// the handler; and the VM must stay fit for immediate reuse.
func TestVMErrorUnwindReuse(t *testing.T) {
	t.Parallel()

	t.Run("native argument failure leaves no stale freeze or operands", func(t *testing.T) {
		t.Parallel()

		env := core.NewEnv(nil)
		env.SetCanonical("+", core.GoFunc{Name: "+", Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
			var sum int64
			for _, a := range args {
				sum += a.(core.Int).V
			}
			return core.Int{V: sum}, nil
		}})
		env.Set("rebind-plus", core.GoFunc{Name: "rebind-plus", Fn: func(_ context.Context, _ core.Evaluator, _ []core.Value, e *core.Env) (core.Value, error) {
			e.Set("+", core.GoFunc{Name: "+", Fn: func(_ context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
				return core.Int{V: 999}, nil
			}})
			return core.Nil{}, nil
		}})

		inner := &Chunk{
			Name: "unwind-reuse-inner",
			Constants: []core.Value{
				core.Symbol{V: "missing"},
			},
			Code: []Instruction{
				Encode(OpGetGlobal, 0), // native argument evaluation fails here
				Encode(OpReturn, 0),
			},
		}
		inner.EnsureSites()

		chunk := &Chunk{
			Name:      "unwind-reuse-freeze",
			SubChunks: []*Chunk{inner},
			Constants: []core.Value{
				core.Symbol{V: "+"},
				core.Symbol{V: "rebind-plus"},
				core.Int{V: 1},
				core.Int{V: 2},
			},
			Code: []Instruction{
				Encode(OpSetupTry, 6),     // 0: handler at 6
				Encode(OpFreezeNative, 0), // 1: freeze canonical +
				Encode(OpClosure, 0),      // 2
				Encode(OpCall, 0),         // 3: inner raises while evaluating the argument
				Encode(OpPopTry, 0),       // 4
				Encode(OpReturn, 0),       // 5
				Encode(OpPop, 0),          // 6: handler — discard caught message
				Encode(OpGetGlobal, 1),    // 7: rebind-plus
				Encode(OpCall, 0),         // 8: rebind + to a 999 fn
				Encode(OpPop, 0),          // 9
				Encode(OpConst, 2),        // 10
				Encode(OpConst, 3),        // 11
				Encode(OpAdd, 2),          // 12: must recover past the truncated freeze — 999, not the stale canonical 3
				Encode(OpReturn, 0),       // 13
			},
		}
		chunk.EnsureSites()

		v := New(env)
		result, err := v.Run(context.Background(), chunk)
		require.NoError(t, err)
		assert.True(t, result.Equals(core.Int{V: 999}),
			"want 999 from the rebound operator, got %v: a stale canonical freeze record or operand survived the unwind", result)
		assert.Empty(t, v.freezeStack, "want 0 callee freeze records after the caught unwind, got %d", len(v.freezeStack))
		assert.Empty(t, v.frames, "want 0 aborted frames after the caught unwind, got %d", len(v.frames))
		assert.Empty(t, v.handlers, "want 0 live handlers after the caught unwind, got %d", len(v.handlers))
		assert.Equal(t, 0, v.depth, "want closure depth 0 after unwinding the closure frame, got %d", v.depth)
		assert.Equal(t, 0, v.stackSize(), "want 0 operands after the caught unwind, got %d", v.stackSize())
		assert.Equal(t, int64(0), v.structDepthLoad(), "want structural depth 0 after the caught unwind, got %d", v.structDepthLoad())
	})

	t.Run("nearest handler catches and continuation runs once", func(t *testing.T) {
		t.Parallel()

		env := core.NewEnv(nil)
		continuations := 0
		env.Set("count", core.GoFunc{Name: "count", Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
			continuations++
			return core.Nil{}, nil
		}})
		handled := 0
		env.Set("handled", core.GoFunc{Name: "handled", Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
			handled++
			return core.Nil{}, nil
		}})

		inner := &Chunk{
			Name: "unwind-reuse-nearest-inner",
			Constants: []core.Value{
				core.Symbol{V: "missing"},
				core.Symbol{V: "handled"},
				core.String{V: "inner-caught"},
			},
			Code: []Instruction{
				Encode(OpSetupTry, 5),  // 0: inner handler at 5
				Encode(OpGetGlobal, 0), // 1: raises
				Encode(OpPopTry, 0),    // 2
				Encode(OpReturn, 0),    // 3
				Encode(OpPop, 0),       // 5: inner handler — discard caught message
				Encode(OpGetGlobal, 1), // 6
				Encode(OpCall, 0),      // 7
				Encode(OpPop, 0),       // 8
				Encode(OpConst, 2),     // 9
				Encode(OpReturn, 0),    // 10
			},
		}
		inner.EnsureSites()

		outer := &Chunk{
			Name:      "unwind-reuse-nearest-outer",
			SubChunks: []*Chunk{inner},
			Constants: []core.Value{
				core.String{V: "outer-caught"},
				core.Symbol{V: "count"},
				core.String{V: "done"},
			},
			Code: []Instruction{
				Encode(OpSetupTry, 9),  // 0: outer handler at 9
				Encode(OpClosure, 0),   // 1
				Encode(OpCall, 0),      // 2
				Encode(OpPopTry, 0),    // 3
				Encode(OpGetGlobal, 1), // 4: continuation after the handled call
				Encode(OpCall, 0),      // 5
				Encode(OpPop, 0),       // 6
				Encode(OpConst, 2),     // 7
				Encode(OpReturn, 0),    // 8
				Encode(OpPop, 0),       // 9: outer handler — must not run
				Encode(OpConst, 0),     // 10
				Encode(OpReturn, 0),    // 11
			},
		}
		outer.EnsureSites()

		v := New(env)
		result, err := v.Run(context.Background(), outer)
		require.NoError(t, err)
		assert.True(t, result.Equals(core.String{V: "done"}),
			"want the continuation value, got %v: the outer handler must not intercept an error its inner handler covers", result)
		assert.Equal(t, 1, continuations, "want the continuation to execute exactly once, got %d", continuations)
		assert.Equal(t, 1, handled, "want the inner handler side effect to execute exactly once, got %d", handled)
		assert.Empty(t, v.handlers, "want 0 live handlers after the nested unwind, got %d", len(v.handlers))
		assert.Empty(t, v.frames, "want 0 frames after the nested unwind, got %d", len(v.frames))
		assert.Empty(t, v.freezeStack, "want 0 freeze records after the nested unwind, got %d", len(v.freezeStack))
	})

	t.Run("structural depth allowance restored after interrupted construction", func(t *testing.T) {
		t.Parallel()

		v := New(core.NewEnv(nil), WithMaxStructuralDepth(2))
		chunk := &Chunk{
			Name: "unwind-reuse-struct",
			Constants: []core.Value{
				core.String{V: "boom"},
				core.Int{V: 7},
				core.Int{V: 8},
			},
			Code: []Instruction{
				Encode(OpSetupTry, 5),    // 0: handler at 5
				Encode(OpStructEnter, 2), // 1: enter, then fail before OpStructLeave
				Encode(OpConst, 0),       // 2
				Encode(OpThrow, 0),       // 3
				Encode(OpStructLeave, 2), // 4: unreached
				Encode(OpPop, 0),         // 5: handler — discard caught value
				Encode(OpStructEnter, 2), // 6: exact-limit construction inside the catch
				Encode(OpConst, 1),       // 7
				Encode(OpConst, 2),       // 8
				Encode(OpMakeVector, 2),  // 9
				Encode(OpStructLeave, 2), // 10
				Encode(OpReturn, 0),      // 11
			},
		}
		chunk.EnsureSites()

		result, err := v.Run(context.Background(), chunk)
		require.NoError(t, err, "the catch body must see the full structural-depth allowance")
		assert.True(t, result.Equals(core.NewVector([]core.Value{core.Int{V: 7}, core.Int{V: 8}})),
			"want the exact-limit vector, got %v", result)
		assert.Equal(t, int64(0), v.structDepthLoad(), "want structural depth 0 after the caught unwind, got %d", v.structDepthLoad())

		// Same VM, no Reset: the allowance must be intact, not reduced.
		exact := &Chunk{
			Name: "unwind-reuse-struct-exact",
			Constants: []core.Value{
				core.Int{V: 9},
			},
			Code: []Instruction{
				Encode(OpStructEnter, 2),
				Encode(OpConst, 0),
				Encode(OpConst, 0),
				Encode(OpMakeVector, 2),
				Encode(OpStructLeave, 2),
				Encode(OpReturn, 0),
			},
		}
		exact.EnsureSites()
		result, err = v.Run(context.Background(), exact)
		require.NoError(t, err, "a later evaluation on the same VM must see the full structural-depth allowance")
		assert.True(t, result.Equals(core.NewVector([]core.Value{core.Int{V: 9}, core.Int{V: 9}})),
			"want the exact-limit vector, got %v", result)
		assert.Equal(t, int64(0), v.structDepthLoad(), "want structural depth 0 after the later run, got %d", v.structDepthLoad())
	})

	t.Run("pending charges settle when the handler catches an ordinary failure", func(t *testing.T) {
		t.Parallel()

		env := core.NewEnv(nil)
		env.Set("scalar", core.GoFunc{Name: "scalar", Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
			return core.Int{V: 1}, nil
		}})
		env.Set("fail-now", core.GoFunc{Name: "fail-now", Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
			return nil, fmt.Errorf("scalar budget exhausted")
		}})

		chunk := &Chunk{
			Name: "unwind-reuse-settle",
			Constants: []core.Value{
				core.Symbol{V: "scalar"},
				core.Symbol{V: "fail-now"},
				core.Int{V: 0},
			},
			Code: []Instruction{
				Encode(OpSetupTry, 12), // 0: handler at 12
				Encode(OpGetGlobal, 0), // 1
				Encode(OpCall, 0),      // 2: first scalar result pending
				Encode(OpPop, 0),       // 3
				Encode(OpGetGlobal, 0), // 4
				Encode(OpCall, 0),      // 5: second scalar result pending
				Encode(OpPop, 0),       // 6
				Encode(OpGetGlobal, 1), // 7: ordinary GoFunc failure
				Encode(OpCall, 0),      // 8
				Encode(OpPopTry, 0),    // 9: unreached
				Encode(OpConst, 2),     // 10: unreached success value
				Encode(OpReturn, 0),    // 11: unreached
				Encode(OpReturn, 0),    // 12: handler — return the caught message
			},
		}
		chunk.EnsureSites()

		ctx := core.WithEvalResourceLimits(t.Context(), 1_000_000, 1_000_000)
		meter := core.EvalMeterFrom(ctx)

		v := New(env)
		v.SetEvalMeter(meter)
		result, err := v.Run(ctx, chunk)
		require.NoError(t, err)
		assert.True(t, result.Equals(core.String{V: "scalar budget exhausted"}),
			"want the handler to receive the original error text, got %v: settlement must preserve the original error", result)

		snap := meter.Snapshot()
		assert.Equal(t, 2*core.MeterScalarBytes, snap.AllocationBytes,
			"want both pending scalar results settled (2 x MeterScalarBytes), got %d", snap.AllocationBytes)
	})

	t.Run("settlement limit outranks the pending error and bypasses the handler", func(t *testing.T) {
		t.Parallel()

		env := core.NewEnv(nil)
		env.Set("scalar", core.GoFunc{Name: "scalar", Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
			return core.Int{V: 1}, nil
		}})
		env.Set("fail-now", core.GoFunc{Name: "fail-now", Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
			return nil, fmt.Errorf("scalar budget exhausted")
		}})
		handlerCalls := 0
		env.Set("count", core.GoFunc{Name: "count", Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
			handlerCalls++
			return core.Nil{}, nil
		}})

		chunk := &Chunk{
			Name: "unwind-reuse-settle-terminal",
			Constants: []core.Value{
				core.Symbol{V: "scalar"},
				core.Symbol{V: "fail-now"},
				core.Int{V: 0},
				core.String{V: "caught"},
			},
			Code: []Instruction{
				Encode(OpSetupTry, 10), // 0: handler at 10
				Encode(OpGetGlobal, 0), // 1
				Encode(OpCall, 0),      // 2: first scalar result pending
				Encode(OpPop, 0),       // 3
				Encode(OpGetGlobal, 0), // 4
				Encode(OpCall, 0),      // 5: second scalar result pending
				Encode(OpPop, 0),       // 6
				Encode(OpPopTry, 0),    // 7
				Encode(OpConst, 2),     // 8: unreached success value
				Encode(OpReturn, 0),    // 9
				Encode(OpPop, 0),       // 10: handler — must not run
				Encode(OpGetGlobal, 2), // 11: count
				Encode(OpCall, 0),      // 12
				Encode(OpPop, 0),       // 13
				Encode(OpConst, 3),     // 14
				Encode(OpReturn, 0),    // 15
			},
		}
		chunk.EnsureSites()

		v := New(env)
		v.SetResourceLimits(0, int(2*core.MeterScalarBytes-1)) // ceiling one byte below the settled total
		_, err := v.Run(context.Background(), chunk)

		var lerr *core.LispicoError
		require.ErrorAs(t, err, &lerr)
		assert.Equal(t, core.CodeResourceLimit, lerr.Code,
			"want the terminal settlement error to replace the pending ordinary error, got code %s", lerr.Code)
		assert.Zero(t, handlerCalls, "want 0 handler side effects for a terminal settlement error, got %d", handlerCalls)
		assert.Empty(t, v.frames, "want 0 frames after the terminal reset, got %d", len(v.frames))
		assert.Empty(t, v.handlers, "want 0 handlers after the terminal reset, got %d", len(v.handlers))
		assert.Empty(t, v.freezeStack, "want 0 freeze records after the terminal reset, got %d", len(v.freezeStack))
		assert.Equal(t, 0, v.depth, "want closure depth 0 after the terminal reset, got %d", v.depth)
		assert.Equal(t, 0, v.stackSize(), "want 0 operands after the terminal reset, got %d", v.stackSize())
		assert.Equal(t, int64(0), v.structDepthLoad(), "want structural depth 0 after the terminal reset, got %d", v.structDepthLoad())

		// The terminal reset leaves the public VM fit for immediate reuse.
		probe := &Chunk{
			Name: "unwind-reuse-after-terminal",
			Constants: []core.Value{
				core.Symbol{V: "scalar"},
			},
			Code: []Instruction{
				Encode(OpGetGlobal, 0),
				Encode(OpCall, 0),
				Encode(OpReturn, 0),
			},
		}
		probe.EnsureSites()
		result, err := v.Run(context.Background(), probe)
		require.NoError(t, err, "a terminal failure must leave the VM fit for the next run")
		assert.True(t, result.Equals(core.Int{V: 1}), "want 1 from the post-terminal run, got %v", result)
	})

	t.Run("later ApplyPooled succeeds after a caught failure", func(t *testing.T) {
		t.Parallel()

		env := core.NewEnv(nil)
		env.Set("fail-now", core.GoFunc{Name: "fail-now", Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
			return nil, fmt.Errorf("call failed")
		}})

		v := New(env)
		_, err := v.Run(context.Background(), terminalTryCallChunk("fail-now"))
		require.NoError(t, err, "the fixture handler must catch the ordinary failure")
		assert.Empty(t, v.frames, "want 0 frames after the caught failure, got %d", len(v.frames))
		assert.Empty(t, v.handlers, "want 0 handlers after the caught failure, got %d", len(v.handlers))
		assert.Empty(t, v.freezeStack, "want 0 freeze records after the caught failure, got %d", len(v.freezeStack))
		assert.Equal(t, 0, v.depth, "want closure depth 0 after the caught failure, got %d", v.depth)
		assert.Equal(t, 0, v.stackSize(), "want 0 operands after the caught failure, got %d", v.stackSize())
		assert.Equal(t, int64(0), v.structDepthLoad(), "want structural depth 0 after the caught failure, got %d", v.structDepthLoad())

		v.Reset()
		callee := &Chunk{
			Name:  "unwind-reuse-callee",
			Arity: 1,
			Code: []Instruction{
				Encode(OpGetLocal, 0),
				Encode(OpReturn, 0),
			},
		}
		result, err := v.ApplyPooled(context.Background(), NewClosure(callee, nil, env), []core.Value{core.Int{V: 42}}, env)
		require.NoError(t, err, "a caught failure must leave the pooled VM fit for the next apply")
		assert.True(t, result.Equals(core.Int{V: 42}), "want 42 from the later ApplyPooled, got %v", result)
		assert.Empty(t, v.frames, "want 0 frames after the later apply, got %d", len(v.frames))
		assert.Empty(t, v.freezeStack, "want 0 freeze records after the later apply, got %d", len(v.freezeStack))
		assert.Equal(t, 0, v.stackSize(), "want 0 operands after the later apply, got %d", v.stackSize())
	})
}
