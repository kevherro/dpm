// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/kevherro/dpm/internal/checksum"
	"github.com/kevherro/dpm/internal/config"
	"github.com/kevherro/dpm/internal/link"
)

var (
	// ErrNotInstalled means a package has no installed state record.
	ErrNotInstalled = errors.New("package not installed")
	// ErrRegistrySnapshotNotFound means no verified registry snapshot is recorded.
	ErrRegistrySnapshotNotFound = errors.New("registry snapshot not found")
)

// Record describes an installed package.
type Record struct {
	Name         string         `json:"name"`
	Version      string         `json:"version"`
	Source       string         `json:"source"`
	SHA256       string         `json:"sha256"`
	Prefix       string         `json:"prefix"`
	Bins         []link.BinLink `json:"bins"`
	Dependencies []string       `json:"dependencies"`
	InstalledAt  time.Time      `json:"installed_at"`
}

// RegistrySnapshot records the latest accepted signed registry snapshot.
type RegistrySnapshot struct {
	Version    int       `json:"version"`
	SHA256     string    `json:"sha256"`
	KeyID      string    `json:"key_id"`
	VerifiedAt time.Time `json:"verified_at"`
}

// Store reads and writes installed package state.
type Store struct {
	cfg config.Config
}

// New returns a state store for cfg.
func New(cfg config.Config) Store {
	return Store{cfg: cfg}
}

// Save writes record to state.
func (s Store) Save(record Record) error {
	if err := s.validateRecord(record); err != nil {
		return err
	}

	dir, err := s.installedDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create installed state directory %s: %w", dir, err)
	}

	path, err := s.recordPath(record.Name)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+record.Name+"-*.json")
	if err != nil {
		return fmt.Errorf("create temporary state record for %s: %w", record.Name, err)
	}
	tmpPath := tmp.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	enc := json.NewEncoder(tmp)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(record); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("encode state record for %s: %w", record.Name, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary state record %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("write state record %s: %w", path, err)
	}
	removeTmp = false

	return nil
}

// Get reads one installed package record.
func (s Store) Get(name string) (Record, error) {
	path, err := s.recordPath(name)
	if err != nil {
		return Record{}, err
	}

	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return Record{}, fmt.Errorf("%w: %s", ErrNotInstalled, name)
	}
	if err != nil {
		return Record{}, fmt.Errorf("open state record %s: %w", path, err)
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	var record Record
	if err := dec.Decode(&record); err != nil {
		return Record{}, fmt.Errorf("decode state record %s: %w", path, err)
	}
	if err := s.validateRecord(record); err != nil {
		return Record{}, fmt.Errorf("invalid state record %s: %w", path, err)
	}
	if record.Name != name {
		return Record{}, fmt.Errorf("state record %s has name %q, want %q", path, record.Name, name)
	}

	return record, nil
}

// List returns all installed package records sorted by name.
func (s Store) List() ([]Record, error) {
	dir, err := s.installedDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read installed state directory %s: %w", dir, err)
	}

	var records []Record
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".json")
		record, err := s.Get(name)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	slices.SortFunc(records, func(a, b Record) int {
		return strings.Compare(a.Name, b.Name)
	})

	return records, nil
}

// Remove deletes one installed package record.
func (s Store) Remove(name string) error {
	path, err := s.recordPath(name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: %s", ErrNotInstalled, name)
	} else if err != nil {
		return fmt.Errorf("remove state record %s: %w", path, err)
	}

	return nil
}

// SaveRegistrySnapshot writes the latest accepted signed registry snapshot.
func (s Store) SaveRegistrySnapshot(snapshot RegistrySnapshot) error {
	if err := s.validateRegistrySnapshot(snapshot); err != nil {
		return err
	}
	if err := s.cfg.RequireInsideRoot(s.cfg.StateDir); err != nil {
		return err
	}
	if err := os.MkdirAll(s.cfg.StateDir, 0o755); err != nil {
		return fmt.Errorf("create state directory %s: %w", s.cfg.StateDir, err)
	}
	path, err := s.registrySnapshotPath()
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.cfg.StateDir, ".registry-snapshot-*.json")
	if err != nil {
		return fmt.Errorf("create temporary registry snapshot state: %w", err)
	}
	tmpPath := tmp.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	enc := json.NewEncoder(tmp)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(snapshot); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("encode registry snapshot state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary registry snapshot state %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("write registry snapshot state %s: %w", path, err)
	}
	removeTmp = false

	return nil
}

