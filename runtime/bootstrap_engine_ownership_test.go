package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
)

// newProbedStdlibEngine builds a bytecode Clojure engine whose root env carries
// its own ownerProbe, installed before stdlib loads so every DefineBootstrap
// call is attributable to the Engine that made it.
func newProbedStdlibEngine(t *testing.T) (Engine, *ownerProbe) {
	t.Helper()

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })

	root := eng.RootEnv()
	probe := newOwnerProbe(root.Evaluator())
	root.SetEvaluator(probe)

	if err := eng.Use(stdlib.New()); err != nil {
		t.Fatalf("Use(stdlib): %v", err)
	}
	return eng, probe
}

// TestStdlibBootstrap_PerEngineDefinitionMultiplicity proves the immutable
// name/source template shared for lazy discovery yields no shared definition
// work: two Engines of the same dialect each run their own DefineBootstrap for
// the same bootstrap name, and neither rides on the other's evaluation.
func TestStdlibBootstrap_PerEngineDefinitionMultiplicity(t *testing.T) {
	t.Parallel()

	engA, probeA := newProbedStdlibEngine(t)
	engB, probeB := newProbedStdlibEngine(t)

	baseA, baseB := probeA.Count(), probeB.Count()

	if _, ok := engA.RootEnv().Get("->"); !ok {
		t.Fatalf("-> not published on engine A after first touch")
	}
	if got := probeA.Count() - baseA; got != 1 {
		t.Fatalf("engine A first touch made %d DefineBootstrap calls, want exactly 1", got)
	}
	if got := probeB.Count() - baseB; got != 0 {
		t.Fatalf("engine B recorded %d DefineBootstrap calls while only engine A touched ->; a definition evaluated for one Engine must not satisfy another", got)
	}

	if _, ok := engB.RootEnv().Get("->"); !ok {
		t.Fatalf("-> not published on engine B after first touch")
	}
	if got := probeB.Count() - baseB; got != 1 {
		t.Fatalf("engine B first touch made %d DefineBootstrap calls, want exactly 1; each Engine evaluates the shared template for its own environment", got)
	}

	assertDefinedItself(t, "A", probeA.Sources()[baseA:])
	assertDefinedItself(t, "B", probeB.Sources()[baseB:])
}

// TestStdlibBootstrap_MacroValuesAreEngineOwned proves the value bound from a
// shared template is Engine-owned: each Engine's -> macro closes over its own
// defining environment, and redefining -> on one Engine leaves the other's
// binding intact.
func TestStdlibBootstrap_MacroValuesAreEngineOwned(t *testing.T) {
	t.Parallel()

	engA := newBytecodeStdlibEngine(t)
	defer engA.Close()
	engB := newBytecodeStdlibEngine(t)
	defer engB.Close()

	ctx := context.Background()
	if _, err := engA.Eval(ctx, "mark-a", "(def owner-marker 1)"); err != nil {
		t.Fatalf("bind owner marker on engine A: %v", err)
	}

	macroA := bootstrapMacro(t, engA, "->")
	macroB := bootstrapMacro(t, engB, "->")

	if macroA.Env == nil || macroB.Env == nil {
		t.Fatalf("-> bound without a defining environment: A=%v B=%v", macroA.Env, macroB.Env)
	}
	if macroA.Env == macroB.Env {
		t.Fatalf("both Engines bound -> to a macro sharing one defining environment; defining environments are Engine-owned")
	}
	if _, ok := macroA.Env.Get("owner-marker"); !ok {
		t.Fatalf("engine A's -> was defined outside engine A's environment chain")
	}
	if _, ok := macroB.Env.Get("owner-marker"); ok {
		t.Fatalf("engine B's -> was defined inside engine A's environment chain")
	}

	if _, err := engA.Eval(ctx, "redefine-a", "(defmacro -> [x & forms] '(+ 4 5))"); err != nil {
		t.Fatalf("redefine -> on engine A: %v", err)
	}

	gotA, err := engA.Eval(ctx, "use-a", "(-> 1 (+ 2))")
	if err != nil {
		t.Fatalf("eval on engine A: %v", err)
	}
	if !(core.Int{V: 9}).Equals(gotA) {
		t.Fatalf("engine A after redefining ->: got %v, want 9", gotA)
	}

	gotB, err := engB.Eval(ctx, "use-b", "(-> 1 (+ 2))")
	if err != nil {
		t.Fatalf("eval on engine B: %v", err)
	}
	if !(core.Int{V: 3}).Equals(gotB) {
		t.Fatalf("engine B saw engine A's redefinition of ->: got %v, want 3", gotB)
	}
}

func assertDefinedItself(t *testing.T, engine string, sources []string) {
	t.Helper()
	for _, src := range sources {
		if strings.Contains(src, "(defmacro -> ") {
			return
		}
	}
	t.Fatalf("engine %s never evaluated the -> definition itself; sources: %v", engine, sources)
}

func bootstrapMacro(t *testing.T, eng Engine, name string) core.Macro {
	t.Helper()
	v, ok := eng.RootEnv().Get(name)
	if !ok {
		t.Fatalf("%s not published in the value cell under the Lisp-1 owner", name)
	}
	m, ok := v.(core.Macro)
	if !ok {
		t.Fatalf("%s bound to %T, want core.Macro", name, v)
	}
	return m
}
