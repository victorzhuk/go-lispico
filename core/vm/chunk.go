package vm

import (
	"fmt"

	"github.com/victorzhuk/go-lispico/core"
)

// Instruction is a single bytecode instruction: an Opcode packed with a
// 24-bit operand A.
type Instruction uint32

// Encode packs op and operand a into an Instruction. a is truncated to 24 bits.
func Encode(op Opcode, a int) Instruction {
	return Instruction(uint32(op)<<24 | uint32(a)&0x00FFFFFF)
}

// Op returns the instruction's opcode.
func (i Instruction) Op() Opcode { return Opcode(i >> 24) }

// A returns the instruction's operand.
func (i Instruction) A() int { return int(i & 0x00FFFFFF) }

// String implements fmt.Stringer.
func (i Instruction) String() string {
	return fmt.Sprintf("%-16s %d", i.Op(), i.A())
}

// Chunk is a compiled unit of bytecode: one function body (or top-level
// form), its constant pool, and any nested closures compiled from it.
type Chunk struct {
	// Name identifies the chunk for debugging (e.g. a closure's #<closure name>).
	Name string
	// Arity is the fixed parameter count, excluding any rest param.
	Arity int
	// Variadic reports whether the chunk takes a trailing rest param.
	Variadic bool
	// Locals is the number of local variable slots.
	Locals int
	// LocalNames holds each local's source name, indexed by slot.
	LocalNames []string
	// Code is the compiled instruction sequence.
	Code []Instruction
	// Constants is the chunk's constant pool, indexed by AddConstant.
	Constants []core.Value
	// SubChunks holds chunks for closures compiled within this one, indexed
	// by the operand of their OpClosure instruction.
	SubChunks []*Chunk
}

// AddConstant interns v into the chunk's constant pool, returning its index.
// An equal existing constant is reused rather than duplicated.
func (c *Chunk) AddConstant(v core.Value) int {
	for i, existing := range c.Constants {
		if existing.Equals(v) {
			return i
		}
	}
	c.Constants = append(c.Constants, v)
	return len(c.Constants) - 1
}

// Emit appends an instruction encoding op and a to the chunk's code,
// returning its index.
func (c *Chunk) Emit(op Opcode, a int) int {
	c.Code = append(c.Code, Encode(op, a))
	return len(c.Code) - 1
}

// EmitJump appends a jump instruction with a placeholder target, returning
// its index for a later PatchJump call.
func (c *Chunk) EmitJump(op Opcode) int {
	return c.Emit(op, 0xFFFFFF)
}

// PatchJump rewrites the jump instruction at offset to target the current
// end of the chunk's code.
func (c *Chunk) PatchJump(offset int) {
	target := len(c.Code) - offset - 1
	c.Code[offset] = Encode(c.Code[offset].Op(), target)
}