// RegistrySnapshot reads the latest accepted signed registry snapshot.
func (s Store) RegistrySnapshot() (RegistrySnapshot, error) {
	path, err := s.registrySnapshotPath()
	if err != nil {
		return RegistrySnapshot{}, err
	}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return RegistrySnapshot{}, ErrRegistrySnapshotNotFound
	}
	if err != nil {
		return RegistrySnapshot{}, fmt.Errorf("open registry snapshot state %s: %w", path, err)
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	var snapshot RegistrySnapshot
	if err := dec.Decode(&snapshot); err != nil {
		return RegistrySnapshot{}, fmt.Errorf("decode registry snapshot state %s: %w", path, err)
	}
	if err := s.validateRegistrySnapshot(snapshot); err != nil {
		return RegistrySnapshot{}, fmt.Errorf("invalid registry snapshot state %s: %w", path, err)
	}

	return snapshot, nil
}

func (s Store) installedDir() (string, error) {
	dir := filepath.Join(s.cfg.StateDir, "installed")
	if err := s.cfg.RequireInsideRoot(dir); err != nil {
		return "", err
	}

	return dir, nil
}

func (s Store) registrySnapshotPath() (string, error) {
	path := filepath.Join(s.cfg.StateDir, "registry_snapshot.json")
	if err := s.cfg.RequireInsideRoot(path); err != nil {
		return "", err
	}

	return path, nil
}

func (s Store) recordPath(name string) (string, error) {
	if err := validateName(name); err != nil {
		return "", err
	}
	dir, err := s.installedDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, name+".json")
	if err := s.cfg.RequireInsideRoot(path); err != nil {
		return "", err
	}

	return path, nil
}

func (s Store) validateRecord(record Record) error {
	if err := validateName(record.Name); err != nil {
		return err
	}
	if record.Version == "" {
		return fmt.Errorf("state record version is required")
	}
	if strings.ContainsAny(record.Version, `/\`) {
		return fmt.Errorf("state record version %q must not contain path separators", record.Version)
	}
	if record.Source == "" {
		return fmt.Errorf("state record source is required")
	}
	if record.SHA256 == "" {
		return fmt.Errorf("state record sha256 is required")
	}
	if record.Prefix == "" {
		return fmt.Errorf("state record prefix is required")
	}
	if err := s.cfg.RequireInsideRoot(record.Prefix); err != nil {
		return err
	}
	if record.InstalledAt.IsZero() {
		return fmt.Errorf("state record installed_at is required")
	}
	for _, bin := range record.Bins {
		if bin.Name == "" {
			return fmt.Errorf("state record bin name is required")
		}
		if strings.ContainsAny(bin.Name, `/\`) {
			return fmt.Errorf("state record bin name %q must not contain path separators", bin.Name)
		}
		if bin.Source == "" {
			return fmt.Errorf("state record bin source is required")
		}
		if err := s.cfg.RequireInsideRoot(bin.Source); err != nil {
			return err
		}
		if bin.Link == "" {
			return fmt.Errorf("state record bin link is required")
		}
		if err := s.cfg.RequireInsideRoot(bin.Link); err != nil {
			return err
		}
		if filepath.Dir(bin.Link) != s.cfg.BinDir {
			return fmt.Errorf("state record bin link %s is not directly in %s", bin.Link, s.cfg.BinDir)
		}
	}
	for _, dep := range record.Dependencies {
		if err := validateName(dep); err != nil {
			return fmt.Errorf("invalid dependency %q: %w", dep, err)
		}
	}

	return nil
}

func (s Store) validateRegistrySnapshot(snapshot RegistrySnapshot) error {
	if snapshot.Version <= 0 {
		return fmt.Errorf("registry snapshot version must be greater than zero")
	}
	if snapshot.SHA256 == "" {
		return fmt.Errorf("registry snapshot sha256 is required")
	}
	if _, err := checksum.NormalizeSHA256(snapshot.SHA256); err != nil {
		return fmt.Errorf("registry snapshot sha256: %w", err)
	}
	if snapshot.KeyID == "" {
		return fmt.Errorf("registry snapshot key_id is required")
	}
	if snapshot.VerifiedAt.IsZero() {
		return fmt.Errorf("registry snapshot verified_at is required")
	}

	return nil
}

func validateName(name string) error {
	if name == "" {
		return fmt.Errorf("state record name is required")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("state record name %q is not allowed", name)
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("state record name %q must not contain path separators", name)
	}
	if !filepath.IsLocal(name) {
		return fmt.Errorf("state record name %q must be local", name)
	}

	return nil
}
