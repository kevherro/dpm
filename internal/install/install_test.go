// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package install

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kevherro/dpm/internal/checksum"
	"github.com/kevherro/dpm/internal/config"
	"github.com/kevherro/dpm/internal/manifest"
	"github.com/kevherro/dpm/internal/state"
)

func TestInstallInstallsPackage(t *testing.T) {
	cfg := testInstallConfig(t)
	artifactPath, artifactSHA := makePackageArtifact(t, "hello")
	writeRegistryManifest(t, cfg, manifestFixture{
		name:    "hello",
		version: "1.0.0",
		url:     "file://" + artifactPath,
		sha256:  artifactSHA,
	})
	now := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)

	result, err := (Installer{
		GOOS:   "darwin",
		GOARCH: "arm64",
		Now:    func() time.Time { return now },
	}).Install(context.Background(), cfg, "hello")
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	if len(result.Packages) != 1 {
		t.Fatalf("len(Packages) = %d, want 1", len(result.Packages))
	}
	pkg := result.Packages[0]
	if pkg.Name != "hello" || pkg.Version != "1.0.0" || pkg.AlreadyInstalled {
		t.Fatalf("PackageResult = %#v, want installed hello 1.0.0", pkg)
	}
	assertFile(t, filepath.Join(cfg.PkgsDir, "hello", "1.0.0", "bin", "hello"), "#!/bin/sh\n")
	assertSymlink(t, filepath.Join(cfg.BinDir, "hello"), filepath.Join(cfg.PkgsDir, "hello", "1.0.0", "bin", "hello"))

	record, err := state.New(cfg).Get("hello")
	if err != nil {
		t.Fatalf("state Get() error = %v", err)
	}
	if record.Name != "hello" || record.Version != "1.0.0" || record.SHA256 != artifactSHA || !record.InstalledAt.Equal(now) {
		t.Fatalf("state record = %#v, want installed hello", record)
	}
}

func TestInstallInstallsDependenciesFirst(t *testing.T) {
	cfg := testInstallConfig(t)
	libArtifact, libSHA := makePackageArtifact(t, "libhello")
	helloArtifact, helloSHA := makePackageArtifact(t, "hello")
	writeRegistryManifest(t, cfg, manifestFixture{
		name:    "libhello",
		version: "1.0.0",
		url:     "file://" + libArtifact,
		sha256:  libSHA,
	})
	writeRegistryManifest(t, cfg, manifestFixture{
		name:         "hello",
		version:      "1.0.0",
		dependencies: []string{"libhello"},
		url:          "file://" + helloArtifact,
		sha256:       helloSHA,
	})

	result, err := (Installer{GOOS: "darwin", GOARCH: "arm64"}).Install(context.Background(), cfg, "hello")
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	if len(result.Packages) != 2 {
		t.Fatalf("len(Packages) = %d, want 2", len(result.Packages))
	}
	if result.Packages[0].Name != "libhello" || result.Packages[1].Name != "hello" {
		t.Fatalf("install order = %#v, want dependency before package", result.Packages)
	}
	for _, name := range []string{"libhello", "hello"} {
		if _, err := state.New(cfg).Get(name); err != nil {
			t.Fatalf("state Get(%s) error = %v", name, err)
		}
	}
}

func TestInstallIsIdempotentForSameVersion(t *testing.T) {
	cfg := testInstallConfig(t)
	artifactPath, artifactSHA := makePackageArtifact(t, "hello")
	writeRegistryManifest(t, cfg, manifestFixture{
		name:    "hello",
		version: "1.0.0",
		url:     "file://" + artifactPath,
		sha256:  artifactSHA,
	})
	installer := Installer{GOOS: "darwin", GOARCH: "arm64"}

	if _, err := installer.Install(context.Background(), cfg, "hello"); err != nil {
		t.Fatalf("first Install() error = %v", err)
	}
	result, err := installer.Install(context.Background(), cfg, "hello")
	if err != nil {
		t.Fatalf("second Install() error = %v", err)
	}

	if len(result.Packages) != 1 || !result.Packages[0].AlreadyInstalled {
		t.Fatalf("second Install() = %#v, want already installed", result)
	}
}

