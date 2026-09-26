## Context

`applyVocabulary` on a full-base dialect keeps every registered GoFunc and then applies vocab entries on top. An entry `name → name` resolves the canonical GoFunc and `env.Set`s it back under the same name. `setVar` assigns `cell.canonical = false` on that write. The lazy template path registers the same name twice, first non-canonical and then canonical.

## Decisions

### Drop identity entries rather than special-casing them

The entries carry no meaning on a full base. Skipping them inside `applyVocabulary` would hide the problem instead of removing it, and a future empty-base policy dialect would still need them as allowlist entries, where they are meaningful.

### Single source for the stock definition

Applied after `dialect-declarative-spec`: one unexported `clSpec core.DialectSpec`; `stockDialect` builds it once with `core.NewDialect`, and tests reach `clSpec` through `export_test.go`.

## Risks

- A CL program that depends on the cell of `cons`/`list`/... being non-canonical: nothing reads the canonical flag outside VM native ops, which do not include these names. Verify with the CL corpus on both evaluators.
