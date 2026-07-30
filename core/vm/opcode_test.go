package vm

import (
	"testing"
)

func TestOpcodeString(t *testing.T) {
	tests := []struct {
		op       Opcode
		expected string
	}{
		{OpConst, "CONST"},
		{OpConstCharged, "CONST_CHARGED"},
		{OpGetGlobal, "GET_GLOBAL"},
		{OpSetGlobal, "SET_GLOBAL"},
		{OpGetLocal, "GET_LOCAL"},
		{OpSetLocal, "SET_LOCAL"},
		{OpCall, "CALL"},
		{OpTailCall, "TAIL_CALL"},
		{OpReturn, "RETURN"},
		{OpJump, "JUMP"},
		{OpJumpIfFalse, "JUMP_IF_FALSE"},
		{OpPop, "POP"},
		{OpClosure, "CLOSURE"},
		{OpMakeList, "MAKE_LIST"},
		{OpMakeVector, "MAKE_VECTOR"},
		{OpMakeMap, "MAKE_MAP"},
		{OpNil, "NIL"},
		{OpTrue, "TRUE"},
		{OpFalse, "FALSE"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.op.String(); got != tt.expected {
				t.Errorf("Opcode.String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestOpcodeInvalid(t *testing.T) {
	op := Opcode(255)
	expected := "OP_255"
	if got := op.String(); got != expected {
		t.Errorf("Opcode.String() = %q, want %q", got, expected)
	}
}

// TestCheckIntervalPinnedByDispatchProto guards checkInterval against silent
// drift: dispatch_proto_test.go's regCheckInterval redeclares this value
// (checkInterval is unexported, unreachable from package vm_test) to keep the
// dispatch-form prototypes' cancellation-poll cadence in lockstep with the
// real VM's. If checkInterval ever changes, that copy must change with it.
func TestCheckIntervalPinnedByDispatchProto(t *testing.T) {
	const pinnedRegCheckInterval = 128
	if checkInterval != pinnedRegCheckInterval {
		t.Fatalf("checkInterval = %d, want %d — update dispatch_proto_test.go's regCheckInterval to match", checkInterval, pinnedRegCheckInterval)
	}
}
