package core

import (
	"context"
	"errors"
	"testing"
)

type depthLimitEvaluator struct{ limit int }

func (depthLimitEvaluator) Eval(context.Context, Value, *Env) (Value, error) { return Nil{}, nil }

func (depthLimitEvaluator) Apply(context.Context, Value, []Value, *Env) (Value, error) {
	return Nil{}, nil
}

func (e depthLimitEvaluator) ConstructionDepthLimit() int { return e.limit }

type limitlessEvaluator struct{}

func (limitlessEvaluator) Eval(context.Context, Value, *Env) (Value, error) { return Nil{}, nil }

func (limitlessEvaluator) Apply(context.Context, Value, []Value, *Env) (Value, error) {
	return Nil{}, nil
}

func requireDepthLimitError(t *testing.T, err error, want string) {
	t.Helper()
	var lerr *LispicoError
	if !errors.As(err, &lerr) {
		t.Fatalf("err = %v, want *LispicoError %q", err, want)
	}
	if lerr.Code != CodeResourceLimit {
		t.Fatalf("Code = %q, want %q", lerr.Code, CodeResourceLimit)
	}
	if lerr.Message != want {
		t.Fatalf("Message = %q, want %q", lerr.Message, want)
	}
}

func TestCheckConstructionDepthWith_UsesEvaluator(t *testing.T) {
	tests := []struct {
		name string
		v    Value
		eval Evaluator
		want string
	}{
		{"limit from evaluator", nestedList(3), depthLimitEvaluator{limit: 2}, "structural depth limit 2 exceeded"},
		{"within evaluator limit", nestedList(2), depthLimitEvaluator{limit: 2}, ""},
		{"nil eval falls back to default", nestedList(3), nil, ""},
		{"nil eval enforces default", nestedList(DefaultMaxStructuralDepth + 1), nil, "structural depth limit 1024 exceeded"},
		{"eval without limit falls back to default", nestedList(3), limitlessEvaluator{}, ""},
		{"zero limit falls back to default", nestedList(3), depthLimitEvaluator{limit: 0}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckConstructionDepthWith(tt.v, tt.eval)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("CheckConstructionDepthWith() = %v, want nil", err)
				}
				return
			}
			requireDepthLimitError(t, err, tt.want)
		})
	}
}

func TestCheckNestedElementDepthWith_UsesEvaluator(t *testing.T) {
	tests := []struct {
		name string
		v    Value
		eval Evaluator
		want string
	}{
		{"element depth counts container level", nestedList(2), depthLimitEvaluator{limit: 2}, "structural depth limit 2 exceeded"},
		{"within evaluator limit", nestedList(1), depthLimitEvaluator{limit: 2}, ""},
		{"nil eval falls back to default", nestedList(2), nil, ""},
		{"nil eval enforces default", nestedList(DefaultMaxStructuralDepth), nil, "structural depth limit 1024 exceeded"},
		{"eval without limit falls back to default", nestedList(2), limitlessEvaluator{}, ""},
		{"zero limit falls back to default", nestedList(2), depthLimitEvaluator{limit: 0}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckNestedElementDepthWith(tt.v, tt.eval)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("CheckNestedElementDepthWith() = %v, want nil", err)
				}
				return
			}
			requireDepthLimitError(t, err, tt.want)
		})
	}
}

func TestCheckConstructionDepth_EnvVariantDelegates(t *testing.T) {
	env := NewEnv(nil)
	env.SetEvaluator(depthLimitEvaluator{limit: 2})

	requireDepthLimitError(t, CheckConstructionDepth(nestedList(3), env), "structural depth limit 2 exceeded")
	if err := CheckConstructionDepth(nestedList(2), env); err != nil {
		t.Fatalf("CheckConstructionDepth() = %v, want nil", err)
	}
	requireDepthLimitError(t, CheckNestedElementDepth(nestedList(2), env), "structural depth limit 2 exceeded")
	if err := CheckNestedElementDepth(nestedList(1), env); err != nil {
		t.Fatalf("CheckNestedElementDepth() = %v, want nil", err)
	}

	if err := CheckConstructionDepth(nestedList(3), nil); err != nil {
		t.Fatalf("CheckConstructionDepth(nil env) = %v, want nil", err)
	}
	requireDepthLimitError(t, CheckConstructionDepth(nestedList(DefaultMaxStructuralDepth+1), nil), "structural depth limit 1024 exceeded")
	if err := CheckNestedElementDepth(nestedList(2), nil); err != nil {
		t.Fatalf("CheckNestedElementDepth(nil env) = %v, want nil", err)
	}
}
