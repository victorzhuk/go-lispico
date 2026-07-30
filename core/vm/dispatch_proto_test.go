package vm_test

import (
	"context"
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/core/compiler"
	"github.com/victorzhuk/go-lispico/core/vm"

	"github.com/stretchr/testify/require"
)

// fibSrc, fibN, and fibExpected are the single source of truth for every arm
// and the correctness test: fib(20) = 6765.
const (
	fibSrc      = `(def fib (fn [n] (if (< n 2) n (+ (fib (- n 1)) (fib (- n 2))))))`
	fibN        = 20
	fibExpected = 6765
)

// regCheckInterval mirrors vm.go's private checkInterval constant (128): the
// unexported field can't be read from this package, so the value is
// redeclared here to keep the prototypes' cancellation-poll cadence in lockstep
// with the real VM's.
const regCheckInterval = 128

// fibonacci is the independent oracle every arm is checked against — plain
// iterative Go, no relation to any VM or compiler in this repo.
func fibonacci(n int) int64 {
	if n < 2 {
		return int64(n)
	}
	a, b := int64(0), int64(1)
	for i := 2; i <= n; i++ {
		a, b = b, a+b
	}
	return b
}

// fibCallShape counts, for naive recursive fib(n), how many of the resulting
// calls hit the base case (n<2, 5 stack instructions/11 register instructions
// per the disassembly below) versus the recursive case (11/11 instructions).
// Naive recursion — fine at n=20 (~22k calls), this only runs at test/bench
// setup time.
func fibCallShape(n int) (base, recursive int) {
	if n < 2 {
		return 1, 0
	}
	b1, r1 := fibCallShape(n - 1)
	b2, r2 := fibCallShape(n - 2)
	return b1 + b2, 1 + r1 + r2
}

// Per-call instruction counts read off the real disassembly (see
// TestFibDispatchFormsDisassembly): the base case executes indices
// 0,1,2,3,12 of the fib body; the recursive case executes 0,1,4,5,6,7,8,9,10,11,12.
const (
	instrPerBaseCase      = 5
	instrPerRecursiveCall = 11
	// stackDriverInstr is the one-time GET_GLOBAL/CONST/CALL/RETURN sequence
	// the stack arm's top-level "(fib N)" chunk pays that the register/
	// pre-decoded arms don't, since those are invoked directly rather than
	// through a compiled driver chunk — see the report's stated deviations.
	stackDriverInstr = 4
	// allocChargePerBaseCase/allocChargePerRecursiveCall count
	// core.MeterScalarBytes charges: one per FUSED_NATIVE/native-op result
	// (LT in the base case; LT+SUB+SUB+ADD in the recursive case).
	allocChargePerBaseCase      = 1
	allocChargePerRecursiveCall = 4
)

func stackInstrCount(n int) int {
	base, rec := fibCallShape(n)
	return stackDriverInstr + base*instrPerBaseCase + rec*instrPerRecursiveCall
}

// protoInstrCount is the per-call instruction count shared by every
// specialized-prototype arm (register, predecoded, stack-specialized, and
// stack-specialized-predecoded) — none of them pays stackDriverInstr, since
// all four are invoked directly rather than through a compiled driver chunk.
func protoInstrCount(n int) int {
	base, rec := fibCallShape(n)
	return base*instrPerBaseCase + rec*instrPerRecursiveCall
}

func expectedAllocCharges(n int) int {
	base, rec := fibCallShape(n)
	return base*allocChargePerBaseCase + rec*allocChargePerRecursiveCall
}

// ---------------------------------------------------------------------------
// Disassembly — the recorded source of truth every prototype below is
// translated from, mechanically, instruction by instruction.
// ---------------------------------------------------------------------------

// TestFibDispatchFormsDisassembly compiles fibSrc, logs the real bytecode for
// the fib closure body, and pins its exact shape so the register/pre-decoded
// translation below (translateFibBody) fails loudly instead of silently
// drifting if the compiler's fusion behavior ever changes.
func TestFibDispatchFormsDisassembly(t *testing.T) {
	forms, err := core.Read(fibSrc)
	require.NoError(t, err)
	chunks, err := compiler.CompileAll(forms)
	require.NoError(t, err)
	require.Len(t, chunks, 1)
	require.NotEmpty(t, chunks[0].SubChunks)
	body := chunks[0].SubChunks[0]

	for i, instr := range body.Code {
		t.Logf("%3d: %s", i, instr.String())
	}
	for i, fo := range body.Fused {
		t.Logf("Fused[%d] = %+v", i, fo)
	}

	var ops []vm.Opcode
	for _, instr := range body.Code {
		ops = append(ops, instr.Op())
	}
	require.Equal(t, []vm.Opcode{
		vm.OpFusedNativeOp, // 0: (< n 2)
		vm.OpJumpIfFalse,   // 1
		vm.OpGetLocal,      // 2: then-branch, push n
		vm.OpJump,          // 3: skip else-branch
		vm.OpFreezeNative,  // 4: freeze "+" head
		vm.OpGetGlobal,     // 5: push fib
		vm.OpFusedNativeOp, // 6: (- n 1)
		vm.OpCall,          // 7: (fib (- n 1))
		vm.OpGetGlobal,     // 8: push fib
		vm.OpFusedNativeOp, // 9: (- n 2)
		vm.OpCall,          // 10: (fib (- n 2))
		vm.OpAdd,           // 11: dispatch frozen "+"
		vm.OpReturn,        // 12
	}, ops, "fib body disassembly drifted from the pinned shape")

	require.Len(t, body.Fused, 3)
	require.Equal(t, vm.OpLt, body.Fused[0].Op)
	require.Equal(t, vm.OpSub, body.Fused[1].Op)
	require.Equal(t, vm.OpSub, body.Fused[2].Op)
}

// ---------------------------------------------------------------------------
// Prototype A/B shared IR — a three-address register-form translation of the
// disassembly above, built mechanically from the live chunk (not hand-typed
// constants) by translateFibBody. Register assignment mirrors the real
// chunk's own operand-stack height at each point (see the report): r0 is the
// param n; r1..r3 are reused exactly as vm.stack's slots are reused.
// ---------------------------------------------------------------------------

type regOp uint8

const (
	rOpLT          regOp = iota // dst = local(src1) < constVal
	rOpJumpIfFalse              // if !truthy(reg[src1]) { ip = target }
	rOpMove                     // dst = reg[src1]
	rOpJump                     // ip = target
	rOpFreezeAdd                // resolve sym ("+"), record canonical flag for rOpAdd
	rOpGetFib                   // dst = resolve sym ("fib")
	rOpSub                      // dst = local(src1) - constVal
	rOpCallFib                  // dst = call fib(reg[src2]); fn read from reg[src1]
	rOpAdd                      // dst = reg[src1] + reg[src2], freeze-checked
	rOpReturn                   // return reg[src1]
)

