package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestEvalTry_DeadlineEvasionLoopTerminates(t *testing.T) {
	t.Parallel()

	env := newTestEnv()
	env.Set("spin-or-recur", GoFunc{Name: "spin-or-recur", Fn: func(context.Context, Evaluator, []Value, *Env) (Value, error) {
		return recurVal{}, nil
	}})

	forms, err := Read(`(loop [] (try (spin-or-recur) (catch e nil)))`)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err = NewEvaluator().Eval(ctx, forms[0], env)
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Fatalf("expected terminal context error, got %v", err)
	}
	if elapsed > time.Second {
		t.Fatalf("terminal context error observed too late: %v", elapsed)
	}
}

func TestEvalTry_CanceledContextNotCaught(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	env := newTestEnv()
	env.Set("cancel-now", GoFunc{Name: "cancel-now", Fn: func(ctx context.Context, _ Evaluator, _ []Value, _ *Env) (Value, error) {
		cancel()
		return nil, ctx.Err()
	}})

	forms, err := Read(`(try (cancel-now) (catch e e))`)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	_, err = NewEvaluator().Eval(ctx, forms[0], env)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestEvalThrow_ErrorLookingStringStaysCatchable(t *testing.T) {
	t.Parallel()

	got := evalStr(t, newTestEnv(), `(try (throw "context deadline exceeded") (catch e e))`)
	if !got.Equals(String{V: "context deadline exceeded"}) {
		t.Fatalf("throw result = %v, want context deadline exceeded", got)
	}
}
