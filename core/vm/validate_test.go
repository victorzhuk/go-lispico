package vm_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/core/compiler"
	"github.com/victorzhuk/go-lispico/core/vm"
)

func TestChunkValidate_RejectsMalformed(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		chunk *vm.Chunk
	}{
		{
			name: "OpConst out-of-range constant index",
			chunk: &vm.Chunk{
				Code: []vm.Instruction{vm.Encode(vm.OpConst, 0)},
			},
		},
		{
			name: "OpGetGlobal out-of-range constant index",
			chunk: &vm.Chunk{
				Code: []vm.Instruction{vm.Encode(vm.OpGetGlobal, 0)},
			},
		},
		{
			name: "OpGetGlobal non-symbol constant",
			chunk: &vm.Chunk{
				Code:      []vm.Instruction{vm.Encode(vm.OpGetGlobal, 0)},
				Constants: []core.Value{core.Int{V: 1}},
			},
		},
		{
			name: "OpSetGlobal non-symbol constant",
			chunk: &vm.Chunk{
				Code:      []vm.Instruction{vm.Encode(vm.OpSetGlobal, 0)},
				Constants: []core.Value{core.Int{V: 1}},
			},
		},
		{
			name: "OpSetLexical non-symbol constant",
			chunk: &vm.Chunk{
				Code:      []vm.Instruction{vm.Encode(vm.OpSetLexical, 0)},
				Constants: []core.Value{core.Int{V: 1}},
			},
		},
		{
			name: "OpGetFunc non-symbol constant",
			chunk: &vm.Chunk{
				Code:      []vm.Instruction{vm.Encode(vm.OpGetFunc, 0)},
				Constants: []core.Value{core.Int{V: 1}},
			},
		},
		{
			name: "OpSetFunc non-symbol constant",
			chunk: &vm.Chunk{
				Code:      []vm.Instruction{vm.Encode(vm.OpSetFunc, 0)},
				Constants: []core.Value{core.Int{V: 1}},
			},
		},
		{
			name: "OpJump forward target out of range",
			chunk: &vm.Chunk{
				Code: []vm.Instruction{vm.Encode(vm.OpJump, 100)},
			},
		},
		{
			name: "OpJumpIfFalse forward target out of range",
			chunk: &vm.Chunk{
				Code: []vm.Instruction{vm.Encode(vm.OpJumpIfFalse, 100)},
			},
		},
		{
			name: "OpLoop absolute target out of range",
			chunk: &vm.Chunk{
				Code: []vm.Instruction{vm.Encode(vm.OpLoop, 50)},
			},
		},
		{
			name: "OpSetupTry handler target out of range",
			chunk: &vm.Chunk{
				Code: []vm.Instruction{vm.Encode(vm.OpSetupTry, 999), vm.Encode(vm.OpReturn, 0)},
			},
		},
		{
			name: "OpGetLocal slot exceeds MaxStack",
			chunk: &vm.Chunk{
				Code:     []vm.Instruction{vm.Encode(vm.OpGetLocal, 3), vm.Encode(vm.OpReturn, 0)},
				MaxStack: 2,
			},
		},
		{
			name: "OpSetLocal slot exceeds MaxStack",
			chunk: &vm.Chunk{
				Code:     []vm.Instruction{vm.Encode(vm.OpTrue, 0), vm.Encode(vm.OpSetLocal, 3), vm.Encode(vm.OpReturn, 0)},
				MaxStack: 2,
			},
		},
		{
			name: "OpClosure out-of-range sub-chunk index",
			chunk: &vm.Chunk{
				Code: []vm.Instruction{vm.Encode(vm.OpClosure, 0)},
			},
		},
		{
			name: "OpClosure recurses into invalid sub-chunk",
			chunk: &vm.Chunk{
				Code: []vm.Instruction{vm.Encode(vm.OpClosure, 0), vm.Encode(vm.OpReturn, 0)},
				SubChunks: []*vm.Chunk{
					{Code: []vm.Instruction{vm.Encode(vm.OpConst, 0)}},
				},
			},
		},
		{
			name: "falls off the end (non-terminal last instruction)",
			chunk: &vm.Chunk{
				Code:      []vm.Instruction{vm.Encode(vm.OpConst, 0)},
				Constants: []core.Value{core.Int{V: 1}},
			},
		},
		{
			name:  "empty code",
			chunk: &vm.Chunk{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.chunk.Validate()
			require.Error(t, err, "malformed chunk must error, not panic")
			var lerr *core.LispicoError
			require.ErrorAs(t, err, &lerr)
			assert.Equal(t, "BytecodeError", lerr.Code)
		})
	}
}

func TestChunkValidate_AcceptsWellFormed(t *testing.T) {
	t.Parallel()

	sub := &vm.Chunk{
		Arity:    1,
		Locals:   1,
		MaxStack: 1,
		Code:     []vm.Instruction{vm.Encode(vm.OpGetLocal, 0), vm.Encode(vm.OpReturn, 0)},
	}
	chunk := &vm.Chunk{
		Locals:    1,
		MaxStack:  2,
		SubChunks: []*vm.Chunk{sub},
		Constants: []core.Value{core.Symbol{V: "x"}, core.Int{V: 42}},
		Code: []vm.Instruction{
			vm.Encode(vm.OpConst, 1),
			vm.Encode(vm.OpSetLocal, 0),
			vm.Encode(vm.OpGetLocal, 0),
			vm.Encode(vm.OpJumpIfFalse, 1),
			vm.Encode(vm.OpJump, 1),
			vm.Encode(vm.OpSetGlobal, 0),
			vm.Encode(vm.OpSetLexical, 0),
			vm.Encode(vm.OpGetFunc, 0),
			vm.Encode(vm.OpSetFunc, 0),
			vm.Encode(vm.OpClosure, 0),
			vm.Encode(vm.OpLoop, 0),
			vm.Encode(vm.OpReturn, 0),
		},
	}

	if err := chunk.Validate(); err != nil {
		t.Fatalf("expected well-formed chunk to validate, got %v", err)
	}
}

// Validate must never reject bytecode the compiler itself produces — every
// construction path that reaches Run calls it first.
func TestChunkValidate_AcceptsCompilerOutput(t *testing.T) {
	t.Parallel()

	sources := []string{
		`(+ 1 2 3)`,
		`(if (< 1 2) "yes" "no")`,
		`(let [x 1 y 2] (+ x y))`,
		`(let* [x 1 y (+ x 1)] (* x y))`,
		`(def f (fn [n] (if (< n 2) n (+ (f (- n 1)) (f (- n 2))))))`,
		`(defn sum-to [n] (loop [i n acc 0] (if (= i 0) acc (recur (- i 1) (+ acc i)))))`,
		`(try (throw "boom") (catch e e))`,
		`(quasiquote (1 2 (unquote (+ 1 2))))`,
		`[1 2 3]`,
		`{:a 1 :b 2}`,
		`(when (> 1 0) 1 2 3)`,
		`(unless (> 0 1) 1 2 3)`,
		`(cond ((< 1 0) 1) ((> 1 0) 2) (else 3))`,
		`(and 1 2 3)`,
		`(or false nil 3)`,
	}

	for _, src := range sources {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			forms, err := core.Read(src)
			if err != nil {
				t.Fatalf("read %q: %v", src, err)
			}
			chunks, err := compiler.CompileAll(forms)
			if err != nil {
				t.Fatalf("compile %q: %v", src, err)
			}
			for _, c := range chunks {
				if err := c.Validate(); err != nil {
					t.Fatalf("compiler output for %q failed to validate: %v", src, err)
				}
			}
		})
	}
}
