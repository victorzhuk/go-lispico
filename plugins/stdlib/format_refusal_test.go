package stdlib

import (
	"fmt"
	"strings"
	"testing"

	"github.com/victorzhuk/go-lispico/core"
)

// TestStrings_FormatRefusalCases pins the estimator's parity with fmt when an
// explicit argument index is refused. Each case derives its expected render
// from fmt.Sprintf at runtime and asserts the estimate covers it; the args
// are arranged so a mis-tracked cursor (restoring argument 0 after a refused
// index) undercharges the render and fails the invariant.
func TestStrings_FormatRefusalCases(t *testing.T) {
	long := strings.Repeat("B", 64)
	cases := []struct {
		name   string
		format string
		args   []any
	}{
		{
			// fmt keeps the refusal through the precision parse and the
			// trailing %s binds argument 0.
			name:   "refused index with precision",
			format: "%[18446744073709551618].2s%s",
			args:   []any{"ABCDEF", "Z"},
		},
		{
			// fmt consumes the dynamic width argument even though the index
			// is refused, so the trailing %s binds the next argument; a
			// long string there makes a restored arg-0 cursor undercharge.
			name:   "refused index with dynamic width",
			format: "%[18446744073709551618]*s%s",
			args:   []any{2, long, "C"},
		},
		{
			// A lone refused directive renders %!(verb)(BADINDEX): the verb
			// byte is part of the charged field.
			name:   "lone refused directive",
			format: "%[18446744073709551618]s",
			args:   []any{"A"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			render := fmt.Sprintf(tc.format, tc.args...)
			args := make([]core.Value, 0, len(tc.args))
			for _, a := range tc.args {
				switch a := a.(type) {
				case int:
					args = append(args, core.Int{V: int64(a)})
				case string:
					args = append(args, core.String{V: a})
				default:
					t.Fatalf("unhandled arg type %T", a)
				}
			}
			estimate := estimateFormatAllocBytes(tc.format, args)
			if estimate < int64(len(render)) {
				t.Fatalf("estimate %d < render %d (%q)", estimate, len(render), render)
			}
		})
	}
}
