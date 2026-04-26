// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package link

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/kevherro/dpm/internal/config"
)

func TestLinkBinsCreatesSymlinks(t *testing.T) {
	cfg := testConfig(t)
	prefix := filepath.Join(cfg.PkgsDir, "hello", "1.0.0")
	makeExecutable(t, filepath.Join(prefix, "bin", "hello"))

	got, err := LinkBins(cfg, prefix, []string{"bin/hello"})
	if err != nil {
		t.Fatalf("LinkBins() error = %v", err)
	}

	want := BinLink{
		Name:   "hello",
		Source: filepath.Join(prefix, "bin", "hello"),
		Link:   filepath.Join(cfg.BinDir, "hello"),
	}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("LinkBins() = %#v, want %#v", got, []BinLink{want})
	}
	assertSymlink(t, want.Link, want.Source)
}

func TestLinkBinsIsIdempotentForExistingOwnedLink(t *testing.T) {
	cfg := testConfig(t)
	prefix := filepath.Join(cfg.PkgsDir, "hello", "1.0.0")
	source := filepath.Join(prefix, "hello")
	makeExecutable(t, source)

	if _, err := LinkBins(cfg, prefix, []string{"hello"}); err != nil {
		t.Fatalf("first LinkBins() error = %v", err)
	}
	if _, err := LinkBins(cfg, prefix, []string{"hello"}); err != nil {
		t.Fatalf("second LinkBins() error = %v", err)
	}
	assertSymlink(t, filepath.Join(cfg.BinDir, "hello"), source)
}

