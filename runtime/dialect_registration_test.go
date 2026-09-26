package runtime

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/cl"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/json"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
)

// newModeEngine builds an engine under d and loads plugins with the eager
// flag held for the whole construction. Eager engines of a dialect must be
// built before any lazy engine sharing its fingerprint: a published lazy
// template layer is process-global and attaches to later engines too.
func newModeEngine(t *testing.T, d core.Dialect, eager bool, opt EngineOption, plugins ...core.Plugin) Engine {
	t.Helper()
	restore := SetStdlibLazyDisabledForTesting(eager)
	defer restore()

	eng, err := New(nil, opt, WithDialect(d))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	for _, p := range plugins {
		require.NoError(t, eng.Use(p))
	}
	return eng
}

func uniqueForms(t *testing.T, forms map[string]string) map[string]string {
	t.Helper()
	out := map[string]string{t.Name() + "-do": "do"}
	for k, v := range forms {
		out[k] = v
	}
	return out
}

var registrationModes = []struct {
	name  string
	eager bool
}{
	{name: "eager", eager: true},
	{name: "lazy", eager: false},
}

var registrationEvaluators = []struct {
	name string
	opt  EngineOption
}{
	{name: "vm", opt: WithBytecode()},
	{name: "tree-walker", opt: WithTreeWalker()},
}

func TestLisp2WithoutVocabBridgesPluginBuiltins(t *testing.T) {
	d := mustDialect(t, spec{Lisp2: true, Forms: uniqueForms(t, nil)})
	ctx := context.Background()

	for _, mode := range registrationModes {
		for _, ev := range registrationEvaluators {
			t.Run(mode.name+"/"+ev.name, func(t *testing.T) {
				eng := newModeEngine(t, d, mode.eager, ev.opt, stdlib.New(), json.New())

				if mode.eager {
					funcs := eng.RootEnv().LocalFuncNames()
					assert.Contains(t, funcs, "+", "Lisp-2 without vocabulary must bridge stdlib + into the function cell")
					assert.Contains(t, funcs, "json/encode", "Lisp-2 without vocabulary must bridge json/encode into the function cell")
				}

				got, err := eng.Eval(ctx, "json", "(json/encode 1)")
				if assert.NoError(t, err, "(json/encode 1) must resolve in head position under Lisp-2 without vocabulary") {
					assert.Equal(t, core.String{V: "1"}, got)
				}

				got, err = eng.Eval(ctx, "plus", "(+ 1 2)")
				if assert.NoError(t, err, "(+ 1 2) must resolve in head position under Lisp-2 without vocabulary") {
					assert.True(t, core.Int{V: 3}.Equals(got), "(+ 1 2) = %v, want 3", got)
				}
			})
		}
	}
}

func TestHostBindSurvivesPluginUseUnderEmptyBase(t *testing.T) {
	d := mustDialect(t, spec{
		Base:  core.BaseEmpty,
		Forms: uniqueForms(t, map[string]string{"if": "if"}),
		Vocab: map[string]string{"+": "+"},
	})
	eng, err := New(nil, WithDialect(d))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	hostFn := core.GoFunc{
		Name: "host/f",
		Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
			return core.Int{V: 7}, nil
		},
	}
	require.NoError(t, eng.Bind("host/f", hostFn))

	ctx := context.Background()
	callHost := func(stage string) {
		t.Helper()
		got, err := eng.Eval(ctx, stage, "(host/f)")
		if assert.NoError(t, err, "%s: host Bind of host/f must survive under an empty base", stage) {
			assert.True(t, core.Int{V: 7}.Equals(got), "%s: (host/f) = %v, want 7", stage, got)
		}
	}

	require.NoError(t, eng.Use(json.New()))
	callHost("after Use(json)")

	require.NoError(t, eng.ReloadPlugin(json.New()))
	callHost("after ReloadPlugin(json)")
}

func TestHostBindSurvivesVocabularyAlias(t *testing.T) {
	ctx := context.Background()
	for _, mode := range registrationModes {
		t.Run(mode.name, func(t *testing.T) {
			restore := SetStdlibLazyDisabledForTesting(mode.eager)
			eng, err := New(nil, WithDialect(cl.Dialect()))
			if err != nil {
				restore()
				require.NoError(t, err)
			}
			t.Cleanup(func() { _ = eng.Close() })

			hostCar := core.GoFunc{
				Name: "host-car",
				Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
					return core.Int{V: 42}, nil
				},
			}
			bindErr := eng.Bind("car", hostCar)
			useErr := eng.Use(stdlib.New())
			restore()
			require.NoError(t, bindErr)
			require.NoError(t, useErr)

			v, ok, _ := eng.RootEnv().GetMaterializedCanonical("car")
			require.True(t, ok, "car value cell must stay bound")
			fn, isGoFunc := v.(core.GoFunc)
			if assert.True(t, isGoFunc, "car value cell = %T, want the host core.GoFunc", v) {
				assert.Equal(t, "host-car", fn.Name, "stdlib Use must not overwrite the host car binding in the value cell")
			}

			got, err := eng.Eval(ctx, "car", "(car '(1))")
			if assert.NoError(t, err) {
				assert.True(t, core.Int{V: 42}.Equals(got), "(car '(1)) = %v, want the host result 42", got)
			}
		})
	}
}

