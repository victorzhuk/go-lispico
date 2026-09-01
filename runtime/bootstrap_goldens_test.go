package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/cl"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
)

// bootstrapGoldenNames is the frozen per-name golden corpus: the six bootstrap
// names with the kind each definition source binds (defmacro -> core.Macro,
// defn -> core.Lambda).
var bootstrapGoldenNames = []struct {
	name   string
	macro  bool
}{
	{name: "->", macro: true},
	{name: "->>", macro: true},
	{name: "as->", macro: true},
	{name: "if-let", macro: true},
	{name: "when-let", macro: true},
	{name: "get-in", macro: false},
}

// loadStdlibEngine builds a tree-walking engine under d, loads stdlib, and —
// in lazy mode — forces every deferred binding through the enumeration sweep
// (RootEnv().VarNames/FuncNames force their lazy layer), so cell assertions
// observe the published state. The process-global lazy flag is restored
// before the caller's assertions run; publication is already complete.
func loadStdlibEngine(t *testing.T, d core.Dialect, eager bool, opts ...EngineOption) Engine {
	t.Helper()
	if len(opts) == 0 {
		opts = []EngineOption{WithTreeWalker()}
	}
	restore := SetStdlibLazyDisabledForTesting(eager)
	defer restore()

	eng, err := New(nil, append(opts, WithDialect(d))...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	require.NoError(t, eng.Use(stdlib.New()))

	if !eager {
		eng.RootEnv().VarNames()
		eng.RootEnv().FuncNames()
	}
	return eng
}

var goldenModes = []struct {
	name  string
	eager bool
}{
	{name: "lazy", eager: false},
	{name: "eager", eager: true},
}

// assertKind checks the published binding is the kind its definition source
// binds: Macro for defmacro, Lambda for defn.
func assertKind(t *testing.T, mode, name string, val core.Value, macro bool) {
	t.Helper()
	if macro {
		mac, ok := val.(core.Macro)
		require.True(t, ok, "%s: %s = %T, want core.Macro", mode, name, val)
		assert.Equal(t, name, mac.Name, "%s: macro %s bound under wrong name", mode, name)
		return
	}
	lam, ok := val.(core.Lambda)
	require.True(t, ok, "%s: %s = %T, want core.Lambda", mode, name, val)
	assert.Equal(t, name, lam.Name, "%s: fn %s bound under wrong name", mode, name)
}

// TestBootstrapDialectGoldens_Lisp2 pins, for all six bootstrap names in both
// eager and lazy modes under the Lisp-2 (CL) dialect, that each definition
// lands in the function cell — so it resolves in head position — and never
// mirrors into the value cell.
func TestBootstrapDialectGoldens_Lisp2(t *testing.T) {
	for _, mode := range goldenModes {
		t.Run(mode.name, func(t *testing.T) {
			eng := loadStdlibEngine(t, cl.Dialect(), mode.eager)
			root := eng.RootEnv()
			for _, g := range bootstrapGoldenNames {
				got, ok := root.GetFunc(g.name)
				require.True(t, ok, "Lisp-2/%s: %s must be bound in the function cell", mode.name, g.name)
				assertKind(t, "Lisp-2/"+mode.name, g.name, got, g.macro)
				if _, ok := root.Get(g.name); ok {
					t.Errorf("Lisp-2/%s: %s mirrored into the value cell; the function cell owns operator bindings under Lisp-2", mode.name, g.name)
				}
			}
		})
	}

	execModes := []struct {
		name string
		opts []EngineOption
	}{
		{name: "tree-walker"},
		{name: "bytecode", opts: []EngineOption{WithBytecode()}},
	}
	goldens := []struct {
		name string
		src  string
		want core.Value
	}{
		{"cl/nth@1", "(nth 1 '(10 20 30))", core.Int{V: 20}},
		{"cl/mapcar@1", "(mapcar (fn (x) (* x x)) '(1 2 3))", core.NewList([]core.Value{core.Int{V: 1}, core.Int{V: 4}, core.Int{V: 9}})},
		{"cl/sort@1", "(sort '(3 1 2) #'<)", core.NewList([]core.Value{core.Int{V: 1}, core.Int{V: 2}, core.Int{V: 3}})},
		{"canonical map", "(map (fn (x) (* x x)) '(1 2 3))", core.NewList([]core.Value{core.Int{V: 1}, core.Int{V: 4}, core.Int{V: 9}})},
	}
	for _, em := range execModes {
		for _, mode := range goldenModes {
			t.Run(em.name+"/"+mode.name, func(t *testing.T) {
				eng := loadStdlibEngine(t, cl.Dialect(), mode.eager, em.opts...)
				root := eng.RootEnv()
				for _, name := range []string{"nth", "mapcar", "sort"} {
					if _, ok := root.Get(name); !ok {
						t.Errorf("%s/%s: %s must be bound in the root env", em.name, mode.name, name)
					}
				}
				for _, g := range goldens {
					got, err := eng.Eval(context.Background(), "lisp2-golden", g.src)
					require.NoError(t, err, "%s/%s: %s", em.name, mode.name, g.src)
					assert.True(t, g.want.Equals(got), "%s/%s: %s = %v, want %v", em.name, mode.name, g.src, got, g.want)
				}
			})
		}
	}
}

// TestBootstrapDialectGoldens_Lisp1 pins, for all six bootstrap names in both
// eager and lazy modes under the Lisp-1 (Clojure identity) dialect, that each
// definition lands in the value cell and never mirrors into the function
// cell — a single namespace has no second cell to populate.
func TestBootstrapDialectGoldens_Lisp1(t *testing.T) {
	for _, mode := range goldenModes {
		t.Run(mode.name, func(t *testing.T) {
			eng := loadStdlibEngine(t, clojure.Dialect(), mode.eager)
			root := eng.RootEnv()
			for _, g := range bootstrapGoldenNames {
				got, ok := root.Get(g.name)
				require.True(t, ok, "Lisp-1/%s: %s must be bound in the value cell", mode.name, g.name)
				assertKind(t, "Lisp-1/"+mode.name, g.name, got, g.macro)
				if _, ok := root.GetFunc(g.name); ok {
					t.Errorf("Lisp-1/%s: %s mirrored into the function cell; Lisp-1 has a single namespace with no func-cell mirror", mode.name, g.name)
				}
			}
		})
	}
}

// TestBootstrapDialectGoldens_EmptyBase pins that the restricted empty-base
// dialect does not lose trusted definitions: all six bootstrap names publish
// into the dialect-owned cell (Lisp-1 axis => value cell) in both modes.
func TestBootstrapDialectGoldens_EmptyBase(t *testing.T) {
	for _, mode := range goldenModes {
		t.Run(mode.name, func(t *testing.T) {
			eng := loadStdlibEngine(t, core.EmptyDialect(), mode.eager)
			root := eng.RootEnv()
			for _, g := range bootstrapGoldenNames {
				got, ok := root.Get(g.name)
				require.True(t, ok, "empty-base/%s: trusted definition %s was lost under the restricted dialect", mode.name, g.name)
				assertKind(t, "empty-base/"+mode.name, g.name, got, g.macro)
			}
		})
	}
}

// TestBootstrap_NoKernelTableWidening proves loading the full stdlib under an
// empty-base dialect widens nothing user-visible: trusted bootstrap names are
// present, yet user-level defmacro/defn still error, and kernel forms absent
// from the delta stay uncallable in both modes.
func TestBootstrap_NoKernelTableWidening(t *testing.T) {
	for _, mode := range goldenModes {
		t.Run(mode.name, func(t *testing.T) {
			eng := loadStdlibEngine(t, core.EmptyDialect(), mode.eager)
			root := eng.RootEnv()
			for _, g := range bootstrapGoldenNames {
				if _, ok := root.Get(g.name); !ok {
					t.Errorf("empty-base/%s: trusted definition %s was lost; the restricted dialect must not lose trusted definitions", mode.name, g.name)
				}
			}

			ctx := context.Background()
			for _, src := range []struct{ label, code string }{
				{label: "user-defmacro", code: "(defmacro user-m [] 1)"},
				{label: "user-defn", code: "(defn user-f [x] x)"},
				{label: "kernel-def", code: "(def)"},
				{label: "kernel-let", code: "(let)"},
				{label: "kernel-fn", code: "(fn)"},
				{label: "kernel-quote", code: "(quote)"},
				{label: "kernel-if", code: "(if)"},
			} {
				_, err := eng.Eval(ctx, src.label, src.code)
				require.Error(t, err, "empty-base/%s: %s must stay uncallable after stdlib load", mode.name, src.label)
				assert.Contains(t, err.Error(), "undefined", "empty-base/%s: %s error must be an undefined-name error, got %v", mode.name, src.label, err)
			}
		})
	}
}

// TestBootstrapCapability_TrustBoundary proves the bootstrap capability is a
// host-Go-only surface: both execution evaluators implement it, while nothing
// Lisp can enumerate or name — bound values, function cells, special-form
// names, empty-base vocabulary entries — exposes it.
func TestBootstrapCapability_TrustBoundary(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		opts []EngineOption
	}{
		{name: "tree", opts: []EngineOption{WithTreeWalker(), WithDialect(clojure.Dialect())}},
		{name: "bytecode", opts: []EngineOption{WithBytecode(), WithDialect(clojure.Dialect())}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eng, err := New(nil, tc.opts...)
			require.NoError(t, err)
			t.Cleanup(func() { _ = eng.Close() })
			require.NoError(t, eng.Use(stdlib.New()))
			root := eng.RootEnv()

			// Trusted host Go code reaches the capability on the installed
			// owner, never a fresh identity evaluator.
			_, ok := root.Evaluator().(core.BootstrapDefiner)
			require.True(t, ok, "%s engine: installed evaluator %T must implement core.BootstrapDefiner for trusted host code", tc.name, root.Evaluator())

			// Force the full lazy surface, then sweep every bound value:
			// zero Lisp-visible Values implement the capability.
			for _, name := range root.VarNames() {
				v, ok := root.Get(name)
				require.True(t, ok, "%s engine: enumerated value %s disappeared", tc.name, name)
				if _, isCap := v.(core.BootstrapDefiner); isCap {
					t.Errorf("%s engine: value binding %q implements core.BootstrapDefiner; Lisp must not reach the host capability", tc.name, name)
				}
			}
			for _, name := range root.FuncNames() {
				v, ok := root.GetFunc(name)
				require.True(t, ok, "%s engine: enumerated function %s disappeared", tc.name, name)
				if _, isCap := v.(core.BootstrapDefiner); isCap {
					t.Errorf("%s engine: function-cell binding %q implements core.BootstrapDefiner; Lisp must not reach the host capability", tc.name, name)
				}
			}

			// No special form or vocabulary name exposes the capability.
			for _, head := range []string{"define-bootstrap", "DefineBootstrap", "defbootstrap", "bootstrap-definer"} {
				_, err := eng.Eval(ctx, "cap-"+head, "("+head+")")
				require.Error(t, err, "%s engine: head (%s) must not resolve to the capability", tc.name, head)
			}
		})
	}

	// The empty-base dialect's vocabulary carries no capability-naming entry.
	for name := range core.EmptyDialect().Vocab() {
		assert.NotContains(t, strings.ToLower(name), "bootstrap",
			"empty-base vocabulary entry %q names the host capability", name)
	}
}