func TestLinkBinsRefusesFileConflict(t *testing.T) {
	cfg := testConfig(t)
	prefix := filepath.Join(cfg.PkgsDir, "hello", "1.0.0")
	makeExecutable(t, filepath.Join(prefix, "hello"))
	if err := os.MkdirAll(cfg.BinDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	conflict := filepath.Join(cfg.BinDir, "hello")
	if err := os.WriteFile(conflict, []byte("user file"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := LinkBins(cfg, prefix, []string{"hello"})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("LinkBins() error = %v, want ErrConflict", err)
	}
	assertFile(t, conflict, "user file")
}

func TestLinkBinsRefusesDifferentSymlinkConflict(t *testing.T) {
	cfg := testConfig(t)
	prefix := filepath.Join(cfg.PkgsDir, "hello", "1.0.0")
	makeExecutable(t, filepath.Join(prefix, "hello"))
	other := filepath.Join(cfg.PkgsDir, "other", "1.0.0", "hello")
	makeExecutable(t, other)
	if err := os.MkdirAll(cfg.BinDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	linkPath := filepath.Join(cfg.BinDir, "hello")
	if err := os.Symlink(other, linkPath); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	_, err := LinkBins(cfg, prefix, []string{"hello"})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("LinkBins() error = %v, want ErrConflict", err)
	}
	assertSymlink(t, linkPath, other)
}

func TestLinkBinsDoesNotLeavePartialLinksOnConflict(t *testing.T) {
	cfg := testConfig(t)
	prefix := filepath.Join(cfg.PkgsDir, "hello", "1.0.0")
	makeExecutable(t, filepath.Join(prefix, "hello"))
	makeExecutable(t, filepath.Join(prefix, "goodbye"))
	other := filepath.Join(cfg.PkgsDir, "other", "1.0.0", "goodbye")
	makeExecutable(t, other)
	if err := os.MkdirAll(cfg.BinDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	conflict := filepath.Join(cfg.BinDir, "goodbye")
	if err := os.Symlink(other, conflict); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	_, err := LinkBins(cfg, prefix, []string{"hello", "goodbye"})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("LinkBins() error = %v, want ErrConflict", err)
	}
	if _, err := os.Lstat(filepath.Join(cfg.BinDir, "hello")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Lstat(hello) error = %v, want not exist", err)
	}
	assertSymlink(t, conflict, other)
}

func TestLinkBinsRejectsUnsafeBins(t *testing.T) {
	cfg := testConfig(t)
	prefix := filepath.Join(cfg.PkgsDir, "hello", "1.0.0")
	makeExecutable(t, filepath.Join(prefix, "hello"))

	tests := []string{
		"",
		"../hello",
		"/absolute/hello",
		"bin/../../hello",
		`bin\hello`,
	}

	for _, bin := range tests {
		t.Run(bin, func(t *testing.T) {
			if _, err := LinkBins(cfg, prefix, []string{bin}); err == nil {
				t.Fatal("LinkBins() error = nil, want error")
			}
		})
	}
}

func TestLinkBinsRejectsSourceOutsideRoot(t *testing.T) {
	cfg := testConfig(t)
	prefix := filepath.Join(t.TempDir(), "outside")
	makeExecutable(t, filepath.Join(prefix, "hello"))

	if _, err := LinkBins(cfg, prefix, []string{"hello"}); err == nil {
		t.Fatal("LinkBins() error = nil, want error")
	}
}

func TestLinkBinsRejectsInvalidSource(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, source string)
	}{
		{
			name: "missing",
			setup: func(t *testing.T, source string) {
				t.Helper()
			},
		},
		{
			name: "directory",
			setup: func(t *testing.T, source string) {
				t.Helper()
				if err := os.MkdirAll(source, 0o755); err != nil {
					t.Fatalf("MkdirAll() error = %v", err)
				}
			},
		},
		{
			name: "symlink",
			setup: func(t *testing.T, source string) {
				t.Helper()
				target := filepath.Join(filepath.Dir(source), "target")
				makeExecutable(t, target)
				if err := os.Symlink(target, source); err != nil {
					t.Fatalf("Symlink() error = %v", err)
				}
			},
		},
		{
			name: "not executable",
			setup: func(t *testing.T, source string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
					t.Fatalf("MkdirAll() error = %v", err)
				}
				if err := os.WriteFile(source, []byte("hello"), 0o644); err != nil {
					t.Fatalf("WriteFile() error = %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testConfig(t)
			prefix := filepath.Join(cfg.PkgsDir, "hello", "1.0.0")
			source := filepath.Join(prefix, "hello")
			tt.setup(t, source)
			if _, err := LinkBins(cfg, prefix, []string{"hello"}); err == nil {
				t.Fatal("LinkBins() error = nil, want error")
			}
		})
	}
}

func TestLinkBinsRejectsDuplicateLinkNames(t *testing.T) {
	cfg := testConfig(t)
	prefix := filepath.Join(cfg.PkgsDir, "hello", "1.0.0")
	makeExecutable(t, filepath.Join(prefix, "bin", "hello"))
	makeExecutable(t, filepath.Join(prefix, "sbin", "hello"))

	if _, err := LinkBins(cfg, prefix, []string{"bin/hello", "sbin/hello"}); err == nil {
		t.Fatal("LinkBins() error = nil, want error")
	}
}

func TestRemoveBinsRemovesOwnedLinks(t *testing.T) {
	cfg := testConfig(t)
	prefix := filepath.Join(cfg.PkgsDir, "hello", "1.0.0")
	source := filepath.Join(prefix, "hello")
	makeExecutable(t, source)
	links, err := LinkBins(cfg, prefix, []string{"hello"})
	if err != nil {
		t.Fatalf("LinkBins() error = %v", err)
	}

	if err := RemoveBins(cfg, links); err != nil {
		t.Fatalf("RemoveBins() error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(cfg.BinDir, "hello")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Lstat() error = %v, want not exist", err)
	}
}

func TestRemoveBinsIsIdempotentForMissingLinks(t *testing.T) {
	cfg := testConfig(t)
	link := BinLink{
		Name:   "hello",
		Source: filepath.Join(cfg.PkgsDir, "hello", "1.0.0", "hello"),
		Link:   filepath.Join(cfg.BinDir, "hello"),
	}

	if err := RemoveBins(cfg, []BinLink{link}); err != nil {
		t.Fatalf("RemoveBins() error = %v", err)
	}
}

func TestRemoveBinsRefusesNonSymlink(t *testing.T) {
	cfg := testConfig(t)
	if err := os.MkdirAll(cfg.BinDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	linkPath := filepath.Join(cfg.BinDir, "hello")
	if err := os.WriteFile(linkPath, []byte("user file"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	err := RemoveBins(cfg, []BinLink{{
		Name:   "hello",
		Source: filepath.Join(cfg.PkgsDir, "hello", "1.0.0", "hello"),
		Link:   linkPath,
	}})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("RemoveBins() error = %v, want ErrConflict", err)
	}
	assertFile(t, linkPath, "user file")
}

func TestRemoveBinsRefusesDifferentSymlinkTarget(t *testing.T) {
	cfg := testConfig(t)
	source := filepath.Join(cfg.PkgsDir, "hello", "1.0.0", "hello")
	other := filepath.Join(cfg.PkgsDir, "other", "1.0.0", "hello")
	makeExecutable(t, source)
	makeExecutable(t, other)
	if err := os.MkdirAll(cfg.BinDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	linkPath := filepath.Join(cfg.BinDir, "hello")
	if err := os.Symlink(other, linkPath); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	err := RemoveBins(cfg, []BinLink{{
		Name:   "hello",
		Source: source,
		Link:   linkPath,
	}})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("RemoveBins() error = %v, want ErrConflict", err)
	}
	assertSymlink(t, linkPath, other)
}

func testConfig(t *testing.T) config.Config {
	t.Helper()

	cfg, err := config.FromRoot(filepath.Join(t.TempDir(), "dpm-root"))
	if err != nil {
		t.Fatalf("FromRoot() error = %v", err)
	}

	return cfg
}

func makeExecutable(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func assertSymlink(t *testing.T, path, wantTarget string) {
	t.Helper()

	got, err := os.Readlink(path)
	if err != nil {
		t.Fatalf("Readlink(%q) error = %v", path, err)
	}
	if got != wantTarget {
		t.Fatalf("Readlink(%q) = %q, want %q", path, got, wantTarget)
	}
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	if string(got) != want {
		t.Fatalf("ReadFile(%q) = %q, want %q", path, got, want)
	}
}
