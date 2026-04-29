// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kevherro/dpm/internal/config"
	"github.com/kevherro/dpm/internal/install"
	"github.com/kevherro/dpm/internal/maintain"
	"github.com/kevherro/dpm/internal/registry"
	"github.com/kevherro/dpm/internal/state"
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return 2
	}

	cfg, err := config.Default()
	if err != nil {
		printError(stderr, err)
		return 1
	}

	var runErr error
	switch args[0] {
	case "install":
		runErr = runInstall(ctx, cfg, args[1:], stdout)
	case "remove":
		runErr = runRemove(cfg, args[1:], stdout)
	case "list":
		runErr = runList(cfg, args[1:], stdout)
	case "search":
		runErr = runSearch(cfg, args[1:], stdout)
	case "info":
		runErr = runInfo(cfg, args[1:], stdout)
	case "update":
		runErr = runUpdate(ctx, cfg, args[1:], stdout)
	case "doctor":
		runErr = runDoctor(ctx, cfg, args[1:], stdout)
	case "registry":
		runErr = runRegistry(ctx, cfg, args[1:], stdout)
	case "help", "-h", "--help":
		printUsage(stdout)
		return 0
	default:
		printUsage(stderr)
		return 2
	}
	if runErr != nil {
		printError(stderr, runErr)
		return 1
	}

	return 0
}

func runInstall(ctx context.Context, cfg config.Config, args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: dpm install <name>")
	}
	result, err := install.Install(ctx, cfg, args[0])
	if err != nil {
		return suggestUpdateForRegistryMiss(cfg, args[0], err)
	}
	for _, pkg := range result.Packages {
		if pkg.AlreadyInstalled {
			fmt.Fprintf(stdout, "%s %s already installed\n", pkg.Name, pkg.Version)
			continue
		}
		fmt.Fprintf(stdout, "installing %s %s\n", pkg.Name, pkg.Version)
		for _, bin := range pkg.Links {
			fmt.Fprintf(stdout, "linked %s\n", bin.Link)
		}
	}
	fmt.Fprintln(stdout, "done")

	return nil
}

func runRemove(cfg config.Config, args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: dpm remove <name>")
	}
	result, err := install.Remove(cfg, args[0])
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "removed %s %s\n", result.Record.Name, result.Record.Version)

	return nil
}

func runList(cfg config.Config, args []string, stdout io.Writer) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: dpm list")
	}
	records, err := state.New(cfg).List()
	if err != nil {
		return err
	}
	for _, record := range records {
		fmt.Fprintf(stdout, "%s %s\n", record.Name, record.Version)
	}

	return nil
}

func runSearch(cfg config.Config, args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: dpm search <query>")
	}
	reg, err := newRegistry(cfg)
	if err != nil {
		return err
	}
	matches, err := reg.Search(args[0])
	if err != nil {
		return err
	}
	for _, match := range matches {
		fmt.Fprintln(stdout, formatSearchResult(match))
	}

	return nil
}

func runInfo(cfg config.Config, args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: dpm info <name>")
	}
	reg, err := newRegistry(cfg)
	if err != nil {
		return err
	}
	pkg, err := optionalPackage(reg, args[0])
	if err != nil {
		return err
	}
	m, err := reg.Resolve(args[0])
	if err != nil {
		return err
	}

	fmt.Fprintf(stdout, "name %s\n", m.Name)
	if pkg.Name != "" {
		fmt.Fprintf(stdout, "summary %s\n", pkg.Summary)
		fmt.Fprintf(stdout, "homepage %s\n", pkg.Homepage)
		fmt.Fprintf(stdout, "license %s\n", pkg.License)
		if len(pkg.Categories) > 0 {
			fmt.Fprintf(stdout, "categories %s\n", strings.Join(pkg.Categories, " "))
		} else {
			fmt.Fprintln(stdout, "categories")
		}
	}
	fmt.Fprintf(stdout, "version %s\n", m.Version)
	if len(m.Dependencies) > 0 {
		fmt.Fprintf(stdout, "dependencies %s\n", strings.Join(m.Dependencies, " "))
	} else {
		fmt.Fprintln(stdout, "dependencies")
	}
	fmt.Fprintf(stdout, "yanked %t\n", m.Yanked)
	if m.YankReason != "" {
		fmt.Fprintf(stdout, "yank_reason %s\n", m.YankReason)
	}
	for _, artifact := range m.Artifacts {
		fmt.Fprintf(stdout, "artifact %s/%s %s\n", artifact.OS, artifact.Arch, artifact.URL)
	}
	fmt.Fprintf(stdout, "bins %s\n", strings.Join(m.Install.Bins, " "))

	return nil
}

