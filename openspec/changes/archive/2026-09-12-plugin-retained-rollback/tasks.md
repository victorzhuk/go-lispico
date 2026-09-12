## 0. Baseline

- [x] 0.1 Reproduce the current leak — a failed `Use` leaves the meter charged for cells `rollbackPluginUse` removed — as a recorded baseline failure, confirm both `OPEN:` decisions are resolved in the proposal, and record bounded focused test commands.

## 1. Pin retained failure contracts

- [x] 1.1 Add regressions for failed initialization with new names, settlement denial on a later meter and on the host meter, capacity rejection during initialization, a concurrent host new binding, and the adoption case under the recorded decision; verify exact charges and releases through existing accounting fixtures and that the baseline fails.

## 2. Settle retained ownership

- [x] 2.1 Track operation-owned retained capacity in the registration journal and release only removed charges, keeping external meter calls outside env and lazy locks; verify capacity rejection, partial settlement failure, and no double release.
- [x] 2.2 Order retained settlement against journal abort in `Use` and `ReloadPlugin` so a failed operation never charges the meter for cells it removes; verify error precedence, unused lease return, and every section 1 case.

## 3. Validate and document

- [x] 3.1 Amend ADR 0012 and the `CONTEXT.md` **Owned capacity** entry with the chosen release path and adoption rule, add a CHANGELOG `[Unreleased]` entry, and run `go test -timeout 2m -p 2 -parallel 2 ./core ./runtime -run 'Test(Meter|Settle|Use|ReloadPlugin|Retained)'`, the same under `-race`, `make lint`, and `openspec validate plugin-retained-rollback --strict --json`.
