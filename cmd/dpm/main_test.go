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

	"github.com/kevherro/dpm/internal/config"
	"github.com/kevherro/dpm/internal/link"
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

func TestRunUpdateChecksLocalRegistry(t *testing.T) {
	cfg := testCLIConfig(t)
	if err := os.MkdirAll(cfg.RegistryDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	code, stdout, stderr := runCLI(t, []string{"update"})
	if code != 0 {
		t.Fatalf("update code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "local registry updates are manual in v1") {
		t.Fatalf("update stdout = %q, want manual registry message", stdout)
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
	if strings.Contains(stdout, "path missing") {
		t.Fatalf("doctor stdout = %q, did not expect path warning", stdout)
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

	dir := filepath.Join(cfg.RegistryDir, "packages", name, version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	contents := `name = "` + name + `"
version = "` + version + `"
dependencies = []

[[artifacts]]
os = "darwin"
arch = "arm64"
url = "file://` + name + `.tar.gz"
sha256 = "` + strings.Repeat("a", 64) + `"

[install]
bins = ["` + name + `"]
`
	if err := os.WriteFile(filepath.Join(dir, "dpm.toml"), []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}
