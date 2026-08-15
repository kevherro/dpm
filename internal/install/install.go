// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package install

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/kevherro/dpm/internal/archive"
	"github.com/kevherro/dpm/internal/config"
	"github.com/kevherro/dpm/internal/link"
	"github.com/kevherro/dpm/internal/manifest"
	"github.com/kevherro/dpm/internal/registry"
	"github.com/kevherro/dpm/internal/state"
)

// Installer installs registry packages into a configured dpm root.
type Installer struct {
	Fetcher       ArtifactFetcher
	Now           func() time.Time
	GOOS          string
	GOARCH        string
	hook          lifecycleHook
	cleanupBins   func(config.Config, []link.BinLink) error
	cleanupPrefix func(config.Config, string) error
}

type lifecyclePoint string

const (
	afterPrefixCommit lifecyclePoint = "after-prefix-commit"
	afterLinksCommit  lifecyclePoint = "after-links-commit"
	beforeStateCommit lifecyclePoint = "before-state-commit"
	afterStateCommit  lifecyclePoint = "after-state-commit"
	afterRemoveBins   lifecyclePoint = "after-remove-bins"
	afterRemovePrefix lifecyclePoint = "after-remove-prefix"
)

type lifecycleHook func(lifecyclePoint, string) error

// InstallResult reports packages handled during an install.
type InstallResult struct {
	Packages []PackageResult
}

// PackageResult reports one package handled during an install.
type PackageResult struct {
	Name             string
	Version          string
	Prefix           string
	Artifact         manifest.Artifact
	DownloadedPath   string
	Links            []link.BinLink
	AlreadyInstalled bool
}

// RemoveResult reports a removed package.
type RemoveResult struct {
	Record state.Record
}

// Install installs name using the default installer.
func Install(ctx context.Context, cfg config.Config, name string) (InstallResult, error) {
	return Installer{}.Install(ctx, cfg, name)
}

// Remove removes name using installed state.
func Remove(cfg config.Config, name string) (RemoveResult, error) {
	return remove(cfg, name, nil)
}

func remove(cfg config.Config, name string, hook lifecycleHook) (RemoveResult, error) {
	if err := cfg.RequireClientMutation(); err != nil {
		return RemoveResult{}, err
	}
	store := state.New(cfg)
	record, err := store.Get(name)
	if err != nil {
		return RemoveResult{}, err
	}
	if err := link.RemoveBins(cfg, record.Bins); err != nil {
		return RemoveResult{}, err
	}
	if err := runLifecycleHook(hook, afterRemoveBins, name); err != nil {
		return RemoveResult{}, err
	}
	prefix := filepath.Join(cfg.PkgsDir, record.Name, record.Version)
	if err := removePrefix(cfg, prefix); err != nil {
		return RemoveResult{}, err
	}
	if err := runLifecycleHook(hook, afterRemovePrefix, name); err != nil {
		return RemoveResult{}, err
	}
	if err := store.Remove(name); err != nil {
		return RemoveResult{}, err
	}

	return RemoveResult{Record: record}, nil
}

// Install installs name and its dependencies.
func (i Installer) Install(ctx context.Context, cfg config.Config, name string) (InstallResult, error) {
	if err := cfg.RequireClientMutation(); err != nil {
		return InstallResult{}, err
	}
	if err := cfg.EnsureDirs(); err != nil {
		return InstallResult{}, err
	}
	reg, err := registry.NewWithOptions(registry.Options{
		Root:        cfg.RegistryDir,
		StaticIndex: cfg.RegistryStaticIndex,
	})
	if err != nil {
		return InstallResult{}, err
	}
	store := state.New(cfg)
	var result InstallResult
	if err := i.installOne(ctx, cfg, reg, store, name, map[string]bool{}, &result); err != nil {
		return InstallResult{}, err
	}

	return result, nil
}

