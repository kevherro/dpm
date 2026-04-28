// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kevherro/dpm/internal/checksum"
	"github.com/kevherro/dpm/internal/config"
	"github.com/kevherro/dpm/internal/link"
	"github.com/kevherro/dpm/internal/registry"
	"github.com/kevherro/dpm/internal/state"
	"github.com/kevherro/dpm/internal/testutil"
)

func TestRunHelloEndToEnd(t *testing.T) {
	cfg := testCLIConfig(t)
	testutil.WriteHelloRegistry(t, cfg)

	code, stdout, stderr := runCLI(t, []string{"install", "hello"})
	if code != 0 {
		t.Fatalf("install code = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{
		"installing hello 1.0.0\n",
		"linked " + filepath.Join(cfg.BinDir, "hello") + "\n",
		"done\n",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("install stdout = %q, want substring %q", stdout, want)
		}
	}

	hello := exec.CommandContext(context.Background(), filepath.Join(cfg.BinDir, "hello"))
	helloOutput, err := hello.Output()
	if err != nil {
		t.Fatalf("installed hello command error = %v", err)
	}
	if string(helloOutput) != testutil.HelloOutput {
		t.Fatalf("hello output = %q, want %q", helloOutput, testutil.HelloOutput)
	}

	code, stdout, stderr = runCLI(t, []string{"list"})
	if code != 0 {
		t.Fatalf("list code = %d, stderr = %q", code, stderr)
	}
	if stdout != "hello 1.0.0\n" {
		t.Fatalf("list stdout = %q, want hello listing", stdout)
	}

	code, stdout, stderr = runCLI(t, []string{"remove", "hello"})
	if code != 0 {
		t.Fatalf("remove code = %d, stderr = %q", code, stderr)
	}
	if stdout != "removed hello 1.0.0\n" {
		t.Fatalf("remove stdout = %q, want removal", stdout)
	}
	if _, err := os.Lstat(filepath.Join(cfg.BinDir, "hello")); !os.IsNotExist(err) {
		t.Fatalf("Lstat(hello link) error = %v, want not exist", err)
	}
	if _, err := state.New(cfg).Get("hello"); err == nil {
		t.Fatal("state Get(hello) error = nil, want missing after remove")
	}
}

func TestRunList(t *testing.T) {
	cfg := testCLIConfig(t)
	record := state.Record{
		Name:    "hello",
		Version: "1.0.0",
		Source:  "file:///tmp/hello.tar.gz",
		SHA256:  strings.Repeat("a", 64),
		Prefix:  filepath.Join(cfg.PkgsDir, "hello", "1.0.0"),
		Bins: []link.BinLink{
			{
				Name:   "hello",
				Source: filepath.Join(cfg.PkgsDir, "hello", "1.0.0", "bin", "hello"),
				Link:   filepath.Join(cfg.BinDir, "hello"),
			},
		},
		InstalledAt: time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC),
	}
	if err := state.New(cfg).Save(record); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	code, stdout, stderr := runCLI(t, []string{"list"})
	if code != 0 {
		t.Fatalf("run() code = %d, stderr = %q", code, stderr)
	}
	if stdout != "hello 1.0.0\n" {
		t.Fatalf("stdout = %q, want hello listing", stdout)
	}
}

func TestRunSearchAndInfo(t *testing.T) {
	cfg := testCLIConfig(t)
	writeCLIManifest(t, cfg, "hello", "1.0.0")
	writeCLIManifest(t, cfg, "help", "1.0.0")
	writeCLIManifest(t, cfg, "goodbye", "1.0.0")

	code, stdout, stderr := runCLI(t, []string{"search", "hel"})
	if code != 0 {
		t.Fatalf("search code = %d, stderr = %q", code, stderr)
	}
	if stdout != "hello\nhelp\n" {
		t.Fatalf("search stdout = %q, want hello/help", stdout)
	}

	code, stdout, stderr = runCLI(t, []string{"info", "hello"})
	if code != 0 {
		t.Fatalf("info code = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{
		"name hello\n",
		"version 1.0.0\n",
		"artifact darwin/arm64 file://hello.tar.gz\n",
		"bins hello\n",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("info stdout = %q, want substring %q", stdout, want)
		}
	}
}

