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
	// CurrentSchema is the supported registry metadata schema version.
	CurrentSchema = 1
	// MetadataFile is the root metadata filename for a dpm registry.
	MetadataFile = "registry.toml"
)

// Metadata describes a dpm registry root.
type Metadata struct {
	Schema      int
	Name        string
	Description string
}

// Metadata reads registry.toml from the registry root.
func (r Registry) Metadata() (Metadata, error) {
	return LoadMetadata(filepath.Join(r.Root, MetadataFile))
}

// LoadMetadata reads and parses registry.toml from path.
func LoadMetadata(path string) (Metadata, error) {
	f, err := os.Open(path)
	if err != nil {
		return Metadata{}, fmt.Errorf("open registry metadata %s: %w", path, err)
	}
	defer f.Close()

	m, err := ParseMetadata(f)
	if err != nil {
		return Metadata{}, fmt.Errorf("parse registry metadata %s: %w", path, err)
	}

	return m, nil
}

// ParseMetadata parses and validates registry.toml.
func ParseMetadata(r io.Reader) (Metadata, error) {
	var m Metadata
	seen := map[string]bool{}
	scanner := bufio.NewScanner(r)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(stripMetadataComment(scanner.Text()))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			return Metadata{}, metadataLineError(lineNo, "sections are not allowed in registry metadata")
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return Metadata{}, metadataLineError(lineNo, "expected key = value")
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			return Metadata{}, metadataLineError(lineNo, "expected key = value")
		}
		if seen[key] {
			return Metadata{}, metadataLineError(lineNo, "duplicate key %q", key)
		}
		seen[key] = true

		if err := parseMetadataKey(&m, key, value); err != nil {
			return Metadata{}, metadataLineError(lineNo, "%s", err)
		}
	}
	if err := scanner.Err(); err != nil {
		return Metadata{}, fmt.Errorf("read registry metadata: %w", err)
	}
	if err := ValidateMetadata(m); err != nil {
		return Metadata{}, err
	}

	return m, nil
}

// ValidateMetadata validates registry.toml fields.
func ValidateMetadata(m Metadata) error {
	if m.Schema == 0 {
		return fmt.Errorf("registry schema is required")
	}
	if m.Schema != CurrentSchema {
		return fmt.Errorf("registry schema %d is not supported", m.Schema)
	}
	if m.Name == "" {
		return fmt.Errorf("registry name is required")
	}
	if strings.ContainsAny(m.Name, `/\`) {
		return fmt.Errorf("registry name %q must not contain path separators", m.Name)
	}

	return nil
}

func parseMetadataKey(m *Metadata, key, value string) error {
	switch key {
	case "schema":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("schema must be an integer: %w", err)
		}
		m.Schema = n
	case "name":
		s, err := parseMetadataString(value)
		if err != nil {
			return err
		}
		m.Name = s
	case "description":
		s, err := parseMetadataString(value)
		if err != nil {
			return err
		}
		m.Description = s
	default:
		return fmt.Errorf("unknown registry metadata key %q", key)
	}

	return nil
}

func parseMetadataString(value string) (string, error) {
	if !strings.HasPrefix(value, `"`) {
		return "", fmt.Errorf("expected quoted string")
	}
	s, err := strconv.Unquote(value)
	if err != nil {
		return "", fmt.Errorf("parse quoted string: %w", err)
	}

	return s, nil
}

func stripMetadataComment(line string) string {
	inString := false
	escaped := false
	for i, r := range line {
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
		case '#':
			if !inString {
				return line[:i]
			}
		}
	}

	return line
}

func metadataLineError(lineNo int, format string, args ...any) error {
	return fmt.Errorf("line %d: %s", lineNo, fmt.Sprintf(format, args...))
}
