// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package registry

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
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
	Root string
}

// New returns a local registry rooted at root.
func New(root string) (Registry, error) {
	if root == "" {
		return Registry{}, fmt.Errorf("registry root is empty")
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Registry{}, fmt.Errorf("resolve registry root %q: %w", root, err)
	}

	return Registry{Root: filepath.Clean(absRoot)}, nil
}

// Resolve returns the newest available version of name.
func (r Registry) Resolve(name string) (manifest.Manifest, error) {
	versions, err := r.Versions(name)
	if err != nil {
		return manifest.Manifest{}, err
	}
	if len(versions) == 0 {
		return manifest.Manifest{}, fmt.Errorf("%w: %s", ErrPackageNotFound, name)
	}

	return r.ResolveVersion(name, versions[len(versions)-1])
}

// ResolveVersion returns a specific package manifest.
func (r Registry) ResolveVersion(name, version string) (manifest.Manifest, error) {
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
	pkgDir, err := r.PackageDir(name)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(pkgDir)
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
		if err := validatePathPart("version", version); err != nil {
			return nil, fmt.Errorf("invalid registry version directory %q for %s: %w", version, name, err)
		}
		versions = append(versions, version)
	}
	sortVersions(versions)

	return versions, nil
}

// Search returns package names containing query.
func (r Registry) Search(query string) ([]string, error) {
	packagesDir := filepath.Join(r.Root, "packages")
	entries, err := os.ReadDir(packagesDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read registry packages: %w", err)
	}

	query = strings.ToLower(query)
	var matches []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if err := validatePathPart("package", name); err != nil {
			return nil, fmt.Errorf("invalid registry package directory %q: %w", name, err)
		}
		if query == "" || strings.Contains(strings.ToLower(name), query) {
			matches = append(matches, name)
		}
	}
	slices.Sort(matches)

	return matches, nil
}

// PackageDir returns the registry directory for name.
func (r Registry) PackageDir(name string) (string, error) {
	if err := validatePathPart("package", name); err != nil {
		return "", err
	}

	return filepath.Join(r.Root, "packages", name), nil
}

// ManifestPath returns the dpm.toml path for name and version.
func (r Registry) ManifestPath(name, version string) (string, error) {
	if err := validatePathPart("package", name); err != nil {
		return "", err
	}
	if err := validatePathPart("version", version); err != nil {
		return "", err
	}

	return filepath.Join(r.Root, "packages", name, version, "dpm.toml"), nil
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
	slices.SortFunc(versions, compareVersions)
}

func compareVersions(a, b string) int {
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")
	maxLen := max(len(aParts), len(bParts))
	for i := range maxLen {
		aPart, bPart := "", ""
		if i < len(aParts) {
			aPart = aParts[i]
		}
		if i < len(bParts) {
			bPart = bParts[i]
		}

		aNum, aErr := strconv.Atoi(aPart)
		bNum, bErr := strconv.Atoi(bPart)
		if aErr == nil && bErr == nil {
			if aNum != bNum {
				return aNum - bNum
			}
			continue
		}
		if aPart != bPart {
			return strings.Compare(aPart, bPart)
		}
	}

	return 0
}
