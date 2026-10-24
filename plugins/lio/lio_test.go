package lio

import (
	"bytes"
	"context"
	"io"
	"math"
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
		assert.Len(t, list.ToSlice(), 2)
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
	path := filepath.Join(tmpDir, "test.txt")

	t.Run("empty read allowlist denies reads", func(t *testing.T) {
		sb, err := NewSandbox(Config{
			Mode:      ModeRelaxed,
			AllowRead: []string{},
		})
		require.NoError(t, err)

		_, err = sb.Validate(path, false)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "read not allowed")
	})

	t.Run("nil write allowlist denies writes", func(t *testing.T) {
		sb, err := NewSandbox(Config{
			Mode:       ModeRelaxed,
			AllowRead:  []string{tmpDir},
			AllowWrite: nil,
		})
		require.NoError(t, err)

		_, err = sb.Validate(path, true)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "write not allowed")
	})

	t.Run("both lists nil deny everything", func(t *testing.T) {
		sb, err := NewSandbox(Config{Mode: ModeRelaxed})
		require.NoError(t, err)

		_, err = sb.Validate(path, false)
		assert.Error(t, err)
		_, err = sb.Validate(path, true)
		assert.Error(t, err)
	})
}

func TestSandboxModeNoneUnrestricted(t *testing.T) {
	tmpDir := t.TempDir()

	sb, err := NewSandbox(Config{Mode: ModeNone})
	require.NoError(t, err)

	inside := filepath.Join(tmpDir, "inside.txt")
	require.NoError(t, os.WriteFile(inside, []byte("inside"), 0o644))

	t.Run("ModeNone bypasses all validation", func(t *testing.T) {
		for _, path := range []string{inside, "/etc/hostname", filepath.Join(tmpDir, "missing.txt")} {
			validated, verr := sb.Validate(path, true)
			assert.NoError(t, verr)
			assert.NotEmpty(t, validated)
			_, verr = sb.Validate(path, false)
			assert.NoError(t, verr)
		}
	})

	t.Run("ModeNone plugin reads and stats any path", func(t *testing.T) {
		plugin := NewUnsafe()

		result, err := plugin.exists(nil, nil, []core.Value{core.String{V: inside}}, nil)
		require.NoError(t, err)
		assert.True(t, result.(core.Bool).V)

		_, err = plugin.stat(nil, nil, []core.Value{core.String{V: inside}}, nil)
		assert.NoError(t, err)

		_, err = plugin.readFile(nil, nil, []core.Value{core.String{V: inside}}, nil)
		assert.NoError(t, err)
	})
}

