package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/cl"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/internal/inventory"
)

// familyDialect maps an arm's dialect selector onto the dialect it is written
// in: "" for Clojure-style canonical names, "cl" for the Common Lisp adapters.
func familyDialect(dia string) core.Dialect {
	if dia == "cl" {
		return cl.Dialect()
	}
	return clojure.Dialect()
}

// callbackCounter records how many times a higher-order builtin dispatched one
// of the shared callbacks below. The count is the observable that separates a
// contract-fixed number of callback invocations from a builtin that grew or
// lost one.
type callbackCounter struct{ n int }

// familyCallbacks are the callbacks every higher-order arm dispatches through.
// They are Go builtins rather than Lisp lambdas so the same callback identity
// serves the source paths and the direct core.Evaluator.Apply path, and so the
// invocation count is observable from the test.
func familyCallbacks(c *callbackCounter) map[string]core.Value {
	binaryInts := func(name string, fn func(a, b int64) core.Value) core.GoFunc {
		return core.GoFunc{
			Name: name,
			Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
				c.n++
				if len(args) < 2 {
					return nil, core.NewArityError(2, len(args))
				}
				a, aok := args[0].(core.Int)
				b, bok := args[1].(core.Int)
				if !aok || !bok {
					return nil, core.NewTypeError("int", args[0])
				}
				return fn(a.V, b.V), nil
			},
		}
	}
	return map[string]core.Value{
		"cb2x": core.GoFunc{
			Name: "cb2x",
			Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
				c.n++
				if len(args) != 1 {
					return nil, core.NewArityError(1, len(args))
				}
				i, ok := args[0].(core.Int)
				if !ok {
					return nil, core.NewTypeError("int", args[0])
				}
				return core.Int{V: i.V * 2}, nil
			},
		},
		"cbkeep": core.GoFunc{
			Name: "cbkeep",
			Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
				c.n++
				if len(args) != 1 {
					return nil, core.NewArityError(1, len(args))
				}
				return core.Bool{V: true}, nil
			},
		},
		"cbcount": core.GoFunc{
			Name: "cbcount",
			Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
				c.n++
				return core.Int{V: int64(len(args))}, nil
			},
		},
		"cbsum":  binaryInts("cbsum", func(a, b int64) core.Value { return core.Int{V: a + b} }),
		"cbless": binaryInts("cbless", func(a, b int64) core.Value { return core.Bool{V: a < b} }),
	}
}

// newFamilyEngine builds a golden engine under the arm's dialect with the
// shared callbacks bound. Publication is eager so a name's binding is settled
// before any arm dispatches through it.
func newFamilyEngine(t *testing.T, dia string, c *callbackCounter, opts ...EngineOption) Engine {
	t.Helper()
	eng := newGoldenEngine(t, familyDialect(dia), true, opts...)
	for name, v := range familyCallbacks(c) {
		require.NoError(t, eng.Bind(name, v))
	}
	return eng
}

// familyReentryDispatch drives src from inside a GoFunc entered by a VM run:
// the engine compiles and runs "(reenter)", the builtin re-enters evaluation
// with eval.Eval on the pre-read form, and the golden's own dispatch therefore
// happens below a VM frame rather than at the top of one.
func familyReentryDispatch(t *testing.T, dia string, c *callbackCounter, src string) (core.Value, error) {
	t.Helper()
	eng := newFamilyEngine(t, dia, c, WithBytecode())
	forms, err := familyDialect(dia).ReadWithMaxDepth(src, 200)
	require.NoError(t, err)
	require.Len(t, forms, 1, "a family golden must be exactly one form")
	form := forms[0]
	require.NoError(t, eng.Bind("reenter", core.GoFunc{
		Name: "reenter",
		Fn: func(ctx context.Context, ev core.Evaluator, _ []core.Value, env *core.Env) (core.Value, error) {
			return ev.Eval(ctx, form, env)
		},
	}))
	return eng.Eval(context.Background(), "families-reentry", "(reenter)")
}

// familyApplyDispatch pulls the builtin out of the engine's env and applies it
// through core.Evaluator.Apply on a bare evaluator and env. Nothing here goes
// through Eval on a source string: the dispatch boundary under test is the
// apply site itself.
func familyApplyDispatch(t *testing.T, dia string, c *callbackCounter, fnName string, args []core.Value) (core.Value, error) {
	t.Helper()
	eng := newFamilyEngine(t, dia, c)
	root := eng.RootEnv()
	var fn core.Value
	var ok bool
	if dia == "cl" {
		fn, ok = root.GetFunc(fnName)
	} else {
		fn, ok = root.Get(fnName)
	}
	require.True(t, ok, "%s must be bound in the engine env before a direct Apply", fnName)
	ev := core.NewEvaluator()
	env := core.NewEnv(nil)
	ctx := core.WithEvalResourceLimits(context.Background(), 1_000_000, 16<<20)
	return ev.Apply(ctx, fn, args, env)
}

