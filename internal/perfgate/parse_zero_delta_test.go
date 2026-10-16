package perfgate

import (
	"math"
	"strings"
	"testing"
)

// TestParseBenchstatCSV_ZeroBaselineDelta covers the "?" delta token benchstat
// emits when the baseline value is zero, so no percentage delta exists.
func TestParseBenchstatCSV_ZeroBaselineDelta(t *testing.T) {
	csvFor := func(old, new, delta string) []byte {
		return []byte(strings.Join([]string{
			"goos: linux",
			"goarch: amd64",
			"pkg: github.com/victorzhuk/go-lispico/internal/goldset",
			"cpu: fixture-runner-a",
			"",
			",old.txt,,new.txt,,,",
			",sec/op,CI,sec/op,CI,vs base,P",
			"Goldset/route-decision-2," + old + ",0%," + new + ",0%," + delta + ",p=0.000 n=10",
		}, "\n"))
	}

	tests := []struct {
		name        string
		csv         []byte
		wantErr     string
		wantInf     bool
		wantZero    bool
		significant bool
	}{
		{
			name:        "zero baseline to positive candidate",
			csv:         csvFor("0", "16", "?"),
			wantInf:     true,
			significant: true,
		},
		{
			name:     "zero baseline to zero candidate",
			csv:      csvFor("0", "0", "?"),
			wantZero: true,
		},
		{
			name:    "nonzero baseline with question mark",
			csv:     csvFor("16", "8", "?"),
			wantErr: `parse delta "?"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cells, err := ParseBenchstatCSV(tt.csv)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("ParseBenchstatCSV() error = nil, want containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ParseBenchstatCSV() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseBenchstatCSV() error = %v", err)
			}
			m, ok := cells["Goldset/route-decision-2"]
			if !ok {
				t.Fatalf("cell missing from parsed output: %v", cells)
			}
			got := m.Latency
			switch {
			case tt.wantInf:
				if !math.IsInf(got.DeltaPct, 1) {
					t.Errorf("DeltaPct = %v, want +Inf", got.DeltaPct)
				}
			case tt.wantZero:
				if got.DeltaPct != 0 || math.IsInf(got.DeltaPct, 0) {
					t.Errorf("DeltaPct = %v, want 0", got.DeltaPct)
				}
			}
			if got.Significant != tt.significant {
				t.Errorf("Significant = %v, want %v", got.Significant, tt.significant)
			}
			if got.N != 10 {
				t.Errorf("N = %d, want 10", got.N)
			}
		})
	}
}