func TestRootConfinedOpsSymlinkSwap(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "leaked.txt"), []byte("leaked"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(root, "d"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "d", "note.txt"), []byte("inside"), 0o644))

	sb, err := NewSandbox(Config{Mode: ModeStrict, RootDir: root})
	require.NoError(t, err)

	validatedNote := filepath.Join(root, "d", "note.txt")
	_, err = sb.Validate(validatedNote, false)
	require.NoError(t, err)

	// Swap the validated directory component for a symlink to the outside
	// tree after Validate, reproducing the TOCTOU window for every op.
	require.NoError(t, os.Rename(filepath.Join(root, "d"), filepath.Join(root, "d.real")))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "d")))

	t.Run("exists never confirms outside content", func(t *testing.T) {
		plugin, err := New(Config{Mode: ModeStrict, RootDir: root})
		require.NoError(t, err)

		// After the swap the path resolves outside the root: validation
		// rejects it, or root-confined stat reports it as absent. Either
		// way it must never report the outside file as existing.
		result, err := plugin.exists(nil, nil, []core.Value{core.String{V: validatedNote}}, nil)
		if err == nil {
			assert.False(t, result.(core.Bool).V,
				"exists must not confirm the swapped outside target")
		} else {
			assert.Contains(t, err.Error(), "io/exists?")
		}
	})

	t.Run("ls never lists outside entries", func(t *testing.T) {
		plugin, err := New(Config{Mode: ModeStrict, RootDir: root})
		require.NoError(t, err)

		result, err := plugin.ls(nil, nil, []core.Value{core.String{V: filepath.Join(root, "d")}}, nil)
		if err == nil {
			for _, item := range result.(core.List).ToSlice() {
				assert.NotEqual(t, "leaked.txt", item.(core.String).V,
					"ls must not read entries through the swapped symlink")
			}
		} else {
			assert.Contains(t, err.Error(), "io/ls")
		}
	})

	t.Run("stat never reports outside file", func(t *testing.T) {
		plugin, err := New(Config{Mode: ModeStrict, RootDir: root})
		require.NoError(t, err)

		_, err = plugin.stat(nil, nil, []core.Value{core.String{V: filepath.Join(root, "d", "secret.txt")}}, nil)
		assert.Error(t, err, "stat through root must refuse the swapped symlink")
	})

	t.Run("mkdir never creates outside", func(t *testing.T) {
		plugin, err := New(Config{Mode: ModeStrict, RootDir: root})
		require.NoError(t, err)

		_, err = plugin.mkdir(nil, nil, []core.Value{core.String{V: filepath.Join(root, "d", "sub")}}, nil)
		assert.Error(t, err)

		_, err = os.Stat(filepath.Join(outside, "sub"))
		assert.True(t, os.IsNotExist(err), "mkdir must not create inside the outside tree")
	})

	t.Run("read through swapped symlink never returns outside content", func(t *testing.T) {
		plugin, err := New(Config{Mode: ModeStrict, RootDir: root})
		require.NoError(t, err)

		result, err := plugin.readFile(nil, nil, []core.Value{core.String{V: validatedNote}}, nil)
		if err == nil {
			assert.NotEqual(t, "secret", result.(core.String).V,
				"read must not observe outside content via the swapped symlink")
		}
	})
}

func TestNoContentEscapeThroughValidatedPath(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("top secret"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "inside.txt"), []byte("inside"), 0o644))

	sb, err := NewSandbox(Config{Mode: ModeStrict, RootDir: root})
	require.NoError(t, err)

	validated, err := sb.Validate(filepath.Join(root, "inside.txt"), false)
	require.NoError(t, err)

	// Turn the validated path's leaf into a symlink pointing outside after
	// validation: openFileWithin must refuse, never read the outside file.
	require.NoError(t, os.Rename(filepath.Join(root, "inside.txt"), filepath.Join(root, "inside.real")))
	require.NoError(t, os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "inside.txt")))

	f, err := sb.openFileWithin(validated, os.O_RDONLY, 0)
	if err == nil {
		content, rerr := io.ReadAll(f)
		_ = f.Close()
		require.NoError(t, rerr)
		assert.Equal(t, "inside", string(content), "read must stay on the pinned inode")
		return
	}

	plugin, err := New(Config{Mode: ModeStrict, RootDir: root})
	require.NoError(t, err)
	_, rerr := plugin.readFile(nil, nil, []core.Value{core.String{V: validated}}, nil)
	require.Error(t, rerr, "content must never come from the swapped outside target")
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

func TestEnvironmentIsolationBetweenPlugins(t *testing.T) {
	p1 := NewUnsafe()
	p2 := NewUnsafe()

	key := "LISPICO_TEST_ISOLATION"

	_, err := p1.envSet(nil, nil, []core.Value{
		core.String{V: key},
		core.String{V: "from-p1"},
	}, nil)
	require.NoError(t, err)

	result, err := p1.envGet(nil, nil, []core.Value{core.String{V: key}}, nil)
	require.NoError(t, err)
	assert.Equal(t, "from-p1", result.(core.String).V)

	result, err = p2.envGet(nil, nil, []core.Value{core.String{V: key}}, nil)
	require.NoError(t, err)
	assert.IsType(t, core.Nil{}, result)

	_, exists := os.LookupEnv(key)
	assert.False(t, exists, "env-set must not mutate the real process environment")
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
		assert.Contains(t, verr.Error(), "too many links")
	case <-time.After(2 * time.Second):
		t.Fatal("Validate hung on symlink cycle")
	}
}

