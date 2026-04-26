// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package install

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kevherro/dpm/internal/checksum"
	"github.com/kevherro/dpm/internal/config"
	"github.com/kevherro/dpm/internal/manifest"
)

// ArtifactFetcher fetches pinned artifacts into the dpm downloads cache.
type ArtifactFetcher struct {
	Client *http.Client
}

// FetchArtifact fetches artifact using a default HTTP client.
func FetchArtifact(ctx context.Context, cfg config.Config, artifact manifest.Artifact) (string, error) {
	return ArtifactFetcher{
		Client: &http.Client{Timeout: 30 * time.Second},
	}.Fetch(ctx, cfg, artifact)
}

// Fetch fetches artifact into cfg.DownloadsDir, caching by SHA-256.
func (f ArtifactFetcher) Fetch(ctx context.Context, cfg config.Config, artifact manifest.Artifact) (string, error) {
	sum, err := checksum.NormalizeSHA256(artifact.SHA256)
	if err != nil {
		return "", fmt.Errorf("invalid artifact checksum: %w", err)
	}
	if err := validateArtifactURL(artifact.URL); err != nil {
		return "", err
	}
	if err := cfg.RequireInsideRoot(cfg.DownloadsDir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(cfg.DownloadsDir, 0o755); err != nil {
		return "", fmt.Errorf("create downloads directory %s: %w", cfg.DownloadsDir, err)
	}

	cachePath := filepath.Join(cfg.DownloadsDir, sum)
	if err := cfg.RequireInsideRoot(cachePath); err != nil {
		return "", err
	}
	if _, err := os.Stat(cachePath); err == nil {
		if err := checksum.VerifyFileSHA256(cachePath, sum); err != nil {
			return "", fmt.Errorf("verify cached artifact %s: %w", cachePath, err)
		}
		return cachePath, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("stat cached artifact %s: %w", cachePath, err)
	}

	tmp, err := os.CreateTemp(cfg.DownloadsDir, ".download-*")
	if err != nil {
		return "", fmt.Errorf("create temporary artifact in %s: %w", cfg.DownloadsDir, err)
	}
	tmpPath := tmp.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := f.copyArtifact(ctx, tmp, artifact.URL); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close temporary artifact %s: %w", tmpPath, err)
	}
	if err := checksum.VerifyFileSHA256(tmpPath, sum); err != nil {
		return "", err
	}
	if err := os.Rename(tmpPath, cachePath); err != nil {
		return "", fmt.Errorf("cache artifact as %s: %w", cachePath, err)
	}
	removeTmp = false

	return cachePath, nil
}

func (f ArtifactFetcher) copyArtifact(ctx context.Context, dst io.Writer, rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("parse artifact url %q: %w", rawURL, err)
	}

	switch u.Scheme {
	case "file":
		path, err := fileURLPath(rawURL)
		if err != nil {
			return err
		}
		return copyFile(dst, path)
	case "https":
		client := f.Client
		if client == nil {
			client = &http.Client{Timeout: 30 * time.Second}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return fmt.Errorf("create artifact request %q: %w", rawURL, err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("fetch artifact %q: %w", rawURL, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return fmt.Errorf("fetch artifact %q: unexpected status %s", rawURL, resp.Status)
		}
		if _, err := io.Copy(dst, resp.Body); err != nil {
			return fmt.Errorf("write artifact %q: %w", rawURL, err)
		}
		return nil
	default:
		return fmt.Errorf("unsupported artifact url scheme %q", u.Scheme)
	}
}

func validateArtifactURL(rawURL string) error {
	if rawURL == "" {
		return fmt.Errorf("artifact url is required")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("parse artifact url %q: %w", rawURL, err)
	}
	if u.Scheme != "file" && u.Scheme != "https" {
		return fmt.Errorf("unsupported artifact url scheme %q", u.Scheme)
	}

	lower := strings.ToLower(rawURL)
	if strings.Contains(lower, "latest") {
		return fmt.Errorf("artifact url %q looks mutable: latest downloads are not allowed", rawURL)
	}
	if strings.Contains(lower, "/archive/main.") || strings.Contains(lower, "/archive/master.") ||
		strings.Contains(lower, "/archive/refs/heads/") {
		return fmt.Errorf("artifact url %q looks mutable: branch archives are not allowed", rawURL)
	}

	return nil
}

func fileURLPath(rawURL string) (string, error) {
	path, ok := strings.CutPrefix(rawURL, "file://")
	if !ok {
		return "", fmt.Errorf("artifact url %q is not a file url", rawURL)
	}
	if path == "" {
		return "", fmt.Errorf("file artifact url %q has empty path", rawURL)
	}
	path, err := url.PathUnescape(path)
	if err != nil {
		return "", fmt.Errorf("decode file artifact url %q: %w", rawURL, err)
	}

	return path, nil
}

func copyFile(dst io.Writer, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open file artifact %s: %w", path, err)
	}
	defer f.Close()

	if _, err := io.Copy(dst, f); err != nil {
		return fmt.Errorf("read file artifact %s: %w", path, err)
	}

	return nil
}
