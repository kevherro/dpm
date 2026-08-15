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
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/kevherro/dpm/internal/archive"
	"github.com/kevherro/dpm/internal/config"
	"github.com/kevherro/dpm/internal/link"
	"github.com/kevherro/dpm/internal/manifest"
	"github.com/kevherro/dpm/internal/operationlock"
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

type installPlan struct {
	manifest   manifest.Manifest
	artifact   manifest.Artifact
	existing   *state.Record
	downloaded string
	staging    string
}

type installMutation struct {
	name       string
	prefix     string
	links      []link.BinLink
	stateSaved bool
}

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

func remove(cfg config.Config, name string, hook lifecycleHook) (result RemoveResult, retErr error) {
	if err := cfg.RequireClientMutation(); err != nil {
		return RemoveResult{}, err
	}
	if err := cfg.EnsureDirs(); err != nil {
		return RemoveResult{}, err
	}
	lock, err := operationlock.Acquire(cfg.Root, operationlock.Exclusive)
	if err != nil {
		return RemoveResult{}, err
	}
	defer func() {
		retErr = errors.Join(retErr, lock.Close())
	}()
	store := state.New(cfg)
	if err := detectInterruptedOperations(cfg, store); err != nil {
		return RemoveResult{}, err
	}
	record, err := store.Get(name)
	if err != nil {
		return RemoveResult{}, err
	}
	records, err := store.List()
	if err != nil {
		return RemoveResult{}, err
	}
	var blockers []string
	for _, installed := range records {
		if slices.Contains(installed.Dependencies, name) {
			blockers = append(blockers, installed.Name)
		}
	}
	if len(blockers) > 0 {
		slices.Sort(blockers)
		return RemoveResult{}, fmt.Errorf("refusing to remove %s: required by %s", name, strings.Join(blockers, ", "))
	}
	prefix := filepath.Join(cfg.PkgsDir, record.Name, record.Version)
	info, err := os.Lstat(prefix)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return RemoveResult{}, fmt.Errorf("package prefix %s is missing or invalid; run `dpm doctor`", prefix)
	}
	if err := link.ValidateRemoveBins(cfg, record.Bins); err != nil {
		return RemoveResult{}, err
	}
	quarantine, err := os.MkdirTemp(cfg.CacheDir, ".remove-"+record.Name+"-"+record.Version+"-*")
	if err != nil {
		return RemoveResult{}, fmt.Errorf("create removal quarantine: %w", err)
	}
	if err := os.Remove(quarantine); err != nil {
		return RemoveResult{}, fmt.Errorf("prepare removal quarantine %s: %w", quarantine, err)
	}
	if err := os.Rename(prefix, quarantine); err != nil {
		return RemoveResult{}, fmt.Errorf("quarantine package prefix %s: %w", prefix, err)
	}
	linksRemoved := false
	stateRemoved := false
	rollback := func(primary error) error {
		var rollbackErr error
		if stateRemoved {
			rollbackErr = errors.Join(rollbackErr, store.Save(record))
		}
		if err := os.Rename(quarantine, prefix); err != nil {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("restore package prefix %s: %w", prefix, err))
		}
		if linksRemoved {
			rollbackErr = errors.Join(rollbackErr, restoreBins(record.Bins))
		}
		return errors.Join(primary, rollbackErr)
	}
	if err := runLifecycleHook(hook, afterRemovePrefix, name); err != nil {
		return RemoveResult{}, rollback(err)
	}
	if err := link.RemoveBins(cfg, record.Bins); err != nil {
		return RemoveResult{}, rollback(err)
	}
	linksRemoved = true
	if err := runLifecycleHook(hook, afterRemoveBins, name); err != nil {
		return RemoveResult{}, rollback(err)
	}
	if err := store.Remove(name); err != nil {
		return RemoveResult{}, rollback(err)
	}
	stateRemoved = true
	if err := os.RemoveAll(quarantine); err != nil {
		return RemoveResult{}, rollback(fmt.Errorf("remove quarantined prefix %s: %w", quarantine, err))
	}
	if err := os.Remove(filepath.Dir(prefix)); err != nil && !errors.Is(err, syscall.ENOTEMPTY) && !errors.Is(err, os.ErrNotExist) {
		return RemoveResult{}, fmt.Errorf("remove empty package directory %s: %w", filepath.Dir(prefix), err)
	}

	return RemoveResult{Record: record}, nil
}

