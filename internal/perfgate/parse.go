package perfgate

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// ParseBenchstatCSV parses the output of
// `benchstat -format csv old.txt new.txt` into per-cell comparisons.
// benchstat emits one block per metric (sec/op, B/op, allocs/op) separated
// by a blank line, with a trailing geomean summary row this skips.
//
// Verified against real output from the pinned benchstat
// (golang.org/x/perf/cmd/benchstat@v0.0.0-20260709024250-82a0b07e230d, the
// version cmd/perfgate/main.go shells out to), comparing two `go test -bench`
// runs of existing go-lispico benchmarks (core.BenchmarkEval_SimpleArith,
// core.BenchmarkEval_Fibonacci). Real output includes `pkg:` and `cpu:`
// preamble lines alongside `goos:`/`goarch:` — the hand-authored fixture in
// perfgate_test.go had none, so the original goos:/goarch:-only filter
// passed the fixture but failed on real output with "wrong number of
// fields"; parseBlock now filters all four known preamble prefixes.
func ParseBenchstatCSV(data []byte) (map[string]CellComparison, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	blocks := strings.Split(strings.TrimSpace(text), "\n\n")

	cells := make(map[string]CellComparison)
	for _, block := range blocks {
		if err := parseBlock(block, cells); err != nil {
			return nil, err
		}
	}
	return cells, nil
}

// benchstatPreamblePrefixes are the environment lines benchstat prints
// before the first CSV block (verified against real
// `benchstat -format csv` output, which also includes pkg: and cpu:
// alongside goos:/goarch: — the fixture-only version of this parser missed
// pkg:/cpu: and broke on real output with "wrong number of fields").
var benchstatPreamblePrefixes = []string{"goos:", "goarch:", "pkg:", "cpu:"}

func parseBlock(block string, cells map[string]CellComparison) error {
	var lines []string
	for line := range strings.SplitSeq(block, "\n") {
		if line == "" || hasAnyPrefix(line, benchstatPreamblePrefixes) {
			continue
		}
		lines = append(lines, line)
	}
	if len(lines) < 3 {
		return nil
	}

	records, err := csv.NewReader(strings.NewReader(strings.Join(lines, "\n"))).ReadAll()
	if err != nil {
		return fmt.Errorf("perfgate: parse benchstat csv block: %w", err)
	}
	if len(records) < 3 {
		return fmt.Errorf("perfgate: benchstat csv block has %d rows, want at least 3 (2 headers + data)", len(records))
	}

	header := records[1]
	if len(header) < 2 {
		return fmt.Errorf("perfgate: benchstat csv header row too short: %v", header)
	}
	metric := header[1]

	for _, row := range records[2:] {
		if len(row) < 7 {
			return fmt.Errorf("perfgate: benchstat csv data row too short: %v", row)
		}
		name := row[0]
		if name == "geomean" {
			continue
		}
		m, err := parseMetricRow(row)
		if err != nil {
			return fmt.Errorf("perfgate: cell %q: %w", name, err)
		}
		cell := cells[name]
		cell.Name = name
		switch metric {
		case "sec/op":
			cell.Latency = m
		case "B/op":
			cell.Bytes = m
		case "allocs/op":
			cell.Allocs = m
		default:
			return fmt.Errorf("perfgate: cell %q: unsupported benchstat metric %q", name, metric)
		}
		cells[name] = cell
	}
	return nil
}

func parseMetricRow(row []string) (MetricResult, error) {
	old, err := strconv.ParseFloat(row[1], 64)
	if err != nil {
		return MetricResult{}, fmt.Errorf("parse old value %q: %w", row[1], err)
	}
	newVal, err := strconv.ParseFloat(row[3], 64)
	if err != nil {
		return MetricResult{}, fmt.Errorf("parse new value %q: %w", row[3], err)
	}

	deltaStr := row[5]
	m := MetricResult{Old: old, New: newVal}
	if deltaStr == "~" {
		// benchstat "~" sets DeltaPct=0 uniformly for sec/op, B/op, and
		// allocs/op alike, but perfgate.go's nonIncreasing() treats byte/alloc
		// counts as exact with no significance gate — so a non-significant
		// byte/alloc delta comes through here as DeltaPct=0 and passes
		// nonIncreasing() undetected. Revisit once real YAGEL cell data exists.
		m.Significant = false
	} else {
		delta, err := strconv.ParseFloat(strings.TrimSuffix(deltaStr, "%"), 64)
		if err != nil {
			return MetricResult{}, fmt.Errorf("parse delta %q: %w", deltaStr, err)
		}
		m.Significant = true
		m.DeltaPct = delta
	}

	p, n, err := parsePValue(row[6])
	if err != nil {
		return MetricResult{}, err
	}
	m.PValue = p
	m.N = n
	return m, nil
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func parsePValue(s string) (p float64, n int, err error) {
	fields := strings.Fields(s)
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("parse p-value field %q: want \"p=X n=Y\"", s)
	}
	p, err = strconv.ParseFloat(strings.TrimPrefix(fields[0], "p="), 64)
	if err != nil {
		return 0, 0, fmt.Errorf("parse p-value %q: %w", fields[0], err)
	}
	n, err = strconv.Atoi(strings.TrimPrefix(fields[1], "n="))
	if err != nil {
		return 0, 0, fmt.Errorf("parse sample count %q: %w", fields[1], err)
	}
	return p, n, nil
}

// TrimProcsSuffix strips the -<GOMAXPROCS> suffix Go appends to benchmark
// names (Goldset/route-decision-24 -> Goldset/route-decision), so tiers.json
// keys stay independent of the runner's core count.
func TrimProcsSuffix(name string) string {
	i := strings.LastIndexByte(name, '-')
	if i <= 0 || i == len(name)-1 {
		return name
	}
	for _, r := range name[i+1:] {
		if r < '0' || r > '9' {
			return name
		}
	}
	return name[:i]
}

// tierConfigFile is tiers.json's on-disk shape: a documenting comment plus
// the cell-name -> tier map.
type tierConfigFile struct {
	Comment string            `json:"comment"`
	Cells   map[string]string `json:"cells"`
}

// LoadTierConfig reads a cell-name -> tier mapping (perfgate/tiers.json).
// Real cell names and tier assignments are YAGEL-owned and not yet
// published (design.md "Open inputs"); tiers.json ships placeholder
// entries only until YAGEL's corpus lands.
func LoadTierConfig(r io.Reader) (map[string]Tier, error) {
	var file tierConfigFile
	if err := json.NewDecoder(r).Decode(&file); err != nil {
		return nil, fmt.Errorf("perfgate: decode tier config: %w", err)
	}
	tiers := make(map[string]Tier, len(file.Cells))
	for name, tier := range file.Cells {
		t := Tier(tier)
		switch t {
		case TierEngineSensitive, TierDataDominated, TierConcurrent, TierStartup:
		default:
			return nil, fmt.Errorf("perfgate: cell %q: unknown tier %q", name, tier)
		}
		tiers[name] = t
	}
	return tiers, nil
}
