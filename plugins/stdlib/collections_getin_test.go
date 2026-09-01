package stdlib

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

func requireCode(t *testing.T, err error, code string) {
	t.Helper()
	require.Error(t, err)
	var lerr *core.LispicoError
	require.True(t, errors.As(err, &lerr), "expected *core.LispicoError, got %v", err)
	require.Equal(t, code, lerr.Code, "error: %v", err)
}

func TestCollections_GetIn(t *testing.T) {
	env := setupEnv(t)

	tests := []struct {
		name     string
		input    string
		expected core.Value
		code     string
	}{
		{name: "empty list path returns subject", input: `(get-in 5 (list))`, expected: core.Int{V: 5}},
		{name: "empty vector path returns subject", input: `(get-in 5 (vector))`, expected: core.Int{V: 5}},
		{name: "nil path is the empty path", input: `(get-in 5 nil)`, expected: core.Int{V: 5}},
		{name: "nil path with nil subject", input: `(get-in nil nil)`, expected: core.Nil{}},
		{name: "empty path ignores default", input: `(get-in 5 (list) :d)`, expected: core.Int{V: 5}},

		{name: "nested hit", input: `(get-in (hash-map :a (hash-map :b 7)) (list :a :b))`, expected: core.Int{V: 7}},
		{name: "nested hit inherited", input: `(get-in (hash-map :a (hash-map :b 1)) (list :a :b))`, expected: core.Int{V: 1}},
		{name: "vector path hit", input: `(get-in (hash-map :a (hash-map :b 7)) (vector :a :b))`, expected: core.Int{V: 7}},
		{name: "single key hit", input: `(get-in (hash-map :a 1) (list :a))`, expected: core.Int{V: 1}},

		{name: "terminal nil is a hit", input: `(get-in (hash-map :a (hash-map :b nil)) (list :a :b))`, expected: core.Nil{}},
		{name: "terminal nil ignores default", input: `(get-in (hash-map :a (hash-map :b nil)) (list :a :b) :d)`, expected: core.Nil{}},

		{name: "intermediate nil is missing", input: `(get-in (hash-map :a nil) (list :a :b))`, expected: core.Nil{}},
		{name: "intermediate nil takes default", input: `(get-in (hash-map :a nil) (list :a :b) :d)`, expected: core.Keyword{V: "d"}},
		{name: "nil subject is missing", input: `(get-in nil (list :a))`, expected: core.Nil{}},
		{name: "nil subject takes default", input: `(get-in nil (list :a) :d)`, expected: core.Keyword{V: "d"}},

		{name: "absent key is missing", input: `(get-in (hash-map :a (hash-map)) (list :a :b :c))`, expected: core.Nil{}},
		{name: "absent key takes default", input: `(get-in (hash-map :a (hash-map)) (list :a :b :c) :d)`, expected: core.Keyword{V: "d"}},
		{name: "unhashable key is absent", input: `(get-in (hash-map :a 1) (list (list 1 2)))`, expected: core.Nil{}},
		{name: "unhashable key takes default", input: `(get-in (hash-map :a 1) (list (list 1 2)) :d)`, expected: core.Keyword{V: "d"}},

		{name: "scalar intermediate", input: `(get-in (hash-map :a 1) (list :a :b))`, code: "TypeError"},
		{name: "scalar intermediate with default", input: `(get-in (hash-map :a 1) (list :a :b) :d)`, code: "TypeError"},
		{name: "list subject is not indexable", input: `(get-in (list 1 2) (list 0))`, code: "TypeError"},
		{name: "vector subject is not indexable", input: `(get-in (vector 1 2) (list 0))`, code: "TypeError"},
		{name: "string subject is not indexable", input: `(get-in "ab" (list 0))`, code: "TypeError"},

		{name: "keyword path", input: `(get-in (hash-map :a 1) :not-a-path)`, code: "TypeError"},
		{name: "int path", input: `(get-in (hash-map :a 1) 5)`, code: "TypeError"},
		{name: "map path", input: `(get-in (hash-map :a 1) (hash-map))`, code: "TypeError"},

		{name: "no args", input: `(get-in)`, code: "ArityError"},
		{name: "one arg", input: `(get-in (hash-map :a 1))`, code: "ArityError"},
		{name: "four args", input: `(get-in (hash-map :a 1) (list :a) :d :x)`, code: "ArityError"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.code != "" {
				requireCode(t, evalErr(t, env, tt.input), tt.code)
				return
			}
			result := eval(t, env, tt.input)
			require.True(t, result.Equals(tt.expected), "expected %v, got %v", tt.expected, result)
		})
	}
}