// regInstr is 72 bytes (unsafe.Sizeof), wider than stkInstr's 48 — the two
// extra int fields (src1/src2 vs stkInstr's implicit push/pop addressing)
// widen every fetch-by-value in the dispatch loop. That handicaps the
// register arm's own numbers rather than flattering them, so it does not
// need correcting for the encoding-delta conclusion to hold — noted here so
// a future reader who spots the size gap doesn't mistake it for an
// unaccounted confound.
type regInstr struct {
	op       regOp
	dst      int
	src1     int
	src2     int
	target   int
	constVal core.Value
	sym      string
}

// fibBodyFields is what both translators (register-form and stack-form) need
// out of the real chunk: the two folded constants and the two symbols the
// fused/frozen instructions resolve, plus the two real jump targets. Neither
// translator hand-types these — both read them off the live chunk through
// parseFibBody, so a compiler change breaks the loud asserts below instead of
// silently drifting.
type fibBodyFields struct {
	twoConst, oneConst    core.Value
	plusSym, fibSym       string
	thenJumpIfFalseTarget int // real target of code[1] (JUMP_IF_FALSE)
	thenJumpTarget        int // real target of code[3] (JUMP)
}

// parseFibBody asserts the exact real opcode at each of the 13 pinned indices
// (see TestFibDispatchFormsDisassembly) before extracting the fields either
// translator needs.
func parseFibBody(tb testing.TB, body *vm.Chunk) fibBodyFields {
	tb.Helper()
	code := body.Code
	require.Len(tb, code, 13, "fib body instruction count drifted from the pinned disassembly")
	require.Equal(tb, 4, body.MaxStack, "fib body MaxStack drifted — regFrame.regs/stkFrame.vals are hardcoded [4]core.Value")

	getSym := func(ip int) string {
		sym, err := body.GetSymbolConstant(code[ip].A())
		require.NoError(tb, err)
		return sym.V
	}
	jumpTarget := func(ip int) int { return ip + 1 + code[ip].A() }

	require.Equal(tb, vm.OpFusedNativeOp, code[0].Op())
	fused0 := body.Fused[code[0].A()]
	require.Equal(tb, vm.OpLt, fused0.Op)
	require.Equal(tb, vm.OperandLocal, fused0.AKind)
	require.Equal(tb, vm.OperandConst, fused0.BKind)
	twoConst := body.Constants[fused0.B]

	require.Equal(tb, vm.OpJumpIfFalse, code[1].Op())
	require.Equal(tb, vm.OpGetLocal, code[2].Op())
	require.Equal(tb, vm.OpJump, code[3].Op())

	require.Equal(tb, vm.OpFreezeNative, code[4].Op())
	plusSym := getSym(4)
	require.Equal(tb, "+", plusSym)

	require.Equal(tb, vm.OpGetGlobal, code[5].Op())
	fibSym := getSym(5)
	require.Equal(tb, "fib", fibSym)

	require.Equal(tb, vm.OpFusedNativeOp, code[6].Op())
	fused1 := body.Fused[code[6].A()]
	require.Equal(tb, vm.OpSub, fused1.Op)
	oneConst := body.Constants[fused1.B]

	require.Equal(tb, vm.OpCall, code[7].Op())

	require.Equal(tb, vm.OpGetGlobal, code[8].Op())
	require.Equal(tb, fibSym, getSym(8))

	require.Equal(tb, vm.OpFusedNativeOp, code[9].Op())
	fused2 := body.Fused[code[9].A()]
	require.Equal(tb, vm.OpSub, fused2.Op)
	require.True(tb, body.Constants[fused2.B].Equals(twoConst))

	require.Equal(tb, vm.OpCall, code[10].Op())
	require.Equal(tb, vm.OpAdd, code[11].Op())
	require.Equal(tb, vm.OpReturn, code[12].Op())

	return fibBodyFields{
		twoConst: twoConst, oneConst: oneConst,
		plusSym: plusSym, fibSym: fibSym,
		thenJumpIfFalseTarget: jumpTarget(1),
		thenJumpTarget:        jumpTarget(3),
	}
}

// translateFibBody mechanically translates body's real Code/Fused into the
// register IR above, using the fields parseFibBody pulled off the live chunk.
func translateFibBody(tb testing.TB, body *vm.Chunk) []regInstr {
	tb.Helper()
	f := parseFibBody(tb, body)

	prog := make([]regInstr, 13)
	prog[0] = regInstr{op: rOpLT, dst: 1, src1: 0, constVal: f.twoConst}
	prog[1] = regInstr{op: rOpJumpIfFalse, src1: 1, target: f.thenJumpIfFalseTarget}
	prog[2] = regInstr{op: rOpMove, dst: 1, src1: 0}
	prog[3] = regInstr{op: rOpJump, target: f.thenJumpTarget}
	prog[4] = regInstr{op: rOpFreezeAdd, sym: f.plusSym}
	prog[5] = regInstr{op: rOpGetFib, dst: 1, sym: f.fibSym}
	prog[6] = regInstr{op: rOpSub, dst: 2, src1: 0, constVal: f.oneConst}
	prog[7] = regInstr{op: rOpCallFib, dst: 1, src1: 1, src2: 2}
	prog[8] = regInstr{op: rOpGetFib, dst: 2, sym: f.fibSym}
	prog[9] = regInstr{op: rOpSub, dst: 3, src1: 0, constVal: f.twoConst}
	prog[10] = regInstr{op: rOpCallFib, dst: 2, src1: 2, src2: 3}
	prog[11] = regInstr{op: rOpAdd, dst: 1, src1: 1, src2: 2}
	prog[12] = regInstr{op: rOpReturn, src1: 1}
	return prog
}

// ---------------------------------------------------------------------------
// Shared register-machine state, driven by two dispatch loops: runSwitch
// (prototype A, task 1.1) and runDecoded (prototype B, task 1.2).
// ---------------------------------------------------------------------------

// regFrame is one call frame: a fixed 4-register window (matches the real
// chunk's MaxStack=4) plus the caller's register that receives this frame's
// return value. Explicit, pushed/popped by the interpreter itself — no
// Go-native recursion, mirroring vm/frame.go's Frame.
type regFrame struct {
	regs [4]core.Value
	ip   int
	dst  int
}

