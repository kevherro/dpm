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
	"strings"

	"github.com/kevherro/dpm/internal/config"
	"github.com/kevherro/dpm/internal/install"
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
		runErr = runDoctor(cfg, args[1:], stdout)
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
		return suggestUpdateForMissingRegistry(cfg, err)
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
	reg, err := registry.New(cfg.RegistryDir)
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
	reg, err := registry.New(cfg.RegistryDir)
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
		if errors.Is(err, os.ErrNotExist) {
			return registry.Package{}, nil
		}

		return registry.Package{}, err
	}

	return pkg, nil
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
	fmt.Fprintf(stdout, "%s registry %s\n", result.Action, result.Root)
	fmt.Fprintf(stdout, "revision %s\n", result.Revision)

	return nil
}

func suggestUpdateForMissingRegistry(cfg config.Config, err error) error {
	if !errors.Is(err, registry.ErrPackageNotFound) {
		return err
	}
	if registryHasPackageDir(cfg.RegistryDir) {
		return err
	}

	return fmt.Errorf("%w\n\nregistry is missing or empty; run `dpm update` to fetch %s", err, cfg.RegistryURL)
}

func registryHasPackageDir(registryDir string) bool {
	info, err := os.Stat(filepath.Join(registryDir, "packages"))
	return err == nil && info.IsDir()
}

func runDoctor(cfg config.Config, args []string, stdout io.Writer) error {
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
	if len(missing) > 0 {
		return fmt.Errorf("doctor found %d filesystem issue(s)", len(missing))
	}

	return nil
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
	fmt.Fprintln(w, "commands: install, remove, list, search, info, update, doctor")
}

func printError(w io.Writer, err error) {
	fmt.Fprintf(w, "error: %v\n", err)
}