func TestSandboxSymlinkEscape(t *testing.T) {
	tmpDir := t.TempDir()

	root := filepath.Join(tmpDir, "root")
	require.NoError(t, os.Mkdir(root, 0o755))

	outside := filepath.Join(tmpDir, "outside")
	require.NoError(t, os.Mkdir(outside, 0o755))

	secret := filepath.Join(outside, "secret.txt")
	require.NoError(t, os.WriteFile(secret, []byte("top secret"), 0o644))

	link := filepath.Join(root, "link")
	require.NoError(t, os.Symlink(outside, link))

	sb, err := NewSandbox(Config{Mode: ModeStrict, RootDir: root})
	require.NoError(t, err)

	t.Run("read through symlinked directory is denied", func(t *testing.T) {
		_, err := sb.Validate(filepath.Join(link, "secret.txt"), false)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "outside sandbox root")
	})

	t.Run("write through symlinked directory is denied", func(t *testing.T) {
		_, err := sb.Validate(filepath.Join(link, "new-secret.txt"), true)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "outside sandbox root")
	})

	t.Run("read inside root still works", func(t *testing.T) {
		inRoot := filepath.Join(root, "note.txt")
		require.NoError(t, os.WriteFile(inRoot, []byte("hello"), 0o644))

		validated, err := sb.Validate(inRoot, false)
		assert.NoError(t, err)
		assert.Equal(t, inRoot, validated)
	})

	t.Run("write inside root still works", func(t *testing.T) {
		inRoot := filepath.Join(root, "new-note.txt")

		validated, err := sb.Validate(inRoot, true)
		assert.NoError(t, err)
		assert.Equal(t, inRoot, validated)
	})
}

func TestSandboxDanglingSymlinkDenied(t *testing.T) {
	tmpDir := t.TempDir()

	root := filepath.Join(tmpDir, "root")
	require.NoError(t, os.Mkdir(root, 0o755))

	dangling := filepath.Join(root, "dangling")
	require.NoError(t, os.Symlink(filepath.Join(tmpDir, "does-not-exist"), dangling))

	sb, err := NewSandbox(Config{Mode: ModeStrict, RootDir: root})
	require.NoError(t, err)

	_, err = sb.Validate(filepath.Join(dangling, "file.txt"), true)
	assert.Error(t, err)
}

func TestSandboxDanglingSymlinkLeafDenied(t *testing.T) {
	tmpDir := t.TempDir()

	root := filepath.Join(tmpDir, "root")
	require.NoError(t, os.Mkdir(root, 0o755))

	// A symlink whose leaf IS the write target, pointing outside root at a path
	// that does not exist yet. Writing through it must be denied, not resolved
	// to an in-root name.
	outside := filepath.Join(tmpDir, "outside", "evil.txt")
	leaf := filepath.Join(root, "escape")
	require.NoError(t, os.Symlink(outside, leaf))

	sb, err := NewSandbox(Config{Mode: ModeStrict, RootDir: root})
	require.NoError(t, err)

	_, err = sb.Validate(leaf, true)
	assert.Error(t, err)
}

func TestOpenThroughRootSurvivesSymlinkSwap(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(root, "d"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "d", "secret.txt"), []byte("inside"), 0o644))

	sb, err := NewSandbox(Config{Mode: ModeStrict, RootDir: root})
	require.NoError(t, err)

	validated, err := sb.Validate(filepath.Join(root, "d", "secret.txt"), false)
	require.NoError(t, err)

	// Swap the validated directory component for a symlink to the outside
	// tree, reproducing the validate-to-open TOCTOU window.
	require.NoError(t, os.Rename(filepath.Join(root, "d"), filepath.Join(root, "d.real")))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "d")))

	t.Run("read never observes outside content", func(t *testing.T) {
		f, err := sb.openFileWithin(validated, os.O_RDONLY, 0)
		if err == nil {
			content, rerr := io.ReadAll(f)
			_ = f.Close()
			require.NoError(t, rerr)
			assert.Equal(t, "inside", string(content))
			return
		}
		assert.NotEmpty(t, err.Error())
	})

	t.Run("write never reaches outside target", func(t *testing.T) {
		validatedWrite, err := sb.Validate(filepath.Join(root, "d.real", "secret.txt"), true)
		require.NoError(t, err)
		// Aim the write at a path whose directory component is the swapped symlink.
		swapped := filepath.Join(root, "d", "secret.txt")
		_, verr := sb.Validate(swapped, true)
		if verr == nil {
			f, oerr := sb.openFileWithin(validatedWrite, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
			_ = f
			if oerr == nil {
				t.Fatal("open through root must refuse the swapped path")
			}
		}
		// The outside secret must remain untouched regardless.
		got, rerr := os.ReadFile(filepath.Join(outside, "secret.txt"))
		require.NoError(t, rerr)
		assert.Equal(t, "secret", string(got))
	})
}

