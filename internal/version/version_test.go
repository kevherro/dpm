// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package version

import (
	"runtime"
	"testing"
)

func TestCurrent(t *testing.T) {
	info := Current()

	if info.Version != Version {
		t.Fatalf("Version = %q, want %q", info.Version, Version)
	}
	if info.Commit != Commit {
		t.Fatalf("Commit = %q, want %q", info.Commit, Commit)
	}
	if info.Date != Date {
		t.Fatalf("Date = %q, want %q", info.Date, Date)
	}
	if info.GoVersion != runtime.Version() {
		t.Fatalf("GoVersion = %q, want %q", info.GoVersion, runtime.Version())
	}
	if info.Platform != runtime.GOOS+"/"+runtime.GOARCH {
		t.Fatalf("Platform = %q, want %q/%q", info.Platform, runtime.GOOS, runtime.GOARCH)
	}
}
