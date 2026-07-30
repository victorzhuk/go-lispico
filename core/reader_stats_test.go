package core

import (
	"os"
	"path/filepath"
	"testing"
)

// TestReaderStats_Goldset pins ReaderStats {Nodes, Bytes} for every gold-set
// fixture. ReaderStats feeds the ADR 0011 evaluator-independent allocation
// ledger, so it must stay bit-identical across reader changes; any diff here
// is a ledger regression, not a refactor to absorb.
func TestReaderStats_Goldset(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		nodes int64
		bytes int64
	}{
		{"counter-closure", 35, 52},
		{"guard-nil", 19, 47},
		{"kw-lookup", 18, 17},
		{"loop-sum", 24, 27},
		{"merge-config", 38, 101},
		{"pipeline", 38, 30},
		{"queue-promote", 33, 44},
		{"registry-fold", 39, 123},
		{"route-decision", 30, 101},
		{"rule-load", 105, 319},
		{"safe-parse", 44, 99},
		{"text-render", 22, 68},
		{"twice-macro", 30, 95},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join("..", "internal", "goldset", "testdata", tt.name+".lisp")
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			_, stats, err := FullDialect().ReadWithMaxDepthStats(string(src), defaultReaderDepth)
			if err != nil {
				t.Fatalf("ReadWithMaxDepthStats: %v", err)
			}
			if stats.Nodes != tt.nodes || stats.Bytes != tt.bytes {
				t.Errorf("stats = {Nodes: %d, Bytes: %d}, want {Nodes: %d, Bytes: %d}",
					stats.Nodes, stats.Bytes, tt.nodes, tt.bytes)
			}
		})
	}
}

// TestReaderStats_Bench pins ReaderStats for the core/bench_test.go reader
// benchmark sources, byte-exact with their definitions there.
func TestReaderStats_Bench(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		src   string
		nodes int64
		bytes int64
	}{
		{"Read_Simple", "(+ 1 (* 2 3))", 7, 2},
		{"Read_SmallListLiteral", "(1 2 3)", 4, 0},
		{"Read_SmallVectorLiteral", "[1 2 3]", 4, 0},
		{"Read_Representative", representativeSource, 61, 51},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, stats, err := FullDialect().ReadWithMaxDepthStats(tt.src, defaultReaderDepth)
			if err != nil {
				t.Fatalf("ReadWithMaxDepthStats: %v", err)
			}
			if stats.Nodes != tt.nodes || stats.Bytes != tt.bytes {
				t.Errorf("stats = {Nodes: %d, Bytes: %d}, want {Nodes: %d, Bytes: %d}",
					stats.Nodes, stats.Bytes, tt.nodes, tt.bytes)
			}
		})
	}
}