func optionalPackage(reg registry.Registry, name string) (registry.Package, error) {
	pkg, err := reg.Package(name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, registry.ErrPackageNotFound) {
			return registry.Package{}, nil
		}

		return registry.Package{}, err
	}

	return pkg, nil
}

func newRegistry(cfg config.Config) (registry.Registry, error) {
	return registry.NewWithOptions(registry.Options{
		Root:        cfg.RegistryDir,
		StaticIndex: cfg.RegistryStaticIndex,
	})
}

func formatSearchResult(match registry.SearchResult) string {
	if match.Summary == "" {
		return match.Name
	}
	if len(match.Categories) == 0 {
		return match.Name + "\t" + match.Summary
	}

	return match.Name + "\t" + match.Summary + "\t" + strings.Join(match.Categories, ",")
}

func runUpdate(ctx context.Context, cfg config.Config, args []string, stdout io.Writer) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: dpm update")
	}
	if err := cfg.RequireInsideRoot(cfg.RegistryDir); err != nil {
		return err
	}
	result, err := registry.Update(ctx, registry.UpdateOptions{
		Root: cfg.RegistryDir,
		URL:  cfg.RegistryURL,
	})
	if err != nil {
		return err
	}
	snapshot, verified, err := verifyUpdatedRegistrySnapshot(cfg)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s registry %s\n", result.Action, result.Root)
	fmt.Fprintf(stdout, "revision %s\n", result.Revision)
	if verified {
		fmt.Fprintf(stdout, "verified snapshot %d %s\n", snapshot.Version, snapshot.SHA256)
	}

	return nil
}

func verifyUpdatedRegistrySnapshot(cfg config.Config) (state.RegistrySnapshot, bool, error) {
	keys, err := registry.ParsePublicKeys(cfg.RegistryPublicKeys)
	if err != nil {
		return state.RegistrySnapshot{}, false, err
	}
	if len(keys) == 0 {
		return state.RegistrySnapshot{}, false, nil
	}
	verified, err := registry.VerifySnapshot(cfg.RegistryDir, keys)
	if err != nil {
		return state.RegistrySnapshot{}, false, err
	}
	store := state.New(cfg)
	previous, err := store.RegistrySnapshot()
	if err != nil && !errors.Is(err, state.ErrRegistrySnapshotNotFound) {
		return state.RegistrySnapshot{}, false, err
	}
	if err == nil {
		if verified.Version < previous.Version {
			return state.RegistrySnapshot{}, false, fmt.Errorf("registry snapshot rollback: current version %d is older than accepted version %d", verified.Version, previous.Version)
		}
		if verified.Version == previous.Version && verified.SHA256 != previous.SHA256 {
			return state.RegistrySnapshot{}, false, fmt.Errorf("registry snapshot version %d changed from %s to %s", verified.Version, previous.SHA256, verified.SHA256)
		}
	}
	snapshot := state.RegistrySnapshot{
		Version:    verified.Version,
		SHA256:     verified.SHA256,
		KeyID:      verified.KeyID,
		VerifiedAt: time.Now().UTC().Round(0),
	}
	if err := store.SaveRegistrySnapshot(snapshot); err != nil {
		return state.RegistrySnapshot{}, false, err
	}

	return snapshot, true, nil
}

func suggestUpdateForRegistryMiss(cfg config.Config, name string, err error) error {
	if !errors.Is(err, registry.ErrPackageNotFound) {
		return err
	}
	if !registryHasPackageData(cfg.RegistryDir) {
		return fmt.Errorf("%w\n\nregistry is missing or empty; run `dpm update` to fetch %s", err, cfg.RegistryURL)
	}

	return fmt.Errorf("%w\n\npackage %q was not found in the current registry; run `dpm update` to refresh %s", err, name, cfg.RegistryURL)
}

func registryHasPackageData(registryDir string) bool {
	info, err := os.Stat(filepath.Join(registryDir, "packages"))
	if err == nil && info.IsDir() {
		return true
	}
	info, err = os.Stat(filepath.Join(registryDir, registry.StaticIndexDir, "packages.json"))
	return err == nil && !info.IsDir()
}

