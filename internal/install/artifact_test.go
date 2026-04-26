// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package install

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kevherro/dpm/internal/checksum"
	"github.com/kevherro/dpm/internal/config"
	"github.com/kevherro/dpm/internal/manifest"
)

const artifactSHA256 = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"

func TestFetchArtifactCopiesFileArtifactIntoChecksumCache(t *testing.T) {
	cfg := testConfig(t)
	source := writeArtifact(t, "hello")

	got, err := FetchArtifact(context.Background(), cfg, manifest.Artifact{
		URL:    "file://" + source,
		SHA256: artifactSHA256,
	})
	if err != nil {
		t.Fatalf("FetchArtifact() error = %v", err)
	}

	want := filepath.Join(cfg.DownloadsDir, artifactSHA256)
	if got != want {
		t.Fatalf("FetchArtifact() = %q, want %q", got, want)
	}
	data, err := os.ReadFile(got)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("cached artifact = %q, want hello", data)
	}
}

func TestFetchArtifactUsesVerifiedCache(t *testing.T) {
	cfg := testConfig(t)
	if err := os.MkdirAll(cfg.DownloadsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	cachePath := filepath.Join(cfg.DownloadsDir, artifactSHA256)
	if err := os.WriteFile(cachePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := FetchArtifact(context.Background(), cfg, manifest.Artifact{
		URL:    "file://" + filepath.Join(t.TempDir(), "missing"),
		SHA256: artifactSHA256,
	})
	if err != nil {
		t.Fatalf("FetchArtifact() error = %v", err)
	}
	if got != cachePath {
		t.Fatalf("FetchArtifact() = %q, want %q", got, cachePath)
	}
}

func TestFetchArtifactRejectsBadChecksum(t *testing.T) {
	cfg := testConfig(t)
	source := writeArtifact(t, "hello")

	_, err := FetchArtifact(context.Background(), cfg, manifest.Artifact{
		URL:    "file://" + source,
		SHA256: strings.Repeat("0", 64),
	})
	var mismatch checksum.MismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("FetchArtifact() error = %v, want checksum mismatch", err)
	}
}

func TestFetchArtifactRejectsPoisonedCache(t *testing.T) {
	cfg := testConfig(t)
	if err := os.MkdirAll(cfg.DownloadsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfg.DownloadsDir, artifactSHA256), []byte("bad"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := FetchArtifact(context.Background(), cfg, manifest.Artifact{
		URL:    "file://" + writeArtifact(t, "hello"),
		SHA256: artifactSHA256,
	})
	var mismatch checksum.MismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("FetchArtifact() error = %v, want checksum mismatch", err)
	}
}

func TestFetchArtifactRejectsMutableURLs(t *testing.T) {
	cfg := testConfig(t)
	tests := []string{
		"https://example.com/foo-latest.tar.gz",
		"https://github.com/org/repo/archive/main.tar.gz",
		"https://github.com/org/repo/archive/master.tar.gz",
		"https://github.com/org/repo/archive/refs/heads/main.tar.gz",
	}

	for _, rawURL := range tests {
		t.Run(rawURL, func(t *testing.T) {
			_, err := FetchArtifact(context.Background(), cfg, manifest.Artifact{
				URL:    rawURL,
				SHA256: artifactSHA256,
			})
			if err == nil {
				t.Fatal("FetchArtifact() error = nil, want error")
			}
		})
	}
}

func TestFetchArtifactRejectsUnsupportedScheme(t *testing.T) {
	cfg := testConfig(t)

	_, err := FetchArtifact(context.Background(), cfg, manifest.Artifact{
		URL:    "http://example.com/hello.tar.gz",
		SHA256: artifactSHA256,
	})
	if err == nil {
		t.Fatal("FetchArtifact() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "unsupported artifact url scheme") {
		t.Fatalf("FetchArtifact() error = %q, want unsupported scheme", err)
	}
}

func TestArtifactFetcherFetchesHTTPSArtifact(t *testing.T) {
	cfg := testConfig(t)
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.String() != "https://example.com/hello-1.0.0.tar.gz" {
				t.Fatalf("request URL = %q, want pinned HTTPS URL", req.URL.String())
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Body:       io.NopCloser(bytes.NewReader([]byte("hello"))),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}),
	}

	got, err := ArtifactFetcher{Client: client}.Fetch(context.Background(), cfg, manifest.Artifact{
		URL:    "https://example.com/hello-1.0.0.tar.gz",
		SHA256: artifactSHA256,
	})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if got != filepath.Join(cfg.DownloadsDir, artifactSHA256) {
		t.Fatalf("Fetch() = %q, want checksum cache path", got)
	}
}

func testConfig(t *testing.T) config.Config {
	t.Helper()

	cfg, err := config.FromRoot(filepath.Join(t.TempDir(), "dpm-root"))
	if err != nil {
		t.Fatalf("FromRoot() error = %v", err)
	}

	return cfg
}

func writeArtifact(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "artifact.tar.gz")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	return path
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
