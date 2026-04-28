// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package registry

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestResolveVersionLoadsManifest(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "hello", "1.0.0")
	reg := newRegistry(t, root)

	got, err := reg.ResolveVersion("hello", "1.0.0")
	if err != nil {
		t.Fatalf("ResolveVersion() error = %v", err)
	}

	if got.Name != "hello" || got.Version != "1.0.0" {
		t.Fatalf("manifest = %s %s, want hello 1.0.0", got.Name, got.Version)
	}
}

func TestResolveChoosesNewestVersion(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "hello", "1.0.0")
	writeManifest(t, root, "hello", "1.10.0")
	writeManifest(t, root, "hello", "1.2.0")
	reg := newRegistry(t, root)

	got, err := reg.Resolve("hello")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if got.Version != "1.10.0" {
		t.Fatalf("Version = %q, want 1.10.0", got.Version)
	}
}

func TestResolveSkipsYankedVersions(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "hello", "1.0.0")
	writeManifestContents(t, root, "hello", "2.0.0", yankedManifest("hello", "2.0.0"))
	reg := newRegistry(t, root)

	got, err := reg.Resolve("hello")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if got.Version != "1.0.0" {
		t.Fatalf("Version = %q, want newest non-yanked 1.0.0", got.Version)
	}
}

func TestResolveRejectsAllYankedVersions(t *testing.T) {
	root := t.TempDir()
	writeManifestContents(t, root, "hello", "1.0.0", yankedManifest("hello", "1.0.0"))
	reg := newRegistry(t, root)

	_, err := reg.Resolve("hello")
	if !errors.Is(err, ErrVersionNotFound) {
		t.Fatalf("Resolve() error = %v, want ErrVersionNotFound", err)
	}
}

func TestVersionsListsSortedVersions(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "hello", "1.0.0")
	writeManifest(t, root, "hello", "1.10.0")
	writeManifest(t, root, "hello", "1.2.0")
	reg := newRegistry(t, root)

	got, err := reg.Versions("hello")
	if err != nil {
		t.Fatalf("Versions() error = %v", err)
	}
	want := []string{"1.0.0", "1.2.0", "1.10.0"}
	if !slices.Equal(got, want) {
		t.Fatalf("Versions() = %#v, want %#v", got, want)
	}
}

func TestVersionsUsesVersionsDirectory(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "hello", "1.0.0")
	reg := newRegistry(t, root)

	got, err := reg.ManifestPath("hello", "1.0.0")
	if err != nil {
		t.Fatalf("ManifestPath() error = %v", err)
	}
	want := filepath.Join(root, "packages", "hello", "versions", "1.0.0", "dpm.toml")
	if got != want {
		t.Fatalf("ManifestPath() = %q, want %q", got, want)
	}

	versions, err := reg.Versions("hello")
	if err != nil {
		t.Fatalf("Versions() error = %v", err)
	}
	if !slices.Equal(versions, []string{"1.0.0"}) {
		t.Fatalf("Versions() = %#v, want 1.0.0", versions)
	}
}

func TestSearchFindsPackages(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "hello", "1.0.0")
	writeManifest(t, root, "help", "1.0.0")
	writeManifest(t, root, "goodbye", "1.0.0")
	reg := newRegistry(t, root)

	got, err := reg.Search("hel")
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	want := []string{"hello", "help"}
	if !slices.Equal(searchNames(got), want) {
		t.Fatalf("Search() = %#v, want names %#v", got, want)
	}
}

func TestSearchFindsPackageMetadata(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "ripgrep", "15.1.0")
	writePackageMetadata(t, root, "ripgrep", `schema = 1
name = "ripgrep"
summary = "Recursively search directories for a regex pattern"
homepage = "https://github.com/BurntSushi/ripgrep"
license = "MIT OR Unlicense"
categories = ["search", "terminal"]
`)
	writeManifest(t, root, "fd", "10.0.0")
	writePackageMetadata(t, root, "fd", `schema = 1
name = "fd"
summary = "Find filesystem entries"
homepage = "https://github.com/sharkdp/fd"
license = "MIT OR Apache-2.0"
categories = ["filesystem"]
`)
	reg := newRegistry(t, root)

	tests := []struct {
		query string
		want  []string
	}{
		{query: "regex", want: []string{"ripgrep"}},
		{query: "BurntSushi", want: []string{"ripgrep"}},
		{query: "terminal", want: []string{"ripgrep"}},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			got, err := reg.Search(tt.query)
			if err != nil {
				t.Fatalf("Search() error = %v", err)
			}
			if !slices.Equal(searchNames(got), tt.want) {
				t.Fatalf("Search() = %#v, want names %#v", got, tt.want)
			}
			if got[0].Summary == "" || got[0].Homepage == "" || len(got[0].Categories) == 0 {
				t.Fatalf("Search() result = %#v, want metadata", got[0])
			}
		})
	}
}

