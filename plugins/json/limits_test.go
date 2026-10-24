package json

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

func evalErrUnder(t *testing.T, ev core.Evaluator, code string) error {
	t.Helper()
	env := setupEnv(t)
	forms, err := core.Read(code)
	require.NoError(t, err)
	_, err = ev.Eval(context.Background(), forms[0], env)
	return err
}

func evalValueUnder(t *testing.T, ev core.Evaluator, code string) core.Value {
	t.Helper()
	env := setupEnv(t)
	forms, err := core.Read(code)
	require.NoError(t, err)
	res, err := ev.Eval(context.Background(), forms[0], env)
	require.NoError(t, err)
	return res
}

func requireJSONResourceLimit(t *testing.T, err error) {
	t.Helper()
	var lerr *core.LispicoError
	require.ErrorAs(t, err, &lerr)
	require.Equal(t, core.CodeResourceLimit, lerr.Code)
}

func jsonIntArray(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = itoa(i)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func itoa(i int) string {
	return string(rune('0' + i%10))
}

func jsonObject(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = `\"k` + itoa(i) + `\":` + itoa(i)
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func TestJSONDecode_ArrayAtCollectionLimit(t *testing.T) {
	ev := core.NewEvaluator()
	ev.MaxCollectionLen = 3
	res := evalValueUnder(t, ev, `(json/decode "`+jsonIntArray(3)+`")`)
	v, ok := res.(core.Vector)
	require.True(t, ok)
	require.Equal(t, 3, v.Len())
}

func TestJSONDecode_ArrayOverCollectionLimit(t *testing.T) {
	ev := core.NewEvaluator()
	ev.MaxCollectionLen = 3
	err := evalErrUnder(t, ev, `(json/decode "`+jsonIntArray(4)+`")`)
	requireJSONResourceLimit(t, err)
}

func TestJSONDecode_ObjectAtAndOverCollectionLimit(t *testing.T) {
	ev := core.NewEvaluator()
	ev.MaxCollectionLen = 3
	res := evalValueUnder(t, ev, `(json/decode "`+jsonObject(3)+`")`)
	m, ok := res.(*core.HashMap)
	require.True(t, ok)
	require.Equal(t, 3, m.Len())

	err := evalErrUnder(t, ev, `(json/decode "`+jsonObject(4)+`")`)
	requireJSONResourceLimit(t, err)
}

func TestJSONDecode_NestedContainersChecked(t *testing.T) {
	ev := core.NewEvaluator()
	ev.MaxCollectionLen = 3

	err := evalErrUnder(t, ev, `(json/decode "{\"a\": `+jsonIntArray(4)+`}")`)
	requireJSONResourceLimit(t, err)

	err = evalErrUnder(t, ev, `(json/decode "[`+jsonIntArray(4)+`]")`)
	requireJSONResourceLimit(t, err)

	err = evalErrUnder(t, ev, `(json/decode "{\"b\": `+strings.ReplaceAll(jsonObject(4), "k", "a")+`}")`)
	requireJSONResourceLimit(t, err)

	res := evalValueUnder(t, ev, `(json/decode "{\"a\": `+jsonIntArray(3)+`}")`)
	m, ok := res.(*core.HashMap)
	require.True(t, ok)
	require.Equal(t, 1, m.Len())
}

func TestJSONDecode_StringScalarNotLimited(t *testing.T) {
	ev := core.NewEvaluator()
	ev.MaxCollectionLen = 3
	long := strings.Repeat("x", 500)
	res := evalValueUnder(t, ev, `(json/decode "\"`+long+`\"")`)
	s, ok := res.(core.String)
	require.True(t, ok)
	require.Equal(t, long, s.V)
}

func TestJSONDecode_NoLimiterIsUnlimited(t *testing.T) {
	ev := core.NewEvaluator()
	res := evalValueUnder(t, ev, `(json/decode "`+jsonIntArray(4)+`")`)
	v, ok := res.(core.Vector)
	require.True(t, ok)
	require.Equal(t, 4, v.Len())
}

func TestJSONDecode_RefusalPublishesNoResult(t *testing.T) {
	ev := core.NewEvaluator()
	ev.MaxCollectionLen = 3
	env := setupEnv(t)
	forms, err := core.Read(`(json/decode "` + jsonIntArray(4) + `")`)
	require.NoError(t, err)
	res, err := ev.Eval(context.Background(), forms[0], env)
	require.Error(t, err)
	require.Nil(t, res)
}