// regInterp is the shared engine both prototypes run on: register-addressed
// value stack (frames), plus the same cross-cutting taxes the real VM pays on
// this hot path — a checkInterval-cadence cancellation poll and a
// MeterScalarBytes charge per arithmetic/comparison result. env and body
// resolve "fib" and "+" and identify the callee for recursive dispatch.
type regInterp struct {
	env  *core.Env
	body *vm.Chunk

	frames []regFrame

	// freezeCanon records whether "+" was still a canonical native op at its
	// FREEZE_NATIVE head-resolution point (rOpFreezeAdd), the fast-path check
	// rOpAdd tests. Production's freeze record also carries the resolved head
	// value for its non-canonical rebind fallback; that fallback is never
	// reachable in this benchmark (see rOpAdd), so no value is tracked here.
	freezeCanon bool

	budget     int
	pollCount  int
	instrCount int

	pendingAlloc int64
	allocCharges int

	depth int

	callErr     error
	finalResult core.Value
}

func newRegInterp(env *core.Env, body *vm.Chunk) *regInterp {
	return &regInterp{env: env, body: body, frames: make([]regFrame, 0, 32)}
}

func (rv *regInterp) top() *regFrame { return &rv.frames[len(rv.frames)-1] }

func (rv *regInterp) pushFrame(arg core.Value, dst int) {
	rv.frames = append(rv.frames, regFrame{dst: dst})
	f := &rv.frames[len(rv.frames)-1]
	f.regs[0] = arg
	f.ip = 0
	rv.depth++
}

// poll mirrors vm.go's pollCancel: reset the instruction budget and check
// ctx.Err() at the same regCheckInterval cadence. pendingAlloc's flush is a
// field reset here because the real chargeAllocBytes early-returns unmetered
// (maxAllocBytes<=0) in this benchmark's default configuration — the same
// steady-state cost the real VM pays when no resource limits are set.
func (rv *regInterp) poll(ctx context.Context) error {
	rv.budget = regCheckInterval
	rv.pollCount++
	rv.pendingAlloc = 0
	return ctx.Err()
}

func (rv *regInterp) chargeScalar() {
	rv.pendingAlloc += core.MeterScalarBytes
	rv.allocCharges++
}

func (rv *regInterp) reset() {
	rv.frames = rv.frames[:0]
	rv.freezeCanon = false
	rv.budget = regCheckInterval
	rv.pollCount = 0
	rv.instrCount = 0
	rv.pendingAlloc = 0
	rv.allocCharges = 0
	rv.depth = 0
	rv.callErr = nil
	rv.finalResult = nil
}

// resolveFibCallee resolves fn to the fib closure identity this interpreter
// recurses into: a type assertion plus pointer-identity check against the
// real *vm.Closure — the closest black-box equivalent of the real VM's
// dynamic dispatch check available from outside package vm (the site-cache
// fast path itself is unexported; see the report).
func (rv *regInterp) resolveFibCallee(fn core.Value) (*vm.Closure, bool) {
	cl, ok := fn.(*vm.Closure)
	if !ok || cl.Chunk != rv.body {
		return nil, false
	}
	return cl, true
}

// runSwitch is prototype A (task 1.1): a big-switch dispatch loop over the
// regInstr array, register-addressed instead of stack-addressed.
func (rv *regInterp) runSwitch(ctx context.Context, code []regInstr, arg core.Value) (core.Value, error) {
	rv.reset()
	rv.pushFrame(arg, -1)

	for {
		if rv.budget--; rv.budget <= 0 {
			if err := rv.poll(ctx); err != nil {
				return nil, err
			}
		}
		rv.instrCount++

		f := &rv.frames[len(rv.frames)-1]
		instr := code[f.ip]
		f.ip++

		switch instr.op {
		case rOpLT:
			n := f.regs[instr.src1].(core.Int).V
			c := instr.constVal.(core.Int).V
			f.regs[instr.dst] = core.BoxBool(n < c)
			rv.chargeScalar()

		case rOpJumpIfFalse:
			if !core.IsTruthy(f.regs[instr.src1]) {
				f.ip = instr.target
			}

		case rOpMove:
			f.regs[instr.dst] = f.regs[instr.src1]

		case rOpJump:
			f.ip = instr.target

		case rOpFreezeAdd:
			_, _, canon := rv.env.GetCanonical(instr.sym)
			rv.freezeCanon = canon

		case rOpGetFib:
			v, _, _ := rv.env.GetCanonical(instr.sym)
			f.regs[instr.dst] = v

		case rOpSub:
			n := f.regs[instr.src1].(core.Int).V
			c := instr.constVal.(core.Int).V
			f.regs[instr.dst] = core.BoxInt(n - c)
			rv.chargeScalar()

		case rOpCallFib:
			fn := f.regs[instr.src1]
			argv := f.regs[instr.src2]
			if _, ok := rv.resolveFibCallee(fn); !ok {
				return nil, fmt.Errorf("regInterp: unexpected fib callee %T", fn)
			}
			rv.pushFrame(argv, instr.dst)

		case rOpAdd:
			if !rv.freezeCanon {
				return nil, fmt.Errorf("regInterp: '+' rebind fallback not implemented (unreachable in this benchmark)")
			}
			a := f.regs[instr.src1].(core.Int).V
			b := f.regs[instr.src2].(core.Int).V
			f.regs[instr.dst] = core.BoxInt(a + b)
			rv.chargeScalar()

		case rOpReturn:
			result := f.regs[instr.src1]
			dst := f.dst
			rv.depth--
			rv.frames = rv.frames[:len(rv.frames)-1]
			if len(rv.frames) == 0 {
				return result, nil
			}
			rv.frames[len(rv.frames)-1].regs[dst] = result
		}
	}
}

// ---------------------------------------------------------------------------
// Prototype B (task 1.2): a flat pre-decoded closure stream. Each regInstr is
// compiled once into a specialized func capturing its own resolved operands
// (dst/src/constVal/sym as plain locals, not re-read from the regInstr struct
// at dispatch time) — a Vitess/goja-shaped []func stream, not nested thunks.
// ---------------------------------------------------------------------------

type decodedStep func(rv *regInterp) int

