// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package install

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kevherro/dpm/internal/config"
	"github.com/kevherro/dpm/internal/operationlock"
	"github.com/kevherro/dpm/internal/state"
)

var errInjectedLifecycle = errors.New("injected lifecycle failure")
var errInjectedCleanup = errors.New("injected cleanup failure")

func TestInstallRollsBackNewDependencyWhenParentFails(t *testing.T) {
	cfg := testInstallConfig(t)
	writeInstallGraph(t, cfg, "app", []string{"library"})

	_, err := redInstaller(func(point lifecyclePoint, name string) error {
		if point == afterPrefixCommit && name == "app" {
			return errInjectedLifecycle
		}
		return nil
	}).Install(context.Background(), cfg, "app")
	if !errors.Is(err, errInjectedLifecycle) {
		t.Fatalf("Install() error = %v, want injected failure", err)
	}

	assertPackageAbsent(t, cfg, "app")
	assertPackageAbsent(t, cfg, "library")
}

func TestInstallRollbackPreservesPreexistingDependency(t *testing.T) {
	cfg := testInstallConfig(t)
	writeInstallGraph(t, cfg, "library", nil)
	if _, err := redInstaller(nil).Install(context.Background(), cfg, "library"); err != nil {
		t.Fatalf("install library error = %v", err)
	}
	writeInstallGraph(t, cfg, "app", []string{"library"})

	_, err := redInstaller(func(point lifecyclePoint, name string) error {
		if point == afterPrefixCommit && name == "app" {
			return errInjectedLifecycle
		}
		return nil
	}).Install(context.Background(), cfg, "app")
	if !errors.Is(err, errInjectedLifecycle) {
		t.Fatalf("Install() error = %v, want injected failure", err)
	}

	assertPackageHealthy(t, cfg, "library")
	assertPackageAbsent(t, cfg, "app")
}

func TestInstallRollsBackAtEveryCommitStage(t *testing.T) {
	for _, point := range []lifecyclePoint{afterPrefixCommit, afterLinksCommit, afterStateCommit} {
		t.Run(string(point), func(t *testing.T) {
			cfg := testInstallConfig(t)
			writeInstallGraph(t, cfg, "app", nil)
			_, err := redInstaller(func(got lifecyclePoint, _ string) error {
				if got == point {
					return errInjectedLifecycle
				}
				return nil
			}).Install(context.Background(), cfg, "app")
			if !errors.Is(err, errInjectedLifecycle) {
				t.Fatalf("Install() error = %v, want injected failure", err)
			}

			assertPackageAbsent(t, cfg, "app")
			assertNoInstallStaging(t, cfg)
		})
	}
}

func TestInstallReportsPrimaryAndCleanupFailures(t *testing.T) {
	cfg := testInstallConfig(t)
	writeInstallGraph(t, cfg, "app", nil)
	installer := redInstaller(func(point lifecyclePoint, _ string) error {
		if point == beforeStateCommit {
			return errInjectedLifecycle
		}
		return nil
	})
	installer.cleanupPrefix = func(config.Config, string) error {
		return errInjectedCleanup
	}

	_, err := installer.Install(context.Background(), cfg, "app")
	if !errors.Is(err, errInjectedLifecycle) || !errors.Is(err, errInjectedCleanup) {
		t.Fatalf("Install() error = %v, want primary and cleanup failures", err)
	}
}