// familyValueGolden is one hand-derived family golden. src and args are two
// spellings of the same call, so the four dispatch paths are compared against
// one want rather than against each other.
type familyValueGolden struct {
	family string
	label  string
	dia    string
	src    string
	fn     string
	args   []core.Value
	want   core.Value
	// cbWant is the exact number of callback dispatches the contract fixes for
	// this arm, or -1 where the count is an implementation detail (a sort's
	// comparison count is not part of the language contract).
	cbWant int
}

func famList(vs ...core.Value) core.List     { return core.NewList(vs) }
func famVector(vs ...core.Value) core.Vector { return core.NewVector(vs) }

func famMapAB(t *testing.T) *core.HashMap {
	t.Helper()
	m := core.NewHashMap()
	require.NoError(t, m.Set(core.Keyword{V: "a"}, core.Int{V: 1}))
	return m
}

// familyValueGoldens: every want below is derived from the language contract,
// never captured from a run. One arm per behaviour the family owns, with the
// higher-order and adapter arms carrying the callback count the contract fixes.
func familyValueGoldens(t *testing.T, c *callbackCounter) []familyValueGolden {
	t.Helper()
	cb := familyCallbacks(c)
	return []familyValueGolden{
		{family: "numeric", label: "sum", src: `(+ 1 2)`, fn: "+",
			args: []core.Value{core.Int{V: 1}, core.Int{V: 2}}, want: core.Int{V: 3}, cbWant: -1},
		{family: "numeric", label: "product", src: `(* 2 3)`, fn: "*",
			args: []core.Value{core.Int{V: 2}, core.Int{V: 3}}, want: core.Int{V: 6}, cbWant: -1},
		{family: "numeric", label: "ordering chain", src: `(< 1 2 3)`, fn: "<",
			args: []core.Value{core.Int{V: 1}, core.Int{V: 2}, core.Int{V: 3}}, want: core.Bool{V: true}, cbWant: -1},
		{family: "numeric", label: "ordering chain refused", src: `(< 1 3 2)`, fn: "<",
			args: []core.Value{core.Int{V: 1}, core.Int{V: 3}, core.Int{V: 2}}, want: core.Bool{V: false}, cbWant: -1},

		{family: "types", label: "int predicate", src: `(int? 1)`, fn: "int?",
			args: []core.Value{core.Int{V: 1}}, want: core.Bool{V: true}, cbWant: -1},
		{family: "types", label: "string predicate refused", src: `(string? 1)`, fn: "string?",
			args: []core.Value{core.Int{V: 1}}, want: core.Bool{V: false}, cbWant: -1},
		{family: "types", label: "nil predicate", src: `(nil? nil)`, fn: "nil?",
			args: []core.Value{core.Nil{}}, want: core.Bool{V: true}, cbWant: -1},
		{family: "types", label: "keyword retyped", src: `(keyword->str :a)`, fn: "keyword->str",
			args: []core.Value{core.Keyword{V: "a"}}, want: core.String{V: "a"}, cbWant: -1},

		{family: "collection", label: "count", src: `(count [1 2 3])`, fn: "count",
			args: []core.Value{famVector(core.Int{V: 1}, core.Int{V: 2}, core.Int{V: 3})}, want: core.Int{V: 3}, cbWant: -1},
		{family: "collection", label: "head", src: `(first [10 20])`, fn: "first",
			args: []core.Value{famVector(core.Int{V: 10}, core.Int{V: 20})}, want: core.Int{V: 10}, cbWant: -1},
		{family: "collection", label: "reversed", src: `(reverse '(1 2 3))`, fn: "reverse",
			args: []core.Value{famList(core.Int{V: 1}, core.Int{V: 2}, core.Int{V: 3})},
			want: famList(core.Int{V: 3}, core.Int{V: 2}, core.Int{V: 1}), cbWant: -1},
		{family: "collection", label: "map lookup", src: `(get {:a 1} :a)`, fn: "get",
			args: []core.Value{famMapAB(t), core.Keyword{V: "a"}}, want: core.Int{V: 1}, cbWant: -1},
		{family: "collection", label: "vector conj", src: `(conj [1 2] 3)`, fn: "conj",
			args: []core.Value{famVector(core.Int{V: 1}, core.Int{V: 2}), core.Int{V: 3}},
			want: famVector(core.Int{V: 1}, core.Int{V: 2}, core.Int{V: 3}), cbWant: -1},

		{family: "higher-order", label: "map over three", src: `(map cb2x '(1 2 3))`, fn: "map",
			args: []core.Value{cb["cb2x"], famList(core.Int{V: 1}, core.Int{V: 2}, core.Int{V: 3})},
			want: famList(core.Int{V: 2}, core.Int{V: 4}, core.Int{V: 6}), cbWant: 3},
		{family: "higher-order", label: "filter retains all", src: `(filter cbkeep '(1 2 3))`, fn: "filter",
			args: []core.Value{cb["cbkeep"], famList(core.Int{V: 1}, core.Int{V: 2}, core.Int{V: 3})},
			want: famList(core.Int{V: 1}, core.Int{V: 2}, core.Int{V: 3}), cbWant: 3},
		{family: "higher-order", label: "reduce without seed", src: `(reduce cbsum '(1 2 3))`, fn: "reduce",
			args: []core.Value{cb["cbsum"], famList(core.Int{V: 1}, core.Int{V: 2}, core.Int{V: 3})},
			want: core.Int{V: 6}, cbWant: 2},
		{family: "higher-order", label: "apply spreads the tail", src: `(apply cbsum '(1 2))`, fn: "apply",
			args: []core.Value{cb["cbsum"], famList(core.Int{V: 1}, core.Int{V: 2})},
			want: core.Int{V: 3}, cbWant: 1},

		{family: "string", label: "concatenation", src: `(str "a" "b")`, fn: "str",
			args: []core.Value{core.String{V: "a"}, core.String{V: "b"}}, want: core.String{V: "ab"}, cbWant: -1},
		{family: "string", label: "upper", src: `(string/upper "ab")`, fn: "string/upper",
			args: []core.Value{core.String{V: "ab"}}, want: core.String{V: "AB"}, cbWant: -1},
		{family: "string", label: "join", src: `(string/join "," '("a" "b"))`, fn: "string/join",
			args: []core.Value{core.String{V: ","}, famList(core.String{V: "a"}, core.String{V: "b"})},
			want: core.String{V: "a,b"}, cbWant: -1},

		{family: "cl-adapter", label: "nth", dia: "cl", src: `(nth 1 (list 10 20 30))`, fn: "nth",
			args: []core.Value{core.Int{V: 1}, famList(core.Int{V: 10}, core.Int{V: 20}, core.Int{V: 30})},
			want: core.Int{V: 20}, cbWant: -1},
		{family: "cl-adapter", label: "mapcar", dia: "cl", src: `(mapcar #'cb2x (list 1 2 3))`, fn: "mapcar",
			args: []core.Value{cb["cb2x"], famList(core.Int{V: 1}, core.Int{V: 2}, core.Int{V: 3})},
			want: famList(core.Int{V: 2}, core.Int{V: 4}, core.Int{V: 6}), cbWant: 3},
		{family: "cl-adapter", label: "sort", dia: "cl", src: `(sort (list 3 1 2) #'cbless)`, fn: "sort",
			args: []core.Value{famList(core.Int{V: 3}, core.Int{V: 1}, core.Int{V: 2}), cb["cbless"]},
			want: famList(core.Int{V: 1}, core.Int{V: 2}, core.Int{V: 3}), cbWant: -1},
	}
}