// compileDecoded turns prog into a flat []decodedStep, one closure per
// instruction, sharing regInterp/regFrame with runSwitch so the two
// prototypes differ only in dispatch mechanism, not in addressing or state.
func compileDecoded(prog []regInstr) []decodedStep {
	steps := make([]decodedStep, len(prog))
	for i, instr := range prog {
		next := i + 1
		switch instr.op {
		case rOpLT:
			dst, src1, c := instr.dst, instr.src1, instr.constVal.(core.Int).V
			steps[i] = func(rv *regInterp) int {
				f := rv.top()
				n := f.regs[src1].(core.Int).V
				f.regs[dst] = core.BoxBool(n < c)
				rv.chargeScalar()
				return next
			}

		case rOpJumpIfFalse:
			src1, target := instr.src1, instr.target
			steps[i] = func(rv *regInterp) int {
				if !core.IsTruthy(rv.top().regs[src1]) {
					return target
				}
				return next
			}

		case rOpMove:
			dst, src1 := instr.dst, instr.src1
			steps[i] = func(rv *regInterp) int {
				f := rv.top()
				f.regs[dst] = f.regs[src1]
				return next
			}

		case rOpJump:
			target := instr.target
			steps[i] = func(rv *regInterp) int { return target }

		case rOpFreezeAdd:
			sym := instr.sym
			steps[i] = func(rv *regInterp) int {
				_, _, canon := rv.env.GetCanonical(sym)
				rv.freezeCanon = canon
				return next
			}

		case rOpGetFib:
			dst, sym := instr.dst, instr.sym
			steps[i] = func(rv *regInterp) int {
				v, _, _ := rv.env.GetCanonical(sym)
				rv.top().regs[dst] = v
				return next
			}

		case rOpSub:
			dst, src1, c := instr.dst, instr.src1, instr.constVal.(core.Int).V
			steps[i] = func(rv *regInterp) int {
				f := rv.top()
				n := f.regs[src1].(core.Int).V
				f.regs[dst] = core.BoxInt(n - c)
				rv.chargeScalar()
				return next
			}

		case rOpCallFib:
			dst, src1, src2 := instr.dst, instr.src1, instr.src2
			steps[i] = func(rv *regInterp) int {
				f := rv.top()
				fn := f.regs[src1]
				argv := f.regs[src2]
				if _, ok := rv.resolveFibCallee(fn); !ok {
					rv.callErr = fmt.Errorf("decodedInterp: unexpected fib callee %T", fn)
					return next
				}
				rv.pushFrame(argv, dst)
				return rv.top().ip
			}

		case rOpAdd:
			dst, src1, src2 := instr.dst, instr.src1, instr.src2
			steps[i] = func(rv *regInterp) int {
				if !rv.freezeCanon {
					rv.callErr = fmt.Errorf("decodedInterp: '+' rebind fallback not implemented (unreachable in this benchmark)")
					return next
				}
				f := rv.top()
				a := f.regs[src1].(core.Int).V
				b := f.regs[src2].(core.Int).V
				f.regs[dst] = core.BoxInt(a + b)
				rv.chargeScalar()
				return next
			}

		case rOpReturn:
			src1 := instr.src1
			steps[i] = func(rv *regInterp) int {
				f := rv.top()
				result := f.regs[src1]
				dst := f.dst
				rv.depth--
				rv.frames = rv.frames[:len(rv.frames)-1]
				if len(rv.frames) == 0 {
					rv.finalResult = result
					return 0
				}
				parent := rv.top()
				parent.regs[dst] = result
				return parent.ip
			}
		}
	}
	return steps
}

// runDecoded is prototype B's dispatch loop: fetch code[ip], call it, use its
// returned ip for whatever frame is now on top (unchanged for straight-line
// ops, the pushed callee for CALL, the popped caller for RETURN).
func (rv *regInterp) runDecoded(ctx context.Context, code []decodedStep, arg core.Value) (core.Value, error) {
	rv.reset()
	rv.pushFrame(arg, -1)

	for {
		if rv.budget--; rv.budget <= 0 {
			if err := rv.poll(ctx); err != nil {
				return nil, err
			}
		}
		rv.instrCount++

		f := rv.top()
		ip := f.ip
		f.ip = ip + 1

		next := code[ip](rv)
		if rv.callErr != nil {
			return nil, rv.callErr
		}
		if len(rv.frames) == 0 {
			return rv.finalResult, nil
		}
		rv.top().ip = next
	}
}

// ---------------------------------------------------------------------------
// Control arm: a stack-addressed twin of the register form above. Same ~10
// specialized opcodes, same explicit frame stack, same taxes, same mechanical
// derivation via parseFibBody — the only thing that differs from regInstr is
// that operands are addressed by push/pop into the frame's fixed 4-slot value
// window instead of by dst/src1/src2 register index. This isolates the
// encoding delta (stack-specialized vs register) from the specialization
// confound (stack-specialized vs production arm=stack, a general ~45-opcode
// interpreter with try/catch, generation checks, and generic Value dispatch).
// ---------------------------------------------------------------------------

type stkOp uint8

const (
	stOpLT          stkOp = iota // push(local(0) < constVal)
	stOpJumpIfFalse              // if !truthy(pop()) { ip = target }
	stOpGetLocal                 // push(local(0))
	stOpJump                     // ip = target
	stOpFreezeAdd                // resolve sym ("+"), record canonical flag for stOpAdd
	stOpGetFib                   // push(resolve sym ("fib"))
	stOpSub                      // push(local(0) - constVal)
	stOpCallFib                  // arg,fn := pop(),pop(); push(call fib(arg))
	stOpAdd                      // b,a := pop(),pop(); push(a+b), freeze-checked
	stOpReturn                   // return pop()
)

type stkInstr struct {
	op       stkOp
	target   int
	constVal core.Value
	sym      string
}

// translateFibBodyStack mechanically translates body's real Code/Fused into
// the stack-form IR above, using the same parseFibBody fields translateFibBody
// uses — the two translators share one derivation, so they can never silently
// disagree about what the real chunk says.
func translateFibBodyStack(tb testing.TB, body *vm.Chunk) []stkInstr {
	tb.Helper()
	f := parseFibBody(tb, body)

	prog := make([]stkInstr, 13)
	prog[0] = stkInstr{op: stOpLT, constVal: f.twoConst}
	prog[1] = stkInstr{op: stOpJumpIfFalse, target: f.thenJumpIfFalseTarget}
	prog[2] = stkInstr{op: stOpGetLocal}
	prog[3] = stkInstr{op: stOpJump, target: f.thenJumpTarget}
	prog[4] = stkInstr{op: stOpFreezeAdd, sym: f.plusSym}
	prog[5] = stkInstr{op: stOpGetFib, sym: f.fibSym}
	prog[6] = stkInstr{op: stOpSub, constVal: f.oneConst}
	prog[7] = stkInstr{op: stOpCallFib}
	prog[8] = stkInstr{op: stOpGetFib, sym: f.fibSym}
	prog[9] = stkInstr{op: stOpSub, constVal: f.twoConst}
	prog[10] = stkInstr{op: stOpCallFib}
	prog[11] = stkInstr{op: stOpAdd}
	prog[12] = stkInstr{op: stOpReturn}
	return prog
}

