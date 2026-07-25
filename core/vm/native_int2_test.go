package vm

import (
	"context"
	"math"
	"testing"

	"github.com/victorzhuk/go-lispico/core"
)

// int2TestValues straddles every boundary that changes behavior along the
// fast path: zero, small values on both sides of the preboxed [-128, 1023]
// range, and the int64 extremes where add/sub/mul wrap.
var int2TestValues = []int64{
	0, 1, -1, 2, -2,
	127, 128, -128, -129,
	1023, 1024, -1024,
	math.MaxInt64, math.MinInt64,
	math.MaxInt64 - 1, math.MinInt64 + 1,
}

var int2TestOps = []Opcode{OpAdd, OpSub, OpMul, OpDiv, OpLt, OpGt, OpLe, OpGe, OpEq}

// TestNativeInt2_MatchesGeneralPath cross-checks nativeInt2 against
// execNative (the general N-ary/mixed-type path) over every op and a wide
// int64 range, including overflow and the preboxed-int boundary. A fast path
// that silently drifted from the general path's semantics would show up here
// as a value or handled-flag mismatch.
func TestNativeInt2_MatchesGeneralPath(t *testing.T) {
	eval := core.NewEvaluator()
	env := core.NewEnv(nil)

	for _, op := range int2TestOps {
		for _, av := range int2TestValues {
			for _, bv := range int2TestValues {
				a := core.Int{V: av}
				b := core.Int{V: bv}
				fast, handled := nativeInt2(op, a, b)
				general, err := execNative(eval, op, []core.Value{a, b}, env)

				if op == OpDiv && bv == 0 {
					if handled {
						t.Fatalf("op=%v a=%d b=%d: nativeInt2 handled division by zero, expected fallthrough", op, av, bv)
					}
					if err == nil {
						t.Fatalf("op=%v a=%d b=%d: general path did not error on division by zero", op, av, bv)
					}
					continue
				}

				if !handled {
					t.Fatalf("op=%v a=%d b=%d: nativeInt2 did not handle a covered op", op, av, bv)
				}
				if err != nil {
					t.Fatalf("op=%v a=%d b=%d: general path errored unexpectedly: %v", op, av, bv, err)
				}
				if !fast.Equals(general) {
					t.Fatalf("op=%v a=%d b=%d: fast=%v general=%v diverge", op, av, bv, fast, general)
				}
			}
		}
	}
}

// TestExecNativeFastFused_MixedTypeUnaffected proves a mixed Int/Float pair —
// which nativeInt2 cannot see, since its parameters are core.Int — still
// routes through the general path with the same result the general path
// alone would produce.
func TestExecNativeFastFused_MixedTypeUnaffected(t *testing.T) {
	env := core.NewEnv(nil)
	env.SetCanonical("+", core.GoFunc{Name: "+", Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
		return nil, nil // unused: fused dispatch bypasses the bound GoFunc entirely
	}})
	v := New(env)
	chunk := &Chunk{Name: "test", Code: []Instruction{
		Encode(OpFreezeNative, 0),
		Encode(OpConst, 1), Encode(OpConst, 2),
		Encode(OpAdd, 2),
		Encode(OpReturn, 0),
	}}
	chunk.Constants = []core.Value{core.Symbol{V: "+"}, core.Int{V: 1}, core.Float{V: 2.5}}

	result, err := v.Run(context.Background(), chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want, wantErr := execNative(core.NewEvaluator(), OpAdd, []core.Value{core.Int{V: 1}, core.Float{V: 2.5}}, core.NewEnv(nil))
	if wantErr != nil {
		t.Fatalf("general path errored: %v", wantErr)
	}
	if !result.Equals(want) {
		t.Errorf("expected %v, got %v", want, result)
	}
}

// TestExecNativeFastFused_NaryUnaffected proves argc > 2 still routes through
// the general N-ary path with the same result the general path alone would
// produce.
func TestExecNativeFastFused_NaryUnaffected(t *testing.T) {
	env := core.NewEnv(nil)
	env.SetCanonical("+", core.GoFunc{Name: "+", Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
		return nil, nil
	}})
	v := New(env)
	chunk := &Chunk{Name: "test", Code: []Instruction{
		Encode(OpFreezeNative, 0),
		Encode(OpConst, 1), Encode(OpConst, 2), Encode(OpConst, 3),
		Encode(OpAdd, 3),
		Encode(OpReturn, 0),
	}}
	chunk.Constants = []core.Value{core.Symbol{V: "+"}, core.Int{V: 1}, core.Int{V: 2}, core.Int{V: 3}}

	result, err := v.Run(context.Background(), chunk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want, wantErr := execNative(core.NewEvaluator(), OpAdd, []core.Value{core.Int{V: 1}, core.Int{V: 2}, core.Int{V: 3}}, core.NewEnv(nil))
	if wantErr != nil {
		t.Fatalf("general path errored: %v", wantErr)
	}
	if !result.Equals(want) {
		t.Errorf("expected %v, got %v", want, result)
	}
}

// TestExecNativeFastFused_ErrorTextUnaffected proves a two-arg non-numeric
// operand (which fails the fast path's Int type assertion) falls through to
// the general path and surfaces its exact error text, unchanged.
func TestExecNativeFastFused_ErrorTextUnaffected(t *testing.T) {
	env := core.NewEnv(nil)
	env.SetCanonical("+", core.GoFunc{Name: "+", Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
		return nil, nil
	}})
	v := New(env)
	chunk := &Chunk{Name: "test", Code: []Instruction{
		Encode(OpFreezeNative, 0),
		Encode(OpConst, 1), Encode(OpConst, 2),
		Encode(OpAdd, 2),
		Encode(OpReturn, 0),
	}}
	chunk.Constants = []core.Value{core.Symbol{V: "+"}, core.Int{V: 1}, core.String{V: "x"}}

	_, err := v.Run(context.Background(), chunk)
	if err == nil {
		t.Fatal("expected an error, got none")
	}

	_, wantErr := execNative(core.NewEvaluator(), OpAdd, []core.Value{core.Int{V: 1}, core.String{V: "x"}}, core.NewEnv(nil))
	if wantErr == nil {
		t.Fatal("general path did not error as expected")
	}
	if err.Error() != wantErr.Error() {
		t.Errorf("error text diverged: fast=%q general=%q", err.Error(), wantErr.Error())
	}
}
