// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// EnvRoot overrides the default dpm root.
	EnvRoot = "DPM_ROOT"
	// EnvRegistryURL overrides the default Git registry URL.
	EnvRegistryURL = "DPM_REGISTRY_URL"
	// EnvRegistryStaticIndex enables reading generated registry index files.
	EnvRegistryStaticIndex = "DPM_REGISTRY_STATIC_INDEX"
	// EnvRegistryPublicKeys configures trusted registry snapshot public keys.
	EnvRegistryPublicKeys = "DPM_REGISTRY_PUBLIC_KEYS"
	// DefaultRegistryURL is the anonymous official Git registry used by dpm update.
	DefaultRegistryURL = "https://github.com/kevherro/dpm-registry.git"
)

// Config contains the filesystem paths dpm is allowed to manage.
type Config struct {
	Root                string
	BinDir              string
	PkgsDir             string
	DownloadsDir        string
	CacheDir            string
	RegistryDir         string
	RegistryURL         string
	RegistryStaticIndex bool
	RegistryPublicKeys  string
	StateDir            string
}

// Default returns configuration derived from the environment.
func Default() (Config, error) {
	if root := os.Getenv(EnvRoot); root != "" {
		cfg, err := FromRoot(root)
		if err != nil {
			return Config{}, err
		}
		if err := validateOverrideRoot(cfg.Root); err != nil {
			return Config{}, err
		}
		return withRegistryEnv(cfg, nil)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, fmt.Errorf("find home directory: %w", err)
	}

	return withRegistryEnv(FromRoot(filepath.Join(home, ".dpm")))
}

// RequireClientMutation rejects client writes made with effective UID 0.
func (cfg Config) RequireClientMutation() error {
	return cfg.requireClientMutation(os.Geteuid())
}

func (cfg Config) requireClientMutation(euid int) error {
	if euid == 0 {
		return fmt.Errorf("dpm refuses client filesystem mutations with effective UID 0; run without sudo")
	}

	return cfg.ValidateLayout()
}

// FromRoot returns configuration rooted at root.
func FromRoot(root string) (Config, error) {
	if root == "" {
		return Config{}, fmt.Errorf("dpm root is empty")
	}

	absRoot, err := canonicalRoot(root)
	if err != nil {
		return Config{}, err
	}

	return Config{
		Root:         absRoot,
		BinDir:       filepath.Join(absRoot, "bin"),
		PkgsDir:      filepath.Join(absRoot, "pkgs"),
		DownloadsDir: filepath.Join(absRoot, "downloads"),
		CacheDir:     filepath.Join(absRoot, "cache"),
		RegistryDir:  filepath.Join(absRoot, "registry"),
		RegistryURL:  DefaultRegistryURL,
		StateDir:     filepath.Join(absRoot, "state"),
	}, nil
}

func withRegistryEnv(cfg Config, err error) (Config, error) {
	if err != nil {
		return Config{}, err
	}
	if url := os.Getenv(EnvRegistryURL); url != "" {
		cfg.RegistryURL = url
	}
	if truthy(os.Getenv(EnvRegistryStaticIndex)) {
		cfg.RegistryStaticIndex = true
	}
	cfg.RegistryPublicKeys = strings.TrimSpace(os.Getenv(EnvRegistryPublicKeys))

	return cfg, nil
}

func truthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// EnsureDirs creates the standard dpm directory layout.
func (cfg Config) EnsureDirs() error {
	if err := cfg.ValidateLayout(); err != nil {
		return err
	}
	if err := cfg.RequireInsideRoot(cfg.Root); err != nil {
		return err
	}
	if err := ensureDirectory(cfg.Root, true); err != nil {
		return err
	}
	for _, dir := range []string{
		cfg.BinDir,
		cfg.PkgsDir,
		cfg.DownloadsDir,
		cfg.CacheDir,
		cfg.RegistryDir,
		cfg.StateDir,
	} {
		if err := cfg.RequireInsideRoot(dir); err != nil {
			return err
		}
		if err := ensureDirectory(dir, false); err != nil {
			return err
		}
	}

	return nil
}