// stkFrame is one call frame: a fixed 4-slot value window (matches the real
// chunk's MaxStack=4, same as regFrame) addressed by push/pop instead of by
// register index. vals[0] holds the local n for the lifetime of the frame;
// operands above it are pushed and popped exactly like vm.stack.
type stkFrame struct {
	vals [4]core.Value
	top  int
	ip   int
}

func (f *stkFrame) push(v core.Value) { f.vals[f.top] = v; f.top++ }
func (f *stkFrame) pop() core.Value   { f.top--; return f.vals[f.top] }
func (f *stkFrame) local() core.Value { return f.vals[0] }

// stkInterp is arm=stack-specialized's engine: the stack-addressed twin of
// regInterp, carrying the identical cross-cutting taxes (poll cadence, alloc
// charge, call depth) so the only measured variable against arm=register is
// addressing mode.
type stkInterp struct {
	env  *core.Env
	body *vm.Chunk

	frames []stkFrame

	// freezeCanon mirrors regInterp.freezeCanon exactly — see its doc.
	freezeCanon bool

	budget     int
	pollCount  int
	instrCount int

	pendingAlloc int64
	allocCharges int

	depth int

	callErr     error
	finalResult core.Value
}

func newStkInterp(env *core.Env, body *vm.Chunk) *stkInterp {
	return &stkInterp{env: env, body: body, frames: make([]stkFrame, 0, 32)}
}

func (sv *stkInterp) top() *stkFrame { return &sv.frames[len(sv.frames)-1] }

func (sv *stkInterp) poll(ctx context.Context) error {
	sv.budget = regCheckInterval
	sv.pollCount++
	sv.pendingAlloc = 0
	return ctx.Err()
}

func (sv *stkInterp) chargeScalar() {
	sv.pendingAlloc += core.MeterScalarBytes
	sv.allocCharges++
}

func (sv *stkInterp) reset() {
	sv.frames = sv.frames[:0]
	sv.freezeCanon = false
	sv.budget = regCheckInterval
	sv.pollCount = 0
	sv.instrCount = 0
	sv.pendingAlloc = 0
	sv.allocCharges = 0
	sv.depth = 0
	sv.callErr = nil
	sv.finalResult = nil
}

// pushFrame mirrors regInterp.pushFrame's idiom exactly: append a zero-value
// frame, then patch it in place through a pointer into the slice, rather than
// building a populated frame on the stack first and copying it in via append
// — the two twins must pay the same per-call frame-push cost so the encoding
// delta measures addressing, not an incidental idiom difference.
func (sv *stkInterp) pushFrame(arg core.Value) {
	sv.frames = append(sv.frames, stkFrame{})
	f := &sv.frames[len(sv.frames)-1]
	f.push(arg)
	f.ip = 0
	sv.depth++
}

// resolveFibCallee mirrors regInterp.resolveFibCallee exactly — see its doc.
func (sv *stkInterp) resolveFibCallee(fn core.Value) (*vm.Closure, bool) {
	cl, ok := fn.(*vm.Closure)
	if !ok || cl.Chunk != sv.body {
		return nil, false
	}
	return cl, true
}

// run is arm=stack-specialized's dispatch loop: the same big switch as
// regInterp.runSwitch, but every operand is read via push/pop on the current
// frame's own value window instead of an absolute register index.
func (sv *stkInterp) run(ctx context.Context, code []stkInstr, arg core.Value) (core.Value, error) {
	sv.reset()
	sv.pushFrame(arg)

	for {
		if sv.budget--; sv.budget <= 0 {
			if err := sv.poll(ctx); err != nil {
				return nil, err
			}
		}
		sv.instrCount++

		f := &sv.frames[len(sv.frames)-1]
		instr := code[f.ip]
		f.ip++

		switch instr.op {
		case stOpLT:
			n := f.local().(core.Int).V
			c := instr.constVal.(core.Int).V
			f.push(core.BoxBool(n < c))
			sv.chargeScalar()

		case stOpJumpIfFalse:
			if !core.IsTruthy(f.pop()) {
				f.ip = instr.target
			}

		case stOpGetLocal:
			f.push(f.local())

		case stOpJump:
			f.ip = instr.target

		case stOpFreezeAdd:
			_, _, canon := sv.env.GetCanonical(instr.sym)
			sv.freezeCanon = canon

		case stOpGetFib:
			v, _, _ := sv.env.GetCanonical(instr.sym)
			f.push(v)

		case stOpSub:
			n := f.local().(core.Int).V
			c := instr.constVal.(core.Int).V
			f.push(core.BoxInt(n - c))
			sv.chargeScalar()

		case stOpCallFib:
			argv := f.pop()
			fn := f.pop()
			if _, ok := sv.resolveFibCallee(fn); !ok {
				return nil, fmt.Errorf("stkInterp: unexpected fib callee %T", fn)
			}
			sv.pushFrame(argv)

		case stOpAdd:
			if !sv.freezeCanon {
				return nil, fmt.Errorf("stkInterp: '+' rebind fallback not implemented (unreachable in this benchmark)")
			}
			b := f.pop().(core.Int).V
			a := f.pop().(core.Int).V
			f.push(core.BoxInt(a + b))
			sv.chargeScalar()

		case stOpReturn:
			result := f.pop()
			sv.depth--
			sv.frames = sv.frames[:len(sv.frames)-1]
			if len(sv.frames) == 0 {
				return result, nil
			}
			sv.frames[len(sv.frames)-1].push(result)
		}
	}
}

// ---------------------------------------------------------------------------
// Fifth arm: the empty cell of the 2x2 (addressing x dispatch) — stack
// addressing with pre-decoded closure dispatch. compileStkDecoded applies the
// exact treatment compileDecoded applies to regInstr, to stkInstr instead:
// same flat []func stream, operands resolved at build time, no nested thunks.
// ---------------------------------------------------------------------------

type stkDecodedStep func(sv *stkInterp) int

