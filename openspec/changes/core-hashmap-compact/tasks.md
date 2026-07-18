## 1. Format-free hash keys

- [ ] 1.1 Replace `hashKey{typ, val string}` with `{typ uint8, num uint64, str string}`; port `toHashKey` (no strconv/fmt), normalize NaN float keys to one bit pattern; ordering helper `(typ, num, str)`.
- [ ] 1.2 Tests: key identity matrix (Int vs Float distinctness, NaN=NaN, ±0.0 policy documented), `AllocsPerRun` zero for numeric-key `Get`.

## 2. Entry storage and small form

- [ ] 2.1 Introduce `entry` storage: sorted `entries []entry` small form, single `map[hashKey]entry` large form; promotion at threshold inside `Set`/`Assoc`; delete the parallel `keys` map.
- [ ] 2.2 Port `Get`/`Set`/`Assoc`/`Dissoc`/`Len`/`Each`/`Pairs`/`String`/`Equals` over both forms; iteration order deterministic `(typ, num, str)` at both.
- [ ] 2.3 Microbenchmark scan-vs-map at 4/8/16 keys to freeze the threshold constant; record result in the commit.
- [ ] 2.4 Tests: promotion boundary (8→9, then Dissoc below), representation-blind `Equals` (small vs promoted with same pairs), immutability of `Assoc`/`Dissoc` at both forms, VM `OpMakeMap` and quasiquote map paths, `data/json` round-trip incl. >8-key objects.

## 3. Order-sensitive fallout

- [ ] 3.1 Audit goldens/tests for numeric-key iteration order; update any that pinned the old lexicographic artifact, with reasoning in the change.

## 4. Verify

- [ ] 4.1 `go test ./...`, `-race` suites, crossval parity green.
- [ ] 4.2 Goldset gate: data-dominated and hot cells alloc count/bytes decreasing, latency non-regressing.
- [ ] 4.3 Bench evidence recorded: rule bench ns/op + allocs before/after; `AllocsPerRun` assertions from tasks 1.2/2.4 committed as tests.
