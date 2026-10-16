package goldset

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/victorzhuk/go-lispico/cl"
	"github.com/victorzhuk/go-lispico/core"
)

// TestGoldset runs every fixture under both execution modes against its
// golden. Neither mode is the oracle: the golden is the independent expected
// result (ADR 0008).
func TestGoldset(t *testing.T) {
	t.Parallel()

	fixtures, err := Fixtures()
	require.NoError(t, err)

	for _, mode := range Modes {
		for _, fx := range fixtures {
			t.Run(string(mode)+"/"+fx.Name, func(t *testing.T) {
				t.Parallel()
				eng, err := NewEngine(mode)
				require.NoError(t, err)
				t.Cleanup(func() { _ = eng.Close() })

				got, err := eng.Eval(context.Background(), fx.Name, fx.Source)
				require.NoError(t, err)
				require.Equal(t, fx.Want, got.String())
			})
		}
	}
}

// TestGoldsetCall runs the Engine.Call gold-set cell under both execution
// modes against its golden. Unlike TestGoldset it asserts through Call, not
// Eval: an unasserted fixture must not silently pass the gate.
func TestGoldsetCall(t *testing.T) {
	t.Parallel()

	cell, err := CallFixture()
	require.NoError(t, err)

	for _, mode := range Modes {
		t.Run(string(mode)+"/"+cell.Name, func(t *testing.T) {
			t.Parallel()
			eng, err := NewEngine(mode)
			require.NoError(t, err)
			t.Cleanup(func() { _ = eng.Close() })

			_, err = eng.Eval(context.Background(), cell.Name, cell.Source)
			require.NoError(t, err)

			got, err := eng.Call(context.Background(), cell.Fn, cell.Args...)
			require.NoError(t, err)
			require.Equal(t, cell.Want, got.String())
		})
	}
}

// TestGoldsetCL runs the representative Clojure-adapter fixtures through a
// CL engine under both execution modes against their independent goldens.
// Each fixture is evaluated twice on one engine: the second result must
// still equal the golden, so stateful reuse cannot drift.
func TestGoldsetCL(t *testing.T) {
	t.Parallel()

	clFixtures, err := fixtures(filepath.Join("testdata", "cl"))
	require.NoError(t, err)
	require.NotEmpty(t, clFixtures)

	for _, mode := range Modes {
		for _, fx := range clFixtures {
			t.Run(string(mode)+"/"+fx.Name, func(t *testing.T) {
				t.Parallel()
				eng, err := newEngine(mode, cl.Dialect())
				require.NoError(t, err)
				t.Cleanup(func() { _ = eng.Close() })
				ctx := context.Background()

				for range 2 {
					got, err := eng.Eval(ctx, fx.Name, fx.Source)
					require.NoError(t, err)
					require.Equal(t, fx.Want, got.String())
				}
			})
		}
	}
}

// TestGoldsetConcurrent runs two closures captured from distinct
// environments of one shared Clojure engine: each worker Calls its own
// closure and must observe only its own offset. Workers record their first
// error locally; only the parent test asserts, after joining both workers.
func TestGoldsetConcurrent(t *testing.T) {
	t.Parallel()

	concFixtures, err := fixtures(filepath.Join("testdata", "concurrent"))
	require.NoError(t, err)
	require.Len(t, concFixtures, 1)
	fx := concFixtures[0]
	require.Equal(t, "distinct-closures", fx.Name)

	for _, mode := range Modes {
		t.Run(string(mode)+"/"+fx.Name, func(t *testing.T) {
			t.Parallel()
			eng, err := NewEngine(mode)
			require.NoError(t, err)
			t.Cleanup(func() { _ = eng.Close() })
			ctx := context.Background()

			got, err := eng.Eval(ctx, fx.Name, fx.Source)
			require.NoError(t, err)
			require.Equal(t, fx.Want, got.String())

			const callsPerWorker = 25
			type worker struct {
				fn   string
				want core.Int
			}
			workers := []worker{{fn: "rule-a", want: core.Int{V: 101}}, {fn: "rule-b", want: core.Int{V: 102}}}
			args := []core.Value{core.Int{V: 100}}
			var wg sync.WaitGroup
			start := make(chan struct{})
			firstErrs := make([]error, len(workers))
			for i, w := range workers {
				wg.Add(1)
				go func(i int, w worker) {
					defer wg.Done()
					<-start
					for range callsPerWorker {
						got, err := eng.Call(ctx, w.fn, args...)
						if err != nil {
							firstErrs[i] = err
							return
						}
						gotInt, ok := got.(core.Int)
						if !ok || gotInt != w.want {
							firstErrs[i] = fmt.Errorf("%s: got %#v, want %#v", w.fn, got, w.want)
							return
						}
					}
				}(i, w)
			}
			close(start)
			wg.Wait()
			for _, err := range firstErrs {
				require.NoError(t, err)
			}
		})
	}
}
