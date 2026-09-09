package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// readContextStats calls the guarded entry point and reports a panic as an
// error: core answers every failure through its error return, so a panic is a
// contract break this test must report as one rather than tear the suite down.
func readContextStats(ctx context.Context, d Dialect, src string, maxDepth int) (forms []Value, stats ReaderStats, err error) {
	defer func() {
		if r := recover(); r != nil {
			forms, stats, err = nil, ReaderStats{}, fmt.Errorf("panicked: %v", r)
		}
	}()
	return d.ReadWithContextStats(ctx, src, maxDepth)
}

func TestReadWithContextStats_LegacyParity(t *testing.T) {
	t.Parallel()

	dialects := []struct {
		name string
		d    Dialect
	}{
		{"full", FullDialect()},
		{"reader-syntax", FullDialect().WithReaderVector().WithFunctionRef()},
	}

	fixtures := []struct {
		name     string
		src      string
		maxDepth int
	}{
		{"empty", "", 0},
		{"comment-only", "; nothing to read\n; still nothing\n", 0},
		{"atoms", "1 -2 2.5 :kw sym nil true false", 0},
		{"string-escapes", `"a\nb\"c\\d"`, 0},
		{"nested-list", "(defn f [x] (+ x 1))", 0},
		{"collections", "[1 2 {:a 1 :b [3 4]}]", 0},
		{"quoting", "'(a b) `(a ~b ~@c)", 0},
		{"multiple-forms", "(a)\n(b)\n; tail comment\n(c)", 0},
		{"low-depth-ceiling", strings.Repeat("[", 40) + "1" + strings.Repeat("]", 40), 8},
		{"unterminated-string", `"abc`, 0},
		{"unexpected-eof", "(1 2", 0},
		{"stray-close", ")", 0},
	}

	for _, dc := range dialects {
		for _, f := range fixtures {
			t.Run(dc.name+"/"+f.name, func(t *testing.T) {
				t.Parallel()
				wantForms, wantStats, wantErr := dc.d.ReadWithMaxDepthStats(f.src, f.maxDepth)
				gotForms, gotStats, gotErr := readContextStats(context.Background(), dc.d, f.src, f.maxDepth)

				assertReadErrorParity(t, gotErr, wantErr)
				if wantErr != nil {
					return
				}
				if len(gotForms) != len(wantForms) {
					t.Fatalf("read %d forms, legacy read %d", len(gotForms), len(wantForms))
				}
				for i, want := range wantForms {
					if !want.Equals(gotForms[i]) {
						t.Fatalf("form %d = %v, legacy form = %v", i, gotForms[i], want)
					}
				}
				if gotStats != wantStats {
					t.Fatalf("stats = %+v, legacy stats = %+v", gotStats, wantStats)
				}
			})
		}
	}
}

func assertReadErrorParity(t *testing.T, got, want error) {
	t.Helper()
	if want == nil {
		if got != nil {
			t.Fatalf("read failed with %v, legacy read succeeded", got)
		}
		return
	}
	if got == nil {
		t.Fatalf("read succeeded, legacy read failed with %v", want)
	}

	var wantLE, gotLE *LispicoError
	if !errors.As(want, &wantLE) {
		if got.Error() != want.Error() {
			t.Fatalf("error = %q, legacy error = %q", got, want)
		}
		return
	}
	if !errors.As(got, &gotLE) {
		t.Fatalf("error %v is not a *LispicoError; legacy error is %v", got, want)
	}
	if gotLE.Code != wantLE.Code {
		t.Fatalf("error code = %q, legacy code = %q", gotLE.Code, wantLE.Code)
	}
	if gotLE.Message != wantLE.Message {
		t.Fatalf("error message = %q, legacy message = %q", gotLE.Message, wantLE.Message)
	}
	if gotLE.Line != wantLE.Line || gotLE.Col != wantLE.Col {
		t.Fatalf("error position = %d:%d, legacy position = %d:%d",
			gotLE.Line, gotLE.Col, wantLE.Line, wantLE.Col)
	}
}