// ValidateLayout rejects symlinks and non-directories in existing managed paths.
func (cfg Config) ValidateLayout() error {
	for _, dir := range []string{
		cfg.Root,
		cfg.BinDir,
		cfg.PkgsDir,
		cfg.DownloadsDir,
		cfg.CacheDir,
		cfg.RegistryDir,
		cfg.StateDir,
	} {
		if err := cfg.RequireInsideRoot(dir); err != nil {
			return err
		}
		info, err := os.Lstat(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect managed directory %s: %w", dir, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("managed directory %s is a symlink", dir)
		}
		if !info.IsDir() {
			return fmt.Errorf("managed path %s is not a directory", dir)
		}
	}

	return nil
}

func canonicalRoot(root string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve dpm root %q: %w", root, err)
	}
	absRoot = filepath.Clean(absRoot)
	if info, err := os.Lstat(absRoot); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("dpm root %s is a symlink", absRoot)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("inspect dpm root %s: %w", absRoot, err)
	}

	ancestor := absRoot
	var missing []string
	for {
		if _, err := os.Lstat(ancestor); err == nil {
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("inspect dpm root ancestor %s: %w", ancestor, err)
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return "", fmt.Errorf("find existing ancestor of dpm root %s", absRoot)
		}
		missing = append(missing, filepath.Base(ancestor))
		ancestor = parent
	}
	ancestor, err = filepath.EvalSymlinks(ancestor)
	if err != nil {
		return "", fmt.Errorf("canonicalize dpm root ancestor: %w", err)
	}
	for i := len(missing) - 1; i >= 0; i-- {
		ancestor = filepath.Join(ancestor, missing[i])
	}

	return ancestor, nil
}

func validateOverrideRoot(root string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("find home directory: %w", err)
	}
	canonicalHome, err := canonicalExistingPath(home)
	if err != nil {
		return fmt.Errorf("canonicalize home directory: %w", err)
	}
	canonicalTemp, err := canonicalExistingPath(os.TempDir())
	if err != nil {
		return fmt.Errorf("canonicalize temporary directory: %w", err)
	}
	if strictlyInside(canonicalHome, root) || strictlyInside(canonicalTemp, root) {
		return nil
	}

	return fmt.Errorf("DPM_ROOT %s must be beneath the canonical home %s or temporary directory %s", root, canonicalHome, canonicalTemp)
}

func canonicalExistingPath(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	return filepath.EvalSymlinks(filepath.Clean(absPath))
}

func strictlyInside(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func ensureDirectory(path string, parents bool) error {
	info, err := os.Lstat(path)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("managed directory %s is a symlink", path)
		}
		if !info.IsDir() {
			return fmt.Errorf("managed path %s is not a directory", path)
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect managed directory %s: %w", path, err)
	}
	if parents {
		err = os.MkdirAll(path, 0o755)
	} else {
		err = os.Mkdir(path, 0o755)
	}
	if err != nil {
		return fmt.Errorf("create directory %s: %w", path, err)
	}

	return nil
}

// IsInsideRoot reports whether path is inside cfg.Root or equal to cfg.Root.
func (cfg Config) IsInsideRoot(path string) (bool, error) {
	if cfg.Root == "" {
		return false, fmt.Errorf("dpm root is empty")
	}
	if path == "" {
		return false, fmt.Errorf("path is empty")
	}

	root, err := filepath.Abs(cfg.Root)
	if err != nil {
		return false, fmt.Errorf("resolve dpm root %q: %w", cfg.Root, err)
	}
	target, err := filepath.Abs(path)
	if err != nil {
		return false, fmt.Errorf("resolve path %q: %w", path, err)
	}

	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(target))
	if err != nil {
		return false, fmt.Errorf("compare %s to dpm root %s: %w", target, root, err)
	}

	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))), nil
}

// RequireInsideRoot rejects paths that are outside cfg.Root.
func (cfg Config) RequireInsideRoot(path string) error {
	inside, err := cfg.IsInsideRoot(path)
	if err != nil {
		return err
	}
	if !inside {
		return fmt.Errorf("path %s is outside dpm root %s", path, cfg.Root)
	}

	return nil
}