// compileStkDecoded mirrors compileDecoded: one specialized closure per
// stkInstr, sharing stkInterp/stkFrame with run so arm=stack-specialized and
// arm=stack-specialized-predecoded differ only in dispatch mechanism.
func compileStkDecoded(prog []stkInstr) []stkDecodedStep {
	steps := make([]stkDecodedStep, len(prog))
	for i, instr := range prog {
		next := i + 1
		switch instr.op {
		case stOpLT:
			c := instr.constVal.(core.Int).V
			steps[i] = func(sv *stkInterp) int {
				f := sv.top()
				n := f.local().(core.Int).V
				f.push(core.BoxBool(n < c))
				sv.chargeScalar()
				return next
			}

		case stOpJumpIfFalse:
			target := instr.target
			steps[i] = func(sv *stkInterp) int {
				if !core.IsTruthy(sv.top().pop()) {
					return target
				}
				return next
			}

		case stOpGetLocal:
			steps[i] = func(sv *stkInterp) int {
				f := sv.top()
				f.push(f.local())
				return next
			}

		case stOpJump:
			target := instr.target
			steps[i] = func(sv *stkInterp) int { return target }

		case stOpFreezeAdd:
			sym := instr.sym
			steps[i] = func(sv *stkInterp) int {
				_, _, canon := sv.env.GetCanonical(sym)
				sv.freezeCanon = canon
				return next
			}

		case stOpGetFib:
			sym := instr.sym
			steps[i] = func(sv *stkInterp) int {
				v, _, _ := sv.env.GetCanonical(sym)
				sv.top().push(v)
				return next
			}

		case stOpSub:
			c := instr.constVal.(core.Int).V
			steps[i] = func(sv *stkInterp) int {
				f := sv.top()
				n := f.local().(core.Int).V
				f.push(core.BoxInt(n - c))
				sv.chargeScalar()
				return next
			}

		case stOpCallFib:
			steps[i] = func(sv *stkInterp) int {
				f := sv.top()
				argv := f.pop()
				fn := f.pop()
				if _, ok := sv.resolveFibCallee(fn); !ok {
					sv.callErr = fmt.Errorf("stkDecodedInterp: unexpected fib callee %T", fn)
					return next
				}
				sv.pushFrame(argv)
				return sv.top().ip
			}

		case stOpAdd:
			steps[i] = func(sv *stkInterp) int {
				if !sv.freezeCanon {
					sv.callErr = fmt.Errorf("stkDecodedInterp: '+' rebind fallback not implemented (unreachable in this benchmark)")
					return next
				}
				f := sv.top()
				b := f.pop().(core.Int).V
				a := f.pop().(core.Int).V
				f.push(core.BoxInt(a + b))
				sv.chargeScalar()
				return next
			}

		case stOpReturn:
			steps[i] = func(sv *stkInterp) int {
				f := sv.top()
				result := f.pop()
				sv.depth--
				sv.frames = sv.frames[:len(sv.frames)-1]
				if len(sv.frames) == 0 {
					sv.finalResult = result
					return 0
				}
				parent := sv.top()
				parent.push(result)
				return parent.ip
			}
		}
	}
	return steps
}

// runDecoded is arm=stack-specialized-predecoded's dispatch loop: fetch
// code[ip], call it, use its returned ip for whatever frame is now on top —
// mirrors regInterp.runDecoded exactly, substituting stkFrame's push/pop
// window for the register array.
func (sv *stkInterp) runDecoded(ctx context.Context, code []stkDecodedStep, arg core.Value) (core.Value, error) {
	sv.reset()
	sv.pushFrame(arg)

	for {
		if sv.budget--; sv.budget <= 0 {
			if err := sv.poll(ctx); err != nil {
				return nil, err
			}
		}
		sv.instrCount++

		f := sv.top()
		ip := f.ip
		f.ip = ip + 1

		next := code[ip](sv)
		if sv.callErr != nil {
			return nil, sv.callErr
		}
		if len(sv.frames) == 0 {
			return sv.finalResult, nil
		}
		sv.top().ip = next
	}
}

// ---------------------------------------------------------------------------
// Correctness gate — must pass before any benchmark number counts.
// ---------------------------------------------------------------------------

func TestFibDispatchFormsCorrectness(t *testing.T) {
	ctx := context.Background()

	stackEnv := newBenchEnv()
	defForms, err := core.Read(fibSrc)
	require.NoError(t, err)
	defChunks, err := compiler.CompileAll(defForms)
	require.NoError(t, err)
	require.Len(t, defChunks, 1)
	defChunk := defChunks[0]
	require.NoError(t, defChunk.Validate())

	stackVM := vm.New(stackEnv)
	_, err = stackVM.Run(ctx, defChunk)
	require.NoError(t, err)

	fibVal, found := stackEnv.Get("fib")
	require.True(t, found)
	fibClosure, ok := fibVal.(*vm.Closure)
	require.True(t, ok)
	body := fibClosure.Chunk

	prog := translateFibBody(t, body)
	decoded := compileDecoded(prog)
	stkProg := translateFibBodyStack(t, body)
	stkDecoded := compileStkDecoded(stkProg)

	treeEnv := newBenchEnv()
	treeEval := core.NewEvaluator()
	_, err = treeEval.Eval(ctx, defForms[0], treeEnv)
	require.NoError(t, err)

	regA := newRegInterp(stackEnv, body)
	regB := newRegInterp(stackEnv, body)
	stk := newStkInterp(stackEnv, body)
	stkB := newStkInterp(stackEnv, body)

	for n := 0; n <= 20; n++ {
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			want := fibonacci(n)

			stackResult, err := stackVM.Apply(ctx, fibClosure, []core.Value{core.Int{V: int64(n)}}, stackEnv)
			require.NoError(t, err)
			si, ok := stackResult.(core.Int)
			require.True(t, ok, "stack arm: expected core.Int, got %T", stackResult)
			require.Equal(t, want, si.V, "stack arm result")

			regResult, err := regA.runSwitch(ctx, prog, core.Int{V: int64(n)})
			require.NoError(t, err)
			ri, ok := regResult.(core.Int)
			require.True(t, ok, "register arm: expected core.Int, got %T", regResult)
			require.Equal(t, want, ri.V, "register arm result")
			require.Zero(t, regA.depth, "register arm: depth did not return to 0")

			decResult, err := regB.runDecoded(ctx, decoded, core.Int{V: int64(n)})
			require.NoError(t, err)
			di, ok := decResult.(core.Int)
			require.True(t, ok, "pre-decoded arm: expected core.Int, got %T", decResult)
			require.Equal(t, want, di.V, "pre-decoded arm result")
			require.Zero(t, regB.depth, "pre-decoded arm: depth did not return to 0")

			stkResult, err := stk.run(ctx, stkProg, core.Int{V: int64(n)})
			require.NoError(t, err)
			ski, ok := stkResult.(core.Int)
			require.True(t, ok, "stack-specialized arm: expected core.Int, got %T", stkResult)
			require.Equal(t, want, ski.V, "stack-specialized arm result")
			require.Zero(t, stk.depth, "stack-specialized arm: depth did not return to 0")

			stkDecResult, err := stkB.runDecoded(ctx, stkDecoded, core.Int{V: int64(n)})
			require.NoError(t, err)
			skdi, ok := stkDecResult.(core.Int)
			require.True(t, ok, "stack-specialized-predecoded arm: expected core.Int, got %T", stkDecResult)
			require.Equal(t, want, skdi.V, "stack-specialized-predecoded arm result")
			require.Zero(t, stkB.depth, "stack-specialized-predecoded arm: depth did not return to 0")

			treeForms, err := core.Read(fmt.Sprintf("(fib %d)", n))
			require.NoError(t, err)
			require.Len(t, treeForms, 1)
			treeResult, err := treeEval.Eval(ctx, treeForms[0], treeEnv)
			require.NoError(t, err)
			ti, ok := treeResult.(core.Int)
			require.True(t, ok, "tree-walker: expected core.Int, got %T", treeResult)
			require.Equal(t, want, ti.V, "tree-walker result")

			// Tax parity, self-consistent across all four prototypes (the
			// real VM's budget/pendingAlloc counters are unexported and
			// unobservable from this package — see the report, this does NOT
			// check the production arm=stack VM's own counters) and against
			// the analytically expected shape derived from the pinned
			// disassembly.
			wantInstr := protoInstrCount(n)
			require.Equal(t, wantInstr, regA.instrCount, "n=%d: register arm instruction count", n)
			require.Equal(t, wantInstr, regB.instrCount, "n=%d: pre-decoded arm instruction count", n)
			require.Equal(t, wantInstr, stk.instrCount, "n=%d: stack-specialized arm instruction count", n)
			require.Equal(t, wantInstr, stkB.instrCount, "n=%d: stack-specialized-predecoded arm instruction count", n)
			require.Equal(t, regA.pollCount, regB.pollCount, "n=%d: poll count mismatch between prototypes", n)
			require.Equal(t, regA.pollCount, stk.pollCount, "n=%d: poll count mismatch, register vs stack-specialized", n)
			require.Equal(t, regA.pollCount, stkB.pollCount, "n=%d: poll count mismatch, register vs stack-specialized-predecoded", n)
			require.Equal(t, wantInstr/regCheckInterval, regA.pollCount, "n=%d: poll count vs analytical expectation", n)

			wantAlloc := expectedAllocCharges(n)
			require.Equal(t, wantAlloc, regA.allocCharges, "n=%d: register arm alloc-charge count", n)
			require.Equal(t, wantAlloc, regB.allocCharges, "n=%d: pre-decoded arm alloc-charge count", n)
			require.Equal(t, wantAlloc, stk.allocCharges, "n=%d: stack-specialized arm alloc-charge count", n)
			require.Equal(t, wantAlloc, stkB.allocCharges, "n=%d: stack-specialized-predecoded arm alloc-charge count", n)
		})
	}
}