func TestRunSearchAndInfoUsePackageMetadata(t *testing.T) {
	cfg := testCLIConfig(t)
	writeCLIManifest(t, cfg, "ripgrep", "15.1.0")
	writeCLIPackageMetadata(t, cfg, "ripgrep", "Recursively search directories for a regex pattern", "https://github.com/BurntSushi/ripgrep", "MIT OR Unlicense", []string{"search", "cli"})

	code, stdout, stderr := runCLI(t, []string{"search", "regex"})
	if code != 0 {
		t.Fatalf("search code = %d, stderr = %q", code, stderr)
	}
	if stdout != "ripgrep\tRecursively search directories for a regex pattern\tsearch,cli\n" {
		t.Fatalf("search stdout = %q, want ripgrep metadata", stdout)
	}

	code, stdout, stderr = runCLI(t, []string{"search", "BurntSushi"})
	if code != 0 {
		t.Fatalf("homepage search code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "ripgrep\t") {
		t.Fatalf("homepage search stdout = %q, want ripgrep", stdout)
	}

	code, stdout, stderr = runCLI(t, []string{"info", "ripgrep"})
	if code != 0 {
		t.Fatalf("info code = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{
		"name ripgrep\n",
		"summary Recursively search directories for a regex pattern\n",
		"homepage https://github.com/BurntSushi/ripgrep\n",
		"license MIT OR Unlicense\n",
		"categories search cli\n",
		"version 15.1.0\n",
		"yanked false\n",
		"artifact darwin/arm64 file://ripgrep.tar.gz\n",
		"bins ripgrep\n",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("info stdout = %q, want substring %q", stdout, want)
		}
	}
}

func TestRunUpdateClonesLocalGitRegistry(t *testing.T) {
	cfg := testCLIConfig(t)
	source := newCLIGitRegistry(t)
	t.Setenv(config.EnvRegistryURL, cliFileURL(source))

	code, stdout, stderr := runCLI(t, []string{"update"})
	if code != 0 {
		t.Fatalf("update code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "cloned registry "+cfg.RegistryDir+"\n") ||
		!strings.Contains(stdout, "revision ") {
		t.Fatalf("update stdout = %q, want clone result", stdout)
	}
	if _, err := os.Stat(filepath.Join(cfg.RegistryDir, "registry.toml")); err != nil {
		t.Fatalf("Stat(registry.toml) error = %v", err)
	}
}

func TestRunInstallSuggestsUpdateWhenRegistryMissing(t *testing.T) {
	testCLIConfig(t)

	code, _, stderr := runCLI(t, []string{"install", "missing"})
	if code != 1 {
		t.Fatalf("install code = %d, want 1", code)
	}
	for _, want := range []string{
		"package not found",
		"run `dpm update`",
		config.DefaultRegistryURL,
	} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("install stderr = %q, want substring %q", stderr, want)
		}
	}
}

func TestRunInstallSuggestsUpdateWhenPackageMissingFromRegistry(t *testing.T) {
	cfg := testCLIConfig(t)
	writeCLIRegistryMetadata(t, cfg)
	writeCLIPackageMetadata(t, cfg, "hello", "Hello", "https://example.com/hello", "MIT", []string{"demo"})

	code, _, stderr := runCLI(t, []string{"install", "missing"})
	if code != 1 {
		t.Fatalf("install code = %d, want 1", code)
	}
	for _, want := range []string{
		`package "missing" was not found in the current registry`,
		"run `dpm update`",
		config.DefaultRegistryURL,
	} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("install stderr = %q, want substring %q", stderr, want)
		}
	}
}

