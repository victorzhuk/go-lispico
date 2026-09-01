// Behavior goldens for the get-in builtin. Every row states its own expected
// value or error Code, so two engines agreeing on a wrong answer still fails.
// Sources stay CL-portable — no bracket literals — because the default dialect
// disables them.
package runtime

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/cl"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
)

// getInGolden is one hand-derived row. want and code are exclusive: a value
// row carries the literal it must produce, an error row carries the Code both
// engines must classify it under. notWant names the plausible wrong answer the
// row exists to rule out.
type getInGolden struct {
	name    string
	src     string
	want    core.Value
	notWant core.Value
	code    string
}

// getInSubjectMap is the subject of the empty-path row, built independently of
// any evaluation so the row compares against a Go-side literal.
func getInSubjectMap(t *testing.T) *core.HashMap {
	t.Helper()
	m := core.NewHashMap()
	require.NoError(t, m.Set(core.Keyword{V: "a"}, core.Int{V: 1}))
	return m
}

// lispErrorCode reads the Code off any *core.LispicoError. resourceLimitErrorCode
// stays with the resource-limit subtest it is named for, so a TypeError row does
// not read as a resource-limit row.
func lispErrorCode(t *testing.T, err error) string {
	t.Helper()
	require.Error(t, err)
	var lerr *core.LispicoError
	require.ErrorAs(t, err, &lerr)
	return lerr.Code
}

func getInGoldenCorpus(t *testing.T) []getInGolden {
	t.Helper()
	return []getInGolden{
		{
			name:    "nested hit",
			src:     "(get-in (hash-map :a (hash-map :b 7)) (list :a :b))",
			want:    core.Int{V: 7},
			notWant: core.Nil{},
		},
		{
			name: "absent key is missing",
			src:  "(get-in (hash-map :a (hash-map)) (list :a :b :c))",
			want: core.Nil{},
		},
		{
			name:    "absent key takes the default",
			src:     "(get-in (hash-map :a (hash-map)) (list :a :b :c) :d)",
			want:    core.Keyword{V: "d"},
			notWant: core.Nil{},
		},
		{
			name:    "stored nil is terminal",
			src:     "(get-in (hash-map :a (hash-map :b nil)) (list :a :b) :d)",
			want:    core.Nil{},
			notWant: core.Keyword{V: "d"},
		},
		{
			name:    "empty path returns the subject",
			src:     "(get-in (hash-map :a 1) (list) :d)",
			want:    getInSubjectMap(t),
			notWant: core.Keyword{V: "d"},
		},
		{
			name: "nil path over a nil subject",
			src:  "(get-in nil nil)",
			want: core.Nil{},
		},
		{
			name: "scalar intermediate is a type error",
			src:  "(get-in (hash-map :a 1) (list :a :b))",
			code: "TypeError",
		},
		{
			name: "non-sequence path is a type error",
			src:  "(get-in (hash-map :a 1) 5)",
			code: "TypeError",
		},
	}
}

func runGetInGoldens(t *testing.T, label string, eng Engine) {
	t.Helper()
	ctx := context.Background()
	rows := getInGoldenCorpus(t)
	require.NotEmpty(t, rows, "%s: the golden corpus must carry rows", label)

	for _, g := range rows {
		got, err := eng.Eval(ctx, "get-in-golden", g.src)
		if g.code != "" {
			require.Error(t, err, "%s/%s: %s must fail", label, g.name, g.src)
			assert.Equal(t, g.code, lispErrorCode(t, err),
				"%s/%s: %s must classify under %s", label, g.name, g.src, g.code)
			continue
		}
		require.NoError(t, err, "%s/%s: %s", label, g.name, g.src)
		assert.True(t, g.want.Equals(got), "%s/%s: %s => %v, want %v", label, g.name, g.src, got, g.want)
		if g.notWant != nil {
			assert.False(t, g.notWant.Equals(got),
				"%s/%s: %s => %v, the wrong answer this row rules out", label, g.name, g.src, got)
		}
	}
}

// TestGetIn_BehaviorGoldens pins the get-in contract across both evaluators and
// both stdlib startup modes, plus the default engine, and closes with the
// reduction ceiling a long path must surface.
func TestGetIn_BehaviorGoldens(t *testing.T) {
	for _, em := range goldenEvaluatorModes {
		for _, sm := range goldenModes {
			label := em.name + "/" + sm.name
			t.Run(label, func(t *testing.T) {
				runGetInGoldens(t, label, loadStdlibEngine(t, cl.Dialect(), sm.eager, em.opts...))
			})
		}
	}

	t.Run("default-engine", func(t *testing.T) {
		eng, err := New(nil)
		require.NoError(t, err)
		t.Cleanup(func() { _ = eng.Close() })
		require.NoError(t, eng.Use(stdlib.New()))
		runGetInGoldens(t, "default-engine", eng)
	})

	t.Run("resource-limit", func(t *testing.T) {
		const pathLen = 4096

		keys := make([]core.Value, pathLen)
		subject := core.Value(core.Int{V: 1})
		for i := pathLen - 1; i >= 0; i-- {
			key := core.Keyword{V: "k" + strconv.Itoa(i)}
			keys[i] = key
			m := core.NewHashMap()
			require.NoError(t, m.Set(key, subject))
			subject = m
		}

		// Headroom for the one-hop control lookup, far under the per-hop
		// charge a full walk of the path costs. Subject and path are bound
		// rather than read, so neither reader nor construction work lands on
		// the ledger the ceiling measures.
		limits := ResourceLimits{
			MaxReaderDepth:     1 << 20,
			MaxStructuralDepth: 1 << 20,
			MaxCollectionLen:   1 << 30,
			MaxCacheEntries:    4096,
			MaxReductions:      512,
			MaxAllocationBytes: 1 << 30,
		}

		for _, em := range goldenEvaluatorModes {
			t.Run(em.name, func(t *testing.T) {
				opts := append(append([]EngineOption{}, em.opts...), WithResourceLimits(limits))
				eng := loadStdlibEngine(t, cl.Dialect(), true, opts...)
				require.NoError(t, eng.Bind("deep-subject", subject))
				require.NoError(t, eng.Bind("deep-path", core.NewList(keys)))
				require.NoError(t, eng.Bind("short-path", core.NewList([]core.Value{keys[0]})))

				ctx := context.Background()
				got, err := eng.Eval(ctx, "get-in-control", "(get-in deep-subject short-path)")
				require.NoError(t, err, "%s: the one-hop control lookup must fit a %d-reduction ceiling", em.name, limits.MaxReductions)
				_, ok := got.(*core.HashMap)
				assert.True(t, ok, "%s: the one-hop control lookup returns the nested map, got %T", em.name, got)

				_, err = eng.Eval(ctx, "get-in-deep", "(get-in deep-subject deep-path)")
				require.Error(t, err, "%s: a %d-hop path must exceed a %d-reduction ceiling", em.name, pathLen, limits.MaxReductions)
				assert.Equal(t, core.CodeResourceLimit, resourceLimitErrorCode(t, err),
					"%s: the deep walk must classify under %s", em.name, core.CodeResourceLimit)
			})
		}
	})
}
