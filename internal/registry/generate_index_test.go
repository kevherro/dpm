// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package registry

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kevherro/dpm/internal/checksum"
)

func TestGenerateIndexWritesStaticIndex(t *testing.T) {
	root := t.TempDir()
	writeValidationMetadata(t, root)
	writeValidationPackage(t, root, "hello")
	writeValidationManifest(t, root, validManifestSpec(t, root, "hello", "1.0.0"))
	yanked := validManifestSpec(t, root, "hello", "2.0.0")
	yanked.yanked = true
	yanked.yankReason = "bad artifact"
	writeValidationManifest(t, root, yanked)

	result, err := GenerateIndex(context.Background(), GenerateIndexOptions{Root: root})
	if err != nil {
		t.Fatalf("GenerateIndex() error = %v", err)
	}
	if result.IndexDir != filepath.Join(root, StaticIndexDir) {
		t.Fatalf("IndexDir = %q, want generated index dir", result.IndexDir)
	}
	for _, want := range []string{
		filepath.Join(root, StaticIndexDir, "packages.json"),
		filepath.Join(root, StaticIndexDir, "packages", "hello", "versions.json"),
		filepath.Join(root, StaticIndexDir, "packages", "hello", "versions", "1.0.0", "dpm.json"),
		filepath.Join(root, StaticIndexDir, "packages", "hello", "versions", "2.0.0", "dpm.json"),
	} {
		assertGeneratedFile(t, result.Files, want)
		assertFileExists(t, want+".sha256")
	}

	packagesData, err := os.ReadFile(filepath.Join(root, StaticIndexDir, "packages.json"))
	if err != nil {
		t.Fatalf("ReadFile(packages.json) error = %v", err)
	}
	if !strings.Contains(string(packagesData), `"versions_sha256"`) {
		t.Fatalf("packages.json = %s, want versions checksum", packagesData)
	}
	versionsData, err := os.ReadFile(filepath.Join(root, StaticIndexDir, "packages", "hello", "versions.json"))
	if err != nil {
		t.Fatalf("ReadFile(versions.json) error = %v", err)
	}
	if !strings.Contains(string(versionsData), `"manifest_sha256"`) {
		t.Fatalf("versions.json = %s, want manifest checksum", versionsData)
	}

	reg := newStaticRegistry(t, root)
	matches, err := reg.Search("regex")
	if err != nil {
		t.Fatalf("static Search() error = %v", err)
	}
	if !slices.Equal(searchNames(matches), []string{"hello"}) {
		t.Fatalf("static Search() = %#v, want hello", matches)
	}
	versions, err := reg.Versions("hello")
	if err != nil {
		t.Fatalf("static Versions() error = %v", err)
	}
	if !slices.Equal(versions, []string{"1.0.0", "2.0.0"}) {
		t.Fatalf("static Versions() = %#v, want sorted versions", versions)
	}
	resolved, err := reg.Resolve("hello")
	if err != nil {
		t.Fatalf("static Resolve() error = %v", err)
	}
	if resolved.Version != "1.0.0" {
		t.Fatalf("static Resolve() version = %q, want newest non-yanked 1.0.0", resolved.Version)
	}
	yankedManifest, err := reg.ResolveVersion("hello", "2.0.0")
	if err != nil {
		t.Fatalf("static ResolveVersion(yanked) error = %v", err)
	}
	if !yankedManifest.Yanked || yankedManifest.YankReason != "bad artifact" {
		t.Fatalf("static yanked manifest = %#v, want yanked reason", yankedManifest)
	}
}

func TestStaticRegistryRejectsChecksumMismatch(t *testing.T) {
	root := t.TempDir()
	writeValidationMetadata(t, root)
	writeValidationPackage(t, root, "hello")
	writeValidationManifest(t, root, validManifestSpec(t, root, "hello", "1.0.0"))
	if _, err := GenerateIndex(context.Background(), GenerateIndexOptions{Root: root}); err != nil {
		t.Fatalf("GenerateIndex() error = %v", err)
	}
	manifestPath := filepath.Join(root, StaticIndexDir, "packages", "hello", "versions", "1.0.0", "dpm.json")
	if err := os.WriteFile(manifestPath, []byte(`{"schema":1}`+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := newStaticRegistry(t, root).ResolveVersion("hello", "1.0.0")
	var mismatch checksum.MismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("ResolveVersion() error = %v, want checksum mismatch", err)
	}
}

func TestGenerateIndexRejectsInvalidSourceRegistry(t *testing.T) {
	root := t.TempDir()
	writeValidationMetadata(t, root)
	writeValidationManifest(t, root, validManifestSpec(t, root, "hello", "1.0.0"))

	_, err := GenerateIndex(context.Background(), GenerateIndexOptions{Root: root})
	if err == nil {
		t.Fatal("GenerateIndex() error = nil, want invalid registry")
	}
	if !strings.Contains(err.Error(), "registry is invalid") {
		t.Fatalf("GenerateIndex() error = %q, want validation summary", err)
	}
}

func newStaticRegistry(t *testing.T, root string) Registry {
	t.Helper()
	reg, err := NewWithOptions(Options{Root: root, StaticIndex: true})
	if err != nil {
		t.Fatalf("NewWithOptions() error = %v", err)
	}

	return reg
}

func assertGeneratedFile(t *testing.T, files []GeneratedIndexFile, want string) {
	t.Helper()
	for _, file := range files {
		if file.Path == want && file.SHA256 != "" {
			return
		}
	}
	t.Fatalf("generated files = %#v, want %s", files, want)
}
