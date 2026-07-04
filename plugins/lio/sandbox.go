package lio

import (
	"fmt"
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
	}

	return s, nil
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

	if len(list) == 0 {
		return path, nil
	}

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
