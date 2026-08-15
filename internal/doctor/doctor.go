// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

// Package doctor performs a read-only audit of a dpm root.
package doctor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kevherro/dpm/internal/config"
	"github.com/kevherro/dpm/internal/operationlock"
	"github.com/kevherro/dpm/internal/registry"
	"github.com/kevherro/dpm/internal/state"
)

// Report contains all findings from an audit.
type Report struct{ Findings []string }

// Audit inspects cfg without changing it.
func Audit(ctx context.Context, cfg config.Config) (r Report) {
	dirs := []string{cfg.Root, cfg.BinDir, cfg.PkgsDir, cfg.DownloadsDir, cfg.CacheDir, cfg.RegistryDir, cfg.StateDir}
	for _, path := range dirs {
		info, err := os.Lstat(path)
		switch {
		case errors.Is(err, os.ErrNotExist):
			r.add("missing %s", path)
		case err != nil:
			r.add("inspect %s: %v", path, err)
		case info.Mode()&os.ModeSymlink != 0:
			r.add("managed directory is a symlink: %s", path)
		case !info.IsDir():
			r.add("not-dir %s", path)
		}
	}
	lock, err := operationlock.AcquireExisting(cfg.Root, operationlock.Shared)
	if err != nil {
		r.add("operation lock unhealthy: %v", err)
	}
	if lock != nil {
		defer func() {
			if err := lock.Close(); err != nil {
				r.add("operation lock release failed: %v", err)
			}
		}()
	}

	records := r.auditState(cfg)
	r.auditPrefixes(cfg, records)
	r.auditLinks(cfg, records)
	r.auditStaging(cfg)

	if registryHasData(cfg.RegistryDir) {
		report, err := registry.Validate(ctx, registry.ValidateOptions{Root: cfg.RegistryDir})
		if err != nil {
			r.add("registry validity unavailable: %v", err)
		} else {
			for _, issue := range report.Issues {
				r.add("registry invalid %s: %s", issue.Path, issue.Message)
			}
		}
	}
	return r
}

func (r *Report) auditState(cfg config.Config) []state.Record {
	dir := filepath.Join(cfg.StateDir, "installed")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		r.add("read installed state %s: %v", dir, err)
		return nil
	}
	var records []state.Record
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			r.add("inspect state entry %s: %v", path, err)
			continue
		}
		if !info.Mode().IsRegular() || filepath.Ext(entry.Name()) != ".json" {
			r.add("unexpected state entry %s", path)
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".json")
		record, err := state.New(cfg).Get(name)
		if err != nil {
			r.add("invalid state record %s: %v", filepath.Join(dir, entry.Name()), err)
			continue
		}
		records = append(records, record)
	}
	names := map[string]bool{}
	for _, record := range records {
		names[record.Name] = true
	}
	for _, record := range records {
		for _, dep := range record.Dependencies {
			if !names[dep] {
				r.add("missing dependency %s required by %s %s", dep, record.Name, record.Version)
			}
		}
	}
	r.auditDependencyCycles(records)
	return records
}

func (r *Report) auditDependencyCycles(records []state.Record) {
	dependencies := make(map[string][]string, len(records))
	for _, record := range records {
		dependencies[record.Name] = record.Dependencies
	}
	visited := make(map[string]bool)
	stack := make(map[string]bool)
	var visit func(string)
	visit = func(name string) {
		if stack[name] {
			r.add("dependency cycle includes %s", name)
			return
		}
		if visited[name] {
			return
		}
		visited[name] = true
		stack[name] = true
		for _, dep := range dependencies[name] {
			if _, installed := dependencies[dep]; installed {
				visit(dep)
			}
		}
		delete(stack, name)
	}
	for name := range dependencies {
		visit(name)
	}
}

func (r *Report) auditPrefixes(cfg config.Config, records []state.Record) {
	owned := map[string]bool{}
	for _, record := range records {
		owned[record.Prefix] = true
		info, err := os.Lstat(record.Prefix)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			r.add("invalid prefix for %s %s: %s", record.Name, record.Version, record.Prefix)
		}
		for _, bin := range record.Bins {
			info, err := os.Lstat(bin.Source)
			if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
				r.add("invalid executable source for %s %s: %s", record.Name, record.Version, bin.Source)
			}
		}
	}
	packages, err := os.ReadDir(cfg.PkgsDir)
	if err != nil {
		r.add("read package directory %s: %v", cfg.PkgsDir, err)
		return
	}
	for _, pkg := range packages {
		pkgPath := filepath.Join(cfg.PkgsDir, pkg.Name())
		info, err := os.Lstat(pkgPath)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			r.add("unexpected package path %s", pkgPath)
			continue
		}
		versions, err := os.ReadDir(pkgPath)
		if err != nil {
			r.add("read package path %s: %v", pkgPath, err)
			continue
		}
		for _, version := range versions {
			prefix := filepath.Join(pkgPath, version.Name())
			if !owned[prefix] {
				r.add("orphan package prefix %s", prefix)
			}
		}
	}
}

func (r *Report) auditLinks(cfg config.Config, records []state.Record) {
	owned := map[string]string{}
	for _, record := range records {
		for _, bin := range record.Bins {
			owned[bin.Link] = bin.Source
			target, err := os.Readlink(bin.Link)
			if err != nil || target != bin.Source {
				r.add("missing or retargeted owned link for %s %s: %s", record.Name, record.Version, bin.Link)
			}
		}
	}
	entries, err := os.ReadDir(cfg.BinDir)
	if err != nil {
		r.add("read bin directory %s: %v", cfg.BinDir, err)
		return
	}
	for _, entry := range entries {
		path := filepath.Join(cfg.BinDir, entry.Name())
		target, err := os.Readlink(path)
		if err == nil && inside(cfg.PkgsDir, target) && owned[path] != target {
			r.add("unowned managed link %s", path)
		}
	}
}

func (r *Report) auditStaging(cfg config.Config) {
	patterns := []string{filepath.Join(cfg.CacheDir, ".install-*"), filepath.Join(cfg.CacheDir, ".remove-*"), filepath.Join(filepath.Dir(cfg.RegistryDir), ".registry-candidate-*"), filepath.Join(filepath.Dir(cfg.RegistryDir), ".registry-previous-*")}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			r.add("inspect staging %s: %v", pattern, err)
		}
		for _, match := range matches {
			r.add("stale staging %s", match)
		}
	}
}
func (r *Report) add(format string, args ...any) {
	r.Findings = append(r.Findings, fmt.Sprintf(format, args...))
}
func inside(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
func registryHasData(root string) bool {
	info, err := os.Lstat(filepath.Join(root, registry.MetadataFile))
	return err == nil && info.Mode().IsRegular()
}
