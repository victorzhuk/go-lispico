package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/cl"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
)

// TestRuntimeMacroInBindingFormsParity pins the consumer-visible contract
// that a macro call inside let/let*/loop binding values evaluates to the
// same value under the tree-walker and the bytecode evaluator. A fresh
// engine per case keeps the chunk cache and macroEpoch from masking a
// divergence between the MacroExpandDeep pre-pass and run-time expansion.
func TestRuntimeMacroInBindingFormsParity(t *testing.T) {
	t.Parallel()

	type bindingFormCase struct {
		name     string
		dialect  core.Dialect
		defmacro string
		src      string
	}

	cases := []bindingFormCase{
		{name: "clojure/let", dialect: clojure.Dialect(), defmacro: `(defmacro m [x] ` + "`" + `(* 2 ~x))`, src: `(let [y (m 3)] y)`},
		{name: "clojure/let*", dialect: clojure.Dialect(), defmacro: `(defmacro m [x] ` + "`" + `(* 2 ~x))`, src: `(let* [y (m 3)] y)`},
		{name: "clojure/loop", dialect: clojure.Dialect(), defmacro: `(defmacro m [x] ` + "`" + `(* 2 ~x))`, src: `(loop [y (m 3)] y)`},
		{name: "cl/let", dialect: cl.Dialect(), defmacro: `(defmacro m (x) ` + "`" + `(* 2 ~x))`, src: `(let ((y (m 3))) y)`},
		{name: "cl/let*", dialect: cl.Dialect(), defmacro: `(defmacro m (x) ` + "`" + `(* 2 ~x))`, src: `(let* ((y (m 3))) y)`},
		{name: "cl/loop", dialect: cl.Dialect(), defmacro: `(defmacro m (x) ` + "`" + `(* 2 ~x))`, src: `(loop ((y (m 3))) y)`},
	}

	for _, mode := range goldenEvaluatorModes {
		for _, tc := range cases {
			t.Run(mode.name+"/"+tc.name, func(t *testing.T) {
				t.Parallel()

				eng := newGoldenEngine(t, tc.dialect, true, mode.opts...)
				ctx := context.Background()

				_, err := eng.Eval(ctx, "defmacro", tc.defmacro)
				require.NoErrorf(t, err, "defmacro must register")

				got, err := eng.Eval(ctx, tc.name, tc.src)
				require.NoErrorf(t, err, "%s under %s must evaluate", tc.src, mode.name)
				assert.Truef(t, core.Int{V: 6}.Equals(got),
					"%s under %s = %v, want 6", tc.src, mode.name, got)
			})
		}
	}
}
