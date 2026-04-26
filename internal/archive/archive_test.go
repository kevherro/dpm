// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package archive

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractTarGzExtractsFilesAndDirectories(t *testing.T) {
	archivePath := makeTarGz(t, []tarEntry{
		dirEntry("bin", 0o755),
		fileEntry("bin/hello", "hello\n", 0o755),
		fileEntry("share/doc.txt", "docs\n", 0o644),
	})
	dst := filepath.Join(t.TempDir(), "dst")

	if err := ExtractTarGz(archivePath, dst); err != nil {
		t.Fatalf("ExtractTarGz() error = %v", err)
	}

	assertFile(t, filepath.Join(dst, "bin", "hello"), "hello\n")
	assertFile(t, filepath.Join(dst, "share", "doc.txt"), "docs\n")
	info, err := os.Stat(filepath.Join(dst, "bin", "hello"))
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("mode = %v, want 0755", got)
	}
}

func TestExtractTarGzRejectsUnsafePaths(t *testing.T) {
	tests := []struct {
		name  string
		entry tarEntry
	}{
		{name: "parent traversal", entry: fileEntry("../evil", "bad", 0o644)},
		{name: "deep traversal", entry: fileEntry("../../.ssh/id_rsa", "bad", 0o644)},
		{name: "absolute", entry: fileEntry("/absolute/path", "bad", 0o644)},
		{name: "clean traversal", entry: fileEntry("bin/../../evil", "bad", 0o644)},
		{name: "backslash", entry: fileEntry(`bin\evil`, "bad", 0o644)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			dst := filepath.Join(root, "dst")
			outside := filepath.Join(root, "evil")
			if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}

			err := ExtractTarGz(makeTarGz(t, []tarEntry{tt.entry}), dst)
			if err == nil {
				t.Fatal("ExtractTarGz() error = nil, want error")
			}
			assertFile(t, outside, "outside")
		})
	}
}

func TestExtractTarGzRejectsLinks(t *testing.T) {
	tests := []tarEntry{
		{
			header: &tar.Header{
				Name:     "bin/hello",
				Typeflag: tar.TypeSymlink,
				Linkname: "../outside",
				Mode:     0o777,
			},
		},
		{
			header: &tar.Header{
				Name:     "bin/hello-hard",
				Typeflag: tar.TypeLink,
				Linkname: "bin/hello",
				Mode:     0o777,
			},
		},
	}

	for _, entry := range tests {
		t.Run(string(entry.header.Typeflag), func(t *testing.T) {
			err := ExtractTarGz(makeTarGz(t, []tarEntry{entry}), filepath.Join(t.TempDir(), "dst"))
			if err == nil {
				t.Fatal("ExtractTarGz() error = nil, want error")
			}
			if !strings.Contains(err.Error(), "links") {
				t.Fatalf("ExtractTarGz() error = %q, want link rejection", err)
			}
		})
	}
}

func TestExtractTarGzRejectsUnsupportedTypes(t *testing.T) {
	err := ExtractTarGz(makeTarGz(t, []tarEntry{
		{
			header: &tar.Header{
				Name:     "dev/null",
				Typeflag: tar.TypeChar,
				Mode:     0o644,
			},
		},
	}), filepath.Join(t.TempDir(), "dst"))
	if err == nil {
		t.Fatal("ExtractTarGz() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "unsupported type") {
		t.Fatalf("ExtractTarGz() error = %q, want unsupported type", err)
	}
}

func TestExtractTarGzRefusesOverwrite(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "dst")
	existing := filepath.Join(dst, "bin", "hello")
	if err := os.MkdirAll(filepath.Dir(existing), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(existing, []byte("existing"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	err := ExtractTarGz(makeTarGz(t, []tarEntry{
		fileEntry("bin/hello", "new", 0o755),
	}), dst)
	if err == nil {
		t.Fatal("ExtractTarGz() error = nil, want error")
	}
	assertFile(t, existing, "existing")
}

func TestExtractTarGzRejectsSymlinkParentEscape(t *testing.T) {
	root := t.TempDir()
	dst := filepath.Join(root, "dst")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(dst, "bin")); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	err := ExtractTarGz(makeTarGz(t, []tarEntry{
		fileEntry("bin/hello", "bad", 0o755),
	}), dst)
	if err == nil {
		t.Fatal("ExtractTarGz() error = nil, want error")
	}
	if _, err := os.Stat(filepath.Join(outside, "hello")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("outside file stat error = %v, want not exist", err)
	}
}

func TestExtractTarGzRejectsInvalidGzip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.tar.gz")
	if err := os.WriteFile(path, []byte("not gzip"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := ExtractTarGz(path, filepath.Join(t.TempDir(), "dst")); err == nil {
		t.Fatal("ExtractTarGz() error = nil, want error")
	}
}

type tarEntry struct {
	header *tar.Header
	body   string
}

func dirEntry(name string, mode int64) tarEntry {
	return tarEntry{
		header: &tar.Header{
			Name:     name,
			Typeflag: tar.TypeDir,
			Mode:     mode,
		},
	}
}

func fileEntry(name, body string, mode int64) tarEntry {
	return tarEntry{
		header: &tar.Header{
			Name:     name,
			Typeflag: tar.TypeReg,
			Mode:     mode,
			Size:     int64(len(body)),
		},
		body: body,
	}
}

func makeTarGz(t *testing.T, entries []tarEntry) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "fixture.tar.gz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, entry := range entries {
		if err := tw.WriteHeader(entry.header); err != nil {
			t.Fatalf("WriteHeader() error = %v", err)
		}
		if entry.body != "" {
			if _, err := io.Copy(tw, strings.NewReader(entry.body)); err != nil {
				t.Fatalf("Copy() error = %v", err)
			}
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

	return path
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
