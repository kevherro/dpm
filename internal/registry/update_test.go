// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package registry

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateClonesMissingRegistry(t *testing.T) {
	source := newGitRegistry(t)
	root := filepath.Join(t.TempDir(), "dpm-root", "registry")

	got, err := Update(context.Background(), UpdateOptions{
		Root: root,
		URL:  fileURL(source),
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	if got.Action != UpdateCloned || got.Root != root || got.Revision == "" {
		t.Fatalf("Update() = %#v, want cloned result", got)
	}
	assertFileExists(t, filepath.Join(root, MetadataFile))
	assertFileExists(t, filepath.Join(root, ".git"))
}

func TestUpdateClonesEmptyRegistryDirectory(t *testing.T) {
	source := newGitRegistry(t)
	root := filepath.Join(t.TempDir(), "dpm-root", "registry")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	got, err := Update(context.Background(), UpdateOptions{
		Root: root,
		URL:  fileURL(source),
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	if got.Action != UpdateCloned {
		t.Fatalf("Action = %q, want cloned", got.Action)
	}
	assertFileExists(t, filepath.Join(root, MetadataFile))
}

func TestUpdatePullsExistingRegistry(t *testing.T) {
	source := newGitRegistry(t)
	root := filepath.Join(t.TempDir(), "dpm-root", "registry")
	if _, err := Update(context.Background(), UpdateOptions{Root: root, URL: fileURL(source)}); err != nil {
		t.Fatalf("first Update() error = %v", err)
	}
	writeFile(t, filepath.Join(source, "packages", "hello", "package.toml"), `schema = 1
name = "hello"
summary = "Hello"
homepage = "https://example.com/hello"
license = "MIT"
`)
	git(t, source, "add", ".")
	git(t, source, "-c", "user.name=dpm-test", "-c", "user.email=dpm@example.invalid", "commit", "--no-gpg-sign", "-m", "add hello")

	got, err := Update(context.Background(), UpdateOptions{
		Root: root,
		URL:  fileURL(source),
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	if got.Action != UpdatePulled || got.Revision == "" {
		t.Fatalf("Update() = %#v, want pulled result", got)
	}
	assertFileExists(t, filepath.Join(root, "packages", "hello", "package.toml"))
}

func TestUpdateRejectsNonGitRegistryDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "registry")
	writeFile(t, filepath.Join(root, "registry.toml"), "not git\n")

	_, err := Update(context.Background(), UpdateOptions{
		Root: root,
		URL:  "file:///tmp/registry",
	})
	if err == nil {
		t.Fatal("Update() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "not a git checkout") {
		t.Fatalf("Update() error = %q, want git checkout error", err)
	}
}

func TestUpdateRejectsDirtyRegistryCheckout(t *testing.T) {
	source := newGitRegistry(t)
	root := filepath.Join(t.TempDir(), "dpm-root", "registry")
	if _, err := Update(context.Background(), UpdateOptions{Root: root, URL: fileURL(source)}); err != nil {
		t.Fatalf("first Update() error = %v", err)
	}
	writeFile(t, filepath.Join(root, "dirty.txt"), "dirty\n")

	_, err := Update(context.Background(), UpdateOptions{
		Root: root,
		URL:  fileURL(source),
	})
	if err == nil {
		t.Fatal("Update() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "uncommitted changes") {
		t.Fatalf("Update() error = %q, want dirty checkout error", err)
	}
}

func newGitRegistry(t *testing.T) string {
	t.Helper()
	requireGit(t)

	root := filepath.Join(t.TempDir(), "source-registry")
	if err := os.MkdirAll(filepath.Join(root, "packages"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	writeFile(t, filepath.Join(root, MetadataFile), `schema = 1
name = "dpm-core"
description = "Test registry"
`)
	git(t, root, "init")
	git(t, root, "add", ".")
	git(t, root, "-c", "user.name=dpm-test", "-c", "user.email=dpm@example.invalid", "commit", "--no-gpg-sign", "-m", "initial registry")

	return root
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not available: %v", err)
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s error = %v\n%s", strings.Join(args, " "), err, out)
	}

	return strings.TrimSpace(string(out))
}

func fileURL(path string) string {
	return "file://" + filepath.ToSlash(path)
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Stat(%q) error = %v", path, err)
	}
}
