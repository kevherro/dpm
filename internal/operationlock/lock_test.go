// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package operationlock

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAcquireCreatesAndReleasesLock(t *testing.T) {
	root := t.TempDir()
	lock, err := Acquire(root, Exclusive)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	info, err := os.Stat(filepath.Join(root, FileName))
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("lock mode = %v, want regular 0600", info.Mode())
	}
}

func TestAcquireRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("unchanged"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, FileName)); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	if _, err := Acquire(root, Exclusive); err == nil {
		t.Fatal("Acquire() error = nil, want symlink rejection")
	}
	data, err := os.ReadFile(outside)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "unchanged" {
		t.Fatalf("outside file = %q, want unchanged", data)
	}
}

func TestAcquireExistingDoesNotCreateMissingLock(t *testing.T) {
	root := t.TempDir()
	lock, err := AcquireExisting(root, Shared)
	if err != nil {
		t.Fatalf("AcquireExisting() error = %v", err)
	}
	if lock != nil {
		t.Fatal("AcquireExisting() lock is non-nil, want nil")
	}
	if _, err := os.Lstat(filepath.Join(root, FileName)); !os.IsNotExist(err) {
		t.Fatalf("Lstat() error = %v, want missing lock", err)
	}
}