func TestInstallPreflightsWholeGraphBeforeMutation(t *testing.T) {
	tests := []struct {
		name         string
		setup        func(*testing.T, config.Config)
		preservedBin string
	}{
		{
			name: "missing transitive dependency",
			setup: func(t *testing.T, cfg config.Config) {
				writeInstallGraph(t, cfg, "ready", nil)
				writeInstallGraph(t, cfg, "app", []string{"ready", "missing"})
			},
		},
		{
			name: "unsupported parent platform",
			setup: func(t *testing.T, cfg config.Config) {
				writeInstallGraph(t, cfg, "library", nil)
				path, sum := makePackageArtifact(t, "app")
				writeRegistryManifest(t, cfg, manifestFixture{name: "app", version: "1.0.0", dependencies: []string{"library"}, url: "file://" + path, sha256: sum})
				manifestPath := filepath.Join(cfg.RegistryDir, "packages", "app", "versions", "1.0.0", "dpm.toml")
				data, err := os.ReadFile(manifestPath)
				if err != nil {
					t.Fatalf("ReadFile() error = %v", err)
				}
				data = []byte(strings.Replace(string(data), `arch = "arm64"`, `arch = "amd64"`, 1))
				if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
					t.Fatalf("WriteFile() error = %v", err)
				}
			},
		},
		{
			name:         "parent bin conflict",
			preservedBin: "user owned",
			setup: func(t *testing.T, cfg config.Config) {
				writeInstallGraph(t, cfg, "library", nil)
				writeInstallGraph(t, cfg, "app", []string{"library"})
				if err := os.MkdirAll(cfg.BinDir, 0o755); err != nil {
					t.Fatalf("MkdirAll() error = %v", err)
				}
				if err := os.WriteFile(filepath.Join(cfg.BinDir, "app"), []byte("user owned"), 0o644); err != nil {
					t.Fatalf("WriteFile() error = %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testInstallConfig(t)
			tt.setup(t, cfg)
			if _, err := redInstaller(nil).Install(context.Background(), cfg, "app"); err == nil {
				t.Fatal("Install() error = nil, want preflight error")
			}

			for _, name := range []string{"ready", "library", "app"} {
				assertPackageNotInstalled(t, cfg, name)
			}
			if tt.preservedBin != "" {
				assertFile(t, filepath.Join(cfg.BinDir, "app"), tt.preservedBin)
			}
		})
	}
}

func TestRemoveRollsBackAtEveryCommitStage(t *testing.T) {
	for _, point := range []lifecyclePoint{afterRemoveBins, afterRemovePrefix} {
		t.Run(string(point), func(t *testing.T) {
			cfg := testInstallConfig(t)
			writeInstallGraph(t, cfg, "app", nil)
			if _, err := redInstaller(nil).Install(context.Background(), cfg, "app"); err != nil {
				t.Fatalf("Install() error = %v", err)
			}

			_, err := remove(cfg, "app", func(got lifecyclePoint, _ string) error {
				if got == point {
					return errInjectedLifecycle
				}
				return nil
			})
			if !errors.Is(err, errInjectedLifecycle) {
				t.Fatalf("remove() error = %v, want injected failure", err)
			}

			assertPackageHealthy(t, cfg, "app")
		})
	}
}

func TestRemoveRefusesInstalledDependency(t *testing.T) {
	cfg := testInstallConfig(t)
	writeInstallGraph(t, cfg, "app", []string{"library"})
	if _, err := redInstaller(nil).Install(context.Background(), cfg, "app"); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	_, err := Remove(cfg, "library")
	if err == nil || !strings.Contains(err.Error(), "app") {
		t.Fatalf("Remove(library) error = %v, want dependent app refusal", err)
	}
	assertPackageHealthy(t, cfg, "library")
	assertPackageHealthy(t, cfg, "app")
}

func TestSameVersionReinstallRejectsDrift(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, config.Config)
	}{
		{name: "missing prefix", mutate: func(t *testing.T, cfg config.Config) {
			if err := os.RemoveAll(filepath.Join(cfg.PkgsDir, "app")); err != nil {
				t.Fatalf("RemoveAll() error = %v", err)
			}
		}},
		{name: "missing executable", mutate: func(t *testing.T, cfg config.Config) {
			if err := os.Remove(filepath.Join(cfg.PkgsDir, "app", "1.0.0", "bin", "app")); err != nil {
				t.Fatalf("Remove() error = %v", err)
			}
		}},
		{name: "missing link", mutate: func(t *testing.T, cfg config.Config) {
			if err := os.Remove(filepath.Join(cfg.BinDir, "app")); err != nil {
				t.Fatalf("Remove() error = %v", err)
			}
		}},
		{name: "retargeted link", mutate: func(t *testing.T, cfg config.Config) {
			linkPath := filepath.Join(cfg.BinDir, "app")
			if err := os.Remove(linkPath); err != nil {
				t.Fatalf("Remove() error = %v", err)
			}
			if err := os.Symlink(filepath.Join(cfg.PkgsDir, "other"), linkPath); err != nil {
				t.Fatalf("Symlink() error = %v", err)
			}
		}},
		{name: "dependency state drift", mutate: func(t *testing.T, cfg config.Config) {
			store := state.New(cfg)
			record, err := store.Get("app")
			if err != nil {
				t.Fatalf("Get() error = %v", err)
			}
			record.Dependencies = []string{"other"}
			if err := store.Save(record); err != nil {
				t.Fatalf("Save() error = %v", err)
			}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testInstallConfig(t)
			writeInstallGraph(t, cfg, "app", nil)
			installer := redInstaller(nil)
			if _, err := installer.Install(context.Background(), cfg, "app"); err != nil {
				t.Fatalf("first Install() error = %v", err)
			}
			tt.mutate(t, cfg)

			result, err := installer.Install(context.Background(), cfg, "app")
			if err == nil || (len(result.Packages) > 0 && result.Packages[0].AlreadyInstalled) {
				t.Fatalf("reinstall result = %#v, error = %v; want integrity error", result, err)
			}
		})
	}
}

