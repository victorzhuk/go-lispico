package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
)

// pubStubPlugin is a stdlib-identity (Name "") stub so its registrations defer
// into the process-shared template layer. A unique version per run keeps the
// global registry clean across tests and -count>1.
type pubStubPlugin struct {
	version string
	init    func(env *core.Env) error
}

func (p *pubStubPlugin) Name() string { return "" }

func (p *pubStubPlugin) Init(env *core.Env) error {
	if p.init != nil {
		return p.init(env)
	}
	return nil
}

func (p *pubStubPlugin) Metadata() core.PluginMeta {
	return core.PluginMeta{Version: p.version, Description: "publication stub"}
}

var pubStubTestSeq atomic.Int64

func newPubTestEngine(t *testing.T, init func(env *core.Env) error) Engine {
	t.Helper()
	version := fmt.Sprintf("0.%d.pub", pubStubTestSeq.Add(1))
	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	require.NoError(t, eng.Use(&pubStubPlugin{version: version, init: init}))
	return eng
}

// hostFact is a host Go binding a test installs over a lazy recursive
// function's root name; it answers with a sentinel so a call that resolves
// the name through the root is distinguishable from one that resolves it
// through a hidden staged cell.
func hostFact(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
	return core.Int{V: -1}, nil
}

var hostFactFn = core.GoFunc{Name: "host-fact", Fn: hostFact}

// TestLazyPublication_ClosureFollowsRootBinding pins: a published lazy
// defn/defmacro closure must capture the destination env, not the staging
// child, so once the root binding is replaced or deleted the escaped function
// resolves exactly like its eagerly defined twin. The bodies recurse only on
// the function's own name plus the built-in if form, so no stdlib name is
// needed and the test needs a single Name("") plugin.
func TestLazyPublication_ClosureFollowsRootBinding(t *testing.T) {
	eng := newPubTestEngine(t, func(env *core.Env) error {
		if !env.RegisterSource("lazy-rec-f", "(defn lazy-rec-f [n] (if n (lazy-rec-f n) :done))") {
			return errors.New("RegisterSource refused to defer")
		}
		if !env.RegisterSource("lazy-rec-del", "(defn lazy-rec-del [n] (if n (lazy-rec-del n) :done))") {
			return errors.New("RegisterSource refused to defer")
		}
		return nil
	})
	root := eng.RootEnv()
	ctx := context.Background()
	eval, ok := root.Evaluator().(interface {
		Apply(ctx context.Context, fn core.Value, args []core.Value, env *core.Env) (core.Value, error)
	})
	require.True(t, ok, "root evaluator must expose Apply")

	// Eager twin: a user defn evaluated normally captures the root; record
	// its observable behavior for the same root manipulations.
	_, err := eng.Eval(ctx, "eager-def", "(defn eager-rec-f [n] (if n (eager-rec-f n) :done))")
	require.NoError(t, err)
	eager, ok := root.Get("eager-rec-f")
	require.True(t, ok)
	require.NotNil(t, eager)

	// Lazy defn: materialize on first touch and grab the published closure.
	lazyAny, ok := root.Get("lazy-rec-f")
	require.True(t, ok, "first touch must materialize")
	lazy, ok := lazyAny.(core.Lambda)
	require.True(t, ok, "lazy published value = %T, want core.Lambda", lazyAny)

	// Before any replacement both terminate on the base case.
	got, err := eval.Apply(ctx, lazy, []core.Value{core.Nil{}}, root)
	require.NoError(t, err)
	require.Equal(t, ":done", got.String(), "lazy recursion before replace")
	gotEager, err := eval.Apply(ctx, eager, []core.Value{core.Nil{}}, root)
	require.NoError(t, err)
	require.Equal(t, ":done", gotEager.String(), "eager twin before replace")

	// Replace the root binding. Both closures must observe the host binding:
	// the recursion call inside the body lands on it and the sentinel
	// propagates. A closure still holding the staged cell would recurse on
	// itself forever and die on the depth limit instead.
	require.NoError(t, root.Set("lazy-rec-f", hostFactFn))
	require.NoError(t, root.Set("eager-rec-f", hostFactFn))
	got, err = eval.Apply(ctx, lazy, []core.Value{core.Int{V: 1}}, root)
	require.NoError(t, err)
	require.True(t, core.Int{V: -1}.Equals(got),
		"lazy closure after root replace = %v, want -1 (host sentinel through recursion)", got)
	gotEager, err = eval.Apply(ctx, eager, []core.Value{core.Int{V: 1}}, root)
	require.NoError(t, err)
	require.True(t, core.Int{V: -1}.Equals(gotEager),
		"eager twin behaves differently: %v, want -1", gotEager)

	// Delete the root binding. Both closures must report the name unbound,
	// like the eager twin; a hidden staged cell would keep the recursion alive.
	lazyDelAny, ok := root.Get("lazy-rec-del")
	require.True(t, ok)
	lazyDel, ok := lazyDelAny.(core.Lambda)
	require.True(t, ok)
	root.Delete("lazy-rec-del")
	root.Delete("eager-rec-f")
	_, err = eval.Apply(ctx, lazyDel, []core.Value{core.Int{V: 1}}, root)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "lazy-rec-del"),
		"lazy closure after root delete = %v, want unbound lazy-rec-del", err)
	_, err = eval.Apply(ctx, eager, []core.Value{core.Int{V: 1}}, root)
	require.Error(t, err, "eager twin must also be unbound after delete")
}

// TestLazyPublication_HostSetWinsRace pins: a host binding racing a lazy
// materialization must always survive, in both namespaces. A staged
// publication must never overwrite a host Set that landed between its
// occupancy check and its write, which the env-level insert-if-unoccupied
// operation makes impossible.
func TestLazyPublication_HostSetWinsRace(t *testing.T) {
	const names = 256
	version := fmt.Sprintf("0.%d.race", pubStubTestSeq.Add(1))
	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	valueNames := make([]string, names)
	funcNames := make([]string, names)
	require.NoError(t, eng.Use(&pubStubPlugin{version: version, init: func(env *core.Env) error {
		for i := range names {
			valueNames[i] = fmt.Sprintf("lazy-race-v-%d", i)
			funcNames[i] = fmt.Sprintf("lazy-race-f-%d", i)
			if err := env.RegisterValue(valueNames[i], core.Int{V: 7}, false); err != nil {
				return err
			}
			if !env.RegisterSource(funcNames[i], fmt.Sprintf("(defn %s [] %d)", funcNames[i], i)) {
				return errors.New("RegisterSource refused to defer")
			}
		}
		return nil
	}}))

	root := eng.RootEnv()
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		<-start
		for _, name := range valueNames {
			root.Get(name)
		}
		for _, name := range funcNames {
			root.GetFunc(name)
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		for _, name := range valueNames {
			_ = root.Set(name, core.Int{V: -1})
		}
		for _, name := range funcNames {
			_ = root.SetFunc(name, core.Int{V: -1})
		}
	}()
	close(start)
	wg.Wait()

	for _, name := range valueNames {
		v, ok, _ := root.GetMaterializedCanonical(name)
		if !ok || !(core.Int{V: -1}).Equals(v) {
			t.Fatalf("value cell %q = %v (found %v), want host -1", name, v, ok)
		}
	}
	for _, name := range funcNames {
		v, ok, _ := root.GetMaterializedFuncCanonical(name)
		if !ok || !(core.Int{V: -1}).Equals(v) {
			t.Fatalf("function cell %q = %v (found %v), want host -1", name, v, ok)
		}
	}
}
