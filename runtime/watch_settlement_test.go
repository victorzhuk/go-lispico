package runtime

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

type lockedBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// watcherReaderPanicMeter panics exactly once, from the second lease it is
// asked for (lease 1 is New's meter probe, lease 2 the reload's enclosing
// draw), so a later scan can still reload successfully.
type watcherReaderPanicMeter struct {
	recordingMeter
}

func (m *watcherReaderPanicMeter) LeaseEval(reductions, allocBytes int64) (int64, int64, error) {
	red, alloc, err := m.recordingMeter.LeaseEval(reductions, allocBytes)
	if m.snapshot().leaseCalls == 2 {
		panic("reader meter failure")
	}
	return red, alloc, err
}

// settlementPanicMeter panics from ReturnEval, the hook FinishEval's
// settlement reaches when it returns the evaluation lease: the way a buggy
// embedder meter would fail settlement.
type settlementPanicMeter struct {
	recordingMeter
	armed          atomic.Bool
	postArmReturns atomic.Int64
}

func (m *settlementPanicMeter) Arm() { m.armed.Store(true) }

func (m *settlementPanicMeter) ReturnEval(reductions, allocBytes int64) {
	m.recordingMeter.ReturnEval(reductions, allocBytes)
	// Panic on the reload's own settlement return: the reload of a valid
	// file is the first ReturnEval after arming (New's probe return happens
	// before arming, and a successful guarded read settles without a
	// return), the path that must be contained by the watcher.
	if m.armed.Load() && m.postArmReturns.Add(1) == 1 {
		panic("settlement meter failure")
	}
}

func watcherFixture(t *testing.T, meter Meter) (*engineImpl, *fileWatcher, *slog.Logger, *strings.Builder) {
	t.Helper()
	var buf strings.Builder
	log := slog.New(slog.NewTextHandler(&buf, nil))
	opts := []EngineOption{WithBytecode()}
	if meter != nil {
		opts = append(opts, WithEngineMeter(meter))
	}
	eng, err := New(log, opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	impl := eng.(*engineImpl)
	w := newFileWatcher(impl, t.TempDir(), 10*time.Millisecond)
	w.ctx = context.Background()
	return impl, w, log, &buf
}

// TestWatch_SettlesLeasesAcrossOutcomes proves the enclosing reload
// lifecycle settles exactly once per reload: every lease drawn is returned,
// and the merge happens only after a successful settlement.
func TestWatch_SettlesLeasesAcrossOutcomes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		source    func(t *testing.T, dir string) string
		meter     func() Meter
		wantOK    bool
		wantMtime bool
	}{
		{
			name: "malformed-read",
			source: func(t *testing.T, dir string) string {
				file := filepath.Join(dir, "bad.lisp")
				require.NoError(t, os.WriteFile(file, []byte("(def x 1"), 0o644))
				return file
			},
			meter: func() Meter { return &recordingMeter{} },
		},
		{
			name: "failing-form",
			source: func(t *testing.T, dir string) string {
				file := filepath.Join(dir, "fail.lisp")
				require.NoError(t, os.WriteFile(file, []byte("(undefined-symbol)"), 0o644))
				return file
			},
			meter: func() Meter { return &recordingMeter{} },
		},
		{
			name: "success",
			source: func(t *testing.T, dir string) string {
				file := filepath.Join(dir, "good.lisp")
				require.NoError(t, os.WriteFile(file, []byte("(def x 42)"), 0o644))
				return file
			},
			meter:     func() Meter { return &recordingMeter{} },
			wantOK:    true,
			wantMtime: true,
		},
		{
			name: "eval-panic",
			source: func(t *testing.T, dir string) string {
				file := filepath.Join(dir, "panic.lisp")
				require.NoError(t, os.WriteFile(file, []byte("(boom)"), 0o644))
				return file
			},
			meter: func() Meter { return &recordingMeter{} },
		},
		{
			name: "settlement-error",
			source: func(t *testing.T, dir string) string {
				file := filepath.Join(dir, "settle.lisp")
				require.NoError(t, os.WriteFile(file, []byte("(def x 42)"), 0o644))
				return file
			},
			meter: func() Meter { return &settlementPanicMeter{} },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			meter := tc.meter()
			impl, w, _, buf := watcherFixture(t, meter)
			if tc.name == "eval-panic" {
				require.NoError(t, impl.Bind("boom", core.GoFunc{
					Name: "boom",
					Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
						panic("watcher settlement boom")
					},
				}))
			}

			file := tc.source(t, w.dir)
			if pm, ok := meter.(*settlementPanicMeter); ok {
				pm.Arm()
			}
			require.NotPanics(t, func() {
				w.scan()
			})

			snap := meter.(interface{ snapshot() recordingMeter }).snapshot()
			assert.Equal(t, snap.leaseCalls, snap.returnCalls,
				"every lease drawn by the reload must be settled exactly once: leases=%d returns=%d", snap.leaseCalls, snap.returnCalls)

			_, hasX := impl.rootEnv.Get("x")
			if tc.wantOK {
				assert.True(t, hasX, "a settled reload must merge into the root")
				assert.Contains(t, w.mtimes, file, "a settled reload must record the mtime")
			} else {
				assert.False(t, hasX, "a failed reload must keep the previous environment")
				assert.NotContains(t, w.mtimes, file, "a failed reload must not record the mtime")
			}
			if tc.wantOK {
				assert.Contains(t, buf.String(), "reloaded file")
			} else {
				assert.NotContains(t, buf.String(), "reloaded file")
			}
			if tc.name == "settlement-error" {
				assert.Contains(t, buf.String(), "settle eval in file",
					"the reload's own settlement must run and contain the meter panic")
			}
		})
	}
}

