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
	t.Setenv(EnvRegistryStaticIndex, "")
	t.Setenv(EnvRegistryPublicKeys, "")

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
	t.Setenv(EnvRegistryStaticIndex, "")
	t.Setenv(EnvRegistryPublicKeys, "")

	cfg, err := Default()
	if err != nil {
		t.Fatalf("Default() error = %v", err)
	}

	if cfg.Root != root {
		t.Fatalf("Root = %q, want %q", cfg.Root, root)
	}
	assertLayout(t, cfg, root)
}

func TestDefaultRejectsDPMRootOutsideHomeAndTemp(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(EnvRoot, filepath.Join(string(filepath.Separator), "var", "lib", "dpm-test"))

	if _, err := Default(); err == nil {
		t.Fatal("Default() error = nil, want root policy error")
	}
}

func TestClientMutationRejectsEffectiveRoot(t *testing.T) {
	cfg, err := FromRoot(filepath.Join(t.TempDir(), "dpm-root"))
	if err != nil {
		t.Fatalf("FromRoot() error = %v", err)
	}

	if err := cfg.requireClientMutation(0); err == nil {
		t.Fatal("requireClientMutation(0) error = nil, want error")
	}
	if err := cfg.requireClientMutation(501); err != nil {
		t.Fatalf("requireClientMutation(501) error = %v", err)
	}
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

func TestDefaultRegistryURLUsesAnonymousHTTPS(t *testing.T) {
	if DefaultRegistryURL != "https://github.com/kevherro/dpm-registry.git" {
		t.Fatalf("DefaultRegistryURL = %q, want official HTTPS Git URL", DefaultRegistryURL)
	}
}

func TestDefaultUsesStaticRegistryFlag(t *testing.T) {
	root := filepath.Join(t.TempDir(), "custom-root")
	t.Setenv(EnvRoot, root)
	t.Setenv(EnvRegistryURL, "")
	t.Setenv(EnvRegistryStaticIndex, "1")

	cfg, err := Default()
	if err != nil {
		t.Fatalf("Default() error = %v", err)
	}

	if !cfg.RegistryStaticIndex {
		t.Fatal("RegistryStaticIndex = false, want true")
	}
}

func TestDefaultUsesRegistryPublicKeys(t *testing.T) {
	root := filepath.Join(t.TempDir(), "custom-root")
	keys := "abc,def"
	t.Setenv(EnvRoot, root)
	t.Setenv(EnvRegistryURL, "")
	t.Setenv(EnvRegistryStaticIndex, "")
	t.Setenv(EnvRegistryPublicKeys, keys)

	cfg, err := Default()
	if err != nil {
		t.Fatalf("Default() error = %v", err)
	}

	if cfg.RegistryPublicKeys != keys {
		t.Fatalf("RegistryPublicKeys = %q, want %q", cfg.RegistryPublicKeys, keys)
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

func TestEnsureDirsRejectsManagedDirectorySymlinksBeforeMutation(t *testing.T) {
	for _, name := range []string{"bin", "pkgs", "downloads", "cache", "registry", "state"} {
		t.Run(name, func(t *testing.T) {
			parent := t.TempDir()
			root := filepath.Join(parent, "dpm-root")
			outside := filepath.Join(parent, "outside")
			if err := os.MkdirAll(root, 0o755); err != nil {
				t.Fatalf("MkdirAll(root) error = %v", err)
			}
			if err := os.MkdirAll(outside, 0o755); err != nil {
				t.Fatalf("MkdirAll(outside) error = %v", err)
			}
			sentinel := filepath.Join(outside, "sentinel")
			if err := os.WriteFile(sentinel, []byte("unchanged"), 0o644); err != nil {
				t.Fatalf("WriteFile(sentinel) error = %v", err)
			}
			if err := os.Symlink(outside, filepath.Join(root, name)); err != nil {
				t.Fatalf("Symlink() error = %v", err)
			}
			cfg, err := FromRoot(root)
			if err != nil {
				t.Fatalf("FromRoot() error = %v", err)
			}

			if err := cfg.EnsureDirs(); err == nil {
				t.Fatal("EnsureDirs() error = nil, want symlink error")
			}
			got, err := os.ReadFile(sentinel)
			if err != nil {
				t.Fatalf("ReadFile(sentinel) error = %v", err)
			}
			if string(got) != "unchanged" {
				t.Fatalf("sentinel = %q, want unchanged", got)
			}
			for _, child := range []string{"bin", "pkgs", "downloads", "cache", "registry", "state"} {
				if child == name {
					continue
				}
				if _, err := os.Lstat(filepath.Join(root, child)); !os.IsNotExist(err) {
					t.Fatalf("managed child %s was mutated before rejection", child)
				}
			}
		})
	}
}

func TestFromRootRejectsRootSymlink(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	root := filepath.Join(parent, "dpm-root")
	if err := os.Symlink(target, root); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	if _, err := FromRoot(root); err == nil {
		t.Fatal("FromRoot() error = nil, want root symlink error")
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
		Root:                root,
		BinDir:              filepath.Join(root, "bin"),
		PkgsDir:             filepath.Join(root, "pkgs"),
		DownloadsDir:        filepath.Join(root, "downloads"),
		CacheDir:            filepath.Join(root, "cache"),
		RegistryDir:         filepath.Join(root, "registry"),
		RegistryURL:         DefaultRegistryURL,
		RegistryStaticIndex: false,
		RegistryPublicKeys:  "",
		StateDir:            filepath.Join(root, "state"),
	}

	if cfg != want {
		t.Fatalf("Config = %#v, want %#v", cfg, want)
	}
}
