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

func TestChunkCopyTreeFreshSites(t *testing.T) {
	sym := core.Symbol{V: "x"}
	sub := &Chunk{
		Name:         "<fn>",
		Code:         []Instruction{Encode(OpGetGlobal, 0), Encode(OpReturn, 0)},
		Constants:    []core.Value{sym},
		ConstCharges: map[int]int64{0: 32},
	}
	root := &Chunk{
		Name:         "<top>",
		Code:         []Instruction{Encode(OpGetGlobal, 0), Encode(OpClosure, 0), Encode(OpReturn, 0)},
		Constants:    []core.Value{sym},
		ConstCharges: map[int]int64{0: 64},
		SubChunks:    []*Chunk{sub},
	}
	root.EnsureSites()
	sub.EnsureSites()

	env := core.NewEnv(nil)
	env.Set("x", core.Int{V: 1})
	cell, ok := env.CellLocal("x")
	if !ok {
		t.Fatal("missing cell")
	}
	root.site(0).entry.Store(&siteEntry{env: env, cell: cell, val: core.Int{V: 1}, gen: env.NameGen(), ver: cell.Version()})
	sub.site(0).entry.Store(&siteEntry{env: env, cell: cell, val: core.Int{V: 1}, gen: env.NameGen(), ver: cell.Version()})

	copied := root.CopyTreeFreshSites()
	if copied == root {
		t.Fatal("root chunk pointer shared")
	}
	if copied.SubChunks[0] == root.SubChunks[0] {
		t.Fatal("subchunk pointer shared")
	}
	if &copied.Code[0] != &root.Code[0] {
		t.Fatal("code slice not shared")
	}
	if &copied.Constants[0] != &root.Constants[0] {
		t.Fatal("constant slice not shared")
	}
	if copied.ConstCharges[0] != root.ConstCharges[0] {
		t.Fatal("const charge table not copied")
	}
	if copied.site(0) == nil {
		t.Fatal("root site table missing")
	}
	if copied.site(0).entry.Load() != nil {
		t.Fatal("root site entry copied")
	}
	if copied.SubChunks[0].site(0) == nil {
		t.Fatal("subchunk site table missing")
	}
	if copied.SubChunks[0].site(0).entry.Load() != nil {
		t.Fatal("subchunk site entry copied")
	}
	if copied.SubChunks[0].ConstCharges[0] != sub.ConstCharges[0] {
		t.Fatal("subchunk const charge table not copied")
	}
	if root.site(0).entry.Load() == nil {
		t.Fatal("source root site entry cleared")
	}
	if sub.site(0).entry.Load() == nil {
		t.Fatal("source subchunk site entry cleared")
	}
}
