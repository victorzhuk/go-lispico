## 1. Behavior contracts

- [ ] 1.1 Red test (CL dialect): `Use(stdlib)` → `(defun + (a b) 999)` →
  assert `(+ 1 2) == 999` → `Use(data.New())` → assert `(+ 1 2)` still `999`.
  Cover both the tree-walker and the VM.
- [ ] 1.2 Red test: the same for a non-operator builtin — `(defun map …)` then
  an unrelated `Use()` — the redefinition survives.
- [ ] 1.3 Characterization: a freshly loaded plugin's `GoFunc`s are still
  callable in head position after the guard (bridge still fires for
  absent/unchanged function cells); the VM native-op fast path still fires for
  un-redefined canonical operators.
- [ ] 1.4 Characterization: `ReloadPlugin(stdlib)` restores the canonical
  operator (a reload of the owning plugin is allowed to reset a redefinition).

## 2. Implementation

- [ ] 2.1 `applyVocabulary` Lisp-2 bridge: before `SetFunc`/`SetFuncCanonical`,
  read the current function cell (`GetFunc`); skip when it is present and not
  `Equals` to the value-cell `GoFunc` being bridged.
- [ ] 2.2 Preserve the canonical-vs-plain write split for the cases that do
  bridge (native operators stay canonical so the VM fast path is intact).

## 3. Integration

- [ ] 3.1 `go test ./... -race` green.
- [ ] 3.2 `GOLDSET_MODE=vm` goldset gate non-increasing (the guard is a read on
  the setup path, not the eval hot path).
- [ ] 3.3 Crossval unaffected; add the redefinition-survival case to the
  dialect/native-op test coverage that already exists
  (`runtime/dialect_native_op_test.go`).

## 4. Verification

- [ ] 4.1 `openspec validate --strict engine-preserve-operator-redefinition`.
- [ ] 4.2 CHANGELOG `[Unreleased]` note under Fixed.