func restoreBins(bins []link.BinLink) error {
	var restoreErr error
	for _, bin := range bins {
		if err := os.Symlink(bin.Source, bin.Link); err != nil {
			restoreErr = errors.Join(restoreErr, fmt.Errorf("restore bin link %s: %w", bin.Link, err))
		}
	}

	return restoreErr
}

// Install installs name and its dependencies.
func (i Installer) Install(ctx context.Context, cfg config.Config, name string) (result InstallResult, retErr error) {
	if err := cfg.RequireClientMutation(); err != nil {
		return InstallResult{}, err
	}
	if err := cfg.EnsureDirs(); err != nil {
		return InstallResult{}, err
	}
	lock, err := operationlock.Acquire(cfg.Root, operationlock.Exclusive)
	if err != nil {
		return InstallResult{}, err
	}
	defer func() {
		retErr = errors.Join(retErr, lock.Close())
	}()
	reg, err := registry.NewWithOptions(registry.Options{
		Root:        cfg.RegistryDir,
		StaticIndex: cfg.RegistryStaticIndex,
	})
	if err != nil {
		return InstallResult{}, err
	}
	store := state.New(cfg)
	if err := detectInterruptedOperations(cfg, store); err != nil {
		return InstallResult{}, err
	}
	plans, err := i.planInstall(cfg, reg, store, name)
	if err != nil {
		return InstallResult{}, err
	}
	if err := i.stageInstall(ctx, cfg, plans); err != nil {
		return InstallResult{}, errors.Join(err, cleanupStaging(plans))
	}
	mutations := make([]installMutation, 0, len(plans))
	for _, plan := range plans {
		if plan.existing != nil {
			result.Packages = append(result.Packages, PackageResult{
				Name:             plan.existing.Name,
				Version:          plan.existing.Version,
				Prefix:           plan.existing.Prefix,
				Links:            plan.existing.Bins,
				AlreadyInstalled: true,
			})
			continue
		}
		if err := i.commitInstall(cfg, store, plan, &mutations, &result); err != nil {
			return InstallResult{}, errors.Join(err, i.rollbackInstall(cfg, store, mutations), cleanupStaging(plans))
		}
	}
	if err := cleanupStaging(plans); err != nil {
		return InstallResult{}, err
	}

	return result, nil
}

func (i Installer) planInstall(cfg config.Config, reg registry.Registry, store state.Store, name string) ([]*installPlan, error) {
	var plans []*installPlan
	seen := make(map[string]bool)
	stack := make(map[string]bool)
	if err := i.resolveInstallPlan(cfg, reg, store, name, seen, stack, &plans); err != nil {
		return nil, err
	}
	binOwners := make(map[string]string)
	for _, plan := range plans {
		for _, bin := range plan.manifest.Install.Bins {
			binName := filepath.Base(bin)
			if owner, ok := binOwners[binName]; ok && owner != plan.manifest.Name {
				return nil, fmt.Errorf("packages %s and %s both declare bin %q", owner, plan.manifest.Name, binName)
			}
			binOwners[binName] = plan.manifest.Name
		}
		if plan.existing != nil {
			continue
		}
		prefix := filepath.Join(cfg.PkgsDir, plan.manifest.Name, plan.manifest.Version)
		if _, err := os.Lstat(prefix); err == nil {
			return nil, fmt.Errorf("package prefix %s already exists without matching state; run `dpm doctor`", prefix)
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("stat package prefix %s: %w", prefix, err)
		}
		packageDir := filepath.Dir(prefix)
		if info, err := os.Lstat(packageDir); err == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return nil, fmt.Errorf("package directory %s is not a managed directory", packageDir)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("inspect package directory %s: %w", packageDir, err)
		}
		for _, bin := range plan.manifest.Install.Bins {
			linkPath := filepath.Join(cfg.BinDir, filepath.Base(bin))
			if _, err := os.Lstat(linkPath); err == nil {
				return nil, fmt.Errorf("%w: %s already exists", link.ErrConflict, linkPath)
			} else if !errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("stat bin path %s: %w", linkPath, err)
			}
		}
	}

	return plans, nil
}

