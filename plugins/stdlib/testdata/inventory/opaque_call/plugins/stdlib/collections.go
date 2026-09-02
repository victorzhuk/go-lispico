package stdlib

import (
	"strings"

	"github.com/victorzhuk/go-lispico/core"
)

// replaceAll reaches strings.ReplaceAll, whose cost scales with the input, while
// its only row carries a disposition that accounts for nothing.
func replaceAll(args []core.Value) (core.Value, error) {
	s, _ := args[0].(core.String)
	return core.String(strings.ReplaceAll(string(s), "a", "b")), nil
}
