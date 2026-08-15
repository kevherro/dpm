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
	Root              string
	URL               string
	ValidateCandidate func(string) error
	AfterActivate     func() error
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
	if _, err := exec.LookPath("git"); err != nil {
		return UpdateResult{}, fmt.Errorf("Git is required for dpm update; install the Xcode Command Line Tools with `xcode-select --install`: %w", err)
	}
	root, err := cleanUpdateRoot(opts.Root)
	if err != nil {
		return UpdateResult{}, err
	}
	if opts.URL == "" {
		return UpdateResult{}, fmt.Errorf("registry url is empty")
	}
	if err := DetectInterruptedUpdate(root); err != nil {
		return UpdateResult{}, err
	}

	action := UpdateCloned
	active := false
	info, err := os.Lstat(root)
	if err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return UpdateResult{}, fmt.Errorf("registry %s is not a managed directory", root)
		}
		empty, err := isEmptyDir(root)
		if err != nil {
			return UpdateResult{}, err
		}
		if !empty {
			if err := requireGitCheckout(ctx, root); err != nil {
				return UpdateResult{}, err
			}
			if err := requireCleanCheckout(ctx, root); err != nil {
				return UpdateResult{}, err
			}
			action = UpdatePulled
		}
		active = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return UpdateResult{}, fmt.Errorf("inspect registry %s: %w", root, err)
	}

	parent := filepath.Dir(root)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return UpdateResult{}, fmt.Errorf("create registry parent %s: %w", parent, err)
	}
	candidate, err := vacantTempPath(parent, ".registry-candidate-*")
	if err != nil {
		return UpdateResult{}, err
	}
	removeCandidate := true
	defer func() {
		if removeCandidate {
			_ = os.RemoveAll(candidate)
		}
	}()
	if _, err := runGit(ctx, "", "clone", "--", opts.URL, candidate); err != nil {
		return UpdateResult{}, fmt.Errorf("clone registry candidate from %s: %w", opts.URL, err)
	}
	report, err := Validate(ctx, ValidateOptions{Root: candidate})
	if err != nil {
		return UpdateResult{}, fmt.Errorf("validate registry candidate: %w", err)
	}
	if !report.Valid() {
		return UpdateResult{}, fmt.Errorf("registry candidate is invalid: %s: %s", report.Issues[0].Path, report.Issues[0].Message)
	}
	if opts.ValidateCandidate != nil {
		if err := opts.ValidateCandidate(candidate); err != nil {
			return UpdateResult{}, fmt.Errorf("validate registry candidate: %w", err)
		}
	}
	rev, err := registryRevision(ctx, candidate)
	if err != nil {
		return UpdateResult{}, err
	}

	backup := ""
	if active {
		backup, err = vacantTempPath(parent, ".registry-previous-*")
		if err != nil {
			return UpdateResult{}, err
		}
		if err := os.Rename(root, backup); err != nil {
			return UpdateResult{}, fmt.Errorf("move active registry aside: %w", err)
		}
	}
	if err := os.Rename(candidate, root); err != nil {
		if backup != "" {
			return UpdateResult{}, errors.Join(fmt.Errorf("activate registry candidate: %w", err), restoreRegistryBackup(root, backup))
		}
		return UpdateResult{}, fmt.Errorf("activate registry candidate: %w", err)
	}
	if opts.AfterActivate != nil {
		if err := opts.AfterActivate(); err != nil {
			rollbackErr := os.Rename(root, candidate)
			if rollbackErr == nil && backup != "" {
				rollbackErr = restoreRegistryBackup(root, backup)
			}
			return UpdateResult{}, errors.Join(fmt.Errorf("finalize registry activation: %w", err), rollbackErr)
		}
	}
	removeCandidate = false
	if backup != "" {
		if err := os.RemoveAll(backup); err != nil {
			return UpdateResult{}, fmt.Errorf("remove previous registry backup %s: %w", backup, err)
		}
	}

	return UpdateResult{Root: root, URL: opts.URL, Action: action, Revision: rev}, nil
}

// DetectInterruptedUpdate refuses registry reads while swap evidence remains.
func DetectInterruptedUpdate(root string) error {
	parent := filepath.Dir(root)
	for _, pattern := range []string{".registry-candidate-*", ".registry-previous-*"} {
		matches, err := filepath.Glob(filepath.Join(parent, pattern))
		if err != nil {
			return fmt.Errorf("inspect registry update staging: %w", err)
		}
		for _, match := range matches {
			if filepath.Clean(match) == filepath.Clean(root) {
				continue
			}
			return fmt.Errorf("interrupted registry update evidence at %s; inspect it and run `dpm doctor`", match)
		}
	}

	return nil
}

func vacantTempPath(parent, pattern string) (string, error) {
	path, err := os.MkdirTemp(parent, pattern)
	if err != nil {
		return "", fmt.Errorf("reserve registry staging path: %w", err)
	}
	if err := os.Remove(path); err != nil {
		return "", fmt.Errorf("prepare registry staging path %s: %w", path, err)
	}

	return path, nil
}

func restoreRegistryBackup(root, backup string) error {
	if _, err := os.Lstat(root); err == nil {
		return fmt.Errorf("cannot restore registry backup while %s exists", root)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect failed registry activation %s: %w", root, err)
	}
	if err := os.Rename(backup, root); err != nil {
		return fmt.Errorf("restore previous registry %s: %w", root, err)
	}

	return nil
}

func isEmptyDir(root string) (bool, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return false, fmt.Errorf("read registry directory %s: %w", root, err)
	}

	return len(entries) == 0, nil
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

// Revision returns the current short Git revision for a registry checkout.
func Revision(ctx context.Context, root string) (string, error) {
	root, err := cleanUpdateRoot(root)
	if err != nil {
		return "", err
	}
	if err := requireGitCheckout(ctx, root); err != nil {
		return "", err
	}

	return registryRevision(ctx, root)
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
