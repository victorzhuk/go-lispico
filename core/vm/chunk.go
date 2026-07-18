package vm

import (
	"fmt"
	"sync/atomic"

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
	// MaxStack is the peak operand-stack height (relative to the frame base)
	// this chunk's Code can reach, computed by the compiler at finalization.
	// Run pre-grows the stack to this size once per frame entry, and Validate
	// bounds OpGetLocal/OpSetLocal operands against it.
	MaxStack int
	// LocalNames holds each local's source name, indexed by slot.
	LocalNames []string
	// Captured marks which local slots are referenced by nested closures.
	// Indexed like LocalNames. A true entry means the local must be mirrored
	// to an Env so closures can access it via OpGetGlobal.
	Captured []bool
	// FullEnv forces all locals to be mirrored to an Env, even uncaptured ones.
	// Set when capture analysis encounters an unanalyzable construct, as a
	// conservative fallback preserving current behavior.
	FullEnv bool
	// Code is the compiled instruction sequence.
	Code []Instruction
	// Constants is the chunk's constant pool, indexed by AddConstant.
	Constants []core.Value
	// SubChunks holds chunks for closures compiled within this one, indexed
	// by the operand of their OpClosure instruction.
	SubChunks []*Chunk
	// Truthiness is the dialect's truthiness predicate for conditional opcodes.
	// When nil, core.IsTruthy (nil+false falsy) is used.
	Truthiness func(core.Value) bool
	// sites is the per-instruction global-read cache, built lazily by
	// EnsureSites once a chunk is known to be reused, and published atomically
	// so concurrent runs of a shared chunk never race on it. Nil until built —
	// the VM then resolves globals through the ordinary chain walk.
	sites atomic.Pointer[siteTable]
}

// siteTable is a chunk's global-read cache: entries keyed by symbol, and idx
// mapping each instruction index to an entry (-1 for none). Immutable once
// published; the per-entry resolution is what varies at run time.
type siteTable struct {
	idx     []int32
	entries []siteCache
}

// siteEntry is a resolved global binding cached at one bytecode site: the
// env it was resolved in, that env's name-binding generation at resolution
// time, and the cell itself. Published once and read many times, so it is
// immutable — a stale entry is replaced, never mutated.
type siteEntry struct {
	env  *core.Env
	gen  uint64
	cell *core.Cell
}

// siteCache is one chunk site's resolution cache. constIdx is fixed at
// compile time; entry is published lock-free so concurrent execution of a
// shared chunk (the same chunk run under different envs) never tears a read.
type siteCache struct {
	constIdx int32
	entry    atomic.Pointer[siteEntry]
}

// EnsureSites builds the global-read site table once, from the chunk's own
// Code, and publishes it. Callers invoke it only when a chunk is known to be
// reused (a bytecode-cache hit, or eager compilation for benchmarks/tests), so
// a run-once chunk never pays for a table it would never reuse. Safe for
// concurrent callers; idempotent.
func (c *Chunk) EnsureSites() {
	if c.sites.Load() == nil {
		// A rare concurrent first hit may build twice; the CAS keeps a single
		// published table and the loser's is discarded.
		c.sites.CompareAndSwap(nil, c.buildSites())
	}
}

// buildSites scans Code for OpGetGlobal reads, assigning one shared entry per
// distinct symbol (constant index) so repeated reads of the same global reuse
// a single cached resolution. Native ops need no site: their canonical
// decision is frozen at the operator's OpGetGlobal.
func (c *Chunk) buildSites() *siteTable {
	idx := make([]int32, len(c.Code))
	for i := range idx {
		idx[i] = -1
	}
	bySym := map[int32]int32{}
	var entries []siteCache
	for ip, inst := range c.Code {
		if inst.Op() != OpGetGlobal {
			continue
		}
		constIdx := int32(inst.A())
		si, ok := bySym[constIdx]
		if !ok {
			si = int32(len(entries))
			entries = append(entries, siteCache{constIdx: constIdx})
			bySym[constIdx] = si
		}
		idx[ip] = si
	}
	return &siteTable{idx: idx, entries: entries}
}

