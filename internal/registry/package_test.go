// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package registry

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestParsePackage(t *testing.T) {
	got, err := ParsePackage(strings.NewReader(`
# package metadata
schema = 1
name = "ripgrep"
summary = "Recursively search directories for a regex pattern"
homepage = "https://github.com/BurntSushi/ripgrep"
license = "MIT OR Unlicense"
categories = ["search", "cli"]
`))
	if err != nil {
		t.Fatalf("ParsePackage() error = %v", err)
	}

	want := Package{
		Schema:     CurrentSchema,
		Name:       "ripgrep",
		Summary:    "Recursively search directories for a regex pattern",
		Homepage:   "https://github.com/BurntSushi/ripgrep",
		License:    "MIT OR Unlicense",
		Categories: []string{"search", "cli"},
	}
	assertPackageEqual(t, got, want)
}

func TestParsePackageAllowsOptionalCategories(t *testing.T) {
	got, err := ParsePackage(strings.NewReader(`
schema = 1
name = "ripgrep"
summary = "Recursively search directories for a regex pattern"
homepage = "https://github.com/BurntSushi/ripgrep"
license = "MIT OR Unlicense"
`))
	if err != nil {
		t.Fatalf("ParsePackage() error = %v", err)
	}

	if len(got.Categories) != 0 {
		t.Fatalf("Categories = %#v, want empty", got.Categories)
	}
}

func TestRegistryPackageLoadsPackageFile(t *testing.T) {
	root := t.TempDir()
	writePackageMetadata(t, root, "ripgrep", validPackageMetadata("ripgrep"))
	reg := newRegistry(t, root)

	got, err := reg.Package("ripgrep")
	if err != nil {
		t.Fatalf("Package() error = %v", err)
	}

	if got.Name != "ripgrep" {
		t.Fatalf("Name = %q, want ripgrep", got.Name)
	}
}

func TestRegistryPackageRejectsNameMismatch(t *testing.T) {
	root := t.TempDir()
	writePackageMetadata(t, root, "ripgrep", validPackageMetadata("fd"))
	reg := newRegistry(t, root)

	_, err := reg.Package("ripgrep")
	if err == nil {
		t.Fatal("Package() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "does not match package directory") {
		t.Fatalf("Package() error = %q, want directory mismatch", err)
	}
}

func TestParsePackageRejectsInvalidPackage(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{
			name: "missing schema",
			input: `
name = "ripgrep"
summary = "Search"
homepage = "https://example.com"
license = "MIT"
`,
			wantErr: "schema is required",
		},
		{
			name: "unsupported schema",
			input: `
schema = 2
name = "ripgrep"
summary = "Search"
homepage = "https://example.com"
license = "MIT"
`,
			wantErr: "not supported",
		},
		{
			name: "missing name",
			input: `
schema = 1
summary = "Search"
homepage = "https://example.com"
license = "MIT"
`,
			wantErr: "package is empty",
		},
		{
			name: "missing summary",
			input: `
schema = 1
name = "ripgrep"
homepage = "https://example.com"
license = "MIT"
`,
			wantErr: "summary is required",
		},
		{
			name: "missing homepage",
			input: `
schema = 1
name = "ripgrep"
summary = "Search"
license = "MIT"
`,
			wantErr: "homepage is required",
		},
		{
			name: "missing license",
			input: `
schema = 1
name = "ripgrep"
summary = "Search"
homepage = "https://example.com"
`,
			wantErr: "license is required",
		},
		{
			name: "unknown key",
			input: `
schema = 1
name = "ripgrep"
summary = "Search"
homepage = "https://example.com"
license = "MIT"
script = "echo no"
`,
			wantErr: "unknown package metadata key",
		},
		{
			name: "duplicate key",
			input: `
schema = 1
name = "ripgrep"
name = "rg"
summary = "Search"
homepage = "https://example.com"
license = "MIT"
`,
			wantErr: "duplicate key",
		},
		{
			name: "section",
			input: `
schema = 1
name = "ripgrep"
summary = "Search"
homepage = "https://example.com"
license = "MIT"
[metadata]
`,
			wantErr: "sections are not allowed",
		},
		{
			name: "bad categories",
			input: `
schema = 1
name = "ripgrep"
summary = "Search"
homepage = "https://example.com"
license = "MIT"
categories = "search"
`,
			wantErr: "expected string array",
		},
		{
			name: "path category",
			input: `
schema = 1
name = "ripgrep"
summary = "Search"
homepage = "https://example.com"
license = "MIT"
categories = ["cli/search"]
`,
			wantErr: "must not contain path separators",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParsePackage(strings.NewReader(tt.input))
			if err == nil {
				t.Fatal("ParsePackage() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ParsePackage() error = %q, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func writePackageMetadata(t *testing.T, root, name, contents string) {
	t.Helper()

	dir := filepath.Join(root, "packages", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, PackageFile), []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func validPackageMetadata(name string) string {
	return `schema = 1
name = "` + name + `"
summary = "Recursively search directories for a regex pattern"
homepage = "https://github.com/BurntSushi/ripgrep"
license = "MIT OR Unlicense"
categories = ["search", "cli"]
`
}

func assertPackageEqual(t *testing.T, got, want Package) {
	t.Helper()

	if got.Schema != want.Schema ||
		got.Name != want.Name ||
		got.Summary != want.Summary ||
		got.Homepage != want.Homepage ||
		got.License != want.License ||
		!slices.Equal(got.Categories, want.Categories) {
		t.Fatalf("Package = %#v, want %#v", got, want)
	}
}
