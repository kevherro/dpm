// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package registry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/kevherro/dpm/internal/checksum"
	"github.com/kevherro/dpm/internal/manifest"
)

// ValidateOptions configures registry validation.
type ValidateOptions struct {
	Root            string
	VerifyArtifacts bool
	Client          *http.Client
}

// ValidationReport contains all issues found while validating a registry.
type ValidationReport struct {
	Root   string
	Issues []ValidationIssue
}

// ValidationIssue describes one registry validation failure.
type ValidationIssue struct {
	Path    string
	Message string
}

// Valid reports whether the registry has no validation issues.
func (r ValidationReport) Valid() bool {
	return len(r.Issues) == 0
}

// Validate validates a registry checkout.
func Validate(ctx context.Context, opts ValidateOptions) (ValidationReport, error) {
	reg, err := New(opts.Root)
	if err != nil {
		return ValidationReport{}, err
	}
	v := validator{
		reg:             reg,
		verifyArtifacts: opts.VerifyArtifacts,
		client:          opts.Client,
	}
	if v.client == nil {
		v.client = &http.Client{Timeout: 30 * time.Second}
	}
	return v.validate(ctx), nil
}

type validator struct {
	reg             Registry
	verifyArtifacts bool
	client          *http.Client
	issues          []ValidationIssue
}

func (v *validator) validate(ctx context.Context) ValidationReport {
	v.validateMetadata()
	packages := v.validatePackages(ctx)
	v.validateDependencies(packages)

	return ValidationReport{Root: v.reg.Root, Issues: v.issues}
}

func (v *validator) validateMetadata() {
	if _, err := v.reg.Metadata(); err != nil {
		v.add(MetadataFile, "%v", err)
	}
}

func (v *validator) validatePackages(ctx context.Context) map[string][]manifest.Manifest {
	result := map[string][]manifest.Manifest{}
	packagesDir := filepath.Join(v.reg.Root, "packages")
	entries, err := os.ReadDir(packagesDir)
	if err != nil {
		v.add("packages", "read packages directory: %v", err)
		return result
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		pkgRel := filepath.Join("packages", name)
		if err := validatePathPart("package", name); err != nil {
			v.add(pkgRel, "invalid package directory: %v", err)
			continue
		}
		if _, err := v.reg.Package(name); err != nil {
			v.add(filepath.Join(pkgRel, PackageFile), "%v", err)
		}
		result[name] = v.validateVersions(ctx, name)
	}

	return result
}

func (v *validator) validateVersions(ctx context.Context, name string) []manifest.Manifest {
	var manifests []manifest.Manifest
	versionsDir := filepath.Join(v.reg.Root, "packages", name, "versions")
	entries, err := os.ReadDir(versionsDir)
	if err != nil {
		v.add(filepath.Join("packages", name, "versions"), "read versions directory: %v", err)
		return manifests
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		version := entry.Name()
		versionRel := filepath.Join("packages", name, "versions", version)
		if err := validatePathPart("version", version); err != nil {
			v.add(versionRel, "invalid version directory: %v", err)
			continue
		}
		m, err := v.reg.ResolveVersion(name, version)
		if err != nil {
			v.add(filepath.Join(versionRel, "dpm.toml"), "%v", err)
			continue
		}
		manifests = append(manifests, m)
		v.validateManifest(ctx, versionRel, m)
	}

	return manifests
}

func (v *validator) validateManifest(ctx context.Context, versionRel string, m manifest.Manifest) {
	if m.Yanked && strings.TrimSpace(m.YankReason) == "" {
		v.add(filepath.Join(versionRel, "dpm.toml"), "yanked version must include yank_reason")
	}
	for _, dep := range m.Dependencies {
		if err := validatePathPart("dependency", dep); err != nil {
			v.add(filepath.Join(versionRel, "dpm.toml"), "invalid dependency %q: %v", dep, err)
		}
	}
	if !hasDarwinARM64Artifact(m) {
		v.add(filepath.Join(versionRel, "dpm.toml"), "missing darwin/arm64 artifact")
	}
	for i, artifact := range m.Artifacts {
		artifactPath := fmt.Sprintf("%s artifact %d", filepath.Join(versionRel, "dpm.toml"), i+1)
		if err := validatePinnedArtifactURL(artifact.URL); err != nil {
			v.add(artifactPath, "%v", err)
		}
		if _, err := checksum.NormalizeSHA256(artifact.SHA256); err != nil {
			v.add(artifactPath, "invalid sha256: %v", err)
		}
		if v.verifyArtifacts {
			if err := verifyArtifact(ctx, v.client, artifact); err != nil {
				v.add(artifactPath, "verify artifact: %v", err)
			}
		}
	}
}

func (v *validator) validateDependencies(packages map[string][]manifest.Manifest) {
	for name, manifests := range packages {
		for _, m := range manifests {
			for _, dep := range m.Dependencies {
				if _, ok := packages[dep]; !ok {
					v.add(filepath.Join("packages", name, "versions", m.Version, "dpm.toml"), "dependency %q is not in registry", dep)
					continue
				}
				if len(packages[dep]) == 0 {
					v.add(filepath.Join("packages", name, "versions", m.Version, "dpm.toml"), "dependency %q has no versions", dep)
				}
			}
		}
	}
}

func (v *validator) add(path, format string, args ...any) {
	v.issues = append(v.issues, ValidationIssue{
		Path:    path,
		Message: fmt.Sprintf(format, args...),
	})
}

func hasDarwinARM64Artifact(m manifest.Manifest) bool {
	return slices.ContainsFunc(m.Artifacts, func(a manifest.Artifact) bool {
		return a.OS == "darwin" && a.Arch == "arm64"
	})
}

func validatePinnedArtifactURL(rawURL string) error {
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
	if strings.Contains(lower, "/archive/main.") ||
		strings.Contains(lower, "/archive/master.") ||
		strings.Contains(lower, "/archive/refs/heads/") {
		return fmt.Errorf("artifact url %q looks mutable: branch archives are not allowed", rawURL)
	}

	return nil
}

func verifyArtifact(ctx context.Context, client *http.Client, artifact manifest.Artifact) error {
	expected, err := checksum.NormalizeSHA256(artifact.SHA256)
	if err != nil {
		return err
	}
	r, err := openArtifact(ctx, client, artifact.URL)
	if err != nil {
		return err
	}
	defer r.Close()

	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return fmt.Errorf("read artifact %q: %w", artifact.URL, err)
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if actual != expected {
		return checksum.MismatchError{
			Path:     artifact.URL,
			Expected: expected,
			Actual:   actual,
		}
	}

	return nil
}

func openArtifact(ctx context.Context, client *http.Client, rawURL string) (io.ReadCloser, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse artifact url %q: %w", rawURL, err)
	}
	switch u.Scheme {
	case "file":
		path, err := fileArtifactPath(rawURL)
		if err != nil {
			return nil, err
		}
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open file artifact %s: %w", path, err)
		}
		return f, nil
	case "https":
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, fmt.Errorf("create artifact request %q: %w", rawURL, err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch artifact %q: %w", rawURL, err)
		}
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			defer resp.Body.Close()
			return nil, fmt.Errorf("fetch artifact %q: unexpected status %s", rawURL, resp.Status)
		}

		return resp.Body, nil
	default:
		return nil, fmt.Errorf("unsupported artifact url scheme %q", u.Scheme)
	}
}

func fileArtifactPath(rawURL string) (string, error) {
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