// The default is not a fallback for "nothing to traverse": an empty path is a
// successful lookup of the subject itself, so the third argument is never read.
func TestCollections_GetIn_EmptyPathIgnoresDefault(t *testing.T) {
	env := setupEnv(t)

	result := eval(t, env, `(get-in (hash-map :a 1) (list) :d)`)
	m, ok := result.(*core.HashMap)
	require.True(t, ok, "expected the subject map, got %v", result)
	v, found := m.Get(core.Keyword{V: "a"})
	require.True(t, found)
	require.True(t, v.Equals(core.Int{V: 1}), "expected 1, got %v", v)

	scalar := eval(t, env, `(get-in 5 (vector) :d)`)
	require.True(t, scalar.Equals(core.Int{V: 5}), "expected 5, got %v", scalar)

	nilSubject := eval(t, env, `(get-in nil nil :d)`)
	require.True(t, nilSubject.Equals(core.Nil{}), "expected nil, got %v", nilSubject)
}

// A stored nil at the last key is present, so the lookup succeeds with nil and
// the default stays unread.
func TestCollections_GetIn_TerminalNilIsSuccess(t *testing.T) {
	env := setupEnv(t)

	plain := eval(t, env, `(get-in (hash-map :a (hash-map :b nil)) (list :a :b))`)
	require.True(t, plain.Equals(core.Nil{}), "expected nil, got %v", plain)

	withDefault := eval(t, env, `(get-in (hash-map :a (hash-map :b nil)) (list :a :b) :d)`)
	require.True(t, withDefault.Equals(core.Nil{}), "expected nil, got %v", withDefault)
}

// A nil met with keys still to walk is a missing path, not a terminal hit, so
// here the default does apply.
func TestCollections_GetIn_IntermediateNilIsMissing(t *testing.T) {
	env := setupEnv(t)

	carried := eval(t, env, `(get-in (hash-map :a nil) (list :a :b))`)
	require.True(t, carried.Equals(core.Nil{}), "expected nil, got %v", carried)

	carriedDefault := eval(t, env, `(get-in (hash-map :a nil) (list :a :b) :d)`)
	require.True(t, carriedDefault.Equals(core.Keyword{V: "d"}), "expected :d, got %v", carriedDefault)

	atStart := eval(t, env, `(get-in nil (list :a))`)
	require.True(t, atStart.Equals(core.Nil{}), "expected nil, got %v", atStart)

	atStartDefault := eval(t, env, `(get-in nil (list :a) :d)`)
	require.True(t, atStartDefault.Equals(core.Keyword{V: "d"}), "expected :d, got %v", atStartDefault)
}

func TestCollections_GetIn_ScalarIntermediateTypeError(t *testing.T) {
	env := setupEnv(t)

	requireCode(t, evalErr(t, env, `(get-in (hash-map :a 1) (list :a :b))`), "TypeError")
	requireCode(t, evalErr(t, env, `(get-in (hash-map :a 1) (list :a :b) :d)`), "TypeError")
	requireCode(t, evalErr(t, env, `(get-in (hash-map :a "s") (list :a :b))`), "TypeError")
}

func TestCollections_GetIn_InvalidPathTypeTypeError(t *testing.T) {
	env := setupEnv(t)

	requireCode(t, evalErr(t, env, `(get-in (hash-map :a 1) :not-a-path)`), "TypeError")
	requireCode(t, evalErr(t, env, `(get-in (hash-map :a 1) 5)`), "TypeError")
	requireCode(t, evalErr(t, env, `(get-in (hash-map :a 1) (hash-map))`), "TypeError")
	requireCode(t, evalErr(t, env, `(get-in (hash-map :a 1) "ab" :d)`), "TypeError")
}

func TestCollections_GetIn_ArityError(t *testing.T) {
	env := setupEnv(t)

	requireCode(t, evalErr(t, env, `(get-in)`), "ArityError")
	requireCode(t, evalErr(t, env, `(get-in (hash-map :a 1))`), "ArityError")
	requireCode(t, evalErr(t, env, `(get-in (hash-map :a 1) (list :a) :d :x)`), "ArityError")
}

func TestCollections_GetIn_IsBuiltin(t *testing.T) {
	env := setupEnv(t)

	v, ok := env.Get("get-in")
	require.True(t, ok, "get-in is not registered")
	gf, ok := v.(core.GoFunc)
	require.True(t, ok, "get-in is %T, want core.GoFunc", v)
	require.Equal(t, "get-in", gf.Name)
}