// familyErrorGolden pins one typed-error code per family. The code is the
// contract's own class name, not a message, so a reworded diagnostic is free
// while a reclassified failure is not.
type familyErrorGolden struct {
	family string
	label  string
	dia    string
	src    string
	fn     string
	args   []core.Value
	code   string
}

func familyErrorGoldens(c *callbackCounter) []familyErrorGolden {
	cb := familyCallbacks(c)
	return []familyErrorGolden{
		{family: "numeric", label: "arity", src: `(=)`, fn: "=", args: nil, code: "ArityError"},
		{family: "numeric", label: "type", src: `(< "a" 1)`, fn: "<",
			args: []core.Value{core.String{V: "a"}, core.Int{V: 1}}, code: "TypeError"},

		{family: "types", label: "arity", src: `(type)`, fn: "type", args: nil, code: "ArityError"},
		{family: "types", label: "type", src: `(str->keyword 1)`, fn: "str->keyword",
			args: []core.Value{core.Int{V: 1}}, code: "TypeError"},

		{family: "collection", label: "arity", src: `(count)`, fn: "count", args: nil, code: "ArityError"},
		{family: "collection", label: "type", src: `(count 1)`, fn: "count",
			args: []core.Value{core.Int{V: 1}}, code: "TypeError"},

		{family: "higher-order", label: "arity", src: `(map cb2x)`, fn: "map",
			args: []core.Value{cb["cb2x"]}, code: "ArityError"},
		{family: "higher-order", label: "type", src: `(map cb2x 1)`, fn: "map",
			args: []core.Value{cb["cb2x"], core.Int{V: 1}}, code: "TypeError"},

		{family: "string", label: "arity", src: `(string/join ",")`, fn: "string/join",
			args: []core.Value{core.String{V: ","}}, code: "ArityError"},
		{family: "string", label: "type", src: `(string/join 1 '("a"))`, fn: "string/join",
			args: []core.Value{core.Int{V: 1}, famList(core.String{V: "a"})}, code: "TypeError"},

		{family: "cl-adapter", label: "arity", dia: "cl", src: `(nth 0)`, fn: "nth",
			args: []core.Value{core.Int{V: 0}}, code: "ArityError"},
		{family: "cl-adapter", label: "type", dia: "cl", src: `(nth "a" (list 1))`, fn: "nth",
			args: []core.Value{core.String{V: "a"}, famList(core.Int{V: 1})}, code: "TypeError"},
	}
}

