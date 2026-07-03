package lio

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

func TestSandboxModeStrict(t *testing.T) {
	tmpDir := t.TempDir()

	sb, err := NewSandbox(Config{
		Mode:    ModeStrict,
		RootDir: tmpDir,
	})
	require.NoError(t, err)

	t.Run("allows path inside root", func(t *testing.T) {
		path := filepath.Join(tmpDir, "test.txt")
		validated, err := sb.Validate(path, false)
		assert.NoError(t, err)
		assert.Equal(t, path, validated)
	})

	t.Run("blocks path outside root", func(t *testing.T) {
		path := filepath.Join(tmpDir, "..", "outside.txt")
		_, err := sb.Validate(path, false)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "outside sandbox root")
	})
}

func TestSandboxStrictWithoutRoot(t *testing.T) {
	cwd, _ := os.Getwd()
	sb, err := NewSandbox(Config{
		Mode:    ModeStrict,
		RootDir: "",
	})
	require.NoError(t, err)

	path := filepath.Join(cwd, "test.txt")
	validated, err := sb.Validate(path, false)
	assert.NoError(t, err)
	assert.Equal(t, path, validated)
}

func TestSandboxModeRelaxed(t *testing.T) {
	tmpDir := t.TempDir()
	allowDir := filepath.Join(tmpDir, "allowed")

	sb, err := NewSandbox(Config{
		Mode:       ModeRelaxed,
		AllowRead:  []string{allowDir},
		AllowWrite: []string{allowDir},
	})
	require.NoError(t, err)

	t.Run("allows path in allow list", func(t *testing.T) {
		path := filepath.Join(allowDir, "test.txt")
		validated, err := sb.Validate(path, false)
		assert.NoError(t, err)
		assert.Equal(t, path, validated)
	})

	t.Run("blocks path not in allow list", func(t *testing.T) {
		path := filepath.Join(tmpDir, "denied", "test.txt")
		_, err := sb.Validate(path, true)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "write not allowed")
	})
}

func TestSandboxModeNone(t *testing.T) {
	sb, err := NewSandbox(Config{Mode: ModeNone})
	require.NoError(t, err)

	t.Run("allows any path", func(t *testing.T) {
		path := "/any/path/file.txt"
		validated, err := sb.Validate(path, true)
		assert.NoError(t, err)
		assert.True(t, strings.HasSuffix(validated, path) || validated == path || strings.Contains(validated, "any"))
	})
}

func TestSandboxPathTraversal(t *testing.T) {
	tmpDir := t.TempDir()

	sb, err := NewSandbox(Config{
		Mode:    ModeStrict,
		RootDir: tmpDir,
	})
	require.NoError(t, err)

	t.Run("blocks path escaping sandbox", func(t *testing.T) {
		path := filepath.Join(tmpDir, "subdir", "..", "..", "etc", "passwd")
		_, err := sb.Validate(path, false)
		assert.Error(t, err)
	})
}

func TestSandboxDenyPattern(t *testing.T) {
	tmpDir := t.TempDir()

	sb, err := NewSandbox(Config{
		Mode:        ModeStrict,
		RootDir:     tmpDir,
		DenyPattern: `\.env$`,
	})
	require.NoError(t, err)

	t.Run("blocks path matching deny pattern", func(t *testing.T) {
		path := filepath.Join(tmpDir, ".env")
		_, err := sb.Validate(path, false)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "denied by pattern")
	})

	t.Run("allows path not matching deny pattern", func(t *testing.T) {
		path := filepath.Join(tmpDir, "config.txt")
		_, err := sb.Validate(path, false)
		assert.NoError(t, err)
	})
}

func TestPluginNew(t *testing.T) {
	t.Run("creates plugin with config", func(t *testing.T) {
		tmpDir := t.TempDir()
		plugin, err := New(Config{
			Mode:    ModeStrict,
			RootDir: tmpDir,
		})
		require.NoError(t, err)
		assert.NotNil(t, plugin)
		assert.Equal(t, "io", plugin.Name())
	})

	t.Run("creates unsafe plugin", func(t *testing.T) {
		plugin := NewUnsafe()
		assert.NotNil(t, plugin)
		assert.Equal(t, "io", plugin.Name())
	})
}

func TestPluginMetadata(t *testing.T) {
	plugin := NewUnsafe()
	meta := plugin.Metadata()

	assert.Equal(t, "1.0.0", meta.Version)
	assert.Contains(t, meta.Description, "IO operations")
	assert.Equal(t, "go-lispico team", meta.Author)
}

