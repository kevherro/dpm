// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package testutil

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kevherro/dpm/internal/checksum"
	"github.com/kevherro/dpm/internal/config"
)

const (
	// HelloName is the demo package name.
	HelloName = "hello"
	// HelloVersion is the demo package version.
	HelloVersion = "1.0.0"
	// HelloOutput is printed by the installed demo executable.
	HelloOutput = "hello from dpm\n"
)

// HelloFixture describes the generated hello registry entry and artifact.
type HelloFixture struct {
	ArtifactPath string
	SHA256       string
	URL          string
}

// WriteHelloRegistry writes a complete local registry entry for hello.
func WriteHelloRegistry(t testing.TB, cfg config.Config) HelloFixture {
	t.Helper()

	artifactPath := filepath.Join(t.TempDir(), "hello-1.0.0-"+runtime.GOOS+"-"+runtime.GOARCH+".tar.gz")
	writeHelloArtifact(t, artifactPath)
	sum, err := checksum.FileSHA256(artifactPath)
	if err != nil {
		t.Fatalf("FileSHA256() error = %v", err)
	}
	url := "file://" + artifactPath
	artifacts := `[[artifacts]]
os = "darwin"
arch = "arm64"
url = "` + url + `"
sha256 = "` + sum + `"
`
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		artifacts += `
[[artifacts]]
os = "` + runtime.GOOS + `"
arch = "` + runtime.GOARCH + `"
url = "` + url + `"
sha256 = "` + sum + `"
`
	}

	dir := filepath.Join(cfg.RegistryDir, "packages", HelloName, "versions", HelloVersion)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	manifest := `schema = 1
name = "` + HelloName + `"
version = "` + HelloVersion + `"
dependencies = []

` + artifacts + `
[install]
bins = ["bin/hello"]
`
	if err := os.WriteFile(filepath.Join(dir, "dpm.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	return HelloFixture{
		ArtifactPath: artifactPath,
		SHA256:       sum,
		URL:          url,
	}
}

func writeHelloArtifact(t testing.TB, path string) {
	t.Helper()

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	body := "#!/bin/sh\nprintf 'hello from dpm\\n'\n"
	header := &tar.Header{
		Name:     "bin/hello",
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
}
