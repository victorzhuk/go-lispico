package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/json"
)

// TestValueWalk_CallerPublication pins the caller side of the contextual
// value walks: once a walk refuses terminally mid-evaluation, no caller
// publishes anything — no result value, no formatted output, no compiled
// chunk, no cache entry — and the Terminal identity overrides any pending
// domain error (throw, assertion) and stays invisible to try/catch.
//
// Seeding follows the contract: real Engine evaluation in tree and VM
// modes, Terminal raised only through live evaluation context state
// (engine allocation ceiling 4,096 bytes = the 256-unit walk cap, or a
// cancelled caller context), never through private helpers.
func TestValueWalk_CallerPublication(t *testing.T) {
	// sharedChain is the canonical fixture: a 10-scalar base wrapped in 5
	// self-referencing Cons levels = 352 logical walk visits, above the
	// 256-unit ceiling the 4,096-byte allocation cap implies (4096/16).
	sharedChain := func() core.Value {
		var v core.Value = core.NewList([]core.Value{
			core.Int{V: 0}, core.Int{V: 1}, core.Int{V: 2}, core.Int{V: 3}, core.Int{V: 4},
			core.Int{V: 5}, core.Int{V: 6}, core.Int{V: 7}, core.Int{V: 8}, core.Int{V: 9},
		})
		for range 5 {
			v = core.NewList([]core.Value{v, v})
		}
		return v
	}

	// walkCeilingLimits: reductions ample, allocation bytes low enough that
	// the contextual walk's work cap (maxAllocBytes/16 = 256 units) sits
	// under the 352-visit fixture.
	walkCeilingLimits := meteringLimits(t, 1_000_000, 4<<10)

	newEngine := func(t *testing.T, bytecode bool) Engine {
		t.Helper()
		eng := newMeteringStdlibEngine(t, bytecode, walkCeilingLimits)
		require.NoError(t, eng.Bind("shared", sharedChain()))
		return eng
	}

	// requireTerminal asserts the refusal is a Terminal ResourceLimit and
	// that no result was published alongside it.
	requireTerminal := func(t *testing.T, got core.Value, err error) {
		t.Helper()
		require.Error(t, err, "a Terminal walk refusal must surface an error")
		assert.True(t, isResourceLimit(t, err), "the walk refusal must keep its %s code, got %v", core.CodeResourceLimit, err)
		assert.True(t, core.IsTerminalEvalError(err), "the refusal must stay Terminal, got %v", err)
		assert.Nil(t, got, "no result may be published after a Terminal walk refusal, got %v", got)
	}

	for _, bytecode := range []bool{false, true} {
		mode := evalModeName(bytecode)

		t.Run(mode+"/throw", func(t *testing.T) {
			eng := newEngine(t, bytecode)
			// The catch body renders the shared chain through the
			// contextual walk; its Terminal refusal must override the
			// pending throw and escape try/catch unpublished.
			got, err := eng.Eval(context.Background(), "throw-publication",
				`(try (throw "boom") (catch e (str shared)))`)
			requireTerminal(t, got, err)
			var le *core.LispicoError
			if errors.As(err, &le) {
				assert.NotEqual(t, "ThrowError", le.Code, "the Terminal refusal must override the pending throw, got %v", err)
			}
		})

		t.Run(mode+"/assert", func(t *testing.T) {
			eng := newEngine(t, bytecode)
			// assert renders its non-string message through
			// ValueStringContext under the caller's context. The payload is
			// a vector of 300 scalars — one walk unit for the vector plus
			// one per element, 301 against the 256-unit ceiling — so the
			// render refuses before any message is emitted.
			bigVec := make([]core.Value, 300)
			for i := range bigVec {
				bigVec[i] = core.Int{V: int64(i)}
			}
			require.NoError(t, eng.Bind("bigvec", core.NewVector(bigVec)))
			got, err := eng.Eval(context.Background(), "assert-publication", `(assert false bigvec)`)
			requireTerminal(t, got, err)
			assert.NotContains(t, err.Error(), "assertion failed",
				"the Terminal refusal must override the pending assertion domain error, got %v", err)

			guarded, gerr := eng.Eval(context.Background(), "assert-publication-guarded",
				`(try (assert false bigvec) (catch e :caught))`)
			require.Error(t, gerr, "try/catch must not swallow the Terminal refusal")
			assert.True(t, core.IsTerminalEvalError(gerr), "try/catch must not catch the Terminal refusal, got %v", gerr)
			assert.Nil(t, guarded, "no caught value may be published after a Terminal refusal, got %v", guarded)
		})

		t.Run(mode+"/stdlib-str", func(t *testing.T) {
			eng := newEngine(t, bytecode)
			got, err := eng.Eval(context.Background(), "str-publication", `(str shared)`)
			requireTerminal(t, got, err)
		})

		t.Run(mode+"/stdlib-cancellation", func(t *testing.T) {
			eng := newEngine(t, bytecode)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			require.NoError(t, eng.Bind("cancel-now", core.GoFunc{
				Name: "cancel-now",
				Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
					cancel()
					return core.Nil{}, nil
				},
			}))
			got, err := eng.Eval(ctx, "str-cancellation", `(do (cancel-now) (str shared))`)
			require.Error(t, err, "a cancelled context must refuse the live walk")
			assert.True(t, errors.Is(err, context.Canceled), "cancellation must surface as context.Canceled, got %v", err)
			assert.True(t, core.IsTerminalEvalError(err), "cancellation must stay Terminal, got %v", err)
			assert.Nil(t, got, "no result may be published after cancellation, got %v", got)
		})

		t.Run(mode+"/json-decode", func(t *testing.T) {
			eng := newEngine(t, bytecode)
			require.NoError(t, eng.Use(json.New()))
			// 301 nested JSON arrays: within the structural depth limit but
			// above the 256-unit walk ceiling, so decode's contextual
			// construction-depth/deep-bytes walk refuses mid-walk and the
			// decoded result is never published nor charged.
			payload := strings.Repeat("[", 300) + "1" + strings.Repeat("]", 300)
			got, err := eng.Eval(context.Background(), "json-publication", `(json/decode "`+payload+`")`)
			requireTerminal(t, got, err)
		})
	}

	// The compiler arm is VM-only, matching the compile-time walk sites:
	// compilation performs the contextual node-count/deep-bytes walks with
	// the Engine's context, and a Terminal refusal there must leave no
	// published chunk — the compilation cache entry count is unchanged.
	t.Run("bytecode/compiler-chunk-cache", func(t *testing.T) {
		eng := newMeteringStdlibEngine(t, true, walkCeilingLimits)

		warm, err := eng.Eval(context.Background(), "warm", "(+ 1 2)")
		require.NoError(t, err)
		assert.True(t, core.Int{V: 3}.Equals(warm))
		before := eng.Stats().Cache.Entries

		// 512-element vector literal: ~2049 walk nodes, above the 256-unit
		// ceiling, refused during the compile-time walk.
		got, err := eng.Eval(context.Background(), "compile-publication", vectorLiteral("(+ 1 2)", 512))
		requireTerminal(t, got, err)

		after := eng.Stats().Cache.Entries
		assert.Equal(t, before, after,
			"no cache entry may be published after a Terminal compile refusal (before=%d after=%d)", before, after)
	})

	// REPL publication: a Terminal refusal inside the REPL loop must print
	// an error line and never render the refused value.
	t.Run("repl", func(t *testing.T) {
		for _, bytecode := range []bool{false, true} {
			t.Run(evalModeName(bytecode), func(t *testing.T) {
				eng := newEngine(t, bytecode)
				var out strings.Builder
				err := eng.REPL(strings.NewReader("(str shared)\n(exit)\n"), &out)
				require.NoError(t, err, "REPL must survive a Terminal refusal and exit cleanly")
				output := out.String()
				assert.Contains(t, output, "Error:", "the REPL must report the refusal, got %q", output)
				assert.NotContains(t, output, "0 1 2 3",
					"the REPL must not render the refused value after Terminal, got %q", output)
			})
		}
	})
}