func TestReadFileSizeCap(t *testing.T) {
	tmpDir := t.TempDir()
	plugin, err := New(Config{Mode: ModeStrict, RootDir: tmpDir})
	require.NoError(t, err)

	at := filepath.Join(tmpDir, "at.txt")
	require.NoError(t, os.WriteFile(at, bytes.Repeat([]byte("a"), int(maxReadFileBytes)), 0o644))
	over := filepath.Join(tmpDir, "over.txt")
	require.NoError(t, os.WriteFile(over, bytes.Repeat([]byte("b"), int(maxReadFileBytes)+1), 0o644))

	t.Run("read exactly at cap succeeds", func(t *testing.T) {
		result, err := plugin.readFile(nil, nil, []core.Value{core.String{V: at}}, nil)
		require.NoError(t, err)
		str, ok := result.(core.String)
		require.True(t, ok)
		assert.Len(t, str.V, int(maxReadFileBytes))
	})

	t.Run("read above cap fails closed", func(t *testing.T) {
		_, err := plugin.readFile(nil, nil, []core.Value{core.String{V: over}}, nil)
		require.Error(t, err)
		var lerr *core.LispicoError
		require.ErrorAs(t, err, &lerr)
		assert.Equal(t, core.CodeResourceLimit, lerr.Code)
	})
}

func TestLsCollectionLimit(t *testing.T) {
	tmpDir := t.TempDir()
	outside := t.TempDir()
	plugin, err := New(Config{Mode: ModeStrict, RootDir: tmpDir})
	require.NoError(t, err)

	names := []string{"a.txt", "b.txt", "c.txt"}
	for _, name := range names {
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, name), []byte(name), 0o644))
	}
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o644))

	limiter := func(n int) core.Evaluator {
		ev := core.NewEvaluator()
		ev.MaxCollectionLen = n
		return ev
	}

	t.Run("at limit succeeds in order", func(t *testing.T) {
		result, err := plugin.ls(context.Background(), limiter(len(names)), []core.Value{core.String{V: tmpDir}}, nil)
		require.NoError(t, err)
		got := result.(core.List).ToSlice()
		require.Len(t, got, len(names))
		for i, name := range names {
			assert.Equal(t, name, got[i].(core.String).V)
		}
	})

	t.Run("above limit fails closed before returning entries", func(t *testing.T) {
		err := evalLsErr(t, plugin, limiter(len(names)-1), tmpDir)
		require.Error(t, err)
		var lerr *core.LispicoError
		require.ErrorAs(t, err, &lerr)
		assert.Equal(t, core.CodeResourceLimit, lerr.Code)
		assert.ErrorContains(t, err, "exceeds collection limit 2")
		assert.ErrorContains(t, err, tmpDir)
	})

	t.Run("unlimited evaluator is unchanged", func(t *testing.T) {
		_, err := plugin.ls(context.Background(), limiter(0), []core.Value{core.String{V: tmpDir}}, nil)
		require.NoError(t, err)
	})

	t.Run("containment unchanged under a limit", func(t *testing.T) {
		err := evalLsErr(t, plugin, limiter(2), outside)
		require.Error(t, err)
		assert.ErrorContains(t, err, "io/ls")
		assert.NotContains(t, err.Error(), "collection limit",
			"outside path must fail validation, not the collection limit")
	})
}

