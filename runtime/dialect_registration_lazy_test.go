package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

type registration struct {
	name  string
	value core.Value
}

// templatePlugin shares stdlib's namespace shape (Name() == "") so a lazy
// engine defers its registrations into the process-level template layer.
type templatePlugin struct {
	version string
	regs    []registration
}

func (p *templatePlugin) Name() string { return "" }

func (p *templatePlugin) Metadata() core.PluginMeta {
	return core.PluginMeta{Version: p.version}
}

func (p *templatePlugin) Init(env *core.Env) error {
	for _, r := range p.regs {
		if err := env.RegisterValue(r.name, r.value, false); err != nil {
			return err
		}
	}
	return nil
}

func namedGoFunc(name string) core.GoFunc {
	return core.GoFunc{
		Name: name,
		Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
			return core.Nil{}, nil
		},
	}
}

func forceMaterialize(env *core.Env) {
	env.VarNames()
	env.FuncNames()
}

func goFuncName(t *testing.T, env *core.Env, name string) string {
	t.Helper()
	v, ok, _ := env.GetMaterializedCanonical(name)
	if !ok {
		return "<unbound>"
	}
	fn, isGoFunc := v.(core.GoFunc)
	if !isGoFunc {
		return "<not a GoFunc>"
	}
	return fn.Name
}

func TestVocabRenameOwnsVisibleNameOnBothPaths(t *testing.T) {
	const visible, canonical = "rename-visible", "rename-canonical"
	d := mustDialect(t, spec{
		Forms: uniqueForms(t, nil),
		Vocab: map[string]string{visible: canonical},
	})

	arms := []struct {
		name    string
		version string
		regs    []registration
		want    string
	}{
		{
			name:    "canonical then visible",
			version: t.Name() + "-both",
			regs: []registration{
				{name: canonical, value: namedGoFunc("canonical-fn")},
				{name: visible, value: namedGoFunc("own-visible-fn")},
			},
			want: "canonical-fn",
		},
		{
			name:    "visible only",
			version: t.Name() + "-visible-only",
			regs: []registration{
				{name: visible, value: namedGoFunc("own-visible-fn")},
			},
			want: "own-visible-fn",
		},
	}
	for _, arm := range arms {
		t.Run(arm.name, func(t *testing.T) {
			eager := newModeEngine(t, d, true, WithTreeWalker(), &templatePlugin{version: arm.version, regs: arm.regs})
			lazy := newModeEngine(t, d, false, WithTreeWalker(), &templatePlugin{version: arm.version, regs: arm.regs})
			forceMaterialize(lazy.RootEnv())

			assert.Equal(t, arm.want, goFuncName(t, eager.RootEnv(), visible),
				"eager: %s must bind to %s", visible, arm.want)
			assert.Equal(t, arm.want, goFuncName(t, lazy.RootEnv(), visible),
				"lazy: %s must bind to %s", visible, arm.want)
		})
	}
}

func TestLisp2RegisterValueMirrorsOnlyFunctions(t *testing.T) {
	d := mustDialect(t, spec{Lisp2: true, Forms: uniqueForms(t, nil)})
	regs := []registration{
		{name: "answer", value: core.Int{V: 42}},
		{name: "mirror-fn", value: namedGoFunc("mirror-fn")},
	}

	for _, mode := range registrationModes {
		t.Run(mode.name, func(t *testing.T) {
			eng := newModeEngine(t, d, mode.eager, WithTreeWalker(), &templatePlugin{version: t.Name(), regs: regs})
			root := eng.RootEnv()
			if !mode.eager {
				forceMaterialize(root)
			}

			require.True(t, root.HasLive("answer"), "answer must be bound in the value cell")
			assert.False(t, root.HasLiveFunc("answer"), "a non-function registered under Lisp-2 must get no function cell")
			assert.True(t, root.HasLiveFunc("mirror-fn"), "a GoFunc registered under Lisp-2 must be mirrored into the function cell")
		})
	}
}
