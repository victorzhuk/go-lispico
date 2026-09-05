# perfgate testdata

Each `profile-<run id>/` directory is a checked-in classification profile with
its own README. Fixtures that belong to no single profile live here.

## `unpaired-single-group.csv`

Real output from the pinned benchstat over a pair it declined to compare: two
runs whose `cpu:` lines disagree. benchstat exits 0 and emits six single-group
blocks — two configurations times three metrics — whose metric headers carry 3
columns ending `CI` instead of the paired 7 ending `vs base`,`P`. Nothing in
the exit code says the comparison never happened, which is why the parser
refuses this shape by name.

Produced by, and reproducible with:

1. Copy `profile-30637802780/bench-evaluator.txt` and `.../bench-vm.txt` to a
   scratch directory outside the repository.
2. In the scratch `bench-evaluator.txt` only, rewrite every `cpu:` line to
   `cpu: INTEL(R) XEON(R) PLATINUM 8573C` — the CPU `profile-30614184386`
   records, so the fixture quotes a machine this project has really used. The
   committed profile files are never modified.
3. From that scratch directory, run
   `go run golang.org/x/perf/cmd/benchstat@v0.0.0-20260709024250-82a0b07e230d -format csv bench-evaluator.txt bench-vm.txt`
   and commit its stdout verbatim. Its stderr — `B65: summaries must be >0 to
   compute geomean`, twice — is about the geomean, not about pairing.

The fixture is committed rather than generated at test time so no unit test
shells out to benchstat.
