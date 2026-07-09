package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
)

func TestDialect_TwoEnginesDoNotInterfere(t *testing.T) {
	full, err := New(nil, WithDialect(core.FullDialect()))
	require.NoError(t, err)
	defer full.Close()

	empty, err := New(nil, WithDialect(core.EmptyDialect().Add("if", "if")))
	require.NoError(t, err)
	defer empty.Close()

	got, err := full.Eval(context.Background(), "full", "(do (def x 1) x)")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 1}.Equals(got))

	_, err = empty.Eval(context.Background(), "empty", "(def x 1)")
	require.Error(t, err, "def must be uncallable on the empty-base engine")
	assert.Contains(t, err.Error(), "undefined")

	got, err = empty.Eval(context.Background(), "empty", "(if true 7 8)")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 7}.Equals(got))
}

func TestDialect_EmptyBaseRejectsUnlistedKernelForm(t *testing.T) {
	e, err := New(nil, WithDialect(core.EmptyDialect().Add("if", "if")))
	require.NoError(t, err)
	defer e.Close()

	got, err := e.Eval(context.Background(), "empty", "(if false 1 2)")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 2}.Equals(got))

	_, err = e.Eval(context.Background(), "empty", "(let [a 1] a)")
	require.Error(t, err, "let is absent from the delta and must be undefined")
	assert.Contains(t, err.Error(), "undefined")
}

func TestDialect_RenameResolvesToCanonicalForm(t *testing.T) {
	e, err := New(nil, WithDialect(core.FullDialect().Rename("if", "si")))
	require.NoError(t, err)
	defer e.Close()

	got, err := e.Eval(context.Background(), "si", "(si true 1 2)")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 1}.Equals(got))

	_, err = e.Eval(context.Background(), "si", "(if true 1 2)")
	require.Error(t, err, "the original name must not resolve after a rename")
}

func TestDialect_RemoveMakesFormUncallable(t *testing.T) {
	e, err := New(nil, WithDialect(core.FullDialect().Remove("def")))
	require.NoError(t, err)
	defer e.Close()

	_, err = e.Eval(context.Background(), "removed", "(def x 1)")
	require.Error(t, err, "removed form must be undefined")
	assert.Contains(t, err.Error(), "undefined")

	got, err := e.Eval(context.Background(), "removed", "(if true 1 2)")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 1}.Equals(got))
}

func TestDialect_BytecodeRejectsNonIdentity(t *testing.T) {
	_, err := New(nil, WithBytecode(), WithDialect(core.EmptyDialect().Add("if", "if")))
	require.Error(t, err, "bytecode + non-identity dialect must be rejected at construction")

	e, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err, "bytecode + Clojure (identity) dialect is allowed")
	e.Close()
}

func TestDialect_NewSurfacesResolutionError(t *testing.T) {
	_, err := New(nil, WithDialect(core.EmptyDialect().Add("x", "no-such-form")))
	require.Error(t, err, "an unresolvable dialect must fail construction")
}

func TestDialect_EvaluatedCodeCannotChangeDialect(t *testing.T) {
	e, err := New(nil, WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	defer e.Close()

	// Binding a var named after a special form must not shadow dispatch.
	got, err := e.Eval(context.Background(), "immutable", "(do (def if 99) (if true 1 2))")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 1}.Equals(got), "special-form dispatch must win over the env binding")
}
