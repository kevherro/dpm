// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package registry

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/kevherro/dpm/internal/manifest"
)

var (
	// ErrPackageNotFound means a package is absent from the registry.
	ErrPackageNotFound = errors.New("package not found")
	// ErrVersionNotFound means a requested package version is absent from the registry.
	ErrVersionNotFound = errors.New("package version not found")
)

// Registry resolves package manifests from a local registry checkout.
type Registry struct {
	Root        string
	StaticIndex bool
}

// Options configures a registry reader.
type Options struct {
	Root        string
	StaticIndex bool
}

// SearchResult describes a package matched by registry search.
type SearchResult struct {
	Name       string
	Summary    string
	Homepage   string
	Categories []string
}

// New returns a local registry rooted at root.
func New(root string) (Registry, error) {
	return NewWithOptions(Options{Root: root})
}

// NewWithOptions returns a local registry reader.
func NewWithOptions(opts Options) (Registry, error) {
	root := opts.Root
	if root == "" {
		return Registry{}, fmt.Errorf("registry root is empty")
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Registry{}, fmt.Errorf("resolve registry root %q: %w", root, err)
	}

	return Registry{Root: filepath.Clean(absRoot), StaticIndex: opts.StaticIndex}, nil
}

// Resolve returns the newest non-yanked version of name.
func (r Registry) Resolve(name string) (manifest.Manifest, error) {
	versions, err := r.Versions(name)
	if err != nil {
		return manifest.Manifest{}, err
	}
	if len(versions) == 0 {
		return manifest.Manifest{}, fmt.Errorf("%w: %s", ErrPackageNotFound, name)
	}

	for i := len(versions) - 1; i >= 0; i-- {
		m, err := r.ResolveVersion(name, versions[i])
		if err != nil {
			return manifest.Manifest{}, err
		}
		if m.Yanked {
			continue
		}

		return m, nil
	}

	return manifest.Manifest{}, fmt.Errorf("%w: no non-yanked versions for %s", ErrVersionNotFound, name)
}

// ResolveVersion returns a specific package manifest.
func (r Registry) ResolveVersion(name, version string) (manifest.Manifest, error) {
	if r.StaticIndex {
		return r.staticResolveVersion(name, version)
	}

	path, err := r.ManifestPath(name, version)
	if err != nil {
		return manifest.Manifest{}, err
	}

	m, err := manifest.Load(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return manifest.Manifest{}, fmt.Errorf("%w: %s %s", ErrVersionNotFound, name, version)
		}
		return manifest.Manifest{}, err
	}
	if m.Name != name {
		return manifest.Manifest{}, fmt.Errorf("manifest name %q does not match registry package %q", m.Name, name)
	}
	if m.Version != version {
		return manifest.Manifest{}, fmt.Errorf("manifest version %q does not match registry version %q", m.Version, version)
	}

	return m, nil
}

// Versions lists available versions for name in ascending order.
func (r Registry) Versions(name string) ([]string, error) {
	if r.StaticIndex {
		return r.staticVersions(name)
	}

	versionsDir, err := r.VersionsDir(name)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(versionsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrPackageNotFound, name)
		}
		return nil, fmt.Errorf("read package versions for %s: %w", name, err)
	}

	var versions []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		version := entry.Name()
		if err := manifest.ValidateVersion(version); err != nil {
			return nil, fmt.Errorf("invalid registry version directory %q for %s: %w", version, name, err)
		}
		versions = append(versions, version)
	}
	sortVersions(versions)

	return versions, nil
}

// Search returns packages whose name or metadata contains query.
func (r Registry) Search(query string) ([]SearchResult, error) {
	if r.StaticIndex {
		return r.staticSearch(query)
	}

	packagesDir := filepath.Join(r.Root, "packages")
	entries, err := os.ReadDir(packagesDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read registry packages: %w", err)
	}

	query = strings.ToLower(query)
	var matches []SearchResult
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if err := validatePathPart("package", name); err != nil {
			return nil, fmt.Errorf("invalid registry package directory %q: %w", name, err)
		}
		result, err := r.searchResult(name)
		if err != nil {
			return nil, err
		}
		if result.matches(query) {
			matches = append(matches, result)
		}
	}
	slices.SortFunc(matches, func(a, b SearchResult) int {
		return strings.Compare(a.Name, b.Name)
	})

	return matches, nil
}

// PackageDir returns the registry directory for name.
func (r Registry) PackageDir(name string) (string, error) {
	if err := validatePathPart("package", name); err != nil {
		return "", err
	}

	return filepath.Join(r.Root, "packages", name), nil
}

// VersionsDir returns the version directory root for name.
func (r Registry) VersionsDir(name string) (string, error) {
	pkgDir, err := r.PackageDir(name)
	if err != nil {
		return "", err
	}

	return filepath.Join(pkgDir, "versions"), nil
}

// ManifestPath returns the dpm.toml path for name and version.
func (r Registry) ManifestPath(name, version string) (string, error) {
	if err := validatePathPart("package", name); err != nil {
		return "", err
	}
	if err := manifest.ValidateVersion(version); err != nil {
		return "", err
	}

	return filepath.Join(r.Root, "packages", name, "versions", version, "dpm.toml"), nil
}

func (r Registry) searchResult(name string) (SearchResult, error) {
	pkg, err := r.Package(name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return SearchResult{Name: name}, nil
		}

		return SearchResult{}, err
	}

	return SearchResult{
		Name:       pkg.Name,
		Summary:    pkg.Summary,
		Homepage:   pkg.Homepage,
		Categories: slices.Clone(pkg.Categories),
	}, nil
}

func (r SearchResult) matches(query string) bool {
	if query == "" {
		return true
	}
	if strings.Contains(strings.ToLower(r.Name), query) ||
		strings.Contains(strings.ToLower(r.Summary), query) ||
		strings.Contains(strings.ToLower(r.Homepage), query) {
		return true
	}
	for _, category := range r.Categories {
		if strings.Contains(strings.ToLower(category), query) {
			return true
		}
	}

	return false
}

func validatePathPart(kind, value string) error {
	if value == "" {
		return fmt.Errorf("%s is empty", kind)
	}
	if value == "." || value == ".." {
		return fmt.Errorf("%s %q is not allowed", kind, value)
	}
	if strings.ContainsAny(value, `/\`) {
		return fmt.Errorf("%s %q must not contain path separators", kind, value)
	}
	if !filepath.IsLocal(value) {
		return fmt.Errorf("%s %q must be local", kind, value)
	}

	return nil
}

func sortVersions(versions []string) {
	slices.SortFunc(versions, manifest.CompareVersions)
}

func compareVersions(a, b string) int {
	return manifest.CompareVersions(a, b)
}