// registeringFamilies is every family that owns at least one dispatchable Lisp
// name, read back out of the inventory rather than restated here. "support"
// registers no name and therefore appears in no NameFamily row.
func registeringFamilies(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for name, family := range inventory.NameFamily {
		require.NotEmpty(t, family, "inventory.NameFamily[%q] names no family", name)
		out[family] = true
	}
	return out
}

// TestStdlibFamilies_GoldensAcrossFourDispatchPaths runs every family's value
// and typed-error goldens through all four runtime dispatch paths — the
// tree-walking Evaluator, the VM, a VM run re-entering evaluation from inside a
// GoFunc, and a direct core.Evaluator.Apply on the builtin pulled from the
// engine's env — and pins the callback counts the contract fixes.
func TestStdlibFamilies_GoldensAcrossFourDispatchPaths(t *testing.T) {
	var probe callbackCounter
	values := familyValueGoldens(t, &probe)
	errs := familyErrorGoldens(&probe)

	armed := map[string]bool{}
	for _, g := range values {
		armed[g.family] = true
	}
	errArmed := map[string]bool{}
	for _, g := range errs {
		errArmed[g.family] = true
	}
	want := registeringFamilies(t)
	for family := range want {
		assert.True(t, armed[family], "family %q registers Lisp names but has no value golden", family)
		assert.True(t, errArmed[family], "family %q registers Lisp names but has no typed-error golden", family)
		assert.True(t, inventory.FamilyMigrated[family], "family %q is armed but not marked migrated", family)
	}
	for family := range armed {
		assert.True(t, want[family], "value golden names family %q, which registers no Lisp name", family)
	}
	require.Len(t, armed, len(want), "one arm per registering family")

	for _, g := range values {
		t.Run(g.family+"/"+g.label, func(t *testing.T) {
			check := func(t *testing.T, path string, c *callbackCounter, got core.Value, err error) {
				t.Helper()
				require.NoError(t, err, "%s: %s must evaluate", path, g.src)
				assert.True(t, g.want.Equals(got), "%s: %s => %v, want %v", path, g.src, got, g.want)
				if g.cbWant >= 0 {
					assert.Equal(t, g.cbWant, c.n,
						"%s: %s must dispatch the callback exactly %d times, got %d", path, g.src, g.cbWant, c.n)
				}
			}

			for _, mode := range goldenEvaluatorModes {
				t.Run(mode.name, func(t *testing.T) {
					var c callbackCounter
					eng := newFamilyEngine(t, g.dia, &c, mode.opts...)
					got, err := eng.Eval(context.Background(), "families", g.src)
					check(t, mode.name, &c, got, err)
				})
			}

			t.Run("re-entry", func(t *testing.T) {
				var c callbackCounter
				got, err := familyReentryDispatch(t, g.dia, &c, g.src)
				check(t, "re-entry", &c, got, err)
			})

			t.Run("direct-apply", func(t *testing.T) {
				var c callbackCounter
				args := familyValueGoldens(t, &c)
				var applyArgs []core.Value
				for _, cand := range args {
					if cand.family == g.family && cand.label == g.label {
						applyArgs = cand.args
						break
					}
				}
				require.NotNil(t, applyArgs, "direct-apply arm must carry its own argument spelling")
				got, err := familyApplyDispatch(t, g.dia, &c, g.fn, applyArgs)
				check(t, "direct-apply", &c, got, err)
			})
		})
	}

	for _, g := range errs {
		t.Run("error/"+g.family+"/"+g.label, func(t *testing.T) {
			for _, mode := range goldenEvaluatorModes {
				t.Run(mode.name, func(t *testing.T) {
					var c callbackCounter
					eng := newFamilyEngine(t, g.dia, &c, mode.opts...)
					_, err := eng.Eval(context.Background(), "families-error", g.src)
					assert.Equal(t, g.code, resourceLimitErrorCode(t, err), "%s: %s", mode.name, g.src)
				})
			}

			t.Run("re-entry", func(t *testing.T) {
				var c callbackCounter
				_, err := familyReentryDispatch(t, g.dia, &c, g.src)
				assert.Equal(t, g.code, resourceLimitErrorCode(t, err), "re-entry: %s", g.src)
			})

			t.Run("direct-apply", func(t *testing.T) {
				var c callbackCounter
				var applyArgs []core.Value
				for _, cand := range familyErrorGoldens(&c) {
					if cand.family == g.family && cand.label == g.label {
						applyArgs = cand.args
						break
					}
				}
				_, err := familyApplyDispatch(t, g.dia, &c, g.fn, applyArgs)
				assert.Equal(t, g.code, resourceLimitErrorCode(t, err), "direct-apply: %s", g.src)
			})
		})
	}
}

