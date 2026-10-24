package inventory

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// benchMatch models go test -bench matching: the selector and the benchmark
// name are both split on /, and each selector component is an anchored regex
// matched against the name component at the same position.
func benchMatch(t *testing.T, sel, name string) bool {
	t.Helper()
	sc := strings.Split(sel, "/")
	nc := strings.Split(name, "/")
	if len(nc) < len(sc) {
		return false
	}
	for i, part := range sc {
		if !regexp.MustCompile(part).MatchString(nc[i]) {
			return false
		}
	}
	return true
}

// TestVerdictSelectorsSelectGoldsetBenchmarks pins the release workflow's
// verdict -bench selectors to the Benchmark-prefixed family alternation. The
// unprefixed form selected nothing: go matches selector components against the
// benchmark's full name, which carries the Benchmark prefix.
func TestVerdictSelectorsSelectGoldsetBenchmarks(t *testing.T) {
	const verdict = `^Benchmark(Goldset|GoldsetParse|GoldsetCall)$`
	const legacy = `^(Goldset|GoldsetParse|GoldsetCall)/`
	raw, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatalf("read release.yml: %v", err)
	}
	text := string(raw)
	if strings.Contains(text, legacy) {
		t.Errorf("legacy unprefixed verdict selector %q still present in release.yml", legacy)
	}
	if !strings.Contains(text, "bench_sel='"+verdict+"'") {
		t.Errorf("bench_sel default in release.yml = %q expected", verdict)
	}
	if n := strings.Count(text, "-bench '"+verdict+"'"); n != 2 {
		t.Errorf("doubled-benchtime rerun uses -bench %q on %d lines, want 2 (eval, vm)", verdict, n)
	}
	if !strings.Contains(text, "bench_sel='.'") {
		t.Error("classification_only capture selector bench_sel='.' missing from release.yml")
	}

	for _, name := range []string{"BenchmarkGoldset/cell", "BenchmarkGoldsetParse/safe-parse", "BenchmarkGoldsetCall/call-boundary"} {
		if !benchMatch(t, verdict, name) {
			t.Errorf("verdict selector does not select %s", name)
		}
	}
	for _, name := range []string{"BenchmarkUnrelated/cell", "BenchmarkGoldsetCL/function-cells", "BenchmarkGoldsetConcurrent/distinct-closures"} {
		if benchMatch(t, verdict, name) {
			t.Errorf("verdict selector selects unrelated benchmark %s", name)
		}
	}
}
