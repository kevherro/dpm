// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/kevherro/dpm/internal/checksum"
	"github.com/kevherro/dpm/internal/manifest"
)

const (
	// StaticIndexDir is the generated registry distribution directory.
	StaticIndexDir = "index"
)

type staticMetadata struct {
	Schema      int    `json:"schema"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type staticPackage struct {
	Name           string   `json:"name"`
	Summary        string   `json:"summary"`
	Homepage       string   `json:"homepage"`
	License        string   `json:"license"`
	Categories     []string `json:"categories"`
	VersionsPath   string   `json:"versions_path"`
	VersionsSHA256 string   `json:"versions_sha256"`
}

type staticPackageMetadata struct {
	Name       string   `json:"name"`
	Summary    string   `json:"summary"`
	Homepage   string   `json:"homepage"`
	License    string   `json:"license"`
	Categories []string `json:"categories"`
}

type staticPackagesIndex struct {
	Schema   int             `json:"schema"`
	Registry staticMetadata  `json:"registry"`
	Packages []staticPackage `json:"packages"`
}

type staticVersion struct {
	Version        string `json:"version"`
	Yanked         bool   `json:"yanked"`
	YankReason     string `json:"yank_reason,omitempty"`
	ManifestPath   string `json:"manifest_path"`
	ManifestSHA256 string `json:"manifest_sha256"`
}

type staticVersionsIndex struct {
	Schema   int                   `json:"schema"`
	Package  staticPackageMetadata `json:"package"`
	Versions []staticVersion       `json:"versions"`
}

type staticArtifact struct {
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

type staticInstall struct {
	Bins []string `json:"bins"`
}

type staticManifest struct {
	Schema       int              `json:"schema"`
	Name         string           `json:"name"`
	Version      string           `json:"version"`
	Dependencies []string         `json:"dependencies"`
	Yanked       bool             `json:"yanked"`
	YankReason   string           `json:"yank_reason,omitempty"`
	Artifacts    []staticArtifact `json:"artifacts"`
	Install      staticInstall    `json:"install"`
}

func (r Registry) staticPackage(name string) (Package, error) {
	if err := validatePathPart("package", name); err != nil {
		return Package{}, err
	}
	index, err := r.loadStaticPackagesIndex()
	if err != nil {
		return Package{}, err
	}
	for _, pkg := range index.Packages {
		if pkg.Name == name {
			return packageFromStatic(pkg), nil
		}
	}

	return Package{}, fmt.Errorf("%w: %s", ErrPackageNotFound, name)
}

func (r Registry) staticVersions(name string) ([]string, error) {
	index, err := r.loadStaticVersionsIndex(name)
	if err != nil {
		return nil, err
	}
	versions := make([]string, 0, len(index.Versions))
	for _, version := range index.Versions {
		versions = append(versions, version.Version)
	}
	sortVersions(versions)

	return versions, nil
}

func (r Registry) staticResolveVersion(name, version string) (manifest.Manifest, error) {
	if err := validatePathPart("version", version); err != nil {
		return manifest.Manifest{}, err
	}
	index, err := r.loadStaticVersionsIndex(name)
	if err != nil {
		return manifest.Manifest{}, err
	}
	for _, entry := range index.Versions {
		if entry.Version != version {
			continue
		}
		m, err := r.loadStaticManifest(entry.ManifestPath, entry.ManifestSHA256)
		if err != nil {
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

	return manifest.Manifest{}, fmt.Errorf("%w: %s %s", ErrVersionNotFound, name, version)
}

func (r Registry) staticSearch(query string) ([]SearchResult, error) {
	index, err := r.loadStaticPackagesIndex()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}

		return nil, err
	}

	query = strings.ToLower(query)
	var matches []SearchResult
	for _, pkg := range index.Packages {
		result := SearchResult{
			Name:       pkg.Name,
			Summary:    pkg.Summary,
			Homepage:   pkg.Homepage,
			Categories: cloneStrings(pkg.Categories),
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

func (r Registry) loadStaticPackagesIndex() (staticPackagesIndex, error) {
	var index staticPackagesIndex
	if err := readStaticJSON(filepath.Join(r.Root, StaticIndexDir, "packages.json"), "", &index); err != nil {
		return staticPackagesIndex{}, err
	}
	if index.Schema != CurrentSchema {
		return staticPackagesIndex{}, fmt.Errorf("static packages index schema %d is not supported", index.Schema)
	}
	if err := ValidateMetadata(Metadata{
		Schema:      index.Registry.Schema,
		Name:        index.Registry.Name,
		Description: index.Registry.Description,
	}); err != nil {
		return staticPackagesIndex{}, fmt.Errorf("static registry metadata: %w", err)
	}
	for _, pkg := range index.Packages {
		if err := ValidatePackage(packageFromStatic(pkg)); err != nil {
			return staticPackagesIndex{}, fmt.Errorf("static package %q: %w", pkg.Name, err)
		}
		if err := validateStaticPath(pkg.VersionsPath); err != nil {
			return staticPackagesIndex{}, fmt.Errorf("static package %q versions path: %w", pkg.Name, err)
		}
		if _, err := checksum.NormalizeSHA256(pkg.VersionsSHA256); err != nil {
			return staticPackagesIndex{}, fmt.Errorf("static package %q versions sha256: %w", pkg.Name, err)
		}
	}

	return index, nil
}

func (r Registry) loadStaticVersionsIndex(name string) (staticVersionsIndex, error) {
	if err := validatePathPart("package", name); err != nil {
		return staticVersionsIndex{}, err
	}
	pkg, err := r.findStaticPackage(name)
	if err != nil {
		return staticVersionsIndex{}, err
	}

	var index staticVersionsIndex
	if err := readStaticJSON(filepath.Join(r.Root, StaticIndexDir, filepath.FromSlash(pkg.VersionsPath)), pkg.VersionsSHA256, &index); err != nil {
		return staticVersionsIndex{}, err
	}
	if index.Schema != CurrentSchema {
		return staticVersionsIndex{}, fmt.Errorf("static versions index schema %d is not supported", index.Schema)
	}
	if index.Package.Name != name {
		return staticVersionsIndex{}, fmt.Errorf("static versions package %q does not match %q", index.Package.Name, name)
	}
	for _, version := range index.Versions {
		if err := validatePathPart("version", version.Version); err != nil {
			return staticVersionsIndex{}, err
		}
		if err := validateStaticPath(version.ManifestPath); err != nil {
			return staticVersionsIndex{}, fmt.Errorf("static version %q manifest path: %w", version.Version, err)
		}
		if _, err := checksum.NormalizeSHA256(version.ManifestSHA256); err != nil {
			return staticVersionsIndex{}, fmt.Errorf("static version %q manifest sha256: %w", version.Version, err)
		}
	}

	return index, nil
}

func (r Registry) findStaticPackage(name string) (staticPackage, error) {
	index, err := r.loadStaticPackagesIndex()
	if err != nil {
		return staticPackage{}, err
	}
	for _, pkg := range index.Packages {
		if pkg.Name == name {
			return pkg, nil
		}
	}

	return staticPackage{}, fmt.Errorf("%w: %s", ErrPackageNotFound, name)
}

func (r Registry) loadStaticManifest(relPath, expectedSHA string) (manifest.Manifest, error) {
	if err := validateStaticPath(relPath); err != nil {
		return manifest.Manifest{}, err
	}

	var m staticManifest
	if err := readStaticJSON(filepath.Join(r.Root, StaticIndexDir, filepath.FromSlash(relPath)), expectedSHA, &m); err != nil {
		return manifest.Manifest{}, err
	}
	result := manifestFromStatic(m)
	if err := manifest.Validate(result); err != nil {
		return manifest.Manifest{}, err
	}

	return result, nil
}

func readStaticJSON(path, expectedSHA string, dst any) error {
	data, err := readStaticFile(path, expectedSHA)
	if err != nil {
		return err
	}

	return decodeStaticJSON(path, data, dst)
}

func readStaticSidecar(path string) (string, error) {
	data, err := os.ReadFile(path + ".sha256")
	if err != nil {
		return "", err
	}
	sum, err := checksum.NormalizeSHA256(string(data))
	if err != nil {
		return "", fmt.Errorf("parse checksum sidecar %s.sha256: %w", path, err)
	}

	return sum, nil
}

func validateStaticPath(rel string) error {
	if rel == "" {
		return fmt.Errorf("path is empty")
	}
	if strings.Contains(rel, `\`) {
		return fmt.Errorf("path %q contains backslash path separators", rel)
	}
	local := filepath.FromSlash(rel)
	if !filepath.IsLocal(local) {
		return fmt.Errorf("path %q must be local", rel)
	}

	return nil
}

func packageFromStatic(pkg staticPackage) Package {
	return Package{
		Schema:     CurrentSchema,
		Name:       pkg.Name,
		Summary:    pkg.Summary,
		Homepage:   pkg.Homepage,
		License:    pkg.License,
		Categories: cloneStrings(pkg.Categories),
	}
}

func staticPackageFromPackage(pkg Package) staticPackage {
	return staticPackage{
		Name:       pkg.Name,
		Summary:    pkg.Summary,
		Homepage:   pkg.Homepage,
		License:    pkg.License,
		Categories: cloneStrings(pkg.Categories),
	}
}

func staticPackageMetadataFromPackage(pkg Package) staticPackageMetadata {
	return staticPackageMetadata{
		Name:       pkg.Name,
		Summary:    pkg.Summary,
		Homepage:   pkg.Homepage,
		License:    pkg.License,
		Categories: cloneStrings(pkg.Categories),
	}
}

func staticManifestFromManifest(m manifest.Manifest) staticManifest {
	artifacts := make([]staticArtifact, 0, len(m.Artifacts))
	for _, artifact := range m.Artifacts {
		artifacts = append(artifacts, staticArtifact{
			OS:     artifact.OS,
			Arch:   artifact.Arch,
			URL:    artifact.URL,
			SHA256: artifact.SHA256,
		})
	}

	return staticManifest{
		Schema:       m.Schema,
		Name:         m.Name,
		Version:      m.Version,
		Dependencies: cloneStrings(m.Dependencies),
		Yanked:       m.Yanked,
		YankReason:   m.YankReason,
		Artifacts:    artifacts,
		Install:      staticInstall{Bins: cloneStrings(m.Install.Bins)},
	}
}

func manifestFromStatic(m staticManifest) manifest.Manifest {
	artifacts := make([]manifest.Artifact, 0, len(m.Artifacts))
	for _, artifact := range m.Artifacts {
		artifacts = append(artifacts, manifest.Artifact{
			OS:     artifact.OS,
			Arch:   artifact.Arch,
			URL:    artifact.URL,
			SHA256: artifact.SHA256,
		})
	}

	return manifest.Manifest{
		Schema:       m.Schema,
		Name:         m.Name,
		Version:      m.Version,
		Dependencies: cloneStrings(m.Dependencies),
		Yanked:       m.Yanked,
		YankReason:   m.YankReason,
		Artifacts:    artifacts,
		Install:      manifest.Install{Bins: cloneStrings(m.Install.Bins)},
	}
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}

	return slices.Clone(values)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