func TestInstallRejectsInterruptedOperationEvidence(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, config.Config)
	}{
		{name: "stale install staging", setup: func(t *testing.T, cfg config.Config) {
			if err := os.Mkdir(filepath.Join(cfg.CacheDir, ".install-stale"), 0o755); err != nil {
				t.Fatalf("Mkdir() error = %v", err)
			}
		}},
		{name: "stale remove staging", setup: func(t *testing.T, cfg config.Config) {
			if err := os.Mkdir(filepath.Join(cfg.CacheDir, ".remove-stale"), 0o755); err != nil {
				t.Fatalf("Mkdir() error = %v", err)
			}
		}},
		{name: "prefix without state", setup: func(t *testing.T, cfg config.Config) {
			if err := os.MkdirAll(filepath.Join(cfg.PkgsDir, "orphan", "1.0.0"), 0o755); err != nil {
				t.Fatalf("MkdirAll() error = %v", err)
			}
		}},
		{name: "link without state", setup: func(t *testing.T, cfg config.Config) {
			if err := os.Symlink(filepath.Join(cfg.PkgsDir, "orphan", "1.0.0", "bin", "orphan"), filepath.Join(cfg.BinDir, "orphan")); err != nil {
				t.Fatalf("Symlink() error = %v", err)
			}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testInstallConfig(t)
			if err := cfg.EnsureDirs(); err != nil {
				t.Fatalf("EnsureDirs() error = %v", err)
			}
			tt.setup(t, cfg)
			writeInstallGraph(t, cfg, "app", nil)

			if _, err := redInstaller(nil).Install(context.Background(), cfg, "app"); err == nil || !strings.Contains(err.Error(), "doctor") {
				t.Fatalf("Install() error = %v, want interrupted-operation guidance", err)
			}
			assertPackageAbsent(t, cfg, "app")
		})
	}
}

func TestConcurrentListDoesNotObservePartialInstallGraph(t *testing.T) {
	cfg := testInstallConfig(t)
	writeInstallGraph(t, cfg, "app", []string{"library"})
	paused := make(chan struct{})
	release := make(chan struct{})
	writerDone := make(chan error, 1)
	go func() {
		_, err := redInstaller(func(point lifecyclePoint, name string) error {
			if point == afterPrefixCommit && name == "app" {
				close(paused)
				<-release
			}
			return nil
		}).Install(context.Background(), cfg, "app")
		writerDone <- err
	}()
	<-paused

	readDone := make(chan []state.Record, 1)
	go func() {
		lock, _ := operationlock.Acquire(cfg.Root, operationlock.Shared)
		defer lock.Close()
		records, _ := state.New(cfg).List()
		readDone <- records
	}()

	select {
	case records := <-readDone:
		close(release)
		<-writerDone
		if len(records) == 1 && records[0].Name == "library" {
			t.Fatalf("List() observed partially committed graph: %#v", records)
		}
	case <-time.After(250 * time.Millisecond):
		close(release)
		if err := <-writerDone; err != nil {
			t.Fatalf("Install() error = %v", err)
		}
		records := <-readDone
		if len(records) != 2 {
			t.Fatalf("List() after writer = %#v, want complete graph", records)
		}
	}
}

