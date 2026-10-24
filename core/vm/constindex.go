package vm

import "github.com/victorzhuk/go-lispico/core"

// constIndex buckets a chunk's constant pool by structural fingerprint so
// AddConstant finds interned candidates without scanning the whole pool.
// Equals stays authoritative inside a bucket. slow holds the indices of
// values with no safe fingerprint (lambdas, macros, host values); those keep
// the original linear-scan behavior, and fingerprintable lookups still scan
// them so a host value equal to a core value cannot duplicate. Entries are
// appended as constants are admitted; admitted constants are never mutated.
type constIndex struct {
	buckets map[uint64][]int
	slow    []int
}

// ensureConstIndex lazily builds the index from a pool that may have been
// populated before the first AddConstant.
func (c *Chunk) ensureConstIndex() {
	if c.constIdx.buckets != nil {
		return
	}
	c.constIdx.buckets = make(map[uint64][]int)
	for i, existing := range c.Constants {
		c.indexConstant(i, existing)
	}
}

func (c *Chunk) indexConstant(i int, v core.Value) {
	if fp, ok := core.Fingerprint(v); ok {
		c.constIdx.buckets[fp] = append(c.constIdx.buckets[fp], i)
	} else {
		c.constIdx.slow = append(c.constIdx.slow, i)
	}
}