func describeBinding(v core.Value, canonical bool) string {
	var kind string
	switch x := v.(type) {
	case core.GoFunc:
		kind = "GoFunc(" + x.Name + ")"
	case core.Macro:
		kind = "Macro(" + x.Name + ")"
	case core.Lambda:
		kind = "Lambda(" + x.Name + ")"
	default:
		kind = fmt.Sprintf("%T", v)
	}
	return fmt.Sprintf("%s canonical=%t", kind, canonical)
}

func materializedBindings(env *core.Env) map[string]string {
	out := make(map[string]string)
	for _, name := range env.LocalNames() {
		if v, ok, canon := env.GetMaterializedCanonical(name); ok {
			out["value "+name] = describeBinding(v, canon)
		}
	}
	for _, name := range env.LocalFuncNames() {
		if v, ok, canon := env.GetMaterializedFuncCanonical(name); ok {
			out["func "+name] = describeBinding(v, canon)
		}
	}
	return out
}

func TestEagerLazyBindingParity(t *testing.T) {
	rows := []struct {
		name    string
		dialect func(t *testing.T) core.Dialect
	}{
		{name: "cl", dialect: func(*testing.T) core.Dialect { return cl.Dialect() }},
		{name: "lisp2-identity-operator", dialect: func(t *testing.T) core.Dialect {
			return mustDialect(t, spec{
				Lisp2: true,
				Forms: uniqueForms(t, nil),
				Vocab: map[string]string{"+": "+", "car": "first"},
			})
		}},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			d := row.dialect(t)

			eager := newModeEngine(t, d, true, WithTreeWalker(), stdlib.New())
			eagerCells := materializedBindings(eager.RootEnv())

			lazy := newModeEngine(t, d, false, WithTreeWalker(), stdlib.New())
			lazy.RootEnv().VarNames()
			lazy.RootEnv().FuncNames()
			lazyCells := materializedBindings(lazy.RootEnv())

			for key, want := range lazyCells {
				got, ok := eagerCells[key]
				if !ok {
					t.Errorf("%s: bound lazily as %q, absent eagerly", key, want)
					continue
				}
				assert.Equal(t, want, got, "%s: eager binding differs from lazy", key)
			}
			for key, got := range eagerCells {
				if _, ok := lazyCells[key]; !ok {
					t.Errorf("%s: bound eagerly as %q, absent lazily", key, got)
				}
			}
		})
	}
}

type cellStamp struct {
	name    string
	isFunc  bool
	cell    *core.Cell
	version uint64
}

// liveCell reads a cell only after a lock-held liveness check: Cell and
// FuncCell materialize from an attached lazy layer on a miss, which would
// write the very cells under observation.
func liveCell(env *core.Env, name string, isFunc bool) (*core.Cell, bool) {
	if isFunc {
		if !env.HasLiveFunc(name) {
			return nil, false
		}
		return env.FuncCell(name)
	}
	if !env.HasLive(name) {
		return nil, false
	}
	return env.Cell(name)
}

func stampCells(env *core.Env) map[string]cellStamp {
	out := make(map[string]cellStamp)
	for _, name := range env.LocalNames() {
		if c, ok := liveCell(env, name, false); ok {
			out["value "+name] = cellStamp{name: name, cell: c, version: c.Version()}
		}
	}
	for _, name := range env.LocalFuncNames() {
		if c, ok := liveCell(env, name, true); ok {
			out["func "+name] = cellStamp{name: name, isFunc: true, cell: c, version: c.Version()}
		}
	}
	return out
}

func TestUseJSONLeavesStdlibCellsUntouched(t *testing.T) {
	for _, mode := range registrationModes {
		t.Run(mode.name, func(t *testing.T) {
			eng := newModeEngine(t, cl.Dialect(), mode.eager, WithTreeWalker(), stdlib.New())
			root := eng.RootEnv()
			if !mode.eager {
				root.VarNames()
				root.FuncNames()
			}
			before := stampCells(root)
			require.NotEmpty(t, before)

			require.NoError(t, eng.Use(json.New()))

			var touched []string
			for key, s := range before {
				c, ok := liveCell(root, s.name, s.isFunc)
				switch {
				case !ok:
					touched = append(touched, key+" unbound")
				case c != s.cell:
					touched = append(touched, key+" cell replaced")
				case c.Version() != s.version:
					touched = append(touched, fmt.Sprintf("%s version %d -> %d", key, s.version, c.Version()))
				}
			}
			slices.Sort(touched)
			assert.Empty(t, touched, "Use(json) must not write or journal any pre-existing stdlib cell")
		})
	}
}
