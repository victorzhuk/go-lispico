// Package goldset runs the release gate's gold set: a corpus of rule-shaped
// fixtures with independent expected results, owned by this repo and
// executed under both execution modes (ADR 0008). The corpus is modeled on
// embedder rule workloads — dispatch, closure state, error handling,
// keyword lookups, macros, collection folds, rule loading — and covers the
// VM correctness-floor risk areas, independent of any single consumer.
//
// Goldens are hand-derived from the language contract, never captured from
// either engine, so neither execution mode is the correctness oracle.
package goldset

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
	"github.com/victorzhuk/go-lispico/runtime"
)

type Mode string

const (
	ModeEvaluator Mode = "eval"
	ModeVM        Mode = "vm"
)

// Modes are the two execution modes every fixture must agree under.
var Modes = []Mode{ModeEvaluator, ModeVM}

// NewEngine builds the engine configuration the gate measures: Clojure
// dialect plus stdlib — the surface embedder rules run on.
func NewEngine(mode Mode) (runtime.Engine, error) {
	opts := []runtime.EngineOption{runtime.WithDialect(clojure.Dialect())}
	if mode == ModeVM {
		opts = append(opts, runtime.WithBytecode())
	}
	eng, err := runtime.New(nil, opts...)
	if err != nil {
		return nil, err
	}
	if err := eng.Use(stdlib.New()); err != nil {
		_ = eng.Close()
		return nil, err
	}
	return eng, nil
}

// Fixture is one gold-set cell: Lisp source and the expected String() of the
// last form's result.
type Fixture struct {
	Name   string
	Source string
	Want   string
}

// Fixtures loads every testdata/<name>.lisp with its testdata/<name>.golden
// pair. A .lisp file without a .golden is an error: an unasserted fixture
// must not silently pass the gate.
func Fixtures() ([]Fixture, error) {
	paths, err := filepath.Glob(filepath.Join("testdata", "*.lisp"))
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("goldset: no fixtures in testdata")
	}
	fixtures := make([]Fixture, 0, len(paths))
	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		name := strings.TrimSuffix(filepath.Base(p), ".lisp")
		golden, err := os.ReadFile(filepath.Join("testdata", name+".golden"))
		if err != nil {
			return nil, fmt.Errorf("goldset: fixture %q has no golden: %w", name, err)
		}
		fixtures = append(fixtures, Fixture{
			Name:   name,
			Source: string(src),
			Want:   strings.TrimSpace(string(golden)),
		})
	}
	return fixtures, nil
}
