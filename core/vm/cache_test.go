package vm

import (
	"encoding/gob"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

func TestBytecodeCache_New(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "cache")
	bc, err := NewBytecodeCache(dir)
	require.NoError(t, err)
	assert.Equal(t, dir, bc.Dir())

	info, err := os.Stat(dir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestBytecodeCache_Load_Miss(t *testing.T) {
	t.Parallel()

	bc, err := NewBytecodeCache(filepath.Join(t.TempDir(), "cache"))
	require.NoError(t, err)

	content := []byte("(+ 1 2)")
	chunks, err := bc.Load("test.lisp", content)
	assert.Error(t, err)
	assert.Nil(t, chunks)
}

func TestBytecodeCache_StoreAndLoad_Hit(t *testing.T) {
	t.Parallel()

	bc, err := NewBytecodeCache(filepath.Join(t.TempDir(), "cache"))
	require.NoError(t, err)

	chunk := &Chunk{
		Name:      "test",
		Code:      []Instruction{Encode(OpConst, 0)},
		Constants: []core.Value{core.Int{V: 42}},
	}
	chunks := []*Chunk{chunk}

	content := []byte("(+ 1 2)")
	bc.Store("test.lisp", content, chunks)

	time.Sleep(50 * time.Millisecond)

	loaded, err := bc.Load("test.lisp", content)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Equal(t, chunk.Name, loaded[0].Name)
	assert.Equal(t, len(chunk.Code), len(loaded[0].Code))
	assert.Equal(t, chunk.Code[0], loaded[0].Code[0])
}

func TestBytecodeCache_Load_VersionMismatch(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "cache")
	bc, err := NewBytecodeCache(dir)
	require.NoError(t, err)

	chunk := &Chunk{Name: "test"}
	chunks := []*Chunk{chunk}
	content := []byte("(+ 1 2)")

	bc.Store("test.lisp", content, chunks)
	time.Sleep(50 * time.Millisecond)

	cacheFile := bc.key("test.lisp", content)

	entry := cacheEntry{Version: 999, Chunks: chunks}
	f, err := os.Create(cacheFile)
	require.NoError(t, err)
	require.NoError(t, gob.NewEncoder(f).Encode(entry))
	f.Close()

	loaded, err := bc.Load("test.lisp", content)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "version mismatch")
	assert.Nil(t, loaded)
}

func TestBytecodeCache_DifferentContent(t *testing.T) {
	t.Parallel()

	bc, err := NewBytecodeCache(filepath.Join(t.TempDir(), "cache"))
	require.NoError(t, err)

	chunk := &Chunk{Name: "first"}
	bc.Store("test.lisp", []byte("(+ 1 2)"), []*Chunk{chunk})
	time.Sleep(50 * time.Millisecond)

	otherChunk := &Chunk{Name: "second"}
	bc.Store("test.lisp", []byte("(* 3 4)"), []*Chunk{otherChunk})
	time.Sleep(50 * time.Millisecond)

	loaded1, err := bc.Load("test.lisp", []byte("(+ 1 2)"))
	require.NoError(t, err)
	assert.Equal(t, "first", loaded1[0].Name)

	loaded2, err := bc.Load("test.lisp", []byte("(* 3 4)"))
	require.NoError(t, err)
	assert.Equal(t, "second", loaded2[0].Name)
}

func TestBytecodeCache_Key(t *testing.T) {
	t.Parallel()

	bc, err := NewBytecodeCache(filepath.Join(t.TempDir(), "cache"))
	require.NoError(t, err)

	key1 := bc.key("foo.lisp", []byte("content"))
	key2 := bc.key("foo.lisp", []byte("different"))
	key3 := bc.key("bar.lisp", []byte("content"))

	assert.NotEqual(t, key1, key2)
	assert.NotEqual(t, key1, key3)
	assert.Contains(t, key1, "foo.lisp")
	assert.Contains(t, key3, "bar.lisp")
}

func TestBytecodeCache_MultipleChunks(t *testing.T) {
	t.Parallel()

	bc, err := NewBytecodeCache(filepath.Join(t.TempDir(), "cache"))
	require.NoError(t, err)

	chunks := []*Chunk{
		{Name: "chunk1", Arity: 0},
		{Name: "chunk2", Arity: 2, Variadic: true},
		{Name: "chunk3", Locals: 5},
	}

	content := []byte("(def x 1) (def y 2)")
	bc.Store("multi.lisp", content, chunks)
	time.Sleep(50 * time.Millisecond)

	loaded, err := bc.Load("multi.lisp", content)
	require.NoError(t, err)
	require.Len(t, loaded, 3)

	assert.Equal(t, "chunk1", loaded[0].Name)
	assert.Equal(t, 0, loaded[0].Arity)

	assert.Equal(t, "chunk2", loaded[1].Name)
	assert.Equal(t, 2, loaded[1].Arity)
	assert.True(t, loaded[1].Variadic)

	assert.Equal(t, "chunk3", loaded[2].Name)
	assert.Equal(t, 5, loaded[2].Locals)
}

func TestBytecodeCache_SubChunks(t *testing.T) {
	t.Parallel()

	bc, err := NewBytecodeCache(filepath.Join(t.TempDir(), "cache"))
	require.NoError(t, err)

	innerChunk := &Chunk{
		Name:  "inner",
		Arity: 1,
	}
	outerChunk := &Chunk{
		Name:      "outer",
		SubChunks: []*Chunk{innerChunk},
	}

	content := []byte("(def f (fn [x] x))")
	bc.Store("nested.lisp", content, []*Chunk{outerChunk})
	time.Sleep(50 * time.Millisecond)

	loaded, err := bc.Load("nested.lisp", content)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	require.Len(t, loaded[0].SubChunks, 1)
	assert.Equal(t, "inner", loaded[0].SubChunks[0].Name)
	assert.Equal(t, 1, loaded[0].SubChunks[0].Arity)
}

func TestBytecodeCache_CodeWithConstants(t *testing.T) {
	t.Parallel()

	bc, err := NewBytecodeCache(filepath.Join(t.TempDir(), "cache"))
	require.NoError(t, err)

	chunk := &Chunk{
		Name: "test",
		Code: []Instruction{
			Encode(OpConst, 0),
			Encode(OpConst, 1),
			Encode(OpConst, 2),
			Encode(OpReturn, 0),
		},
		Constants: []core.Value{
			core.Int{V: 42},
			core.String{V: "hello"},
			core.Bool{V: true},
		},
	}

	content := []byte("42")
	bc.Store("const.lisp", content, []*Chunk{chunk})
	time.Sleep(50 * time.Millisecond)

	loaded, err := bc.Load("const.lisp", content)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	require.Len(t, loaded[0].Constants, 3)

	assert.Equal(t, core.Int{V: 42}, loaded[0].Constants[0])
	assert.Equal(t, core.String{V: "hello"}, loaded[0].Constants[1])
	assert.Equal(t, core.Bool{V: true}, loaded[0].Constants[2])
}
