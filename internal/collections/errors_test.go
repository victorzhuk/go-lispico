package collections

import (
	"testing"

	"github.com/victorzhuk/go-lispico/core"
)

// Each helper returns *core.LispicoError directly; an error-interface return
// would admit a non-LispicoError value at the boundary.
var (
	_ func(string, ...any) *core.LispicoError = typeErrorf
	_ func(string, ...any) *core.LispicoError = domainErrorf
)

func mustLispicoError(t *testing.T, build func() *core.LispicoError) *core.LispicoError {
	t.Helper()
	var (
		got *core.LispicoError
		rec any
	)
	func() {
		defer func() { rec = recover() }()
		got = build()
	}()
	if rec != nil {
		t.Fatalf("helper panicked instead of returning an error: %v", rec)
	}
	if got == nil {
		t.Fatal("helper returned a nil error")
	}
	return got
}

func TestCollectionErrorHelperCodes(t *testing.T) {
	tests := []struct {
		name  string
		build func() *core.LispicoError
		code  string
		msg   string
	}{
		{
			name: "type expected number",
			build: func() *core.LispicoError {
				return typeErrorf("%s: expected number, got %T", "max", core.String{V: "x"})
			},
			code: "TypeError",
			msg:  "max: expected number, got core.String",
		},
		{
			name:  "type unsupported sequence",
			build: func() *core.LispicoError { return typeErrorf("map: unsupported sequence type %T", core.Int{V: 1}) },
			code:  "TypeError",
			msg:   "map: unsupported sequence type core.Int",
		},
		{
			name: "domain incomparable pair",
			build: func() *core.LispicoError {
				return domainErrorf("sort: cannot compare %T with %T", core.Int{V: 1}, core.String{V: "x"})
			},
			code: "EvalError",
			msg:  "sort: cannot compare core.Int with core.String",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mustLispicoError(t, tt.build)
			if got.Code != tt.code {
				t.Errorf("Code = %q, want %q", got.Code, tt.code)
			}
			if got.Message != tt.msg {
				t.Errorf("Message = %q, want %q", got.Message, tt.msg)
			}
			if got.Cause != nil {
				t.Errorf("Cause = %v, want nil", got.Cause)
			}
		})
	}
}
