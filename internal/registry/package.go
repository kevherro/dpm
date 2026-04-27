// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package registry

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	// PackageFile is the metadata filename for one package.
	PackageFile = "package.toml"
)

// Package describes searchable package metadata.
type Package struct {
	Schema     int
	Name       string
	Summary    string
	Homepage   string
	License    string
	Categories []string
}

// Package reads package.toml for name.
func (r Registry) Package(name string) (Package, error) {
	dir, err := r.PackageDir(name)
	if err != nil {
		return Package{}, err
	}
	pkg, err := LoadPackage(filepath.Join(dir, PackageFile))
	if err != nil {
		return Package{}, err
	}
	if pkg.Name != name {
		return Package{}, fmt.Errorf("package metadata name %q does not match package directory %q", pkg.Name, name)
	}

	return pkg, nil
}

// LoadPackage reads and parses package.toml from path.
func LoadPackage(path string) (Package, error) {
	f, err := os.Open(path)
	if err != nil {
		return Package{}, fmt.Errorf("open package metadata %s: %w", path, err)
	}
	defer f.Close()

	pkg, err := ParsePackage(f)
	if err != nil {
		return Package{}, fmt.Errorf("parse package metadata %s: %w", path, err)
	}

	return pkg, nil
}

// ParsePackage parses and validates package.toml.
func ParsePackage(r io.Reader) (Package, error) {
	var pkg Package
	seen := map[string]bool{}
	scanner := bufio.NewScanner(r)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(stripMetadataComment(scanner.Text()))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			return Package{}, metadataLineError(lineNo, "sections are not allowed in package metadata")
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return Package{}, metadataLineError(lineNo, "expected key = value")
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			return Package{}, metadataLineError(lineNo, "expected key = value")
		}
		if seen[key] {
			return Package{}, metadataLineError(lineNo, "duplicate key %q", key)
		}
		seen[key] = true

		if err := parsePackageKey(&pkg, key, value); err != nil {
			return Package{}, metadataLineError(lineNo, "%s", err)
		}
	}
	if err := scanner.Err(); err != nil {
		return Package{}, fmt.Errorf("read package metadata: %w", err)
	}
	if err := ValidatePackage(pkg); err != nil {
		return Package{}, err
	}

	return pkg, nil
}

// ValidatePackage validates package.toml fields.
func ValidatePackage(pkg Package) error {
	if pkg.Schema == 0 {
		return fmt.Errorf("package schema is required")
	}
	if pkg.Schema != CurrentSchema {
		return fmt.Errorf("package schema %d is not supported", pkg.Schema)
	}
	if err := validatePathPart("package", pkg.Name); err != nil {
		return err
	}
	if pkg.Summary == "" {
		return fmt.Errorf("package summary is required")
	}
	if pkg.Homepage == "" {
		return fmt.Errorf("package homepage is required")
	}
	if pkg.License == "" {
		return fmt.Errorf("package license is required")
	}
	for _, category := range pkg.Categories {
		if err := validateCategory(category); err != nil {
			return err
		}
	}

	return nil
}

func parsePackageKey(pkg *Package, key, value string) error {
	switch key {
	case "schema":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("schema must be an integer: %w", err)
		}
		pkg.Schema = n
	case "name":
		s, err := parseMetadataString(value)
		if err != nil {
			return err
		}
		pkg.Name = s
	case "summary":
		s, err := parseMetadataString(value)
		if err != nil {
			return err
		}
		pkg.Summary = s
	case "homepage":
		s, err := parseMetadataString(value)
		if err != nil {
			return err
		}
		pkg.Homepage = s
	case "license":
		s, err := parseMetadataString(value)
		if err != nil {
			return err
		}
		pkg.License = s
	case "categories":
		values, err := parseMetadataStringArray(value)
		if err != nil {
			return err
		}
		pkg.Categories = values
	default:
		return fmt.Errorf("unknown package metadata key %q", key)
	}

	return nil
}

func parseMetadataStringArray(value string) ([]string, error) {
	if !strings.HasPrefix(value, "[") || !strings.HasSuffix(value, "]") {
		return nil, fmt.Errorf("expected string array")
	}

	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(value, "["), "]"))
	if inner == "" {
		return []string{}, nil
	}

	parts := splitMetadataArray(inner)
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		s, err := parseMetadataString(strings.TrimSpace(part))
		if err != nil {
			return nil, err
		}
		values = append(values, s)
	}

	return values, nil
}

func splitMetadataArray(value string) []string {
	var parts []string
	start := 0
	inString := false
	escaped := false
	for i, r := range value {
		if escaped {
			escaped = false
			continue
		}
		switch r {
		case '\\':
			if inString {
				escaped = true
			}
		case '"':
			inString = !inString
		case ',':
			if !inString {
				parts = append(parts, value[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, value[start:])

	return parts
}

func validateCategory(category string) error {
	if category == "" {
		return fmt.Errorf("package category is empty")
	}
	if strings.ContainsAny(category, `/\`) {
		return fmt.Errorf("package category %q must not contain path separators", category)
	}
	if !filepath.IsLocal(category) {
		return fmt.Errorf("package category %q must be local", category)
	}

	return nil
}
