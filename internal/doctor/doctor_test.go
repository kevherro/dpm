// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package doctor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kevherro/dpm/internal/config"
	"github.com/kevherro/dpm/internal/link"
	"github.com/kevherro/dpm/internal/operationlock"
	"github.com/kevherro/dpm/internal/state"
)

func TestAuditCorruptionMatrixAggregatesWithoutCreatingLock(t *testing.T) {
	cfg, err := config.FromRoot(filepath.Join(t.TempDir(), "dpm"))
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	prefix := filepath.Join(cfg.PkgsDir, "app", "1.0.0")
	source := filepath.Join(prefix, "bin", "app")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("not executable"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(cfg.BinDir, "app")
	if err := os.Symlink(filepath.Join(prefix, "wrong"), linkPath); err != nil {
		t.Fatal(err)
	}
	record := state.Record{Schema: state.CurrentSchema, Name: "app", Version: "1.0.0", Source: "file:///app.tgz", SHA256: strings.Repeat("a", 64), Prefix: prefix, Bins: []link.BinLink{{Name: "app", Source: source, Link: linkPath}}, Dependencies: []string{"missing"}, InstalledAt: time.Now()}
	if err := state.New(cfg).Save(record); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(cfg.PkgsDir, "orphan", "2.0.0")
	if err := os.MkdirAll(orphan, 0o755); err != nil {
		t.Fatal(err)
	}
	staging := filepath.Join(cfg.CacheDir, ".install-app-stale")
	if err := os.Mkdir(staging, 0o755); err != nil {
		t.Fatal(err)
	}

	report := Audit(context.Background(), cfg)
	joined := strings.Join(report.Findings, "\n")
	for _, want := range []string{"missing dependency missing required by app 1.0.0", "invalid executable source for app 1.0.0", "missing or retargeted owned link for app 1.0.0", "orphan package prefix " + orphan, "stale staging " + staging} {
		if !strings.Contains(joined, want) {
			t.Errorf("findings = %q, want %q", joined, want)
		}
	}
	if _, err := os.Lstat(filepath.Join(cfg.Root, operationlock.FileName)); !os.IsNotExist(err) {
		t.Fatalf("doctor created operation lock: %v", err)
	}
}

func TestAuditReportsInvalidStateAndLayoutTogether(t *testing.T) {
	cfg, err := config.FromRoot(filepath.Join(t.TempDir(), "dpm"))
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(cfg.StateDir, "installed"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.StateDir, "installed", "bad.json"), []byte(`{"schema":99}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(cfg.DownloadsDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.DownloadsDir, []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}
	report := Audit(context.Background(), cfg)
	joined := strings.Join(report.Findings, "\n")
	for _, want := range []string{"not-dir " + cfg.DownloadsDir, "invalid state record"} {
		if !strings.Contains(joined, want) {
			t.Errorf("findings = %q, want %q", joined, want)
		}
	}
}

func TestAuditRejectsSymlinkedStateWithoutReadingIt(t *testing.T) {
	cfg, err := config.FromRoot(filepath.Join(t.TempDir(), "dpm"))
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(cfg.StateDir, "installed")
	if err := os.Mkdir(installed, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, []byte(`{"schema":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(installed, "outside.json")
	if err := os.Symlink(outside, statePath); err != nil {
		t.Fatal(err)
	}

	report := Audit(context.Background(), cfg)
	if joined := strings.Join(report.Findings, "\n"); !strings.Contains(joined, "unexpected state entry "+statePath) {
		t.Fatalf("findings = %q, want symlinked state finding", joined)
	}
}
