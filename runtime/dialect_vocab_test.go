package runtime

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
)

func TestDialectVocab_RenamedBuiltinResolvesToSharedImpl(t *testing.T) {
	d := core.FullDialect().Vocabulary(map[string]string{"car": "first"})
	e, err := New(nil, WithDialect(d))
	require.NoError(t, err)
	defer e.Close()

	require.NoError(t, e.Use(stdlib.New()))

	got, err := e.Eval(context.Background(), "car", "(car '(1 2 3))")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 1}.Equals(got), "car must resolve to the shared first implementation")
}

func TestDialectVocab_EmptyBaseOmitsUnlistedBuiltin(t *testing.T) {
	d := core.EmptyDialect().
		Add("if", "if").
		Add("quote", "quote").
		Add("def", "def").
		Vocabulary(map[string]string{"first": "first"})
	e, err := New(nil, WithDialect(d))
	require.NoError(t, err)
	defer e.Close()

	require.NoError(t, e.Use(stdlib.New()))

	got, err := e.Eval(context.Background(), "first-only", "(first '(1 2 3))")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 1}.Equals(got), "first is in the allowlist and must be callable")

	_, err = e.Eval(context.Background(), "first-only", "(rest '(1 2 3))")
	require.Error(t, err, "rest is absent from the vocabulary allowlist and must be uncallable")
	assert.Contains(t, err.Error(), "undefined")

	got, err = e.Eval(context.Background(), "first-only", "(if true 7 8)")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 7}.Equals(got), "special forms from the delta remain callable")
}

// Empty-base rename: a visible name resolves to a canonical whose name is
// absent from the allowlist. The canonical's GoFunc is not in the env after
// the allowlist pass, so the rename must use the pre-strip snapshot.
func TestDialectVocab_EmptyBaseRenameResolvesThroughSnapshot(t *testing.T) {
	d := core.EmptyDialect().
		Add("if", "if").
		Add("quote", "quote").
		Vocabulary(map[string]string{"car": "first"})
	e, err := New(nil, WithDialect(d))
	require.NoError(t, err)
	defer e.Close()

	require.NoError(t, e.Use(stdlib.New()))

	got, err := e.Eval(context.Background(), "car-only", "(car '(7 8 9))")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 7}.Equals(got), "car must resolve to the shared first implementation through the snapshot")

	_, err = e.Eval(context.Background(), "car-only", "(first '(7 8 9))")
	require.Error(t, err, "first is not in the allowlist and must be uncallable")
}

// As a registered builtin get-in is subject to the empty-base allowlist: a
// vocabulary that omits it strips the binding, where the former Lisp
// definition survived the strip.
func TestDialectVocab_EmptyBaseOmitsGetIn(t *testing.T) {
	d := core.EmptyDialect().
		Add("if", "if").
		Add("quote", "quote").
		Add("def", "def").
		Vocabulary(map[string]string{"first": "first"})
	e, err := New(nil, WithDialect(d))
	require.NoError(t, err)
	defer e.Close()

	require.NoError(t, e.Use(stdlib.New()))

	root := e.RootEnv()
	if val, ok := root.Get("get-in"); ok {
		t.Errorf("get-in is absent from the vocabulary allowlist and must not stay bound in the value cell, got %T", val)
	}
	if val, ok := root.GetFunc("get-in"); ok {
		t.Errorf("get-in is absent from the vocabulary allowlist and must not stay bound in the function cell, got %T", val)
	}

	_, err = e.Eval(context.Background(), "no-get-in", "(get-in (quote ()) (quote ()))")
	require.Error(t, err, "get-in is absent from the vocabulary allowlist and must be uncallable")
	assert.Contains(t, err.Error(), "undefined")
}

// An allowlisted get-in rebinds to the snapshotted shared builtin, so the
// lookup runs under a vocabulary that omits every name its former Lisp
// definition needed.
func TestDialectVocab_EmptyBaseAllowlistedGetInResolvesToSharedBuiltin(t *testing.T) {
	d := core.EmptyDialect().
		Add("if", "if").
		Add("quote", "quote").
		Add("def", "def").
		Vocabulary(map[string]string{
			"first":    "first",
			"get-in":   "get-in",
			"hash-map": "hash-map",
			"list":     "list",
		})
	e, err := New(nil, WithDialect(d))
	require.NoError(t, err)
	defer e.Close()

	require.NoError(t, e.Use(stdlib.New()))

	bound, ok := e.RootEnv().Get("get-in")
	require.True(t, ok, "an allowlisted get-in must stay bound")
	fn, ok := bound.(core.GoFunc)
	require.True(t, ok, "get-in = %T, want core.GoFunc", bound)
	assert.Equal(t, "get-in", fn.Name, "get-in must rebind to the shared builtin")

	got, err := e.Eval(context.Background(), "get-in-allowed", "(get-in (hash-map :a 1) (list :a))")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 1}.Equals(got), "allowlisted get-in must resolve to the shared builtin")
}

func TestDialectVocab_AdapterResolvesToSharedImpl(t *testing.T) {
	adapter := core.GoFunc{
		Name: "rev-first",
		Fn: func(ctx context.Context, _ core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			if len(args) != 2 {
				return nil, assert.AnError
			}
			shared, ok := env.Get("first")
			if !ok {
				return nil, assert.AnError
			}
			return shared.(core.GoFunc).Fn(ctx, nil, []core.Value{args[1]}, env)
		},
	}

	d := core.FullDialect().WithAdapter("rev-first", "rev-first@1", adapter)
	e, err := New(nil, WithDialect(d))
	require.NoError(t, err)
	defer e.Close()

	require.NoError(t, e.Use(stdlib.New()))

	carVal, ok := e.RootEnv().Get("first")
	require.True(t, ok, "the shared first builtin must remain in the env")
	adapterVal, ok := e.RootEnv().Get("rev-first")
	require.True(t, ok, "the adapter must be bound under its visible name")

	carFn := carVal.(core.GoFunc).Fn
	adapterFn := adapterVal.(core.GoFunc).Fn
	samePtr := reflect.ValueOf(carFn).Pointer() == reflect.ValueOf(adapterFn).Pointer()
	assert.False(t, samePtr, "adapter must not be a duplicate of the shared implementation")

	got, err := e.Eval(context.Background(), "rev", "(rev-first 99 '(10 20 30))")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 10}.Equals(got), "adapter must delegate to the shared first implementation")
}