func TestSearchMissingRegistryReturnsEmpty(t *testing.T) {
	reg := newRegistry(t, filepath.Join(t.TempDir(), "missing"))

	got, err := reg.Search("")
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Search() = %#v, want empty", got)
	}
}

func TestResolveRejectsMissingPackage(t *testing.T) {
	reg := newRegistry(t, t.TempDir())

	_, err := reg.Resolve("missing")
	if !errors.Is(err, ErrPackageNotFound) {
		t.Fatalf("Resolve() error = %v, want ErrPackageNotFound", err)
	}
}

func TestResolveRejectsMissingVersion(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "hello", "1.0.0")
	reg := newRegistry(t, root)

	_, err := reg.ResolveVersion("hello", "2.0.0")
	if !errors.Is(err, ErrVersionNotFound) {
		t.Fatalf("ResolveVersion() error = %v, want ErrVersionNotFound", err)
	}
}

func TestResolveRejectsManifestNameMismatch(t *testing.T) {
	root := t.TempDir()
	writeManifestContents(t, root, "hello", "1.0.0", validManifest("goodbye", "1.0.0"))
	reg := newRegistry(t, root)

	_, err := reg.ResolveVersion("hello", "1.0.0")
	if err == nil {
		t.Fatal("ResolveVersion() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "does not match registry package") {
		t.Fatalf("ResolveVersion() error = %q, want package mismatch", err)
	}
}

func TestResolveRejectsManifestVersionMismatch(t *testing.T) {
	root := t.TempDir()
	writeManifestContents(t, root, "hello", "1.0.0", validManifest("hello", "2.0.0"))
	reg := newRegistry(t, root)

	_, err := reg.ResolveVersion("hello", "1.0.0")
	if err == nil {
		t.Fatal("ResolveVersion() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "does not match registry version") {
		t.Fatalf("ResolveVersion() error = %q, want version mismatch", err)
	}
}

func TestRejectsUnsafePackageAndVersionNames(t *testing.T) {
	reg := newRegistry(t, t.TempDir())

	tests := []struct {
		name    string
		pkg     string
		version string
	}{
		{name: "empty package", pkg: "", version: "1.0.0"},
		{name: "package traversal", pkg: "../hello", version: "1.0.0"},
		{name: "package slash", pkg: "org/hello", version: "1.0.0"},
		{name: "empty version", pkg: "hello", version: ""},
		{name: "version traversal", pkg: "hello", version: "../1.0.0"},
		{name: "version slash", pkg: "hello", version: "stable/1.0.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := reg.ManifestPath(tt.pkg, tt.version)
			if err == nil {
				t.Fatal("ManifestPath() error = nil, want error")
			}
		})
	}
}

func newRegistry(t *testing.T, root string) Registry {
	t.Helper()

	reg, err := New(root)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	return reg
}

func writeManifest(t *testing.T, root, name, version string) {
	t.Helper()
	writeManifestContents(t, root, name, version, validManifest(name, version))
}

func writeManifestContents(t *testing.T, root, name, version, contents string) {
	t.Helper()

	dir := filepath.Join(root, "packages", name, "versions", version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dpm.toml"), []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func validManifest(name, version string) string {
	return `schema = 1
name = "` + name + `"
version = "` + version + `"
dependencies = []

[[artifacts]]
os = "darwin"
arch = "arm64"
url = "file://hello.tar.gz"
sha256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

[install]
bins = ["hello"]
`
}

func yankedManifest(name, version string) string {
	return strings.Replace(validManifest(name, version), "dependencies = []\n", "dependencies = []\nyanked = true\nyank_reason = \"bad artifact\"\n", 1)
}

func searchNames(results []SearchResult) []string {
	names := make([]string, 0, len(results))
	for _, result := range results {
		names = append(names, result.Name)
	}

	return names
}
