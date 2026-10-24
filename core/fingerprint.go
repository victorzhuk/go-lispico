package core

import "math"

// fingerprintMaxDepth bounds structural recursion. Equals itself stops
// matching beyond DefaultMaxStructuralDepth, so structures deeper than the
// cap are never equal to anything: a shared truncation marker for them cannot
// break the invariant that equal values share a fingerprint.
const fingerprintMaxDepth = 64

// Fingerprinter lets value types outside core join constant interning. The
// returned fingerprint must be equal for any two values this type's Equals
// reports equal. Collisions are allowed: Equals stays authoritative.
type Fingerprinter interface {
	ConstantFingerprint() uint64
}

// Fingerprint returns a structural fingerprint of v for use as an index key
// with constant pools. Values that report equal via Equals always share a
// fingerprint; the converse does not hold, so callers must resolve collisions
// with Equals. Floats hash by bits with -0 normalized to +0 (matching Float
// equality) while NaN keeps its bits (NaN never equals anything, including
// itself, so distinct NaNs may share a bucket only to be split by Equals).
// ok is false for values with no safe structural fingerprint (lambdas,
// macros, host values); those need a linear scan.
func Fingerprint(v Value) (uint64, bool) {
	fp := &fingerprintState{
		memo:   make(map[fingerprintMemoKey]fingerprintMemoResult),
		active: make(map[*HashMap]bool),
	}
	return fp.fingerprint(v, 0)
}

// fingerprintMemoKey identifies a HashMap fingerprint result. Remaining depth
// is part of the key because truncation depends on it: the same map reached
// at two depths can fold to different fingerprints.
type fingerprintMemoKey struct {
	m     *HashMap
	depth int
}

// fingerprintMemoResult keeps the supported flag with the hash: a map whose
// subtree contains an unsupported value must also fold once, or a shared DAG
// ending in a lambda still costs one walk per path.
type fingerprintMemoResult struct {
	h  uint64
	ok bool
}

// fingerprintState carries per-Fingerprint-invocation memoization so shared
// HashMap DAG nodes are folded once per remaining-depth state instead of once
// per path, and detects self-referential maps by tracking the recursion stack.
type fingerprintState struct {
	memo   map[fingerprintMemoKey]fingerprintMemoResult
	active map[*HashMap]bool
}

func (fp *fingerprintState) fingerprint(v Value, depth int) (uint64, bool) {
	if depth > fingerprintMaxDepth {
		return 0x9E3779B97F4A7C15, true
	}
	switch x := v.(type) {
	case nil:
		return 1, true
	case Nil:
		return 2, true
	case Bool:
		if x.V {
			return 3, true
		}
		return 4, true
	case Int:
		return mix(5, uint64(x.V)), true
	case Float:
		bits := math.Float64bits(x.V)
		if x.V == 0 {
			bits = 0
		}
		return mix(6, bits), true
	case String:
		return mix(7, hashString(x.V)), true
	case Symbol:
		return mix(8, hashString(x.V)), true
	case Keyword:
		return mix(9, hashString(x.V)), true
	case GoFunc:
		return mix(10, hashString(x.Name)), true
	case List:
		h := uint64(11)
		ok := true
		x.each(func(item Value) bool {
			f, itemOK := fp.fingerprint(item, depth+1)
			h = mix(h, f)
			ok = ok && itemOK
			return true
		})
		return h, ok
	case Vector:
		h := uint64(12)
		ok := true
		for i := range x.Len() {
			f, itemOK := fp.fingerprint(x.At(i), depth+1)
			h = mix(h, f)
			ok = ok && itemOK
		}
		return h, ok
	case *HashMap:
		if x == nil {
			return 13, true
		}
		key := fingerprintMemoKey{m: x, depth: depth}
		if r, hit := fp.memo[key]; hit {
			return r.h, r.ok
		}
		if fp.active[x] {
			// Cycle: structural equality never terminates on such a value
			// either, so no safe fingerprint exists. Report unsupported.
			return 0, false
		}
		fp.active[x] = true
		h := mix(14, uint64(x.Len()))
		ok := true
		x.eachRaw(func(e entry) {
			vf, valOK := fp.fingerprint(e.v, depth+1)
			h ^= mix(mix(15, fingerprintHashKey(e.hk)), vf)
			ok = ok && valOK
		})
		delete(fp.active, x)
		fp.memo[key] = fingerprintMemoResult{h: h, ok: ok}
		return h, ok
	case Fingerprinter:
		return x.ConstantFingerprint(), true
	default:
		return 0, false
	}
}

// fingerprintHashKey hashes the canonical key form HashMap entries are looked
// up by, so equal maps (same hashKey multiset) fold to the same value.
func fingerprintHashKey(hk hashKey) uint64 {
	return mix(uint64(hk.typ), mix(hk.num, hashString(hk.str)))
}

func mix(h, x uint64) uint64 {
	h ^= x
	h *= 0x9E3779B97F4A7C15
	h ^= h >> 29
	return h
}

func hashString(s string) uint64 {
	h := uint64(0xCBF29CE484222325)
	for i := range len(s) {
		h ^= uint64(s[i])
		h *= 0x100000001B3
	}
	return h
}