func TestLsBoundedEnumeration(t *testing.T) {
	tmpDir := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt", "c.txt", "d.txt", "e.txt"} {
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, name), []byte(name), 0o644))
	}
	plugin, err := New(Config{Mode: ModeStrict, RootDir: tmpDir})
	require.NoError(t, err)

	ev := core.NewEvaluator()
	ev.MaxCollectionLen = 2

	orig := readDirN
	t.Cleanup(func() { readDirN = orig })
	var requested int
	readDirN = func(f *os.File, n int) ([]os.DirEntry, error) {
		requested = n
		return orig(f, n)
	}

	err = evalLsErr(t, plugin, ev, tmpDir)
	require.Error(t, err)
	var lerr *core.LispicoError
	require.ErrorAs(t, err, &lerr)
	assert.Equal(t, core.CodeResourceLimit, lerr.Code)
	assert.ErrorContains(t, err, "exceeds collection limit 2")
	assert.Equal(t, 3, requested, "enumeration must stop at limit+1 entries")
}

func TestLsLimitAtMaxInt(t *testing.T) {
	tmpDir := t.TempDir()
	names := []string{"a.txt", "b.txt"}
	for _, name := range names {
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, name), []byte(name), 0o644))
	}
	plugin, err := New(Config{Mode: ModeStrict, RootDir: tmpDir})
	require.NoError(t, err)

	ev := core.NewEvaluator()
	ev.MaxCollectionLen = math.MaxInt

	orig := readDirN
	t.Cleanup(func() { readDirN = orig })
	var requested []int
	readDirN = func(f *os.File, n int) ([]os.DirEntry, error) {
		assert.GreaterOrEqual(t, n, 0, "ReadDir count must never be negative")
		requested = append(requested, n)
		return orig(f, n)
	}

	result, err := plugin.ls(context.Background(), ev, []core.Value{core.String{V: tmpDir}}, nil)
	require.NoError(t, err)
	got := result.(core.List).ToSlice()
	require.Len(t, got, len(names))
	assert.Equal(t, names[0], got[0].(core.String).V)
	assert.Equal(t, names[1], got[1].(core.String).V)
	require.NotEmpty(t, requested)
	assert.NotContains(t, requested, -1)
}

func evalLsErr(t *testing.T, plugin *Plugin, eval core.Evaluator, dir string) error {
	t.Helper()
	_, err := plugin.ls(context.Background(), eval, []core.Value{core.String{V: dir}}, nil)
	return err
}

func TestLsEmptyDirUnderLimiter(t *testing.T) {
	tmpDir := t.TempDir()
	emptyDir := filepath.Join(tmpDir, "empty")
	require.NoError(t, os.Mkdir(emptyDir, 0o755))
	plugin, err := New(Config{Mode: ModeStrict, RootDir: tmpDir})
	require.NoError(t, err)

	ev := core.NewEvaluator()
	ev.MaxCollectionLen = 2

	result, err := plugin.ls(context.Background(), ev, []core.Value{core.String{V: emptyDir}}, nil)
	require.NoError(t, err)
	got := result.(core.List).ToSlice()
	assert.Empty(t, got)
}

func TestLsSortedOutput(t *testing.T) {
	tmpDir := t.TempDir()
	plugin, err := New(Config{Mode: ModeStrict, RootDir: tmpDir})
	require.NoError(t, err)

	names := []string{"z.txt", "a.txt", "m.txt"}
	for _, name := range names {
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, name), nil, 0o644))
	}
	sorted := []string{"a.txt", "m.txt", "z.txt"}

	limiter := func(n int) core.Evaluator {
		ev := core.NewEvaluator()
		ev.MaxCollectionLen = n
		return ev
	}

	t.Run("limited", func(t *testing.T) {
		result, err := plugin.ls(context.Background(), limiter(len(names)), []core.Value{core.String{V: tmpDir}}, nil)
		require.NoError(t, err)
		got := result.(core.List).ToSlice()
		require.Len(t, got, len(sorted))
		for i, name := range sorted {
			assert.Equal(t, name, got[i].(core.String).V)
		}
	})

	t.Run("unlimited", func(t *testing.T) {
		result, err := plugin.ls(context.Background(), limiter(0), []core.Value{core.String{V: tmpDir}}, nil)
		require.NoError(t, err)
		got := result.(core.List).ToSlice()
		require.Len(t, got, len(sorted))
		for i, name := range sorted {
			assert.Equal(t, name, got[i].(core.String).V)
		}
	})
}
