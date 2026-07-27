package vm

import "fmt"

// Opcode identifies a bytecode instruction the VM can execute.
type Opcode uint8

// Bytecode instructions executed by the VM. Each operates on the operand
// stack and, where noted, the operand A encoded alongside it.
const (
	OpConst Opcode = iota
	OpConstCharged
	OpGetGlobal
	OpSetGlobal
	OpSetLexical
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
	OpFreezeNative
	OpFreezeNativeFunc
	OpGetFunc
	OpSetFunc

	OpStructEnter
	OpStructLeave

	OpGetCell
	OpSetCell
	OpBindCell
	OpGetCap
	OpSetCap
	// OpDefMacro and OpDefMacroFunc bind a macro built from the Macro
	// prototype at the operand's constant index, filling its defining
	// environment in at run time. Two opcodes rather than one because the VM
	// carries no dialect: the compiler picks the cell, exactly as it does
	// with OpSetGlobal and OpSetFunc.
	OpDefMacro
	OpDefMacroFunc
)

var opNames = [...]string{
	OpConst:            "CONST",
	OpConstCharged:     "CONST_CHARGED",
	OpGetGlobal:        "GET_GLOBAL",
	OpSetGlobal:        "SET_GLOBAL",
	OpSetLexical:       "SET_LEXICAL",
	OpGetLocal:         "GET_LOCAL",
	OpSetLocal:         "SET_LOCAL",
	OpCall:             "CALL",
	OpTailCall:         "TAIL_CALL",
	OpReturn:           "RETURN",
	OpJump:             "JUMP",
	OpJumpIfFalse:      "JUMP_IF_FALSE",
	OpPop:              "POP",
	OpClosure:          "CLOSURE",
	OpMakeList:         "MAKE_LIST",
	OpMakeVector:       "MAKE_VECTOR",
	OpMakeMap:          "MAKE_MAP",
	OpNil:              "NIL",
	OpTrue:             "TRUE",
	OpFalse:            "FALSE",
	OpLoop:             "LOOP",
	OpSetupTry:         "SETUP_TRY",
	OpPopTry:           "POP_TRY",
	OpThrow:            "THROW",
	OpDup:              "DUP",
	OpAdd:              "ADD",
	OpSub:              "SUB",
	OpMul:              "MUL",
	OpDiv:              "DIV",
	OpLt:               "LT",
	OpGt:               "GT",
	OpLe:               "LE",
	OpGe:               "GE",
	OpEq:               "EQ",
	OpFreezeNative:     "FREEZE_NATIVE",
	OpFreezeNativeFunc: "FREEZE_NATIVE_FUNC",
	OpGetFunc:          "GET_FUNC",
	OpSetFunc:          "SET_FUNC",
	OpStructEnter:      "STRUCT_ENTER",
	OpStructLeave:      "STRUCT_LEAVE",
	OpGetCell:          "GET_CELL",
	OpSetCell:          "SET_CELL",
	OpBindCell:         "BIND_CELL",
	OpGetCap:           "GET_CAP",
	OpSetCap:           "SET_CAP",
	OpDefMacro:         "DEF_MACRO",
	OpDefMacroFunc:     "DEF_MACRO_FUNC",
}

// String implements fmt.Stringer.
func (o Opcode) String() string {
	if int(o) < len(opNames) {
		return opNames[o]
	}
	return fmt.Sprintf("OP_%d", o)
}