// ---------------------------------------------------------------------------
// Measurement — one parent benchmark, five interleaved sub-arms.
// ---------------------------------------------------------------------------

// sinkStack, sinkRegister, sinkPredecoded, sinkStackSpecialized, and
// sinkStackSpecializedPredecoded are DCE guards: each arm's final result is
// stored here, so the compiler cannot eliminate the computation as dead.
var (
	sinkStack                      core.Value
	sinkRegister                   core.Value
	sinkPredecoded                 core.Value
	sinkStackSpecialized           core.Value
	sinkStackSpecializedPredecoded core.Value
)

// fibDispatchReps is the number of interleaving repetitions
// BenchmarkFibDispatchForms drives internally. Run with -count=1: -count
// repeats each b.Run leaf in place (b.Run("arm=x", fn) runs fn's own full
// calibration-and-measurement cycle to completion before the parent's Go code
// reaches the next b.Run call), not the parent function itself — so a fixed
// -count=N here would produce N consecutive samples of arm 1, then N of arm
// 2, and so on, never interleaved in time regardless of any shuffle applied
// once per parent invocation. Verified directly against this file's own past
// output: `grep -oP 'arm=[a-z-]+' <file> | uniq -c` showed 20 consecutive
// hits per arm, never mixed.
//
// The fix is to do the repetition ourselves, inside the parent: each rep
// reshuffles and runs all five arms once, so within a rep the arms genuinely
// interleave in time, and across reps any monotonic drift (thermal ramp,
// frequency/cache state) spreads across every arm instead of concentrating on
// whichever arm happens to occupy a given block. Leaf names carry a rep=
// component so benchstat can still find each arm's samples: pool across reps
// with `benchstat -col /arm -ignore /rep` — the leading slash is required,
// since it names a benchmark-name key rather than a file-level config key, and
// `-ignore rep` silently leaves every rep its own single-sample row.
// Each decomposed pairwise delta needs its own `-filter` scoping; one
// invocation cannot produce them all, because benchstat picks a single base
// column per run.
const fibDispatchReps = 20

func BenchmarkFibDispatchForms(b *testing.B) {
	arms := []struct {
		name string
		fn   func(*testing.B)
	}{
		{"arm=stack", benchFibDispatchStack},
		{"arm=register", benchFibDispatchRegister},
		{"arm=predecoded", benchFibDispatchPredecoded},
		{"arm=stack-specialized", benchFibDispatchStackSpecialized},
		{"arm=stack-specialized-predecoded", benchFibDispatchStackSpecializedPredecoded},
	}
	for rep := range fibDispatchReps {
		rand.Shuffle(len(arms), func(i, j int) { arms[i], arms[j] = arms[j], arms[i] })
		for _, arm := range arms {
			b.Run(fmt.Sprintf("rep=%d/%s", rep, arm.name), arm.fn)
		}
	}
}