func TestConcurrentInstallsSerialize(t *testing.T) {
	cfg := testInstallConfig(t)
	writeInstallGraph(t, cfg, "app", nil)
	paused := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		_, err := redInstaller(func(point lifecyclePoint, _ string) error {
			if point == afterPrefixCommit {
				close(paused)
				<-release
			}
			return nil
		}).Install(context.Background(), cfg, "app")
		firstDone <- err
	}()
	<-paused

	secondDone := make(chan error, 1)
	go func() {
		_, err := redInstaller(nil).Install(context.Background(), cfg, "app")
		secondDone <- err
	}()

	select {
	case err := <-secondDone:
		close(release)
		<-firstDone
		t.Fatalf("second Install() completed before first released operation lock: %v", err)
	case <-time.After(250 * time.Millisecond):
		close(release)
		if err := <-firstDone; err != nil {
			t.Fatalf("first Install() error = %v", err)
		}
		if err := <-secondDone; err != nil {
			t.Fatalf("second Install() error = %v", err)
		}
	}
}

func TestConcurrentInstallAndRemoveSerialize(t *testing.T) {
	cfg := testInstallConfig(t)
	writeInstallGraph(t, cfg, "app", nil)
	paused := make(chan struct{})
	release := make(chan struct{})
	installDone := make(chan error, 1)
	go func() {
		_, err := redInstaller(func(point lifecyclePoint, _ string) error {
			if point == afterStateCommit {
				close(paused)
				<-release
			}
			return nil
		}).Install(context.Background(), cfg, "app")
		installDone <- err
	}()
	<-paused

	removeDone := make(chan error, 1)
	go func() {
		_, err := Remove(cfg, "app")
		removeDone <- err
	}()

	select {
	case err := <-removeDone:
		close(release)
		<-installDone
		t.Fatalf("Remove() completed before install released operation lock: %v", err)
	case <-time.After(250 * time.Millisecond):
		close(release)
		if err := <-installDone; err != nil {
			t.Fatalf("Install() error = %v", err)
		}
		if err := <-removeDone; err != nil {
			t.Fatalf("Remove() error = %v", err)
		}
	}
}

func TestConcurrentUnrelatedWritersSerialize(t *testing.T) {
	cfg := testInstallConfig(t)
	writeInstallGraph(t, cfg, "app", nil)
	writeInstallGraph(t, cfg, "other", nil)
	paused := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		_, err := redInstaller(func(point lifecyclePoint, _ string) error {
			if point == afterPrefixCommit {
				close(paused)
				<-release
			}
			return nil
		}).Install(context.Background(), cfg, "app")
		firstDone <- err
	}()
	<-paused

	secondDone := make(chan error, 1)
	go func() {
		_, err := redInstaller(nil).Install(context.Background(), cfg, "other")
		secondDone <- err
	}()

	select {
	case err := <-secondDone:
		close(release)
		<-firstDone
		t.Fatalf("unrelated writer completed before first released root lock: %v", err)
	case <-time.After(250 * time.Millisecond):
		close(release)
		if err := <-firstDone; err != nil {
			t.Fatalf("first Install() error = %v", err)
		}
		if err := <-secondDone; err != nil {
			t.Fatalf("second Install() error = %v", err)
		}
	}
}

func TestConcurrentProcessesSerialize(t *testing.T) {
	cfg := testInstallConfig(t)
	writeInstallGraph(t, cfg, "app", nil)
	readyR, readyW, err := os.Pipe()
	if err != nil {
		t.Fatalf("ready pipe error = %v", err)
	}
	releaseR, releaseW, err := os.Pipe()
	if err != nil {
		t.Fatalf("release pipe error = %v", err)
	}
	defer readyR.Close()
	defer releaseW.Close()

	first := lifecycleHelperCommand(t, cfg, "paused")
	first.ExtraFiles = []*os.File{readyW, releaseR}
	if err := first.Start(); err != nil {
		t.Fatalf("start first helper error = %v", err)
	}
	readyW.Close()
	releaseR.Close()
	if _, err := io.ReadFull(readyR, make([]byte, 1)); err != nil {
		t.Fatalf("wait for first helper error = %v", err)
	}

	second := lifecycleHelperCommand(t, cfg, "install")
	secondDone := make(chan error, 1)
	if err := second.Start(); err != nil {
		t.Fatalf("start second helper error = %v", err)
	}
	go func() { secondDone <- second.Wait() }()

	select {
	case err := <-secondDone:
		releaseW.Write([]byte{1})
		firstErr := first.Wait()
		t.Fatalf("second process completed before first released root lock: second=%v first=%v", err, firstErr)
	case <-time.After(500 * time.Millisecond):
		if _, err := releaseW.Write([]byte{1}); err != nil {
			t.Fatalf("release first helper error = %v", err)
		}
		if err := first.Wait(); err != nil {
			t.Fatalf("first helper error = %v", err)
		}
		if err := <-secondDone; err != nil {
			t.Fatalf("second helper error = %v", err)
		}
	}
}

