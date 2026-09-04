package vm

import (
	"math"
	"testing"
)

func BenchmarkVMChargeCounters(b *testing.B) {
	b.Run("successful", func(b *testing.B) {
		vm := New(nil)
		vm.SetResourceLimits(math.MaxInt64, math.MaxInt64)
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			if err := vm.chargeReductions(1); err != nil {
				b.Fatal(err)
			}
			if err := vm.chargeAllocBytes(1); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("overflow-refusal", func(b *testing.B) {
		vm := New(nil)
		vm.SetResourceLimits(math.MaxInt64, math.MaxInt64)
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			_ = vm.chargeReductions(math.MaxInt64)
			_ = vm.chargeAllocBytes(math.MaxInt64)
		}
	})
}
