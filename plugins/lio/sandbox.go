package lio

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type SandboxMode int

const (
	ModeStrict  SandboxMode = iota
	ModeRelaxed SandboxMode = iota
	ModeNone    SandboxMode = iota
)

type Config struct {
	Mode        SandboxMode
	RootDir     string
	AllowRead   []string
	AllowWrite  []string
	DenyPattern string
}

type Sandbox struct {
	cfg        Config
	denyRegex  *regexp.Regexp
	rootDirAbs string
	// root pins the root directory inode so opens through it cannot
	// follow a symlink swapped after validation.
	root *os.Root
}

func NewSandbox(cfg Config) (*Sandbox, error) {
	s := &Sandbox{cfg: cfg}

	if cfg.Mode == ModeNone {
		return s, nil
	}

	if cfg.DenyPattern != "" {
		re, err := regexp.Compile(cfg.DenyPattern)
		if err != nil {
			return nil, fmt.Errorf("invalid deny pattern: %w", err)
		}
		s.denyRegex = re
	}

	if cfg.RootDir != "" {
		abs, err := filepath.Abs(cfg.RootDir)
		if err != nil {
			return nil, fmt.Errorf("resolve root dir: %w", err)
		}
		root, err := resolveReal(abs)
		if err != nil {
			return nil, fmt.Errorf("resolve root dir: %w", err)
		}
		s.rootDirAbs = root
		pinned, err := os.OpenRoot(root)
		if err != nil {
			return nil, fmt.Errorf("open root dir: %w", err)
		}
		s.root = pinned
	} else if cfg.Mode == ModeStrict {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("get working directory: %w", err)
		}
		root, err := resolveReal(cwd)
		if err != nil {
			return nil, fmt.Errorf("resolve working directory: %w", err)
		}
		s.rootDirAbs = root
		pinned, err := os.OpenRoot(root)
		if err != nil {
			return nil, fmt.Errorf("open root dir: %w", err)
		}
		s.root = pinned
	}

	return s, nil
}

// openFileWithin opens a validated path. When a pinned root exists, the
// open goes through it so containment is enforced by the open syscall
// itself: a symlink swapped after Validate cannot escape. Paths outside
// the root are only reachable in relaxed mode via an explicit allowlist
// entry, and fall back to the plain open there.
func (s *Sandbox) openFileWithin(validated string, flag int, perm os.FileMode) (*os.File, error) {
	if s.root == nil || (s.cfg.Mode != ModeStrict && !withinRoot(validated, s.rootDirAbs)) {
		return os.OpenFile(validated, flag, perm)
	}
	rel, err := filepath.Rel(s.rootDirAbs, validated)
	if err != nil {
		return nil, err
	}
	return s.root.OpenFile(rel, flag, perm)
}

func (s *Sandbox) mkdirAllWithin(validated string, perm os.FileMode) error {
	return s.mkdirAllRooted(filepath.Dir(validated), perm)
}

// mkdirAllRooted creates each component through the pinned root when one
// applies, so a symlink swapped mid-creation cannot escape.
func (s *Sandbox) mkdirAllRooted(dir string, perm os.FileMode) error {
	if s.root == nil || (s.cfg.Mode != ModeStrict && !withinRoot(dir, s.rootDirAbs)) {
		return os.MkdirAll(dir, perm)
	}
	rel, err := filepath.Rel(s.rootDirAbs, dir)
	if err != nil {
		return err
	}
	// os.Root.MkdirAll needs go1.25; go.mod declares go1.24.
	cur := ""
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		if part == "" {
			continue
		}
		if cur == "" {
			cur = part
		} else {
			cur += string(os.PathSeparator) + part
		}
		if err := s.root.Mkdir(cur, perm); err != nil && !os.IsExist(err) {
			return err
		}
	}
	return nil
}

// statWithin goes through the pinned root when one applies, so a symlink
// swapped after Validate cannot make the stat reach outside the tree.
func (s *Sandbox) statWithin(validated string) (os.FileInfo, error) {
	if s.root == nil || (s.cfg.Mode != ModeStrict && !withinRoot(validated, s.rootDirAbs)) {
		return os.Stat(validated)
	}
	rel, err := filepath.Rel(s.rootDirAbs, validated)
	if err != nil {
		return nil, err
	}
	return s.root.Stat(rel)
}