func TestPluginInit(t *testing.T) {
	plugin := NewUnsafe()
	env := core.NewEnv(nil)

	err := plugin.Init(env)
	require.NoError(t, err)

	functions := []string{
		"io/read-file",
		"io/write-file",
		"io/exists?",
		"io/ls",
		"io/mkdir",
		"io/stat",
		"io/env-get",
		"io/env-set",
	}

	for _, fn := range functions {
		t.Run("registers "+fn, func(t *testing.T) {
			val, ok := env.Get(fn)
			assert.True(t, ok)
			_, isGoFunc := val.(core.GoFunc)
			assert.True(t, isGoFunc)
		})
	}
}

func TestFileOperations(t *testing.T) {
	tmpDir := t.TempDir()
	plugin, err := New(Config{
		Mode:    ModeStrict,
		RootDir: tmpDir,
	})
	require.NoError(t, err)

	t.Run("write and read file", func(t *testing.T) {
		testPath := filepath.Join(tmpDir, "test.txt")
		testContent := "hello world"

		_, err := plugin.writeFile(nil, nil, []core.Value{
			core.String{V: testPath},
			core.String{V: testContent},
		}, nil)
		require.NoError(t, err)

		result, err := plugin.readFile(nil, nil, []core.Value{
			core.String{V: testPath},
		}, nil)
		require.NoError(t, err)

		str, ok := result.(core.String)
		require.True(t, ok)
		assert.Equal(t, testContent, str.V)
	})

	t.Run("write file creates parent dirs", func(t *testing.T) {
		testPath := filepath.Join(tmpDir, "nested", "deep", "file.txt")
		testContent := "nested content"

		_, err := plugin.writeFile(nil, nil, []core.Value{
			core.String{V: testPath},
			core.String{V: testContent},
		}, nil)
		require.NoError(t, err)

		assert.FileExists(t, testPath)
	})

	t.Run("exists returns true for existing file", func(t *testing.T) {
		testPath := filepath.Join(tmpDir, "existing.txt")
		os.WriteFile(testPath, []byte("test"), 0644)

		result, err := plugin.exists(nil, nil, []core.Value{
			core.String{V: testPath},
		}, nil)
		require.NoError(t, err)

		b, ok := result.(core.Bool)
		require.True(t, ok)
		assert.True(t, b.V)
	})

	t.Run("exists returns false for missing file", func(t *testing.T) {
		testPath := filepath.Join(tmpDir, "missing.txt")

		result, err := plugin.exists(nil, nil, []core.Value{
			core.String{V: testPath},
		}, nil)
		require.NoError(t, err)

		b, ok := result.(core.Bool)
		require.True(t, ok)
		assert.False(t, b.V)
	})

	t.Run("ls lists directory contents", func(t *testing.T) {
		dirPath := filepath.Join(tmpDir, "lsdir")
		os.MkdirAll(dirPath, 0755)
		os.WriteFile(filepath.Join(dirPath, "a.txt"), nil, 0644)
		os.WriteFile(filepath.Join(dirPath, "b.txt"), nil, 0644)

		result, err := plugin.ls(nil, nil, []core.Value{
			core.String{V: dirPath},
		}, nil)
		require.NoError(t, err)

		list, ok := result.(core.List)
		require.True(t, ok)
		assert.Len(t, list.Items, 2)
	})

	t.Run("mkdir creates directory", func(t *testing.T) {
		dirPath := filepath.Join(tmpDir, "newdir", "nested")

		_, err := plugin.mkdir(nil, nil, []core.Value{
			core.String{V: dirPath},
		}, nil)
		require.NoError(t, err)

		assert.DirExists(t, dirPath)
	})

	t.Run("stat returns file metadata", func(t *testing.T) {
		testPath := filepath.Join(tmpDir, "stat.txt")
		os.WriteFile(testPath, []byte("content"), 0644)

		result, err := plugin.stat(nil, nil, []core.Value{
			core.String{V: testPath},
		}, nil)
		require.NoError(t, err)

		m, ok := result.(*core.HashMap)
		require.True(t, ok)

		size, ok := m.Get(core.Keyword{V: "size"})
		require.True(t, ok)
		sizeInt, ok := size.(core.Int)
		require.True(t, ok)
		assert.Equal(t, int64(7), sizeInt.V)

		isdir, ok := m.Get(core.Keyword{V: "isdir"})
		require.True(t, ok)
		isdirBool, ok := isdir.(core.Bool)
		require.True(t, ok)
		assert.False(t, isdirBool.V)
	})
}

