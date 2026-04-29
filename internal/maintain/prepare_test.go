// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package maintain

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kevherro/dpm/internal/checksum"
	"github.com/kevherro/dpm/internal/manifest"
	"github.com/kevherro/dpm/internal/registry"
)

func TestPrepareCreatesPackageManifestAndVerifiesInstall(t *testing.T) {
	root := newPrepareRegistry(t)
	artifact := filepath.Join(t.TempDir(), "tool-1.2.3-darwin-arm64.tar.gz")
	writePrepareArtifact(t, artifact, []prepareArchiveEntry{
		{Name: "tool-1.2.3/bin/tool", Mode: 0o755, Body: "#!/bin/sh\nprintf 'tool\\n'\n"},
		{Name: "tool-1.2.3/README.md", Mode: 0o644, Body: "tool\n"},
	})
	sum, err := checksum.FileSHA256(artifact)
	if err != nil {
		t.Fatalf("FileSHA256() error = %v", err)
	}

	got, err := Prepare(context.Background(), PrepareOptions{
		RegistryRoot: root,
		Name:         "tool",
		Version:      "1.2.3",
		ArtifactURL:  fileURL(artifact),
		Summary:      "A tiny tool",
		Homepage:     "https://example.com/tool",
		License:      "MIT",
		Categories:   []string{"demo", "cli"},
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	if got.SHA256 != sum {
		t.Fatalf("SHA256 = %q, want %q", got.SHA256, sum)
	}
	if got.Size <= 0 {
		t.Fatalf("Size = %d, want positive", got.Size)
	}
	wantBins := []string{"tool-1.2.3/bin/tool"}
	if !slices.Equal(got.SuggestedBins, wantBins) || !slices.Equal(got.Bins, wantBins) {
		t.Fatalf("bins = %#v suggested %#v, want %#v", got.Bins, got.SuggestedBins, wantBins)
	}
	if !got.VerifiedInstall {
		t.Fatal("VerifiedInstall = false, want true")
	}
	if len(got.CreatedFiles) != 2 {
		t.Fatalf("CreatedFiles = %#v, want package and manifest", got.CreatedFiles)
	}

	pkg, err := registry.LoadPackage(filepath.Join(root, "packages", "tool", registry.PackageFile))
	if err != nil {
		t.Fatalf("LoadPackage() error = %v", err)
	}
	if pkg.Name != "tool" || pkg.Summary != "A tiny tool" || !slices.Equal(pkg.Categories, []string{"demo", "cli"}) {
		t.Fatalf("package metadata = %#v, want generated metadata", pkg)
	}
	m, err := manifest.Load(filepath.Join(root, "packages", "tool", "versions", "1.2.3", "dpm.toml"))
	if err != nil {
		t.Fatalf("manifest Load() error = %v", err)
	}
	if m.Name != "tool" || m.Version != "1.2.3" || m.Artifacts[0].SHA256 != sum || !slices.Equal(m.Install.Bins, wantBins) {
		t.Fatalf("manifest = %#v, want generated manifest", m)
	}
	for _, want := range []string{
		"diff --git a/packages/tool/package.toml b/packages/tool/package.toml\n",
		"diff --git a/packages/tool/versions/1.2.3/dpm.toml b/packages/tool/versions/1.2.3/dpm.toml\n",
		`+sha256 = "` + sum + `"`,
		`+bins = ["tool-1.2.3/bin/tool"]`,
	} {
		if !strings.Contains(got.Diff, want) {
			t.Fatalf("Diff = %q, want substring %q", got.Diff, want)
		}
	}
}

func TestPrepareAddsVersionForExistingPackageMetadata(t *testing.T) {
	root := newPrepareRegistry(t)
	writePreparePackageMetadata(t, root, "tool")
	before, err := os.ReadFile(filepath.Join(root, "packages", "tool", registry.PackageFile))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	artifact := filepath.Join(t.TempDir(), "tool-1.2.4-darwin-arm64.tar.gz")
	writePrepareArtifact(t, artifact, []prepareArchiveEntry{
		{Name: "bin/tool", Mode: 0o755, Body: "#!/bin/sh\nprintf 'tool\\n'\n"},
	})

	got, err := Prepare(context.Background(), PrepareOptions{
		RegistryRoot: root,
		Name:         "tool",
		Version:      "1.2.4",
		ArtifactURL:  fileURL(artifact),
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	after, err := os.ReadFile(filepath.Join(root, "packages", "tool", registry.PackageFile))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("package metadata changed:\n%s", after)
	}
	if len(got.CreatedFiles) != 1 {
		t.Fatalf("CreatedFiles = %#v, want only manifest", got.CreatedFiles)
	}
	if strings.Contains(got.Diff, "package.toml") {
		t.Fatalf("Diff = %q, did not expect package metadata diff", got.Diff)
	}
	if _, err := manifest.Load(filepath.Join(root, "packages", "tool", "versions", "1.2.4", "dpm.toml")); err != nil {
		t.Fatalf("manifest Load() error = %v", err)
	}
}

func TestPrepareRequiresMetadataForNewPackage(t *testing.T) {
	root := newPrepareRegistry(t)
	artifact := filepath.Join(t.TempDir(), "tool-1.2.3-darwin-arm64.tar.gz")
	writePrepareArtifact(t, artifact, []prepareArchiveEntry{
		{Name: "bin/tool", Mode: 0o755, Body: "#!/bin/sh\n"},
	})

	_, err := Prepare(context.Background(), PrepareOptions{
		RegistryRoot: root,
		Name:         "tool",
		Version:      "1.2.3",
		ArtifactURL:  fileURL(artifact),
	})
	if err == nil {
		t.Fatal("Prepare() error = nil, want missing metadata error")
	}
	if !strings.Contains(err.Error(), "summary is required") {
		t.Fatalf("Prepare() error = %q, want summary requirement", err)
	}
}

type prepareArchiveEntry struct {
	Name string
	Mode int64
	Body string
}

func newPrepareRegistry(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "registry")
	writePrepareFile(t, filepath.Join(root, registry.MetadataFile), `schema = 1
name = "dpm-core"
description = "Test registry"
`)

	return root
}

func writePreparePackageMetadata(t *testing.T, root, name string) {
	t.Helper()
	writePrepareFile(t, filepath.Join(root, "packages", name, registry.PackageFile), `schema = 1
name = "`+name+`"
summary = "Existing tool"
homepage = "https://example.com/tool"
license = "MIT"
categories = ["demo"]
`)
}

func writePrepareArtifact(t *testing.T, path string, entries []prepareArchiveEntry) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, entry := range entries {
		header := &tar.Header{
			Name:     entry.Name,
			Typeflag: tar.TypeReg,
			Mode:     entry.Mode,
			Size:     int64(len(entry.Body)),
		}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatalf("WriteHeader() error = %v", err)
		}
		if _, err := io.Copy(tw, strings.NewReader(entry.Body)); err != nil {
			t.Fatalf("Copy() error = %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar Close() error = %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip Close() error = %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("file Close() error = %v", err)
	}
}

func writePrepareFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func fileURL(path string) string {
	return "file://" + filepath.ToSlash(path)
}
