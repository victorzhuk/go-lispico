package stdlib

import (
	"fmt"
	"testing"

	"github.com/victorzhuk/go-lispico/core"
)

// TestCollections_ConsDepthEscalationStillCaught guards the narrowing that
// makes extension linear. cons and conj check only the newly added element
// rather than re-walking what they extend, on the grounds that the extended
// collection was checked when it was built. The shape that must still fail is
// the one where the new element IS the accumulator, so each step nests one
// level deeper — there, checking the element is exactly checking the result.
func TestCollections_ConsDepthEscalationStillCaught(t *testing.T) {
	for _, tc := range []struct{ name, expr string }{
		{"cons wraps accumulator", `(cons acc (quote ()))`},
		{"conj wraps accumulator", `(conj [] acc)`},
		{"cons wraps accumulator onto nil", `(cons acc nil)`},
		{"conj onto nil", `(conj nil acc)`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := setupEnv(t)
			src := fmt.Sprintf(`(loop [i 0 acc nil] (if (< i %d) (recur (+ i 1) %s) acc))`,
				core.DefaultMaxStructuralDepth+2, tc.expr)
			requireResourceLimit(t, evalErr(t, env, src))
		})
	}
}