// inputArm is one row of the input-identity table: exactly one entry per name
// in inventory.NameFamily, carrying either a call shape that hands the builtin
// a List, Vector or HashMap, or a stated reason no such call exists.
type inputArm struct {
	name string
	dia  string
	src  string
	// reason states why the name admits no collection argument at all. Exactly
	// one of src and reason is set: a name with an awkward call shape is not a
	// name without one.
	reason string
}

const (
	reasonArith    = "arithmetic over numbers; every argument is type-checked as a number, so a collection argument is a TypeError rather than a call shape"
	reasonOrdering = "ordering coerces every argument through ToFloat, so a collection argument is a TypeError rather than a call shape"
	reasonConvert  = "a total conversion between two scalar types; a collection argument is a TypeError rather than a call shape"
	reasonStringIn = "takes string arguments only; a collection argument is a TypeError rather than a call shape"
	reasonRange    = "generates its own elements from integer bounds and accepts no collection argument"
)

// inputArms covers every name in inventory.NameFamily. A name absent here, or
// an entry naming no such builtin, fails the reconciliation below, so a new
// builtin cannot land without a stated decision either way.
var inputArms = []inputArm{
	{name: "=", src: `(= l l)`},
	{name: "+", reason: reasonArith},
	{name: "-", reason: reasonArith},
	{name: "*", reason: reasonArith},
	{name: "/", reason: reasonArith},
	{name: "<", reason: reasonOrdering},
	{name: "<=", reason: reasonOrdering},
	{name: ">", reason: reasonOrdering},
	{name: ">=", reason: reasonOrdering},
	{name: "abs", reason: reasonArith},
	{name: "ceil", reason: reasonArith},
	{name: "floor", reason: reasonArith},
	{name: "max", reason: reasonArith},
	{name: "min", reason: reasonArith},
	{name: "mod", reason: reasonArith},
	{name: "neg?", src: `(neg? m)`},
	{name: "pos?", src: `(pos? v)`},
	{name: "pow", reason: reasonArith},
	{name: "quot", reason: reasonArith},
	{name: "sqrt", reason: reasonArith},
	{name: "zero?", src: `(zero? l)`},

	{name: "bool?", src: `(bool? l)`},
	{name: "float?", src: `(float? v)`},
	{name: "fn?", src: `(fn? l)`},
	{name: "int?", src: `(int? m)`},
	{name: "keyword?", src: `(keyword? l)`},
	{name: "list?", src: `(list? l)`},
	{name: "macro?", src: `(macro? v)`},
	{name: "map?", src: `(map? m)`},
	{name: "nil?", src: `(nil? l)`},
	{name: "string?", src: `(string? v)`},
	{name: "symbol?", src: `(symbol? m)`},
	{name: "type", src: `(type l)`},
	{name: "vector?", src: `(vector? v)`},
	{name: "float->int", reason: reasonConvert},
	{name: "int->float", reason: reasonConvert},
	{name: "keyword->str", reason: reasonConvert},
	{name: "str->keyword", reason: reasonConvert},

	{name: "assoc", src: `(assoc m :c 3)`},
	{name: "concat", src: `(concat l v)`},
	{name: "conj", src: `(conj v 4)`},
	{name: "cons", src: `(cons 0 l)`},
	{name: "contains?", src: `(contains? m :a)`},
	{name: "count", src: `(count l)`},
	{name: "dissoc", src: `(dissoc m :a)`},
	{name: "empty?", src: `(empty? l)`},
	{name: "first", src: `(first l)`},
	{name: "get", src: `(get m :a)`},
	{name: "get-in", src: `(get-in m pa)`},
	{name: "hash-map", src: `(hash-map :k l)`},
	{name: "keys", src: `(keys m)`},
	{name: "last", src: `(last l)`},
	{name: "list", src: `(list l)`},
	{name: "merge", src: `(merge m m)`},
	{name: "nth", src: `(nth l 0)`},
	{name: "range", reason: reasonRange},
	{name: "rest", src: `(rest l)`},
	{name: "reverse", src: `(reverse l)`},
	{name: "sort", src: `(sort l)`},
	{name: "vals", src: `(vals m)`},
	{name: "vector", src: `(vector l)`},

	{name: "apply", src: `(apply cbcount l)`},
	{name: "assert", src: `(assert v)`},
	{name: "filter", src: `(filter cbkeep l)`},
	{name: "map", src: `(map cb2x l)`},
	{name: "reduce", src: `(reduce cbsum l)`},

	{name: "format", src: `(format "%s" l)`},
	{name: "str", src: `(str l)`},
	{name: "string/join", src: `(string/join "," ls)`},
	{name: "string->float", reason: reasonStringIn},
	{name: "string->int", reason: reasonStringIn},
	{name: "string/contains?", reason: reasonStringIn},
	{name: "string/ends-with?", reason: reasonStringIn},
	{name: "string/length", reason: reasonStringIn},
	{name: "string/lines", reason: reasonStringIn},
	{name: "string/lower", reason: reasonStringIn},
	{name: "string/replace", reason: reasonStringIn},
	{name: "string/split", reason: reasonStringIn},
	{name: "string/starts-with?", reason: reasonStringIn},
	{name: "string/trim", reason: reasonStringIn},
	{name: "string/upper", reason: reasonStringIn},

	{name: "cl/nth@1", dia: "cl", src: `(nth 0 l)`},
	{name: "cl/mapcar@1", dia: "cl", src: `(mapcar #'cb2x l)`},
	{name: "cl/sort@1", dia: "cl", src: `(sort l #'cbless)`},
}

