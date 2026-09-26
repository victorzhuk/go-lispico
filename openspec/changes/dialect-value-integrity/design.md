## Context

Both probes ran against the current tree from a scratch module with a `replace` directive: the singleton mutation reproduced with an unchanged fingerprint, and the two vocabularies hashed equal.

## Decisions

- **Copy on read** for `Vocab()`: callers are construction-time (`applyVocabulary`, the lazy template build), so one map copy per engine or per template build is acceptable. Hot or repeated lookups use `VocabEntry(name)`.
- **Length-prefixed fields** (`len:value`) for every string in the digest, including ops. `%q` would also work; length prefixes avoid depending on quoting rules.
- **No version salt** in the digest: the contract already says it may change between releases, and the template registry is in-memory.
