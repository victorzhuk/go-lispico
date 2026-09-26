package cl_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/cl"
	"github.com/victorzhuk/go-lispico/core"
)

func noopAdapter(name string) core.Value {
	return core.GoFunc{
		Name: name,
		Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
			return core.Nil{}, nil
		},
	}
}

func TestCL_StockMatchesSpecFingerprint(t *testing.T) {
	want, err := core.NewDialect(core.DialectSpec{
		Lisp2:        true,
		NoBrackets:   true,
		FunctionRef:  true,
		ReaderVector: true,
		Forms: map[string]string{
			"defun": "defn",
			"setq":  "set!",
			"progn": "do",
		},
		Hide: []string{"set!", "do"},
		Vocab: map[string]string{
			"car":     "first",
			"cdr":     "rest",
			"null":    "nil?",
			"cons":    "cons",
			"list":    "list",
			"append":  "concat",
			"length":  "count",
			"reverse": "reverse",
			"apply":   "apply",
			"type":    "type",
		},
		Adapters: map[string]core.Adapter{
			"nth":    {ID: "cl/nth@1", Value: noopAdapter("nth")},
			"mapcar": {ID: "cl/mapcar@1", Value: noopAdapter("mapcar")},
			"sort":   {ID: "cl/sort@1", Value: noopAdapter("sort")},
		},
	})
	require.NoError(t, err)

	assert.Equal(t, want.Fingerprint(), cl.Dialect().Fingerprint(),
		"stock cl dialect fingerprint must equal the fingerprint of its equivalent DialectSpec")

	d := cl.Dialect()
	allocs := testing.AllocsPerRun(100, func() { _ = d.Fingerprint() })
	assert.Zero(t, allocs, "cl.Dialect().Fingerprint() must not allocate")
}