// readDirN is the directory enumeration seam; tests substitute it to
// observe the bound passed to the kernel.
var readDirN = (*os.File).ReadDir

// readDirWithin lists at most n entries of a validated directory (n < 0
// means all) through the pinned root when one applies, for the same
// containment reason as statWithin.
func (s *Sandbox) readDirWithin(validated string, n int) ([]os.DirEntry, error) {
	var dir *os.File
	if s.root == nil || (s.cfg.Mode != ModeStrict && !withinRoot(validated, s.rootDirAbs)) {
		d, err := os.Open(validated)
		if err != nil {
			return nil, err
		}
		dir = d
	} else {
		rel, err := filepath.Rel(s.rootDirAbs, validated)
		if err != nil {
			return nil, err
		}
		d, err := s.root.Open(rel)
		if err != nil {
			return nil, err
		}
		dir = d
	}
	defer func() { _ = dir.Close() }()
	entries, err := readDirN(dir, n)
	if errors.Is(err, io.EOF) {
		return entries, nil
	}
	return entries, err
}

func (s *Sandbox) Validate(path string, write bool) (string, error) {
	if s.cfg.Mode == ModeNone {
		return filepath.Abs(path)
	}

	cleanPath, err := s.cleanPath(path)
	if err != nil {
		return "", err
	}

	if s.denyRegex != nil && s.denyRegex.MatchString(cleanPath) {
		return "", fmt.Errorf("path denied by pattern: %s", path)
	}

	switch s.cfg.Mode {
	case ModeStrict:
		return s.validateStrict(cleanPath, write)
	case ModeRelaxed:
		return s.validateRelaxed(cleanPath, write)
	default:
		return cleanPath, nil
	}
}

func (s *Sandbox) cleanPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}

	resolved, err := resolveReal(abs)
	if err != nil {
		return "", fmt.Errorf("resolve symlink: %w", err)
	}

	return resolved, nil
}

// resolveReal resolves every symlink in path, including intermediate
// directory components, so a symlink cannot make a path outside the sandbox
// root look like it is inside. When path (a write target, typically) does
// not exist yet, it walks up to the nearest existing ancestor, resolves
// that, and rejoins the missing suffix onto the real ancestor.
func resolveReal(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}

	var suffix []string
	dir := path
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", err
		}
		suffix = append([]string{filepath.Base(dir)}, suffix...)
		dir = parent

		resolvedParent, perr := filepath.EvalSymlinks(dir)
		if perr == nil {
			child := filepath.Join(resolvedParent, suffix[0])
			if fi, lerr := os.Lstat(child); lerr == nil && fi.Mode()&os.ModeSymlink != 0 {
				return "", fmt.Errorf("broken symlink: %s", child)
			}
			return filepath.Join(append([]string{resolvedParent}, suffix...)...), nil
		}
		if !os.IsNotExist(perr) {
			return "", perr
		}

		if fi, lerr := os.Lstat(dir); lerr == nil && fi.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("broken symlink: %s", dir)
		}
	}
}

func (s *Sandbox) validateStrict(path string, write bool) (string, error) {
	if s.rootDirAbs == "" {
		return "", fmt.Errorf("strict mode requires root directory")
	}

	if !withinRoot(path, s.rootDirAbs) {
		return "", fmt.Errorf("path outside sandbox root: %s", path)
	}

	return path, nil
}

func withinRoot(path, root string) bool {
	return path == root || strings.HasPrefix(path, root+string(os.PathSeparator))
}

func (s *Sandbox) validateRelaxed(path string, write bool) (string, error) {
	list := s.cfg.AllowRead
	if write {
		list = s.cfg.AllowWrite
	}

	// An empty applicable allowlist denies everything: relaxed mode grants
	// access only through explicit allowlist entries, never by omission.
	for _, allowed := range list {
		absAllowed, err := filepath.Abs(allowed)
		if err != nil {
			continue
		}

		if withinRoot(path, absAllowed) {
			return path, nil
		}
	}

	op := "read"
	if write {
		op = "write"
	}
	return "", fmt.Errorf("%s not allowed: %s", op, path)
}
