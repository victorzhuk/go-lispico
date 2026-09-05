package perfgate

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// intelProfileDir is the repo's cross-runner counterexample: a profile
// recorded on Intel while pinnedProfileDir was recorded on AMD.
const intelProfileDir = "testdata/profile-30614184386"

func TestReadRunnerIdentity(t *testing.T) {
	t.Parallel()

	corpus := func(t *testing.T, dir string) io.Reader {
		t.Helper()
		f, err := os.Open(filepath.Join(dir, "bench-vm.txt"))
		require.NoError(t, err)
		t.Cleanup(func() { _ = f.Close() })
		return f
	}

	const missingCPU = "goos: linux\n" +
		"goarch: amd64\n" +
		"pkg: github.com/victorzhuk/go-lispico/internal/goldset\n"

	const inconsistentCPU = "goos: linux\ngoarch: amd64\npkg: github.com/victorzhuk/go-lispico/internal/goldset\ncpu: A\n" +
		"goos: linux\ngoarch: amd64\npkg: github.com/victorzhuk/go-lispico/internal/goldset\ncpu: B\n"

	const whitespaceDrift = "goos: linux\ngoarch: amd64\npkg: github.com/victorzhuk/go-lispico/internal/goldset\ncpu: AMD EPYC 7763 64-Core Processor                \n" +
		"goos: linux\ngoarch: amd64\npkg: github.com/victorzhuk/go-lispico/internal/goldset\ncpu: AMD EPYC 7763 64-Core Processor\n"

	tests := []struct {
		name       string
		reader     func(t *testing.T) io.Reader
		want       RunnerIdentity
		wantKnown  bool
		wantString string
		wantErr    error
	}{
		{
			name:       "amd corpus",
			reader:     func(t *testing.T) io.Reader { return corpus(t, pinnedProfileDir) },
			want:       RunnerIdentity{GOOS: "linux", GOARCH: "amd64", CPU: "AMD EPYC 7763 64-Core Processor"},
			wantKnown:  true,
			wantString: "linux/amd64/AMD EPYC 7763 64-Core Processor",
		},
		{
			name:       "intel corpus",
			reader:     func(t *testing.T) io.Reader { return corpus(t, intelProfileDir) },
			want:       RunnerIdentity{GOOS: "linux", GOARCH: "amd64", CPU: "INTEL(R) XEON(R) PLATINUM 8573C"},
			wantKnown:  true,
			wantString: "linux/amd64/INTEL(R) XEON(R) PLATINUM 8573C",
		},
		{
			name:       "missing cpu line",
			reader:     func(*testing.T) io.Reader { return strings.NewReader(missingCPU) },
			want:       RunnerIdentity{GOOS: "linux", GOARCH: "amd64"},
			wantString: "linux/amd64/unknown",
		},
		{
			name:       "empty file",
			reader:     func(*testing.T) io.Reader { return strings.NewReader("") },
			want:       RunnerIdentity{},
			wantString: "unknown/unknown/unknown",
		},
		{
			name:    "preambles disagree",
			reader:  func(*testing.T) io.Reader { return strings.NewReader(inconsistentCPU) },
			wantErr: ErrInconsistentPreamble,
		},
		{
			name:       "preambles differ only in trailing whitespace",
			reader:     func(*testing.T) io.Reader { return strings.NewReader(whitespaceDrift) },
			want:       RunnerIdentity{GOOS: "linux", GOARCH: "amd64", CPU: "AMD EPYC 7763 64-Core Processor"},
			wantKnown:  true,
			wantString: "linux/amd64/AMD EPYC 7763 64-Core Processor",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ReadRunnerIdentity(tt.reader(t))
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Equal(t, RunnerIdentity{}, got, "an inconsistent file yields no identity")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantKnown, got.Known())
			assert.Equal(t, tt.wantString, got.String())
		})
	}
}
