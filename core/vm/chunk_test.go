package vm

import (
	"testing"

	"github.com/victorzhuk/go-lispico/core"
)

func TestInstructionRoundTrip(t *testing.T) {
	tests := []struct {
		op Opcode
		a  int
	}{
		{OpConst, 0},
		{OpConst, 255},
		{OpConst, 65535},
		{OpConst, 16777215},
		{OpJump, 1000000},
		{OpCall, 42},
	}

	for _, tt := range tests {
		t.Run(tt.op.String(), func(t *testing.T) {
			instr := Encode(tt.op, tt.a)
			if got := instr.Op(); got != tt.op {
				t.Errorf("Op() = %v, want %v", got, tt.op)
			}
			if got := instr.A(); got != tt.a {
				t.Errorf("A() = %d, want %d", got, tt.a)
			}
		})
	}
}

func TestChunkAddConstant(t *testing.T) {
	c := &Chunk{}

	idx1 := c.AddConstant(core.Int{V: 42})
	if idx1 != 0 {
		t.Errorf("first constant index = %d, want 0", idx1)
	}
	if len(c.Constants) != 1 {
		t.Errorf("constants length = %d, want 1", len(c.Constants))
	}

	idx2 := c.AddConstant(core.Int{V: 42})
	if idx2 != 0 {
		t.Errorf("duplicate constant index = %d, want 0", idx2)
	}
	if len(c.Constants) != 1 {
		t.Errorf("constants length after dedup = %d, want 1", len(c.Constants))
	}

	idx3 := c.AddConstant(core.Int{V: 43})
	if idx3 != 1 {
		t.Errorf("new constant index = %d, want 1", idx3)
	}
	if len(c.Constants) != 2 {
		t.Errorf("constants length = %d, want 2", len(c.Constants))
	}
}

func TestChunkEmit(t *testing.T) {
	c := &Chunk{}

	off1 := c.Emit(OpConst, 10)
	if off1 != 0 {
		t.Errorf("first emit offset = %d, want 0", off1)
	}
	if len(c.Code) != 1 {
		t.Errorf("code length = %d, want 1", len(c.Code))
	}

	off2 := c.Emit(OpPop, 0)
	if off2 != 1 {
		t.Errorf("second emit offset = %d, want 1", off2)
	}
	if len(c.Code) != 2 {
		t.Errorf("code length = %d, want 2", len(c.Code))
	}
}

func TestChunkPatchJump(t *testing.T) {
	c := &Chunk{}

	c.Emit(OpConst, 0)

	jumpOff := c.EmitJump(OpJumpIfFalse)

	c.Emit(OpConst, 1)
	c.Emit(OpConst, 2)

	c.PatchJump(jumpOff)

	if got := c.Code[jumpOff].A(); got != 2 {
		t.Errorf("patched jump operand = %d, want 2", got)
	}
}
