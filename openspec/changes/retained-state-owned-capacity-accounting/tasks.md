## 1. Behavior contracts

- [ ] 1.1 Red tests: slot ceiling, byte ceiling, delete-doesn't-release, rebuild-releases, closure-no-double-count (per scenarios above). Characterization coverage records the unchanged baseline.
- [ ] 1.2 Race test: concurrent binding writes to one child env under `-race`.

## 2. Implementation

- [ ] 2.1 Add `MaxRetainedBytesPerEnv`, `MaxRetainedSlotsPerEnv` to `runtime.ResourceLimits`; defaults 32 MiB / 100,000.
- [ ] 2.2 Add owned-capacity counter (bytes + slots) to `core.Env`; thread the limits via `engineConfig` into every env created through `Child` / `NewEnv` on the engine path.
- [ ] 2.3 Charge on new-slot write in `Env.Set` / `Env.SetFunc`; do not charge on rebind through an existing `Cell`.
- [ ] 2.4 Delete tombstones the slot without decrementing the counter.
- [ ] 2.5 Implement `(*Env).Rebuild()` — fresh env from live bindings, atomic swap; release old backing.
- [ ] 2.6 Wire VM per-call frame env allocation (`needsCallEnv` path) to the same counter.

## 3. Integration

- [ ] 3.1 `go test ./... -race`.
- [ ] 3.2 Verify the existing allocation tests (HashMap / Env alloc assertions) still hold.

## 4. Verification

- [ ] 4.1 `openspec validate --strict retained-state-owned-capacity-accounting`.