func (i Installer) installOne(ctx context.Context, cfg config.Config, reg registry.Registry, store state.Store, name string, stack map[string]bool, result *InstallResult) error {
	if stack[name] {
		return fmt.Errorf("dependency cycle includes %s", name)
	}
	stack[name] = true
	defer delete(stack, name)

	m, err := reg.Resolve(name)
	if err != nil {
		return err
	}
	for _, dep := range m.Dependencies {
		if err := i.installOne(ctx, cfg, reg, store, dep, stack, result); err != nil {
			return err
		}
	}

	if record, err := store.Get(name); err == nil {
		if record.Version == m.Version {
			result.Packages = append(result.Packages, PackageResult{
				Name:             record.Name,
				Version:          record.Version,
				Prefix:           record.Prefix,
				Links:            record.Bins,
				AlreadyInstalled: true,
			})
			return nil
		}
		return fmt.Errorf("%s %s is already installed; refusing to replace with %s", record.Name, record.Version, m.Version)
	} else if !errors.Is(err, state.ErrNotInstalled) {
		return err
	}

	artifact, err := i.selectArtifact(m)
	if err != nil {
		return err
	}
	downloadedPath, err := i.Fetcher.Fetch(ctx, cfg, artifact)
	if err != nil {
		return err
	}
	prefix := filepath.Join(cfg.PkgsDir, m.Name, m.Version)
	if err := cfg.RequireInsideRoot(prefix); err != nil {
		return err
	}
	if _, err := os.Lstat(prefix); err == nil {
		return fmt.Errorf("package prefix %s already exists without matching state", prefix)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat package prefix %s: %w", prefix, err)
	}
	if err := os.MkdirAll(filepath.Dir(prefix), 0o755); err != nil {
		return fmt.Errorf("create package parent %s: %w", filepath.Dir(prefix), err)
	}
	staging, err := os.MkdirTemp(cfg.CacheDir, ".install-"+m.Name+"-"+m.Version+"-*")
	if err != nil {
		return fmt.Errorf("create install staging directory: %w", err)
	}
	removeStaging := true
	defer func() {
		if removeStaging {
			_ = os.RemoveAll(staging)
		}
	}()
	if err := cfg.RequireInsideRoot(staging); err != nil {
		return err
	}

	if err := archive.ExtractTarGz(downloadedPath, staging); err != nil {
		return err
	}
	if err := os.Rename(staging, prefix); err != nil {
		return fmt.Errorf("install package prefix %s: %w", prefix, err)
	}
	removeStaging = false
	if err := runLifecycleHook(i.hook, afterPrefixCommit, name); err != nil {
		return err
	}

	links, err := link.LinkBins(cfg, prefix, m.Install.Bins)
	if err != nil {
		_ = i.removePrefix(cfg, prefix)
		return err
	}
	if err := runLifecycleHook(i.hook, afterLinksCommit, name); err != nil {
		return err
	}
	if err := runLifecycleHook(i.hook, beforeStateCommit, name); err != nil {
		_ = i.removeBins(cfg, links)
		_ = i.removePrefix(cfg, prefix)
		return err
	}

	record := state.Record{
		Schema:       state.CurrentSchema,
		Name:         m.Name,
		Version:      m.Version,
		Source:       artifact.URL,
		SHA256:       artifact.SHA256,
		Prefix:       prefix,
		Bins:         links,
		Dependencies: m.Dependencies,
		InstalledAt:  i.now().UTC().Round(0),
	}
	if err := store.Save(record); err != nil {
		_ = i.removeBins(cfg, links)
		_ = i.removePrefix(cfg, prefix)
		return err
	}
	if err := runLifecycleHook(i.hook, afterStateCommit, name); err != nil {
		return err
	}

	result.Packages = append(result.Packages, PackageResult{
		Name:           m.Name,
		Version:        m.Version,
		Prefix:         prefix,
		Artifact:       artifact,
		DownloadedPath: downloadedPath,
		Links:          links,
	})

	return nil
}

func (i Installer) removeBins(cfg config.Config, bins []link.BinLink) error {
	if i.cleanupBins != nil {
		return i.cleanupBins(cfg, bins)
	}

	return link.RemoveBins(cfg, bins)
}

func (i Installer) removePrefix(cfg config.Config, prefix string) error {
	if i.cleanupPrefix != nil {
		return i.cleanupPrefix(cfg, prefix)
	}

	return removePrefix(cfg, prefix)
}

func runLifecycleHook(hook lifecycleHook, point lifecyclePoint, name string) error {
	if hook == nil {
		return nil
	}
	if err := hook(point, name); err != nil {
		return fmt.Errorf("%s for %s: %w", point, name, err)
	}

	return nil
}

func (i Installer) selectArtifact(m manifest.Manifest) (manifest.Artifact, error) {
	goos := i.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	goarch := i.GOARCH
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	return SelectArtifact(m, goos, goarch)
}

func (i Installer) now() time.Time {
	if i.Now != nil {
		return i.Now()
	}

	return time.Now()
}

// SelectArtifact chooses the artifact matching goos/goarch.
func SelectArtifact(m manifest.Manifest, goos, goarch string) (manifest.Artifact, error) {
	goos = normalizeOS(goos)
	goarch = normalizeArch(goarch)
	for _, artifact := range m.Artifacts {
		if normalizeOS(artifact.OS) == goos && normalizeArch(artifact.Arch) == goarch {
			return artifact, nil
		}
	}

	return manifest.Artifact{}, fmt.Errorf("no artifact for %s/%s in %s %s", goos, goarch, m.Name, m.Version)
}

func normalizeOS(goos string) string {
	return strings.ToLower(strings.TrimSpace(goos))
}

func normalizeArch(goarch string) string {
	switch strings.ToLower(strings.TrimSpace(goarch)) {
	case "x86_64":
		return "amd64"
	default:
		return strings.ToLower(strings.TrimSpace(goarch))
	}
}

func removePrefix(cfg config.Config, prefix string) error {
	if err := cfg.RequireInsideRoot(prefix); err != nil {
		return err
	}
	if err := requireInside(cfg.PkgsDir, prefix); err != nil {
		return err
	}
	if err := os.RemoveAll(prefix); err != nil {
		return fmt.Errorf("remove package prefix %s: %w", prefix, err)
	}

	return nil
}

func requireInside(root, path string) error {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return fmt.Errorf("compare %s to root %s: %w", path, root, err)
	}
	if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		return nil
	}

	return fmt.Errorf("path %s escapes root %s", path, root)
}
