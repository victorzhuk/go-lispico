package stdlib

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/runtime"
)

func TestCollectionConstructors_CopyArgsOnBytecodePath(t *testing.T) {
	eng, err := runtime.New(nil, runtime.WithBytecode(), runtime.WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	defer eng.Close()
	require.NoError(t, eng.Use(New()))

	ctx := context.Background()
	tests := []struct {
		name string
		src  string
		want core.Value
	}{
		{name: "vector first stable", src: "((fn [ks] (first ks)) (vector :a :b))", want: core.Keyword{V: "a"}},
		{name: "list first stable", src: "((fn [ks] (first ks)) (list :a :b))", want: core.Keyword{V: "a"}},
		{name: "get-in via vector path", src: "(get-in (hash-map :a (hash-map :b 7)) (vector :a :b))", want: core.Int{V: 7}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := eng.Eval(ctx, tt.name, tt.src)
			require.NoError(t, err)
			assert.True(t, tt.want.Equals(got), "got %v", got)
		})
	}
}
