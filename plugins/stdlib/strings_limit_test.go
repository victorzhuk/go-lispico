package stdlib

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

func evalValueUnder(t *testing.T, ev core.Evaluator, code string) core.Value {
	t.Helper()
	forms, err := core.Read(code)
	require.NoError(t, err)
	res, err := ev.Eval(context.Background(), forms[0], setupEnv(t))
	require.NoError(t, err)
	return res
}

func TestStringSplit_AtCollectionLimit(t *testing.T) {
	ev := core.NewEvaluator()
	ev.MaxCollectionLen = 3
	res := evalValueUnder(t, ev, `(string/split "a,b,c" ",")`)
	l, ok := res.(core.List)
	require.True(t, ok)
	require.Equal(t, 3, l.Len())
}

func TestStringSplit_OverCollectionLimit(t *testing.T) {
	ev := core.NewEvaluator()
	ev.MaxCollectionLen = 3
	err := evalErrUnder(t, ev, setupEnv(t), `(string/split "a,b,c,d" ",")`)
	requireResourceLimit(t, err)
	require.ErrorContains(t, err, "string/split length 4 exceeds collection limit 3")
}

func TestStringLines_AtAndOverCollectionLimit(t *testing.T) {
	ev := core.NewEvaluator()
	ev.MaxCollectionLen = 3
	res := evalValueUnder(t, ev, `(string/lines "a\nb\nc")`)
	l, ok := res.(core.List)
	require.True(t, ok)
	require.Equal(t, 3, l.Len())

	err := evalErrUnder(t, ev, setupEnv(t), `(string/lines "a\nb\nc\nd")`)
	requireResourceLimit(t, err)
	require.ErrorContains(t, err, "string/lines length 4 exceeds collection limit 3")
}

func TestStringSplit_EmptySeparatorCountsRunes(t *testing.T) {
	ev := core.NewEvaluator()
	ev.MaxCollectionLen = 3
	res := evalValueUnder(t, ev, `(string/split "abc" "")`)
	l, ok := res.(core.List)
	require.True(t, ok)
	require.Equal(t, 3, l.Len())

	err := evalErrUnder(t, ev, setupEnv(t), `(string/split "abcd" "")`)
	requireResourceLimit(t, err)
}

func TestStringSplit_ScalarResultNotLimited(t *testing.T) {
	ev := core.NewEvaluator()
	ev.MaxCollectionLen = 2
	long := strings.Repeat("x", 1000)
	res := evalValueUnder(t, ev, `(string/split "`+long+`" ",")`)
	l, ok := res.(core.List)
	require.True(t, ok)
	require.Equal(t, 1, l.Len())
}

func TestStringSplit_RefusalPublishesNoResult(t *testing.T) {
	ev := core.NewEvaluator()
	ev.MaxCollectionLen = 1
	forms, err := core.Read(`(string/split "a,b" ",")`)
	require.NoError(t, err)
	res, err := ev.Eval(context.Background(), forms[0], setupEnv(t))
	require.Error(t, err)
	require.Nil(t, res)
}