func TestFileOperationsErrors(t *testing.T) {
	tmpDir := t.TempDir()
	plugin, err := New(Config{
		Mode:    ModeStrict,
		RootDir: tmpDir,
	})
	require.NoError(t, err)

	t.Run("read file outside sandbox fails", func(t *testing.T) {
		_, err := plugin.readFile(nil, nil, []core.Value{
			core.String{V: "/etc/passwd"},
		}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "sandbox")
	})

	t.Run("write file outside sandbox fails", func(t *testing.T) {
		_, err := plugin.writeFile(nil, nil, []core.Value{
			core.String{V: "/tmp/outside.txt"},
			core.String{V: "content"},
		}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "sandbox")
	})

	t.Run("wrong argument type", func(t *testing.T) {
		_, err := plugin.readFile(nil, nil, []core.Value{
			core.Int{V: 123},
		}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "must be string")
	})
}

func TestSandboxInvalidDenyPattern(t *testing.T) {
	_, err := NewSandbox(Config{
		Mode:        ModeStrict,
		RootDir:     t.TempDir(),
		DenyPattern: `[invalid(`,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid deny pattern")
}

func TestSandboxRelaxedEmptyAllowList(t *testing.T) {
	tmpDir := t.TempDir()
	sb, err := NewSandbox(Config{
		Mode:      ModeRelaxed,
		AllowRead: []string{},
	})
	require.NoError(t, err)

	path := filepath.Join(tmpDir, "test.txt")
	validated, err := sb.Validate(path, false)
	assert.NoError(t, err)
	assert.Equal(t, path, validated)
}

func TestSandboxRelaxedReadBlocked(t *testing.T) {
	tmpDir := t.TempDir()
	allowDir := filepath.Join(tmpDir, "allowed")
	os.MkdirAll(allowDir, 0755)

	sb, err := NewSandbox(Config{
		Mode:       ModeRelaxed,
		AllowRead:  []string{allowDir},
		AllowWrite: nil,
	})
	require.NoError(t, err)

	t.Run("read from allowed path", func(t *testing.T) {
		path := filepath.Join(allowDir, "file.txt")
		_, err := sb.Validate(path, false)
		assert.NoError(t, err)
	})

	t.Run("read from non-allowed path blocked", func(t *testing.T) {
		path := filepath.Join(tmpDir, "denied", "file.txt")
		_, err := sb.Validate(path, false)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "read not allowed")
	})
}

func TestSandboxStrictWithSymlink(t *testing.T) {
	tmpDir := t.TempDir()

	targetDir := filepath.Join(tmpDir, "target")
	os.MkdirAll(targetDir, 0755)

	linkPath := filepath.Join(tmpDir, "link")
	err := os.Symlink(targetDir, linkPath)
	require.NoError(t, err)

	sb, err := NewSandbox(Config{
		Mode:    ModeStrict,
		RootDir: tmpDir,
	})
	require.NoError(t, err)

	t.Run("allows path through symlink inside root", func(t *testing.T) {
		path := filepath.Join(linkPath, "file.txt")
		_, err := sb.Validate(path, false)
		assert.NoError(t, err)
	})
}

func TestStatDirectory(t *testing.T) {
	plugin := NewUnsafe()
	tmpDir := t.TempDir()

	result, err := plugin.stat(nil, nil, []core.Value{
		core.String{V: tmpDir},
	}, nil)
	require.NoError(t, err)

	m, ok := result.(*core.HashMap)
	require.True(t, ok)

	isdir, ok := m.Get(core.Keyword{V: "isdir"})
	require.True(t, ok)
	isdirBool, ok := isdir.(core.Bool)
	require.True(t, ok)
	assert.True(t, isdirBool.V)
}

func TestFileOperationsArgumentErrors(t *testing.T) {
	plugin := NewUnsafe()

	t.Run("read-file wrong arg count", func(t *testing.T) {
		_, err := plugin.readFile(nil, nil, []core.Value{}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "requires 1 argument")
	})

	t.Run("write-file wrong arg count", func(t *testing.T) {
		_, err := plugin.writeFile(nil, nil, []core.Value{core.String{V: "path"}}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "requires 2 arguments")
	})

	t.Run("write-file wrong content type", func(t *testing.T) {
		_, err := plugin.writeFile(nil, nil, []core.Value{
			core.String{V: "path"},
			core.Int{V: 123},
		}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "content must be string")
	})

	t.Run("exists? wrong arg count", func(t *testing.T) {
		_, err := plugin.exists(nil, nil, []core.Value{}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "requires 1 argument")
	})

	t.Run("exists? wrong type", func(t *testing.T) {
		_, err := plugin.exists(nil, nil, []core.Value{core.Int{V: 123}}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "must be string")
	})

	t.Run("ls wrong arg count", func(t *testing.T) {
		_, err := plugin.ls(nil, nil, []core.Value{}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "requires 1 argument")
	})

	t.Run("ls wrong type", func(t *testing.T) {
		_, err := plugin.ls(nil, nil, []core.Value{core.Int{V: 123}}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "must be string")
	})

	t.Run("mkdir wrong arg count", func(t *testing.T) {
		_, err := plugin.mkdir(nil, nil, []core.Value{}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "requires 1 argument")
	})

	t.Run("mkdir wrong type", func(t *testing.T) {
		_, err := plugin.mkdir(nil, nil, []core.Value{core.Int{V: 123}}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "must be string")
	})

	t.Run("stat wrong arg count", func(t *testing.T) {
		_, err := plugin.stat(nil, nil, []core.Value{}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "requires 1 argument")
	})

	t.Run("stat wrong type", func(t *testing.T) {
		_, err := plugin.stat(nil, nil, []core.Value{core.Int{V: 123}}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "must be string")
	})
}

func TestFileOperationsIOErrors(t *testing.T) {
	plugin := NewUnsafe()

	t.Run("read non-existent file", func(t *testing.T) {
		_, err := plugin.readFile(nil, nil, []core.Value{
			core.String{V: "/nonexistent/path/file.txt"},
		}, nil)
		assert.Error(t, err)
	})

	t.Run("ls non-existent directory", func(t *testing.T) {
		_, err := plugin.ls(nil, nil, []core.Value{
			core.String{V: "/nonexistent/directory"},
		}, nil)
		assert.Error(t, err)
	})

	t.Run("stat non-existent file", func(t *testing.T) {
		_, err := plugin.stat(nil, nil, []core.Value{
			core.String{V: "/nonexistent/file.txt"},
		}, nil)
		assert.Error(t, err)
	})

	t.Run("ls on file instead of directory", func(t *testing.T) {
		tmpFile := filepath.Join(t.TempDir(), "file.txt")
		os.WriteFile(tmpFile, []byte("test"), 0644)

		_, err := plugin.ls(nil, nil, []core.Value{
			core.String{V: tmpFile},
		}, nil)
		assert.Error(t, err)
	})

	t.Run("write to read-only location", func(t *testing.T) {
		_, err := plugin.writeFile(nil, nil, []core.Value{
			core.String{V: "/proc/nonexistent/file.txt"},
			core.String{V: "content"},
		}, nil)
		assert.Error(t, err)
	})
}

func TestEnvironmentOperationsErrors(t *testing.T) {
	plugin := NewUnsafe()

	t.Run("env-get wrong arg count", func(t *testing.T) {
		_, err := plugin.envGet(nil, nil, []core.Value{}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "requires 1 argument")
	})

	t.Run("env-set wrong arg count", func(t *testing.T) {
		_, err := plugin.envSet(nil, nil, []core.Value{core.String{V: "key"}}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "requires 2 arguments")
	})

	t.Run("env-set wrong value type", func(t *testing.T) {
		_, err := plugin.envSet(nil, nil, []core.Value{
			core.String{V: "key"},
			core.Int{V: 123},
		}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "value must be string")
	})
}

func TestEnvironmentOperations(t *testing.T) {
	plugin := NewUnsafe()

	t.Run("env-get returns nil for unset variable", func(t *testing.T) {
		result, err := plugin.envGet(nil, nil, []core.Value{
			core.String{V: "LISPICO_TEST_NONEXISTENT"},
		}, nil)
		require.NoError(t, err)
		assert.IsType(t, core.Nil{}, result)
	})

	t.Run("env-set and env-get", func(t *testing.T) {
		key := "LISPICO_TEST_VAR"
		value := "test_value"

		_, err := plugin.envSet(nil, nil, []core.Value{
			core.String{V: key},
			core.String{V: value},
		}, nil)
		require.NoError(t, err)

		result, err := plugin.envGet(nil, nil, []core.Value{
			core.String{V: key},
		}, nil)
		require.NoError(t, err)

		str, ok := result.(core.String)
		require.True(t, ok)
		assert.Equal(t, value, str.V)

		os.Unsetenv(key)
	})

	t.Run("env-get existing variable", func(t *testing.T) {
		result, err := plugin.envGet(nil, nil, []core.Value{
			core.String{V: "PATH"},
		}, nil)
		require.NoError(t, err)
		_, ok := result.(core.String)
		assert.True(t, ok)
	})
}

func TestContextCancellation(t *testing.T) {
	plugin := NewUnsafe()
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	os.WriteFile(testFile, []byte("content"), 0644)

	t.Run("read-file with cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := plugin.readFile(ctx, nil, []core.Value{
			core.String{V: testFile},
		}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "context canceled")
	})

	t.Run("write-file with cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := plugin.writeFile(ctx, nil, []core.Value{
			core.String{V: filepath.Join(tmpDir, "new.txt")},
			core.String{V: "content"},
		}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "context canceled")
	})

	t.Run("exists? with cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := plugin.exists(ctx, nil, []core.Value{
			core.String{V: testFile},
		}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "context canceled")
	})

	t.Run("ls with cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := plugin.ls(ctx, nil, []core.Value{
			core.String{V: tmpDir},
		}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "context canceled")
	})

	t.Run("mkdir with cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := plugin.mkdir(ctx, nil, []core.Value{
			core.String{V: filepath.Join(tmpDir, "newdir")},
		}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "context canceled")
	})

	t.Run("stat with cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := plugin.stat(ctx, nil, []core.Value{
			core.String{V: testFile},
		}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "context canceled")
	})

	t.Run("env-get with cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := plugin.envGet(ctx, nil, []core.Value{
			core.String{V: "PATH"},
		}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "context canceled")
	})

	t.Run("env-set with cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := plugin.envSet(ctx, nil, []core.Value{
			core.String{V: "TEST_KEY"},
			core.String{V: "test_value"},
		}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "context canceled")
	})
}