// familyInputFixtures are the collections an input arm can name, all built in
// Go so the subject reaching the builtin is the object the assertion holds and
// not one a reader or another builtin produced.
func familyInputFixtures(t *testing.T) map[string]core.Value {
	t.Helper()
	m := core.NewHashMap()
	require.NoError(t, m.Set(core.Keyword{V: "a"}, core.Int{V: 1}))
	require.NoError(t, m.Set(core.Keyword{V: "b"}, core.Int{V: 2}))
	return map[string]core.Value{
		"l":  famList(core.Int{V: 1}, core.Int{V: 2}, core.Int{V: 3}),
		"v":  famVector(core.Int{V: 1}, core.Int{V: 2}, core.Int{V: 3}),
		"m":  m,
		"ls": famList(core.String{V: "a"}, core.String{V: "b"}),
		"pa": famList(core.Keyword{V: "a"}),
	}
}

// deepCopyValue rebuilds every container along the way, so the copy shares no
// backing array or map with the original: a builtin writing through the
// argument it was handed moves one and not the other.
func deepCopyValue(t *testing.T, v core.Value) core.Value {
	t.Helper()
	switch c := v.(type) {
	case core.List:
		items := c.ToSlice()
		out := make([]core.Value, len(items))
		for i, item := range items {
			out[i] = deepCopyValue(t, item)
		}
		return core.NewList(out)
	case core.Vector:
		items := c.ToSlice()
		out := make([]core.Value, len(items))
		for i, item := range items {
			out[i] = deepCopyValue(t, item)
		}
		return core.NewVector(out)
	case *core.HashMap:
		out := core.NewHashMap()
		for _, pair := range c.Pairs() {
			require.NoError(t, out.Set(deepCopyValue(t, pair[0]), deepCopyValue(t, pair[1])))
		}
		return out
	default:
		return v
	}
}

// TestStdlibFamilies_InputsUnchanged reconciles the input table against
// inventory.NameFamily in both directions and, for every name that admits a
// collection argument, proves the argument the builtin was handed still equals
// a copy taken before the call.
func TestStdlibFamilies_InputsUnchanged(t *testing.T) {
	byName := map[string]inputArm{}
	for _, a := range inputArms {
		_, dup := byName[a.name]
		require.False(t, dup, "duplicate input arm for %q", a.name)
		byName[a.name] = a
		hasSrc, hasReason := a.src != "", a.reason != ""
		assert.True(t, hasSrc != hasReason,
			"%q must carry exactly one of a call shape and a no-collection-shape reason", a.name)
	}

	for name := range inventory.NameFamily {
		assert.Contains(t, byName, name,
			"inventory.NameFamily has %q but the input table has no entry for it: give it a call shape or a stated no-collection-shape reason", name)
	}
	for name := range byName {
		assert.Contains(t, inventory.NameFamily, name,
			"input table entry %q names no builtin in inventory.NameFamily", name)
	}

	for _, a := range inputArms {
		if a.src == "" {
			continue
		}
		t.Run(a.name, func(t *testing.T) {
			for _, mode := range goldenEvaluatorModes {
				t.Run(mode.name, func(t *testing.T) {
					var c callbackCounter
					eng := newFamilyEngine(t, a.dia, &c, mode.opts...)

					fixtures := familyInputFixtures(t)
					copies := map[string]core.Value{}
					for name, v := range fixtures {
						copies[name] = deepCopyValue(t, v)
						require.NoError(t, eng.Bind(name, v))
					}

					_, err := eng.Eval(context.Background(), "input-identity", a.src)
					require.NoError(t, err, "%s: %s must be a legal call, not an error path", a.name, a.src)

					for name, before := range copies {
						assert.True(t, fixtures[name].Equals(before),
							"%s: %s mutated the %s argument it was handed (now %v, was %v)", a.name, a.src, name, fixtures[name], before)
						bound, ok := eng.RootEnv().Get(name)
						require.True(t, ok, "%s must still be bound after %s", name, a.src)
						assert.True(t, bound.Equals(before),
							"%s: %s left %s bound to something other than the value it was handed (now %v, was %v)", a.name, a.src, name, bound, before)
					}
				})
			}
		})
	}
}

