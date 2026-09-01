package stdlib

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

// Every map builtin that hands a caller-supplied key to core.HashMap must wrap
// the unhashable-key failure, so the host sees a typed, catchable error instead
// of core's bare fmt error. conj shipped that error bare while its siblings
// wrapped; these rows pin all four reachable boundaries against that drift.
func TestMapBuiltins_UnhashableKeyIsWrappedEvalError(t *testing.T) {
	env := setupEnv(t)

	key := core.NewList([]core.Value{core.Int{V: 1}, core.Int{V: 2}})
	one := core.Int{V: 1}

	// Source of truth for the cause, taken from core directly so the assertion
	// does not read it back out of the wrapper it is checking.
	_, _, wantCause := core.NewHashMap().Assoc(key, one)
	require.Error(t, wantCause, "core.HashMap must reject a List key")

	m := core.NewHashMap()
	require.NoError(t, m.Set(core.Keyword{V: "a"}, one))

	rows := []struct {
		name    string
		builtin string
		op      string
		args    []core.Value
	}{
		{"hash-map key", "hash-map", "hash-map", []core.Value{key, one}},
		{"conj key", "conj", "conj", []core.Value{m, key, one}},
		{"assoc key", "assoc", "assoc", []core.Value{m, key, one}},
		{"dissoc key", "dissoc", "dissoc", []core.Value{m, key}},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			err := builtinErr(t, env, row.builtin, row.args...)

			var le *core.LispicoError
			require.ErrorAs(t, err, &le)
			require.Equal(t, "EvalError", le.Code)
			require.Contains(t, le.Message, row.op)

			require.NotNil(t, le.Cause, "wrapCause sets Cause directly; losing it strands the host with no cause")
			require.Equal(t, le.Cause, errors.Unwrap(le), "Unwrap is the only edge errors.Is/As traverse")
			require.Equal(t, wantCause.Error(), le.Cause.Error(), "cause must be what core.HashMap returned")

			require.False(t, core.IsTerminalEvalError(err), "an unhashable key is a domain failure and must stay catchable")
		})
	}
}
