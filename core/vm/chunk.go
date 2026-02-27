package vm

import (
	"fmt"

	"github.com/victorzhuk/go-lispico/core"
)

type Instruction uint32

func Encode(op Opcode, a int) Instruction {
	return Instruction(uint32(op)<<24 | uint32(a)&0x00FFFFFF)
}

func (i Instruction) Op() Opcode { return Opcode(i >> 24) }
func (i Instruction) A() int     { return int(i & 0x00FFFFFF) }

func (i Instruction) String() string {
	return fmt.Sprintf("%-16s %d", i.Op(), i.A())
}

type Chunk struct {
	Name      string
	Arity     int
	Variadic  bool
	Locals    int
	Code      []Instruction
	Constants []core.Value
	SubChunks []*Chunk
}

func (c *Chunk) AddConstant(v core.Value) int {
	for i, existing := range c.Constants {
		if existing.Equals(v) {
			return i
		}
	}
	c.Constants = append(c.Constants, v)
	return len(c.Constants) - 1
}

func (c *Chunk) Emit(op Opcode, a int) int {
	c.Code = append(c.Code, Encode(op, a))
	return len(c.Code) - 1
}

func (c *Chunk) EmitJump(op Opcode) int {
	return c.Emit(op, 0xFFFFFF)
}

func (c *Chunk) PatchJump(offset int) {
	target := len(c.Code) - offset - 1
	c.Code[offset] = Encode(c.Code[offset].Op(), target)
}
