package stdlib

import (
	"errors"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

// Each helper returns *core.LispicoError directly; an error-interface return
// would admit a non-LispicoError value at the boundary.
var (
	_ func(string, ...any) *core.LispicoError = arityErrorf
	_ func(string, ...any) *core.LispicoError = typeErrorf
	_ func(string, ...any) *core.LispicoError = domainErrorf
	_ func(string, error) *core.LispicoError  = wrapCause
)

func mustError(t *testing.T, build func() *core.LispicoError) *core.LispicoError {
	t.Helper()
	var (
		got *core.LispicoError
		rec any
	)
	func() {
		defer func() { rec = recover() }()
		got = build()
	}()
	if rec != nil {
		t.Fatalf("helper panicked instead of returning an error: %v", rec)
	}
	require.NotNil(t, got)
	return got
}

func TestErrorHelperCodes(t *testing.T) {
	tests := []struct {
		name  string
		build func() *core.LispicoError
		code  string
		msg   string
	}{
		{
			name:  "arity exact",
			build: func() *core.LispicoError { return arityErrorf("first: requires 1 argument") },
			code:  "ArityError",
			msg:   "first: requires 1 argument",
		},
		{
			name:  "arity alternatives",
			build: func() *core.LispicoError { return arityErrorf("nth: requires 2 or 3 arguments") },
			code:  "ArityError",
			msg:   "nth: requires 2 or 3 arguments",
		},
		{
			name:  "arity ranged",
			build: func() *core.LispicoError { return arityErrorf("range: requires %d to %d arguments", 1, 3) },
			code:  "ArityError",
			msg:   "range: requires 1 to 3 arguments",
		},
		{
			name:  "arity variadic",
			build: func() *core.LispicoError { return arityErrorf("str: requires at least %d argument", 1) },
			code:  "ArityError",
			msg:   "str: requires at least 1 argument",
		},
		{
			name: "arity parity",
			build: func() *core.LispicoError {
				return arityErrorf("hash-map: requires an even number of arguments, got %d", 3)
			},
			code: "ArityError",
			msg:  "hash-map: requires an even number of arguments, got 3",
		},
		{
			name:  "type",
			build: func() *core.LispicoError { return typeErrorf("first: expected collection, got %T", core.Int{V: 1}) },
			code:  "TypeError",
			msg:   "first: expected collection, got core.Int",
		},
		{
			name:  "domain",
			build: func() *core.LispicoError { return domainErrorf("/: division by zero") },
			code:  "EvalError",
			msg:   "/: division by zero",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mustError(t, tt.build)
			require.Equal(t, tt.code, got.Code)
			require.Equal(t, tt.msg, got.Message)
			require.Nil(t, got.Cause)
		})
	}
}

func TestWrapCauseNamesOperationAndSetsCause(t *testing.T) {
	_, cause := strconv.Atoi("not-a-number")
	require.Error(t, cause)

	got := mustError(t, func() *core.LispicoError { return wrapCause("string->int", cause) })

	require.Equal(t, "EvalError", got.Code)
	require.Same(t, cause, got.Cause)
	require.Contains(t, got.Message, "string->int")
}

func TestWrapCauseReachesCauseWithErrorsIs(t *testing.T) {
	_, cause := strconv.Atoi("not-a-number")

	got := mustError(t, func() *core.LispicoError { return wrapCause("string->int", cause) })

	require.ErrorIs(t, got, strconv.ErrSyntax)
}

func TestWrapCauseAsResolvesToOuterError(t *testing.T) {
	_, cause := strconv.Atoi("not-a-number")

	got := mustError(t, func() *core.LispicoError { return wrapCause("string->int", cause) })

	var le *core.LispicoError
	require.True(t, errors.As(error(got), &le))
	require.Same(t, got, le)
	require.Equal(t, "EvalError", le.Code)
}

func TestWrapCauseDoesNotDoubleWrap(t *testing.T) {
	inner := core.NewTypeError("collection", core.Int{V: 1})

	got := mustError(t, func() *core.LispicoError { return wrapCause("string->int", inner) })

	require.Same(t, inner, got)
	require.Equal(t, "TypeError", got.Code)
	require.Nil(t, got.Cause)
}
