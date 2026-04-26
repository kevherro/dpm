// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package link

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kevherro/dpm/internal/config"
)

var (
	// ErrConflict means a bin path is occupied by something dpm must not overwrite.
	ErrConflict = errors.New("bin link conflict")
)

// BinLink records one executable symlink managed by dpm.
type BinLink struct {
	Name   string
	Source string
	Link   string
}

// LinkBins links declared package binaries from prefix into cfg.BinDir.
func LinkBins(cfg config.Config, prefix string, bins []string) ([]BinLink, error) {
	prefix, err := cleanPrefix(cfg, prefix)
	if err != nil {
		return nil, err
	}
	if len(bins) == 0 {
		return nil, fmt.Errorf("no bins declared")
	}
	if err := cfg.RequireInsideRoot(cfg.BinDir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.BinDir, 0o755); err != nil {
		return nil, fmt.Errorf("create bin directory %s: %w", cfg.BinDir, err)
	}

	seen := map[string]bool{}
	links := make([]BinLink, 0, len(bins))
	for _, bin := range bins {
		link, err := planLink(cfg, prefix, bin)
		if err != nil {
			return nil, err
		}
		if seen[link.Name] {
			return nil, fmt.Errorf("duplicate bin link name %q", link.Name)
		}
		seen[link.Name] = true
		if err := createLink(link); err != nil {
			return nil, err
		}
		links = append(links, link)
	}

	return links, nil
}

// RemoveBins removes dpm-managed symlinks recorded in links.
func RemoveBins(cfg config.Config, links []BinLink) error {
	for _, link := range links {
		if err := cfg.RequireInsideRoot(link.Link); err != nil {
			return err
		}
		if filepath.Dir(link.Link) != cfg.BinDir {
			return fmt.Errorf("bin link %s is not directly in %s", link.Link, cfg.BinDir)
		}
		if err := removeLink(link); err != nil {
			return err
		}
	}

	return nil
}

func cleanPrefix(cfg config.Config, prefix string) (string, error) {
	if prefix == "" {
		return "", fmt.Errorf("package prefix is empty")
	}
	prefix, err := filepath.Abs(prefix)
	if err != nil {
		return "", fmt.Errorf("resolve package prefix %q: %w", prefix, err)
	}
	prefix = filepath.Clean(prefix)
	if err := cfg.RequireInsideRoot(prefix); err != nil {
		return "", err
	}

	return prefix, nil
}

func planLink(cfg config.Config, prefix, bin string) (BinLink, error) {
	if err := validateBinPath(bin); err != nil {
		return BinLink{}, err
	}

	source := filepath.Join(prefix, bin)
	if err := requireInside(prefix, source); err != nil {
		return BinLink{}, err
	}
	if err := validateSource(source); err != nil {
		return BinLink{}, err
	}

	name := filepath.Base(bin)
	linkPath := filepath.Join(cfg.BinDir, name)
	if err := cfg.RequireInsideRoot(linkPath); err != nil {
		return BinLink{}, err
	}

	return BinLink{
		Name:   name,
		Source: source,
		Link:   linkPath,
	}, nil
}

func validateBinPath(bin string) error {
	if bin == "" {
		return fmt.Errorf("bin path is empty")
	}
	if strings.Contains(bin, `\`) {
		return fmt.Errorf("bin path %q contains backslash path separators", bin)
	}
	if !filepath.IsLocal(bin) {
		return fmt.Errorf("bin path %q must be relative and stay inside the package prefix", bin)
	}
	name := filepath.Base(bin)
	if name == "." || name == string(filepath.Separator) {
		return fmt.Errorf("bin path %q does not name an executable", bin)
	}

	return nil
}

func validateSource(source string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return fmt.Errorf("stat bin source %s: %w", source, err)
	}
	if info.IsDir() {
		return fmt.Errorf("bin source %s is a directory", source)
	}
	if info.Mode().Type() == os.ModeSymlink {
		return fmt.Errorf("bin source %s is a symlink, which is not allowed", source)
	}
	if info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("bin source %s is not executable", source)
	}

	return nil
}

func createLink(link BinLink) error {
	info, err := os.Lstat(link.Link)
	if err == nil {
		if info.Mode().Type() != os.ModeSymlink {
			return fmt.Errorf("%w: %s already exists and is not a symlink", ErrConflict, link.Link)
		}
		target, err := os.Readlink(link.Link)
		if err != nil {
			return fmt.Errorf("read existing bin link %s: %w", link.Link, err)
		}
		if target != link.Source {
			return fmt.Errorf("%w: %s points to %s, not %s", ErrConflict, link.Link, target, link.Source)
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat bin link %s: %w", link.Link, err)
	}
	if err := os.Symlink(link.Source, link.Link); err != nil {
		return fmt.Errorf("create bin link %s -> %s: %w", link.Link, link.Source, err)
	}

	return nil
}

func removeLink(link BinLink) error {
	info, err := os.Lstat(link.Link)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat bin link %s: %w", link.Link, err)
	}
	if info.Mode().Type() != os.ModeSymlink {
		return fmt.Errorf("%w: %s exists and is not a symlink", ErrConflict, link.Link)
	}

	target, err := os.Readlink(link.Link)
	if err != nil {
		return fmt.Errorf("read bin link %s: %w", link.Link, err)
	}
	if target != link.Source {
		return fmt.Errorf("%w: %s points to %s, not %s", ErrConflict, link.Link, target, link.Source)
	}
	if err := os.Remove(link.Link); err != nil {
		return fmt.Errorf("remove bin link %s: %w", link.Link, err)
	}

	return nil
}

func requireInside(root, path string) error {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return fmt.Errorf("compare %s to root %s: %w", path, root, err)
	}
	if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		return nil
	}

	return fmt.Errorf("path %s escapes root %s", path, root)
}
