package vm

import (
	"errors"
	"math"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

// requireResourceLimit asserts err is a *core.LispicoError with
// CodeResourceLimit.
func requireResourceLimit(t *testing.T, err error) *core.LispicoError {
	t.Helper()

	require.Error(t, err)
	var lerr *core.LispicoError
	require.True(t, errors.As(err, &lerr), "expected *core.LispicoError, got %T: %v", err, err)
	require.Equal(t, core.CodeResourceLimit, lerr.Code)
	return lerr
}

// TestVM_SetResourceLimits_CountersFailClosedAtInt64Ceiling pins the private
// counter paths at the int64 ceiling: an overflowing charge must refuse and
// pin the counter at math.MaxInt64, and every later positive charge must
// still refuse. Seeding uses only SetResourceLimits + chargeReductions /
// chargeAllocBytes — no direct field assignment.
func TestVM_SetResourceLimits_CountersFailClosedAtInt64Ceiling(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("int64 ceiling arithmetic is 64-bit specific; repo targets amd64")
	}

	const maxStr = "9223372036854775807"

	t.Run("reductions", func(t *testing.T) {
		vm := New(core.NewEnv(nil))
		vm.SetResourceLimits(math.MaxInt64, math.MaxInt64)
		require.Zero(t, vm.reductions)
		require.Zero(t, vm.allocBytes)

		// Exactly three positive charge attempts per counter: the
		// non-overflow seed, the overflowing charge, the post-refusal probe.
		require.NoError(t, vm.chargeReductions(math.MaxInt64-100))
		require.Positive(t, vm.reductions)

		lerr := requireResourceLimit(t, vm.chargeReductions(200))
		require.Equal(t, "reduction limit "+maxStr+" exceeded", lerr.Message)
		require.Equal(t, int64(math.MaxInt64), vm.reductions, "counter must be pinned at math.MaxInt64 after overflowing refusal")

		lerr = requireResourceLimit(t, vm.chargeReductions(1))
		require.Equal(t, "reduction limit "+maxStr+" exceeded", lerr.Message)
		require.Equal(t, int64(math.MaxInt64), vm.reductions, "counter must stay pinned after post-refusal charge")
		require.GreaterOrEqual(t, vm.reductions, int64(0), "counter must never go negative")
	})

	t.Run("allocBytes", func(t *testing.T) {
		vm := New(core.NewEnv(nil))
		vm.SetResourceLimits(math.MaxInt64, math.MaxInt64)
		require.Zero(t, vm.allocBytes)

		require.NoError(t, vm.chargeAllocBytes(math.MaxInt64 - 100))
		require.Positive(t, vm.allocBytes)

		lerr := requireResourceLimit(t, vm.chargeAllocBytes(200))
		require.Equal(t, "allocation limit "+maxStr+" bytes exceeded", lerr.Message)
		require.Equal(t, int64(math.MaxInt64), vm.allocBytes, "counter must be pinned at math.MaxInt64 after overflowing refusal")

		lerr = requireResourceLimit(t, vm.chargeAllocBytes(1))
		require.Equal(t, "allocation limit "+maxStr+" bytes exceeded", lerr.Message)
		require.Equal(t, int64(math.MaxInt64), vm.allocBytes, "counter must stay pinned after post-refusal charge")
		require.GreaterOrEqual(t, vm.allocBytes, int64(0), "counter must never go negative")
	})
}
