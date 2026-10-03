package compiler

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/cl"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/core/vm"
)

func ifForm() core.Value {
	return core.NewList([]core.Value{
		core.Symbol{V: "if"},
		core.Bool{V: true},
		core.Int{V: 1},
		core.Int{V: 2},
	})
}

func runChunk(t *testing.T, chunk *vm.Chunk) core.Value {
	t.Helper()
	v := vm.New(core.NewEnv(nil))
	res, err := v.Run(context.Background(), chunk)
	require.NoError(t, err)
	return res
}

func chunkOps(chunk *vm.Chunk) []vm.Opcode {
	ops := make([]vm.Opcode, 0, len(chunk.Code))
	for _, instr := range chunk.Code {
		ops = append(ops, instr.Op())
	}
	return ops
}

// TestCompiler_ZeroValueDialect pins the zero-value dialect contract: the
// constructor takes core.Dialect by value, never panics on the zero value,
// and the compiled chunk behaves as the identity dialect.
func TestCompiler_ZeroValueDialect(t *testing.T) {
	var c *Compiler
	require.NotPanics(t, func() {
		c = NewCompilerWithDialect("test", core.Dialect{})
	})
	require.NotNil(t, c)

	require.NoError(t, c.Compile(ifForm()))
	require.NoError(t, c.EmitReturn())

	res := runChunk(t, c.Chunk())
	require.True(t, core.Int{V: 1}.Equals(res), "expected 1, got %s", res.String())
}

// TestCompiler_DefaultConstructorEquivalent pins that NewCompiler is exactly
// NewCompilerWithDialect(name, core.Dialect{}): same compiled code, same
// constants, same runtime result.
func TestCompiler_DefaultConstructorEquivalent(t *testing.T) {
	a := NewCompiler("test")
	b := NewCompilerWithDialect("test", core.Dialect{})

	require.NoError(t, a.Compile(ifForm()))
	require.NoError(t, a.EmitReturn())
	require.NoError(t, b.Compile(ifForm()))
	require.NoError(t, b.EmitReturn())

	assert.Equal(t, chunkOps(a.Chunk()), chunkOps(b.Chunk()), "opcode sequence")
	assert.Equal(t, len(a.Chunk().Constants), len(b.Chunk().Constants), "constant count")

	for _, chunk := range []*vm.Chunk{a.Chunk(), b.Chunk()} {
		res := runChunk(t, chunk)
		require.True(t, core.Int{V: 1}.Equals(res), "expected 1, got %s", res.String())
	}
}

// TestCompiler_DialectNestedFunctions pins that a child compiler created for
// a nested fn keeps its parent's dialect: canonical-name resolution, hidden
// special forms, and the Lisp-2 function-cell head rule all survive nesting.
func TestCompiler_DialectNestedFunctions(t *testing.T) {
	cases := []struct {
		name    string
		dialect core.Dialect
		headOp  vm.Opcode
	}{
		{"cl", cl.Dialect(), vm.OpFreezeNativeFunc},
		{"clojure", clojure.Dialect(), vm.OpFreezeNative},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			params := func(names ...string) core.Value {
				syms := make([]core.Value, 0, len(names))
				for _, n := range names {
					syms = append(syms, core.Symbol{V: n})
				}
				if tc.name == "cl" {
					return core.NewList(syms)
				}
				return core.NewVector(syms)
			}
			call := func(head string, args ...string) core.Value {
				vals := []core.Value{core.Symbol{V: head}}
				for _, a := range args {
					vals = append(vals, core.Symbol{V: a})
				}
				return core.NewList(vals)
			}
			inner := core.NewList([]core.Value{
				core.Symbol{V: "fn"}, params("y"), call("+", "x", "y"),
			})
			outer := core.NewList([]core.Value{
				core.Symbol{V: "fn"}, params("x"), inner,
			})

			c := NewCompilerWithDialect("test", tc.dialect)
			require.NoError(t, c.Compile(outer))

			require.Len(t, c.chunk.SubChunks, 1, "outer fn sub-chunk")
			outerBody := c.chunk.SubChunks[0]
			require.Len(t, outerBody.SubChunks, 1, "inner fn sub-chunk")
			innerBody := outerBody.SubChunks[0]
			require.NotEmpty(t, innerBody.Code)
			assert.Equal(t, tc.headOp, innerBody.Code[0].Op(),
				"inner fn keeps the dialect's native-op head rule after nesting")
		})
	}

	t.Run("canonical names survive nesting under cl", func(t *testing.T) {
		// progn is the canonical do under CL: it compiles as the special
		// form, with no call to an unknown function head.
		form := core.NewList([]core.Value{
			core.Symbol{V: "fn"},
			core.NewList([]core.Value{}),
			core.NewList([]core.Value{
				core.Symbol{V: "progn"},
				core.Int{V: 1},
				core.Int{V: 2},
			}),
		})
		c := NewCompilerWithDialect("test", cl.Dialect())
		require.NoError(t, c.Compile(form))
		inner := c.chunk.SubChunks[0]
		for _, instr := range inner.Code {
			assert.NotEqual(t, vm.OpCall, instr.Op(), "progn compiles as do, not as a call")
		}
	})

	t.Run("hidden forms are not special under cl", func(t *testing.T) {
		// do is hidden under CL: the head must fall through to an ordinary
		// Lisp-2 function call, not the kernel do form.
		form := core.NewList([]core.Value{
			core.Symbol{V: "fn"},
			core.NewList([]core.Value{}),
			core.NewList([]core.Value{
				core.Symbol{V: "do"},
				core.Int{V: 1},
			}),
		})
		c := NewCompilerWithDialect("test", cl.Dialect())
		require.NoError(t, c.Compile(form))
		inner := c.chunk.SubChunks[0]
		hasCall := false
		for _, instr := range inner.Code {
			if instr.Op() == vm.OpCall {
				hasCall = true
				break
			}
		}
		assert.True(t, hasCall, "hidden do compiles as an ordinary call head")
	})

	t.Run("flat cond accepted under clojure", func(t *testing.T) {
		form := core.NewList([]core.Value{
			core.Symbol{V: "cond"},
			core.Bool{V: true},
			core.Int{V: 1},
			core.Keyword{V: "else"},
			core.Int{V: 2},
		})
		c := NewCompilerWithDialect("test", clojure.Dialect())
		require.NoError(t, c.Compile(form), "clojure cond is flat test/body pairs")
	})

	t.Run("flat cond selected inside nested fn under clojure", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			test core.Bool
			want core.Int
		}{
			{"falsy test takes else branch", core.Bool{V: false}, core.Int{V: 22}},
			{"truthy test takes first branch", core.Bool{V: true}, core.Int{V: 11}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				// cond normalization must survive the child compiler a
				// nested fn builds: the closure body still pairs flat
				// test/body clauses instead of calling an unknown head.
				cond := core.NewList([]core.Value{
					core.Symbol{V: "cond"},
					tc.test,
					core.Int{V: 11},
					core.Keyword{V: "else"},
					core.Int{V: 22},
				})
				fn := core.NewList([]core.Value{
					core.Symbol{V: "fn"},
					core.NewList([]core.Value{}),
					cond,
				})
				form := core.NewList([]core.Value{fn})
				c := NewCompilerWithDialect("test", clojure.Dialect())
				require.NoError(t, c.Compile(form))
				require.NoError(t, c.EmitReturn())
				res := runChunk(t, c.Chunk())
				require.True(t, tc.want.Equals(res), "expected %s, got %s", tc.want.String(), res.String())
			})
		}
	})
}
