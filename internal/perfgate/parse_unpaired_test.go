package perfgate

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// unpairedFixture is benchstat's own output over two runs whose cpu: lines
// disagree: benchstat exits 0 and emits two single-group tables instead of
// one paired comparison, which is the shape the gate must refuse by name.
const unpairedFixture = "testdata/unpaired-single-group.csv"

func TestParseBenchstatCSV_UnpairedSingleGroup(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(unpairedFixture)
	require.NoError(t, err, "single-group fixture is missing")

	cells, err := ParseBenchstatCSV(data)
	require.Error(t, err, "single-group csv parsed as if it were a paired comparison")
	assert.True(t, errors.Is(err, ErrUnpairedComparison), "want ErrUnpairedComparison, got %v", err)
	assert.Contains(t, err.Error(), "single-group", "refusal does not name the single-group shape: %v", err)
	assert.Contains(t, err.Error(), "sec/op", "refusal does not name the metric: %v", err)
	assert.Contains(t, err.Error(), "3", "refusal does not name the column count: %v", err)
	assert.Empty(t, cells, "single-group csv yielded cells")
}

// TestParseBenchstatCSV_MalformedIsNotUnpaired keeps the two failures
// distinguishable: a malformed csv must not be reported as a pairing refusal,
// or the generic error stops meaning what it says.
func TestParseBenchstatCSV_MalformedIsNotUnpaired(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		csv  string
	}{
		{
			name: "ragged rows",
			csv: `,old.txt,,new.txt,,,
,sec/op,CI,sec/op,CI,vs base,P
Apply-8,1.0005e-07,1%
`,
		},
		{
			name: "header matches neither shape",
			csv: `,old.txt,,new.txt
,sec/op,CI,bogus
Apply-8,1.0005e-07,1%,2
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := ParseBenchstatCSV([]byte(tt.csv))
			require.Error(t, err)
			assert.False(t, errors.Is(err, ErrUnpairedComparison),
				"malformed csv reported as an unpaired comparison: %v", err)
		})
	}
}

func TestParseBenchstatCSV_PairedHeaderStillParses(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join(pinnedProfileDir, "benchstat.csv"))
	require.NoError(t, err)

	cells, err := ParseBenchstatCSV(data)
	require.NoError(t, err)
	assert.Len(t, cells, 27)
}