// site returns the chunk's site cache entry for ip, or nil if the table is not
// built yet or ip has no site (the VM then resolves through the chain walk).
func (c *Chunk) site(ip int) *siteCache {
	t := c.sites.Load()
	if t == nil || ip < 0 || ip >= len(t.idx) {
		return nil
	}
	si := t.idx[ip]
	if si < 0 {
		return nil
	}
	return &t.entries[si]
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

// EmitLoop appends an OpLoop that jumps back to the instruction at start.
func (c *Chunk) EmitLoop(start int) int {
	return c.Emit(OpLoop, start)
}

// PatchJump rewrites the jump instruction at offset to target the current
// end of the chunk's code.
func (c *Chunk) PatchJump(offset int) {
	target := len(c.Code) - offset - 1
	c.Code[offset] = Encode(c.Code[offset].Op(), target)
}

// GetConstant returns the constant at index i, or an error if i is out of range.
func (c *Chunk) GetConstant(i int) (core.Value, error) {
	if i < 0 || i >= len(c.Constants) {
		return nil, fmt.Errorf("constant index %d out of range", i)
	}
	return c.Constants[i], nil
}

// GetSymbolConstant returns the constant at index i as a core.Symbol, or an error.
func (c *Chunk) GetSymbolConstant(i int) (core.Symbol, error) {
	v, err := c.GetConstant(i)
	if err != nil {
		return core.Symbol{}, err
	}
	sym, ok := v.(core.Symbol)
	if !ok {
		return core.Symbol{}, fmt.Errorf("expected symbol constant, got %T", v)
	}
	return sym, nil
}

// GetSubChunk returns the sub-chunk at index i, or an error if i is out of range.
func (c *Chunk) GetSubChunk(i int) (*Chunk, error) {
	if i < 0 || i >= len(c.SubChunks) {
		return nil, fmt.Errorf("subchunk index %d out of range", i)
	}
	return c.SubChunks[i], nil
}

// PatchJumpTo rewrites the jump instruction at offset to jump to target.
func (c *Chunk) PatchJumpTo(offset, target int) {
	c.Code[offset] = Encode(c.Code[offset].Op(), target)
}

// Validate walks c.Code checking that every operand is in range for its
// opcode, recursing into chunks reachable via OpClosure. Run trusts a
// validated chunk and skips these checks in its hot loop, so every path that
// can reach Run with a newly built or cached chunk must call Validate first.
func (c *Chunk) Validate() error {
	for ip, inst := range c.Code {
		op, a := inst.Op(), inst.A()
		switch op {
		case OpConst:
			if a < 0 || a >= len(c.Constants) {
				return bytecodeErrorf("%s: constant index %d out of range", op, a)
			}
		case OpGetGlobal, OpSetGlobal, OpSetLexical, OpGetFunc, OpSetFunc:
			if a < 0 || a >= len(c.Constants) {
				return bytecodeErrorf("%s: constant index %d out of range", op, a)
			}
			if _, ok := c.Constants[a].(core.Symbol); !ok {
				return bytecodeErrorf("%s: constant %d is not a symbol", op, a)
			}
		case OpJump, OpJumpIfFalse:
			target := ip + 1 + a
			if target < 0 || target >= len(c.Code) {
				return bytecodeErrorf("%s: jump target %d out of range", op, target)
			}
		case OpLoop:
			if a < 0 || a >= len(c.Code) {
				return bytecodeErrorf("%s: loop target %d out of range", op, a)
			}
		case OpSetupTry:
			if a < 0 || a >= len(c.Code) {
				return bytecodeErrorf("%s: handler target %d out of range", op, a)
			}
		case OpGetLocal, OpSetLocal:
			if a < 0 || a >= c.MaxStack {
				return bytecodeErrorf("%s: local slot %d out of range", op, a)
			}
		case OpClosure:
			if a < 0 || a >= len(c.SubChunks) {
				return bytecodeErrorf("%s: subchunk index %d out of range", op, a)
			}
			if err := c.SubChunks[a].Validate(); err != nil {
				return err
			}
		}
	}
	// ip only ever reaches len(Code) by falling through the final instruction,
	// so a chunk whose last op transfers control (or errors) can never run off
	// the end — which is what lets Run index code[ip] without a bounds check.
	if len(c.Code) == 0 {
		return bytecodeErrorf("chunk has no instructions")
	}
	switch c.Code[len(c.Code)-1].Op() {
	case OpReturn, OpJump, OpLoop, OpThrow:
	default:
		return bytecodeErrorf("chunk does not end in a control-transfer instruction")
	}
	return nil
}

func bytecodeErrorf(format string, args ...any) error {
	return &core.LispicoError{Code: "BytecodeError", Message: fmt.Sprintf(format, args...)}
}
