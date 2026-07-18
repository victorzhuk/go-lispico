package compiler

import "github.com/victorzhuk/go-lispico/core/vm"

// computeMaxStack estimates the peak operand-stack height (relative to the
// frame base) chunk.Code can reach. It walks Code once in array order,
// accumulating each instruction's stack delta without resetting at branch
// targets — reconverging branches (if/when/cond) then carry the taller
// side's height into the other, which only ever overestimates, never
// undercounts. A back-edge (OpLoop) is walked once too, so a chunk whose
// stack genuinely grows without bound across loop iterations (a local bound
// to a fresh value inside a loop body, never reclaimed) is underestimated by
// this single pass; that only costs the pre-grow optimization a reallocation
// for such chunks, since push still grows the stack safely. The result is
// floored at Locals and at the highest local slot index actually referenced,
// so Validate's OpGetLocal/OpSetLocal bound is never too tight regardless of
// how well the height estimate tracks reality.
func computeMaxStack(chunk *vm.Chunk) int {
	height, peak, maxSlot := 0, 0, -1
	for _, inst := range chunk.Code {
		op, a := inst.Op(), inst.A()
		if op == vm.OpGetLocal || op == vm.OpSetLocal {
			if a > maxSlot {
				maxSlot = a
			}
		}
		height += stackDelta(op, a)
		if height > peak {
			peak = height
		}
	}
	result := peak
	if chunk.Locals > result {
		result = chunk.Locals
	}
	if maxSlot+1 > result {
		result = maxSlot + 1
	}
	return result
}

// stackDelta returns op's net effect on the operand stack for operand a.
func stackDelta(op vm.Opcode, a int) int {
	switch op {
	case vm.OpNil, vm.OpTrue, vm.OpFalse, vm.OpConst, vm.OpGetGlobal, vm.OpGetLocal, vm.OpGetFunc, vm.OpDup, vm.OpClosure:
		return 1
	case vm.OpPop, vm.OpJumpIfFalse, vm.OpThrow, vm.OpReturn:
		return -1
	case vm.OpCall, vm.OpTailCall,
		vm.OpAdd, vm.OpSub, vm.OpMul, vm.OpDiv, vm.OpLt, vm.OpGt, vm.OpLe, vm.OpGe, vm.OpEq:
		return -a
	case vm.OpMakeList, vm.OpMakeVector:
		return 1 - a
	case vm.OpMakeMap:
		return 1 - 2*a
	default:
		return 0
	}
}
