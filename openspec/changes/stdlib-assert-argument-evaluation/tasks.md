## 1. Pin the defect

- [ ] 1.1 Add failing goldens for the argument shapes that expose the second evaluation — a quoted symbol message, a list message, a symbol condition, and a list condition — asserting the assertion message rather than a lookup or call error, under both the tree-walker and the VM and under both dialects.
- [ ] 1.2 Verify the existing string-message and no-message goldens already pass, so the fix is scoped to the shapes that actually change.

## 2. Fix the evaluation

- [ ] 2.1 Use the arguments the apply site already evaluated; verify no evaluator re-entry remains on either the condition or the message path.
- [ ] 2.2 Verify truthiness, arity errors, and the returned value on success are unchanged, and that the error stays a domain error with its existing code.

## 3. Verify

- [ ] 3.1 Verify the inventory rows for `assert` still match the code after the re-entry is removed, including the callback-owned and formatting dispositions.
- [ ] 3.2 Run the repository test suite, the race suite over `core`, `plugins` and `runtime`, `go vet`, and the linter; verify every command exits successfully.
