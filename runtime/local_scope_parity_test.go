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

// TestRuntimeLocalScopeParity pins the vm-local-stack-scope local-scope
// contract at the public Engine seam: cold and repeated Eval (the second Eval
// hits the chunk cache), closure application, Clojure vector bindings and CL
// list-pair bindings must produce the spec-pinned values under both evaluator
// modes, with sequential let/let* as the control.
func TestRuntimeLocalScopeParity(t *testing.T) {
	t.Parallel()

	type parityCase struct {
		name     string
		dialect  core.Dialect
		src      string
		want     core.Value
		callFn   string
		callArg  core.Value
		callWant core.Value
	}

	cases := []parityCase{
		// Spec deltas: "let init sees the enclosing binding, not its sibling",
		// "let init sees the earlier sibling", "let* init sees the earlier
		// sibling" (sequential kernel let/let* controls).
		{
			name:    "clojure/let init sees enclosing binding not sibling",
			dialect: clojure.Dialect(),
			src:     `(def a 10) (let [b a a 1] b)`,
			want:    core.Int{V: 10},
		},
		{
			name:    "clojure/let init sees earlier sibling",
			dialect: clojure.Dialect(),
			src:     `(def a 10) (let [a 1 b a] b)`,
			want:    core.Int{V: 1},
		},
		{
			name:    "clojure/let-star init sees earlier sibling",
			dialect: clojure.Dialect(),
			src:     `(def a 10) (let* [a 1 b a] b)`,
			want:    core.Int{V: 1},
		},
		// Spec: "Binding expression preserves an earlier vector element".
		{
			name:    "clojure/binding expression preserves earlier vector element",
			dialect: clojure.Dialect(),
			src:     `[10 (let [x 2] x)]`,
			want:    core.NewVector([]core.Value{core.Int{V: 10}, core.Int{V: 2}}),
		},
		// Spec: "Binding expression preserves an earlier native argument".
		{
			name:    "clojure/binding expression preserves earlier native argument",
			dialect: clojure.Dialect(),
			src:     `(+ 10 (let [x 2] x))`,
			want:    core.Int{V: 12},
		},
		// Spec: "Binding expression preserves an ordinary call target".
		{
			name:    "clojure/binding expression preserves ordinary call target",
			dialect: clojure.Dialect(),
			src:     `((fn [a b] a) 10 (let [x 2] x))`,
			want:    core.Int{V: 10},
		},
		// Spec: "Escaping closures retain their own binding": the closure is
		// created inside a binding expression, escapes the lexical scope, and
		// must observe its own binding's mutation without seeing operands.
		{
			name:    "clojure/escaping closure retains own binding and mutation",
			dialect: clojure.Dialect(),
			src:     `(do (def f (nth [0 (let [x 2] (fn [] (set! x (+ x 1)) x))] 1)) (f) (f))`,
			want:    core.Int{V: 4},
		},
		// Task 1.2: closure application — a function whose body binds locals
		// applied through the Engine.Call boundary, cold and repeated.
		{
			name:     "clojure/closure application binds locals per call",
			dialect:  clojure.Dialect(),
			src:      `(defn twice-plus [n] (let [x (* n 2)] (let [y (+ x 1)] y)))`,
			callFn:   "twice-plus",
			callArg:  core.Int{V: 5},
			callWant: core.Int{V: 11},
		},
		// Task 2.2 control: mutation stores preserve the set! expression's
		// value while initializer stores consume their operand.
		{
			name:    "clojure/set bang preserves expression value",
			dialect: clojure.Dialect(),
			src:     `(let [x 1] (+ 10 (set! x 5)))`,
			want:    core.Int{V: 15},
		},
		// Core-engine spec: "Common Lisp list-pair bindings under the default
		// dialect" and "Both evaluators agree on the list form".
		{
			name:    "cl/list-pair let binds and sums",
			dialect: cl.Dialect(),
			src:     `(let ((a 1) (b 2)) (+ a b))`,
			want:    core.Int{V: 3},
		},
		{
			name:    "cl/list-pair let init sees earlier sibling",
			dialect: cl.Dialect(),
			src:     `(def a 10) (let ((a 1) (b a)) b)`,
			want:    core.Int{V: 1},
		},
		{
			name:    "cl/list-pair let-star init sees earlier sibling",
			dialect: cl.Dialect(),
			src:     `(def a 10) (let* ((a 1) (b a)) b)`,
			want:    core.Int{V: 1},
		},
		{
			name:    "cl/list-pair binding preserves earlier native argument",
			dialect: cl.Dialect(),
			src:     `(+ 10 (let ((x 2)) x))`,
			want:    core.Int{V: 12},
		},
	}

	for _, mode := range goldenEvaluatorModes {
		for _, tc := range cases {
			t.Run(mode.name+"/"+tc.name, func(t *testing.T) {
				t.Parallel()

				eng := newGoldenEngine(t, tc.dialect, true, mode.opts...)
				ctx := context.Background()

				// Cold Eval: first evaluation compiles and runs the form.
				cold, err := eng.Eval(ctx, "cold", tc.src)
				require.NoErrorf(t, err, "TestRuntimeLocalScopeParity/%s/%s: cold Eval must succeed", mode.name, tc.name)
				if tc.want != nil {
					require.Truef(t, cold.Equals(tc.want),
						"TestRuntimeLocalScopeParity/%s/%s: cold Eval want %v, got %v (%T)", mode.name, tc.name, tc.want, cold, cold)
				}

				// Repeated Eval: the same source re-evaluated through the
				// cached chunk must stay pinned.
				warm, err := eng.Eval(ctx, "warm", tc.src)
				require.NoErrorf(t, err, "TestRuntimeLocalScopeParity/%s/%s: repeated Eval must succeed", mode.name, tc.name)
				if tc.want != nil {
					assert.Truef(t, warm.Equals(cold),
						"TestRuntimeLocalScopeParity/%s/%s: repeated Eval want %v, got %v (%T)", mode.name, tc.name, cold, warm, warm)
				}
				if tc.callFn != "" {
					got, err := eng.Call(ctx, tc.callFn, tc.callArg)
					require.NoErrorf(t, err, "TestRuntimeLocalScopeParity/%s/%s: Call must succeed", mode.name, tc.name)
					assert.Truef(t, got.Equals(tc.callWant),
						"TestRuntimeLocalScopeParity/%s/%s: closure application want %v, got %v (%T)", mode.name, tc.name, tc.callWant, got, got)
				}
			})
		}
	}
}
