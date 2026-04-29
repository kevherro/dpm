// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kevherro/dpm/internal/config"
	"github.com/kevherro/dpm/internal/link"
)

func TestSaveAndGetRecord(t *testing.T) {
	store, cfg := testStore(t)
	record := testRecord(cfg, "hello", "1.0.0")

	if err := store.Save(record); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, err := store.Get("hello")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	assertRecordEqual(t, got, record)
}

func TestSaveOverwritesSamePackageRecord(t *testing.T) {
	store, cfg := testStore(t)
	first := testRecord(cfg, "hello", "1.0.0")
	next := testRecord(cfg, "hello", "1.1.0")

	if err := store.Save(first); err != nil {
		t.Fatalf("first Save() error = %v", err)
	}
	if err := store.Save(next); err != nil {
		t.Fatalf("second Save() error = %v", err)
	}
	got, err := store.Get("hello")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	assertRecordEqual(t, got, next)
}

func TestListRecordsSortedByName(t *testing.T) {
	store, cfg := testStore(t)
	for _, name := range []string{"zlib", "hello", "alpha"} {
		if err := store.Save(testRecord(cfg, name, "1.0.0")); err != nil {
			t.Fatalf("Save(%s) error = %v", name, err)
		}
	}

	got, err := store.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	var names []string
	for _, record := range got {
		names = append(names, record.Name)
	}
	want := []string{"alpha", "hello", "zlib"}
	if !slices.Equal(names, want) {
		t.Fatalf("List names = %#v, want %#v", names, want)
	}
}

func TestListMissingStateReturnsEmpty(t *testing.T) {
	store, _ := testStore(t)

	got, err := store.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("List() = %#v, want empty", got)
	}
}

func TestRemoveRecord(t *testing.T) {
	store, cfg := testStore(t)
	if err := store.Save(testRecord(cfg, "hello", "1.0.0")); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if err := store.Remove("hello"); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	_, err := store.Get("hello")
	if !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("Get() error = %v, want ErrNotInstalled", err)
	}
}

func TestGetAndRemoveMissingRecord(t *testing.T) {
	store, _ := testStore(t)

	if _, err := store.Get("missing"); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("Get() error = %v, want ErrNotInstalled", err)
	}
	if err := store.Remove("missing"); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("Remove() error = %v, want ErrNotInstalled", err)
	}
}

func TestSaveRejectsInvalidRecords(t *testing.T) {
	store, cfg := testStore(t)
	valid := testRecord(cfg, "hello", "1.0.0")

	tests := []struct {
		name   string
		mutate func(*Record)
	}{
		{name: "empty name", mutate: func(r *Record) { r.Name = "" }},
		{name: "path name", mutate: func(r *Record) { r.Name = "../hello" }},
		{name: "empty version", mutate: func(r *Record) { r.Version = "" }},
		{name: "path version", mutate: func(r *Record) { r.Version = "stable/1.0.0" }},
		{name: "empty source", mutate: func(r *Record) { r.Source = "" }},
		{name: "empty sha", mutate: func(r *Record) { r.SHA256 = "" }},
		{name: "outside prefix", mutate: func(r *Record) { r.Prefix = filepath.Join(t.TempDir(), "outside") }},
		{name: "zero time", mutate: func(r *Record) { r.InstalledAt = time.Time{} }},
		{name: "bad dependency", mutate: func(r *Record) { r.Dependencies = []string{"../dep"} }},
		{name: "bad bin name", mutate: func(r *Record) { r.Bins[0].Name = "bin/hello" }},
		{name: "outside bin source", mutate: func(r *Record) { r.Bins[0].Source = filepath.Join(t.TempDir(), "hello") }},
		{name: "outside bin link", mutate: func(r *Record) { r.Bins[0].Link = filepath.Join(t.TempDir(), "hello") }},
		{name: "nested bin link", mutate: func(r *Record) { r.Bins[0].Link = filepath.Join(cfg.BinDir, "nested", "hello") }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := valid
			record.Bins = slices.Clone(valid.Bins)
			record.Dependencies = slices.Clone(valid.Dependencies)
			tt.mutate(&record)
			if err := store.Save(record); err == nil {
				t.Fatal("Save() error = nil, want error")
			}
		})
	}
}

func TestRecordPathRejectsUnsafeNames(t *testing.T) {
	store, _ := testStore(t)

	for _, name := range []string{"", ".", "..", "../hello", "org/hello", `org\hello`} {
		t.Run(name, func(t *testing.T) {
			if _, err := store.recordPath(name); err == nil {
				t.Fatal("recordPath() error = nil, want error")
			}
		})
	}
}

