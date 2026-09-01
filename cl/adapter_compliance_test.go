// Dialect-spec compliance for the CL collection adapters: every adapter-local
// rejection is a typed *core.LispicoError under the code its class mandates,
// and an error raised by a callback the adapter drives reaches the caller with
// its Code unchanged. Codes are asserted through require.ErrorAs, never
// through err.Error(), so typed construction prefixing "<Code>: " onto the
// rendered string cannot rewrite a golden.
//
// The sort grammar rows are pinned by TestCLSort_GrammarBeforeCallbacks and
// are not repeated here; sort appears below only on the callback axis.
package cl_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/runtime"
)

// clComplianceModes runs every row under both execution paths, so a code that
// holds on the tree-walker but not the VM fails loudly.
var clComplianceModes = []struct {
	name string
	opts []runtime.EngineOption
}{
	{name: "tree-walker", opts: []runtime.EngineOption{runtime.WithTreeWalker()}},
	{name: "vm", opts: []runtime.EngineOption{runtime.WithBytecode()}},
}

// errCLCallbackMarker carries a code no CL adapter and no shared kernel ever
// produces, so recovering it proves the callback's own error reached the
// caller instead of an adapter-manufactured substitute.
var errCLCallbackMarker = core.NewConcurrentUseError("cl-adapter-callback")

// bindCLCompliance installs the callbacks the compliance rows drive: an
// identity function, a working integer predicate, and one that always fails
// with errCLCallbackMarker.
func bindCLCompliance(t *testing.T, e runtime.Engine) {
	t.Helper()
	require.NoError(t, e.Bind("clcompliance-id", core.GoFunc{
		Name: "clcompliance-id",
		Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
			if len(args) == 0 {
				return core.Nil{}, nil
			}
			return args[0], nil
		},
	}))
	require.NoError(t, e.Bind("clcompliance-lt", core.GoFunc{
		Name: "clcompliance-lt",
		Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
			if len(args) != 2 {
				return core.Bool{V: false}, nil
			}
			a, aok := args[0].(core.Int)
			b, bok := args[1].(core.Int)
			return core.Bool{V: aok && bok && a.V < b.V}, nil
		},
	}))
	require.NoError(t, e.Bind("clcompliance-fail", core.GoFunc{
		Name: "clcompliance-fail",
		Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
			return nil, errCLCallbackMarker
		},
	}))
}

// TestCLAdapters_LocalValidationCodes pins the delta-spec classification of
// every rejection the nth and mapcar adapters make on their own: a malformed
// argument shape is ArityError, a runtime type rejection is TypeError, and a
// value outside the accepted domain is EvalError.
//
// The stdlib-shape row is the distinguisher. CL nth takes (nth index list)
// while the stdlib builtin of the same name takes (nth coll index), so a
// stdlib registration answering here would return an element instead of
// rejecting the swapped order, and the row would fail.
func TestCLAdapters_LocalValidationCodes(t *testing.T) {
	cases := []struct {
		name string
		src  string
		code string
	}{
		{name: "nth/too-few-arguments", src: `(nth 1)`, code: "ArityError"},
		{name: "nth/too-many-arguments", src: `(nth 1 (list 1 2) 3)`, code: "ArityError"},
		{name: "nth/index-not-an-integer", src: `(nth "x" (list 1 2))`, code: "TypeError"},
		{name: "nth/stdlib-argument-order", src: `(nth (list 10 20) 0)`, code: "TypeError"},
		{name: "nth/negative-index", src: `(nth -1 (list 1 2))`, code: "EvalError"},
		{name: "nth/subject-not-a-sequence", src: `(nth 0 5)`, code: "TypeError"},
		{name: "nth/subject-vector", src: `(nth 0 #(1 2))`, code: "TypeError"},

		{name: "mapcar/no-arguments", src: `(mapcar)`, code: "ArityError"},
		{name: "mapcar/missing-sequence", src: `(mapcar #'clcompliance-id)`, code: "ArityError"},
		{name: "mapcar/sequence-not-a-list", src: `(mapcar #'clcompliance-id 5)`, code: "TypeError"},
		{name: "mapcar/sequence-vector", src: `(mapcar #'clcompliance-id #(1 2))`, code: "TypeError"},
		{name: "mapcar/second-sequence-not-a-list", src: `(mapcar #'clcompliance-id (list 1) 5)`, code: "TypeError"},
	}
	for _, mode := range clComplianceModes {
		t.Run(mode.name, func(t *testing.T) {
			e := newEngine(t, mode.opts...)
			bindCLCompliance(t, e)
			ctx := context.Background()

			got, err := e.Eval(ctx, "cl-adapter-shape", `(nth 1 (list 10 20 30))`)
			require.NoError(t, err, "%s: the CL shape (nth index list) must evaluate", mode.name)
			assert.True(t, (core.Int{V: 20}).Equals(got), "%s: (nth 1 (list 10 20 30)) = %v, want 20", mode.name, got)

			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					_, err := e.Eval(ctx, "cl-adapter-validation", tc.src)
					require.Error(t, err, "%s: %s must be rejected", tc.name, tc.src)
					var le *core.LispicoError
					require.ErrorAs(t, err, &le, "%s: error must be a typed *core.LispicoError, got %v", tc.name, err)
					assert.Equal(t, tc.code, le.Code, "%s: error code", tc.name)
					assert.False(t, core.IsTerminalEvalError(err), "%s: a local validation rejection must stay catchable", tc.name)
				})
			}
		})
	}
}

// TestCLAdapters_CallbackErrorCodePassthrough pins the passthrough contract:
// an error raised by a key function, a predicate, or a mapped callback is
// returned by the adapter with its Code unchanged, never reclassified into
// one of the adapter's own local codes.
func TestCLAdapters_CallbackErrorCodePassthrough(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{name: "mapcar/mapped-callback", src: `(mapcar #'clcompliance-fail (list 1 2))`},
		{name: "sort/predicate", src: `(sort (list 3 1 2) #'clcompliance-fail)`},
		{name: "sort/key", src: `(sort (list 3 1 2) #'clcompliance-lt :key #'clcompliance-fail)`},
	}
	for _, mode := range clComplianceModes {
		t.Run(mode.name, func(t *testing.T) {
			e := newEngine(t, mode.opts...)
			bindCLCompliance(t, e)
			ctx := context.Background()
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					_, err := e.Eval(ctx, "cl-adapter-callback", tc.src)
					require.Error(t, err, "%s: a failing callback must surface an error", tc.name)
					assert.True(t, errors.Is(err, errCLCallbackMarker),
						"%s: the callback's own error must reach the caller, got %v", tc.name, err)
					var le *core.LispicoError
					require.ErrorAs(t, err, &le, "%s: error must be a typed *core.LispicoError, got %v", tc.name, err)
					assert.Equal(t, core.CodeConcurrentUse, le.Code,
						"%s: the adapter must return the callback error with its Code unchanged", tc.name)
					assert.False(t, core.IsTerminalEvalError(err),
						"%s: a non-Terminal callback error must not become Terminal", tc.name)
				})
			}
		})
	}
}