func TestRunRegistryValidate(t *testing.T) {
	cfg := testCLIConfig(t)
	writeCLIRegistryMetadata(t, cfg)
	writeCLIPackageMetadata(t, cfg, "hello", "Hello", "https://example.com/hello", "MIT", []string{"demo"})
	writeCLIManifest(t, cfg, "hello", "1.0.0")

	code, stdout, stderr := runCLI(t, []string{"registry", "validate", cfg.RegistryDir})
	if code != 0 {
		t.Fatalf("registry validate code = %d, stderr = %q", code, stderr)
	}
	if stdout != "registry valid "+cfg.RegistryDir+"\n" {
		t.Fatalf("registry validate stdout = %q, want valid", stdout)
	}
}

func TestRunRegistryValidateReportsIssues(t *testing.T) {
	cfg := testCLIConfig(t)
	writeCLIPackageMetadata(t, cfg, "hello", "Hello", "https://example.com/hello", "MIT", []string{"demo"})
	writeCLIManifest(t, cfg, "hello", "1.0.0")

	code, stdout, stderr := runCLI(t, []string{"registry", "validate", cfg.RegistryDir})
	if code != 1 {
		t.Fatalf("registry validate code = %d, want 1", code)
	}
	if !strings.Contains(stdout, "error registry.toml:") {
		t.Fatalf("registry validate stdout = %q, want registry.toml issue", stdout)
	}
	if !strings.Contains(stderr, "registry validation failed") {
		t.Fatalf("registry validate stderr = %q, want validation failure", stderr)
	}
}