func (i Installer) resolveInstallPlan(cfg config.Config, reg registry.Registry, store state.Store, name string, seen, stack map[string]bool, plans *[]*installPlan) error {
	if seen[name] {
		return nil
	}
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
		if err := i.resolveInstallPlan(cfg, reg, store, dep, seen, stack, plans); err != nil {
			return err
		}
	}
	artifact, err := i.selectArtifact(m)
	if err != nil {
		return err
	}
	plan := &installPlan{manifest: m, artifact: artifact}
	if record, err := store.Get(name); err == nil {
		if record.Version != m.Version {
			return fmt.Errorf("%s %s is already installed; refusing to replace with %s", record.Name, record.Version, m.Version)
		}
		if err := validateHealthyInstallation(cfg, m, artifact, record); err != nil {
			return fmt.Errorf("integrity check failed for %s %s: %w; run `dpm doctor`", name, m.Version, err)
		}
		plan.existing = &record
	} else if !errors.Is(err, state.ErrNotInstalled) {
		return err
	}
	seen[name] = true
	*plans = append(*plans, plan)

	return nil
}

func (i Installer) stageInstall(ctx context.Context, cfg config.Config, plans []*installPlan) error {
	for _, plan := range plans {
		if plan.existing != nil {
			continue
		}
		downloaded, err := i.Fetcher.Fetch(ctx, cfg, plan.artifact)
		if err != nil {
			return fmt.Errorf("fetch artifact for %s %s: %w", plan.manifest.Name, plan.manifest.Version, err)
		}
		plan.downloaded = downloaded
		staging, err := os.MkdirTemp(cfg.CacheDir, ".install-"+plan.manifest.Name+"-"+plan.manifest.Version+"-*")
		if err != nil {
			return fmt.Errorf("create install staging directory: %w", err)
		}
		plan.staging = staging
		if err := archive.ExtractTarGz(downloaded, staging); err != nil {
			return err
		}
		if err := validateStagedBins(staging, plan.manifest.Install.Bins); err != nil {
			return fmt.Errorf("validate staged package %s %s: %w", plan.manifest.Name, plan.manifest.Version, err)
		}
	}

	return nil
}