// TestWatch_ReaderMeterPanicSurvivesProcess proves in a bounded subprocess
// that a host meter panicking while the guarded read is still running cannot
// take the process down: the panic is contained, the watcher stays alive,
// and a later scan reloads the file successfully.
func TestWatch_ReaderMeterPanicSurvivesProcess(t *testing.T) {
	t.Parallel()

	exe, err := os.Executable()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	tmp := t.TempDir()
	cmd := exec.CommandContext(ctx, exe,
		"-test.run=^TestWatch_ReaderPanicMeter_Child$",
		"-test.timeout=6s",
	)
	cmd.Env = append(os.Environ(),
		"LISPICO_WATCH_PANIC_CASE=reader-panic",
		"TMPDIR="+tmp,
	)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("reader-panic containment hung: parent deadline exceeded; child output:\n%s", out)
	}
	require.NoError(t, err, "child output:\n%s", out)
	marker, markerErr := os.ReadFile(filepath.Join(tmp, "reload-recovered"))
	require.NoError(t, markerErr, "child never reloaded after the contained panic; output:\n%s", out)
	assert.Equal(t, "later-reload-ok", strings.TrimSpace(string(marker)))
}

func TestWatch_ReaderPanicMeter_Child(t *testing.T) {
	if os.Getenv("LISPICO_WATCH_PANIC_CASE") == "" {
		t.Skip("harness child; driven by TestWatch_ReaderMeterPanicSurvivesProcess")
	}

	buf := &lockedBuffer{}
	log := slog.New(slog.NewTextHandler(buf, nil))
	meter := &watcherReaderPanicMeter{}
	eng, err := New(log, WithBytecode(), WithEngineMeter(meter), WithResourceLimits(leaseReaderLimits(t)))
	require.NoError(t, err)

	dir := t.TempDir()
	file := filepath.Join(dir, "panic.lisp")
	require.NoError(t, os.WriteFile(file, []byte("(def x 1)\n"), 0o644))

	impl := eng.(*engineImpl)
	w := newFileWatcher(impl, dir, 5*time.Millisecond)
	w.ctx = context.Background()
	go w.watchLoop()

	// The first scan reloads; the reader's re-lease panics mid-read and must
	// be contained by the reload lifecycle.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if strings.Contains(buf.String(), "recovered panic") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("reader panic never surfaced in the log; tail:\n%s", buf.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
	_, hasX := impl.rootEnv.Get("x")
	require.False(t, hasX, "the panicked reload must not merge")

	// The watcher must survive: rewriting the file with a newer mtime lets a
	// later scan reload it successfully.
	time.Sleep(20 * time.Millisecond)
	require.NoError(t, os.WriteFile(file, []byte("(def x 7)\n"), 0o644))
	deadline = time.Now().Add(2 * time.Second)
	for {
		if v, ok := impl.rootEnv.Get("x"); ok && v.(core.Int).V == 7 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("watcher never reloaded after the contained panic; tail:\n%s", buf.String())
		}
		time.Sleep(5 * time.Millisecond)
	}

	w.Stop()
	w.wg.Wait()
	require.NoError(t, eng.Close())
	require.NoError(t, os.WriteFile(filepath.Join(os.TempDir(), "reload-recovered"), []byte("later-reload-ok"), 0o644))
}
