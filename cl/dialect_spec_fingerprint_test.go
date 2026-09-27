package cl_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/cl"
	"github.com/victorzhuk/go-lispico/core"
)

func TestCL_StockMatchesSpecFingerprint(t *testing.T) {
	want, err := core.NewDialect(cl.CLSpec())
	require.NoError(t, err)

	assert.Equal(t, want.Fingerprint(), cl.Dialect().Fingerprint(),
		"stock cl dialect fingerprint must equal the fingerprint of its equivalent DialectSpec")

	d := cl.Dialect()
	allocs := testing.AllocsPerRun(100, func() { _ = d.Fingerprint() })
	assert.Zero(t, allocs, "cl.Dialect().Fingerprint() must not allocate")
}
