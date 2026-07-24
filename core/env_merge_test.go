package core

import (
	"strings"
	"testing"
	"time"
)

type barrierRetainedMeter struct {
	released chan struct{}
	proceed  chan struct{}
}

func (m *barrierRetainedMeter) LeaseEval(reductions, allocBytes int64) (int64, int64, error) {
	return reductions, allocBytes, nil
}

func (m *barrierRetainedMeter) ReturnEval(reductions, allocBytes int64) {}

func (m *barrierRetainedMeter) ChargeRetained(_, _ int64) error { return nil }

func (m *barrierRetainedMeter) ReleaseRetained(_, _ int64) {
	select {
	case m.released <- struct{}{}:
	case <-time.After(2 * time.Second):
		panic("barrierReleaseMeter: release did not start")
	}
	select {
	case <-m.proceed:
	case <-time.After(2 * time.Second):
		panic("barrierReleaseMeter: release did not resume")
	}
}

func waitForSignal(t *testing.T, ch <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("%s", message)
	}
}

func sendWithTimeout(t *testing.T, ch chan struct{}, message string) {
	t.Helper()
	select {
	case ch <- struct{}{}:
	case <-time.After(2 * time.Second):
		t.Fatalf("%s", message)
	}
}

func TestMergeInto_ConcurrentSetNotLost(t *testing.T) {
	barrier := &barrierRetainedMeter{
		released: make(chan struct{}, 1),
		proceed:  make(chan struct{}, 1),
	}

	rootEnv := NewEnv(nil)
	if err := rootEnv.SetWithContext(WithEvalMeter(t.Context(), barrier), "x", Int{V: 1}); err != nil {
		t.Fatalf("seed root env: %v", err)
	}

	childEnv := rootEnv.Child()
	if err := childEnv.Set("x", String{V: strings.Repeat("x", 8)}); err != nil {
		t.Fatalf("seed child env: %v", err)
	}

	mergeErr := make(chan error, 1)
	go func() {
		mergeErr <- childEnv.MergeInto(rootEnv)
	}()

	waitForSignal(t, barrier.released, "merge did not enter retained release")
	if err := rootEnv.Set("x", String{V: "SETVAL"}); err != nil {
		t.Fatalf("concurrent set into root: %v", err)
	}
	sendWithTimeout(t, barrier.proceed, "failed to resume merge release")

	select {
	case err := <-mergeErr:
		if err != nil {
			t.Fatalf("merge into root: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("merge did not finish")
	}

	got, ok := rootEnv.Get("x")
	if !ok {
		t.Fatal("root env lost x after concurrent merge")
	}
	if !got.Equals(String{V: "SETVAL"}) {
		t.Fatalf("root env x = %v, want %q", got, "SETVAL")
	}
}

func sumLiveRetainedBytes(e *Env) int64 {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var total int64
	for _, cell := range e.vars {
		if cell.v != nil {
			total += cell.retainedBytes
		}
	}
	for _, cell := range e.funcs {
		if cell.v != nil {
			total += cell.retainedBytes
		}
	}

	return total
}

func TestMergeInto_OverwriteRetainedBytesMatchesCellSum(t *testing.T) {
	dst := NewEnv(nil)

	for _, size := range []int{10, 1000, 50} {
		src := NewEnv(nil)
		if err := src.Set("x", String{V: strings.Repeat("x", size)}); err != nil {
			t.Fatalf("set src x (%d): %v", size, err)
		}
		if err := src.MergeInto(dst); err != nil {
			t.Fatalf("merge size %d: %v", size, err)
		}

		gotBytes, gotSlots := dst.RetainedUsage()
		wantBytes := sumLiveRetainedBytes(dst)
		if gotBytes != wantBytes {
			t.Fatalf("RetainedUsage bytes = %d, want %d after merge size %d", gotBytes, wantBytes, size)
		}
		if gotSlots != 1 {
			t.Fatalf("RetainedUsage slots = %d, want 1 after merge size %d", gotSlots, size)
		}
	}
}

func TestMergeIntoCanonical_OverwriteFuncRetainedBytesMatchesCellSum(t *testing.T) {
	dst := NewEnv(nil)

	for _, size := range []int{10, 1000, 50} {
		src := NewEnv(nil)
		if err := src.SetFunc("x", String{V: strings.Repeat("x", size)}); err != nil {
			t.Fatalf("set src func x (%d): %v", size, err)
		}
		if err := src.MergeIntoCanonical(dst); err != nil {
			t.Fatalf("merge canonical size %d: %v", size, err)
		}

		gotBytes, gotSlots := dst.RetainedUsage()
		wantBytes := sumLiveRetainedBytes(dst)
		if gotBytes != wantBytes {
			t.Fatalf("canonical RetainedUsage bytes = %d, want %d after merge size %d", gotBytes, wantBytes, size)
		}
		if gotSlots != 1 {
			t.Fatalf("canonical RetainedUsage slots = %d, want 1 after merge size %d", gotSlots, size)
		}
	}
}
