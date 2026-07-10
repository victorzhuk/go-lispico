package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var lispicoBin string

func TestMain(m *testing.M) {
	tmp, _ := os.MkdirTemp("", "lispico-test")
	lispicoBin = filepath.Join(tmp, "lispico")
	cmd := exec.Command("go", "build", "-o", lispicoBin, ".")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func TestPipedInput(t *testing.T) {
	cmd := exec.Command(lispicoBin)
	cmd.Stdin = strings.NewReader("(+ 1 2)\n")
	out, err := cmd.CombinedOutput()
	assert.NoError(t, err)
	assert.Contains(t, string(out), "3")
}

func TestFileMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lisp")
	require.NoError(t, os.WriteFile(path, []byte("(+ 1 2)\n"), 0o644))
	cmd := exec.Command(lispicoBin, path)
	out, err := cmd.CombinedOutput()
	assert.NoError(t, err)
	assert.Contains(t, string(out), "3")
}

func TestFileErrorPosition(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.lisp")
	content := "(+ 1 2)\n(+ 3 4)\n[bad]\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	cmd := exec.Command(lispicoBin, path)
	out, err := cmd.CombinedOutput()
	assert.Error(t, err)
	assert.Contains(t, string(out), "bad.lisp")
	assert.Contains(t, string(out), "bad.lisp:3")
}

func TestUnknownDialect(t *testing.T) {
	cmd := exec.Command(lispicoBin, "-dialect", "scheme")
	out, err := cmd.CombinedOutput()
	assert.Error(t, err)
	assert.Contains(t, string(out), "cl")
	assert.Contains(t, string(out), "clojure")
}

func TestClojureDialect(t *testing.T) {
	cmd := exec.Command(lispicoBin, "-dialect", "clojure")
	cmd.Stdin = strings.NewReader("(do 1 2)\n")
	out, err := cmd.CombinedOutput()
	assert.NoError(t, err)
	assert.Contains(t, string(out), "2")
}