func runDoctor(ctx context.Context, cfg config.Config, args []string, stdout io.Writer) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: dpm doctor")
	}

	dirs := []string{
		cfg.Root,
		cfg.BinDir,
		cfg.PkgsDir,
		cfg.DownloadsDir,
		cfg.CacheDir,
		cfg.RegistryDir,
		cfg.StateDir,
	}
	var missing []string
	for _, dir := range dirs {
		info, err := os.Stat(dir)
		if err != nil {
			missing = append(missing, dir)
			fmt.Fprintf(stdout, "missing %s\n", dir)
			continue
		}
		if !info.IsDir() {
			missing = append(missing, dir)
			fmt.Fprintf(stdout, "not-dir %s\n", dir)
			continue
		}
		fmt.Fprintf(stdout, "ok %s\n", dir)
	}

	if !pathContains(cfg.BinDir) {
		fmt.Fprintf(stdout, "path missing %s\n", cfg.BinDir)
	}
	fmt.Fprintf(stdout, "registry url %s\n", cfg.RegistryURL)
	fmt.Fprintf(stdout, "registry path %s\n", cfg.RegistryDir)
	rev, err := registry.Revision(ctx, cfg.RegistryDir)
	if err != nil {
		fmt.Fprintf(stdout, "registry revision unavailable: %v\n", err)
	} else {
		fmt.Fprintf(stdout, "registry revision %s\n", rev)
	}
	if len(missing) > 0 {
		return fmt.Errorf("doctor found %d filesystem issue(s)", len(missing))
	}

	return nil
}

func runRegistry(ctx context.Context, cfg config.Config, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: dpm registry <validate|prepare|generate-index> [args]")
	}
	switch args[0] {
	case "validate":
		return runRegistryValidate(ctx, cfg, args[1:], stdout)
	case "prepare":
		return runRegistryPrepare(ctx, args[1:], stdout)
	case "generate-index":
		return runRegistryGenerateIndex(ctx, args[1:], stdout)
	default:
		return fmt.Errorf("usage: dpm registry <validate|prepare|generate-index> [args]")
	}
}

func runRegistryValidate(ctx context.Context, _ config.Config, args []string, stdout io.Writer) error {
	verifyArtifacts := false
	var path string
	for _, arg := range args {
		switch arg {
		case "--verify-artifacts":
			verifyArtifacts = true
		default:
			if path != "" {
				return fmt.Errorf("usage: dpm registry validate [--verify-artifacts] <path>")
			}
			path = arg
		}
	}
	if path == "" {
		return fmt.Errorf("usage: dpm registry validate [--verify-artifacts] <path>")
	}

	report, err := registry.Validate(ctx, registry.ValidateOptions{
		Root:            path,
		VerifyArtifacts: verifyArtifacts,
	})
	if err != nil {
		return err
	}
	if report.Valid() {
		fmt.Fprintf(stdout, "registry valid %s\n", report.Root)
		return nil
	}
	for _, issue := range report.Issues {
		fmt.Fprintf(stdout, "error %s: %s\n", issue.Path, issue.Message)
	}

	return fmt.Errorf("registry validation failed with %d issue(s)", len(report.Issues))
}

func runRegistryGenerateIndex(ctx context.Context, args []string, stdout io.Writer) error {
	opts, err := parseRegistryGenerateIndexArgs(args)
	if err != nil {
		return err
	}
	result, err := registry.GenerateIndex(ctx, opts)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "generated index %s\n", result.IndexDir)
	if result.Signed {
		fmt.Fprintf(stdout, "signed snapshot %d\n", result.SnapshotVersion)
	}
	for _, file := range result.Files {
		fmt.Fprintf(stdout, "metadata %s %s\n", relativePath(result.Root, file.Path), file.SHA256)
	}

	return nil
}

func parseRegistryGenerateIndexArgs(args []string) (registry.GenerateIndexOptions, error) {
	var opts registry.GenerateIndexOptions
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--snapshot-version":
			value, next, err := nextRegistryPrepareValue(args, i, arg)
			if err != nil {
				return registry.GenerateIndexOptions{}, err
			}
			version, err := strconv.Atoi(value)
			if err != nil || version <= 0 {
				return registry.GenerateIndexOptions{}, fmt.Errorf("--snapshot-version requires a positive integer")
			}
			opts.SnapshotVersion = version
			i = next
		case "--signing-key-file":
			value, next, err := nextRegistryPrepareValue(args, i, arg)
			if err != nil {
				return registry.GenerateIndexOptions{}, err
			}
			data, err := os.ReadFile(value)
			if err != nil {
				return registry.GenerateIndexOptions{}, fmt.Errorf("read signing key file %s: %w", value, err)
			}
			opts.SigningKey = strings.TrimSpace(string(data))
			i = next
		default:
			if strings.HasPrefix(arg, "-") {
				return registry.GenerateIndexOptions{}, fmt.Errorf("unknown registry generate-index flag %s", arg)
			}
			if opts.Root != "" {
				return registry.GenerateIndexOptions{}, fmt.Errorf("usage: dpm registry generate-index [--snapshot-version N --signing-key-file path] <path>")
			}
			opts.Root = arg
		}
	}
	if opts.Root == "" {
		return registry.GenerateIndexOptions{}, fmt.Errorf("usage: dpm registry generate-index [--snapshot-version N --signing-key-file path] <path>")
	}

	return opts, nil
}