func (i Installer) commitInstall(cfg config.Config, store state.Store, plan *installPlan, mutations *[]installMutation, result *InstallResult) error {
	m := plan.manifest
	prefix := filepath.Join(cfg.PkgsDir, m.Name, m.Version)
	packageDir := filepath.Dir(prefix)
	if err := os.Mkdir(packageDir, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("create package directory %s: %w", packageDir, err)
	}
	if err := os.Rename(plan.staging, prefix); err != nil {
		return fmt.Errorf("install package prefix %s: %w", prefix, err)
	}
	plan.staging = ""
	*mutations = append(*mutations, installMutation{name: m.Name, prefix: prefix})
	mutation := &(*mutations)[len(*mutations)-1]
	if err := runLifecycleHook(i.hook, afterPrefixCommit, m.Name); err != nil {
		return err
	}
	links, err := link.LinkBins(cfg, prefix, m.Install.Bins)
	if err != nil {
		return err
	}
	mutation.links = links
	if err := runLifecycleHook(i.hook, afterLinksCommit, m.Name); err != nil {
		return err
	}
	if err := runLifecycleHook(i.hook, beforeStateCommit, m.Name); err != nil {
		return err
	}
	record := state.Record{
		Schema:       state.CurrentSchema,
		Name:         m.Name,
		Version:      m.Version,
		Source:       plan.artifact.URL,
		SHA256:       plan.artifact.SHA256,
		Prefix:       prefix,
		Bins:         links,
		Dependencies: m.Dependencies,
		InstalledAt:  i.now().UTC().Round(0),
	}
	if err := store.Save(record); err != nil {
		return err
	}
	mutation.stateSaved = true
	if err := runLifecycleHook(i.hook, afterStateCommit, m.Name); err != nil {
		return err
	}
	result.Packages = append(result.Packages, PackageResult{
		Name:           m.Name,
		Version:        m.Version,
		Prefix:         prefix,
		Artifact:       plan.artifact,
		DownloadedPath: plan.downloaded,
		Links:          links,
	})

	return nil
}

func (i Installer) rollbackInstall(cfg config.Config, store state.Store, mutations []installMutation) error {
	var rollbackErr error
	for j := len(mutations) - 1; j >= 0; j-- {
		mutation := mutations[j]
		if mutation.stateSaved {
			rollbackErr = errors.Join(rollbackErr, store.Remove(mutation.name))
		}
		if len(mutation.links) > 0 {
			rollbackErr = errors.Join(rollbackErr, i.removeBins(cfg, mutation.links))
		}
		rollbackErr = errors.Join(rollbackErr, i.removePrefix(cfg, mutation.prefix))
		if err := os.Remove(filepath.Dir(mutation.prefix)); err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, syscall.ENOTEMPTY) {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("remove empty package directory: %w", err))
		}
	}

	return rollbackErr
}

func cleanupStaging(plans []*installPlan) error {
	var cleanupErr error
	for _, plan := range plans {
		if plan.staging == "" {
			continue
		}
		if err := os.RemoveAll(plan.staging); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove staging directory %s: %w", plan.staging, err))
		}
		plan.staging = ""
	}

	return cleanupErr
}

func validateStagedBins(staging string, bins []string) error {
	for _, bin := range bins {
		path := filepath.Join(staging, bin)
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("stat bin %s: %w", bin, err)
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			return fmt.Errorf("bin %s is not a regular executable", bin)
		}
	}

	return nil
}

func validateHealthyInstallation(cfg config.Config, m manifest.Manifest, artifact manifest.Artifact, record state.Record) error {
	if record.Source != artifact.URL || record.SHA256 != artifact.SHA256 {
		return fmt.Errorf("artifact ownership differs from registry manifest")
	}
	if !slices.Equal(record.Dependencies, m.Dependencies) {
		return fmt.Errorf("dependency state differs from registry manifest")
	}
	info, err := os.Lstat(record.Prefix)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("package prefix is missing or invalid")
	}
	if len(record.Bins) != len(m.Install.Bins) {
		return fmt.Errorf("bin state differs from registry manifest")
	}
	for idx, binPath := range m.Install.Bins {
		bin := record.Bins[idx]
		wantName := filepath.Base(binPath)
		wantSource := filepath.Join(record.Prefix, binPath)
		wantLink := filepath.Join(cfg.BinDir, wantName)
		if bin.Name != wantName || bin.Source != wantSource || bin.Link != wantLink {
			return fmt.Errorf("bin ownership differs for %s", wantName)
		}
		info, err := os.Lstat(bin.Source)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			return fmt.Errorf("bin source %s is missing or invalid", bin.Source)
		}
		target, err := os.Readlink(bin.Link)
		if err != nil || target != bin.Source {
			return fmt.Errorf("bin link %s is missing or retargeted", bin.Link)
		}
	}

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
