## 1. Pin the baseline

- [ ] 1.1 Capture an interleaved baseline (≥10 counts, one session) for the
      article-harness Rule/Callback/Call rows and goldset both modes, plus a
      new micro benchmark: a compiled function returning
      `{:model :large :tools [:read :grep]}`, recording ns/op, B/op,
      allocs/op.
- [ ] 1.2 Disassemble the micro's chunk and record the instruction sequence
      the literal compiles to today (element pushes + `OpMake*` +
      `OpStructEnter/Leave`), as the before-picture.

## 2. Constant detection and folding

- [ ] 2.1 Add a compile-time constant classifier: `Nil`, `Bool`, `Int`,
      `Float`, `String`, `Keyword` are constants; `Vector`/`HashMap`/list
      literals are constants iff all elements (and keys and values) classify;
      `Symbol` and any form with a head do not.
- [ ] 2.2 In the `Vector` and `HashMap` compile cases, when the literal
      classifies as constant, build the value once using the same
      constructors the runtime path uses, register it via `AddConstant`
      (existing `Equals` dedup applies), and emit the charged-constant
      reference instead of the element/`OpMake*` sequence. Nested constant
      literals fold into the parent; mixed literals compile as today.
- [ ] 2.3 Compute the folded value's deep bytes (`core.ValueDeepBytes`) and
      structural depth once at compile time and store them alongside the
      constant index. Do not bake any engine limit into the chunk.

## 3. Charged emission in the VM

- [ ] 3.1 Add the charged-constant instruction (opcode + side table). On
      execution: push the constant, add the precomputed deep bytes to the
      per-evaluation allocation ledger, compare the precomputed depth against
      the running VM's `maxStructuralDepth`, and fail with the same terminal
      `ResourceLimitError` the construction path raises today.
- [ ] 3.2 Extend `Chunk.Validate()` for the new opcode: constant index in
      range, side-table entry present. Re-audit against the
      validation-completeness lesson (every operand-carrying opcode needs a
      Validate case or the hot loop panics).
- [ ] 3.3 Confirm the tree-walker path is untouched, and the chunk retained
      charge (`chunkDeepBytes`) picks up folded constants through the
      existing constant-pool sum.

## 4. Parity and limits

- [ ] 4.1 Cross-val: `TestVMVsTreeWalker` green; add a case evaluating a
      function returning nested all-constant literals under both evaluators.
- [ ] 4.2 Ledger parity test: a program whose folded literals would exceed a
      small `MaxAllocationBytes` fails with `ResourceLimitError` identically
      under VM and tree-walker.
- [ ] 4.3 Depth parity test: a folded literal deeper than a configured
      `MaxStructuralDepth` fails identically under both evaluators, and the
      error remains non-catchable.
- [ ] 4.4 Sharing test: two evaluations of the same folded literal return
      `Equals` values; document (test comment) that pointer identity is
      shared by design.

## 5. Measure

- [ ] 5.1 Re-run 1.1 in the same interleaved session as the baseline. Success
      criteria: micro B/op reduced to boundary-only cost (construction
      column gone); article Rule allocs and bytes drop by the construction
      share (~60% of engine-side bytes); Rule ns/op improves double-digit
      percent; no cell regresses.
- [ ] 5.2 Record the disassembly after: the literal is a single charged
      constant reference.

## 6. Verify

- [ ] 6.1 `go build ./...`, `go vet ./...`, `gofmt -l .`, `golangci-lint run`
      clean.
- [ ] 6.2 Full suite + `-race`; crossval; goldset both modes.
- [ ] 6.3 `cmd/perfgate` non-regression (one-sided) green; investigate any
      positive-delta cell in a paired re-measure before dismissing.