// benchFibDispatchStack is the landed floor at its best: compile once,
// Validate once, publish the site cache (including the fib body SubChunk,
// which EnsureSites does not reach on its own) and warm it with one call,
// all outside the timer — mirrors BenchmarkFoldedConstantLiteral.
func benchFibDispatchStack(b *testing.B) {
	ctx := context.Background()
	env := newBenchEnv()

	forms, err := core.Read(fmt.Sprintf("%s\n(fib %d)", fibSrc, fibN))
	require.NoError(b, err)
	chunks, err := compiler.CompileAll(forms)
	require.NoError(b, err)
	require.Len(b, chunks, 2)
	for _, c := range chunks {
		require.NoError(b, c.Validate())
	}
	defChunk, callChunk := chunks[0], chunks[1]
	require.NotEmpty(b, defChunk.SubChunks)
	defChunk.SubChunks[0].EnsureSites()
	callChunk.EnsureSites()

	v := vm.New(env)
	if _, err := v.Run(ctx, defChunk); err != nil {
		b.Fatal(err)
	}
	if _, err := v.Run(ctx, callChunk); err != nil { // warm-up: publishes site-cache entries
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	var result core.Value
	for range b.N {
		result, err = v.Run(ctx, callChunk)
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()

	sinkStack = result
	iv, ok := sinkStack.(core.Int)
	if !ok || iv.V != fibExpected {
		b.Fatalf("arm=stack: got %v, want %d", sinkStack, fibExpected)
	}
	b.ReportMetric(float64(stackInstrCount(fibN)), "instr/op")
}

// benchFibDispatchRegister is prototype A: compile+translate once outside the
// timer, then drive runSwitch repeatedly on a reused regInterp.
func benchFibDispatchRegister(b *testing.B) {
	ctx := context.Background()
	env := newBenchEnv()

	forms, err := core.Read(fibSrc)
	require.NoError(b, err)
	chunks, err := compiler.CompileAll(forms)
	require.NoError(b, err)
	require.Len(b, chunks, 1)
	require.NoError(b, chunks[0].Validate())

	v := vm.New(env)
	if _, err := v.Run(ctx, chunks[0]); err != nil {
		b.Fatal(err)
	}
	fibVal, found := env.Get("fib")
	require.True(b, found)
	fibClosure := fibVal.(*vm.Closure)
	prog := translateFibBody(b, fibClosure.Chunk)

	rv := newRegInterp(env, fibClosure.Chunk)
	arg := core.Int{V: fibN}

	b.ReportAllocs()
	b.ResetTimer()
	var result core.Value
	for range b.N {
		result, err = rv.runSwitch(ctx, prog, arg)
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()

	sinkRegister = result
	iv, ok := sinkRegister.(core.Int)
	if !ok || iv.V != fibExpected {
		b.Fatalf("arm=register: got %v, want %d", sinkRegister, fibExpected)
	}
	b.ReportMetric(float64(protoInstrCount(fibN)), "instr/op")
}

// benchFibDispatchPredecoded is prototype B: same setup as arm=register, plus
// one extra one-time compileDecoded pass (also outside the timer), then
// drives runDecoded.
func benchFibDispatchPredecoded(b *testing.B) {
	ctx := context.Background()
	env := newBenchEnv()

	forms, err := core.Read(fibSrc)
	require.NoError(b, err)
	chunks, err := compiler.CompileAll(forms)
	require.NoError(b, err)
	require.Len(b, chunks, 1)
	require.NoError(b, chunks[0].Validate())

	v := vm.New(env)
	if _, err := v.Run(ctx, chunks[0]); err != nil {
		b.Fatal(err)
	}
	fibVal, found := env.Get("fib")
	require.True(b, found)
	fibClosure := fibVal.(*vm.Closure)
	prog := translateFibBody(b, fibClosure.Chunk)
	decoded := compileDecoded(prog)

	rv := newRegInterp(env, fibClosure.Chunk)
	arg := core.Int{V: fibN}

	b.ReportAllocs()
	b.ResetTimer()
	var result core.Value
	for range b.N {
		result, err = rv.runDecoded(ctx, decoded, arg)
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()

	sinkPredecoded = result
	iv, ok := sinkPredecoded.(core.Int)
	if !ok || iv.V != fibExpected {
		b.Fatalf("arm=predecoded: got %v, want %d", sinkPredecoded, fibExpected)
	}
	b.ReportMetric(float64(protoInstrCount(fibN)), "instr/op")
}

// benchFibDispatchStackSpecialized is the control arm: same specialized
// opcode set and setup as arm=register, but stack-addressed via stkInterp.run
// instead of register-addressed via regInterp.runSwitch. Isolates the
// encoding delta from the specialization confound production's general
// ~45-opcode arm=stack carries (see the report).
func benchFibDispatchStackSpecialized(b *testing.B) {
	ctx := context.Background()
	env := newBenchEnv()

	forms, err := core.Read(fibSrc)
	require.NoError(b, err)
	chunks, err := compiler.CompileAll(forms)
	require.NoError(b, err)
	require.Len(b, chunks, 1)
	require.NoError(b, chunks[0].Validate())

	v := vm.New(env)
	if _, err := v.Run(ctx, chunks[0]); err != nil {
		b.Fatal(err)
	}
	fibVal, found := env.Get("fib")
	require.True(b, found)
	fibClosure := fibVal.(*vm.Closure)
	prog := translateFibBodyStack(b, fibClosure.Chunk)

	sv := newStkInterp(env, fibClosure.Chunk)
	arg := core.Int{V: fibN}

	b.ReportAllocs()
	b.ResetTimer()
	var result core.Value
	for range b.N {
		result, err = sv.run(ctx, prog, arg)
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()

	sinkStackSpecialized = result
	iv, ok := sinkStackSpecialized.(core.Int)
	if !ok || iv.V != fibExpected {
		b.Fatalf("arm=stack-specialized: got %v, want %d", sinkStackSpecialized, fibExpected)
	}
	b.ReportMetric(float64(protoInstrCount(fibN)), "instr/op")
}

// benchFibDispatchStackSpecializedPredecoded fills the 2x2's fourth cell:
// same setup as arm=stack-specialized, plus one extra one-time
// compileStkDecoded pass (also outside the timer), then drives runDecoded —
// stack addressing with closure dispatch, mirroring exactly how
// arm=predecoded relates to arm=register.
func benchFibDispatchStackSpecializedPredecoded(b *testing.B) {
	ctx := context.Background()
	env := newBenchEnv()

	forms, err := core.Read(fibSrc)
	require.NoError(b, err)
	chunks, err := compiler.CompileAll(forms)
	require.NoError(b, err)
	require.Len(b, chunks, 1)
	require.NoError(b, chunks[0].Validate())

	v := vm.New(env)
	if _, err := v.Run(ctx, chunks[0]); err != nil {
		b.Fatal(err)
	}
	fibVal, found := env.Get("fib")
	require.True(b, found)
	fibClosure := fibVal.(*vm.Closure)
	prog := translateFibBodyStack(b, fibClosure.Chunk)
	decoded := compileStkDecoded(prog)

	sv := newStkInterp(env, fibClosure.Chunk)
	arg := core.Int{V: fibN}

	b.ReportAllocs()
	b.ResetTimer()
	var result core.Value
	for range b.N {
		result, err = sv.runDecoded(ctx, decoded, arg)
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()

	sinkStackSpecializedPredecoded = result
	iv, ok := sinkStackSpecializedPredecoded.(core.Int)
	if !ok || iv.V != fibExpected {
		b.Fatalf("arm=stack-specialized-predecoded: got %v, want %d", sinkStackSpecializedPredecoded, fibExpected)
	}
	b.ReportMetric(float64(protoInstrCount(fibN)), "instr/op")
}
