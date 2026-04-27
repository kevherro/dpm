// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package manifest

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
	// CurrentSchema is the supported version manifest schema.
	CurrentSchema = 1
)

// Manifest is a declarative dpm package manifest.
type Manifest struct {
	Schema       int
	Name         string
	Version      string
	Dependencies []string
	Yanked       bool
	YankReason   string
	Artifacts    []Artifact
	Install      Install
}

// Artifact is a pinned binary artifact for one platform.
type Artifact struct {
	OS     string
	Arch   string
	URL    string
	SHA256 string
}

// Install declares files dpm should expose after extraction.
type Install struct {
	Bins []string
}

type section int

const (
	rootSection section = iota
	artifactSection
	installSection
)

// Load reads and parses a dpm.toml manifest from path.
func Load(path string) (Manifest, error) {
	f, err := os.Open(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("open manifest %s: %w", path, err)
	}
	defer f.Close()

	m, err := Parse(f)
	if err != nil {
		return Manifest{}, fmt.Errorf("parse manifest %s: %w", path, err)
	}

	return m, nil
}

// Parse parses and validates a dpm.toml manifest.
func Parse(r io.Reader) (Manifest, error) {
	var m Manifest
	seenRoot := map[string]bool{}
	seenInstall := map[string]bool{}
	var seenArtifact map[string]bool
	current := rootSection

	scanner := bufio.NewScanner(r)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(stripComment(scanner.Text()))
		if line == "" {
			continue
		}

		switch line {
		case "[[artifacts]]":
			current = artifactSection
			m.Artifacts = append(m.Artifacts, Artifact{})
			seenArtifact = map[string]bool{}
			continue
		case "[install]":
			current = installSection
			continue
		}

		if strings.HasPrefix(line, "[") {
			return Manifest{}, lineError(lineNo, "unknown section %q", line)
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return Manifest{}, lineError(lineNo, "expected key = value")
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			return Manifest{}, lineError(lineNo, "expected key = value")
		}

		switch current {
		case rootSection:
			if err := rejectDuplicate(lineNo, seenRoot, key); err != nil {
				return Manifest{}, err
			}
			if err := parseRootKey(&m, key, value); err != nil {
				return Manifest{}, lineError(lineNo, "%s", err)
			}
		case artifactSection:
			if len(m.Artifacts) == 0 {
				return Manifest{}, lineError(lineNo, "artifact key before [[artifacts]]")
			}
			if err := rejectDuplicate(lineNo, seenArtifact, key); err != nil {
				return Manifest{}, err
			}
			if err := parseArtifactKey(&m.Artifacts[len(m.Artifacts)-1], key, value); err != nil {
				return Manifest{}, lineError(lineNo, "%s", err)
			}
		case installSection:
			if err := rejectDuplicate(lineNo, seenInstall, key); err != nil {
				return Manifest{}, err
			}
			if err := parseInstallKey(&m.Install, key, value); err != nil {
				return Manifest{}, lineError(lineNo, "%s", err)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return Manifest{}, fmt.Errorf("read manifest: %w", err)
	}

	if err := Validate(m); err != nil {
		return Manifest{}, err
	}

	return m, nil
}

// Validate checks required manifest fields and rejects unsafe declarations.
func Validate(m Manifest) error {
	if m.Schema == 0 {
		return fmt.Errorf("manifest schema is required")
	}
	if m.Schema != CurrentSchema {
		return fmt.Errorf("manifest schema %d is not supported", m.Schema)
	}
	if m.Name == "" {
		return fmt.Errorf("manifest name is required")
	}
	if strings.ContainsAny(m.Name, `/\`) {
		return fmt.Errorf("manifest name %q must not contain path separators", m.Name)
	}
	if m.Version == "" {
		return fmt.Errorf("manifest version is required")
	}
	if strings.ContainsAny(m.Version, `/\`) {
		return fmt.Errorf("manifest version %q must not contain path separators", m.Version)
	}
	if len(m.Artifacts) == 0 {
		return fmt.Errorf("at least one artifact is required")
	}
	for i, artifact := range m.Artifacts {
		if artifact.OS == "" {
			return fmt.Errorf("artifact %d os is required", i+1)
		}
		if artifact.Arch == "" {
			return fmt.Errorf("artifact %d arch is required", i+1)
		}
		if artifact.URL == "" {
			return fmt.Errorf("artifact %d url is required", i+1)
		}
		if artifact.SHA256 == "" {
			return fmt.Errorf("artifact %d sha256 is required", i+1)
		}
	}
	if len(m.Install.Bins) == 0 {
		return fmt.Errorf("install bins must be declared")
	}
	for _, bin := range m.Install.Bins {
		if bin == "" {
			return fmt.Errorf("install bin path is empty")
		}
		if !filepath.IsLocal(bin) {
			return fmt.Errorf("install bin path %q must be relative and stay inside the package prefix", bin)
		}
	}

	return nil
}

func parseRootKey(m *Manifest, key, value string) error {
	switch key {
	case "schema":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("schema must be an integer: %w", err)
		}
		m.Schema = n
	case "name":
		s, err := parseString(value)
		if err != nil {
			return err
		}
		m.Name = s
	case "version":
		s, err := parseString(value)
		if err != nil {
			return err
		}
		m.Version = s
	case "dependencies":
		values, err := parseStringArray(value)
		if err != nil {
			return err
		}
		m.Dependencies = values
	case "yanked":
		b, err := parseBool(value)
		if err != nil {
			return err
		}
		m.Yanked = b
	case "yank_reason":
		s, err := parseString(value)
		if err != nil {
			return err
		}
		m.YankReason = s
	default:
		return fmt.Errorf("unknown root key %q", key)
	}

	return nil
}

func parseArtifactKey(a *Artifact, key, value string) error {
	s, err := parseString(value)
	if err != nil {
		return err
	}

	switch key {
	case "os":
		a.OS = s
	case "arch":
		a.Arch = s
	case "url":
		a.URL = s
	case "sha256":
		a.SHA256 = s
	default:
		return fmt.Errorf("unknown artifact key %q", key)
	}

	return nil
}

func parseInstallKey(i *Install, key, value string) error {
	switch key {
	case "bins":
		values, err := parseStringArray(value)
		if err != nil {
			return err
		}
		i.Bins = values
	default:
		return fmt.Errorf("unknown install key %q", key)
	}

	return nil
}

func parseString(value string) (string, error) {
	if !strings.HasPrefix(value, `"`) {
		return "", fmt.Errorf("expected quoted string")
	}
	s, err := strconv.Unquote(value)
	if err != nil {
		return "", fmt.Errorf("parse quoted string: %w", err)
	}

	return s, nil
}

func parseStringArray(value string) ([]string, error) {
	if !strings.HasPrefix(value, "[") || !strings.HasSuffix(value, "]") {
		return nil, fmt.Errorf("expected string array")
	}

	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(value, "["), "]"))
	if inner == "" {
		return []string{}, nil
	}

	parts := splitArray(inner)
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		s, err := parseString(strings.TrimSpace(part))
		if err != nil {
			return nil, err
		}
		values = append(values, s)
	}

	return values, nil
}

func parseBool(value string) (bool, error) {
	switch value {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("expected boolean true or false")
	}
}

func splitArray(value string) []string {
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

func stripComment(line string) string {
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

func rejectDuplicate(lineNo int, seen map[string]bool, key string) error {
	if seen[key] {
		return lineError(lineNo, "duplicate key %q", key)
	}
	seen[key] = true

	return nil
}

func lineError(lineNo int, format string, args ...any) error {
	return fmt.Errorf("line %d: %s", lineNo, fmt.Sprintf(format, args...))
}
