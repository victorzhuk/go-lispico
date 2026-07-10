package vm

import "fmt"

// Opcode identifies a bytecode instruction the VM can execute.
type Opcode uint8

// Bytecode instructions executed by the VM. Each operates on the operand
// stack and, where noted, the operand A encoded alongside it.
const (
	OpConst Opcode = iota
	OpGetGlobal
	OpSetGlobal
	OpGetLocal
	OpSetLocal
	OpCall
	OpTailCall
	OpReturn
	OpJump
	OpJumpIfFalse
	OpPop
	OpClosure
	OpMakeList
	OpMakeVector
	OpMakeMap
	OpNil
	OpTrue
	OpFalse
	OpLoop
	OpSetupTry
	OpPopTry
	OpThrow
	OpDup
	OpAdd
	OpSub
	OpMul
	OpDiv
	OpLt
	OpGt
	OpLe
	OpGe
	OpEq
	OpGetFunc
	OpSetFunc
)

var opNames = [...]string{
	OpConst:       "CONST",
	OpGetGlobal:   "GET_GLOBAL",
	OpSetGlobal:   "SET_GLOBAL",
	OpGetLocal:    "GET_LOCAL",
	OpSetLocal:    "SET_LOCAL",
	OpGetFunc:     "GET_FUNC",
	OpSetFunc:     "SET_FUNC",
	OpCall:        "CALL",
	OpTailCall:    "TAIL_CALL",
	OpReturn:      "RETURN",
	OpJump:        "JUMP",
	OpJumpIfFalse: "JUMP_IF_FALSE",
	OpPop:         "POP",
	OpClosure:     "CLOSURE",
	OpMakeList:    "MAKE_LIST",
	OpMakeVector:  "MAKE_VECTOR",
	OpMakeMap:     "MAKE_MAP",
	OpNil:         "NIL",
	OpTrue:        "TRUE",
	OpFalse:       "FALSE",
	OpLoop:        "LOOP",
	OpSetupTry:    "SETUP_TRY",
	OpPopTry:      "POP_TRY",
	OpThrow:       "THROW",
	OpDup:         "DUP",
	OpAdd:         "ADD",
	OpSub:         "SUB",
	OpMul:         "MUL",
	OpDiv:         "DIV",
	OpLt:          "LT",
	OpGt:          "GT",
	OpLe:          "LE",
	OpGe:          "GE",
	OpEq:          "EQ",
}

// String implements fmt.Stringer.
func (o Opcode) String() string {
	if int(o) < len(opNames) {
		return opNames[o]
	}
	return fmt.Sprintf("OP_%d", o)
}