func TestLifecycleHelperProcess(t *testing.T) {
	mode := os.Getenv("DPM_LIFECYCLE_HELPER")
	if mode == "" {
		return
	}
	cfg, err := config.FromRoot(os.Getenv("DPM_LIFECYCLE_ROOT"))
	if err != nil {
		t.Fatalf("FromRoot() error = %v", err)
	}
	installer := redInstaller(nil)
	if mode == "paused" {
		ready := os.NewFile(3, "ready")
		release := os.NewFile(4, "release")
		installer.hook = func(point lifecyclePoint, _ string) error {
			if point != afterPrefixCommit {
				return nil
			}
			if _, err := ready.Write([]byte{1}); err != nil {
				return err
			}
			_, err := io.ReadFull(release, make([]byte, 1))
			return err
		}
	}
	if _, err := installer.Install(context.Background(), cfg, "app"); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
}

func lifecycleHelperCommand(t *testing.T, cfg config.Config, mode string) *exec.Cmd {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("Executable() error = %v", err)
	}
	cmd := exec.Command(executable, "-test.run=^TestLifecycleHelperProcess$")
	cmd.Env = append(os.Environ(), "DPM_LIFECYCLE_HELPER="+mode, "DPM_LIFECYCLE_ROOT="+cfg.Root)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}

func redInstaller(hook lifecycleHook) Installer {
	return Installer{GOOS: "darwin", GOARCH: "arm64", hook: hook}
}

func writeInstallGraph(t *testing.T, cfg config.Config, name string, dependencies []string) {
	t.Helper()
	for _, dependency := range dependencies {
		if dependency == "missing" {
			continue
		}
		manifestPath := filepath.Join(cfg.RegistryDir, "packages", dependency, "versions", "1.0.0", "dpm.toml")
		if _, err := os.Stat(manifestPath); errors.Is(err, os.ErrNotExist) {
			writeInstallGraph(t, cfg, dependency, nil)
		}
	}
	path, sum := makePackageArtifact(t, name)
	writeRegistryManifest(t, cfg, manifestFixture{name: name, version: "1.0.0", dependencies: dependencies, url: "file://" + path, sha256: sum})
}

func assertPackageAbsent(t *testing.T, cfg config.Config, name string) {
	t.Helper()
	assertPackageNotInstalled(t, cfg, name)
	if _, err := os.Lstat(filepath.Join(cfg.BinDir, name)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("bin link %s exists after failed operation", name)
	}
}

func assertPackageNotInstalled(t *testing.T, cfg config.Config, name string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(cfg.PkgsDir, name)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("package directory %s exists after failed operation", name)
	}
	if _, err := state.New(cfg).Get(name); !errors.Is(err, state.ErrNotInstalled) {
		t.Errorf("state Get(%s) error = %v, want ErrNotInstalled", name, err)
	}
}

func assertPackageHealthy(t *testing.T, cfg config.Config, name string) {
	t.Helper()
	record, err := state.New(cfg).Get(name)
	if err != nil {
		t.Fatalf("state Get(%s) error = %v", name, err)
	}
	if _, err := os.Stat(record.Prefix); err != nil {
		t.Fatalf("package prefix %s error = %v", name, err)
	}
	for _, bin := range record.Bins {
		if _, err := os.Stat(bin.Source); err != nil {
			t.Fatalf("bin source %s error = %v", name, err)
		}
		target, err := os.Readlink(bin.Link)
		if err != nil || target != bin.Source {
			t.Fatalf("bin link %s target = %q, error = %v; want %s", name, target, err, bin.Source)
		}
	}
}

func assertNoInstallStaging(t *testing.T, cfg config.Config) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(cfg.CacheDir, ".install-*"))
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(matches) != 0 {
		t.Errorf("staging directories remain: %v", matches)
	}
}