func TestRunRegistryValidateVerifiesArtifacts(t *testing.T) {
	cfg := testCLIConfig(t)
	writeCLIRegistryMetadata(t, cfg)
	writeCLIPackageMetadata(t, cfg, "hello", "Hello", "https://example.com/hello", "MIT", []string{"demo"})
	artifact := filepath.Join(t.TempDir(), "hello.txt")
	if err := os.WriteFile(artifact, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	sum, err := checksum.FileSHA256(artifact)
	if err != nil {
		t.Fatalf("FileSHA256() error = %v", err)
	}
	writeCLIManifestWithArtifact(t, cfg, "hello", "1.0.0", "file://"+filepath.ToSlash(artifact), sum)

	code, stdout, stderr := runCLI(t, []string{"registry", "validate", "--verify-artifacts", cfg.RegistryDir})
	if code != 0 {
		t.Fatalf("registry validate code = %d, stderr = %q", code, stderr)
	}
	if stdout != "registry valid "+cfg.RegistryDir+"\n" {
		t.Fatalf("registry validate stdout = %q, want valid", stdout)
	}
}

func TestRunDoctor(t *testing.T) {
	cfg := testCLIConfig(t)
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs() error = %v", err)
	}
	t.Setenv("PATH", cfg.BinDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	code, stdout, stderr := runCLI(t, []string{"doctor"})
	if code != 0 {
		t.Fatalf("doctor code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "ok "+cfg.Root) {
		t.Fatalf("doctor stdout = %q, want root ok", stdout)
	}
	for _, want := range []string{
		"registry url " + config.DefaultRegistryURL + "\n",
		"registry path " + cfg.RegistryDir + "\n",
		"registry revision unavailable:",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("doctor stdout = %q, want substring %q", stdout, want)
		}
	}
	if strings.Contains(stdout, "path missing") {
		t.Fatalf("doctor stdout = %q, did not expect path warning", stdout)
	}
}

func TestRunDoctorReportsRegistryRevision(t *testing.T) {
	cfg := testCLIConfig(t)
	source := newCLIGitRegistry(t)
	t.Setenv(config.EnvRegistryURL, cliFileURL(source))

	code, _, stderr := runCLI(t, []string{"update"})
	if code != 0 {
		t.Fatalf("update code = %d, stderr = %q", code, stderr)
	}
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs() error = %v", err)
	}
	t.Setenv("PATH", cfg.BinDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	code, stdout, stderr := runCLI(t, []string{"doctor"})
	if code != 0 {
		t.Fatalf("doctor code = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{
		"registry url " + cliFileURL(source) + "\n",
		"registry path " + cfg.RegistryDir + "\n",
		"registry revision ",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("doctor stdout = %q, want substring %q", stdout, want)
		}
	}
	if strings.Contains(stdout, "registry revision unavailable") {
		t.Fatalf("doctor stdout = %q, did not expect unavailable revision", stdout)
	}
}

func TestRunBadArgs(t *testing.T) {
	testCLIConfig(t)

	code, _, stderr := runCLI(t, []string{"install"})
	if code != 1 {
		t.Fatalf("install without args code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "usage: dpm install <name>") {
		t.Fatalf("stderr = %q, want install usage", stderr)
	}

	code, _, stderr = runCLI(t, []string{"bogus"})
	if code != 2 {
		t.Fatalf("unknown command code = %d, want 2", code)
	}
	if !strings.Contains(stderr, "usage: dpm <command>") {
		t.Fatalf("stderr = %q, want top-level usage", stderr)
	}
}

func testCLIConfig(t *testing.T) config.Config {
	t.Helper()

	root := filepath.Join(t.TempDir(), "dpm-root")
	t.Setenv(config.EnvRoot, root)
	t.Setenv(config.EnvRegistryURL, "")
	cfg, err := config.FromRoot(root)
	if err != nil {
		t.Fatalf("FromRoot() error = %v", err)
	}

	return cfg
}

func runCLI(t *testing.T, args []string) (int, string, string) {
	t.Helper()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(context.Background(), args, &stdout, &stderr)

	return code, stdout.String(), stderr.String()
}

func writeCLIManifest(t *testing.T, cfg config.Config, name, version string) {
	t.Helper()
	writeCLIManifestWithArtifact(t, cfg, name, version, "file://"+name+".tar.gz", strings.Repeat("a", 64))
}

func writeCLIManifestWithArtifact(t *testing.T, cfg config.Config, name, version, url, sha256 string) {
	t.Helper()

	dir := filepath.Join(cfg.RegistryDir, "packages", name, "versions", version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	contents := `schema = 1
name = "` + name + `"
version = "` + version + `"
dependencies = []

[[artifacts]]
os = "darwin"
arch = "arm64"
url = "` + url + `"
sha256 = "` + sha256 + `"

[install]
bins = ["` + name + `"]
`
	if err := os.WriteFile(filepath.Join(dir, "dpm.toml"), []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func writeCLIRegistryMetadata(t *testing.T, cfg config.Config) {
	t.Helper()
	contents := `schema = 1
name = "dpm-core"
description = "Test registry"
`
	if err := os.MkdirAll(cfg.RegistryDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfg.RegistryDir, registry.MetadataFile), []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func writeCLIPackageMetadata(t *testing.T, cfg config.Config, name, summary, homepage, license string, categories []string) {
	t.Helper()

	dir := filepath.Join(cfg.RegistryDir, "packages", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	quotedCategories := make([]string, 0, len(categories))
	for _, category := range categories {
		quotedCategories = append(quotedCategories, `"`+category+`"`)
	}
	contents := `schema = 1
name = "` + name + `"
summary = "` + summary + `"
homepage = "` + homepage + `"
license = "` + license + `"
categories = [` + strings.Join(quotedCategories, ", ") + `]
`
	if err := os.WriteFile(filepath.Join(dir, registry.PackageFile), []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func newCLIGitRegistry(t *testing.T) string {
	t.Helper()
	requireCLIGit(t)

	root := filepath.Join(t.TempDir(), "source-registry")
	writeRawFile(t, filepath.Join(root, "registry.toml"), `schema = 1
name = "dpm-core"
description = "Test registry"
`)
	runGit(t, root, "init")
	runGit(t, root, "add", ".")
	runGit(t, root, "-c", "user.name=dpm-test", "-c", "user.email=dpm@example.invalid", "commit", "--no-gpg-sign", "-m", "initial registry")

	return root
}

func requireCLIGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not available: %v", err)
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s error = %v\n%s", strings.Join(args, " "), err, out)
	}

	return strings.TrimSpace(string(out))
}

func cliFileURL(path string) string {
	return "file://" + filepath.ToSlash(path)
}

func writeRawFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}