func TestSandboxSiblingPrefixBypass(t *testing.T) {
	tmpDir := t.TempDir()
	root := filepath.Join(tmpDir, "data")
	require.NoError(t, os.Mkdir(root, 0o755))

	t.Run("strict rejects sibling sharing the root string prefix", func(t *testing.T) {
		sb, err := NewSandbox(Config{Mode: ModeStrict, RootDir: root})
		require.NoError(t, err)

		path := filepath.Join(tmpDir, "data-evil", "secret.txt")
		_, err = sb.Validate(path, false)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "outside sandbox root")
	})

	t.Run("strict rejects dotdot traversal into sibling", func(t *testing.T) {
		sb, err := NewSandbox(Config{Mode: ModeStrict, RootDir: root})
		require.NoError(t, err)

		path := filepath.Join(root, "..", "data-evil", "secret.txt")
		_, err = sb.Validate(path, false)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "outside sandbox root")
	})

	t.Run("relaxed rejects sibling sharing an allowed prefix", func(t *testing.T) {
		sb, err := NewSandbox(Config{Mode: ModeRelaxed, AllowRead: []string{root}})
		require.NoError(t, err)

		path := filepath.Join(tmpDir, "data-evil", "secret.txt")
		_, err = sb.Validate(path, false)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "read not allowed")
	})

	t.Run("strict allows a filename containing dotdot", func(t *testing.T) {
		sb, err := NewSandbox(Config{Mode: ModeStrict, RootDir: root})
		require.NoError(t, err)

		path := filepath.Join(root, "a..b.txt")
		validated, err := sb.Validate(path, false)
		assert.NoError(t, err)
		assert.Equal(t, path, validated)
	})
}

func TestSandboxSymlinkCycle(t *testing.T) {
	tmpDir := t.TempDir()
	a := filepath.Join(tmpDir, "loop-a")
	b := filepath.Join(tmpDir, "loop-b")
	require.NoError(t, os.Symlink(b, a))
	require.NoError(t, os.Symlink(a, b))

	sb, err := NewSandbox(Config{Mode: ModeStrict, RootDir: tmpDir})
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() {
		_, verr := sb.Validate(a, false)
		done <- verr
	}()

	select {
	case verr := <-done:
		require.Error(t, verr)
		assert.Contains(t, verr.Error(), "too many symlink levels")
	case <-time.After(2 * time.Second):
		t.Fatal("Validate hung on symlink cycle")
	}
}
