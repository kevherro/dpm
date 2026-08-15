// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kevherro/dpm/internal/config"
	"github.com/kevherro/dpm/internal/state"
)

func detectInterruptedOperations(cfg config.Config, store state.Store) error {
	for _, pattern := range []string{".install-*", ".remove-*"} {
		matches, err := filepath.Glob(filepath.Join(cfg.CacheDir, pattern))
		if err != nil {
			return fmt.Errorf("inspect operation staging: %w", err)
		}
		if len(matches) > 0 {
			return fmt.Errorf("interrupted operation evidence at %s; inspect it and run `dpm doctor`", matches[0])
		}
	}

	records, err := store.List()
	if err != nil {
		return err
	}
	prefixes := make(map[string]bool, len(records))
	links := make(map[string]string)
	for _, record := range records {
		prefixes[record.Prefix] = true
		info, err := os.Lstat(record.Prefix)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("installed state for %s has no valid prefix %s; run `dpm doctor`", record.Name, record.Prefix)
		}
		for _, bin := range record.Bins {
			links[bin.Link] = bin.Source
		}
	}

	packages, err := os.ReadDir(cfg.PkgsDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect package prefixes: %w", err)
	}
	for _, pkg := range packages {
		pkgPath := filepath.Join(cfg.PkgsDir, pkg.Name())
		info, err := os.Lstat(pkgPath)
		if err != nil {
			return fmt.Errorf("inspect package path %s: %w", pkgPath, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("unexpected package path %s; run `dpm doctor`", pkgPath)
		}
		versions, err := os.ReadDir(pkgPath)
		if err != nil {
			return fmt.Errorf("inspect package versions %s: %w", pkgPath, err)
		}
		for _, version := range versions {
			prefix := filepath.Join(pkgPath, version.Name())
			if !prefixes[prefix] {
				return fmt.Errorf("package prefix %s has no installed state; run `dpm doctor`", prefix)
			}
		}
	}

	entries, err := os.ReadDir(cfg.BinDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect bin directory: %w", err)
	}
	for _, entry := range entries {
		path := filepath.Join(cfg.BinDir, entry.Name())
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			continue
		}
		target, err := os.Readlink(path)
		if err != nil {
			return fmt.Errorf("read bin link %s: %w", path, err)
		}
		if isInside(cfg.PkgsDir, target) && links[path] != target {
			return fmt.Errorf("bin link %s points into managed packages without state ownership; run `dpm doctor`", path)
		}
	}

	return nil
}

func isInside(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
