// Terminal-error compliance for the CL collection adapters at the engine
// boundary: a Terminal error raised by a callback an adapter drives stays
// Terminal, keeps its Code, and remains invisible to Lisp try/catch, while the
// same seeding path with a non-Terminal code is catchable.
package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/cl"
	"github.com/victorzhuk/go-lispico/core"
)

// errCLTerminalCallback is Terminal under core.IsTerminalEvalError. Raised
// from a CL adapter callback, it must reach the caller unchanged.
var errCLTerminalCallback = core.NewResourceLimitError("cl-adapter-terminal-callback")

// errCLCatchableCallback is the non-Terminal control on the same seeding
// path, so "try/catch did not catch it" reads as Terminal rather than as
// try/catch never seeing an adapter callback error at all.
var errCLCatchableCallback = core.NewTypeError("integer", core.String{V: "x"})

// TestCLAdapters_TerminalCallbackStaysUncatchable pins the Terminal
// passthrough contract at every CL adapter callback site.
func TestCLAdapters_TerminalCallbackStaysUncatchable(t *testing.T) {
	sites := []struct {
		name string
		src  string
	}{
		{name: "sort/predicate", src: `(sort (list 3 1 2) #'clterm-cb)`},
		{name: "sort/key", src: `(sort (list 3 1 2) #'clterm-lt :key #'clterm-cb)`},
		{name: "mapcar/mapped-callback", src: `(mapcar #'clterm-cb (list 1 2))`},
	}
	for _, mode := range goldenEvaluatorModes {
		t.Run(mode.name, func(t *testing.T) {
			eng := newGoldenEngine(t, cl.Dialect(), true, mode.opts...)
			failure := error(errCLTerminalCallback)
			require.NoError(t, eng.Bind("clterm-cb", core.GoFunc{
				Name: "clterm-cb",
				Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
					return nil, failure
				},
			}))
			require.NoError(t, eng.Bind("clterm-lt", core.GoFunc{
				Name: "clterm-lt",
				Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
					if len(args) != 2 {
						return core.Bool{V: false}, nil
					}
					a, aok := args[0].(core.Int)
					b, bok := args[1].(core.Int)
					return core.Bool{V: aok && bok && a.V < b.V}, nil
				},
			}))

			ctx := context.Background()
			for _, site := range sites {
				t.Run(site.name, func(t *testing.T) {
					failure = errCLTerminalCallback
					_, err := eng.Eval(ctx, "cl-terminal-callback", site.src)
					require.Error(t, err, "%s: a Terminal callback must surface an error", site.name)
					assert.True(t, errors.Is(err, errCLTerminalCallback),
						"%s: the callback's own Terminal error must reach the caller, got %v", site.name, err)
					var le *core.LispicoError
					require.ErrorAs(t, err, &le, "%s: error must be a typed *core.LispicoError, got %v", site.name, err)
					assert.Equal(t, core.CodeResourceLimit, le.Code,
						"%s: the adapter must return the Terminal error with its Code unchanged", site.name)
					assert.True(t, core.IsTerminalEvalError(err), "%s: the error must stay Terminal", site.name)

					guarded := `(try ` + site.src + ` (catch e :caught))`
					_, err = eng.Eval(ctx, "cl-terminal-callback", guarded)
					require.Error(t, err, "%s: try/catch must not catch a Terminal adapter error", site.name)
					assert.True(t, core.IsTerminalEvalError(err),
						"%s: the error escaping try/catch must still be Terminal, got %v", site.name, err)
					assert.True(t, errors.Is(err, errCLTerminalCallback),
						"%s: the escaping error must still be the callback's own, got %v", site.name, err)

					failure = errCLCatchableCallback
					got, err := eng.Eval(ctx, "cl-terminal-callback", guarded)
					require.NoError(t, err, "%s: a non-Terminal callback error must be catchable", site.name)
					assert.True(t, (core.Keyword{V: "caught"}).Equals(got),
						"%s: try/catch must observe the non-Terminal error, got %v", site.name, got)
				})
			}
		})
	}
}

// TestCLAdapters_CancelDuringCallbackIsTerminal pins the cancellation path
// through the shared sort kernel: a context cancelled from inside the
// predicate surfaces as Terminal context.Canceled and is not observable by
// try/catch.
func TestCLAdapters_CancelDuringCallbackIsTerminal(t *testing.T) {
	for _, mode := range goldenEvaluatorModes {
		t.Run(mode.name, func(t *testing.T) {
			eng := newGoldenEngine(t, cl.Dialect(), true, mode.opts...)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			require.NoError(t, eng.Bind("clcancel-lt", core.GoFunc{
				Name: "clcancel-lt",
				Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
					cancel()
					return core.Bool{V: false}, nil
				},
			}))

			_, err := eng.Eval(ctx, "cl-cancel", `(try (sort (list 3 1 2) #'clcancel-lt) (catch e :caught))`)
			require.Error(t, err, "a cancellation raised inside a sort predicate must not be catchable")
			assert.True(t, errors.Is(err, context.Canceled),
				"cancellation must surface as context.Canceled, got %v", err)
			assert.True(t, core.IsTerminalEvalError(err), "a cancellation must be Terminal, got %v", err)
		})
	}
}
