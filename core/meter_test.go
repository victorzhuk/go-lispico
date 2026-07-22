package core

import (
	"context"
	"errors"
	"testing"
)

type testEvalMeter struct {
	leaseCalls  int
	returnCalls int
	returnedRed int64
	returnedMem int64
	deny        bool
}

type partialGrantEvalMeter struct {
	leaseRed   int64
	leaseAlloc int64
	leaseErr   error

	leaseCalls     int
	returnCalls    int
	returnedRed    int64
	returnedMem    int64
	requestedRed   int64
	requestedAlloc int64
}

func (m *partialGrantEvalMeter) LeaseEval(reductions, allocBytes int64) (int64, int64, error) {
	m.leaseCalls++
	m.requestedRed = reductions
	m.requestedAlloc = allocBytes
	return m.leaseRed, m.leaseAlloc, m.leaseErr
}

func (m *partialGrantEvalMeter) ReturnEval(reductions, allocBytes int64) {
	m.returnCalls++
	m.returnedRed += reductions
	m.returnedMem += allocBytes
}

func (m *partialGrantEvalMeter) ChargeRetained(_, _ int64) error { return nil }

func (m *partialGrantEvalMeter) ReleaseRetained(_, _ int64) {}

func TestEvalState_LeaseEvalReturnsPartialGrantOnErrNil(t *testing.T) {
	t.Parallel()

	m := &partialGrantEvalMeter{
		leaseRed:   4,
		leaseAlloc: 0,
	}
	st := newEvalState()
	st.attachMeter(m)

	err := st.leaseEval(4, 4)
	if err == nil {
		t.Fatal("leaseEval succeeded, want resource limit")
	}
	var lerr *LispicoError
	if !errors.As(err, &lerr) || lerr.Code != CodeResourceLimit {
		t.Fatalf("leaseEval error = %v, want %s", err, CodeResourceLimit)
	}
	if lerr.Message != "evaluation allocation meter exhausted" {
		t.Fatalf("message = %q, want evaluation allocation meter exhausted", lerr.Message)
	}
	if m.returnCalls != 1 {
		t.Fatalf("ReturnEval calls = %d, want 1", m.returnCalls)
	}
	if m.returnedRed != 4 || m.returnedMem != 0 {
		t.Fatalf("ReturnEval returned (%d, %d), want (4, 0)", m.returnedRed, m.returnedMem)
	}
}

func TestEvalState_LeaseEvalClampsOversizedRequests(t *testing.T) {
	t.Parallel()

	m := &partialGrantEvalMeter{
		leaseRed:   maxEvalReductionLease,
		leaseAlloc: maxEvalAllocLease,
	}
	st := newEvalState()
	st.attachMeter(m)

	if err := st.leaseEval(maxEvalReductionLease*8, maxEvalAllocLease*8); err != nil {
		t.Fatalf("leaseEval: %v", err)
	}
	if m.requestedRed != maxEvalReductionLease || m.requestedAlloc != maxEvalAllocLease {
		t.Fatalf("LeaseEval request = (%d, %d), want (%d, %d)", m.requestedRed, m.requestedAlloc, maxEvalReductionLease, maxEvalAllocLease)
	}
}

func (m *testEvalMeter) LeaseEval(reductions, allocBytes int64) (int64, int64, error) {
	m.leaseCalls++
	if m.deny {
		return 0, 0, errors.New("exhausted")
	}
	return reductions, allocBytes, nil
}

func (m *testEvalMeter) ReturnEval(reductions, allocBytes int64) {
	m.returnCalls++
	m.returnedRed += reductions
	m.returnedMem += allocBytes
}

func (m *testEvalMeter) ChargeRetained(_, _ int64) error { return nil }

func (m *testEvalMeter) ReleaseRetained(_, _ int64) {}

func TestMeter_DirectEvaluatorApplyUsesContextMeter(t *testing.T) {
	m := &testEvalMeter{}
	ctx := WithEvalMeter(t.Context(), m)
	eval := NewEvaluator()
	got, err := eval.Apply(ctx, GoFunc{
		Name: "answer",
		Fn: func(_ context.Context, _ Evaluator, _ []Value, _ *Env) (Value, error) {
			return Int{V: 42}, nil
		},
	}, nil, NewEnv(nil))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !got.Equals(Int{V: 42}) {
		t.Fatalf("Apply result = %v, want 42", got)
	}
	if m.leaseCalls == 0 {
		t.Fatal("LeaseEval was not called")
	}
	if m.returnCalls != 1 {
		t.Fatalf("ReturnEval calls = %d, want 1", m.returnCalls)
	}
	if m.returnedRed <= 0 || m.returnedMem <= 0 {
		t.Fatalf("ReturnEval returned (%d, %d), want positive remainder", m.returnedRed, m.returnedMem)
	}
}

func TestMeter_DirectEvaluatorEvalDeniedLeaseIsTerminalResourceLimit(t *testing.T) {
	m := &testEvalMeter{deny: true}
	_, err := NewEvaluator().Eval(WithEvalMeter(t.Context(), m), Int{V: 1}, NewEnv(nil))
	if err == nil {
		t.Fatal("Eval succeeded, want resource limit")
	}
	var lerr *LispicoError
	if !errors.As(err, &lerr) || lerr.Code != CodeResourceLimit {
		t.Fatalf("Eval error = %v, want %s", err, CodeResourceLimit)
	}
	if !IsTerminalEvalError(err) {
		t.Fatalf("Eval error is not terminal: %v", err)
	}
	if m.returnCalls != 0 {
		t.Fatalf("ReturnEval calls = %d, want 0", m.returnCalls)
	}
}

func TestMeter_DirectEvaluatorEvalAbsentMeterPreservesBehavior(t *testing.T) {
	got, err := NewEvaluator().Eval(t.Context(), Vector{Items: []Value{Int{V: 1}, Int{V: 2}}}, NewEnv(nil))
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	want := Vector{Items: []Value{Int{V: 1}, Int{V: 2}}}
	if !got.Equals(want) {
		t.Fatalf("Eval result = %v, want %v", got, want)
	}
}
