package core

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
)

func TestLispicoError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  *LispicoError
		want string
	}{
		{
			name: "with source",
			err:  &LispicoError{Code: "ReadError", Message: "unexpected EOF", Source: "test.lisp", Line: 3, Col: 5},
			want: "ReadError at test.lisp:3:5: unexpected EOF",
		},
		{
			name: "without source",
			err:  &LispicoError{Code: "EvalError", Message: "undefined symbol"},
			want: "EvalError: undefined symbol",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLispicoError_Unwrap(t *testing.T) {
	cause := errors.New("underlying")
	e := &LispicoError{Code: "EvalError", Message: "wrapped", Cause: cause}
	if !errors.Is(e, cause) {
		t.Error("Unwrap should expose Cause via errors.Is")
	}
}

func TestIsTerminalEvalError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "plain error", err: errors.New("boom"), want: false},
		{name: "context canceled", err: context.Canceled, want: true},
		{name: "context deadline exceeded", err: context.DeadlineExceeded, want: true},
		{name: "wrapped canceled", err: fmt.Errorf("vm: %w", context.Canceled), want: true},
		{name: "resource limit", err: &LispicoError{Code: CodeResourceLimit, Message: "limit"}, want: true},
		{name: "throw error", err: &LispicoError{Code: "ThrowError", Message: "context deadline exceeded"}, want: false},
		{name: "eval error", err: &LispicoError{Code: "EvalError", Message: "context deadline exceeded"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsTerminalEvalError(tt.err); got != tt.want {
				t.Fatalf("IsTerminalEvalError(%v) = %t, want %t", tt.err, got, tt.want)
			}
		})
	}
}

func TestNewReadError(t *testing.T) {
	e := NewReadError("unexpected EOF", 5, 10)
	if e.Code != "ReadError" {
		t.Errorf("Code = %q, want ReadError", e.Code)
	}
	if e.Line != 5 || e.Col != 10 {
		t.Errorf("Line/Col = %d/%d, want 5/10", e.Line, e.Col)
	}
}

func TestNewEvalError(t *testing.T) {
	e := NewEvalError("cannot call", Int{V: 42})
	if e.Code != "EvalError" {
		t.Errorf("Code = %q, want EvalError", e.Code)
	}
}

func TestNewTypeError(t *testing.T) {
	e := NewTypeError("symbol", Int{V: 1})
	if e.Code != "TypeError" {
		t.Errorf("Code = %q, want TypeError", e.Code)
	}
}

func TestNewArityError(t *testing.T) {
	e := NewArityError(2, 3)
	if e.Code != "ArityError" {
		t.Errorf("Code = %q, want ArityError", e.Code)
	}
}

func TestNewUndefinedError(t *testing.T) {
	e := NewUndefinedError("foo")
	if e.Code != "UndefinedError" {
		t.Errorf("Code = %q, want UndefinedError", e.Code)
	}
	if e.Message != "undefined: foo" {
		t.Errorf("Message = %q, want 'undefined: foo'", e.Message)
	}
}

func standardTerminalErrors() []struct {
	name string
	err  error
	want bool
} {
	ordinary := errors.New("operation failed")
	typed := NewTypeError("number", String{V: "invalid"})
	resource := NewResourceLimitError("reduction limit")
	return []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil"},
		{name: "ordinary", err: ordinary},
		{name: "typed-ordinary", err: typed},
		{name: "wrapped-ordinary", err: fmt.Errorf("callback: %w", ordinary)},
		{name: "wrapped-typed-ordinary", err: fmt.Errorf("callback: %w", typed)},
		{name: "resource", err: resource, want: true},
		{name: "wrapped-resource", err: fmt.Errorf("callback: %w", resource), want: true},
		{name: "canceled", err: context.Canceled, want: true},
		{name: "wrapped-canceled", err: fmt.Errorf("callback: %w", context.Canceled), want: true},
		{name: "deadline", err: context.DeadlineExceeded, want: true},
		{name: "wrapped-deadline", err: fmt.Errorf("callback: %w", context.DeadlineExceeded), want: true},
		{name: "joined-ordinary", err: errors.Join(ordinary, errors.New("another failure"))},
		{name: "joined-typed-first", err: errors.Join(typed, resource)},
		{name: "joined-resource-first", err: errors.Join(resource, typed), want: true},
		{name: "nested-typed-first", err: &LispicoError{Code: "TypeError", Cause: resource}},
		{name: "nested-resource-first", err: &LispicoError{Code: CodeResourceLimit, Cause: typed}, want: true},
		{name: "nested-canceled", err: &LispicoError{Code: "TypeError", Cause: context.Canceled}, want: true},
		{name: "nested-deadline", err: &LispicoError{Code: "TypeError", Cause: context.DeadlineExceeded}, want: true},
		{name: "multi-wrap-typed-first", err: fmt.Errorf("callbacks: %w; %w", typed, resource)},
		{name: "multi-wrap-resource-first", err: fmt.Errorf("callbacks: %w; %w", resource, typed), want: true},
	}
}

