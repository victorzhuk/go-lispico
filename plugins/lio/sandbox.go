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
		s.rootDirAbs = abs
	} else if cfg.Mode == ModeStrict {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("get working directory: %w", err)
		}
		s.rootDirAbs = cwd
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
	if strings.Contains(path, "..") {
		cleaned := filepath.Clean(path)
		if strings.Contains(cleaned, "..") {
			return "", fmt.Errorf("path traversal detected: %s", path)
		}
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}

	realPath, err := s.resolveSymlink(abs)
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("resolve symlink: %w", err)
	}
	if realPath != "" {
		abs = realPath
	}

	return abs, nil
}

func (s *Sandbox) resolveSymlink(path string) (string, error) {
	for {
		fi, err := os.Lstat(path)
		if err != nil {
			return "", err
		}

		if fi.Mode()&os.ModeSymlink == 0 {
			return path, nil
		}

		target, err := os.Readlink(path)
		if err != nil {
			return "", fmt.Errorf("read symlink: %w", err)
		}

		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(path), target)
		}

		path = target
	}
}

func (s *Sandbox) validateStrict(path string, write bool) (string, error) {
	if s.rootDirAbs == "" {
		return "", fmt.Errorf("strict mode requires root directory")
	}

	if !strings.HasPrefix(path, s.rootDirAbs) {
		return "", fmt.Errorf("path outside sandbox root: %s", path)
	}

	return path, nil
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

		if strings.HasPrefix(path, absAllowed) || path == absAllowed {
			return path, nil
		}
	}

	op := "read"
	if write {
		op = "write"
	}
	return "", fmt.Errorf("%s not allowed: %s", op, path)
}