func TestGetRejectsUnknownJSONFields(t *testing.T) {
	store, cfg := testStore(t)
	record := testRecord(cfg, "hello", "1.0.0")
	path, err := store.recordPath("hello")
	if err != nil {
		t.Fatalf("recordPath() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	contents := strings.TrimSuffix(string(data), "}") + `,"script":"echo no"}`
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if _, err := store.Get("hello"); err == nil {
		t.Fatal("Get() error = nil, want error")
	}
}

func TestGetRejectsRecordNameMismatch(t *testing.T) {
	store, cfg := testStore(t)
	record := testRecord(cfg, "goodbye", "1.0.0")
	path, err := store.recordPath("hello")
	if err != nil {
		t.Fatalf("recordPath() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err = store.Get("hello")
	if err == nil {
		t.Fatal("Get() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "has name") {
		t.Fatalf("Get() error = %q, want name mismatch", err)
	}
}

func TestRemoveDoesNotDeleteOutsideStateRoot(t *testing.T) {
	store, _ := testStore(t)
	outside := filepath.Join(t.TempDir(), "hello.json")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := store.Remove("../hello"); err == nil {
		t.Fatal("Remove() error = nil, want error")
	}
	assertFile(t, outside, "outside")
}

func TestSaveAndGetRegistrySnapshot(t *testing.T) {
	store, _ := testStore(t)
	snapshot := RegistrySnapshot{
		Version:    2,
		SHA256:     strings.Repeat("a", 64),
		KeyID:      strings.Repeat("b", 64),
		VerifiedAt: time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC),
	}

	if err := store.SaveRegistrySnapshot(snapshot); err != nil {
		t.Fatalf("SaveRegistrySnapshot() error = %v", err)
	}
	got, err := store.RegistrySnapshot()
	if err != nil {
		t.Fatalf("RegistrySnapshot() error = %v", err)
	}
	if got != snapshot {
		t.Fatalf("RegistrySnapshot() = %#v, want %#v", got, snapshot)
	}
}

func TestRegistrySnapshotMissing(t *testing.T) {
	store, _ := testStore(t)

	_, err := store.RegistrySnapshot()
	if !errors.Is(err, ErrRegistrySnapshotNotFound) {
		t.Fatalf("RegistrySnapshot() error = %v, want ErrRegistrySnapshotNotFound", err)
	}
}

func TestSaveRegistrySnapshotRejectsInvalidSnapshot(t *testing.T) {
	store, _ := testStore(t)
	valid := RegistrySnapshot{
		Version:    2,
		SHA256:     strings.Repeat("a", 64),
		KeyID:      strings.Repeat("b", 64),
		VerifiedAt: time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC),
	}
	tests := []struct {
		name   string
		mutate func(*RegistrySnapshot)
	}{
		{name: "zero version", mutate: func(s *RegistrySnapshot) { s.Version = 0 }},
		{name: "empty sha", mutate: func(s *RegistrySnapshot) { s.SHA256 = "" }},
		{name: "short sha", mutate: func(s *RegistrySnapshot) { s.SHA256 = "abc" }},
		{name: "empty key", mutate: func(s *RegistrySnapshot) { s.KeyID = "" }},
		{name: "zero verified", mutate: func(s *RegistrySnapshot) { s.VerifiedAt = time.Time{} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := valid
			tt.mutate(&snapshot)
			if err := store.SaveRegistrySnapshot(snapshot); err == nil {
				t.Fatal("SaveRegistrySnapshot() error = nil, want error")
			}
		})
	}
}

func testStore(t *testing.T) (Store, config.Config) {
	t.Helper()

	cfg, err := config.FromRoot(filepath.Join(t.TempDir(), "dpm-root"))
	if err != nil {
		t.Fatalf("FromRoot() error = %v", err)
	}

	return New(cfg), cfg
}

func testRecord(cfg config.Config, name, version string) Record {
	prefix := filepath.Join(cfg.PkgsDir, name, version)
	return Record{
		Name:    name,
		Version: version,
		Source:  "file:///tmp/" + name + ".tar.gz",
		SHA256:  strings.Repeat("a", 64),
		Prefix:  prefix,
		Bins: []link.BinLink{
			{
				Name:   name,
				Source: filepath.Join(prefix, "bin", name),
				Link:   filepath.Join(cfg.BinDir, name),
			},
		},
		Dependencies: []string{"lib" + name},
		InstalledAt:  time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC),
	}
}

func assertRecordEqual(t *testing.T, got, want Record) {
	t.Helper()

	if got.Name != want.Name ||
		got.Version != want.Version ||
		got.Source != want.Source ||
		got.SHA256 != want.SHA256 ||
		got.Prefix != want.Prefix ||
		!got.InstalledAt.Equal(want.InstalledAt) ||
		!slices.Equal(got.Dependencies, want.Dependencies) ||
		!slices.Equal(got.Bins, want.Bins) {
		t.Fatalf("record = %#v, want %#v", got, want)
	}
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	if string(got) != want {
		t.Fatalf("ReadFile(%q) = %q, want %q", path, got, want)
	}
}