func TestIsTerminalEvalError_StandardErrorsAllocateZero(t *testing.T) {
	for _, tc := range standardTerminalErrors() {
		t.Run(tc.name, func(t *testing.T) {
			var got bool
			allocs := testing.AllocsPerRun(1000, func() {
				got = IsTerminalEvalError(tc.err)
			})
			if got != tc.want {
				t.Errorf("IsTerminalEvalError = %t, want %t", got, tc.want)
			}
			if allocs != 0 {
				t.Errorf("IsTerminalEvalError allocs = %v, want 0", allocs)
			}
		})
	}
}

func TestIsTerminalEvalError_TraversalSemantics(t *testing.T) {
	for _, tc := range standardTerminalErrors() {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsTerminalEvalError(tc.err); got != tc.want {
				t.Errorf("IsTerminalEvalError = %t, want %t", got, tc.want)
			}
		})
	}
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{name: "depth-first-ordinary", err: errors.Join(errors.Join(errors.New("failure"), NewTypeError("number", Nil{})), NewResourceLimitError("limit"))},
		{name: "depth-first-resource", err: errors.Join(errors.Join(errors.New("failure"), NewResourceLimitError("limit")), NewTypeError("number", Nil{})), want: true},
		{name: "later-sentinel", err: errors.Join(NewTypeError("number", Nil{}), context.DeadlineExceeded), want: true},
		{name: "nil-children", err: terminalErrorList{nil, errors.New("failure"), nil, NewResourceLimitError("limit"), nil}, want: true},
		{name: "only-nil-children", err: terminalErrorList{nil, nil}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsTerminalEvalError(tc.err); got != tc.want {
				t.Errorf("IsTerminalEvalError = %t, want %t", got, tc.want)
			}
		})
	}
}

type terminalErrorHook struct {
	is     func(error) bool
	as     func(any) bool
	unwrap func() error
}

func (*terminalErrorHook) Error() string { return "error hook" }

func (e *terminalErrorHook) Is(target error) bool { return e.is != nil && e.is(target) }

func (e *terminalErrorHook) As(target any) bool { return e.as != nil && e.as(target) }

func (e *terminalErrorHook) Unwrap() error {
	if e.unwrap != nil {
		return e.unwrap()
	}
	return nil
}

type terminalErrorList []error

func (terminalErrorList) Error() string     { return "error list" }
func (e terminalErrorList) Unwrap() []error { return e }

type terminalErrorWrapper struct{ unwrap func() error }

func (*terminalErrorWrapper) Error() string   { return "error wrapper" }
func (e *terminalErrorWrapper) Unwrap() error { return e.unwrap() }

