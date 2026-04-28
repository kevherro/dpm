// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultUsesHomeDPMRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(EnvRoot, "")
	t.Setenv(EnvRegistryURL, "")

	cfg, err := Default()
	if err != nil {
		t.Fatalf("Default() error = %v", err)
	}

	wantRoot := filepath.Join(home, ".dpm")
	if cfg.Root != wantRoot {
		t.Fatalf("Root = %q, want %q", cfg.Root, wantRoot)
	}
	assertLayout(t, cfg, wantRoot)
}

func TestDefaultUsesDPMRootOverride(t *testing.T) {
	root := filepath.Join(t.TempDir(), "custom-root")
	t.Setenv(EnvRoot, root)
	t.Setenv(EnvRegistryURL, "")

	cfg, err := Default()
	if err != nil {
		t.Fatalf("Default() error = %v", err)
	}

	if cfg.Root != root {
		t.Fatalf("Root = %q, want %q", cfg.Root, root)
	}
	assertLayout(t, cfg, root)
}

func TestDefaultUsesRegistryURLOverride(t *testing.T) {
	root := filepath.Join(t.TempDir(), "custom-root")
	url := "file:///tmp/dpm-registry"
	t.Setenv(EnvRoot, root)
	t.Setenv(EnvRegistryURL, url)

	cfg, err := Default()
	if err != nil {
		t.Fatalf("Default() error = %v", err)
	}

	if cfg.RegistryURL != url {
		t.Fatalf("RegistryURL = %q, want %q", cfg.RegistryURL, url)
	}
}

func TestFromRootRejectsEmptyRoot(t *testing.T) {
	if _, err := FromRoot(""); err == nil {
		t.Fatal("FromRoot(\"\") error = nil, want error")
	}
}

func TestEnsureDirsCreatesStandardLayout(t *testing.T) {
	root := filepath.Join(t.TempDir(), "dpm-root")
	cfg, err := FromRoot(root)
	if err != nil {
		t.Fatalf("FromRoot() error = %v", err)
	}

	if err := cfg.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs() error = %v", err)
	}

	for _, dir := range []string{
		cfg.Root,
		cfg.BinDir,
		cfg.PkgsDir,
		cfg.DownloadsDir,
		cfg.CacheDir,
		cfg.RegistryDir,
		cfg.StateDir,
	} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("Stat(%q) error = %v", dir, err)
		}
		if !info.IsDir() {
			t.Fatalf("%q is not a directory", dir)
		}
	}
}

func TestRootSafety(t *testing.T) {
	root := filepath.Join(t.TempDir(), "dpm-root")
	cfg, err := FromRoot(root)
	if err != nil {
		t.Fatalf("FromRoot() error = %v", err)
	}

	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "root", path: root, want: true},
		{name: "child", path: filepath.Join(root, "bin", "hello"), want: true},
		{name: "clean child", path: filepath.Join(root, "bin", "..", "pkgs"), want: true},
		{name: "parent", path: filepath.Dir(root), want: false},
		{name: "sibling", path: root + "-sibling", want: false},
		{name: "traversal", path: filepath.Join(root, "..", "outside"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := cfg.IsInsideRoot(tt.path)
			if err != nil {
				t.Fatalf("IsInsideRoot() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("IsInsideRoot(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestRequireInsideRootRejectsOutsidePath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "dpm-root")
	cfg, err := FromRoot(root)
	if err != nil {
		t.Fatalf("FromRoot() error = %v", err)
	}

	if err := cfg.RequireInsideRoot(filepath.Join(root, "bin")); err != nil {
		t.Fatalf("RequireInsideRoot() error = %v", err)
	}
	if err := cfg.RequireInsideRoot(filepath.Dir(root)); err == nil {
		t.Fatal("RequireInsideRoot(outside) error = nil, want error")
	}
}

func assertLayout(t *testing.T, cfg Config, root string) {
	t.Helper()

	want := Config{
		Root:         root,
		BinDir:       filepath.Join(root, "bin"),
		PkgsDir:      filepath.Join(root, "pkgs"),
		DownloadsDir: filepath.Join(root, "downloads"),
		CacheDir:     filepath.Join(root, "cache"),
		RegistryDir:  filepath.Join(root, "registry"),
		RegistryURL:  DefaultRegistryURL,
		StateDir:     filepath.Join(root, "state"),
	}

	if cfg != want {
		t.Fatalf("Config = %#v, want %#v", cfg, want)
	}
}
