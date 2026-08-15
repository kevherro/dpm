// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package registry

import (
	"context"
	"errors"
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
	assertNoUpdateStaging(t, root)
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
	writeFile(t, filepath.Join(source, "packages", "hello", "versions", "1.0.0", "dpm.toml"), `schema = 1
name = "hello"
version = "1.0.0"
dependencies = []

[[artifacts]]
os = "darwin"
arch = "arm64"
url = "https://example.com/hello-1.0.0.tar.gz"
sha256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

[install]
bins = ["hello"]
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
	assertNoUpdateStaging(t, root)
}

func TestUpdateMissingGitHasActionableError(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	_, err := Update(context.Background(), UpdateOptions{
		Root: filepath.Join(t.TempDir(), "registry"),
		URL:  "https://example.invalid/registry.git",
	})
	if err == nil || !strings.Contains(err.Error(), "xcode-select --install") {
		t.Fatalf("Update() error = %v, want Xcode Command Line Tools guidance", err)
	}
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

func TestUpdateCandidateFailurePreservesActiveRegistry(t *testing.T) {
	source := newGitRegistry(t)
	root := filepath.Join(t.TempDir(), "dpm-root", "registry")
	first, err := Update(context.Background(), UpdateOptions{Root: root, URL: fileURL(source)})
	if err != nil {
		t.Fatalf("first Update() error = %v", err)
	}
	writeFile(t, filepath.Join(source, "packages", "broken", "package.toml"), "schema = 1\nname = \"broken\"\n")
	git(t, source, "add", ".")
	git(t, source, "-c", "user.name=dpm-test", "-c", "user.email=dpm@example.invalid", "commit", "--no-gpg-sign", "-m", "add invalid package")

	if _, err := Update(context.Background(), UpdateOptions{Root: root, URL: fileURL(source)}); err == nil {
		t.Fatal("second Update() error = nil, want candidate validation error")
	}
	if got := git(t, root, "rev-parse", "--short", "HEAD"); got != first.Revision {
		t.Fatalf("active revision = %s, want preserved %s", got, first.Revision)
	}
	assertNoUpdateStaging(t, root)
}

func TestUpdateCandidateCallbackFailurePreservesActiveRegistry(t *testing.T) {
	source := newGitRegistry(t)
	root := filepath.Join(t.TempDir(), "dpm-root", "registry")
	first, err := Update(context.Background(), UpdateOptions{Root: root, URL: fileURL(source)})
	if err != nil {
		t.Fatalf("first Update() error = %v", err)
	}

	_, err = Update(context.Background(), UpdateOptions{
		Root: root,
		URL:  fileURL(source),
		ValidateCandidate: func(string) error {
			return errors.New("signature rejected")
		},
	})
	if err == nil || !strings.Contains(err.Error(), "signature rejected") {
		t.Fatalf("Update() error = %v, want callback rejection", err)
	}
	if got := git(t, root, "rev-parse", "--short", "HEAD"); got != first.Revision {
		t.Fatalf("active revision = %s, want preserved %s", got, first.Revision)
	}
	assertNoUpdateStaging(t, root)
}

func TestUpdateFinalizationFailureRestoresActiveRegistry(t *testing.T) {
	source := newGitRegistry(t)
	root := filepath.Join(t.TempDir(), "dpm-root", "registry")
	first, err := Update(context.Background(), UpdateOptions{Root: root, URL: fileURL(source)})
	if err != nil {
		t.Fatalf("first Update() error = %v", err)
	}
	writeFile(t, filepath.Join(source, "new-file"), "new\n")
	git(t, source, "add", ".")
	git(t, source, "-c", "user.name=dpm-test", "-c", "user.email=dpm@example.invalid", "commit", "--no-gpg-sign", "-m", "new revision")

	_, err = Update(context.Background(), UpdateOptions{
		Root: root,
		URL:  fileURL(source),
		AfterActivate: func() error {
			return errors.New("state write failed")
		},
	})
	if err == nil || !strings.Contains(err.Error(), "state write failed") {
		t.Fatalf("Update() error = %v, want finalization error", err)
	}
	if got := git(t, root, "rev-parse", "--short", "HEAD"); got != first.Revision {
		t.Fatalf("active revision = %s, want restored %s", got, first.Revision)
	}
	assertNoUpdateStaging(t, root)
}

func TestRegistryRejectsInterruptedUpdateEvidence(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "registry")
	if err := os.Mkdir(filepath.Join(parent, ".registry-candidate-stale"), 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}

	if _, err := New(root); err == nil || !strings.Contains(err.Error(), "interrupted registry update") {
		t.Fatalf("New() error = %v, want interrupted update evidence", err)
	}
}

func newGitRegistry(t *testing.T) string {
	t.Helper()
	requireGit(t)

	root := filepath.Join(t.TempDir(), "source-registry")
	if err := os.MkdirAll(filepath.Join(root, "packages"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	writeFile(t, filepath.Join(root, "packages", ".gitkeep"), "")
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

func assertNoUpdateStaging(t *testing.T, root string) {
	t.Helper()
	for _, pattern := range []string{".registry-candidate-*", ".registry-previous-*"} {
		matches, err := filepath.Glob(filepath.Join(filepath.Dir(root), pattern))
		if err != nil {
			t.Fatalf("Glob(%q) error = %v", pattern, err)
		}
		if len(matches) != 0 {
			t.Fatalf("update staging for %q = %v, want none", pattern, matches)
		}
	}
}
