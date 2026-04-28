// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package registry

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	// UpdateCloned means Update cloned a missing registry checkout.
	UpdateCloned = "cloned"
	// UpdatePulled means Update pulled an existing registry checkout.
	UpdatePulled = "pulled"
)

// UpdateOptions configures a Git-backed registry update.
type UpdateOptions struct {
	Root string
	URL  string
}

// UpdateResult reports what dpm update changed.
type UpdateResult struct {
	Root     string
	URL      string
	Action   string
	Revision string
}

// Update clones or fast-forwards a Git-backed registry checkout.
func Update(ctx context.Context, opts UpdateOptions) (UpdateResult, error) {
	root, err := cleanUpdateRoot(opts.Root)
	if err != nil {
		return UpdateResult{}, err
	}
	if opts.URL == "" {
		return UpdateResult{}, fmt.Errorf("registry url is empty")
	}

	info, err := os.Stat(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cloneRegistry(ctx, root, opts.URL)
		}

		return UpdateResult{}, fmt.Errorf("stat registry %s: %w", root, err)
	}
	if !info.IsDir() {
		return UpdateResult{}, fmt.Errorf("registry %s is not a directory", root)
	}
	empty, err := isEmptyDir(root)
	if err != nil {
		return UpdateResult{}, err
	}
	if empty {
		return cloneRegistry(ctx, root, opts.URL)
	}
	if err := requireGitCheckout(ctx, root); err != nil {
		return UpdateResult{}, err
	}
	if err := requireCleanCheckout(ctx, root); err != nil {
		return UpdateResult{}, err
	}
	if _, err := runGit(ctx, root, "pull", "--ff-only"); err != nil {
		return UpdateResult{}, fmt.Errorf("pull registry %s: %w", root, err)
	}

	rev, err := registryRevision(ctx, root)
	if err != nil {
		return UpdateResult{}, err
	}

	return UpdateResult{Root: root, URL: opts.URL, Action: UpdatePulled, Revision: rev}, nil
}

func isEmptyDir(root string) (bool, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return false, fmt.Errorf("read registry directory %s: %w", root, err)
	}

	return len(entries) == 0, nil
}

func cloneRegistry(ctx context.Context, root, url string) (UpdateResult, error) {
	if err := os.MkdirAll(filepath.Dir(root), 0o755); err != nil {
		return UpdateResult{}, fmt.Errorf("create registry parent %s: %w", filepath.Dir(root), err)
	}
	if _, err := runGit(ctx, "", "clone", url, root); err != nil {
		return UpdateResult{}, fmt.Errorf("clone registry %s into %s: %w", url, root, err)
	}
	rev, err := registryRevision(ctx, root)
	if err != nil {
		return UpdateResult{}, err
	}

	return UpdateResult{Root: root, URL: url, Action: UpdateCloned, Revision: rev}, nil
}

func cleanUpdateRoot(root string) (string, error) {
	if root == "" {
		return "", fmt.Errorf("registry root is empty")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve registry root %q: %w", root, err)
	}

	return filepath.Clean(abs), nil
}

func requireGitCheckout(ctx context.Context, root string) error {
	top, err := runGit(ctx, root, "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("registry %s is not a git checkout", root)
	}
	top = strings.TrimSpace(top)
	absTop, err := filepath.Abs(top)
	if err != nil {
		return fmt.Errorf("resolve git checkout root %q: %w", top, err)
	}
	evalTop, err := filepath.EvalSymlinks(absTop)
	if err != nil {
		return fmt.Errorf("resolve git checkout root symlinks %q: %w", absTop, err)
	}
	evalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("resolve registry root symlinks %q: %w", root, err)
	}
	if filepath.Clean(evalTop) != filepath.Clean(evalRoot) {
		return fmt.Errorf("registry %s is not the root of a git checkout", root)
	}

	return nil
}

func requireCleanCheckout(ctx context.Context, root string) error {
	status, err := runGit(ctx, root, "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("check registry status %s: %w", root, err)
	}
	if strings.TrimSpace(status) != "" {
		return fmt.Errorf("registry %s has uncommitted changes; commit or discard them before running dpm update", root)
	}

	return nil
}

func registryRevision(ctx context.Context, root string) (string, error) {
	rev, err := runGit(ctx, root, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", fmt.Errorf("read registry revision %s: %w", root, err)
	}

	return strings.TrimSpace(rev), nil
}

func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	if err != nil {
		if output == "" {
			return "", err
		}

		return "", fmt.Errorf("%s: %w", output, err)
	}

	return output, nil
}