func TestInstallRejectsDifferentInstalledVersion(t *testing.T) {
	cfg := testInstallConfig(t)
	firstArtifact, firstSHA := makePackageArtifact(t, "hello")
	writeRegistryManifest(t, cfg, manifestFixture{
		name:    "hello",
		version: "1.0.0",
		url:     "file://" + firstArtifact,
		sha256:  firstSHA,
	})
	installer := Installer{GOOS: "darwin", GOARCH: "arm64"}
	if _, err := installer.Install(context.Background(), cfg, "hello"); err != nil {
		t.Fatalf("first Install() error = %v", err)
	}
	nextArtifact, nextSHA := makePackageArtifact(t, "hello")
	writeRegistryManifest(t, cfg, manifestFixture{
		name:    "hello",
		version: "1.1.0",
		url:     "file://" + nextArtifact,
		sha256:  nextSHA,
	})

	_, err := installer.Install(context.Background(), cfg, "hello")
	if err == nil {
		t.Fatal("Install() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "already installed") {
		t.Fatalf("Install() error = %q, want already installed", err)
	}
}

func TestInstallRejectsDependencyCycle(t *testing.T) {
	cfg := testInstallConfig(t)
	helloArtifact, helloSHA := makePackageArtifact(t, "hello")
	libArtifact, libSHA := makePackageArtifact(t, "libhello")
	writeRegistryManifest(t, cfg, manifestFixture{
		name:         "hello",
		version:      "1.0.0",
		dependencies: []string{"libhello"},
		url:          "file://" + helloArtifact,
		sha256:       helloSHA,
	})
	writeRegistryManifest(t, cfg, manifestFixture{
		name:         "libhello",
		version:      "1.0.0",
		dependencies: []string{"hello"},
		url:          "file://" + libArtifact,
		sha256:       libSHA,
	})

	_, err := (Installer{GOOS: "darwin", GOARCH: "arm64"}).Install(context.Background(), cfg, "hello")
	if err == nil {
		t.Fatal("Install() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "dependency cycle") {
		t.Fatalf("Install() error = %q, want dependency cycle", err)
	}
}

func TestRemoveRemovesLinksPrefixAndState(t *testing.T) {
	cfg := testInstallConfig(t)
	artifactPath, artifactSHA := makePackageArtifact(t, "hello")
	writeRegistryManifest(t, cfg, manifestFixture{
		name:    "hello",
		version: "1.0.0",
		url:     "file://" + artifactPath,
		sha256:  artifactSHA,
	})
	if _, err := (Installer{GOOS: "darwin", GOARCH: "arm64"}).Install(context.Background(), cfg, "hello"); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	result, err := Remove(cfg, "hello")
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if result.Record.Name != "hello" || result.Record.Version != "1.0.0" {
		t.Fatalf("Remove() = %#v, want hello 1.0.0", result)
	}
	if _, err := os.Lstat(filepath.Join(cfg.BinDir, "hello")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("bin link Lstat() error = %v, want not exist", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.PkgsDir, "hello", "1.0.0")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("prefix Stat() error = %v, want not exist", err)
	}
	if _, err := state.New(cfg).Get("hello"); !errors.Is(err, state.ErrNotInstalled) {
		t.Fatalf("state Get() error = %v, want ErrNotInstalled", err)
	}
}

func TestRemoveRejectsMissingPackage(t *testing.T) {
	cfg := testInstallConfig(t)

	_, err := Remove(cfg, "missing")
	if !errors.Is(err, state.ErrNotInstalled) {
		t.Fatalf("Remove() error = %v, want ErrNotInstalled", err)
	}
}

func TestSelectArtifactNormalizesArchitecture(t *testing.T) {
	m := manifest.Manifest{
		Name:    "hello",
		Version: "1.0.0",
		Artifacts: []manifest.Artifact{
			{OS: "darwin", Arch: "x86_64", URL: "file://hello.tar.gz", SHA256: strings.Repeat("a", 64)},
		},
	}

	got, err := SelectArtifact(m, "darwin", "amd64")
	if err != nil {
		t.Fatalf("SelectArtifact() error = %v", err)
	}
	if got.Arch != "x86_64" {
		t.Fatalf("Arch = %q, want x86_64", got.Arch)
	}
}

type manifestFixture struct {
	name         string
	version      string
	dependencies []string
	url          string
	sha256       string
}

func testInstallConfig(t *testing.T) config.Config {
	t.Helper()

	cfg, err := config.FromRoot(filepath.Join(t.TempDir(), "dpm-root"))
	if err != nil {
		t.Fatalf("FromRoot() error = %v", err)
	}

	return cfg
}

func writeRegistryManifest(t *testing.T, cfg config.Config, fixture manifestFixture) {
	t.Helper()

	dir := filepath.Join(cfg.RegistryDir, "packages", fixture.name, fixture.version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	deps := "[]"
	if len(fixture.dependencies) > 0 {
		parts := make([]string, 0, len(fixture.dependencies))
		for _, dep := range fixture.dependencies {
			parts = append(parts, `"`+dep+`"`)
		}
		deps = "[" + strings.Join(parts, ", ") + "]"
	}
	contents := `name = "` + fixture.name + `"
version = "` + fixture.version + `"
dependencies = ` + deps + `

[[artifacts]]
os = "darwin"
arch = "arm64"
url = "` + fixture.url + `"
sha256 = "` + fixture.sha256 + `"

[install]
bins = ["bin/` + fixture.name + `"]
`
	if err := os.WriteFile(filepath.Join(dir, "dpm.toml"), []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func makePackageArtifact(t *testing.T, name string) (string, string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), name+".tar.gz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	body := "#!/bin/sh\n"
	header := &tar.Header{
		Name:     "bin/" + name,
		Typeflag: tar.TypeReg,
		Mode:     0o755,
		Size:     int64(len(body)),
	}
	if err := tw.WriteHeader(header); err != nil {
		t.Fatalf("WriteHeader() error = %v", err)
	}
	if _, err := io.Copy(tw, strings.NewReader(body)); err != nil {
		t.Fatalf("Copy() error = %v", err)
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

	sum, err := checksum.FileSHA256(path)
	if err != nil {
		t.Fatalf("FileSHA256() error = %v", err)
	}

	return path, sum
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
