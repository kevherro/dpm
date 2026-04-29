// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package config

import (
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
	// DefaultRegistryURL is the placeholder Git registry used by dpm update.
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
	StateDir            string
}

// Default returns configuration derived from the environment.
func Default() (Config, error) {
	if root := os.Getenv(EnvRoot); root != "" {
		return withRegistryEnv(FromRoot(root))
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, fmt.Errorf("find home directory: %w", err)
	}

	return withRegistryEnv(FromRoot(filepath.Join(home, ".dpm")))
}

// FromRoot returns configuration rooted at root.
func FromRoot(root string) (Config, error) {
	if root == "" {
		return Config{}, fmt.Errorf("dpm root is empty")
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Config{}, fmt.Errorf("resolve dpm root %q: %w", root, err)
	}
	absRoot = filepath.Clean(absRoot)

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
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create directory %s: %w", dir, err)
		}
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