// tpSubjectLen: enough elements for the comparator to be reached twice and for
// sort's copy walk to leave unpublished work for the mandatory flush to carry,
// and small enough that the calibration runs below stay cheap.
const tpSubjectLen = 64

// tpPred fails the second comparison with a non-terminal TypeError. That error
// is pending when the budget settles, so whatever the caller sees says which of
// the two won.
func tpPred(calls *int) core.GoFunc {
	return core.GoFunc{
		Name: "tp-pred",
		Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
			*calls++
			if *calls == 2 {
				return nil, core.NewTypeError("int", core.Int{V: 1})
			}
			return core.Bool{V: true}, nil
		},
	}
}

// TestStdlibFamilies_TerminalOutranksCallbackError pins the precedence at
// sort's mandatory flush from both sides: with a budget that the flush crosses,
// the terminal ResourceLimitError outranks the callback's pending TypeError;
// with a generous budget the same callback's TypeError surfaces unchanged and
// non-terminal. Without the second half the first would pass for a sort that
// had simply stopped reporting callback errors at all.
//
// Both halves are run twice: once through the engine, where the runtime's own
// settle at the dispatch boundary has the last word, and once through a direct
// core.Evaluator.Apply, which observes the error sort's own settle path
// produced before that boundary is reached.
func TestStdlibFamilies_TerminalOutranksCallbackError(t *testing.T) {
	skipUntilMeteringFields(t)

	t.Run("engine dispatch", func(t *testing.T) {
		build := func(t *testing.T, maxReductions int, calls *int) Engine {
			t.Helper()
			eng := newGoldenEngine(t, cl.Dialect(), true, WithBytecode(),
				WithResourceLimits(meteringLimits(t, maxReductions, 1<<30)))
			bindPrebuiltSubject(t, eng, "tp-subject", tpSubjectLen)
			require.NoError(t, eng.Bind("tp-pred", tpPred(calls)))
			return eng
		}

		const src = `(sort tp-subject #'tp-pred)`

		var generousCalls int
		generous := build(t, 1_000_000, &generousCalls)
		ctx := core.EnsureEvalState(context.Background())
		_, generousErr := generous.Eval(ctx, "terminal-precedence-generous", src)
		used := core.EvalMeterFrom(ctx).Snapshot().Reductions

		require.Equal(t, 2, generousCalls, "the callback must raise its TypeError on the second comparison")
		assert.Equal(t, "TypeError", resourceLimitErrorCode(t, generousErr),
			"under a generous budget the callback's own TypeError must surface unchanged")
		assert.False(t, core.IsTerminalEvalError(generousErr),
			"a callback TypeError is not terminal, got %v", generousErr)

		// The generous run's published total is the ledger after the mandatory
		// flush. One below it is therefore a ceiling the run clears everywhere
		// except that flush, which is exactly the racing point under test.
		ceiling := int(used) - 1
		require.Positive(t, ceiling, "the calibration run must accrue reductions to place a ceiling under")

		var tightCalls int
		tight := build(t, ceiling, &tightCalls)
		_, tightErr := tight.Eval(context.Background(), "terminal-precedence-tight", src)

		require.Equal(t, 2, tightCalls,
			"the callback must have raised its TypeError before the budget crossed, else nothing was outranked (ceiling=%d generous total=%d)", ceiling, used)
		assert.Equal(t, core.CodeResourceLimit, resourceLimitErrorCode(t, tightErr),
			"the terminal budget error must outrank the callback's pending TypeError (ceiling=%d generous total=%d)", ceiling, used)
		assert.True(t, core.IsTerminalEvalError(tightErr),
			"the surfaced error must be terminal, got %v", tightErr)
	})

	t.Run("direct apply", func(t *testing.T) {
		// bindPrebuiltSubject's shape, built inline: the direct path binds
		// nothing, so the descending subject is handed straight to Apply.
		elems := make([]core.Value, tpSubjectLen)
		for i := range elems {
			elems[i] = core.Int{V: int64(tpSubjectLen - i)}
		}
		subject := core.NewList(elems)

		apply := func(t *testing.T, maxReductions int) (int, error, int64) {
			t.Helper()
			eng := newGoldenEngine(t, cl.Dialect(), true)
			fn, ok := eng.RootEnv().GetFunc("sort")
			require.True(t, ok, "sort must be bound in the CL function cell")
			var calls int
			ctx := core.WithEvalResourceLimits(context.Background(), maxReductions, 1<<30)
			_, err := core.NewEvaluator().Apply(ctx, fn,
				[]core.Value{subject, tpPred(&calls)}, core.NewEnv(nil))
			return calls, err, core.EvalMeterFrom(ctx).Snapshot().Reductions
		}

		generousCalls, generousErr, used := apply(t, 1_000_000)
		require.Equal(t, 2, generousCalls, "the callback must raise its TypeError on the second comparison")
		assert.Equal(t, "TypeError", resourceLimitErrorCode(t, generousErr),
			"under a generous budget sort must return the callback's own TypeError unchanged")
		assert.False(t, core.IsTerminalEvalError(generousErr),
			"a callback TypeError is not terminal, got %v", generousErr)

		ceiling := int(used) - 1
		require.Positive(t, ceiling, "the calibration apply must accrue reductions to place a ceiling under")

		tightCalls, tightErr, _ := apply(t, ceiling)
		require.Equal(t, 2, tightCalls,
			"the callback must have raised its TypeError before the budget crossed, else nothing was outranked (ceiling=%d generous total=%d)", ceiling, used)
		assert.Equal(t, core.CodeResourceLimit, resourceLimitErrorCode(t, tightErr),
			"sort's own settle must return the terminal budget error over the callback's pending TypeError (ceiling=%d generous total=%d)", ceiling, used)
		assert.True(t, core.IsTerminalEvalError(tightErr),
			"the error sort returned must be terminal, got %v", tightErr)
	})

	// finishAdapter's own preference needs both of this arm's peculiarities to
	// be visible at all: the engine re-terminalizes at its dispatch boundary, so
	// only a direct Apply reads the error the adapter itself chose; and of
	// sort's finishAdapter returns, the sequence-type rejection is the only one
	// that still carries pending keyword work — the later returns sit behind the
	// mandatory bare flush, and every other path reaches the kernel first.
	t.Run("direct apply non-sequence subject", func(t *testing.T) {
		apply := func(t *testing.T, maxReductions int) (int, error, int64) {
			t.Helper()
			eng := newGoldenEngine(t, cl.Dialect(), true)
			fn, ok := eng.RootEnv().GetFunc("sort")
			require.True(t, ok, "sort must be bound in the CL function cell")
			var calls int
			pred := tpPred(&calls)
			ctx := core.WithEvalResourceLimits(context.Background(), maxReductions, 1<<30)
			_, err := core.NewEvaluator().Apply(ctx, fn,
				[]core.Value{core.Int{V: 42}, pred, core.Keyword{V: "key"}, pred}, core.NewEnv(nil))
			return calls, err, core.EvalMeterFrom(ctx).Snapshot().Reductions
		}

		generousCalls, generousErr, used := apply(t, 1_000_000)
		require.Zero(t, generousCalls, "an Int subject is rejected before the kernel, so no comparison may be dispatched")
		assert.Equal(t, "TypeError", resourceLimitErrorCode(t, generousErr),
			"under a generous budget sort must return the sequence TypeError unchanged")
		assert.False(t, core.IsTerminalEvalError(generousErr),
			"a sequence TypeError is not terminal, got %v", generousErr)

		ceiling := int(used) - 1
		require.Positive(t, ceiling, "the calibration apply must accrue reductions to place a ceiling under")

		tightCalls, tightErr, _ := apply(t, ceiling)
		require.Zero(t, tightCalls,
			"the subject must still be rejected before the kernel (ceiling=%d generous total=%d)", ceiling, used)
		assert.Equal(t, core.CodeResourceLimit, resourceLimitErrorCode(t, tightErr),
			"the adapter's settle must return the terminal budget error over the pending sequence TypeError (ceiling=%d generous total=%d)", ceiling, used)
		assert.True(t, core.IsTerminalEvalError(tightErr),
			"the error the adapter returned must be terminal, got %v", tightErr)
	})
}