func TestIsTerminalEvalError_CustomHooks(t *testing.T) {
	for _, classifier := range []struct {
		name string
		run  func(error) bool
	}{
		{name: "classifier", run: IsTerminalEvalError},
		{name: "stdlib", run: func(err error) bool {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return true
			}
			var typed *LispicoError
			return errors.As(err, &typed) && typed.Code == CodeResourceLimit
		}},
	} {
		t.Run(classifier.name, func(t *testing.T) {
			for _, tc := range []struct {
				name  string
				match bool
				code  string
				child bool
				want  bool
			}{
				{name: "as-resource", match: true, code: CodeResourceLimit, want: true},
				{name: "as-ordinary-masks-resource", match: true, code: "TypeError", child: true},
				{name: "as-false-descendant", code: "TypeError", child: true, want: true},
				{name: "as-false-mutation-without-match", code: CodeResourceLimit},
			} {
				t.Run(tc.name, func(t *testing.T) {
					value := &LispicoError{Code: tc.code}
					calls := 0
					err := &terminalErrorHook{as: func(target any) bool {
						calls++
						*target.(**LispicoError) = value
						return tc.match
					}}
					if tc.child {
						child := NewResourceLimitError("limit")
						err.unwrap = func() error { return child }
					}
					if got := classifier.run(err); got != tc.want {
						t.Errorf("classification = %t, want %t", got, tc.want)
					}
					if calls != 1 {
						t.Errorf("As calls = %d, want 1", calls)
					}
				})
			}

			t.Run("shared-target-across-siblings", func(t *testing.T) {
				ordinary := NewTypeError("number", Nil{})
				resource := NewResourceLimitError("limit")
				var retained **LispicoError
				var trace []string
				first := &terminalErrorHook{as: func(target any) bool {
					trace = append(trace, "first")
					retained = target.(**LispicoError)
					*retained = ordinary
					return false
				}}
				second := &terminalErrorHook{as: func(target any) bool {
					trace = append(trace, "second")
					ptr := target.(**LispicoError)
					if ptr != retained || *ptr != ordinary {
						t.Error("As target lost identity or prior mutation")
					}
					*ptr = resource
					return true
				}}
				err := errors.Join(errors.Join(first, errors.New("failure")), second)
				if !classifier.run(err) {
					t.Error("classification = false, want true")
				}
				if !slices.Equal(trace, []string{"first", "second"}) {
					t.Errorf("As trace = %v, want [first second]", trace)
				}
			})

			t.Run("direct-match-updates-retained-target", func(t *testing.T) {
				ordinary := NewTypeError("number", Nil{})
				resource := NewResourceLimitError("limit")
				var retained **LispicoError
				first := &terminalErrorHook{as: func(target any) bool {
					retained = target.(**LispicoError)
					*retained = ordinary
					return false
				}}
				if !classifier.run(errors.Join(first, resource)) {
					t.Error("classification = false, want true")
				}
				if retained == nil || *retained != resource {
					t.Error("direct typed match did not update retained As target")
				}
			})

			for _, tc := range []struct {
				name  string
				match error
				want  bool
				trace []string
			}{
				{name: "is-canceled", match: context.Canceled, want: true, trace: []string{"is canceled"}},
				{name: "is-deadline", match: context.DeadlineExceeded, want: true, trace: []string{"is canceled", "unwrap", "is deadline"}},
				{name: "is-no-match", trace: []string{"is canceled", "unwrap", "is deadline", "unwrap", "as", "unwrap"}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					var trace []string
					child := errors.New("failure")
					err := &terminalErrorHook{
						is: func(target error) bool {
							if target == context.Canceled {
								trace = append(trace, "is canceled")
							} else if target == context.DeadlineExceeded {
								trace = append(trace, "is deadline")
							} else {
								t.Errorf("unexpected Is target: %v", target)
							}
							return target == tc.match
						},
						as: func(any) bool {
							trace = append(trace, "as")
							return false
						},
						unwrap: func() error {
							trace = append(trace, "unwrap")
							return child
						},
					}
					if got := classifier.run(err); got != tc.want {
						t.Errorf("classification = %t, want %t", got, tc.want)
					}
					if !slices.Equal(trace, tc.trace) {
						t.Errorf("hook trace = %v, want %v", trace, tc.trace)
					}
				})
			}

			t.Run("unwrap-before-custom-as", func(t *testing.T) {
				var trace []string
				resource := NewResourceLimitError("limit")
				child := &terminalErrorHook{as: func(target any) bool {
					trace = append(trace, "as child")
					*target.(**LispicoError) = resource
					return true
				}}
				err := &terminalErrorWrapper{unwrap: func() error {
					trace = append(trace, "unwrap parent")
					return child
				}}
				if !classifier.run(err) {
					t.Error("classification = false, want true")
				}
				want := []string{"unwrap parent", "unwrap parent", "unwrap parent", "as child"}
				if !slices.Equal(trace, want) {
					t.Errorf("hook trace = %v, want %v", trace, want)
				}
			})

			t.Run("as-nil-match-panics", func(t *testing.T) {
				err := &terminalErrorHook{as: func(target any) bool {
					*target.(**LispicoError) = nil
					return true
				}}
				defer func() {
					if recover() == nil {
						t.Error("classification did not panic for nil As match")
					}
				}()
				classifier.run(err)
			})
		})
	}
}
