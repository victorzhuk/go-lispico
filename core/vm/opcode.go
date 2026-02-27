package vm

import "fmt"

type Opcode uint8

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
)

var opNames = [...]string{
	OpConst:       "CONST",
	OpGetGlobal:   "GET_GLOBAL",
	OpSetGlobal:   "SET_GLOBAL",
	OpGetLocal:    "GET_LOCAL",
	OpSetLocal:    "SET_LOCAL",
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
}

func (o Opcode) String() string {
	if int(o) < len(opNames) {
		return opNames[o]
	}
	return fmt.Sprintf("OP_%d", o)
}