func runRegistryPrepare(ctx context.Context, args []string, stdout io.Writer) error {
	opts, err := parseRegistryPrepareArgs(args)
	if err != nil {
		return err
	}
	result, err := maintain.Prepare(ctx, opts)
	if err != nil {
		return err
	}

	fmt.Fprintf(stdout, "prepared %s %s\n", result.Name, result.Version)
	fmt.Fprintf(stdout, "artifact %s\n", result.ArtifactURL)
	fmt.Fprintf(stdout, "size %d\n", result.Size)
	if result.ContentType != "" {
		fmt.Fprintf(stdout, "content-type %s\n", result.ContentType)
	}
	fmt.Fprintf(stdout, "sha256 %s\n", result.SHA256)
	fmt.Fprintf(stdout, "bins %s\n", strings.Join(result.Bins, " "))
	if result.VerifiedInstall {
		fmt.Fprintln(stdout, "verified install")
	}
	fmt.Fprintf(stdout, "files %s\n", strings.Join(relativePaths(opts.RegistryRoot, result.CreatedFiles), " "))
	fmt.Fprintln(stdout, "diff")
	fmt.Fprint(stdout, result.Diff)

	return nil
}

func parseRegistryPrepareArgs(args []string) (maintain.PrepareOptions, error) {
	var opts maintain.PrepareOptions
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--name":
			value, next, err := nextRegistryPrepareValue(args, i, arg)
			if err != nil {
				return maintain.PrepareOptions{}, err
			}
			opts.Name = value
			i = next
		case "--version":
			value, next, err := nextRegistryPrepareValue(args, i, arg)
			if err != nil {
				return maintain.PrepareOptions{}, err
			}
			opts.Version = value
			i = next
		case "--url":
			value, next, err := nextRegistryPrepareValue(args, i, arg)
			if err != nil {
				return maintain.PrepareOptions{}, err
			}
			opts.ArtifactURL = value
			i = next
		case "--summary":
			value, next, err := nextRegistryPrepareValue(args, i, arg)
			if err != nil {
				return maintain.PrepareOptions{}, err
			}
			opts.Summary = value
			i = next
		case "--homepage":
			value, next, err := nextRegistryPrepareValue(args, i, arg)
			if err != nil {
				return maintain.PrepareOptions{}, err
			}
			opts.Homepage = value
			i = next
		case "--license":
			value, next, err := nextRegistryPrepareValue(args, i, arg)
			if err != nil {
				return maintain.PrepareOptions{}, err
			}
			opts.License = value
			i = next
		case "--category":
			value, next, err := nextRegistryPrepareValue(args, i, arg)
			if err != nil {
				return maintain.PrepareOptions{}, err
			}
			opts.Categories = append(opts.Categories, value)
			i = next
		case "--dependency":
			value, next, err := nextRegistryPrepareValue(args, i, arg)
			if err != nil {
				return maintain.PrepareOptions{}, err
			}
			opts.Dependencies = append(opts.Dependencies, value)
			i = next
		case "--bin":
			value, next, err := nextRegistryPrepareValue(args, i, arg)
			if err != nil {
				return maintain.PrepareOptions{}, err
			}
			opts.Bins = append(opts.Bins, value)
			i = next
		case "--skip-install-verify":
			opts.SkipInstallVerify = true
		default:
			if strings.HasPrefix(arg, "-") {
				return maintain.PrepareOptions{}, fmt.Errorf("unknown registry prepare flag %s", arg)
			}
			if opts.RegistryRoot != "" {
				return maintain.PrepareOptions{}, fmt.Errorf("usage: dpm registry prepare [options] <path>")
			}
			opts.RegistryRoot = arg
		}
	}
	if opts.RegistryRoot == "" {
		return maintain.PrepareOptions{}, fmt.Errorf("usage: dpm registry prepare [options] <path>")
	}

	return opts, nil
}

func nextRegistryPrepareValue(args []string, i int, flag string) (string, int, error) {
	if i+1 >= len(args) {
		return "", i, fmt.Errorf("%s requires a value", flag)
	}
	value := args[i+1]
	if value == "" {
		return "", i, fmt.Errorf("%s requires a non-empty value", flag)
	}

	return value, i + 1, nil
}

func relativePaths(root string, paths []string) []string {
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		result = append(result, relativePath(root, path))
	}

	return result
}

func relativePath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}

	return filepath.ToSlash(rel)
}

func pathContains(dir string) bool {
	for _, entry := range filepath.SplitList(os.Getenv("PATH")) {
		if entry == dir {
			return true
		}
	}

	return false
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: dpm <command> [args]")
	fmt.Fprintln(w, "commands: install, remove, list, search, info, update, doctor, registry")
}

func printError(w io.Writer, err error) {
	fmt.Fprintf(w, "error: %v\n", err)
}
