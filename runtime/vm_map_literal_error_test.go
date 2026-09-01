// Engine-level golden for the compiled map-literal path. A literal unhashable
// key never reaches the VM: Parser.parseHashMap builds the HashMap while
// reading, so `{[1] 2}` fails at read time. The VM site is reachable only when
// a key EXPRESSION evaluates to an unhashable value — the compiler cannot fold
// a symbol key into a chunk constant, so it emits OpMakeMap and the rejection
// happens at run time.
package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
)

// mapLiteralUnhashableSrc binds a vector to k so the map literal's key is an
// expression rather than a value the reader already hashed.
const mapLiteralUnhashableSrc = `(let [k [1 2]] {k 3})`

// TestVMMapLiteral_UnhashableKeyIsTypedEvalError pins that OpMakeMap's
// unhashable-key rejection reaches an embedder as a typed, catchable error
// carrying the original rejection on Cause.
func TestVMMapLiteral_UnhashableKeyIsTypedEvalError(t *testing.T) {
	eng := loadStdlibEngine(t, clojure.Dialect(), true, WithBytecode())

	val, err := eng.Eval(context.Background(), "vm-map-literal-golden", mapLiteralUnhashableSrc)
	require.Error(t, err, "%s must fail, got %v", mapLiteralUnhashableSrc, val)

	var le *core.LispicoError
	require.ErrorAs(t, err, &le,
		"an unhashable map-literal key must reach the host as *core.LispicoError, got %T: %v", err, err)
	assert.Equal(t, "EvalError", le.Code)

	// Only core/vm's OpMakeMap builds this prefix, so it also witnesses that the
	// compiled path ran rather than a tree-walker fallback.
	assert.True(t, strings.HasPrefix(le.Message, "map literal: "),
		"want the compiled map-literal message, got %q", le.Message)

	wantCause := core.NewHashMap().Set(
		core.NewVector([]core.Value{core.Int{V: 1}, core.Int{V: 2}}),
		core.Int{V: 3},
	)
	require.Error(t, wantCause, "the key under test must be unhashable for the map itself")

	cause := errors.Unwrap(le)
	require.NotNil(t, cause, "the typed error must keep the original rejection on Cause")
	assert.Equal(t, wantCause.Error(), cause.Error())
	assert.True(t, errors.Is(err, cause), "the cause must stay reachable through the wrap chain")

	assert.False(t, core.IsTerminalEvalError(err),
		"an unhashable key is a domain failure and must stay catchable")
}
